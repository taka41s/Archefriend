// +build windows

package main

import (
	"archefriend/buff"
	"archefriend/config"
	"archefriend/entity"
	"archefriend/gui"
	"archefriend/input"
	"archefriend/loot"
	"archefriend/monitor"
	"archefriend/process"
	"archefriend/reaction"
	"archefriend/target"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

const (
	OVERLAY_WIDTH  = 700
	OVERLAY_HEIGHT = 150
)

type App struct {
	// ArcheAge connection
	handle    windows.Handle
	x2game    uintptr
	connected bool
	mu        sync.RWMutex // Protege connected

	// Managers
	lootBypass      *loot.Bypass
	inputManager    *input.Manager
	reactionManager *reaction.Manager
	buffMonitor     *monitor.BuffMonitor
	debuffMonitor   *monitor.DebuffMonitor
	targetMonitor   *target.Monitor
	buffInjector    *buff.Injector
	presetManager   *buff.PresetManager

	// Keybinds
	keybinds *config.KeybindsConfig

	// Overlay
	window       *gui.OverlayWindow
	configWindow *gui.ConfigWindow
	buffWindow   *gui.BuffWindow
	visible      bool

	// Key states
	keyStates map[int]bool

	// Stats
	frameCount int

	// Goroutine control
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

	// Load keybinds
	kb, _ := config.LoadKeybinds("keybinds.json")
	if kb == nil {
		kb = &config.KeybindsConfig{}
	}
	app.keybinds = kb

	// Connect to ArcheAge
	pid, err := process.FindProcess("archeage.exe")
	if err != nil {
		fmt.Println("[WARN] ArcheAge não encontrado")
		return app, nil
	}

	handle, err := process.OpenProcess(pid)
	if err != nil {
		fmt.Printf("[WARN] Falha ao abrir processo: %v\n", err)
		return app, nil
	}

	x2game, err := process.GetModuleBase(pid, "x2game.dll")
	if err != nil {
		fmt.Printf("[WARN] x2game.dll não encontrado: %v\n", err)
		windows.CloseHandle(handle)
		return app, nil
	}

	app.handle = handle
	app.x2game = x2game
	app.connected = true

	// Initialize managers
	app.lootBypass = loot.NewBypass(handle, x2game)
	app.inputManager = input.NewManager()
	app.reactionManager = reaction.NewManager()
	app.buffMonitor = monitor.NewBuffMonitor(handle, x2game)
	app.debuffMonitor = monitor.NewDebuffMonitor(handle, x2game)
	app.targetMonitor = target.NewMonitor(handle, x2game)

	// Initialize buff injection
	app.buffInjector = buff.NewInjector(handle)
	app.presetManager = buff.NewPresetManager(app.buffInjector)

	// Start freeze loop for permanent buffs
	app.buffInjector.StartFreezeLoop()

	// Load presets
	if err := app.presetManager.LoadFromJSON("buff_presets.json"); err != nil {
		fmt.Printf("[WARN] Could not load buff presets: %v\n", err)
		// Create default presets
		app.presetManager.CreateDefaultPresets()
		app.presetManager.SaveToJSON("buff_presets.json")
	}

	// Load reactions
	app.reactionManager.LoadFromJSON("reactions.json")

	// Create config window
	configWindow, err := gui.NewConfigWindow(app.reactionManager)
	if err != nil {
		fmt.Printf("[WARN] Falha ao criar janela de configuração: %v\n", err)
	} else {
		app.configWindow = configWindow
	}

	// Create buff window
	buffWindow, err := gui.NewBuffWindow(app.buffInjector, app.presetManager)
	if err != nil {
		fmt.Printf("[WARN] Falha ao criar janela de buffs: %v\n", err)
	} else {
		app.buffWindow = buffWindow
	}

	// Setup callbacks
	app.buffMonitor.OnBuffGained = func(buff monitor.BuffInfo) {
		app.reactionManager.OnBuffGained(buff.ID)
	}

	app.buffMonitor.OnBuffLost = func(buffID uint32) {
		app.reactionManager.OnBuffLost(buffID)
	}

	app.debuffMonitor.OnDebuffGained = func(debuff monitor.DebuffInfo) {
		app.reactionManager.OnDebuffGained(debuff.TypeID)
	}

	app.debuffMonitor.OnDebuffLost = func(debuffTypeID uint32) {
		app.reactionManager.OnDebuffLost(debuffTypeID)
	}

	// Start background goroutines
	app.startBackgroundTasks()

	return app, nil
}

// startBackgroundTasks inicia todas as goroutines em background
func (app *App) startBackgroundTasks() {
	// Inicializa heartbeats
	now := time.Now()
	app.heartbeatMu.Lock()
	app.hotkeyHeartbeat = now
	app.monitorHeartbeat = now
	app.heartbeatMu.Unlock()

	// Goroutine para hotkeys
	go app.hotkeyLoop()

	// Goroutine para monitoramento de buffs/debuffs
	go app.monitorLoop()

	// Goroutine para watchdog
	go app.watchdogLoop()
}

// hotkeyLoop processa hotkeys em background
func (app *App) hotkeyLoop() {
	ticker := time.NewTicker(50 * time.Millisecond) // 20 FPS para hotkeys
	defer ticker.Stop()

	fmt.Println("[GOROUTINE] Hotkey loop iniciada")

	for {
		select {
		case <-app.stopChan:
			fmt.Println("[GOROUTINE] Hotkey loop encerrada")
			return
		case <-ticker.C:
			// Update heartbeat
			app.heartbeatMu.Lock()
			app.hotkeyHeartbeat = time.Now()
			app.heartbeatMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[ERROR] Panic no hotkey loop: %v\n", r)
					}
				}()
				app.pollHotkeys()
			}()
		}
	}
}

// monitorLoop processa buff/debuff monitoring em background
func (app *App) monitorLoop() {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 FPS para monitors
	defer ticker.Stop()

	fmt.Println("[GOROUTINE] Monitor loop iniciada")

	for {
		select {
		case <-app.stopChan:
			fmt.Println("[GOROUTINE] Monitor loop encerrada")
			return
		case <-ticker.C:
			// Update heartbeat
			app.heartbeatMu.Lock()
			app.monitorHeartbeat = time.Now()
			app.heartbeatMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[ERROR] Panic no monitor loop: %v\n", r)
					}
				}()

				app.mu.RLock()
				connected := app.connected
				app.mu.RUnlock()

				if !connected {
					return
				}

				// Get player entity address
				playerAddr := entity.GetPlayerEntityAddr(app.handle, app.x2game)
				if playerAddr == 0 {
					return
				}

				// Update monitors with player address
				app.buffMonitor.Update(playerAddr)
				app.debuffMonitor.Update(playerAddr)

				// Update buff list address for injector
				if app.buffInjector != nil {
					buffListAddr := app.buffMonitor.GetBuffListAddr(playerAddr)
					app.buffInjector.SetBuffListAddr(buffListAddr)
				}
			}()
		}
	}
}

// watchdogLoop monitora se as goroutines estão rodando
func (app *App) watchdogLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	fmt.Println("[GOROUTINE] Watchdog iniciado")

	for {
		select {
		case <-app.stopChan:
			fmt.Println("[GOROUTINE] Watchdog encerrado")
			return
		case <-ticker.C:
			app.heartbeatMu.Lock()
			hotkeyAge := time.Since(app.hotkeyHeartbeat)
			monitorAge := time.Since(app.monitorHeartbeat)
			app.heartbeatMu.Unlock()

			// Se heartbeat está muito antigo (>10 segundos), a goroutine pode ter travado
			if hotkeyAge > 10*time.Second && app.hotkeyHeartbeat.Unix() > 0 {
				fmt.Printf("[WATCHDOG] ⚠️ Hotkey loop pode ter travado (último heartbeat: %v atrás)\n", hotkeyAge)
			}

			if monitorAge > 10*time.Second && app.monitorHeartbeat.Unix() > 0 {
				fmt.Printf("[WATCHDOG] ⚠️ Monitor loop pode ter travado (último heartbeat: %v atrás)\n", monitorAge)
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
		0x7A: func() { // F11 - Diagnóstico
			app.printDiagnostics()
		},
	}

	for vk, callback := range keys {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		isPressed := (ret & 0x8000) != 0
		wasPressed := app.keyStates[vk]

		if isPressed && !wasPressed {
			// END (0x23) sempre funciona para poder mostrar/ocultar o overlay
			// Outras teclas só funcionam quando o overlay está visível
			if vk == 0x23 || app.visible {
				// Executar callback em goroutine para não bloquear
				// Com recover para evitar crashes
				cb := callback
				go func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Printf("[ERROR] Panic na callback de hotkey: %v\n", r)
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

	lines = append(lines, fmt.Sprintf("[F1] Loot:%s  [F2] Doodad:%s  [F3] Spam  [F4] AutoSpam:%s", lootStatus, doodadStatus, spamStatus))
	lines = append(lines, fmt.Sprintf("[F5] Reload  [F6] Reactions:%s  [END] Hide", reactionStatus))
	lines = append(lines, "[F7] Config  [F8] Buffs  [F9] Quick")
	lines = append(lines, fmt.Sprintf("Quick:%s (%s)", quickStatus, quickPreset))

	return lines
}

func (app *App) Update() {
	// Update apenas conta frames e atualiza UI
	// Hotkeys e monitoring são feitos em goroutines separadas
	// Nada pesado aqui para não bloquear a UI
}

func (app *App) printDiagnostics() {
	fmt.Println("\n╔════════════════════════════════════════╗")
	fmt.Println("║       DIAGNÓSTICO DO SISTEMA           ║")
	fmt.Println("╚════════════════════════════════════════╝")

	// Status de conexão
	app.mu.RLock()
	connected := app.connected
	app.mu.RUnlock()

	fmt.Printf("\n[CONEXÃO]\n")
	fmt.Printf("  Conectado: %v\n", connected)
	fmt.Printf("  Handle: 0x%X\n", app.handle)
	fmt.Printf("  X2Game Base: 0x%X\n", app.x2game)

	if !connected {
		fmt.Println("\n⚠️  Não conectado ao ArcheAge!")
		return
	}

	// Player address
	playerAddr := entity.GetPlayerEntityAddr(app.handle, app.x2game)
	fmt.Printf("\n[PLAYER]\n")
	fmt.Printf("  Player Address: 0x%X\n", playerAddr)

	if playerAddr == 0 {
		fmt.Println("\n⚠️  Player address é 0! Verifique se está no jogo.")
		return
	}

	// Buff Monitor
	if app.buffMonitor != nil {
		buffListAddr := app.buffMonitor.GetBuffListAddr(playerAddr)
		fmt.Printf("\n[BUFF MONITOR]\n")
		fmt.Printf("  Enabled: %v\n", app.buffMonitor.Enabled)
		fmt.Printf("  BuffList Address: 0x%X\n", buffListAddr)
		fmt.Printf("  Raw Count: %d\n", app.buffMonitor.RawCount)
		fmt.Printf("  Buffs Detectados: %d\n", len(app.buffMonitor.Buffs))
		fmt.Printf("  Known IDs: %d\n", len(app.buffMonitor.KnownIDs))

		if len(app.buffMonitor.Buffs) > 0 {
			fmt.Println("  Buffs atuais:")
			for _, buff := range app.buffMonitor.Buffs {
				fmt.Printf("    - ID:%d Duration:%d Left:%d Stack:%d\n",
					buff.ID, buff.Duration, buff.TimeLeft, buff.Stack)
			}
		}
	}

	// Debuff Monitor
	if app.debuffMonitor != nil {
		debuffBase := app.debuffMonitor.GetDebuffBase(playerAddr)
		fmt.Printf("\n[DEBUFF MONITOR]\n")
		fmt.Printf("  Enabled: %v\n", app.debuffMonitor.Enabled)
		fmt.Printf("  Debuff Base: 0x%X\n", debuffBase)
		fmt.Printf("  Raw Count: %d\n", app.debuffMonitor.RawCount)
		fmt.Printf("  Debuffs Detectados: %d\n", len(app.debuffMonitor.Debuffs))
		fmt.Printf("  Known IDs: %d\n", len(app.debuffMonitor.KnownIDs))

		if len(app.debuffMonitor.Debuffs) > 0 {
			fmt.Println("  Debuffs atuais:")
			for _, debuff := range app.debuffMonitor.Debuffs {
				fmt.Printf("    - ID:%d TypeID:%d DurMax:%d DurLeft:%d\n",
					debuff.ID, debuff.TypeID, debuff.DurMax, debuff.DurLeft)
			}
		}
	}

	// Reaction Manager
	if app.reactionManager != nil {
		fmt.Printf("\n[REACTION MANAGER]\n")
		fmt.Printf("  Enabled: %v\n", app.reactionManager.IsEnabled())
		fmt.Printf("  Active Reactions: %d\n", app.reactionManager.GetActiveCount())

		reactions := app.reactionManager.GetAllReactions()
		fmt.Printf("  Total Reactions: %d\n", len(reactions))
		if len(reactions) > 0 {
			fmt.Println("  Configured Reactions:")
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

	fmt.Println("\n════════════════════════════════════════")
	fmt.Println("Diagnóstico concluído. Pressione F11 novamente para atualizar.")
	fmt.Println("════════════════════════════════════════\n")
}

func (app *App) Close() {
	// Stop background goroutines
	close(app.stopChan)

	if app.inputManager != nil && app.inputManager.IsAutoSpamming() {
		app.inputManager.StopAutoSpam()
	}
	if app.lootBypass != nil {
		app.lootBypass.Cleanup()
	}
	if app.buffInjector != nil {
		app.buffInjector.StopFreezeLoop()
	}
	if app.handle != 0 {
		windows.CloseHandle(app.handle)
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║      ARCHEFRIEND OVERLAY v3.0         ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  Win32 Pure Overlay (No Background!)  ║")
	fmt.Println("║  F1: Loot | F2: Doodad | F3: Spam    ║")
	fmt.Println("║  F4: Auto | F5: Reload | F6: React   ║")
	fmt.Println("║  F7: Config | F8: Buffs | END: Hide  ║")
	fmt.Println("║  F9: Quick | F11: Diagnostico        ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	app, err := NewApp()
	if err != nil {
		fmt.Printf("[ERROR] %v\n", err)
		return
	}
	defer app.Close()

	// Create window
	window, err := gui.NewOverlayWindow(OVERLAY_WIDTH, OVERLAY_HEIGHT)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create window: %v\n", err)
		return
	}
	app.window = window

	// Procura a janela do jogo inicialmente
	window.FindGameWindow()

	// Força o overlay a ser visível no início
	window.SetVisible(true)

	// Update loop - simples e direto
	frameCount := 0
	lastUpdate := time.Now()

	for {
		// Processa todas as mensagens pendentes (não bloqueia)
		window.ProcessMessages()

		// Atualiza a cada ~16ms (60 FPS)
		now := time.Now()
		if now.Sub(lastUpdate) >= 16*time.Millisecond {
			lastUpdate = now

			// Atualiza lógica do jogo
			app.Update()

			// Atualiza display
			lines := app.getDisplayLines()
			window.SetLines(lines)

			// Atualiza posição do overlay a cada 30 frames (~0.5s)
			frameCount++
			if frameCount%30 == 0 {
				window.UpdatePosition()
			}
		} else {
			// Pequena pausa para não consumir 100% CPU
			time.Sleep(1 * time.Millisecond)
		}
	}
}
