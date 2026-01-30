package gui

import (
	"archefriend/input"
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Control IDs para AutoSpam Window
const (
	IDC_LIST_KEYS     = 2001
	IDC_EDIT_NEWCOMBO = 2002
	IDC_BUTTON_ADDKEY = 2003
	IDC_BUTTON_DELKEY = 2004
	IDC_EDIT_INTERVAL = 2005
	IDC_BUTTON_APPLY  = 2006
)

type AutoSpamWindow struct {
	hwnd         windows.Handle
	inputManager *input.Manager

	// Controls
	listKeys     windows.Handle
	editNewCombo windows.Handle
	editInterval windows.Handle
	btnAdd       windows.Handle
	btnDelete    windows.Handle
	btnApply     windows.Handle

	visible bool
	ready   chan bool
}

func NewAutoSpamWindow(inputManager *input.Manager) (*AutoSpamWindow, error) {
	asw := &AutoSpamWindow{
		inputManager: inputManager,
		ready:        make(chan bool),
	}

	// Create window in separate goroutine with dedicated OS thread
	go asw.runWindow()

	// Wait for window to be created
	<-asw.ready

	if asw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create autospam window")
	}

	return asw, nil
}

func (asw *AutoSpamWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Register window class
	className, _ := syscall.UTF16PtrFromString("ArcheFriendAutoSpamClass")
	windowName, _ := syscall.UTF16PtrFromString("AutoSpam - Configuração de Teclas")

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003,
		WndProc:    syscall.NewCallback(asw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5,
		ClassName:  className,
	}

	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		asw.ready <- true
		return
	}

	// Create window (initially hidden)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0x00CF0000, // WS_OVERLAPPEDWINDOW
		100, 100, 450, 400,
		0, 0,
		hInstance,
		0,
	)

	asw.hwnd = windows.Handle(hwnd)
	asw.createControls()
	asw.loadCurrentKeys()

	asw.ready <- true

	// Message loop
	var msg MSG
	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (asw *AutoSpamWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)

	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listboxClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	// Label: Lista de Teclas
	label, _ := syscall.UTF16PtrFromString("Teclas configuradas:")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, 10, 200, 20,
		uintptr(asw.hwnd), 0, hInstance, 0,
	)

	// ListBox: Lista de teclas
	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(listboxClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00200000, // LBS_NOTIFY
		10, 35, 410, 150,
		uintptr(asw.hwnd), IDC_LIST_KEYS, hInstance, 0,
	)
	asw.listKeys = windows.Handle(hwnd)

	// Label: Nova tecla/combo
	label, _ = syscall.UTF16PtrFromString("Nova tecla (ex: V, SHIFT+F, ALT+Q):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, 195, 250, 20,
		uintptr(asw.hwnd), 0, hInstance, 0,
	)

	// Edit: Nova combo
	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		10, 220, 300, 25,
		uintptr(asw.hwnd), IDC_EDIT_NEWCOMBO, hInstance, 0,
	)
	asw.editNewCombo = windows.Handle(hwnd)

	// Button: Adicionar
	labelBtn, _ := syscall.UTF16PtrFromString("Adicionar")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(labelBtn)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		320, 220, 100, 25,
		uintptr(asw.hwnd), IDC_BUTTON_ADDKEY, hInstance, 0,
	)
	asw.btnAdd = windows.Handle(hwnd)

	// Button: Remover selecionado
	labelBtn, _ = syscall.UTF16PtrFromString("Remover")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(labelBtn)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,
		10, 255, 100, 25,
		uintptr(asw.hwnd), IDC_BUTTON_DELKEY, hInstance, 0,
	)
	asw.btnDelete = windows.Handle(hwnd)

	// Label: Intervalo
	label, _ = syscall.UTF16PtrFromString("Intervalo (ms):")
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(label)),
		WS_CHILD|WS_VISIBLE,
		10, 295, 100, 20,
		uintptr(asw.hwnd), 0, hInstance, 0,
	)

	// Edit: Intervalo
	initialValue, _ := syscall.UTF16PtrFromString("120")
	hwnd, _, _ = procCreateWindowExW.Call(
		0x00000200, // WS_EX_CLIENTEDGE
		uintptr(unsafe.Pointer(editClass)),
		uintptr(unsafe.Pointer(initialValue)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		110, 290, 80, 25,
		uintptr(asw.hwnd), IDC_EDIT_INTERVAL, hInstance, 0,
	)
	asw.editInterval = windows.Handle(hwnd)

	// Button: Aplicar
	labelBtn, _ = syscall.UTF16PtrFromString("Aplicar")
	hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(buttonClass)),
		uintptr(unsafe.Pointer(labelBtn)),
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON,
		320, 320, 100, 35,
		uintptr(asw.hwnd), IDC_BUTTON_APPLY, hInstance, 0,
	)
	asw.btnApply = windows.Handle(hwnd)
}

func (asw *AutoSpamWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case 0x0002: // WM_DESTROY
		procPostQuitMessage.Call(0)
		return 0

	case 0x0010: // WM_CLOSE
		asw.Hide()
		return 0

	case 0x0111: // WM_COMMAND
		controlID := wParam & 0xFFFF
		switch controlID {
		case IDC_BUTTON_ADDKEY:
			asw.addKey()
		case IDC_BUTTON_DELKEY:
			asw.deleteKey()
		case IDC_BUTTON_APPLY:
			asw.applySettings()
		}
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

func (asw *AutoSpamWindow) loadCurrentKeys() {
	// Limpar lista
	procSendMessage.Call(uintptr(asw.listKeys), 0x0184, 0, 0) // LB_RESETCONTENT

	// Carregar teclas atuais
	keys := asw.inputManager.GetKeys()
	for _, combo := range keys {
		comboStr := asw.comboToString(combo)
		asw.addListBoxItem(comboStr)
	}
}

func (asw *AutoSpamWindow) comboToString(combo []uint16) string {
	keyNames := []string{}
	for _, vk := range combo {
		name := asw.vkToName(vk)
		keyNames = append(keyNames, name)
	}

	if len(keyNames) == 0 {
		return ""
	}
	if len(keyNames) == 1 {
		return keyNames[0]
	}

	result := ""
	for i, name := range keyNames {
		if i > 0 {
			result += "+"
		}
		result += name
	}
	return result
}

func (asw *AutoSpamWindow) vkToName(vk uint16) string {
	names := map[uint16]string{
		input.VK_V:     "V",
		input.VK_F:     "F",
		input.VK_SHIFT: "SHIFT",
		input.VK_LSHIFT: "SHIFT",
		input.VK_CONTROL: "CTRL",
		input.VK_LCONTROL: "CTRL",
		input.VK_ALT: "ALT",
		input.VK_LALT: "ALT",
		input.VK_SPACE: "SPACE",
		input.VK_Q: "Q",
		input.VK_E: "E",
		input.VK_R: "R",
		input.VK_1: "1",
		input.VK_2: "2",
		input.VK_3: "3",
		input.VK_4: "4",
		input.VK_5: "5",
	}

	if name, ok := names[vk]; ok {
		return name
	}
	return fmt.Sprintf("VK_%X", vk)
}

func (asw *AutoSpamWindow) addKey() {
	comboStr := asw.getWindowText(asw.editNewCombo)
	if comboStr == "" {
		asw.messageBox("Digite uma tecla ou combo!", "Erro", 0x10)
		return
	}

	// Parse a string para combo de teclas
	combo, err := input.ParseKeyString(comboStr)
	if err != nil {
		asw.messageBox(fmt.Sprintf("Erro ao parsear tecla: %v", err), "Erro", 0x10)
		return
	}

	// Adicionar à lista
	keys := asw.inputManager.GetKeys()
	keys = append(keys, combo)
	asw.inputManager.SetKeys(keys)

	// Recarregar lista
	asw.loadCurrentKeys()

	// Limpar campo
	asw.setWindowText(asw.editNewCombo, "")

	fmt.Printf("[AUTOSPAM-UI] Tecla adicionada: %s\n", comboStr)
}

func (asw *AutoSpamWindow) deleteKey() {
	// Obter índice selecionado
	idx, _, _ := procSendMessage.Call(uintptr(asw.listKeys), 0x0188, 0, 0) // LB_GETCURSEL
	if idx == 0xFFFFFFFF { // LB_ERR
		asw.messageBox("Selecione uma tecla para remover!", "Erro", 0x10)
		return
	}

	// Remover da lista
	keys := asw.inputManager.GetKeys()
	if int(idx) < len(keys) {
		keys = append(keys[:idx], keys[idx+1:]...)
		asw.inputManager.SetKeys(keys)
		asw.loadCurrentKeys()
		fmt.Printf("[AUTOSPAM-UI] Tecla removida (índice %d)\n", idx)
	}
}

func (asw *AutoSpamWindow) applySettings() {
	// Aplicar intervalo
	intervalStr := asw.getWindowText(asw.editInterval)
	if intervalStr != "" {
		interval, err := strconv.Atoi(intervalStr)
		if err == nil && interval > 0 {
			asw.inputManager.SetInterval(time.Duration(interval) * time.Millisecond)
			fmt.Printf("[AUTOSPAM-UI] Intervalo atualizado: %dms\n", interval)
		}
	}

	asw.messageBox("Configurações aplicadas com sucesso!", "Sucesso", 0x40)
}

// Helper functions
func (asw *AutoSpamWindow) getWindowText(hwnd windows.Handle) string {
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}

	buffer := make([]uint16, length+1)
	procGetWindowText.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buffer[0])),
		length+1,
	)

	return syscall.UTF16ToString(buffer)
}

func (asw *AutoSpamWindow) setWindowText(hwnd windows.Handle, text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(textPtr)))
}

func (asw *AutoSpamWindow) addListBoxItem(text string) {
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procSendMessage.Call(
		uintptr(asw.listKeys),
		0x0180, // LB_ADDSTRING
		0,
		uintptr(unsafe.Pointer(textPtr)),
	)
}

func (asw *AutoSpamWindow) messageBox(text, title string, flags uint) {
	procMessageBox := user32.NewProc("MessageBoxW")
	textPtr, _ := syscall.UTF16PtrFromString(text)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	procMessageBox.Call(
		uintptr(asw.hwnd),
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(flags),
	)
}

func (asw *AutoSpamWindow) Toggle() {
	if asw.visible {
		asw.Hide()
	} else {
		asw.Show()
	}
}

func (asw *AutoSpamWindow) Show() {
	if asw.hwnd != 0 {
		asw.loadCurrentKeys() // Recarregar teclas ao abrir
		procShowWindow.Call(uintptr(asw.hwnd), 5) // SW_SHOW
		procSetForegroundWindow.Call(uintptr(asw.hwnd))
		asw.visible = true
	}
}

func (asw *AutoSpamWindow) Hide() {
	if asw.hwnd != 0 {
		procShowWindow.Call(uintptr(asw.hwnd), 0) // SW_HIDE
		asw.visible = false
	}
}
