package gui

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Routes control IDs
const (
	IDC_ROUTES_LIST    = 4001
	IDC_ROUTES_NAME    = 4002
	IDC_ROUTES_REFRESH = 4003
	IDC_ROUTES_SAVE    = 4004
	IDC_ROUTES_FOLLOW  = 4005
	IDC_ROUTES_STOP    = 4006
	IDC_ROUTES_DELETE  = 4007
	IDC_ROUTES_RECORD  = 4008
	IDC_ROUTES_CLEAR   = 4009

	IDC_ROUTES_SAVE_SPARSE = 4010
)

// Listbox / window messages not in common.go
const (
	lbAddString    = 0x0180
	lbResetContent = 0x0184
	lbGetCurSel    = 0x0188
	lbGetText      = 0x0189
	lbGetTextLen   = 0x018A
	lbErr          = 0xFFFFFFFF

	wsVScroll = 0x00200000
	wsBorder  = 0x00800000
	lbsNotify = 0x0001
	wmTimer   = 0x0113
)

// RoutesWindow is the route CRUD: list saved routes, record, save, follow, delete.
type RoutesWindow struct {
	hwnd      windows.Handle
	list      windows.Handle
	editName  windows.Handle
	lblStatus windows.Handle
	btnRecord windows.Handle

	visible bool
	ready   chan bool

	// Callbacks into the app (all safe to call from the window thread).
	ListRoutes   func() []string
	OnSave       func(name string) error // save current recording as <name>
	OnSaveSparse func(name string) error // save a decimated (sparse) copy of the recording
	OnFollow     func(name string) error // load <name> and start following
	OnStop       func()
	OnDelete     func(name string) error
	OnToggleRec  func()      // start/stop recording
	IsRecording  func() bool // recording state
	OnClearRec   func()      // clear the current recording buffer
	StatusText   func() string
}

func NewRoutesWindow() (*RoutesWindow, error) {
	rw := &RoutesWindow{ready: make(chan bool)}
	go rw.runWindow()
	<-rw.ready
	if rw.hwnd == 0 {
		return nil, fmt.Errorf("failed to create routes window")
	}
	return rw, nil
}

func (rw *RoutesWindow) runWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("RtCfgClass")
	windowName, _ := syscall.UTF16PtrFromString("Rotas")
	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0x0003,
		WndProc:    syscall.NewCallback(rw.wndProc),
		Instance:   windows.Handle(hInstance),
		Background: 5,
		ClassName:  className,
	}
	atom, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		rw.ready <- true
		return
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPEDWINDOW,
		240, 180,
		400, 520,
		0, 0, hInstance, 0,
	)
	rw.hwnd = windows.Handle(hwnd)
	if hwnd != 0 {
		rw.createControls()
		// Live status refresh.
		procSetTimer := user32.NewProc("SetTimer")
		procSetTimer.Call(uintptr(hwnd), 1, 500, 0)
	}

	rw.ready <- true
	rw.messageLoop()
}

func (rw *RoutesWindow) messageLoop() {
	msg := &MSG{}
	procIsDialogMessage := user32.NewProc("IsDialogMessageW")
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		isDialog, _, _ := procIsDialogMessage.Call(uintptr(rw.hwnd), uintptr(unsafe.Pointer(msg)))
		if isDialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
		}
	}
}

func (rw *RoutesWindow) createControls() {
	hInstance, _, _ := procGetModuleHandle.Call(0)
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	listClass, _ := syscall.UTF16PtrFromString("LISTBOX")

	static := func(text string, x, y, w, h int) {
		p, _ := syscall.UTF16PtrFromString(text)
		procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(p)),
			WS_CHILD|WS_VISIBLE, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
			uintptr(rw.hwnd), 0, hInstance, 0)
	}
	button := func(text string, id, x, y, w, h int) windows.Handle {
		p, _ := syscall.UTF16PtrFromString(text)
		h2, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(p)),
			WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
			uintptr(rw.hwnd), uintptr(id), hInstance, 0)
		return windows.Handle(h2)
	}

	static("Status:", 10, 10, 60, 18)
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(mustUTF16("idle"))),
		WS_CHILD|WS_VISIBLE, 70, 10, 310, 18, uintptr(rw.hwnd), 0, hInstance, 0)
	rw.lblStatus = windows.Handle(h)

	static("Rotas salvas:", 10, 38, 200, 18)
	h, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(listClass)), 0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|wsVScroll|wsBorder|lbsNotify,
		10, 58, 365, 200, uintptr(rw.hwnd), IDC_ROUTES_LIST, hInstance, 0)
	rw.list = windows.Handle(h)

	button("Atualizar", IDC_ROUTES_REFRESH, 10, 266, 120, 28)
	button("Seguir", IDC_ROUTES_FOLLOW, 135, 266, 115, 28)
	button("Deletar", IDC_ROUTES_DELETE, 255, 266, 120, 28)

	static("Nome:", 10, 306, 50, 20)
	h, _, _ = procCreateWindowExW.Call(0x00000200, uintptr(unsafe.Pointer(editClass)), 0,
		WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL,
		65, 303, 310, 24, uintptr(rw.hwnd), IDC_ROUTES_NAME, hInstance, 0)
	rw.editName = windows.Handle(h)

	rw.btnRecord = button("Gravar", IDC_ROUTES_RECORD, 10, 340, 85, 30)
	button("Salvar gravacao", IDC_ROUTES_SAVE, 100, 340, 130, 30)
	button("Salvar esparso", IDC_ROUTES_SAVE_SPARSE, 235, 340, 140, 30)

	button("Limpar", IDC_ROUTES_CLEAR, 10, 382, 120, 30)

	button("Parar de seguir", IDC_ROUTES_STOP, 10, 424, 365, 30)

	rw.refreshList()
}

func (rw *RoutesWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		cmdID := wParam & 0xFFFF
		if (wParam>>16)&0xFFFF == BN_CLICKED {
			switch cmdID {
			case IDC_ROUTES_REFRESH:
				rw.refreshList()
			case IDC_ROUTES_RECORD:
				if rw.OnToggleRec != nil {
					rw.OnToggleRec()
				}
				rw.updateStatus()
			case IDC_ROUTES_CLEAR:
				if rw.OnClearRec != nil {
					rw.OnClearRec()
				}
				rw.updateStatus()
			case IDC_ROUTES_SAVE:
				rw.onSave()
			case IDC_ROUTES_SAVE_SPARSE:
				rw.onSaveSparse()
			case IDC_ROUTES_FOLLOW:
				rw.onFollow()
			case IDC_ROUTES_STOP:
				if rw.OnStop != nil {
					rw.OnStop()
				}
				rw.updateStatus()
			case IDC_ROUTES_DELETE:
				rw.onDelete()
			}
		}
	case wmTimer:
		rw.updateStatus()
	case WM_CLOSE:
		rw.Hide()
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (rw *RoutesWindow) refreshList() {
	procSendMessage.Call(uintptr(rw.list), lbResetContent, 0, 0)
	if rw.ListRoutes == nil {
		return
	}
	for _, name := range rw.ListRoutes() {
		p, _ := syscall.UTF16PtrFromString(name)
		procSendMessage.Call(uintptr(rw.list), lbAddString, 0, uintptr(unsafe.Pointer(p)))
	}
}

// selectedRoute returns the highlighted route name, or "".
func (rw *RoutesWindow) selectedRoute() string {
	// SendMessage returns an LRESULT (pointer-sized signed); LB_ERR = -1. Test
	// via int() so it's correct on both 386 and amd64 (a bare 0xFFFFFFFF const
	// does NOT match the 64-bit all-ones LB_ERR).
	idx, _, _ := procSendMessage.Call(uintptr(rw.list), lbGetCurSel, 0, 0)
	if int(idx) < 0 { // LB_ERR / nothing selected
		return ""
	}
	n, _, _ := procSendMessage.Call(uintptr(rw.list), lbGetTextLen, idx, 0)
	if int(n) <= 0 { // empty string or LB_ERR
		return ""
	}
	buf := make([]uint16, int(n)+1)
	procSendMessage.Call(uintptr(rw.list), lbGetText, idx, uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func (rw *RoutesWindow) onSave() {
	name := strings.TrimSpace(rw.getEditText(rw.editName))
	if name == "" {
		name = rw.selectedRoute()
	}
	if name == "" {
		rw.showMessage("Salvar", "Digite um nome (campo Nome) para a rota.")
		return
	}
	if rw.OnSave == nil {
		return
	}
	if err := rw.OnSave(name); err != nil {
		rw.showMessage("Erro", fmt.Sprintf("Falha ao salvar: %v", err))
		return
	}
	rw.refreshList()
	rw.showMessage("Salvo", fmt.Sprintf("Rota '%s' salva.", name))
}

func (rw *RoutesWindow) onSaveSparse() {
	name := strings.TrimSpace(rw.getEditText(rw.editName))
	if name == "" {
		name = rw.selectedRoute()
	}
	if name == "" {
		rw.showMessage("Salvar esparso", "Digite um nome (campo Nome) para a rota.")
		return
	}
	if rw.OnSaveSparse == nil {
		return
	}
	if err := rw.OnSaveSparse(name); err != nil {
		rw.showMessage("Erro", fmt.Sprintf("Falha ao salvar esparso: %v", err))
		return
	}
	rw.refreshList()
	rw.showMessage("Salvo", fmt.Sprintf("Rota esparsa '%s' salva.", name))
}

func (rw *RoutesWindow) onFollow() {
	name := rw.selectedRoute()
	if name == "" {
		rw.showMessage("Seguir", "Selecione uma rota na lista.")
		return
	}
	if rw.OnFollow == nil {
		return
	}
	if err := rw.OnFollow(name); err != nil {
		rw.showMessage("Erro", fmt.Sprintf("Falha ao seguir: %v", err))
		return
	}
	rw.updateStatus()
}

func (rw *RoutesWindow) onDelete() {
	name := rw.selectedRoute()
	if name == "" {
		rw.showMessage("Deletar", "Selecione uma rota na lista.")
		return
	}
	if rw.OnDelete == nil {
		return
	}
	if err := rw.OnDelete(name); err != nil {
		rw.showMessage("Erro", fmt.Sprintf("Falha ao deletar: %v", err))
		return
	}
	rw.refreshList()
}

func (rw *RoutesWindow) updateStatus() {
	if rw.StatusText != nil {
		rw.setText(rw.lblStatus, rw.StatusText())
	}
	if rw.btnRecord != 0 && rw.IsRecording != nil {
		if rw.IsRecording() {
			rw.setText(rw.btnRecord, "Parar gravacao")
		} else {
			rw.setText(rw.btnRecord, "Gravar")
		}
	}
}

func (rw *RoutesWindow) getEditText(hwnd windows.Handle) string {
	length, _, _ := procGetWindowTextLength.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	procGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func (rw *RoutesWindow) setText(hwnd windows.Handle, text string) {
	p, _ := syscall.UTF16PtrFromString(text)
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(p)))
}

func (rw *RoutesWindow) showMessage(title, message string) {
	procMessageBox := user32.NewProc("MessageBoxW")
	procMessageBox.Call(uintptr(rw.hwnd),
		uintptr(unsafe.Pointer(mustUTF16(message))), uintptr(unsafe.Pointer(mustUTF16(title))), 0x00000040)
}

func mustUTF16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func (rw *RoutesWindow) Show() {
	rw.visible = true
	rw.refreshList()
	procShowWindow.Call(uintptr(rw.hwnd), SW_SHOW)
	procSetWindowPos.Call(uintptr(rw.hwnd), HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|0x0040)
}

func (rw *RoutesWindow) Hide() {
	rw.visible = false
	procShowWindow.Call(uintptr(rw.hwnd), SW_HIDE)
}

func (rw *RoutesWindow) IsVisible() bool { return rw.visible }

func (rw *RoutesWindow) Toggle() {
	if rw.visible {
		rw.Hide()
	} else {
		rw.Show()
	}
}
