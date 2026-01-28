package gui

import (
	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazyDLL("user32.dll")
	gdi32    = windows.NewLazyDLL("gdi32.dll")
	kernel32 = windows.NewLazyDLL("kernel32.dll")

	// Window management
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProc              = user32.NewProc("DefWindowProcW")
	procRegisterClassEx            = user32.NewProc("RegisterClassExW")
	procGetMessage                 = user32.NewProc("GetMessageW")
	procPeekMessage                = user32.NewProc("PeekMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessage            = user32.NewProc("DispatchMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procFindWindowW                = user32.NewProc("FindWindowW")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetWindowLongPtr           = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr           = user32.NewProc("GetWindowLongPtrW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procGetDC                      = user32.NewProc("GetDC")
	procReleaseDC                  = user32.NewProc("ReleaseDC")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procRedrawWindow               = user32.NewProc("RedrawWindow")
	procSendMessage                = user32.NewProc("SendMessageW")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procSetWindowText              = user32.NewProc("SetWindowTextW")
	procGetWindowText              = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength        = user32.NewProc("GetWindowTextLengthW")
	procEnableWindow               = user32.NewProc("EnableWindow")

	// GDI
	procCreateFontIndirect   = gdi32.NewProc("CreateFontIndirectW")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procSetTextColor         = gdi32.NewProc("SetTextColor")
	procSetBkMode            = gdi32.NewProc("SetBkMode")
	procTextOut              = gdi32.NewProc("TextOutW")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procFillRect             = user32.NewProc("FillRect")
	procCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procBitBlt               = gdi32.NewProc("BitBlt")

	// Kernel
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

const (
	// Window styles
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_POPUP            = 0x80000000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000

	// Extended window styles
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TOOLWINDOW  = 0x00000080

	// Window positioning
	GWL_EXSTYLE    = -20
	LWA_ALPHA      = 0x00000002
	LWA_COLORKEY   = 0x00000001
	HWND_TOPMOST   = ^uintptr(0)
	SWP_NOMOVE     = 0x0002
	SWP_NOSIZE     = 0x0001
	SWP_NOACTIVATE = 0x0010

	// Show window
	SW_SHOW = 5
	SW_HIDE = 0

	// Messages
	WM_DESTROY = 0x0002
	WM_PAINT   = 0x000F
	WM_COMMAND = 0x0111
	WM_CLOSE   = 0x0010
	WM_QUIT    = 0x0012
	PM_REMOVE  = 0x0001

	// Other
	TRANSPARENT  = 1
	COLOR_WINDOW = 5

	// RedrawWindow flags
	RDW_INVALIDATE = 0x0001
	RDW_ERASE      = 0x0004
	RDW_UPDATENOW  = 0x0100

	// BitBlt raster operations
	SRCCOPY = 0x00CC0020

	// Edit control styles
	ES_LEFT       = 0x0000
	ES_AUTOHSCROLL = 0x0080
	ES_MULTILINE  = 0x0004

	// Button styles
	BS_PUSHBUTTON    = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001

	// Button notifications
	BN_CLICKED = 0
)

type WNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type MSG struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct {
		X int32
		Y int32
	}
}

type PAINTSTRUCT struct {
	Hdc         windows.Handle
	Erase       int32
	RcPaint     RECT
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type LOGFONT struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}
