// +build windows

package main

import (
	"archefriend/afk"
	"archefriend/bot"
	"archefriend/buff"
	"archefriend/config"
	"archefriend/entity"
	"archefriend/esp"
	"archefriend/fishing"
	"archefriend/gui"
	"archefriend/input"
	"archefriend/loot"
	"archefriend/monitor"

	"archefriend/patch"
	"archefriend/process"
	"archefriend/reaction"
	"archefriend/skill"
	"archefriend/target"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AppSettings holds configurable paths loaded from settings.json
type AppSettings struct {
	LuaExportPath    string `json:"lua_export_path"`
	SkillCastOffset  string `json:"skill_cast_offset"`  // Hex offset for skill cast hook (e.g. "0x1A2B3C")
	SkillTryOffset   string `json:"skill_try_offset"`   // Hex offset for skill try hook (e.g. "0x1A2B3C")
}

func loadSettings() AppSettings {
	var s AppSettings
	data, err := os.ReadFile("settings.json")
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

const (
	OVERLAY_WIDTH  = 700
	OVERLAY_HEIGHT = 150
)

// Feature flags - override via: go build -ldflags "-X main.featureX=false"
var (
	featureLoot      = "true"
	featurePatches   = "true"
	featureReactions = "true"
	featureBuffs     = "true"
	featureESP       = "true"
	featureBot       = "true"

	featureKeyspam   = "true"
)

func feat(flag string) bool {
	return flag == "true" || flag == "1"
}

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
	keybinds             *config.KeybindsConfig
	targetScanner        *esp.TargetScanner

	// Bot
	botInstance  *bot.Bot
	botConfig    *bot.FileConfig

	// Fishing
	fishingBot    *fishing.Bot

	// Skill Hooks
	skillMonitor         *skill.SkillMonitor
	skillReactionManager *skill.ReactionManager
	skillConfigWindow    *gui.SkillConfigWindow

	window            *gui.OverlayWindow
	configWindow      *gui.ConfigWindow
	buffWindow        *gui.BuffWindow
	autospamWindow    *gui.AutoSpamWindow
	botConfigWindow   *gui.BotConfigWindow
	fishingWindow     *gui.FishingWindow
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

	settings := loadSettings()

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
	if feat(featurePatches) {
		app.patchManager = patch.NewManager(handle, x2game)
		app.patchManager.ApplyAll()
	}

	// Encontrar janela do ArcheAge usando o PID
	app.gameHwnd = findWindowByPID(pid)

	// Loot/Doodad bypass
	if feat(featureLoot) {
		app.lootBypass = loot.NewBypass(handle, x2game)
	}

	// Keyspam / Input manager
	if feat(featureKeyspam) {
		app.inputManager = input.NewManager()
		app.inputManager.SetGameWindow(app.gameHwnd)
		app.inputManager.SetKeys([][]uint16{
			{input.VK_V},
			{input.VK_LSHIFT, input.VK_F},
		})
	}

	// Reactions (buff/debuff monitor + reaction manager + AFK)
	if feat(featureReactions) {
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
	}

	// Buff injector
	if feat(featureBuffs) {
		app.buffInjector = buff.NewInjector(handle)
		app.presetManager = buff.NewPresetManager(app.buffInjector)
		app.buffInjector.StartFreezeLoop()
	}

	// ESP
	if feat(featureESP) {
		espMgr, err := esp.NewManager(uintptr(handle), pid, x2game)
		if err != nil {
			fmt.Printf("[WARN] Falha ao criar ESP: %v\n", err)
		} else {
			app.espManager = espMgr
			app.targetScanner = espMgr.NewTargetScanner()

			if err := espMgr.LoadAimbotConfig("aimbot_config.json"); err != nil {
				fmt.Printf("[AIMBOT] Config não encontrada, usando padrão (Mouse4, Mouse5)\n")
				espMgr.SetAimbotKeys([]int{0x05, 0x06})
			}

			espMgr.Enable()
			espMgr.ToggleAllEntities()
			fmt.Println("[ESP] Target ESP e All Entities ESP iniciados automaticamente")

			if settings.LuaExportPath != "" {
				espMgr.SetLuaExportPath(settings.LuaExportPath)
			}
		}
	}


	// Bot (requires ESP)
	if feat(featureBot) {
		app.initBot()
	}

	// Buff presets (requires featureBuffs)
	if feat(featureBuffs) && app.presetManager != nil {
		if err := app.presetManager.LoadFromJSON("buff_presets.json"); err != nil {
			app.presetManager.CreateDefaultPresets()
			app.presetManager.SaveToJSON("buff_presets.json")
		}
	}

	// Reaction configs + callbacks (requires featureReactions)
	if feat(featureReactions) && app.reactionManager != nil {
		app.reactionManager.LoadFromJSON("reactions.json")
		app.buffMonitor.SetReactionHandler(app.reactionManager)
		app.debuffMonitor.SetReactionHandler(app.reactionManager)

		configWindow, err := gui.NewConfigWindow(app.reactionManager)
		if err == nil {
			app.configWindow = configWindow
			app.configWindow.TestReaction = func(id uint32) {
				keyExecutor := func(keys [][]uint16) error {
					return input.SendKeySequenceToWindow(app.gameHwnd, keys)
				}
				if err := app.reactionManager.TriggerForTest(id, keyExecutor); err != nil {
					fmt.Printf("[REACTION-TEST] Error: %v\n", err)
				}
			}
		}

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

		// Wire up entity resolution: target ID -> entity address via ESP entity list
		app.targetMonitor.ResolveEntityAddr = func(entityID uint32) uint32 {
			if app.espManager != nil {
				return app.espManager.FindEntityByID(entityID)
			}
			return 0
		}

		// Target buff/debuff callbacks (via target.Monitor) - uses target-specific reaction methods
		app.targetMonitor.OnBuffGained = func(buff target.TargetBuff) {
			fmt.Printf("[TARGET-BUFF+] ID:%d Duration:%dms Stack:%d\n", buff.ID, buff.Duration, buff.Stack)
			app.reactionManager.OnTargetBuffGained(buff.ID)
			// Forward to fishing bot
			if app.fishingBot != nil {
				app.fishingBot.OnTargetBuffGained(buff.ID)
			}
		}
		app.targetMonitor.OnBuffLost = func(buffID uint32) {
			fmt.Printf("[TARGET-BUFF-] ID:%d\n", buffID)
			app.reactionManager.OnTargetBuffLost(buffID)
		}
		app.targetMonitor.OnDebuffGained = func(debuff target.TargetBuff) {
			fmt.Printf("[TARGET-DEBUFF+] TypeID:%d ID:%d Duration:%.1fs\n", debuff.TypeID, debuff.ID, float64(debuff.Duration)/1000)
			app.reactionManager.OnTargetDebuffGained(debuff.TypeID)
		}
		app.targetMonitor.OnDebuffLost = func(debuffID uint32) {
			fmt.Printf("[TARGET-DEBUFF-] ID:%d\n", debuffID)
			app.reactionManager.OnTargetDebuffLost(debuffID)
		}
	}

	// Buff window (requires featureBuffs)
	if feat(featureBuffs) && app.buffInjector != nil {
		buffWindow, err := gui.NewBuffWindow(app.buffInjector, app.presetManager)
		if err == nil {
			app.buffWindow = buffWindow
		}
	}


	// Autospam window (requires featureKeyspam)
	if feat(featureKeyspam) && app.inputManager != nil {
		autospamWindow, err := gui.NewAutoSpamWindow(app.inputManager)
		if err == nil {
			app.autospamWindow = autospamWindow
		}
	}

	// Bot config window (requires featureBot)
	if feat(featureBot) && app.botInstance != nil {
		botConfigWindow, err := gui.NewBotConfigWindow(app.botInstance, app.botConfig, "bot_config.json")
		if err == nil {
			app.botConfigWindow = botConfigWindow
			app.botConfigWindow.OnToggleBot = func() {
				app.toggleBot()
			}
		}
	}

	// Skill Hook System (requires process handle + offsets in settings.json)
	if app.connected && settings.SkillCastOffset != "" {
		app.initSkillSystem(settings)
	}

	// Fishing Bot (requires featureReactions for target buff monitoring)
	if feat(featureReactions) && app.targetMonitor != nil {
		app.fishingBot = fishing.New(func(keyStr string) {
			if app.gameHwnd == 0 {
				fishing.ParseAndSendKey(keyStr)
				return
			}
			if err := input.SendKeyStringToWindow(app.gameHwnd, keyStr); err != nil {
				fmt.Printf("[FISHING] SendKey failed: %v\n", err)
			}
		})
		if err := app.fishingBot.LoadConfig("fishing_config.json"); err != nil {
			fmt.Printf("[FISHING] Config error: %v\n", err)
		}

		fishingWindow, err := gui.NewFishingWindow(app.fishingBot, "fishing_config.json")
		if err == nil {
			app.fishingWindow = fishingWindow
		}
		fmt.Println("[FISHING] Sport Fishing Bot initialized")
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
// Skill System
// ============================================================================

func parseHexOffset(s string) (uintptr, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, err
	}
	return uintptr(val), nil
}

func (app *App) initSkillSystem(settings AppSettings) {
	castOffset, err := parseHexOffset(settings.SkillCastOffset)
	if err != nil {
		fmt.Printf("[SKILL] Invalid skill_cast_offset: %v\n", err)
		return
	}

	app.skillMonitor = skill.NewSkillMonitor(app.handle, app.x2game, castOffset)

	// Install main hook (skill cast detection)
	if err := app.skillMonitor.InstallHook(); err != nil {
		fmt.Printf("[SKILL] Falha ao instalar hook: %v\n", err)
		app.skillMonitor = nil
		return
	}

	// Install try hook if offset configured
	if settings.SkillTryOffset != "" {
		tryOffset, err := parseHexOffset(settings.SkillTryOffset)
		if err != nil {
			fmt.Printf("[SKILL] Invalid skill_try_offset: %v\n", err)
		} else {
			if err := app.skillMonitor.InstallTryHook(tryOffset); err != nil {
				fmt.Printf("[SKILL] Falha ao instalar try hook: %v\n", err)
			}
		}
	}

	// Skill Reaction Manager
	app.skillReactionManager = skill.NewReactionManager()
	if err := app.skillReactionManager.LoadFromJSON("skill_reactions.json"); err != nil {
		fmt.Printf("[SKILL] skill_reactions.json não encontrado, criando padrão\n")
		skill.SaveDefaultReactions("skill_reactions.json")
		app.skillReactionManager.LoadFromJSON("skill_reactions.json")
	}

	// Wire callbacks
	app.skillReactionManager.ExecuteKeys = func(keys [][]uint16) error {
		return input.SendKeySequenceToWindow(app.gameHwnd, keys)
	}
	app.skillReactionManager.SpamKeys = func(keys [][]uint16, repeatCount int) error {
		for i := 0; i < repeatCount; i++ {
			if err := input.SendKeySequenceToWindow(app.gameHwnd, keys); err != nil {
				return err
			}
			time.Sleep(50 * time.Millisecond)
		}
		return nil
	}

	// Wire aimbot if ESP available
	if app.espManager != nil {
		app.skillReactionManager.AimAtTarget = func() bool {
			return app.espManager.AimAtTarget()
		}
	}

	// Wire buff checker
	if app.buffMonitor != nil {
		app.skillReactionManager.HasBuff = func(buffID uint32) bool {
			for _, b := range app.buffMonitor.Buffs {
				if b.ID == buffID {
					return true
				}
			}
			return false
		}
	}

	// Parse keys for existing reactions
	app.skillReactionManager.SetKeyParser(func(s string) ([][]uint16, error) {
		keys, err := input.ParseKeyString(s)
		if err != nil {
			return nil, err
		}
		return [][]uint16{keys}, nil
	})

	// Connect SkillMonitor callbacks to ReactionManager
	app.skillMonitor.OnSkillCast = func(skillID uint32) {
		app.skillReactionManager.OnSkillCast(skillID)
	}
	app.skillMonitor.OnSkillTry = func(skillID uint32) {
		app.skillReactionManager.OnSkillTry(skillID)
	}

	// Skill Config Window (GUI)
	skillConfigWindow, err := gui.NewSkillConfigWindow(app.skillReactionManager, "skill_reactions.json")
	if err == nil {
		app.skillConfigWindow = skillConfigWindow
		app.skillConfigWindow.ExecuteOnCast = func(onCast string) {
			keys, err := input.ParseKeyString(onCast)
			if err != nil {
				fmt.Printf("[SKILL] Invalid key: %s - %v\n", onCast, err)
				return
			}
			input.SendKeySequenceToWindow(app.gameHwnd, [][]uint16{keys})
		}
	}

	fmt.Printf("[SKILL] Sistema de skills inicializado (cast:0x%X", castOffset)
	if settings.SkillTryOffset != "" {
		fmt.Printf(" try:0x%s", settings.SkillTryOffset)
	}
	fmt.Println(")")
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
	ticker := time.NewTicker(1 * time.Millisecond) // 50ms = faster buff/debuff detection
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

				if app.buffMonitor != nil {
					app.buffMonitor.Update(playerAddr)
				}
				if app.debuffMonitor != nil {
					app.debuffMonitor.Update(playerAddr)
				}

				// Update target monitor (reads target buffs/debuffs)
				if app.targetMonitor != nil {
					player := entity.GetLocalPlayer(app.handle, app.x2game)
					app.targetMonitor.Update(player.PosX, player.PosY, player.PosZ)
				}

				// Update skill monitor (checks code cave flags)
				if app.skillMonitor != nil {
					app.skillMonitor.Update()
				}

				if app.buffInjector != nil && app.buffMonitor != nil {
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
		0x7A: func() { // F11 - Fishing Bot Window
			if app.fishingWindow != nil {
				app.fishingWindow.Toggle()
			}
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
		0xDD: func() { // CLOSE BRACKET ] - Toggle House ESP
			if app.espManager != nil {
				enabled := app.espManager.ToggleHouseESP()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] Houses: %s\n", status)
			}
		},
		0x21: func() { // PAGE UP - Toggle Recheck panel
			if app.espManager != nil {
				enabled := app.espManager.ToggleRecheckPanel()
				status := "OFF"
				if enabled {
					status = "ON"
				}
				fmt.Printf("[ESP] Recheck Panel: %s\n", status)
			}
		},
		0xDE: func() { // ' (single quote) - Next house filter type
			if app.espManager != nil {
				app.espManager.NextHouseFilterType()
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
		0x22: func() { // PAGE DOWN - Skill Config Window
			if app.skillConfigWindow != nil {
				app.skillConfigWindow.Toggle()
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
		0x68: func() { // NUMPAD8 - Diagnostics
			app.printDiagnostics()
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

	playerPosStr := "N/A"
	if app.espManager != nil {
		px, py, pz, ok := app.espManager.GetPlayerPosition()
		if ok {
			playerPosStr = fmt.Sprintf("X:%.1f Y:%.1f Z:%.1f", px, py, pz)
		}
	}

	lines = append(lines, fmt.Sprintf("ARCHEFRIEND [%s] | Reactions: %d", status, activeReactions))
	lines = append(lines, fmt.Sprintf("Player: %s", playerPosStr))
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

	// ==================== FISHING STATUS LINE ====================
	if app.fishingBot != nil {
		fishStatus := "OFF"
		if app.fishingBot.IsRunning() {
			stats := app.fishingBot.GetStats()
			fishStatus = fmt.Sprintf("ON | R:%d", stats.ReactionsTriggered)
		}
		lines = append(lines, fmt.Sprintf("[F11] Fish:%s", fishStatus))
	}

	// ==================== SKILL STATUS LINE ====================
	if app.skillMonitor != nil {
		skillStatus := "OFF"
		if app.skillMonitor.Hooked && app.skillMonitor.Enabled {
			skillStatus = fmt.Sprintf("ON | Casts:%d", app.skillMonitor.CastCount)
		}
		lines = append(lines, fmt.Sprintf("[PGDN] Skills:%s", skillStatus))
	}

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
		app.targetMonitor.DebugTargetBuffs()
	}

	// Debug faction offset scan
	if app.espManager != nil {
		fmt.Println("\n[SCANNING ENTITY FACTION OFFSETS...]")
		app.espManager.DebugEntityFaction()
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

	if app.skillMonitor != nil {
		fmt.Printf("\n[SKILL MONITOR]\n")
		fmt.Printf("  Hooked: %v\n", app.skillMonitor.Hooked)
		fmt.Printf("  Enabled: %v\n", app.skillMonitor.Enabled)
		fmt.Printf("  Last Skill: %d\n", app.skillMonitor.LastSkillID)
		fmt.Printf("  Cast Count: %d\n", app.skillMonitor.CastCount)
		if app.skillReactionManager != nil {
			fmt.Printf("  Reactions Enabled: %v\n", app.skillReactionManager.IsEnabled())
			fmt.Printf("  Reactions Count: %d\n", len(app.skillReactionManager.GetAllReactions()))
		}
	}

	if app.espManager != nil {
		fmt.Printf("\n[ESP TARGET DEBUG]\n")
		app.espManager.DebugTargetInfo()
		fmt.Printf("\n[AIMBOT DEBUG]\n")
		app.espManager.AimAtTargetDebug(true)
	}

	fmt.Println("\n════════════════════════════════════════")
	fmt.Println("Press NUMPAD8 again to refresh.")
	fmt.Println("════════════════════════════════════════")
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

	// Stop fishing bot
	if app.fishingBot != nil && app.fishingBot.IsRunning() {
		app.fishingBot.Stop()
	}

	// Remove skill hooks (restore original bytes BEFORE closing handle)
	if app.skillMonitor != nil {
		fmt.Println("[SKILL] Removendo hooks...")
		app.skillMonitor.Close()
		fmt.Println("[SKILL] Hooks removidos e bytes restaurados")
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
	fmt.Println("║  F10: BotCfg | F11: Fish | F12: ESP  ║")
	fmt.Println("║  HOME: Style | END: Hide             ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  DEL: Bot ON/OFF | PGDN: Skills      ║")
	fmt.Println("║  NUM1-3: Mob Presets | NUM4: Reload  ║")
	fmt.Println("║  NUM+/-: Range | NUM5: Match Mode    ║")
	fmt.Println("╠═══════════════════════════════════════╣")
	fmt.Println("║  Ctrl+R: Record | Ctrl+T: Run Route  ║")
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

	// Print active features
	fmt.Println()
	fmt.Print("[FEATURES] ")
	features := map[string]string{
		"Loot": featureLoot, "Patches": featurePatches, "Reactions": featureReactions,
		"Buffs": featureBuffs, "ESP": featureESP, "Bot": featureBot,
		"Keyspam": featureKeyspam,
	}
	for name, flag := range features {
		if feat(flag) {
			fmt.Printf("%s:ON ", name)
		}
	}
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
