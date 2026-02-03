package gui

import (
	"archefriend/bot"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Bot Control IDs
const (
	IDC_BOT_EDIT_MOBS       = 2001
	IDC_BOT_EDIT_RANGE      = 2002
	IDC_BOT_CHECK_PARTIAL   = 2003
	IDC_BOT_BUTTON_SAVE     = 2004
	IDC_BOT_BUTTON_START    = 2005
	IDC_BOT_LIST_PRESETS    = 2006
	IDC_BOT_BUTTON_LOAD     = 2007
	IDC_BOT_EDIT_SCAN       = 2008
	IDC_BOT_EDIT_DELAY      = 2009
	IDC_BOT_EDIT_ATTACK_KEY = 2010
	IDC_BOT_EDIT_LOOT_KEY   = 2011
	IDC_BOT_CHECK_AUTOATTACK= 2012
	IDC_BOT_CHECK_AUTOLOOT  = 2013
)

type BotConfigWindow struct {
	hwnd windows.Handle

	// Controls
	editMobs       windows.Handle
	editRange      windows.Handle
	editScan       windows.Handle
	editDelay      windows.Handle
	editAttackKey  windows.Handle
	editLootKey    windows.Handle
	checkPartial   windows.Handle
	checkAutoAttack windows.Handle
	checkAutoLoot  windows.Handle
	listPresets    windows.Handle
	btnSave        windows.Handle
	btnStart       windows.Handle
	btnLoad        windows.Handle

	visible bool
	ready   chan bool

	// Bot references
	botInstance *bot.Bot
	botConfig   *bot.FileConfig
	configFile  string

	// Callbacks
	OnToggleBot func()
}

func NewBotConfigWindow(botInstance *bot.Bot, botConfig *bot.FileConfig, configFile string) (*BotConfigWindow, error) {
	bw := &BotConfigWindow{
		botInstance: botInstance,
		botConfig:   botConfig,
		configFile:  configFile,
		ready:       make(chan bool),
	}

	go bw.runWindow()
	<-bw.ready

	if bw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create bot config window")
	}

	return bw, nil
}

func (bw *BotConfigWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("ArcheFriendBotConfigClass")
	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend - Bot Config")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003,
		WndProc:    syscall.NewCallback(bw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5,
		ClassName:  className,
	}

	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		bw.ready <- true
		return
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW,
		100, 100,
		500, 520,
		0, 0,
		hInstance,
		0,
	)

	bw.hwnd = windows.Handle(hwnd)

	if hwnd != 0 {
		bw.createControls()
	}

	bw.ready <- true
	bw.messageLoop()
}

func (bw *BotConfigWindow) messageLoop() {
	msg := &MSG{}
	procIsDialogMessage := user32.NewProc("IsDialogMessageW")

	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(msg)),
			0, 0, 0,
		)

		if ret == 0 {
			break
		}

		isDialog, _, _ := procIsDialogMessage.Call(
			uintptr(bw.hwnd),
			uintptr(unsafe.Pointer(msg)),
		)

		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (bw *BotConfigWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listboxClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	y := 10

	// Title
	label, _ := syscall.UTF16PtrFromString("═══ BOT CONFIG ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 300, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// Mob Names Label + Edit (multiline)
	label, _ = syscall.UTF16PtrFromString("Mob Names (um por linha):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 200, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 25

	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_MULTILINE|ES_AUTOVSCROLL|0x00200000, // WS_VSCROLL
		10, uintptr(y), 300, 80,
		uintptr(bw.hwnd), IDC_BOT_EDIT_MOBS, hInstance, 0,
	)
	bw.editMobs = windows.Handle(hwnd)
	y += 90

	// Range
	label, _ = syscall.UTF16PtrFromString("Max Range (metros):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 150, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		170, uintptr(y), 80, 25,
		uintptr(bw.hwnd), IDC_BOT_EDIT_RANGE, hInstance, 0,
	)
	bw.editRange = windows.Handle(hwnd)
	y += 35

	// Scan Interval
	label, _ = syscall.UTF16PtrFromString("Scan Interval (ms):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 150, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		170, uintptr(y), 80, 25,
		uintptr(bw.hwnd), IDC_BOT_EDIT_SCAN, hInstance, 0,
	)
	bw.editScan = windows.Handle(hwnd)
	y += 35

	// Target Delay
	label, _ = syscall.UTF16PtrFromString("Target Delay (ms):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 150, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		170, uintptr(y), 80, 25,
		uintptr(bw.hwnd), IDC_BOT_EDIT_DELAY, hInstance, 0,
	)
	bw.editDelay = windows.Handle(hwnd)
	y += 35

	// Partial Match checkbox
	checkText, _ := syscall.UTF16PtrFromString("Partial Match (contains)")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		10, uintptr(y), 200, 25,
		uintptr(bw.hwnd), IDC_BOT_CHECK_PARTIAL, hInstance, 0,
	)
	bw.checkPartial = windows.Handle(hwnd)
	y += 30

	// === Combat Settings ===
	label, _ = syscall.UTF16PtrFromString("═══ COMBAT ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 200, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 25

	// Attack Key
	label, _ = syscall.UTF16PtrFromString("Attack Key:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 80, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		95, uintptr(y), 50, 22,
		uintptr(bw.hwnd), IDC_BOT_EDIT_ATTACK_KEY, hInstance, 0,
	)
	bw.editAttackKey = windows.Handle(hwnd)

	// Loot Key
	label, _ = syscall.UTF16PtrFromString("Loot Key:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		160, uintptr(y), 70, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		235, uintptr(y), 50, 22,
		uintptr(bw.hwnd), IDC_BOT_EDIT_LOOT_KEY, hInstance, 0,
	)
	bw.editLootKey = windows.Handle(hwnd)
	y += 28

	// Auto-Attack checkbox
	checkText, _ = syscall.UTF16PtrFromString("Auto-Attack")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003,
		10, uintptr(y), 120, 22,
		uintptr(bw.hwnd), IDC_BOT_CHECK_AUTOATTACK, hInstance, 0,
	)
	bw.checkAutoAttack = windows.Handle(hwnd)

	// Auto-Loot checkbox
	checkText, _ = syscall.UTF16PtrFromString("Auto-Loot")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003,
		140, uintptr(y), 120, 22,
		uintptr(bw.hwnd), IDC_BOT_CHECK_AUTOLOOT, hInstance, 0,
	)
	bw.checkAutoLoot = windows.Handle(hwnd)
	y += 35

	// Buttons
	btnText, _ := syscall.UTF16PtrFromString("Salvar Config")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, uintptr(y), 140, 30,
		uintptr(bw.hwnd), IDC_BOT_BUTTON_SAVE, hInstance, 0,
	)
	bw.btnSave = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Start/Stop Bot")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		160, uintptr(y), 140, 30,
		uintptr(bw.hwnd), IDC_BOT_BUTTON_START, hInstance, 0,
	)
	bw.btnStart = windows.Handle(hwnd)
	y += 45

	// Presets section (right side)
	label, _ = syscall.UTF16PtrFromString("═══ PRESETS ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		330, 10, 150, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000|0x00100000, // LBS_NOTIFY | WS_VSCROLL
		330, 40, 140, 200,
		uintptr(bw.hwnd), IDC_BOT_LIST_PRESETS, hInstance, 0,
	)
	bw.listPresets = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Load Preset")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		330, 250, 140, 30,
		uintptr(bw.hwnd), IDC_BOT_BUTTON_LOAD, hInstance, 0,
	)
	bw.btnLoad = windows.Handle(hwnd)

	// Load current values
	bw.loadValues()
}

func (bw *BotConfigWindow) loadValues() {
	if bw.botConfig == nil {
		return
	}

	// Mob names
	mobsText := strings.Join(bw.botConfig.MobNames, "\r\n")
	bw.setEditText(bw.editMobs, mobsText)

	// Range
	bw.setEditText(bw.editRange, fmt.Sprintf("%.0f", bw.botConfig.MaxRange))

	// Scan interval
	bw.setEditText(bw.editScan, fmt.Sprintf("%d", bw.botConfig.ScanIntervalMs))

	// Target delay
	bw.setEditText(bw.editDelay, fmt.Sprintf("%d", bw.botConfig.TargetDelayMs))

	// Partial match
	checkValue := uintptr(0)
	if bw.botConfig.PartialMatch {
		checkValue = 1
	}
	procSendMessage.Call(uintptr(bw.checkPartial), 0x00F1, checkValue, 0)

	// Attack key
	bw.setEditText(bw.editAttackKey, bw.botConfig.AttackKey)

	// Loot key
	bw.setEditText(bw.editLootKey, bw.botConfig.LootKey)

	// Auto-attack
	checkValue = 0
	if bw.botConfig.AutoAttack {
		checkValue = 1
	}
	procSendMessage.Call(uintptr(bw.checkAutoAttack), 0x00F1, checkValue, 0)

	// Auto-loot
	checkValue = 0
	if bw.botConfig.AutoLoot {
		checkValue = 1
	}
	procSendMessage.Call(uintptr(bw.checkAutoLoot), 0x00F1, checkValue, 0)

	// Presets
	procSendMessage.Call(uintptr(bw.listPresets), 0x0184, 0, 0) // LB_RESETCONTENT
	for name, mobs := range bw.botConfig.Presets {
		text := fmt.Sprintf("%s (%d mobs)", name, len(mobs))
		textPtr, _ := syscall.UTF16PtrFromString(text)
		procSendMessage.Call(uintptr(bw.listPresets), 0x0180, 0, uintptr(unsafe.Pointer(textPtr)))
	}
}

func (bw *BotConfigWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_BOT_BUTTON_SAVE:
				bw.onSave()
			case IDC_BOT_BUTTON_START:
				bw.onToggleBot()
			case IDC_BOT_BUTTON_LOAD:
				bw.onLoadPreset()
			}
		}

		// Double-click preset = load
		if cmdID == IDC_BOT_LIST_PRESETS && notifyCode == 2 {
			bw.onLoadPreset()
		}

	case WM_CLOSE:
		bw.Hide()
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (bw *BotConfigWindow) onSave() {
	if bw.botConfig == nil {
		return
	}

	// Parse mob names
	mobsText := bw.getEditText(bw.editMobs)
	lines := strings.Split(mobsText, "\n")
	var mobs []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if line != "" {
			mobs = append(mobs, line)
		}
	}
	bw.botConfig.MobNames = mobs

	// Range
	if r, err := strconv.ParseFloat(bw.getEditText(bw.editRange), 32); err == nil {
		bw.botConfig.MaxRange = float32(r)
	}

	// Scan interval
	if s, err := strconv.Atoi(bw.getEditText(bw.editScan)); err == nil {
		bw.botConfig.ScanIntervalMs = s
	}

	// Target delay
	if d, err := strconv.Atoi(bw.getEditText(bw.editDelay)); err == nil {
		bw.botConfig.TargetDelayMs = d
	}

	// Partial match
	ret, _, _ := procSendMessage.Call(uintptr(bw.checkPartial), 0x00F0, 0, 0)
	bw.botConfig.PartialMatch = ret == 1

	// Attack key
	bw.botConfig.AttackKey = strings.ToUpper(strings.TrimSpace(bw.getEditText(bw.editAttackKey)))

	// Loot key
	bw.botConfig.LootKey = strings.ToUpper(strings.TrimSpace(bw.getEditText(bw.editLootKey)))

	// Auto-attack
	ret, _, _ = procSendMessage.Call(uintptr(bw.checkAutoAttack), 0x00F0, 0, 0)
	bw.botConfig.AutoAttack = ret == 1

	// Auto-loot
	ret, _, _ = procSendMessage.Call(uintptr(bw.checkAutoLoot), 0x00F0, 0, 0)
	bw.botConfig.AutoLoot = ret == 1

	// Save to file
	if err := bot.SaveFileConfig(bw.configFile, bw.botConfig); err != nil {
		bw.showMessage("Erro", fmt.Sprintf("Falha ao salvar: %v", err))
		return
	}

	// Apply to running bot
	if bw.botInstance != nil {
		bw.botInstance.ApplyFileConfig(bw.botConfig)
	}

	fmt.Printf("[BOT] Config salva: %v | Range: %.0fm\n", mobs, bw.botConfig.MaxRange)
	bw.showMessage("Sucesso", "Configuração salva!")
}

func (bw *BotConfigWindow) onToggleBot() {
	if bw.OnToggleBot != nil {
		bw.OnToggleBot()
	}
}

func (bw *BotConfigWindow) onLoadPreset() {
	if bw.botConfig == nil {
		return
	}

	// Get selected index
	idx, _, _ := procSendMessage.Call(uintptr(bw.listPresets), 0x0188, 0, 0)
	if idx == 0xFFFFFFFF {
		bw.showMessage("Erro", "Selecione um preset!")
		return
	}

	// Get preset name by index
	i := 0
	for name, mobs := range bw.botConfig.Presets {
		if i == int(idx) {
			bw.botConfig.MobNames = mobs
			bw.setEditText(bw.editMobs, strings.Join(mobs, "\r\n"))

			if bw.botInstance != nil {
				bw.botInstance.SetMobNames(mobs)
			}

			fmt.Printf("[BOT] Preset '%s' carregado: %v\n", name, mobs)
			return
		}
		i++
	}
}

func (bw *BotConfigWindow) getEditText(hwnd windows.Handle) string {
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	procGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func (bw *BotConfigWindow) setEditText(hwnd windows.Handle, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(textPtr)))
}

func (bw *BotConfigWindow) showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(uintptr(bw.hwnd), uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00000040)
}

func (bw *BotConfigWindow) Show() {
	bw.visible = true
	bw.loadValues()
	procShowWindow.Call(uintptr(bw.hwnd), SW_SHOW)
	procSetWindowPos.Call(uintptr(bw.hwnd), HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|0x0040)
}

func (bw *BotConfigWindow) Hide() {
	bw.visible = false
	procShowWindow.Call(uintptr(bw.hwnd), SW_HIDE)
}

func (bw *BotConfigWindow) IsVisible() bool {
	return bw.visible
}

func (bw *BotConfigWindow) Toggle() {
	if bw.visible {
		bw.Hide()
	} else {
		bw.Show()
	}
}

func (bw *BotConfigWindow) SetBotInstance(b *bot.Bot) {
	bw.botInstance = b
}

func (bw *BotConfigWindow) SetBotConfig(cfg *bot.FileConfig) {
	bw.botConfig = cfg
	bw.loadValues()
}
