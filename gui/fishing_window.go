package gui

import (
	"archefriend/fishing"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Fishing Control IDs
const (
	IDC_FISH_EDIT_RIGHT     = 3001
	IDC_FISH_EDIT_LEFT      = 3002
	IDC_FISH_EDIT_BIGREEL   = 3003
	IDC_FISH_EDIT_SMALLREEL = 3004
	IDC_FISH_EDIT_ESCAPE    = 3005
	IDC_FISH_BUTTON_SAVE    = 3007
	IDC_FISH_BUTTON_TOGGLE  = 3008
)

type FishingWindow struct {
	hwnd windows.Handle

	// Controls
	editRight     windows.Handle
	editLeft      windows.Handle
	editBigReel   windows.Handle
	editSmallReel windows.Handle
	editEscape    windows.Handle
	btnSave       windows.Handle
	btnToggle     windows.Handle
	lblStatus     windows.Handle

	visible    bool
	ready      chan bool
	configFile string

	// Bot reference
	fishBot *fishing.Bot

	// Callbacks
	OnToggle func()
}

func NewFishingWindow(fishBot *fishing.Bot, configFile string) (*FishingWindow, error) {
	fw := &FishingWindow{
		fishBot:    fishBot,
		configFile: configFile,
		ready:      make(chan bool),
	}

	go fw.runWindow()
	<-fw.ready

	if fw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create fishing window")
	}

	return fw, nil
}

func (fw *FishingWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("ArcheFriendFishingClass")
	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend - Sport Fishing Bot")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003,
		WndProc:    syscall.NewCallback(fw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5,
		ClassName:  className,
	}

	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		fw.ready <- true
		return
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW,
		200, 200,
		400, 350,
		0, 0,
		hInstance,
		0,
	)

	fw.hwnd = windows.Handle(hwnd)

	if hwnd != 0 {
		fw.createControls()
	}

	fw.ready <- true
	fw.messageLoop()
}

func (fw *FishingWindow) messageLoop() {
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
			uintptr(fw.hwnd),
			uintptr(unsafe.Pointer(msg)),
		)

		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (fw *FishingWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")

	y := 10

	// Title
	label, _ := syscall.UTF16PtrFromString("=== SPORT FISHING BOT ===")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 370, 20,
		uintptr(fw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// Status label
	statusText, _ := syscall.UTF16PtrFromString("[OFF]")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(statusText)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 370, 20,
		uintptr(fw.hwnd), 9999, hInstance, 0,
	)
	fw.lblStatus = windows.Handle(hwnd)
	y += 30

	// RIGHT (buff 5264)
	fw.editRight = fw.createKeybindRow(staticClass, editClass, hInstance, "RIGHT (5264):", IDC_FISH_EDIT_RIGHT, &y)

	// LEFT (buff 5265)
	fw.editLeft = fw.createKeybindRow(staticClass, editClass, hInstance, "LEFT (5265):", IDC_FISH_EDIT_LEFT, &y)

	// BIG REEL IN (buff 5508)
	fw.editBigReel = fw.createKeybindRow(staticClass, editClass, hInstance, "BIG REEL (5508):", IDC_FISH_EDIT_BIGREEL, &y)

	// SMALL REEL IN (buff 5266)
	fw.editSmallReel = fw.createKeybindRow(staticClass, editClass, hInstance, "SM REEL (5266):", IDC_FISH_EDIT_SMALLREEL, &y)

	// ESCAPE (buff 5267)
	fw.editEscape = fw.createKeybindRow(staticClass, editClass, hInstance, "ESCAPE (5267):", IDC_FISH_EDIT_ESCAPE, &y)

	y += 10

	// Buttons: Save | Toggle
	btnText, _ := syscall.UTF16PtrFromString("Save")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, uintptr(y), 170, 30,
		uintptr(fw.hwnd), IDC_FISH_BUTTON_SAVE, hInstance, 0,
	)
	fw.btnSave = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Start/Stop")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		190, uintptr(y), 170, 30,
		uintptr(fw.hwnd), IDC_FISH_BUTTON_TOGGLE, hInstance, 0,
	)
	fw.btnToggle = windows.Handle(hwnd)

	// Load values from config
	fw.loadValues()
}

func (fw *FishingWindow) createKeybindRow(staticClass, editClass *uint16, hInstance uintptr, labelText string, controlID uintptr, y *int) windows.Handle {
	label, _ := syscall.UTF16PtrFromString(labelText)
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(*y+3), 140, 20,
		uintptr(fw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		160, uintptr(*y), 190, 25,
		uintptr(fw.hwnd), controlID, hInstance, 0,
	)

	*y += 32
	return windows.Handle(hwnd)
}

func (fw *FishingWindow) loadValues() {
	if fw.fishBot == nil {
		return
	}

	cfg := fw.fishBot.GetConfig()

	// Map buff IDs to edit controls
	buffEdits := map[uint32]windows.Handle{
		5264: fw.editRight,
		5265: fw.editLeft,
		5508: fw.editBigReel,
		5266: fw.editSmallReel,
		5267: fw.editEscape,
	}

	for _, action := range cfg.Actions {
		if edit, ok := buffEdits[action.BuffID]; ok {
			fw.setEditText(edit, action.Keybind)
		}
	}

	fw.updateStatus()
}

func (fw *FishingWindow) updateStatus() {
	if fw.fishBot == nil {
		return
	}

	status := "[OFF]"
	if fw.fishBot.IsRunning() {
		stats := fw.fishBot.GetStats()
		status = fmt.Sprintf("[ON] Reactions: %d", stats.ReactionsTriggered)
	}

	fw.setEditText(fw.lblStatus, status)
}

func (fw *FishingWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_FISH_BUTTON_SAVE:
				fw.onSave()
			case IDC_FISH_BUTTON_TOGGLE:
				fw.onToggle()
			}
		}

	case WM_CLOSE:
		fw.Hide()
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (fw *FishingWindow) onSave() {
	if fw.fishBot == nil {
		return
	}

	// Read keybinds from edit controls
	rightKey := strings.ToUpper(strings.TrimSpace(fw.getEditText(fw.editRight)))
	leftKey := strings.ToUpper(strings.TrimSpace(fw.getEditText(fw.editLeft)))
	bigReelKey := strings.ToUpper(strings.TrimSpace(fw.getEditText(fw.editBigReel)))
	smallReelKey := strings.ToUpper(strings.TrimSpace(fw.getEditText(fw.editSmallReel)))
	escapeKey := strings.ToUpper(strings.TrimSpace(fw.getEditText(fw.editEscape)))

	cfg := fishing.Config{
		Actions: []fishing.FishingAction{
			{BuffID: 5264, Name: "RIGHT", Keybind: rightKey},
			{BuffID: 5508, Name: "BIG REEL IN", Keybind: bigReelKey},
			{BuffID: 5267, Name: "ESCAPE", Keybind: escapeKey},
			{BuffID: 5265, Name: "LEFT", Keybind: leftKey},
			{BuffID: 5266, Name: "SMALL REEL IN", Keybind: smallReelKey},
		},
	}

	fw.fishBot.SetConfig(cfg)

	if err := fw.fishBot.SaveConfig(fw.configFile); err != nil {
		fw.showMessage("Erro", fmt.Sprintf("Falha ao salvar: %v", err))
		return
	}

	fmt.Printf("[FISHING] Config salva: R=%s L=%s BR=%s SR=%s ESC=%s\n",
		rightKey, leftKey, bigReelKey, smallReelKey, escapeKey)
	fw.showMessage("Sucesso", "Fishing config salva!")
}

func (fw *FishingWindow) onToggle() {
	if fw.fishBot == nil {
		return
	}

	fw.fishBot.Toggle()
	fw.updateStatus()

	if fw.OnToggle != nil {
		fw.OnToggle()
	}
}

func (fw *FishingWindow) getEditText(hwnd windows.Handle) string {
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	procGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func (fw *FishingWindow) setEditText(hwnd windows.Handle, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(textPtr)))
}

func (fw *FishingWindow) showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(uintptr(fw.hwnd), uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00000040)
}

func (fw *FishingWindow) Show() {
	fw.visible = true
	fw.loadValues()
	procShowWindow.Call(uintptr(fw.hwnd), SW_SHOW)
	procSetWindowPos.Call(uintptr(fw.hwnd), HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|0x0040)
}

func (fw *FishingWindow) Hide() {
	fw.visible = false
	procShowWindow.Call(uintptr(fw.hwnd), SW_HIDE)
}

func (fw *FishingWindow) IsVisible() bool {
	return fw.visible
}

func (fw *FishingWindow) Toggle() {
	if fw.visible {
		fw.Hide()
	} else {
		fw.Show()
	}
}
