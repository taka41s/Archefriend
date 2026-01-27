package gui

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type OverlayWindow struct {
	hwnd      windows.Handle
	className *uint16
	lines     []string
	visible   bool
	alpha     byte
	width     int
	height    int
	gameHwnd  uintptr
}

func NewOverlayWindow(width, height int) (*OverlayWindow, error) {
	w := &OverlayWindow{
		visible: true,
		alpha:   200,
		width:   width,
		height:  height,
	}

	className, _ := syscall.UTF16PtrFromString("ArcheFriendOverlay")
	w.className = className

	hInstance, _, _ := procGetModuleHandle.Call(0)

	wc := WNDCLASSEX{
		Size:      uint32(unsafe.Sizeof(WNDCLASSEX{})),
		WndProc:   syscall.NewCallback(w.wndProc),
		Instance:  windows.Handle(hInstance),
		ClassName: className,
	}

	ret, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		return nil, fmt.Errorf("failed to register window class")
	}

	windowName, _ := syscall.UTF16PtrFromString("ArcheFriend")

	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_POPUP,
		10, 10, // x, y
		uintptr(width), uintptr(height),
		0, 0,
		hInstance,
		0,
	)

	if hwnd == 0 {
		return nil, fmt.Errorf("failed to create window")
	}

	w.hwnd = windows.Handle(hwnd)

	// Configure transparency
	procSetLayeredWindowAttributes.Call(
		hwnd,
		0,
		uintptr(w.alpha),
		LWA_ALPHA,
	)

	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)

	return w, nil
}

func (w *OverlayWindow) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

		// Clear drawing area with black
		hBrush, _, _ := procCreateSolidBrush.Call(0x00000000)

		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&ps.RcPaint)), hBrush)
		procDeleteObject.Call(hBrush)

		// Create font
		lf := LOGFONT{
			Height: 16,
			Weight: 400,
		}
		copy(lf.FaceName[:], syscall.StringToUTF16("Consolas"))
		hFont, _, _ := procCreateFontIndirect.Call(uintptr(unsafe.Pointer(&lf)))
		procSelectObject.Call(hdc, hFont)

		// Transparent background
		procSetBkMode.Call(hdc, TRANSPARENT)
		procSetTextColor.Call(hdc, 0x00FFFFFF)

		// Draw lines
		if len(w.lines) > 0 {
			y := int32(10)
			for _, line := range w.lines {
				// Convert to UTF-16
				utf16Text := syscall.StringToUTF16(line)
				// Don't include null terminator in count
				textLen := len(utf16Text) - 1
				if textLen > 0 {
					procTextOut.Call(hdc, 10, uintptr(y), uintptr(unsafe.Pointer(&utf16Text[0])), uintptr(textLen))
				}
				y += 18
			}
		}

		procDeleteObject.Call(hFont)
		procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (w *OverlayWindow) SetLines(lines []string) {
	w.lines = lines
	w.Invalidate()
}

func (w *OverlayWindow) Invalidate() {
	procInvalidateRect.Call(uintptr(w.hwnd), 0, 1)
	procUpdateWindow.Call(uintptr(w.hwnd))
}

// FindGameWindow finds the ArcheAge window
func (w *OverlayWindow) FindGameWindow() {
	// Lista de possíveis títulos da janela do ArcheAge
	possibleTitles := []string{
		"ArcheAge",
		"archeage",
		"Archeage",
	}

	for _, title := range possibleTitles {
		titlePtr, _ := syscall.UTF16PtrFromString(title)
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))

		if hwnd != 0 {
			w.gameHwnd = hwnd
			return
		}
	}
}

// UpdatePosition updates the overlay position to follow the game window
func (w *OverlayWindow) UpdatePosition() {
	if w.gameHwnd == 0 {
		w.FindGameWindow()
		if w.gameHwnd == 0 {
			return
		}
	}

	var rect RECT
	ret, _, _ := procGetWindowRect.Call(w.gameHwnd, uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		// Window no longer exists, search again
		w.gameHwnd = 0
		return
	}

	// Position overlay at top of game window
	x := rect.Left + 10
	y := rect.Top + 10

	procSetWindowPos.Call(
		uintptr(w.hwnd),
		HWND_TOPMOST,
		uintptr(x),
		uintptr(y),
		uintptr(w.width),
		uintptr(w.height),
		SWP_NOACTIVATE,
	)
}

func (w *OverlayWindow) SetVisible(visible bool) {
	w.visible = visible
	if visible {
		procSetLayeredWindowAttributes.Call(
			uintptr(w.hwnd),
			0,
			uintptr(w.alpha),
			LWA_ALPHA,
		)
		procShowWindow.Call(uintptr(w.hwnd), SW_SHOW)
	} else {
		procSetLayeredWindowAttributes.Call(
			uintptr(w.hwnd),
			0,
			0, // Alpha 0 = invisível
			LWA_ALPHA,
		)
	}
	w.Invalidate()
}

func (w *OverlayWindow) GetHWND() uintptr {
	return uintptr(w.hwnd)
}

func (w *OverlayWindow) ProcessMessages() {
	var msg MSG
	// Process up to 10 messages per call to avoid blocking
	for i := 0; i < 10; i++ {
		ret, _, _ := procPeekMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
			PM_REMOVE,
		)

		if ret == 0 {
			// No more messages
			return
		}

		if msg.Message == WM_QUIT {
			return
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
