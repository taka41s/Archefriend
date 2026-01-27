package overlay

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazyDLL("user32.dll")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr    = user32.NewProc("GetWindowLongPtrW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procFindWindow          = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
)

var (
	GWL_EXSTYLE     = uintptr(0xFFFFFFFFFFFFFFEC) // -20 as uintptr
)

const (
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOPMOST     = 0x00000008
	LWA_ALPHA         = 0x00000002
	LWA_COLORKEY      = 0x00000001
	HWND_TOPMOST      = ^uintptr(0)
	SWP_NOMOVE        = 0x0002
	SWP_NOSIZE        = 0x0001
	SWP_HIDEWINDOW    = 0x0080
	SWP_SHOWWINDOW    = 0x0040
	SW_HIDE           = 0
	SW_SHOW           = 5
	SW_MINIMIZE       = 6
)

// MakeTransparentOverlay torna a janela um overlay transparente
func MakeTransparentOverlay(hwnd uintptr, alpha byte, clickThrough bool) error {
	// Get current style
	ret, _, _ := procGetWindowLongPtr.Call(hwnd, GWL_EXSTYLE)
	currentStyle := ret

	// Add layered window style
	newStyle := currentStyle | WS_EX_LAYERED | WS_EX_TOPMOST
	if clickThrough {
		newStyle |= WS_EX_TRANSPARENT
	}

	// Set new style
	ret, _, err := procSetWindowLongPtr.Call(hwnd, GWL_EXSTYLE, newStyle)
	if ret == 0 {
		return err
	}

	// Set window transparency
	ret, _, err = procSetLayeredWindowAttributes.Call(
		hwnd,
		0,                  // color key
		uintptr(alpha),     // alpha value 0-255
		LWA_ALPHA,          // use alpha
	)
	if ret == 0 {
		return err
	}

	// Make topmost
	procSetWindowPos.Call(
		hwnd,
		HWND_TOPMOST,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE,
	)

	return nil
}

// SetAlpha define a transparência da janela (0-255)
func SetAlpha(hwnd uintptr, alpha byte) error {
	ret, _, err := procSetLayeredWindowAttributes.Call(
		hwnd,
		0,
		uintptr(alpha),
		LWA_ALPHA,
	)
	if ret == 0 {
		return err
	}
	return nil
}

// SetClickThrough define se a janela é clicável ou não
func SetClickThrough(hwnd uintptr, enabled bool) error {
	ret, _, _ := procGetWindowLongPtr.Call(hwnd, GWL_EXSTYLE)
	currentStyle := ret

	var newStyle uintptr
	if enabled {
		newStyle = currentStyle | WS_EX_TRANSPARENT
	} else {
		newStyle = currentStyle &^ WS_EX_TRANSPARENT
	}

	ret, _, err := procSetWindowLongPtr.Call(hwnd, GWL_EXSTYLE, newStyle)
	if ret == 0 {
		return err
	}
	return nil
}

// FindGameWindow encontra a janela do jogo
func FindGameWindow(className, windowName string) (uintptr, error) {
	var classNamePtr, windowNamePtr *uint16
	var err error

	if className != "" {
		classNamePtr, err = syscall.UTF16PtrFromString(className)
		if err != nil {
			return 0, err
		}
	}

	if windowName != "" {
		windowNamePtr, err = syscall.UTF16PtrFromString(windowName)
		if err != nil {
			return 0, err
		}
	}

	ret, _, _ := procFindWindow.Call(
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(windowNamePtr)),
	)

	return ret, nil
}

// ShowWindow mostra a janela
func ShowWindow(hwnd uintptr) {
	// Usa SetWindowPos com SWP_SHOWWINDOW para garantir que apareça
	procSetWindowPos.Call(
		hwnd,
		HWND_TOPMOST,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW,
	)
}

// HideWindow esconde a janela completamente
func HideWindow(hwnd uintptr) {
	// Usa SetWindowPos com SWP_HIDEWINDOW que é mais efetivo
	procSetWindowPos.Call(
		hwnd,
		0,
		0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_HIDEWINDOW,
	)
}

