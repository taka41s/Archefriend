package gui

import (
	"archefriend/reaction"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Control IDs
const (
	IDC_EDIT_ID        = 1001
	IDC_EDIT_NAME      = 1002
	IDC_EDIT_ONSTART   = 1003
	IDC_EDIT_ONEND     = 1004
	IDC_CHECK_ISDEBUFF = 1005
	IDC_BUTTON_ADD     = 1006
	IDC_BUTTON_REMOVE  = 1007
	IDC_LIST_REACTIONS = 1009
	IDC_BUTTON_EDIT    = 1011
	IDC_BUTTON_TEST    = 1013
	IDC_CHECK_ISTARGET = 1014
)

type ConfigWindow struct {
	hwnd            windows.Handle
	reactionManager *reaction.Manager

	// Controls
	editID        windows.Handle
	editName      windows.Handle
	editOnStart   windows.Handle
	editOnEnd     windows.Handle
	checkIsDebuff windows.Handle
	checkIsTarget windows.Handle
	listReactions windows.Handle
	btnAdd        windows.Handle
	btnEdit       windows.Handle
	btnRemove     windows.Handle
	btnTest       windows.Handle

	// Callback to test reaction (emulates buff/debuff detection)
	// Passes the reaction ID to main to trigger via TriggerForTest
	TestReaction func(id uint32)

	visible bool
	ready   chan bool
}

func NewConfigWindow(reactionManager *reaction.Manager) (*ConfigWindow, error) {
	cw := &ConfigWindow{
		reactionManager: reactionManager,
		ready:           make(chan bool),
	}

	// Create window in separate goroutine with dedicated OS thread
	go cw.runWindow()

	// Wait for window to be created
	<-cw.ready

	if cw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create window")
	}

	return cw, nil
}

func (cw *ConfigWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Register window class
	className, _ := syscall.UTF16PtrFromString("ArcheFriendConfigClass")
	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend - Reaction Config")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003, // CS_HREDRAW | CS_VREDRAW
		WndProc:    syscall.NewCallback(cw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5, // COLOR_BTNFACE (light gray dialog background)
		ClassName:  className,
	}

	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		cw.ready <- true
		return
	}

	// Create window (initially hidden)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW,
		100, 100, // x, y
		800, 600, // width, height
		0, 0,
		hInstance,
		0,
	)

	cw.hwnd = windows.Handle(hwnd)

	if hwnd != 0 {
		cw.createControls()
	}

	// Signal that window is ready
	cw.ready <- true

	// Run message loop
	cw.messageLoop()
}

func (cw *ConfigWindow) messageLoop() {
	msg := &MSG{}
	procIsDialogMessage := user32.NewProc("IsDialogMessageW")

	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(msg)),
			0,
			0,
			0,
		)

		if ret == 0 {
			break
		}

		// Process dialog messages (allows TAB navigation and edit controls to work)
		isDialog, _, _ := procIsDialogMessage.Call(
			uintptr(cw.hwnd),
			uintptr(unsafe.Pointer(msg)),
		)

		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (cw *ConfigWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listboxClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	y := 10

	// Section header
	label, _ := syscall.UTF16PtrFromString("═══ NEW REACTION ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 300, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// ID Label + Edit
	label, _ = syscall.UTF16PtrFromString("Buff/Debuff ID:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 150, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		170, uintptr(y), 150, 25,
		uintptr(cw.hwnd), IDC_EDIT_ID, hInstance, 0,
	)
	cw.editID = windows.Handle(hwnd)
	y += 35

	// Name Label + Edit
	label, _ = syscall.UTF16PtrFromString("Name:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 150, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		170, uintptr(y), 250, 25,
		uintptr(cw.hwnd), IDC_EDIT_NAME, hInstance, 0,
	)
	cw.editName = windows.Handle(hwnd)
	y += 35

	// OnStart Label + Edit
	label, _ = syscall.UTF16PtrFromString("OnStart (e.g.: ALT+E && ALT+Q):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 250, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)
	y += 25

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL|ES_MULTILINE,
		10, uintptr(y), 410, 60,
		uintptr(cw.hwnd), IDC_EDIT_ONSTART, hInstance, 0,
	)
	cw.editOnStart = windows.Handle(hwnd)
	y += 70

	// OnEnd Label + Edit
	label, _ = syscall.UTF16PtrFromString("OnEnd (e.g.: E && Q):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 250, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)
	y += 25

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL|ES_MULTILINE,
		10, uintptr(y), 410, 60,
		uintptr(cw.hwnd), IDC_EDIT_ONEND, hInstance, 0,
	)
	cw.editOnEnd = windows.Handle(hwnd)
	y += 70

	// IsDebuff Checkbox
	checkText, _ := syscall.UTF16PtrFromString("Is Debuff")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		10, uintptr(y), 150, 25,
		uintptr(cw.hwnd), IDC_CHECK_ISDEBUFF, hInstance, 0,
	)
	cw.checkIsDebuff = windows.Handle(hwnd)

	// IsTarget Checkbox (same row, next to IsDebuff)
	checkText, _ = syscall.UTF16PtrFromString("Target Reaction")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		170, uintptr(y), 200, 25,
		uintptr(cw.hwnd), IDC_CHECK_ISTARGET, hInstance, 0,
	)
	cw.checkIsTarget = windows.Handle(hwnd)
	y += 35

	// Buttons row 1: Add/Update + Edit Selected
	btnText, _ := syscall.UTF16PtrFromString("Add/Update")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		10, uintptr(y), 150, 30,
		uintptr(cw.hwnd), IDC_BUTTON_ADD, hInstance, 0,
	)
	cw.btnAdd = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Edit Selected")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		170, uintptr(y), 140, 30,
		uintptr(cw.hwnd), IDC_BUTTON_EDIT, hInstance, 0,
	)
	cw.btnEdit = windows.Handle(hwnd)

	// Buttons row 2: Remove + Test
	btnText, _ = syscall.UTF16PtrFromString("Remove")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, uintptr(y)+40, 200, 30,
		uintptr(cw.hwnd), IDC_BUTTON_REMOVE, hInstance, 0,
	)
	cw.btnRemove = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Test")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		220, uintptr(y)+40, 200, 30,
		uintptr(cw.hwnd), IDC_BUTTON_TEST, hInstance, 0,
	)
	cw.btnTest = windows.Handle(hwnd)

	// List of reactions (right side)
	label, _ = syscall.UTF16PtrFromString("═══ CONFIGURED REACTIONS ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		440, 10, 340, 20,
		uintptr(cw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000|0x00100000, // LBS_NOTIFY | WS_VSCROLL
		440, 40, 340, 480,
		uintptr(cw.hwnd), IDC_LIST_REACTIONS, hInstance, 0,
	)
	cw.listReactions = windows.Handle(hwnd)

	// Populate list
	cw.refreshList()
}

func (cw *ConfigWindow) refreshList() {
	// Clear list
	procSendMessage.Call(
		uintptr(cw.listReactions),
		0x0184, // LB_RESETCONTENT
		0, 0,
	)

	reactions := cw.reactionManager.GetAllReactions()

	fmt.Printf("[CONFIG] RefreshList: %d reactions\n", len(reactions))
	for i, r := range reactions {
		typeStr := "BUFF"
		if r.IsDebuff {
			typeStr = "DEBUFF"
		}
		sourceStr := ""
		if r.Source == "target" {
			sourceStr = " [TGT]"
		}

		fmt.Printf("[CONFIG]   [%d] ID:%d %s%s\n", i, r.ID, r.Name, sourceStr)

		text := fmt.Sprintf("[%s%s] ID:%d %s -> %s", typeStr, sourceStr, r.ID, r.Name, r.UseString)
		textPtr, _ := syscall.UTF16PtrFromString(text)

		procSendMessage.Call(
			uintptr(cw.listReactions),
			0x0180, // LB_ADDSTRING
			0,
			uintptr(unsafe.Pointer(textPtr)),
		)
	}
}

func (cw *ConfigWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_BUTTON_ADD:
				cw.onAddReaction()
			case IDC_BUTTON_EDIT:
				cw.onEditReaction()
			case IDC_BUTTON_REMOVE:
				cw.onRemoveReaction()
			case IDC_BUTTON_TEST:
				cw.onTestReaction()
			}
		}

		// Double-click on list = edit
		if cmdID == IDC_LIST_REACTIONS && notifyCode == 2 { // LBN_DBLCLK
			cw.onEditReaction()
		}

	case WM_CLOSE:
		cw.Hide()
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(
		uintptr(hwnd),
		uintptr(msg),
		wParam,
		lParam,
	)
	return ret
}

func (cw *ConfigWindow) onAddReaction() {
	// Get values
	id := cw.getEditText(cw.editID)
	name := cw.getEditText(cw.editName)
	onStart := cw.getEditText(cw.editOnStart)
	onEnd := cw.getEditText(cw.editOnEnd)

	// Parse ID
	buffID, err := strconv.Atoi(id)
	if err != nil || buffID == 0 {
		cw.showMessage("Error", "Invalid ID! Enter a valid number.")
		return
	}

	// Check isDebuff
	isDebuff := false
	ret, _, _ := procSendMessage.Call(
		uintptr(cw.checkIsDebuff),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	if ret == 0x0001 { // BST_CHECKED
		isDebuff = true
	}

	// Check isTarget
	source := "player"
	ret, _, _ = procSendMessage.Call(
		uintptr(cw.checkIsTarget),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	if ret == 0x0001 { // BST_CHECKED
		source = "target"
	}

	// Check if already exists
	_, exists := cw.reactionManager.GetReaction(uint32(buffID))

	// Create reaction
	r := &reaction.Reaction{
		ID:          uint32(buffID),
		Name:        name,
		UseString:   onStart,
		OnEndString: onEnd,
		IsDebuff:    isDebuff,
		CooldownMS:  1000, // default cooldown
		Source:      source,
	}

	// Add to manager
	if isDebuff {
		err = cw.reactionManager.AddDebuffReaction(r)
	} else {
		err = cw.reactionManager.AddBuffReaction(r)
	}

	if err != nil {
		cw.showMessage("Error", fmt.Sprintf("Failed to add reaction: %v", err))
		return
	}

	// Save to JSON automatically
	err = cw.reactionManager.SaveToJSON()
	if err != nil {
		cw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	// Refresh list
	cw.refreshList()

	// Clear fields
	cw.clearFields()

	action := "added"
	if exists {
		action = "updated"
	}
	fmt.Printf("[CONFIG] Reaction %s: ID:%d %s\n", action, buffID, name)
	cw.showMessage("Success", fmt.Sprintf("Reaction %s: %s", action, name))
}

func (cw *ConfigWindow) onEditReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(cw.listReactions),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF { // LB_ERR
		cw.showMessage("Error", "Select a reaction to edit!")
		return
	}

	reactions := cw.reactionManager.GetAllReactions()

	fmt.Printf("[CONFIG] Edit: idx=%d, total reactions=%d\n", idx, len(reactions))

	if int(idx) >= len(reactions) {
		cw.showMessage("Error", fmt.Sprintf("Invalid index: %d >= %d", idx, len(reactions)))
		return
	}

	r := reactions[idx]

	fmt.Printf("[CONFIG] Editing reaction: idx=%d, ID:%d %s\n", idx, r.ID, r.Name)

	// Fill fields with selected reaction data
	cw.setEditText(cw.editID, fmt.Sprintf("%d", r.ID))
	cw.setEditText(cw.editName, r.Name)
	cw.setEditText(cw.editOnStart, r.UseString)
	cw.setEditText(cw.editOnEnd, r.OnEndString)

	// Set checkboxes
	checkValue := uintptr(0)
	if r.IsDebuff {
		checkValue = 1
	}
	procSendMessage.Call(
		uintptr(cw.checkIsDebuff),
		0x00F1, // BM_SETCHECK
		checkValue,
		0,
	)

	targetValue := uintptr(0)
	if r.Source == "target" {
		targetValue = 1
	}
	procSendMessage.Call(
		uintptr(cw.checkIsTarget),
		0x00F1, // BM_SETCHECK
		targetValue,
		0,
	)
}

func (cw *ConfigWindow) onRemoveReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(cw.listReactions),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF { // LB_ERR
		cw.showMessage("Error", "Select a reaction to remove!")
		return
	}

	reactions := cw.reactionManager.GetAllReactions()
	if int(idx) >= len(reactions) {
		return
	}

	r := reactions[idx]

	cw.reactionManager.RemoveReactionBySource(r.ID, r.Source)

	// Save to JSON automatically
	err := cw.reactionManager.SaveToJSON()
	if err != nil {
		cw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	cw.refreshList()
	fmt.Printf("[CONFIG] Removed reaction: ID:%d %s\n", r.ID, r.Name)
	cw.showMessage("Success", fmt.Sprintf("Reaction removed: %s", r.Name))
}

func (cw *ConfigWindow) onTestReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(cw.listReactions),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		cw.showMessage("Error", "Select a reaction to test!")
		return
	}

	reactions := cw.reactionManager.GetAllReactions()
	if int(idx) >= len(reactions) {
		return
	}

	r := reactions[idx]

	if r.UseString == "" {
		cw.showMessage("Error", "Reaction has no OnStart configured!")
		return
	}

	// Emulates full buff/debuff lifecycle via TriggerForTest (start + 2s delay + end)
	if cw.TestReaction != nil {
		rType := "BUFF"
		if r.IsDebuff {
			rType = "DEBUFF"
		}
		endInfo := ""
		if r.OnEndString != "" {
			endInfo = fmt.Sprintf(" | OnEnd(2s): %s", r.OnEndString)
		}
		fmt.Printf("[CONFIG] Emulating %s: %s (ID:%d) -> OnStart: %s%s\n", rType, r.Name, r.ID, r.UseString, endInfo)
		cw.TestReaction(r.ID)
		msg := fmt.Sprintf("Emulated %s: %s\nOnStart: %s", rType, r.Name, r.UseString)
		if r.OnEndString != "" {
			msg += fmt.Sprintf("\nOnEnd (after 2s): %s", r.OnEndString)
		}
		cw.showMessage("Test", msg)
	} else {
		cw.showMessage("Error", "TestReaction callback not set!")
	}
}

func (cw *ConfigWindow) getEditText(hwnd windows.Handle) string {
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))

	if length == 0 {
		return ""
	}

	buf := make([]uint16, length+1)
	procGetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)

	return syscall.UTF16ToString(buf)
}

func (cw *ConfigWindow) setEditText(hwnd windows.Handle, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(textPtr)),
	)
}

func (cw *ConfigWindow) clearFields() {
	cw.setEditText(cw.editID, "")
	cw.setEditText(cw.editName, "")
	cw.setEditText(cw.editOnStart, "")
	cw.setEditText(cw.editOnEnd, "")

	// Uncheck isDebuff
	procSendMessage.Call(
		uintptr(cw.checkIsDebuff),
		0x00F1, // BM_SETCHECK
		0,
		0,
	)
	// Uncheck isTarget
	procSendMessage.Call(
		uintptr(cw.checkIsTarget),
		0x00F1, // BM_SETCHECK
		0,
		0,
	)
}

func (cw *ConfigWindow) showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)

	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(
		uintptr(cw.hwnd),
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		0x00000040, // MB_ICONINFORMATION
	)
}

func (cw *ConfigWindow) Show() {
	cw.visible = true
	procShowWindow.Call(uintptr(cw.hwnd), SW_SHOW)
	procSetWindowPos.Call(
		uintptr(cw.hwnd),
		HWND_TOPMOST,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|0x0040, // SWP_SHOWWINDOW
	)
	cw.refreshList()

	// Set focus to first edit control
	procSetFocus := user32.NewProc("SetFocus")
	procSetFocus.Call(uintptr(cw.editID))
}

func (cw *ConfigWindow) Hide() {
	cw.visible = false
	procShowWindow.Call(uintptr(cw.hwnd), SW_HIDE)
}

func (cw *ConfigWindow) IsVisible() bool {
	return cw.visible
}

func (cw *ConfigWindow) Toggle() {
	if cw.visible {
		cw.Hide()
	} else {
		cw.Show()
	}
}
