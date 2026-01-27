package gui

import (
	"archefriend/buff"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Control IDs para buff window
const (
	IDC_BUFF_EDIT_ID        = 2001
	IDC_BUFF_EDIT_NAME      = 2002
	IDC_BUFF_CHECK_PERM     = 2003
	IDC_BUFF_CHECK_HIDDEN   = 2004
	IDC_BUFF_EDIT_STACK     = 2005
	IDC_BUFF_BTN_INJECT     = 2006
	IDC_BUFF_BTN_REMOVE     = 2007
	IDC_BUFF_BTN_CLEAR      = 2008
	IDC_BUFF_LIST_INJECTED  = 2009
	IDC_BUFF_LIST_PRESETS   = 2010
	IDC_BUFF_BTN_APPLY      = 2011
	IDC_BUFF_BTN_UNAPPLY    = 2012
	IDC_BUFF_EDIT_PRESET    = 2013
	IDC_BUFF_BTN_SAVEPRESET = 2014
	IDC_BUFF_BTN_DELPRESET  = 2015
	IDC_BUFF_CHECK_FREEZE   = 2016
	IDC_BUFF_BTN_SETQUICK   = 2017
)

type BuffWindow struct {
	hwnd            windows.Handle
	injector        *buff.Injector
	presetManager   *buff.PresetManager

	// Controls - Injection
	editID          windows.Handle
	editName        windows.Handle
	editStack       windows.Handle
	checkPermanent  windows.Handle
	checkHidden     windows.Handle
	btnInject       windows.Handle
	btnRemove       windows.Handle
	btnClear        windows.Handle
	listInjected    windows.Handle
	checkFreeze     windows.Handle

	// Controls - Presets
	listPresets    windows.Handle
	btnApply       windows.Handle
	btnUnapply     windows.Handle
	btnSetQuick    windows.Handle
	editPresetName windows.Handle
	btnSavePreset  windows.Handle
	btnDelPreset   windows.Handle

	visible bool
	ready   chan bool
}

func NewBuffWindow(injector *buff.Injector, presetManager *buff.PresetManager) (*BuffWindow, error) {
	bw := &BuffWindow{
		injector:      injector,
		presetManager: presetManager,
		ready:         make(chan bool),
	}

	// Create window in separate goroutine with dedicated OS thread
	go bw.runWindow()

	// Wait for window to be created
	<-bw.ready

	if bw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create window")
	}

	return bw, nil
}

func (bw *BuffWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("ArcheFriendBuffClass")
	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend - Buff Injector")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003,
		WndProc:    syscall.NewCallback(bw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5, // COLOR_BTNFACE (light gray dialog background)
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
		150, 150,
		900, 650,
		0, 0,
		hInstance,
		0,
	)

	bw.hwnd = windows.Handle(hwnd)

	if hwnd != 0 {
		bw.createControls()
	}

	// Signal that window is ready
	bw.ready <- true

	// Run message loop
	bw.messageLoop()
}

func (bw *BuffWindow) messageLoop() {
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
			uintptr(bw.hwnd),
			uintptr(unsafe.Pointer(msg)),
		)

		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (bw *BuffWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listboxClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	y := 10

	// ═══ INJECT SINGLE BUFF ═══
	label, _ := syscall.UTF16PtrFromString("═══ INJECT BUFF ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 400, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// Buff ID
	label, _ = syscall.UTF16PtrFromString("Buff ID:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 80, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE - 3D sunken border
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		100, uintptr(y), 100, 25,
		uintptr(bw.hwnd), IDC_BUFF_EDIT_ID, hInstance, 0,
	)
	bw.editID = windows.Handle(hwnd)

	// Name
	label, _ = syscall.UTF16PtrFromString("Nome:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		210, uintptr(y), 50, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE - 3D sunken border
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		270, uintptr(y), 140, 25,
		uintptr(bw.hwnd), IDC_BUFF_EDIT_NAME, hInstance, 0,
	)
	bw.editName = windows.Handle(hwnd)
	y += 35

	// Stack
	label, _ = syscall.UTF16PtrFromString("Stack (0=default):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 120, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE - 3D sunken border
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		140, uintptr(y), 80, 25,
		uintptr(bw.hwnd), IDC_BUFF_EDIT_STACK, hInstance, 0,
	)
	bw.editStack = windows.Handle(hwnd)

	// Default stack 0
	stackText, _ := syscall.UTF16PtrFromString("0")
	procSetWindowText.Call(uintptr(bw.editStack), uintptr(unsafe.Pointer(stackText)))
	y += 35

	// Checkboxes
	checkText, _ := syscall.UTF16PtrFromString("Permanent")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003,
		10, uintptr(y), 100, 25,
		uintptr(bw.hwnd), IDC_BUFF_CHECK_PERM, hInstance, 0,
	)
	bw.checkPermanent = windows.Handle(hwnd)

	checkText, _ = syscall.UTF16PtrFromString("Hidden")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003,
		120, uintptr(y), 100, 25,
		uintptr(bw.hwnd), IDC_BUFF_CHECK_HIDDEN, hInstance, 0,
	)
	bw.checkHidden = windows.Handle(hwnd)

	checkText, _ = syscall.UTF16PtrFromString("Freeze Permanent Buffs")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(checkText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000003,
		240, uintptr(y), 170, 25,
		uintptr(bw.hwnd), IDC_BUFF_CHECK_FREEZE, hInstance, 0,
	)
	bw.checkFreeze = windows.Handle(hwnd)
	y += 35

	// Buttons
	btnText, _ := syscall.UTF16PtrFromString("Inject")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		10, uintptr(y), 80, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_INJECT, hInstance, 0,
	)
	bw.btnInject = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Remove ID")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		100, uintptr(y), 90, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_REMOVE, hInstance, 0,
	)
	bw.btnRemove = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Clear All")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		200, uintptr(y), 90, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_CLEAR, hInstance, 0,
	)
	bw.btnClear = windows.Handle(hwnd)
	y += 45

	// List of injected buffs
	label, _ = syscall.UTF16PtrFromString("─── Injected Buffs ───")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, uintptr(y), 200, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 25

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000|0x00100000,
		10, uintptr(y), 410, 200,
		uintptr(bw.hwnd), IDC_BUFF_LIST_INJECTED, hInstance, 0,
	)
	bw.listInjected = windows.Handle(hwnd)

	// ═══ PRESETS (Right side) ═══
	y = 10
	label, _ = syscall.UTF16PtrFromString("═══ BUFF PRESETS ═══")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		440, uintptr(y), 400, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)
	y += 30

	// Preset list
	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200,
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000|0x00100000,
		440, uintptr(y), 420, 300,
		uintptr(bw.hwnd), IDC_BUFF_LIST_PRESETS, hInstance, 0,
	)
	bw.listPresets = windows.Handle(hwnd)
	y += 310

	// Preset buttons
	btnText, _ = syscall.UTF16PtrFromString("Apply Preset")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		440, uintptr(y), 120, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_APPLY, hInstance, 0,
	)
	bw.btnApply = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Remove Preset")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		570, uintptr(y), 130, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_UNAPPLY, hInstance, 0,
	)
	bw.btnUnapply = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Set Quick (F9)")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		710, uintptr(y), 150, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_SETQUICK, hInstance, 0,
	)
	bw.btnSetQuick = windows.Handle(hwnd)
	y += 40

	// New preset name
	label, _ = syscall.UTF16PtrFromString("New Preset Name:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		440, uintptr(y), 130, 20,
		uintptr(bw.hwnd), 0, hInstance, 0,
	)

	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE - 3D sunken border
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		580, uintptr(y), 280, 25,
		uintptr(bw.hwnd), IDC_BUFF_EDIT_PRESET, hInstance, 0,
	)
	bw.editPresetName = windows.Handle(hwnd)
	y += 35

	btnText, _ = syscall.UTF16PtrFromString("Save Current as Preset")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		440, uintptr(y), 180, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_SAVEPRESET, hInstance, 0,
	)
	bw.btnSavePreset = windows.Handle(hwnd)

	btnText, _ = syscall.UTF16PtrFromString("Delete Preset")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(btnText)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		630, uintptr(y), 130, 30,
		uintptr(bw.hwnd), IDC_BUFF_BTN_DELPRESET, hInstance, 0,
	)
	bw.btnDelPreset = windows.Handle(hwnd)

	// Populate lists
	bw.refreshLists()
}

func (bw *BuffWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_BUFF_BTN_INJECT:
				bw.onInject()
			case IDC_BUFF_BTN_REMOVE:
				bw.onRemove()
			case IDC_BUFF_BTN_CLEAR:
				bw.onClear()
			case IDC_BUFF_BTN_APPLY:
				bw.onApplyPreset()
			case IDC_BUFF_BTN_UNAPPLY:
				bw.onRemovePreset()
			case IDC_BUFF_BTN_SAVEPRESET:
				bw.onSavePreset()
			case IDC_BUFF_BTN_DELPRESET:
				bw.onDeletePreset()
			case IDC_BUFF_BTN_SETQUICK:
				bw.onSetQuickAction()
			case IDC_BUFF_CHECK_FREEZE:
				bw.onToggleFreeze()
			}
		}

	case WM_CLOSE:
		bw.Hide()
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

func (bw *BuffWindow) onInject() {
	idStr := bw.getEditText(bw.editID)

	buffID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		bw.showMessage("Erro", "ID inválido! Digite apenas números.")
		return
	}

	if buffID == 0 {
		bw.showMessage("Erro", "ID não pode ser zero!")
		return
	}

	name := bw.getEditText(bw.editName)
	if name == "" {
		name = fmt.Sprintf("Buff_%d", buffID)
	}

	stackStr := bw.getEditText(bw.editStack)
	stack, _ := strconv.ParseUint(stackStr, 10, 32)

	permanent := bw.isChecked(bw.checkPermanent)
	hidden := bw.isChecked(bw.checkHidden)

	var success bool
	if hidden {
		success = bw.injector.InjectFirstAsHidden(uint32(buffID), permanent)
	} else {
		success = bw.injector.CloneFirstAndInject(uint32(buffID), permanent)
	}

	if success {
		if stack > 0 {
			idx := bw.injector.FindBuffByID(uint32(buffID))
			if idx >= 0 {
				bw.injector.SetBuffStack(idx, uint32(stack))
			}
		}
		bw.showMessage("Sucesso", fmt.Sprintf("Buff %s injetado!", name))
		bw.refreshLists()
	} else {
		bw.showMessage("Erro", "Falha ao injetar buff!")
	}
}

func (bw *BuffWindow) onRemove() {
	idStr := bw.getEditText(bw.editID)
	buffID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || buffID == 0 {
		bw.showMessage("Erro", "ID inválido!")
		return
	}

	if bw.injector.RemoveBuffExtended(uint32(buffID)) {
		bw.showMessage("Sucesso", fmt.Sprintf("Buff %d removido!", buffID))
		bw.refreshLists()
	} else {
		bw.showMessage("Erro", "Buff não encontrado!")
	}
}

func (bw *BuffWindow) onClear() {
	count := bw.injector.ClearAllInjected()
	bw.showMessage("Sucesso", fmt.Sprintf("%d buffs removidos!", count))
	bw.refreshLists()
}

func (bw *BuffWindow) onApplyPreset() {
	idx, _, _ := procSendMessage.Call(
		uintptr(bw.listPresets),
		0x0188, // LB_GETCURSEL
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		bw.showMessage("Erro", "Selecione um preset!")
		return
	}

	presets := bw.presetManager.GetAllPresets()
	if int(idx) >= len(presets) {
		return
	}

	preset := presets[idx]
	count, err := bw.presetManager.ApplyPreset(preset.Name)
	if err != nil {
		bw.showMessage("Erro", fmt.Sprintf("Falha: %v", err))
		return
	}

	bw.showMessage("Sucesso", fmt.Sprintf("Preset '%s' aplicado! %d buffs injetados.", preset.Name, count))
	bw.refreshLists()
}

func (bw *BuffWindow) onRemovePreset() {
	idx, _, _ := procSendMessage.Call(
		uintptr(bw.listPresets),
		0x0188,
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		bw.showMessage("Erro", "Selecione um preset!")
		return
	}

	presets := bw.presetManager.GetAllPresets()
	if int(idx) >= len(presets) {
		return
	}

	preset := presets[idx]
	count, err := bw.presetManager.RemovePresetBuffs(preset.Name)
	if err != nil {
		bw.showMessage("Erro", fmt.Sprintf("Falha: %v", err))
		return
	}

	bw.showMessage("Sucesso", fmt.Sprintf("%d buffs removidos de '%s'.", count, preset.Name))
	bw.refreshLists()
}

func (bw *BuffWindow) onSavePreset() {
	name := bw.getEditText(bw.editPresetName)
	if name == "" {
		bw.showMessage("Erro", "Digite um nome para o preset!")
		return
	}

	// Create preset from all currently visible buffs
	description := fmt.Sprintf("Preset criado automaticamente")
	if err := bw.presetManager.CreatePresetFromCurrent(name, description); err != nil {
		bw.showMessage("Erro", fmt.Sprintf("Falha ao salvar preset: %v", err))
		return
	}

	bw.presetManager.SaveToJSON("buff_presets.json")
	bw.showMessage("Sucesso", fmt.Sprintf("Preset '%s' salvo!", name))
	bw.refreshLists()
}

func (bw *BuffWindow) onDeletePreset() {
	idx, _, _ := procSendMessage.Call(
		uintptr(bw.listPresets),
		0x0188,
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		bw.showMessage("Erro", "Selecione um preset!")
		return
	}

	presets := bw.presetManager.GetAllPresets()
	if int(idx) >= len(presets) {
		return
	}

	preset := presets[idx]
	bw.presetManager.RemovePreset(preset.Name)
	bw.presetManager.SaveToJSON("buff_presets.json")

	bw.showMessage("Sucesso", fmt.Sprintf("Preset '%s' deletado!", preset.Name))
	bw.refreshLists()
}

func (bw *BuffWindow) onSetQuickAction() {
	idx, _, _ := procSendMessage.Call(
		uintptr(bw.listPresets),
		0x0188,
		0, 0,
	)

	if idx == 0xFFFFFFFF {
		bw.showMessage("Erro", "Selecione um preset!")
		return
	}

	presets := bw.presetManager.GetAllPresets()
	if int(idx) >= len(presets) {
		return
	}

	preset := presets[idx]
	if err := bw.presetManager.SetQuickActionPreset(preset.Name); err != nil {
		bw.showMessage("Erro", fmt.Sprintf("Falha ao definir quick action: %v", err))
		return
	}

	bw.showMessage("Sucesso", fmt.Sprintf("Quick action definido: '%s'\nUse F9 para aplicar/remover!", preset.Name))
}

func (bw *BuffWindow) onToggleFreeze() {
	enabled := bw.isChecked(bw.checkFreeze)
	bw.injector.SetFreezeEnabled(enabled)

	if enabled {
		fmt.Println("[BUFF] Freeze enabled")
	} else {
		fmt.Println("[BUFF] Freeze disabled")
	}
}

func (bw *BuffWindow) refreshLists() {
	// Clear injected list
	procSendMessage.Call(
		uintptr(bw.listInjected),
		0x0184,
		0, 0,
	)

	// Add injected buffs
	for id, info := range bw.injector.GetInjectedBuffs() {
		text := fmt.Sprintf("ID:%d (src:%d, perm:%v)", id, info.SourceID, info.Permanent)
		textPtr, _ := syscall.UTF16PtrFromString(text)
		procSendMessage.Call(
			uintptr(bw.listInjected),
			0x0180,
			0,
			uintptr(unsafe.Pointer(textPtr)),
		)
	}

	// Clear preset list
	procSendMessage.Call(
		uintptr(bw.listPresets),
		0x0184,
		0, 0,
	)

	// Add presets
	for _, preset := range bw.presetManager.GetAllPresets() {
		text := fmt.Sprintf("%s (%d buffs) - %s", preset.Name, len(preset.Buffs), preset.Description)
		textPtr, _ := syscall.UTF16PtrFromString(text)
		procSendMessage.Call(
			uintptr(bw.listPresets),
			0x0180,
			0,
			uintptr(unsafe.Pointer(textPtr)),
		)
	}
}

func (bw *BuffWindow) getEditText(hwnd windows.Handle) string {
	// Get text length using GetWindowTextLength
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))

	if length == 0 {
		return ""
	}

	// Read text (length+1 para incluir null terminator)
	buf := make([]uint16, length+1)
	procGetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)

	return syscall.UTF16ToString(buf)
}

func (bw *BuffWindow) isChecked(hwnd windows.Handle) bool {
	ret, _, _ := procSendMessage.Call(
		uintptr(hwnd),
		0x00F0, // BM_GETCHECK
		0, 0,
	)
	return ret == 0x0001
}

func (bw *BuffWindow) showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)

	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(
		uintptr(bw.hwnd),
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		0x00000040,
	)
}

func (bw *BuffWindow) Show() {
	bw.visible = true
	procShowWindow.Call(uintptr(bw.hwnd), SW_SHOW)
	procSetWindowPos.Call(
		uintptr(bw.hwnd),
		HWND_TOPMOST,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|0x0040,
	)
	bw.refreshLists()

	// Set focus to first edit control
	procSetFocus := user32.NewProc("SetFocus")
	procSetFocus.Call(uintptr(bw.editID))
}

func (bw *BuffWindow) Hide() {
	bw.visible = false
	procShowWindow.Call(uintptr(bw.hwnd), SW_HIDE)
}

func (bw *BuffWindow) IsVisible() bool {
	return bw.visible
}

func (bw *BuffWindow) Toggle() {
	if bw.visible {
		bw.Hide()
	} else {
		bw.Show()
	}
}
