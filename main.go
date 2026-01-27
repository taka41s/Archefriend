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
	handle    windows.Handle
	x2game    uintptr
	connected bool
	mu        sync.RWMutex

	lootBypass      *loot.Bypass
	inputManager    *input.Manager
	reactionManager *reaction.Manager
	buffMonitor     *monitor.BuffMonitor
	debuffMonitor   *monitor.DebuffMonitor
	targetMonitor   *target.Monitor
	buffInjector    *buff.Injector
	presetManager   *buff.PresetManager
	keybinds        *config.KeybindsConfig

	window       *gui.OverlayWindow
	configWindow *gui.ConfigWindow
	buffWindow   *gui.BuffWindow
	visible      bool
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
	app.connected = true

	app.lootBypass = loot.NewBypass(handle, x2game)
	app.inputManager = input.NewManager()
	app.reactionManager = reaction.NewManager()
	app.buffMonitor = monitor.NewBuffMonitor(handle, x2game)
	app.debuffMonitor = monitor.NewDebuffMonitor(handle, x2game)
	app.targetMonitor = target.NewMonitor(handle, x2game)
	app.buffInjector = buff.NewInjector(handle)
	app.presetManager = buff.NewPresetManager(app.buffInjector)
	app.buffInjector.StartFreezeLoop()

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

	app.buffMonitor.OnBuffGained = func(buff monitor.BuffInfo) {}
	app.buffMonitor.OnBuffLost = func(buffID uint32) {}
	app.debuffMonitor.OnDebuffGained = func(debuff monitor.DebuffInfo) {}
	app.debuffMonitor.OnDebuffLost = func(debuffTypeID uint32) {}

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
		0x7A: func() { // F11 - Diagnóstico
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
}

func (app *App) printDiagnostics() {
	fmt.Println("\n╔════════════════════════════════════════╗")
	fmt.Println("║         SYSTEM DIAGNOSTICS             ║")
	fmt.Println("╚════════════════════════════════════════╝")

	app.mu.RLock()
	connected := app.connected
	app.mu.RUnlock()

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

	fmt.Println("\n════════════════════════════════════════")
	fmt.Println("Press F11 again to refresh.")
	fmt.Println("════════════════════════════════════════\n")
}

func (app *App) Close() {
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
