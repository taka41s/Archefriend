package gui

import (
	"archefriend/skill"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Skill Control IDs
const (
	IDC_SKILL_EDIT_ID         = 2001
	IDC_SKILL_EDIT_NAME       = 2002
	IDC_SKILL_EDIT_ONCAST     = 2003
	IDC_SKILL_EDIT_COOLDOWN   = 2004
	IDC_SKILL_CHECK_ENABLED   = 2005
	IDC_SKILL_BUTTON_ADD      = 2006
	IDC_SKILL_BUTTON_REMOVE   = 2007
	IDC_SKILL_BUTTON_SAVE     = 2008
	IDC_SKILL_LIST            = 2009
	IDC_SKILL_BUTTON_EDIT     = 2010
	IDC_SKILL_BUTTON_CLEAR    = 2011
	IDC_SKILL_BUTTON_TOGGLE   = 2012
	IDC_SKILL_CHECK_AIMBOT    = 2013
	IDC_SKILL_CHECK_AIMONTRY  = 2014
)

type SkillConfigWindow struct {
	hwnd            windows.Handle
	reactionManager *skill.ReactionManager
	configPath      string

	// Controls
	editID          windows.Handle
	editName        windows.Handle
	editOnCast      windows.Handle
	editCooldown    windows.Handle
	checkEnabled    windows.Handle
	checkAimbot     windows.Handle
	checkAimbotOnTry windows.Handle
	listSkills      windows.Handle
	btnAdd       windows.Handle
	btnEdit      windows.Handle
	btnRemove    windows.Handle
	btnClear     windows.Handle
	btnSave      windows.Handle
	btnToggle    windows.Handle

	visible bool
	ready   chan bool
}

func NewSkillConfigWindow(reactionManager *skill.ReactionManager, configPath string) (*SkillConfigWindow, error) {
	sw := &SkillConfigWindow{
		reactionManager: reactionManager,
		configPath:      configPath,
		ready:           make(chan bool),
	}

	// Create window in separate goroutine with dedicated OS thread
	go sw.runWindow()

	// Wait for window to be created
	<-sw.ready

	if sw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create skill config window")
	}

	return sw, nil
}

func (sw *SkillConfigWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Register window class
	className, _ := syscall.UTF16PtrFromString("ArcheFriendSkillConfigClass")
	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend - Skill Reactions")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003, // CS_HREDRAW | CS_VREDRAW
		WndProc:    syscall.NewCallback(sw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5, // COLOR_BTNFACE
		ClassName:  className,
	}

	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		sw.ready <- true
		return
	}

	// Create window (initially hidden)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW,
		150, 150, // x, y
		700, 500, // width, height
		0, 0,
		hInstance,
		0,
	)

	sw.hwnd = windows.Handle(hwnd)

	if hwnd != 0 {
		sw.createControls()
	}

	// Signal that window is ready
	sw.ready <- true

	// Run message loop
	sw.messageLoop()
}

func (sw *SkillConfigWindow) messageLoop() {
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

		// Process dialog messages (allows TAB navigation)
		isDialog, _, _ := procIsDialogMessage.Call(
			uintptr(sw.hwnd),
			uintptr(unsafe.Pointer(msg)),
		)

		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (sw *SkillConfigWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listboxClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	y := 10

	// Title
	label, _ := syscall.UTF16PtrFromString("=== SKILL REACTION ===")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 300, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// Skill ID Label + Edit
	label, _ = syscall.UTF16PtrFromString("Skill ID:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 100, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		120, uintptr(y), 150, 25,
		uintptr(sw.hwnd), IDC_SKILL_EDIT_ID, hInstance, 0,
	)
	sw.editID = windows.Handle(hwnd)
	y += 35

	// Name Label + Edit
	label, _ = syscall.UTF16PtrFromString("Name:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 100, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		120, uintptr(y), 200, 25,
		uintptr(sw.hwnd), IDC_SKILL_EDIT_NAME, hInstance, 0,
	)
	sw.editName = windows.Handle(hwnd)
	y += 35

	// OnCast Label + Edit
	label, _ = syscall.UTF16PtrFromString("OnCast (ex: ALT+Q):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 200, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)
	y += 25

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		10, uintptr(y), 310, 25,
		uintptr(sw.hwnd), IDC_SKILL_EDIT_ONCAST, hInstance, 0,
	)
	sw.editOnCast = windows.Handle(hwnd)
	y += 35

	// Cooldown Label + Edit
	label, _ = syscall.UTF16PtrFromString("Cooldown (ms):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 100, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		120, uintptr(y), 100, 25,
		uintptr(sw.hwnd), IDC_SKILL_EDIT_COOLDOWN, hInstance, 0,
	)
	sw.editCooldown = windows.Handle(hwnd)

	// Default cooldown
	defaultCooldown, _ := syscall.UTF16PtrFromString("500")
	procSetWindowText.Call(uintptr(sw.editCooldown), uintptr(unsafe.Pointer(defaultCooldown)))
	y += 35

	// Enabled Checkbox
	checkText, _ := syscall.UTF16PtrFromString("Enabled")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		10, uintptr(y), 100, 25,
		uintptr(sw.hwnd), IDC_SKILL_CHECK_ENABLED, hInstance, 0,
	)
	sw.checkEnabled = windows.Handle(hwnd)

	// Set checked by default
	procSendMessage.Call(uintptr(sw.checkEnabled), 0x00F1, 1, 0) // BM_SETCHECK, BST_CHECKED

	// Aimbot Checkbox
	checkText, _ = syscall.UTF16PtrFromString("Aimbot")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		120, uintptr(y), 80, 25,
		uintptr(sw.hwnd), IDC_SKILL_CHECK_AIMBOT, hInstance, 0,
	)
	sw.checkAimbot = windows.Handle(hwnd)

	// Aimbot on Try Checkbox (antes do cast)
	checkText, _ = syscall.UTF16PtrFromString("On Try")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003, // BS_AUTOCHECKBOX
		210, uintptr(y), 80, 25,
		uintptr(sw.hwnd), IDC_SKILL_CHECK_AIMONTRY, hInstance, 0,
	)
	sw.checkAimbotOnTry = windows.Handle(hwnd)
	y += 35

	// Buttons row 1
	btnText, _ := syscall.UTF16PtrFromString("Add/Update")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		10, uintptr(y), 100, 30,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_ADD, hInstance, 0,
	)
	sw.btnAdd = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Edit Selected")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		120, uintptr(y), 100, 30,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_EDIT, hInstance, 0,
	)
	sw.btnEdit = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Clear")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		230, uintptr(y), 90, 30,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_CLEAR, hInstance, 0,
	)
	sw.btnClear = windows.Handle(hwnd)
	y += 40

	// Buttons row 2
	btnText, _ = syscall.UTF16PtrFromString("Remove")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, uintptr(y), 100, 30,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_REMOVE, hInstance, 0,
	)
	sw.btnRemove = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Toggle ON/OFF")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		120, uintptr(y), 100, 30,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_TOGGLE, hInstance, 0,
	)
	sw.btnToggle = windows.Handle(hwnd)
	y += 40

	// Save button
	btnText, _ = syscall.UTF16PtrFromString("Save All")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, uintptr(y), 310, 35,
		uintptr(sw.hwnd), IDC_SKILL_BUTTON_SAVE, hInstance, 0,
	)
	sw.btnSave = windows.Handle(hwnd)

	// List of skill reactions (right side)
	label, _ = syscall.UTF16PtrFromString("=== SKILL REACTIONS ===")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		340, 10, 340, 20,
		uintptr(sw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000|0x00100000, // LBS_NOTIFY | WS_VSCROLL
		340, 40, 340, 380,
		uintptr(sw.hwnd), IDC_SKILL_LIST, hInstance, 0,
	)
	sw.listSkills = windows.Handle(hwnd)

	// Populate list
	sw.refreshList()
}

func (sw *SkillConfigWindow) refreshList() {
	// Clear list
	procSendMessage.Call(
		uintptr(sw.listSkills),
		0x0184, // LB_RESETCONTENT
		0, 0,
	)

	reactions := sw.reactionManager.GetAllReactions()

	fmt.Printf("[SKILL-CONFIG] RefreshList: %d reactions\n", len(reactions))
	for _, r := range reactions {
		status := "OFF"
		if r.Enabled {
			status = "ON"
		}

		aimStr := ""
		if r.UseAimbot {
			if r.AimbotOnTry {
				aimStr = "[AIM-TRY]"
			} else {
				aimStr = "[AIM]"
			}
		}

		text := fmt.Sprintf("[%s]%s ID:%d %s -> %s", status, aimStr, r.SkillID, r.Name, r.OnCast)
		textPtr, _ := syscall.UTF16PtrFromString(text)

		procSendMessage.Call(
			uintptr(sw.listSkills),
			0x0180, // LB_ADDSTRING
			0,
			uintptr(unsafe.Pointer(textPtr)),
		)
	}
}

func (sw *SkillConfigWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_SKILL_BUTTON_ADD:
				sw.onAddReaction()
			case IDC_SKILL_BUTTON_EDIT:
				sw.onEditReaction()
			case IDC_SKILL_BUTTON_CLEAR:
				sw.clearFields()
			case IDC_SKILL_BUTTON_REMOVE:
				sw.onRemoveReaction()
			case IDC_SKILL_BUTTON_SAVE:
				sw.onSaveAll()
			case IDC_SKILL_BUTTON_TOGGLE:
				sw.onToggleReaction()
			}
		}

		// Double-click = edit
		if cmdID == IDC_SKILL_LIST && notifyCode == 2 { // LBN_DBLCLK
			sw.onEditReaction()
		}

	case WM_CLOSE:
		sw.Hide()
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

func (sw *SkillConfigWindow) onAddReaction() {
	// Get values
	id := sw.getEditText(sw.editID)
	name := sw.getEditText(sw.editName)
	onCast := sw.getEditText(sw.editOnCast)
	cooldownStr := sw.getEditText(sw.editCooldown)

	// Parse ID
	skillID, err := strconv.Atoi(id)
	if err != nil || skillID == 0 {
		sw.showMessage("Error", "Invalid Skill ID!")
		return
	}

	// Parse cooldown
	cooldown, err := strconv.Atoi(cooldownStr)
	if err != nil {
		cooldown = 500
	}

	// Check enabled
	enabled := false
	ret, _, _ := procSendMessage.Call(
		uintptr(sw.checkEnabled),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	if ret == 0x0001 { // BST_CHECKED
		enabled = true
	}

	// Check aimbot
	useAimbot := false
	ret, _, _ = procSendMessage.Call(
		uintptr(sw.checkAimbot),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	if ret == 0x0001 { // BST_CHECKED
		useAimbot = true
	}

	// Check aimbot on try
	aimbotOnTry := false
	ret, _, _ = procSendMessage.Call(
		uintptr(sw.checkAimbotOnTry),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	if ret == 0x0001 { // BST_CHECKED
		aimbotOnTry = true
	}

	// Check if already exists
	existing := sw.reactionManager.GetReaction(uint32(skillID))

	// Create reaction
	r := &skill.SkillReaction{
		SkillID:     uint32(skillID),
		Name:        name,
		OnCast:      onCast,
		Enabled:     enabled,
		CooldownMS:  cooldown,
		UseAimbot:   useAimbot,
		AimbotOnTry: aimbotOnTry,
	}

	// Add to manager
	sw.reactionManager.AddReaction(r)

	// Save to JSON
	err = sw.reactionManager.SaveToJSON(sw.configPath)
	if err != nil {
		sw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	// Refresh list
	sw.refreshList()

	// Clear fields
	sw.clearFields()

	// Show success message
	action := "added"
	if existing != nil {
		action = "updated"
	}
	fmt.Printf("[SKILL-CONFIG] Reaction %s: ID:%d %s\n", action, skillID, name)
	sw.showMessage("Success", fmt.Sprintf("Reaction %s: %s", action, name))
}

func (sw *SkillConfigWindow) onEditReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(sw.listSkills),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF { // LB_ERR
		sw.showMessage("Error", "Select a reaction to edit!")
		return
	}

	reactions := sw.reactionManager.GetAllReactions()

	if int(idx) >= len(reactions) {
		sw.showMessage("Error", fmt.Sprintf("Invalid index: %d >= %d", idx, len(reactions)))
		return
	}

	r := reactions[idx]

	// Fill fields with selected reaction data
	sw.setEditText(sw.editID, fmt.Sprintf("%d", r.SkillID))
	sw.setEditText(sw.editName, r.Name)
	sw.setEditText(sw.editOnCast, r.OnCast)
	sw.setEditText(sw.editCooldown, fmt.Sprintf("%d", r.CooldownMS))

	// Set enabled checkbox
	checkValue := uintptr(0)
	if r.Enabled {
		checkValue = 1
	}
	procSendMessage.Call(
		uintptr(sw.checkEnabled),
		0x00F1, // BM_SETCHECK
		checkValue,
		0,
	)

	// Set aimbot checkbox
	aimbotValue := uintptr(0)
	if r.UseAimbot {
		aimbotValue = 1
	}
	procSendMessage.Call(
		uintptr(sw.checkAimbot),
		0x00F1, // BM_SETCHECK
		aimbotValue,
		0,
	)

	// Set aimbot on try checkbox
	aimbotOnTryValue := uintptr(0)
	if r.AimbotOnTry {
		aimbotOnTryValue = 1
	}
	procSendMessage.Call(
		uintptr(sw.checkAimbotOnTry),
		0x00F1, // BM_SETCHECK
		aimbotOnTryValue,
		0,
	)
}

func (sw *SkillConfigWindow) onToggleReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(sw.listSkills),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		sw.showMessage("Error", "Select a reaction to toggle!")
		return
	}

	reactions := sw.reactionManager.GetAllReactions()
	if int(idx) >= len(reactions) {
		return
	}

	r := reactions[idx]
	r.Enabled = !r.Enabled

	// Save to JSON
	err := sw.reactionManager.SaveToJSON(sw.configPath)
	if err != nil {
		sw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	sw.refreshList()

	status := "OFF"
	if r.Enabled {
		status = "ON"
	}
	fmt.Printf("[SKILL-CONFIG] Toggled %s: %s\n", r.Name, status)
}

func (sw *SkillConfigWindow) onRemoveReaction() {
	// Get selected index
	idx, _, _ := procSendMessage.Call(
		uintptr(sw.listSkills),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		sw.showMessage("Error", "Select a reaction to remove!")
		return
	}

	reactions := sw.reactionManager.GetAllReactions()
	if int(idx) >= len(reactions) {
		return
	}

	r := reactions[idx]

	sw.reactionManager.RemoveReaction(r.SkillID)

	// Save to JSON
	err := sw.reactionManager.SaveToJSON(sw.configPath)
	if err != nil {
		sw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	sw.refreshList()
	fmt.Printf("[SKILL-CONFIG] Removed reaction: ID:%d %s\n", r.SkillID, r.Name)
	sw.showMessage("Success", fmt.Sprintf("Removed: %s", r.Name))
}

func (sw *SkillConfigWindow) onSaveAll() {
	err := sw.reactionManager.SaveToJSON(sw.configPath)
	if err != nil {
		sw.showMessage("Error", fmt.Sprintf("Failed to save: %v", err))
		return
	}

	fmt.Println("[SKILL-CONFIG] All reactions saved to skill_reactions.json")
	sw.showMessage("Success", "All reactions saved!")
}

func (sw *SkillConfigWindow) getEditText(hwnd windows.Handle) string {
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

func (sw *SkillConfigWindow) setEditText(hwnd windows.Handle, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(textPtr)),
	)
}

func (sw *SkillConfigWindow) clearFields() {
	sw.setEditText(sw.editID, "")
	sw.setEditText(sw.editName, "")
	sw.setEditText(sw.editOnCast, "")
	sw.setEditText(sw.editCooldown, "500")

	// Check enabled by default
	procSendMessage.Call(
		uintptr(sw.checkEnabled),
		0x00F1, // BM_SETCHECK
		1,      // BST_CHECKED
		0,
	)

	// Uncheck aimbot by default
	procSendMessage.Call(
		uintptr(sw.checkAimbot),
		0x00F1, // BM_SETCHECK
		0,      // BST_UNCHECKED
		0,
	)

	// Uncheck aimbot on try by default
	procSendMessage.Call(
		uintptr(sw.checkAimbotOnTry),
		0x00F1, // BM_SETCHECK
		0,      // BST_UNCHECKED
		0,
	)
}

func (sw *SkillConfigWindow) showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)

	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(
		uintptr(sw.hwnd),
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		0x00000040, // MB_ICONINFORMATION
	)
}

func (sw *SkillConfigWindow) Show() {
	sw.visible = true
	procShowWindow.Call(uintptr(sw.hwnd), SW_SHOW)
	procSetWindowPos.Call(
		uintptr(sw.hwnd),
		HWND_TOPMOST,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|0x0040, // SWP_SHOWWINDOW
	)
	sw.refreshList()

	// Set focus to first edit control
	procSetFocus := user32.NewProc("SetFocus")
	procSetFocus.Call(uintptr(sw.editID))
}

func (sw *SkillConfigWindow) Hide() {
	sw.visible = false
	procShowWindow.Call(uintptr(sw.hwnd), SW_HIDE)
}

func (sw *SkillConfigWindow) IsVisible() bool {
	return sw.visible
}

func (sw *SkillConfigWindow) Toggle() {
	if sw.visible {
		sw.Hide()
	} else {
		sw.Show()
	}
}
