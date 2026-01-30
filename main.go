// +build windows

package main

import (
	"archefriend/afk"
	"archefriend/buff"
	"archefriend/config"
	"archefriend/entity"
	"archefriend/esp"
	"archefriend/gui"
	"archefriend/input"
	"archefriend/loot"
	"archefriend/monitor"
	"archefriend/process"
	"archefriend/reaction"
	"archefriend/skill"
	"archefriend/target"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	OVERLAY_WIDTH  = 700
	OVERLAY_HEIGHT = 150
)

type App struct {
	handle    windows.Handle
	x2game    uintptr
	connected bool
	mu        sync.RWMutex
	pid       uint32 // PID do ArcheAge
	gameHwnd  uintptr // Handle da janela do ArcheAge

	lootBypass      *loot.Bypass
	inputManager    *input.Manager
	reactionManager *reaction.Manager
	afkMonitor      *afk.Monitor
	buffMonitor     *monitor.BuffMonitor
	debuffMonitor   *monitor.DebuffMonitor
	targetMonitor   *target.Monitor
	buffInjector    *buff.Injector
	presetManager   *buff.PresetManager
	espManager           *esp.Manager
	skillMonitor         *skill.SkillMonitor
	skillReactionManager *skill.ReactionManager
	keybinds             *config.KeybindsConfig
	targetScanner        *esp.TargetScanner

	window            *gui.OverlayWindow
	configWindow      *gui.ConfigWindow
	buffWindow        *gui.BuffWindow
	skillConfigWindow *gui.SkillConfigWindow
	autospamWindow    *gui.AutoSpamWindow
	visible           bool
	keyStates    map[int]bool
	frameCount   int

	stopChan         chan struct{}
	hotkeyHeartbeat  time.Time
	monitorHeartbeat time.Time
	heartbeatMu      sync.Mutex
}

func NewApp() (*App, error) {
	app := &App{
		visible:   true,
		keyStates: make(map[int]bool),
		stopChan:  make(chan struct{}),
	}

	kb, _ := config.LoadKeybinds("keybinds.json")
	if kb == nil {
		kb = &config.KeybindsConfig{}
	}
	app.keybinds = kb

	pid, err := process.FindProcess("archeage.exe")
	if err != nil {
		return app, nil
	}

	handle, err := process.OpenProcess(pid)
	if err != nil {
		windows.CloseHandle(handle)
		return app, nil
	}

	x2game, err := process.GetModuleBase(pid, "x2game.dll")
	if err != nil {
		windows.CloseHandle(handle)
		return app, nil
	}

	app.handle = handle
	app.x2game = x2game
	app.pid = pid
	app.connected = true

	// Encontrar janela do ArcheAge usando o PID
	app.gameHwnd = findWindowByPID(pid)

	app.lootBypass = loot.NewBypass(handle, x2game)
	app.inputManager = input.NewManager()

	// Configurar inputManager para enviar para a janela do ArcheAge
	app.inputManager.SetGameWindow(app.gameHwnd)
	// Configurar teclas padrão: V e SHIFT+F
	app.inputManager.SetKeys([][]uint16{
		{input.VK_V},
		{input.VK_LSHIFT, input.VK_F},
	})

	app.afkMonitor = afk.NewMonitor(10)
	app.afkMonitor.OnStateChange = func(isAFK bool) {
		if isAFK {
			fmt.Println("[AFK] No input detected for 10s - reactions paused")
		} else {
			fmt.Println("[AFK] Input detected - reactions resumed")
		}
	}
	app.afkMonitor.Start()
	app.reactionManager = reaction.NewManager()
	app.reactionManager.SetAFKChecker(app.afkMonitor)
	app.buffMonitor = monitor.NewBuffMonitor(handle, x2game)
	app.debuffMonitor = monitor.NewDebuffMonitor(handle, x2game)
	app.targetMonitor = target.NewMonitor(handle, x2game)
	app.buffInjector = buff.NewInjector(handle)
	app.presetManager = buff.NewPresetManager(app.buffInjector)
	app.buffInjector.StartFreezeLoop()

	// Create ESP manager
	espMgr, err := esp.NewManager(uintptr(handle), pid, x2game)
	if err != nil {
		fmt.Printf("[WARN] Falha ao criar ESP: %v\n", err)
	} else {
		app.espManager = espMgr
		// Criar scanner de target para debug
		app.targetScanner = espMgr.NewTargetScanner()

		// Carregar configuracao do aimbot
		if err := espMgr.LoadAimbotConfig("aimbot_config.json"); err != nil {
			fmt.Printf("[AIMBOT] Config não encontrada, usando padrão (Mouse4, Mouse5)\n")
			espMgr.SetAimbotKeys([]int{0x05, 0x06})
		}
	}

	// Create Skill monitor (offset 0x569E1A para hook de skill success)
	app.skillMonitor = skill.NewSkillMonitor(handle, x2game, 0x569E1A)
	if err := app.skillMonitor.LoadConfig("skills.json"); err != nil {
		fmt.Printf("[SKILL] Config não encontrada, usando padrão\n")
	}

	// Create Skill reaction manager
	app.skillReactionManager = skill.NewReactionManager()
	if err := app.skillReactionManager.LoadFromJSON("skill_reactions.json"); err != nil {
		fmt.Printf("[SKILL-REACT] Config não encontrada, criando padrão\n")
		skill.SaveDefaultReactions("skill_reactions.json")
		app.skillReactionManager.LoadFromJSON("skill_reactions.json")
	}

	// Configurar parser e executor de teclas
	app.skillReactionManager.SetKeyParser(input.ParseKeySequence)
	app.skillReactionManager.ExecuteKeys = input.SendKeySequence

	// Configurar aimbot callback
	app.skillReactionManager.AimAtTarget = func() bool {
		if app.espManager != nil {
			return app.espManager.AimAtTarget()
		}
		return false
	}

	// Callback para printar skill usada E executar reações
	app.skillMonitor.OnSkillCast = func(skillID uint32) {
		name := app.skillMonitor.GetSkillName(skillID)
		fmt.Printf("[SKILL] >>> %s (ID:%d) usado! <<<\n", name, skillID)

		// Executar reação se configurada
		app.skillReactionManager.OnSkillCast(skillID)
	}

	// Callback para tentativa de uso de skill (antes do cast)
	app.skillMonitor.OnSkillTry = func(skillID uint32) {
		name := app.skillMonitor.GetSkillName(skillID)
		fmt.Printf("[SKILL-TRY] Tentando usar %s (ID:%d)\n", name, skillID)

		// Executar aimbot se configurado para OnTry
		app.skillReactionManager.OnSkillTry(skillID)
	}

	// Ativar hook automaticamente
	if err := app.skillMonitor.InstallHook(); err != nil {
		fmt.Printf("[SKILL] Falha ao instalar hook: %v\n", err)
	} else {
		fmt.Println("[SKILL] Monitor de skills ATIVADO")
	}

	if err := app.presetManager.LoadFromJSON("buff_presets.json"); err != nil {
		app.presetManager.CreateDefaultPresets()
		app.presetManager.SaveToJSON("buff_presets.json")
	}

	app.reactionManager.LoadFromJSON("reactions.json")
	app.buffMonitor.SetReactionHandler(app.reactionManager)
	app.debuffMonitor.SetReactionHandler(app.reactionManager)

	configWindow, err := gui.NewConfigWindow(app.reactionManager)
	if err == nil {
		app.configWindow = configWindow
	}

	buffWindow, err := gui.NewBuffWindow(app.buffInjector, app.presetManager)
	if err == nil {
		app.buffWindow = buffWindow
	}

	skillConfigWindow, err := gui.NewSkillConfigWindow(app.skillReactionManager, "skill_reactions.json")
	if err == nil {
		app.skillConfigWindow = skillConfigWindow
	}

	autospamWindow, err := gui.NewAutoSpamWindow(app.inputManager)
	if err == nil {
		app.autospamWindow = autospamWindow
	}

	// Setup reaction callbacks
	app.buffMonitor.OnBuffGained = func(buff monitor.BuffInfo) {
		app.reactionManager.OnBuffGained(buff.ID)
	}
	app.buffMonitor.OnBuffLost = func(buffID uint32) {
		app.reactionManager.OnBuffLost(buffID)
	}
	app.debuffMonitor.OnDebuffGained = func(debuff monitor.DebuffInfo) {
		fmt.Printf("[MAIN] Debuff detectado: TypeID:%d (instance ID:%d)\n", debuff.TypeID, debuff.ID)
		app.reactionManager.OnDebuffGained(debuff.TypeID)
	}
	app.debuffMonitor.OnDebuffLost = func(debuffTypeID uint32) {
		app.reactionManager.OnDebuffLost(debuffTypeID)
	}

	app.startBackgroundTasks()

	return app, nil
}

func (app *App) startBackgroundTasks() {
	now := time.Now()
	app.heartbeatMu.Lock()
	app.hotkeyHeartbeat = now
	app.monitorHeartbeat = now
	app.heartbeatMu.Unlock()

	go app.hotkeyLoop()
	go app.monitorLoop()
	go app.watchdogLoop()
}

func (app *App) hotkeyLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-app.stopChan:
			return
		case <-ticker.C:
			app.heartbeatMu.Lock()
			app.hotkeyHeartbeat = time.Now()
			app.heartbeatMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[ERROR] Panic in hotkey loop: %v\n", r)
					}
				}()
				app.pollHotkeys()
			}()
		}
	}
}

func (app *App) monitorLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-app.stopChan:
			return
		case <-ticker.C:
			app.heartbeatMu.Lock()
			app.monitorHeartbeat = time.Now()
			app.heartbeatMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[ERROR] Panic in monitor loop: %v\n", r)
					}
				}()

				app.mu.RLock()
				connected := app.connected
				app.mu.RUnlock()

				if !connected {
					return
				}

				playerAddr := entity.GetPlayerEntityAddr(app.handle, app.x2game)
				if playerAddr == 0 {
					return
				}

				app.buffMonitor.Update(playerAddr)
				app.debuffMonitor.Update(playerAddr)

				// Update skill monitor
				if app.skillMonitor != nil && app.skillMonitor.Enabled {
					app.skillMonitor.Update()
				}

				// Update target monitor
				if app.targetMonitor != nil {
					// Obter posição do player para calcular distância
					player := entity.GetLocalPlayer(app.handle, app.x2game)
					app.targetMonitor.Update(player.PosX, player.PosY, player.PosZ)
				}

				if app.buffInjector != nil {
					buffListAddr := app.buffMonitor.GetBuffListAddr(playerAddr)
					app.buffInjector.SetBuffListAddr(buffListAddr)
				}
			}()
		}
	}
}

func (app *App) watchdogLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.stopChan:
			return
		case <-ticker.C:
			app.heartbeatMu.Lock()
			hotkeyAge := time.Since(app.hotkeyHeartbeat)
			monitorAge := time.Since(app.monitorHeartbeat)
			app.heartbeatMu.Unlock()

			if hotkeyAge > 10*time.Second && app.hotkeyHeartbeat.Unix() > 0 {
				fmt.Printf("[WATCHDOG] Hotkey loop may be stuck (last heartbeat: %v ago)\n", hotkeyAge)
			}

			if monitorAge > 10*time.Second && app.monitorHeartbeat.Unix() > 0 {
				fmt.Printf("[WATCHDOG] Monitor loop may be stuck (last heartbeat: %v ago)\n", monitorAge)
			}
		}
	}
}

func (app *App) pollHotkeys() {
	user32 := windows.NewLazyDLL("user32.dll")
	procGetAsyncKeyState := user32.NewProc("GetAsyncKeyState")

	keys := map[int]func(){
		0x70: func() { // F1
			if app.lootBypass != nil {
				app.lootBypass.ToggleLoot()
			}
		},
		0x71: func() { // F2
			if app.lootBypass != nil {
				app.lootBypass.ToggleDoodad()
			}
		},
		0x72: func() { // F3
			if app.inputManager != nil {
				app.inputManager.SendSingle()
			}
		},
		0x73: func() { // F4
			if app.inputManager != nil {
				app.inputManager.ToggleAutoSpam()
			}
		},
		0x74: func() { // F5
			if app.reactionManager != nil {
				app.reactionManager.ReloadFromJSON()
			}
		},
		0x23: func() { // END
			app.visible = !app.visible
			if app.window != nil {
				app.window.SetVisible(app.visible)
			}
		},
		0x75: func() { // F6
			if app.reactionManager != nil {
				app.reactionManager.Toggle()
			}
		},
		0x76: func() { // F7
			if app.configWindow != nil {
				app.configWindow.Toggle()
			}
		},
		0x77: func() { // F8
			if app.buffWindow != nil {
				app.buffWindow.Toggle()
			}
		},
		0x78: func() { // F9
			if app.presetManager != nil {
				app.presetManager.ToggleQuickAction()
			}
		},
		0x79: func() { // F10
			if app.afkMonitor != nil {
				app.afkMonitor.Toggle()
			}
		},
		0x2D: func() { // INSERT - AutoSpam Config
			if app.autospamWindow != nil {
				app.autospamWindow.Toggle()
			}
		},
		0x7A: func() { // F11
			app.printDiagnostics()
		},
		0x7B: func() { // F12
			if app.espManager != nil {
				enabled := app.espManager.Toggle()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] Target ESP: %s\n", status)
			}
		},
		0x24: func() { // HOME - Cycle ESP style
			if app.espManager != nil && app.espManager.IsEnabled() {
				style := app.espManager.CycleStyle()
				fmt.Printf("[ESP] Style: %s\n", app.espManager.GetStyleName())
				_ = style
			}
		},
		0x22: func() { // PAGE DOWN - Aim once
			if app.espManager != nil && app.espManager.IsEnabled() {
				if app.espManager.AimAtTarget() {
					fmt.Println("[ESP] Aimed at target")
				}
			}
		},
		0x21: func() { // PAGE UP - Skill Config Window
			if app.skillConfigWindow != nil {
				app.skillConfigWindow.Toggle()
			}
		},
		0x91: func() { // SCROLL LOCK - Start/Stop Target Scanner
			if app.targetScanner != nil {
				if app.targetScanner.IsScanning() {
					app.targetScanner.StopScanning()
				} else {
					if err := app.targetScanner.StartScanning(); err != nil {
						fmt.Printf("[SCANNER] Erro: %v\n", err)
					}
				}
			}
		},
		0x13: func() { // PAUSE - Trigger scan (pressione quando mudar de target)
			if app.targetScanner != nil && app.targetScanner.IsScanning() {
				app.targetScanner.ScanForChanges("TARGET_CHANGE")
			}
		},
		0x2E: func() { // DELETE - Dump full memory region
			if app.targetScanner != nil && app.targetScanner.IsScanning() {
				app.targetScanner.DumpRegion()
				fmt.Println("[SCANNER] Memory dump salvo!")
			}
		},
	}

	for vk, callback := range keys {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		isPressed := (ret & 0x8000) != 0
		wasPressed := app.keyStates[vk]

		if isPressed && !wasPressed {
			if vk == 0x23 || app.visible {
				cb := callback
				go func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Printf("[ERROR] Panic in hotkey callback: %v\n", r)
						}
					}()
					cb()
				}()
			}
		}

		app.keyStates[vk] = isPressed
	}
}

func (app *App) getDisplayLines() []string {
	lines := []string{}

	app.mu.RLock()
	connected := app.connected
	app.mu.RUnlock()

	status := "DISCONNECTED"
	if connected {
		status = "CONNECTED"
	}

	activeReactions := 0
	if app.reactionManager != nil {
		activeReactions = app.reactionManager.GetActiveCount()
	}

	lines = append(lines, fmt.Sprintf("ARCHEFRIEND [%s] | Reactions: %d", status, activeReactions))
	lines = append(lines, "────────────────────────────────────────────────────────")

	lootStatus := "OFF"
	if app.lootBypass != nil && app.lootBypass.IsLootEnabled() {
		lootStatus = "ON"
	}

	doodadStatus := "OFF"
	if app.lootBypass != nil && app.lootBypass.IsDoodadEnabled() {
		doodadStatus = "ON"
	}

	spamStatus := "OFF"
	if app.inputManager != nil && app.inputManager.IsAutoSpamming() {
		spamStatus = "ON"
	}

	quickStatus := "OFF"
	quickPreset := "None"
	if app.presetManager != nil {
		quickPreset = app.presetManager.GetQuickActionPreset()
		if quickPreset == "" {
			quickPreset = "None"
		}
		if app.presetManager.IsQuickActionActive() {
			quickStatus = "ON"
		}
	}

	reactionStatus := "OFF"
	if app.reactionManager != nil && app.reactionManager.IsEnabled() {
		reactionStatus = "ON"
	}

	afkStatus := "-"
	if app.afkMonitor != nil && app.afkMonitor.IsEnabled() {
		if app.afkMonitor.IsAFK() {
			afkStatus = "AFK"
		} else {
			afkStatus = "OK"
		}
	}

	espStatus := "OFF"
	espStyle := ""
	if app.espManager != nil && app.espManager.IsEnabled() {
		espStatus = "ON"
		espStyle = app.espManager.GetStyleName()
	}

	lines = append(lines, fmt.Sprintf("[F1] Loot:%s  [F2] Doodad:%s  [F3] Spam  [F4] AutoSpam:%s", lootStatus, doodadStatus, spamStatus))
	lines = append(lines, fmt.Sprintf("[F5] Reload  [F6] Reactions:%s  [F10] AFK:%s", reactionStatus, afkStatus))
	lines = append(lines, fmt.Sprintf("[F12] ESP:%s %s  [PGDN] Aim", espStatus, espStyle))
	lines = append(lines, "[F7] Config  [F8] Buffs  [F9] Quick  [PGUP] Skills  [END] Hide")
	lines = append(lines, fmt.Sprintf("Quick:%s (%s)", quickStatus, quickPreset))

	return lines
}

func (app *App) Update() {
}

func (app *App) printDiagnostics() {
	fmt.Println("\n╔════════════════════════════════════════╗")
	fmt.Println("║         SYSTEM DIAGNOSTICS             ║")
	fmt.Println("╚════════════════════════════════════════╝")

	app.mu.RLock()
	connected := app.connected
	app.mu.RUnlock()

	// Debug HP offsets
	if app.targetMonitor != nil {
		fmt.Println("\n[SCANNING TARGET HP OFFSETS...]")
		app.targetMonitor.DebugScanHP()
	}

	fmt.Printf("\n[CONNECTION]\n")
	fmt.Printf("  Connected: %v\n", connected)
	fmt.Printf("  Handle: 0x%X\n", app.handle)
	fmt.Printf("  X2Game Base: 0x%X\n", app.x2game)

	if !connected {
		fmt.Println("\n  Not connected to ArcheAge!")
		return
	}

	playerAddr := entity.GetPlayerEntityAddr(app.handle, app.x2game)
	fmt.Printf("\n[PLAYER]\n")
	fmt.Printf("  Address: 0x%X\n", playerAddr)

	if playerAddr == 0 {
		fmt.Println("\n  Player address is 0! Check if you are in game.")
		return
	}

	if app.buffMonitor != nil {
		buffListAddr := app.buffMonitor.GetBuffListAddr(playerAddr)
		fmt.Printf("\n[BUFF MONITOR]\n")
		fmt.Printf("  Enabled: %v\n", app.buffMonitor.Enabled)
		fmt.Printf("  BuffList Address: 0x%X\n", buffListAddr)
		fmt.Printf("  Raw Count: %d\n", app.buffMonitor.RawCount)
		fmt.Printf("  Detected: %d\n", len(app.buffMonitor.Buffs))
		fmt.Printf("  Known IDs: %d\n", len(app.buffMonitor.KnownIDs))

		if len(app.buffMonitor.Buffs) > 0 {
			fmt.Println("  Current buffs:")
			for _, buff := range app.buffMonitor.Buffs {
				fmt.Printf("    - ID:%d Duration:%d Left:%d Stack:%d\n",
					buff.ID, buff.Duration, buff.TimeLeft, buff.Stack)
			}
		}
	}

	if app.debuffMonitor != nil {
		debuffBase := app.debuffMonitor.GetDebuffBase(playerAddr)
		fmt.Printf("\n[DEBUFF MONITOR]\n")
		fmt.Printf("  Enabled: %v\n", app.debuffMonitor.Enabled)
		fmt.Printf("  Debuff Base: 0x%X\n", debuffBase)
		fmt.Printf("  Raw Count: %d\n", app.debuffMonitor.RawCount)
		fmt.Printf("  Detected: %d\n", len(app.debuffMonitor.Debuffs))
		fmt.Printf("  Known IDs: %d\n", len(app.debuffMonitor.KnownIDs))

		if len(app.debuffMonitor.Debuffs) > 0 {
			fmt.Println("  Current debuffs:")
			for _, debuff := range app.debuffMonitor.Debuffs {
				fmt.Printf("    - ID:%d TypeID:%d DurMax:%d DurLeft:%d\n",
					debuff.ID, debuff.TypeID, debuff.DurMax, debuff.DurLeft)
			}
		}
	}

	if app.afkMonitor != nil {
		fmt.Printf("\n[AFK MONITOR]\n")
		fmt.Printf("  Enabled: %v\n", app.afkMonitor.IsEnabled())
		fmt.Printf("  Timeout: %ds\n", app.afkMonitor.GetTimeout())
		fmt.Printf("  Idle: %ds\n", app.afkMonitor.GetIdleSeconds())
		fmt.Printf("  Is AFK: %v\n", app.afkMonitor.IsAFK())
	}

	if app.reactionManager != nil {
		fmt.Printf("\n[REACTION MANAGER]\n")
		fmt.Printf("  Enabled: %v\n", app.reactionManager.IsEnabled())
		fmt.Printf("  Active: %d\n", app.reactionManager.GetActiveCount())

		reactions := app.reactionManager.GetAllReactions()
		fmt.Printf("  Total: %d\n", len(reactions))
		if len(reactions) > 0 {
			fmt.Println("  Configured:")
			for _, r := range reactions {
				rType := "BUFF"
				if r.IsDebuff {
					rType = "DEBUFF"
				}
				fmt.Printf("    - [%s] %s (ID:%d) OnStart:%s OnEnd:%s\n",
					rType, r.Name, r.ID, r.UseString, r.OnEndString)
			}
		}
	}

	if app.espManager != nil {
		fmt.Printf("\n[ESP TARGET DEBUG]\n")
		app.espManager.DebugTargetInfo()
		fmt.Printf("\n[AIMBOT DEBUG]\n")
		app.espManager.AimAtTargetDebug(true)
	}

	fmt.Println("\n════════════════════════════════════════")
	fmt.Println("Press F11 again to refresh.")
	fmt.Println("════════════════════════════════════════\n")
}

// findWindowByPID encontra a janela principal de um processo pelo PID
func findWindowByPID(targetPID uint32) uintptr {
	user32 := windows.NewLazyDLL("user32.dll")
	procEnumWindows := user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId := user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible := user32.NewProc("IsWindowVisible")

	var foundHwnd uintptr

	// Callback para EnumWindows
	callback := func(hwnd uintptr, lParam uintptr) uintptr {
		// Verificar se a janela é visível
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1 // Continuar enumeração
		}

		// Obter PID da janela
		var windowPID uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))

		// Se for o PID do ArcheAge, salvar e parar
		if windowPID == targetPID {
			foundHwnd = hwnd
			return 0 // Parar enumeração
		}

		return 1 // Continuar enumeração
	}

	// Enumerar todas as janelas
	procEnumWindows.Call(
		windows.NewCallback(callback),
		0,
	)

	return foundHwnd
}

func (app *App) Close() {
	close(app.stopChan)

	if app.inputManager != nil && app.inputManager.IsAutoSpamming() {
		app.inputManager.StopAutoSpam()
	}
	if app.afkMonitor != nil {
		app.afkMonitor.Stop()
	}
	if app.lootBypass != nil {
		app.lootBypass.Cleanup()
	}
	if app.buffInjector != nil {
		app.buffInjector.StopFreezeLoop()
	}
	if app.targetScanner != nil && app.targetScanner.IsScanning() {
		app.targetScanner.StopScanning()
	}
	if app.espManager != nil {
		app.espManager.Close()
	}
	if app.skillMonitor != nil {
		app.skillMonitor.Close()
	}
	if app.handle != 0 {
		windows.CloseHandle(app.handle)
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║      ARCHEFRIEND OVERLAY v3.5         ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  F1: Loot | F2: Doodad | F3: Spam    ║")
	fmt.Println("║  F4: Auto | F5: Reload | F6: React   ║")
	fmt.Println("║  F7: Config | F8: Buffs | F9: Quick  ║")
	fmt.Println("║  F10: AFK | F11: Diag | F12: ESP     ║")
	fmt.Println("║  PGDN: Aim | HOME: Style | END: Hide ║")
	fmt.Println("║  PGUP: Skill Config                  ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  Aimbot: Config keys (aimbot_config) ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	app, err := NewApp()
	if err != nil {
		fmt.Printf("[ERROR] %v\n", err)
		return
	}
	defer app.Close()

	window, err := gui.NewOverlayWindow(OVERLAY_WIDTH, OVERLAY_HEIGHT)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create window: %v\n", err)
		return
	}
	app.window = window

	window.FindGameWindow()
	window.SetVisible(true)

	frameCount := 0
	lastUpdate := time.Now()

	for {
		window.ProcessMessages()

		now := time.Now()
		if now.Sub(lastUpdate) >= 16*time.Millisecond {
			lastUpdate = now

			app.Update()

			lines := app.getDisplayLines()
			window.SetLines(lines)

			frameCount++
			if frameCount%30 == 0 {
				window.UpdatePosition()
			}
		} else {
			time.Sleep(1 * time.Millisecond)
		}
	}
}
