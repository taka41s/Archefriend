// +build windows

package main

import (
	"archefriend/afk"
	"archefriend/bot"
	"archefriend/buff"
	"archefriend/config"
	"archefriend/entity"
	"archefriend/esp"
	"archefriend/gui"
	"archefriend/input"
	"archefriend/loot"
	"archefriend/monitor"
	"archefriend/patch"
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
	pid       uint32
	gameHwnd  uintptr

	patchManager    *patch.Manager

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

	// Bot
	botInstance  *bot.Bot
	botConfig    *bot.FileConfig

	window            *gui.OverlayWindow
	configWindow      *gui.ConfigWindow
	buffWindow        *gui.BuffWindow
	skillConfigWindow *gui.SkillConfigWindow
	autospamWindow    *gui.AutoSpamWindow
	botConfigWindow   *gui.BotConfigWindow
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

	// Aplicar patches de mount + GCD
	app.patchManager = patch.NewManager(handle, x2game)
	app.patchManager.ApplyAll()

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

		// Iniciar ambos ESPs por padrão
		espMgr.Enable()
		espMgr.ToggleAllEntities()
		fmt.Println("[ESP] Target ESP e All Entities ESP iniciados automaticamente")
	}

	// ============================
	// Bot setup
	// ============================
	app.initBot()

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
		// Callback to test reactions via GUI (F7) - emulates buff/debuff detection
		app.configWindow.TestReaction = func(id uint32) {
			// Uses TriggerForTest with key executor that sends directly to game window
			keyExecutor := func(keys [][]uint16) error {
				return input.SendKeySequenceToWindow(app.gameHwnd, keys)
			}
			if err := app.reactionManager.TriggerForTest(id, keyExecutor); err != nil {
				fmt.Printf("[REACTION-TEST] Error: %v\n", err)
			}
		}
	}

	buffWindow, err := gui.NewBuffWindow(app.buffInjector, app.presetManager)
	if err == nil {
		app.buffWindow = buffWindow
	}

	skillConfigWindow, err := gui.NewSkillConfigWindow(app.skillReactionManager, "skill_reactions.json")
	if err == nil {
		app.skillConfigWindow = skillConfigWindow
		// Callback to test reactions via GUI - sends directly to game window
		app.skillConfigWindow.ExecuteOnCast = func(onCast string) {
			keys, err := input.ParseKeySequence(onCast)
			if err != nil {
				fmt.Printf("[SKILL-TEST] Error parsing '%s': %v\n", onCast, err)
				return
			}
			if err := input.SendKeySequenceToWindow(app.gameHwnd, keys); err != nil {
				fmt.Printf("[SKILL-TEST] Error sending '%s': %v\n", onCast, err)
			} else {
				fmt.Printf("[SKILL-TEST] Sent to game window: %s\n", onCast)
			}
		}
	}

	autospamWindow, err := gui.NewAutoSpamWindow(app.inputManager)
	if err == nil {
		app.autospamWindow = autospamWindow
	}

	// Bot config window
	botConfigWindow, err := gui.NewBotConfigWindow(app.botInstance, app.botConfig, "bot_config.json")
	if err == nil {
		app.botConfigWindow = botConfigWindow
		app.botConfigWindow.OnToggleBot = func() {
			app.toggleBot()
		}
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

// ============================================================================
// Bot
// ============================================================================

func (app *App) initBot() {
	if app.espManager == nil {
		fmt.Println("[BOT] ESP não disponível, bot desabilitado")
		return
	}

	// Carregar config do arquivo (mob names, range, presets)
	fc, err := bot.LoadFileConfig("bot_config.json")
	if err != nil {
		fmt.Printf("[BOT] Config não encontrada, criando padrão\n")
		bot.SaveDefaultConfig("bot_config.json")
		fc2 := bot.DefaultFileConfig()
		fc = &fc2
	}
	app.botConfig = fc

	// Adapter: converte esp.EntityInfo -> bot.EntityInfo
	// Também sincroniza range com o overlay ESP
	adapter := &bot.ESPAdapter{
		GetEntitiesFn: func() []bot.EntityInfo {
			entities := app.espManager.GetAllEntitiesCached()
			result := make([]bot.EntityInfo, 0, len(entities))
			for _, e := range entities {
				result = append(result, bot.EntityInfo{
					Address:  e.Address,
					EntityID: e.EntityID,
					Name:     e.Name,
					PosX:     e.PosX,
					PosY:     e.PosY,
					PosZ:     e.PosZ,
					HP:       e.HP,
					MaxHP:    e.MaxHP,
					Distance: e.Distance,
					IsPlayer: e.IsPlayer,
					IsNPC:    e.IsNPC,
					IsMate:   e.IsMate,
				})
			}
			return result
		},
		// Sincroniza range do bot com range do ESP overlay
		GetRangeFn: func() float32 {
			return app.espManager.GetAllEntitiesMaxRange()
		},
	}

	cfg := bot.DefaultConfig()
	cfg.MobNames = fc.MobNames
	cfg.MaxRange = fc.MaxRange
	cfg.PartialMatch = fc.PartialMatch

	if fc.ScanIntervalMs > 0 {
		cfg.ScanInterval = time.Duration(fc.ScanIntervalMs) * time.Millisecond
	}
	if fc.TargetDelayMs > 0 {
		cfg.TargetDelay = time.Duration(fc.TargetDelayMs) * time.Millisecond
	}

	cfg.OnTargetDead = func(t bot.EntityInfo) {
		fmt.Printf("[BOT] Killed: %s → scanning next...\n", t.Name)
	}

	cfg.OnTargetAcquired = func(t bot.EntityInfo) {
		fmt.Printf("[BOT] Attacking: %s (HP:%d Dist:%.0fm)\n", t.Name, t.HP, t.Distance)
	}

	cfg.OnCombatTick = func(t bot.EntityInfo) {
		// Auto-attack handled by bot internally
	}

	// Configurar keys de ataque/loot
	cfg.AttackKey = fc.AttackKey
	cfg.LootKey = fc.LootKey
	cfg.AutoAttack = fc.AutoAttack
	cfg.AutoLoot = fc.AutoLoot
	if fc.AttackDelay > 0 {
		cfg.AttackDelay = time.Duration(fc.AttackDelay) * time.Millisecond
	}
	if fc.LootDelay > 0 {
		cfg.LootDelay = time.Duration(fc.LootDelay) * time.Millisecond
	}

	// Key sender function - usa PostMessage para enviar direto pro jogo (como o keyspam)
	cfg.SendKey = func(keyStr string) {
		if app.gameHwnd == 0 {
			// Fallback para SendInput se não tiver janela do jogo
			keys, err := input.ParseKeyString(keyStr)
			if err != nil {
				fmt.Printf("[BOT] Invalid key: %s - %v\n", keyStr, err)
				return
			}
			if err := input.SendKeyCombo(keys); err != nil {
				fmt.Printf("[BOT] SendKey failed: %v\n", err)
			}
			return
		}
		// Envia direto pro jogo via PostMessage (mesmo método do keyspam)
		if err := input.SendKeyStringToWindow(app.gameHwnd, keyStr); err != nil {
			fmt.Printf("[BOT] SendKey failed: %v\n", err)
		}
	}

	// Potion settings
	cfg.HPPotionKey = fc.HPPotionKey
	cfg.HPPotionThreshold = fc.HPPotionThreshold
	cfg.HPPotionEnabled = fc.HPPotionEnabled
	cfg.MPPotionKey = fc.MPPotionKey
	cfg.MPPotionThreshold = fc.MPPotionThreshold
	cfg.MPPotionEnabled = fc.MPPotionEnabled
	if fc.PotionCooldownMs > 0 {
		cfg.PotionCooldown = time.Duration(fc.PotionCooldownMs) * time.Millisecond
	}

	// Player HP/MP providers - closure over app to read player stats
	cfg.GetPlayerHP = func() (uint32, uint32) {
		player := entity.GetLocalPlayer(app.handle, app.x2game)
		return player.HP, player.MaxHP
	}
	cfg.GetPlayerMP = func() (uint32, uint32) {
		player := entity.GetLocalPlayer(app.handle, app.x2game)
		return player.MP, player.MaxMP
	}

	app.botInstance = bot.New(app.handle, app.x2game, adapter, cfg)

	// Log potion config if enabled
	potionInfo := ""
	if fc.HPPotionEnabled {
		potionInfo += fmt.Sprintf(" | HP Pot: %s(<%.0f%%)", fc.HPPotionKey, fc.HPPotionThreshold)
	}
	if fc.MPPotionEnabled {
		potionInfo += fmt.Sprintf(" | MP Pot: %s(<%.0f%%)", fc.MPPotionKey, fc.MPPotionThreshold)
	}
	fmt.Printf("[BOT] Initialized | Mobs: %v | Range: %.0fm | Attack: %s | Loot: %s%s\n",
		fc.MobNames, fc.MaxRange, fc.AttackKey, fc.LootKey, potionInfo)
}

func (app *App) toggleBot() {
	if app.botInstance == nil {
		return
	}

	if app.botInstance.IsRunning() {
		app.botInstance.Stop()
	} else {
		// Garante que AllEntities ESP tá rodando
		if app.espManager != nil && !app.espManager.IsAllEntitiesEnabled() {
			app.espManager.ToggleAllEntities()
			fmt.Println("[BOT] All Entities ESP ativado automaticamente")
		}
		app.botInstance.Start()
	}
}

func (app *App) botLoadPreset(presetName string) {
	if app.botInstance == nil || app.botConfig == nil {
		return
	}

	names, ok := app.botConfig.Presets[presetName]
	if !ok {
		fmt.Printf("[BOT] Preset '%s' não encontrado\n", presetName)
		return
	}

	app.botInstance.SetMobNames(names)
	fmt.Printf("[BOT] Preset '%s': %v\n", presetName, names)
}

func (app *App) botReloadConfig() {
	fc, err := bot.LoadFileConfig("bot_config.json")
	if err != nil {
		fmt.Printf("[BOT] Erro ao recarregar config: %v\n", err)
		return
	}
	app.botConfig = fc

	if app.botInstance != nil {
		app.botInstance.ApplyFileConfig(fc)
		fmt.Println("[BOT] Config recarregada")
	}
}

// ============================================================================
// Background tasks
// ============================================================================

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

// ============================================================================
// Hotkeys
// ============================================================================

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
		0x79: func() { // F10 - Bot Config Window
			if app.botConfigWindow != nil {
				app.botConfigWindow.Toggle()
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
		0xBD: func() { // MINUS - Toggle All Entities ESP
			if app.espManager != nil {
				enabled := app.espManager.ToggleAllEntities()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] All Entities: %s\n", status)
			}
		},
		0xBB: func() { // EQUALS/PLUS - Toggle Show Players
			if app.espManager != nil {
				enabled := app.espManager.ToggleShowPlayers()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] Show Players: %s\n", status)
			}
		},
		0xDB: func() { // OPEN BRACKET [ - Toggle Show NPCs
			if app.espManager != nil {
				enabled := app.espManager.ToggleShowNPCs()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] Show NPCs: %s\n", status)
			}
		},
		0x24: func() { // HOME - Cycle ESP style
			if app.espManager != nil && app.espManager.IsEnabled() {
				style := app.espManager.CycleStyle()
				fmt.Printf("[ESP] Style: %s\n", app.espManager.GetStyleName())
				_ = style
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
		0x13: func() { // PAUSE - Trigger scan
			if app.targetScanner != nil && app.targetScanner.IsScanning() {
				app.targetScanner.ScanForChanges("TARGET_CHANGE")
			}
		},
		0x2E: func() { // DELETE - Toggle Bot ON/OFF
			app.toggleBot()
		},

		// ==================== BOT HOTKEYS ====================
		0x60: func() { // NUMPAD0 - Toggle Bot ON/OFF
			app.toggleBot()
		},
		0x61: func() { // NUMPAD1 - Preset 1
			app.botLoadPreset("preset1")
		},
		0x62: func() { // NUMPAD2 - Preset 2
			app.botLoadPreset("preset2")
		},
		0x63: func() { // NUMPAD3 - Preset 3
			app.botLoadPreset("preset3")
		},
		0x64: func() { // NUMPAD4 - Reload bot_config.json
			app.botReloadConfig()
		},
		0x6B: func() { // NUMPAD+ - Increase range +5m
			if app.botInstance != nil {
				cfg := app.botInstance.GetConfig()
				app.botInstance.SetMaxRange(cfg.MaxRange + 5)
			}
		},
		0x6D: func() { // NUMPAD- - Decrease range -5m
			if app.botInstance != nil {
				cfg := app.botInstance.GetConfig()
				if cfg.MaxRange > 5 {
					app.botInstance.SetMaxRange(cfg.MaxRange - 5)
				}
			}
		},
		0x65: func() { // NUMPAD5 - Toggle partial match
			if app.botInstance != nil {
				cfg := app.botInstance.GetConfig()
				app.botInstance.SetPartialMatch(!cfg.PartialMatch)
			}
		},
		0x69: func() { // NUMPAD9 - Print bot stats
			if app.botInstance != nil {
				app.botInstance.PrintStats()
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

// ============================================================================
// Display
// ============================================================================

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

	allESPStatus := "OFF"
	if app.espManager != nil && app.espManager.IsAllEntitiesEnabled() {
		allESPStatus = "ON"
	}

	patchStatus := ""
	if app.patchManager != nil {
		patchStatus = app.patchManager.GetStatus()
	}

	lines = append(lines, fmt.Sprintf("[F1] Loot:%s  [F2] Doodad:%s  [F3] Spam  [F4] AutoSpam:%s", lootStatus, doodadStatus, spamStatus))
	lines = append(lines, fmt.Sprintf("[F5] Reload  [F6] Reactions:%s  [F10] AFK:%s  %s", reactionStatus, afkStatus, patchStatus))
	lines = append(lines, fmt.Sprintf("[F12] ESP:%s %s  [-] AllESP:%s", espStatus, espStyle, allESPStatus))
	lines = append(lines, "[F7] Config  [F8] Buffs  [F9] Quick  [F10] BotCfg  [DEL] Bot  [END] Hide")
	lines = append(lines, fmt.Sprintf("Quick:%s (%s)", quickStatus, quickPreset))

	// ==================== BOT STATUS LINE ====================
	lines = append(lines, "────────────────────────────────────────────────────────")
	lines = append(lines, app.getBotDisplayLine())

	return lines
}

func (app *App) getBotDisplayLine() string {
	if app.botInstance == nil {
		return "[BOT] N/A (sem ESP)"
	}

	if !app.botInstance.IsRunning() {
		// Mostra mob list configurada mesmo quando OFF
		cfg := app.botInstance.GetConfig()
		mobList := "none"
		if len(cfg.MobNames) > 0 {
			mobList = ""
			for i, n := range cfg.MobNames {
				if i > 0 {
					mobList += ", "
				}
				if len(mobList)+len(n) > 50 {
					mobList += "..."
					break
				}
				mobList += n
			}
		}
		return fmt.Sprintf("[DEL] Bot:OFF | Mobs:[%s] | Range:%.0fm", mobList, cfg.MaxRange)
	}

	// Bot rodando - mostra estado + target atual
	state := app.botInstance.GetState()
	stats := app.botInstance.GetStats()
	cfg := app.botInstance.GetConfig()

	line := fmt.Sprintf("[DEL] Bot:%s | Kills:%d | R:%.0fm",
		state, stats.MobsKilled, cfg.MaxRange)

	if target := app.botInstance.GetCurrentTarget(); target != nil {
		line += fmt.Sprintf(" | %s HP:%d D:%.0fm", target.Name, target.HP, target.Distance)
	}

	return line
}

func (app *App) Update() {
}

// ============================================================================
// Diagnostics
// ============================================================================

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

	// Patch status
	if app.patchManager != nil {
		fmt.Printf("\n[PATCHES]\n")
		fmt.Printf("  %s\n", app.patchManager.GetStatus())
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

	// Bot diagnostics
	if app.botInstance != nil {
		fmt.Printf("\n[BOT]\n")
		fmt.Printf("  Running: %v\n", app.botInstance.IsRunning())
		fmt.Printf("  State: %s\n", app.botInstance.GetState())
		cfg := app.botInstance.GetConfig()
		fmt.Printf("  MobNames: %v\n", cfg.MobNames)
		fmt.Printf("  MaxRange: %.0fm\n", cfg.MaxRange)
		fmt.Printf("  PartialMatch: %v\n", cfg.PartialMatch)
		stats := app.botInstance.GetStats()
		fmt.Printf("  Kills: %d | Targets: %d\n", stats.MobsKilled, stats.TargetsSet)
		if target := app.botInstance.GetCurrentTarget(); target != nil {
			fmt.Printf("  Current: %s (ID:%d HP:%d Dist:%.0fm)\n",
				target.Name, target.EntityID, target.HP, target.Distance)
		}
		if app.botConfig != nil && len(app.botConfig.Presets) > 0 {
			fmt.Printf("  Presets:\n")
			for name, mobs := range app.botConfig.Presets {
				fmt.Printf("    %s: %v\n", name, mobs)
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

	callback := func(hwnd uintptr, lParam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}

		var windowPID uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))

		if windowPID == targetPID {
			foundHwnd = hwnd
			return 0
		}

		return 1
	}

	procEnumWindows.Call(
		windows.NewCallback(callback),
		0,
	)

	return foundHwnd
}

func (app *App) Close() {
	close(app.stopChan)

	// Stop bot
	if app.botInstance != nil && app.botInstance.IsRunning() {
		app.botInstance.Stop()
	}

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
	if app.patchManager != nil {
		app.patchManager.RestoreAll()
	}
	if app.handle != 0 {
		windows.CloseHandle(app.handle)
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║      ARCHEFRIEND OVERLAY v3.7         ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  F1: Loot | F2: Doodad | F3: Spam    ║")
	fmt.Println("║  F4: Auto | F5: Reload | F6: React   ║")
	fmt.Println("║  F7: Config | F8: Buffs | F9: Quick  ║")
	fmt.Println("║  F10: BotCfg | F11: Diag | F12: ESP  ║")
	fmt.Println("║  HOME: Style | END: Hide             ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  DEL: Bot ON/OFF                     ║")
	fmt.Println("║  NUM1-3: Mob Presets | NUM4: Reload  ║")
	fmt.Println("║  NUM+/-: Range | NUM5: Match Mode    ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	// Check admin privileges
	if !process.IsAdmin() {
		fmt.Println("╔═══════════════════════════════════════╗")
		fmt.Println("║  ⚠️  AVISO: NÃO ESTÁ COMO ADMIN!      ║")
		fmt.Println("║  Bot/SetTarget pode falhar.           ║")
		fmt.Println("║  Execute como Administrador!          ║")
		fmt.Println("╚═══════════════════════════════════════╝")
		fmt.Println()
	} else {
		fmt.Println("[OK] Rodando como Administrador")
	}

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