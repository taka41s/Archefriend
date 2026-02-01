package esp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ============================================================================
// Windows API
// ============================================================================

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procReadProcessMemory        = kernel32.NewProc("ReadProcessMemory")
	procWriteProcessMemory       = kernel32.NewProc("WriteProcessMemory")
	procVirtualAllocEx           = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx            = kernel32.NewProc("VirtualFreeEx")
	procVirtualProtectEx         = kernel32.NewProc("VirtualProtectEx")
	procCreateRemoteThread       = kernel32.NewProc("CreateRemoteThread")
	procWaitForSingleObject      = kernel32.NewProc("WaitForSingleObject")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procPeekMessageW               = user32.NewProc("PeekMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procGetDC                      = user32.NewProc("GetDC")
	procReleaseDC                  = user32.NewProc("ReleaseDC")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procFillRect                   = user32.NewProc("FillRect")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procSetCursorPos               = user32.NewProc("SetCursorPos")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procScreenToClient             = user32.NewProc("ScreenToClient")
	procSetWindowLongW             = user32.NewProc("SetWindowLongW")
	procGetWindowLongW             = user32.NewProc("GetWindowLongW")

	procCreatePen          = gdi32.NewProc("CreatePen")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procMoveToEx           = gdi32.NewProc("MoveToEx")
	procLineTo             = gdi32.NewProc("LineTo")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procTextOutW           = gdi32.NewProc("TextOutW")
	procGetStockObject     = gdi32.NewProc("GetStockObject")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateFont         = gdi32.NewProc("CreateFontW")
	procEllipse            = gdi32.NewProc("Ellipse")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteDC           = gdi32.NewProc("DeleteDC")

	procDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
)

// ============================================================================
// Constants
// ============================================================================

const (
	PROCESS_ALL_ACCESS     = 0x1F0FFF
	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	MEM_RELEASE            = 0x8000
	PAGE_EXECUTE_READWRITE = 0x40
	MAX_PATH               = 260

	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_NOACTIVATE  = 0x08000000
	WS_POPUP          = 0x80000000

	LWA_COLORKEY = 0x00000001
	SW_SHOW      = 5
	SW_HIDE      = 0
	PM_REMOVE    = 0x0001

	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202

	TRANSPARENT_COLOR = 0x00FF00FF

	PS_SOLID    = 0
	TRANSPARENT = 1
	NULL_BRUSH  = 5
	FW_BOLD     = 700
	SRCCOPY     = 0x00CC0020
)

// Distance colors
const (
	COLOR_BLACK  = 0x00000000
	COLOR_WHITE  = 0x00FFFFFF
	COLOR_GREEN  = 0x0000FF00 // 0-25m
	COLOR_BLUE   = 0x00FF0000 // 25-30m
	COLOR_YELLOW = 0x0000FFFF // 30-40m
	COLOR_RED    = 0x000000FF // 40m+
)

// Game Offsets - WorldToScreen
const (
	gEnvPtr    = 0x39EA2074
	targetBase = 0x3AB81E98

	// Player target offsets
	playerTargetPosX = 0x6A4
	playerTargetPosY = 0x6AC
	playerTargetPosZ = 0x6A8

	// Target type flag: 1 = mob/NPC, 0 = player
	targetTypeFlag = 0x028

	// Current target ID (0 = no target, != 0 = target selected)
	targetIDOffset = 0x008

	// Target pointer (to check if target is selected)
	PTR_ENEMY_TARGET uintptr = 0x19EBF4

	INPUT_X_OFFSET  = 0x100
	INPUT_Y_OFFSET  = 0x104
	INPUT_Z_OFFSET  = 0x108
	OUTPUT_X_OFFSET = 0x10C
	OUTPUT_Y_OFFSET = 0x110
	OUTPUT_Z_OFFSET = 0x114
	ALLOC_SIZE      = 0x200
)

// Game Offsets - Player Position
const (
	PTR_LOCALPLAYER   uintptr = 0xE9DC54
	OFF_PLAYER_ENTITY uint32  = 0x10
	OFF_POS_X         uint32  = 0x830
	OFF_POS_Z         uint32  = 0x834
	OFF_POS_Y         uint32  = 0x838
)

// ============================================================================
// Structs
// ============================================================================

type WNDCLASSEXW struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

type MSG struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             struct{ X, Y int32 }
}

type RECT struct{ Left, Top, Right, Bottom int32 }
type MARGINS struct{ Left, Right, Top, Bottom int32 }
type POINT struct{ X, Y int32 }

// ESPStyle defines ESP visual style
type ESPStyle int

const (
	StyleCorners   ESPStyle = iota // Cantos estilo CS2/Valorant
	StyleFullBox                   // Box completo
	StyleCircle                    // Circulo ao redor
	StyleBrackets                  // Colchetes [ ]
)

// ============================================================================
// ESP Manager
// ============================================================================

var (
	foundGameHwnd uintptr
	targetPID     uint32
)

// Manager gerencia o ESP overlay
type Manager struct {
	processHandle uintptr
	x2game        uintptr
	shellcodeBase uintptr
	overlayHwnd   uintptr
	screenW       int32
	screenH       int32
	enabled       bool
	running       bool
	stopChan      chan bool
	hdc           uintptr
	font          uintptr

	// Double buffering
	backDC     uintptr
	backBitmap uintptr
	oldBitmap  uintptr

	// Configuracoes visuais
	Style        ESPStyle
	CornerLength int32
	BoxThickness int32

	// All Entities ESP (separate module)
	allEntitiesManager *AllEntitiesManager

	// Mutex for WorldToScreen (prevents race condition between ESP and Aimbot)
	wtsMutex sync.Mutex

	// Aimbot
	aimbotEnabled   bool
	aimbotRunning   bool
	aimbotStopChan  chan bool
	aimbotKeys      []int // Keys that activate aimbot when pressed
	lastTargetX     int32
	lastTargetY     int32

	// Checkbox UI
	checkboxPlayerX int32
	checkboxPlayerY int32
	checkboxNPCX    int32
	checkboxNPCY    int32
	checkboxMateX   int32
	checkboxMateY   int32
	checkboxSize    int32
	uiVisible       bool
	lastMouseState  bool

	// Range filter UI
	rangeDecBtnX   int32
	rangeDecBtnY   int32
	rangeIncBtnX   int32
	rangeIncBtnY   int32
	rangeBtnSize   int32
}

// EntityInfo stores entity information
type EntityInfo struct {
	Address  uint32
	VTable   uint32
	EntityID uint32
	Name     string
	PosX     float32
	PosY     float32
	PosZ     float32
	HP       uint32
	MaxHP    uint32
	IsPlayer bool
	IsNPC    bool
	IsMate   bool
	Distance float32
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func enumWindowsCallback(hwnd uintptr, lParam uintptr) uintptr {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == targetPID {
		buf := make([]uint16, 256)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		title := syscall.UTF16ToString(buf)
		if strings.Contains(title, "ArcheAge") && strings.Contains(title, "DX11") {
			foundGameHwnd = hwnd
			return 0
		}
	}
	return 1
}

// NewManager creates a new ESP manager
func NewManager(processHandle uintptr, pid uint32, x2game uintptr) (*Manager, error) {
	m := &Manager{
		processHandle:  processHandle,
		x2game:         x2game,
		enabled:        true,  // Target ESP enabled by default
		running:        false,
		stopChan:       make(chan bool),
		aimbotStopChan: make(chan bool),
		Style:          StyleCorners,
		CornerLength:   12,
		BoxThickness:   2,
		checkboxSize:   16,
		uiVisible:      true,
		lastMouseState: false,
		rangeBtnSize:   20,
	}

	// Create separate module for All Entities ESP
	m.allEntitiesManager = NewAllEntitiesManager(processHandle, x2game, m)

	// Allocate shellcode
	if err := m.allocateShellcode(); err != nil {
		return nil, err
	}

	// Find game window
	foundGameHwnd = 0
	targetPID = pid
	procEnumWindows.Call(syscall.NewCallback(enumWindowsCallback), 0)

	var gameRect RECT
	if foundGameHwnd != 0 {
		procGetWindowRect.Call(foundGameHwnd, uintptr(unsafe.Pointer(&gameRect)))
	}
	if foundGameHwnd == 0 || gameRect.Right-gameRect.Left == 0 {
		gameRect = RECT{0, 0, 1920, 1080}
	}
	m.screenW = gameRect.Right - gameRect.Left
	m.screenH = gameRect.Bottom - gameRect.Top

	// Create overlay with WS_EX_NOACTIVATE to prevent focus stealing
	className := syscall.StringToUTF16Ptr("ArcheFriendESP")
	wc := WNDCLASSEXW{
		Size:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		WndProc:   syscall.NewCallback(wndProc),
		ClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TRANSPARENT|WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE,
		uintptr(unsafe.Pointer(className)),
		0,
		WS_POPUP,
		uintptr(gameRect.Left), uintptr(gameRect.Top),
		uintptr(m.screenW), uintptr(m.screenH),
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("CreateWindowExW failed")
	}
	m.overlayHwnd = hwnd

	procSetLayeredWindowAttributes.Call(hwnd, TRANSPARENT_COLOR, 0, LWA_COLORKEY)
	margins := MARGINS{-1, -1, -1, -1}
	procDwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&margins)))

	m.hdc, _, _ = procGetDC.Call(m.overlayHwnd)

	// Create back buffer for double buffering (elimina flickering)
	m.backDC, _, _ = procCreateCompatibleDC.Call(m.hdc)
	m.backBitmap, _, _ = procCreateCompatibleBitmap.Call(m.hdc, uintptr(m.screenW), uintptr(m.screenH))
	m.oldBitmap, _, _ = procSelectObject.Call(m.backDC, m.backBitmap)

	// Create modern font
	fontName := syscall.StringToUTF16Ptr("Segoe UI")
	m.font, _, _ = procCreateFont.Call(
		uintptr(14), 0, 0, 0, FW_BOLD, 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(fontName)),
	)

	return m, nil
}

func (m *Manager) allocateShellcode() error {
	addr, _, _ := procVirtualAllocEx.Call(m.processHandle, 0, ALLOC_SIZE, MEM_COMMIT|MEM_RESERVE, PAGE_EXECUTE_READWRITE)
	if addr == 0 {
		return fmt.Errorf("VirtualAllocEx failed")
	}
	m.shellcodeBase = addr

	shellcode := []byte{
		0xA1, 0, 0, 0, 0,
		0x8B, 0x00,
		0x8B, 0x48, 0x0C,
		0x8B, 0x01,
		0x8B, 0x80, 0x68, 0x01, 0x00, 0x00,
		0x68, 0, 0, 0, 0,
		0x68, 0, 0, 0, 0,
		0x68, 0, 0, 0, 0,
		0xFF, 0x35, 0, 0, 0, 0,
		0xFF, 0x35, 0, 0, 0, 0,
		0xFF, 0x35, 0, 0, 0, 0,
		0xFF, 0xD0,
		0xC3,
	}

	binary.LittleEndian.PutUint32(shellcode[1:5], uint32(gEnvPtr))
	binary.LittleEndian.PutUint32(shellcode[19:23], uint32(addr+OUTPUT_Z_OFFSET))
	binary.LittleEndian.PutUint32(shellcode[24:28], uint32(addr+OUTPUT_Y_OFFSET))
	binary.LittleEndian.PutUint32(shellcode[29:33], uint32(addr+OUTPUT_X_OFFSET))
	binary.LittleEndian.PutUint32(shellcode[35:39], uint32(addr+INPUT_Z_OFFSET))
	binary.LittleEndian.PutUint32(shellcode[41:45], uint32(addr+INPUT_Y_OFFSET))
	binary.LittleEndian.PutUint32(shellcode[47:51], uint32(addr+INPUT_X_OFFSET))

	var written uintptr
	procWriteProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&shellcode[0])), uintptr(len(shellcode)), uintptr(unsafe.Pointer(&written)))
	return nil
}

// Close libera recursos
func (m *Manager) Close() {
	m.Stop()
	// Stop All Entities ESP if running
	if m.allEntitiesManager != nil {
		m.allEntitiesManager.Stop()
	}
	if m.font != 0 {
		procDeleteObject.Call(m.font)
	}
	// Clean up back buffer
	if m.backDC != 0 {
		if m.oldBitmap != 0 {
			procSelectObject.Call(m.backDC, m.oldBitmap)
		}
		if m.backBitmap != 0 {
			procDeleteObject.Call(m.backBitmap)
		}
		procDeleteDC.Call(m.backDC)
	}
	if m.hdc != 0 {
		procReleaseDC.Call(m.overlayHwnd, m.hdc)
	}
	if m.overlayHwnd != 0 {
		procDestroyWindow.Call(m.overlayHwnd)
	}
	if m.shellcodeBase != 0 {
		procVirtualFreeEx.Call(m.processHandle, m.shellcodeBase, 0, MEM_RELEASE)
	}
}

func (m *Manager) readU32(addr uintptr) uint32 {
	var buf [4]byte
	var read uintptr
	procReadProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&buf[0])), 4, uintptr(unsafe.Pointer(&read)))
	return binary.LittleEndian.Uint32(buf[:])
}

func (m *Manager) readFloat32(addr uintptr) float32 {
	var buf [4]byte
	var read uintptr
	procReadProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&buf[0])), 4, uintptr(unsafe.Pointer(&read)))
	return math.Float32frombits(binary.LittleEndian.Uint32(buf[:]))
}

func (m *Manager) writeFloat32(addr uintptr, val float32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, math.Float32bits(val))
	var written uintptr
	procWriteProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&buf[0])), 4, uintptr(unsafe.Pointer(&written)))
}

// GetPlayerPosition returns local player position
func (m *Manager) GetPlayerPosition() (float32, float32, float32, bool) {
	ptr1 := m.readU32(m.x2game + PTR_LOCALPLAYER)
	if ptr1 == 0 {
		return 0, 0, 0, false
	}

	playerAddr := m.readU32(uintptr(ptr1) + uintptr(OFF_PLAYER_ENTITY))
	if playerAddr == 0 {
		return 0, 0, 0, false
	}

	x := m.readFloat32(uintptr(playerAddr) + uintptr(OFF_POS_X))
	z := m.readFloat32(uintptr(playerAddr) + uintptr(OFF_POS_Z))
	y := m.readFloat32(uintptr(playerAddr) + uintptr(OFF_POS_Y))

	return x, y, z, true
}

// WorldToScreen converts world coordinates to screen
// Returns screenX, screenY (percentage 0-100) and screenZ (depth, >= 1.0 = behind camera)
func (m *Manager) WorldToScreen(x, y, z float32) (float32, float32, float32) {
	// Mutex to prevent race condition between ESP and Aimbot
	m.wtsMutex.Lock()
	defer m.wtsMutex.Unlock()

	m.writeFloat32(m.shellcodeBase+INPUT_X_OFFSET, x)
	m.writeFloat32(m.shellcodeBase+INPUT_Y_OFFSET, y)
	m.writeFloat32(m.shellcodeBase+INPUT_Z_OFFSET, z)

	var threadID uint32
	th, _, _ := procCreateRemoteThread.Call(m.processHandle, 0, 0, m.shellcodeBase, 0, 0, uintptr(unsafe.Pointer(&threadID)))
	if th == 0 {
		return 0, 0, -1
	}
	procWaitForSingleObject.Call(th, 5000)
	procCloseHandle.Call(th)

	screenX := m.readFloat32(m.shellcodeBase + OUTPUT_X_OFFSET)
	screenY := m.readFloat32(m.shellcodeBase + OUTPUT_Y_OFFSET)
	screenZ := m.readFloat32(m.shellcodeBase + OUTPUT_Z_OFFSET)
	return screenX, screenY, screenZ
}

// HasTarget checks if a target is selected
func (m *Manager) HasTarget() bool {
	targetPtr := m.readU32(m.x2game + PTR_ENEMY_TARGET)
	return targetPtr != 0
}

// GetTarget returns current target position (player targets only)
func (m *Manager) GetTarget() (float32, float32, float32, bool) {
	// Check if has target using ID
	if !m.HasTargetByID() {
		return 0, 0, 0, false
	}

	// Player target coords (if mob, these coords will be zero)
	x := m.readFloat32(targetBase + playerTargetPosX)
	y := m.readFloat32(targetBase + playerTargetPosY)
	z := m.readFloat32(targetBase + playerTargetPosZ)

	if x == 0 && y == 0 && z == 0 {
		return 0, 0, 0, false
	}
	return x, y, z, true
}

// DebugTargetInfo prints debug info about current target
func (m *Manager) DebugTargetInfo() {
	targetID := m.readU32(targetBase + targetIDOffset)
	flag := m.readU32(targetBase + targetTypeFlag)

	playerX := m.readFloat32(targetBase + playerTargetPosX)
	playerY := m.readFloat32(targetBase + playerTargetPosY)
	playerZ := m.readFloat32(targetBase + playerTargetPosZ)

	hasTarget := "NO"
	if targetID != 0 {
		hasTarget = "YES"
	}

	targetType := "PLAYER"
	if flag == 1 {
		targetType = "MOB (ignorado)"
	}

	fmt.Printf("[DEBUG] TargetID: 0x%X (%s) | Type: %s (flag=%d)\n", targetID, hasTarget, targetType, flag)
	fmt.Printf("[DEBUG] Player coords: (%.1f, %.1f, %.1f)\n", playerX, playerY, playerZ)
}

// HasTargetByID checks if has target using ID
func (m *Manager) HasTargetByID() bool {
	targetID := m.readU32(targetBase + targetIDOffset)
	return targetID != 0
}

// HasPlayerTarget checks if target is a player
func (m *Manager) HasPlayerTarget() bool {
	if !m.HasTargetByID() {
		return false
	}
	flag := m.readU32(targetBase + targetTypeFlag)
	return flag == 0
}

// CalculateDistance calculates 3D distance between two points
func CalculateDistance(x1, y1, z1, x2, y2, z2 float32) float32 {
	dx := x2 - x1
	dy := y2 - y1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// GetColorByDistance returns color based on distance
func GetColorByDistance(distance float32) uintptr {
	if distance <= 25 {
		return COLOR_GREEN
	} else if distance <= 30 {
		return COLOR_BLUE
	} else if distance <= 40 {
		return COLOR_YELLOW
	}
	return COLOR_RED
}

// Enable ativa o ESP
func (m *Manager) Enable() {
	if m.enabled {
		return
	}
	m.enabled = true
	procShowWindow.Call(m.overlayHwnd, SW_SHOW)
	procUpdateWindow.Call(m.overlayHwnd)

	if !m.running {
		m.running = true
		go m.renderLoop()
		go m.aimbotLoop() // Aimbot in separate goroutine
	}
}

// Disable desativa o ESP
func (m *Manager) Disable() {
	if !m.enabled {
		return
	}
	m.enabled = false
	m.clearOverlay()
	procShowWindow.Call(m.overlayHwnd, SW_HIDE)
}

// Toggle toggles ESP state
func (m *Manager) Toggle() bool {
	if m.enabled {
		m.Disable()
	} else {
		m.Enable()
	}
	return m.enabled
}

// IsEnabled returns if ESP is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// Stop stops the render loop
func (m *Manager) Stop() {
	if m.running {
		m.running = false
		select {
		case m.stopChan <- true:
		default:
		}
	}
}

func (m *Manager) clearOverlay() {
	brush, _, _ := procCreateSolidBrush.Call(TRANSPARENT_COLOR)
	rect := RECT{0, 0, m.screenW, m.screenH}
	procFillRect.Call(m.hdc, uintptr(unsafe.Pointer(&rect)), brush)
	procDeleteObject.Call(brush)
}

func (m *Manager) clearBackBuffer() {
	brush, _, _ := procCreateSolidBrush.Call(TRANSPARENT_COLOR)
	rect := RECT{0, 0, m.screenW, m.screenH}
	procFillRect.Call(m.backDC, uintptr(unsafe.Pointer(&rect)), brush)
	procDeleteObject.Call(brush)
}

func (m *Manager) present() {
	// Copy back buffer to screen at once (eliminates flickering)
	procBitBlt.Call(m.hdc, 0, 0, uintptr(m.screenW), uintptr(m.screenH),
		m.backDC, 0, 0, SRCCOPY)
}

// ============================================================================
// Drawing Functions
// ============================================================================

func (m *Manager) drawLine(x1, y1, x2, y2 int32, color uintptr, thickness int) {
	pen, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thickness), color)
	oldPen, _, _ := procSelectObject.Call(m.backDC, pen)
	procMoveToEx.Call(m.backDC, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(m.backDC, uintptr(x2), uintptr(y2))
	procSelectObject.Call(m.backDC, oldPen)
	procDeleteObject.Call(pen)
}

func (m *Manager) drawOutlinedLine(x1, y1, x2, y2 int32, color uintptr, thickness int) {
	// Outline preto
	m.drawLine(x1, y1, x2, y2, COLOR_BLACK, thickness+2)
	// Linha principal
	m.drawLine(x1, y1, x2, y2, color, thickness)
}

func (m *Manager) drawCorners(x, y, w, h int32, color uintptr) {
	cornerLen := m.CornerLength
	thick := int(m.BoxThickness)

	// Top-left
	m.drawOutlinedLine(x, y, x+cornerLen, y, color, thick)
	m.drawOutlinedLine(x, y, x, y+cornerLen, color, thick)

	// Top-right
	m.drawOutlinedLine(x+w-cornerLen, y, x+w, y, color, thick)
	m.drawOutlinedLine(x+w, y, x+w, y+cornerLen, color, thick)

	// Bottom-left
	m.drawOutlinedLine(x, y+h-cornerLen, x, y+h, color, thick)
	m.drawOutlinedLine(x, y+h, x+cornerLen, y+h, color, thick)

	// Bottom-right
	m.drawOutlinedLine(x+w, y+h-cornerLen, x+w, y+h, color, thick)
	m.drawOutlinedLine(x+w-cornerLen, y+h, x+w, y+h, color, thick)
}

func (m *Manager) drawFullBox(x, y, w, h int32, color uintptr) {
	thick := int(m.BoxThickness)

	// Outline
	m.drawLine(x-1, y-1, x+w+1, y-1, COLOR_BLACK, thick+2)
	m.drawLine(x-1, y+h+1, x+w+1, y+h+1, COLOR_BLACK, thick+2)
	m.drawLine(x-1, y-1, x-1, y+h+1, COLOR_BLACK, thick+2)
	m.drawLine(x+w+1, y-1, x+w+1, y+h+1, COLOR_BLACK, thick+2)

	// Main box
	m.drawLine(x, y, x+w, y, color, thick)
	m.drawLine(x, y+h, x+w, y+h, color, thick)
	m.drawLine(x, y, x, y+h, color, thick)
	m.drawLine(x+w, y, x+w, y+h, color, thick)
}

func (m *Manager) drawBrackets(x, y, w, h int32, color uintptr) {
	thick := int(m.BoxThickness)
	bracketLen := h / 3

	// Left bracket [
	m.drawOutlinedLine(x, y, x+8, y, color, thick)
	m.drawOutlinedLine(x, y, x, y+bracketLen, color, thick)
	m.drawOutlinedLine(x, y+h, x+8, y+h, color, thick)
	m.drawOutlinedLine(x, y+h-bracketLen, x, y+h, color, thick)

	// Right bracket ]
	m.drawOutlinedLine(x+w-8, y, x+w, y, color, thick)
	m.drawOutlinedLine(x+w, y, x+w, y+bracketLen, color, thick)
	m.drawOutlinedLine(x+w-8, y+h, x+w, y+h, color, thick)
	m.drawOutlinedLine(x+w, y+h-bracketLen, x+w, y+h, color, thick)
}

func (m *Manager) drawCircle(centerX, centerY, radius int32, color uintptr) {
	thick := int(m.BoxThickness)

	// Outline
	pen1, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thick+2), COLOR_BLACK)
	oldPen1, _, _ := procSelectObject.Call(m.backDC, pen1)
	nullBrush, _, _ := procGetStockObject.Call(NULL_BRUSH)
	oldBrush, _, _ := procSelectObject.Call(m.backDC, nullBrush)
	procEllipse.Call(m.backDC, uintptr(centerX-radius-1), uintptr(centerY-radius-1), uintptr(centerX+radius+1), uintptr(centerY+radius+1))
	procSelectObject.Call(m.backDC, oldPen1)
	procDeleteObject.Call(pen1)

	// Main circle
	pen2, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thick), color)
	oldPen2, _, _ := procSelectObject.Call(m.backDC, pen2)
	procEllipse.Call(m.backDC, uintptr(centerX-radius), uintptr(centerY-radius), uintptr(centerX+radius), uintptr(centerY+radius))
	procSelectObject.Call(m.backDC, oldPen2)
	procSelectObject.Call(m.backDC, oldBrush)
	procDeleteObject.Call(pen2)
}

func (m *Manager) drawText(x, y int32, text string, color uintptr) {
	if m.font != 0 {
		procSelectObject.Call(m.backDC, m.font)
	}

	textPtr := syscall.StringToUTF16Ptr(text)

	// Draw text without shadow
	procSetBkMode.Call(m.backDC, TRANSPARENT)
	procSetTextColor.Call(m.backDC, color)
	procTextOutW.Call(m.backDC, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(textPtr)), uintptr(len(text)))
}

func (m *Manager) renderLoop() {
	for m.running {
		select {
		case <-m.stopChan:
			return
		default:
		}

		time.Sleep(8 * time.Millisecond) // ~120 FPS for more responsive aimbot

		// Process messages
		var msg MSG
		for {
			ret, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, PM_REMOVE)
			if ret == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		// Aimbot now runs in separate goroutine (aimbotLoop)

		if !m.enabled {
			m.clearOverlay() // Clear when disabled
			continue
		}

		// Get player position
		playerX, playerY, playerZ, hasPlayer := m.GetPlayerPosition()
		if !hasPlayer {
			m.clearOverlay()
			continue
		}

		// CRITICAL: Force window to be transparent by default
		// Only make it clickable if mouse is over UI
		m.setWindowClickable(false)

		// Process mouse clicks for checkboxes
		m.processMouseInput()

		// === DOUBLE BUFFERING ===
		// 1. Limpa o back buffer
		m.clearBackBuffer()

		// Target ESP (always active when has target)
		targetX, targetY, targetZ, hasTarget := m.GetTarget()
		if hasTarget {
			// Calculate distance
			distance := CalculateDistance(playerX, playerY, playerZ, targetX, targetY, targetZ)

			// Color based on distance
			color := GetColorByDistance(distance)

			// WorldToScreen (order: X, Z, Y)
			screenX, screenY, screenZ := m.WorldToScreen(targetX, targetZ, targetY)

			// Filter target behind camera (screenZ >= 1.0 = behind)
			isInvalidZ := math.IsNaN(float64(screenZ)) || math.IsInf(float64(screenZ), 0)
			if !isInvalidZ && screenZ < 1.0 &&
				screenX >= 0 && screenX <= 100 && screenY >= 0 && screenY <= 100 {
				// Convert to pixels
				pixelX := int32(screenX * float32(m.screenW) / 100.0)
				pixelY := int32(screenY * float32(m.screenH) / 100.0)

					if pixelX > 0 && pixelX < m.screenW && pixelY > 0 && pixelY < m.screenH {
					// Store target position for aimbot
					m.lastTargetX = pixelX
					m.lastTargetY = pixelY

					// Box dimensions (small)
					boxW := int32(30)
					boxH := int32(50)
					boxX := pixelX - boxW/2
					boxY := pixelY - boxH/2

					// Draw target ESP
					switch m.Style {
					case StyleCorners:
						m.drawCorners(boxX, boxY, boxW, boxH, color)
					case StyleFullBox:
						m.drawFullBox(boxX, boxY, boxW, boxH, color)
					case StyleCircle:
						m.drawCircle(pixelX, pixelY, 25, color)
					case StyleBrackets:
						m.drawBrackets(boxX, boxY, boxW, boxH, color)
					}

					// Draw label with distance
					labelText := fmt.Sprintf("TARGET %.0fm", distance)
					m.drawText(pixelX-30, boxY-18, labelText, COLOR_RED)
				}
			}
		} else {
			m.lastTargetX = 0
			m.lastTargetY = 0
		}

		// All Entities ESP (additional, when enabled)
		// Don't render All Entities if overlay is hidden
		// to avoid race condition in WorldToScreen
		isVisible, _, _ := procIsWindowVisible.Call(m.overlayHwnd)
		if m.allEntitiesManager.IsEnabled() && isVisible != 0 {
			// Cache is updated in background goroutine (separate module)
			entities := m.allEntitiesManager.GetCachedEntities()

			// Render all entities
			renderedCount := 0
			skippedOffscreen := 0
			skippedFiltered := 0
			for _, entity := range entities {
				// Filter by type
				if entity.IsPlayer && !m.allEntitiesManager.GetShowPlayers() {
					skippedFiltered++
					continue
				}
				if entity.IsNPC && !m.allEntitiesManager.GetShowNPCs() {
					skippedFiltered++
					continue
				}
				if entity.IsMate && !m.allEntitiesManager.GetShowMates() {
					skippedFiltered++
					continue
				}

				// WorldToScreen (order: X, Z, Y)
				screenX, screenY, screenZ := m.WorldToScreen(entity.PosX, entity.PosZ, entity.PosY)

				// Filter entities behind camera (screenZ >= 1.0 = behind)
				isInvalidZ := math.IsNaN(float64(screenZ)) || math.IsInf(float64(screenZ), 0)
				if isInvalidZ || screenZ >= 1.0 {
					skippedOffscreen++
					continue
				}

				// Filter entities off screen
				if screenX < 0 || screenX > 100 || screenY < 0 || screenY > 100 {
					skippedOffscreen++
					continue
				}

				pixelX := int32(screenX * float32(m.screenW) / 100.0)
				pixelY := int32(screenY * float32(m.screenH) / 100.0)

				if pixelX <= 0 || pixelX >= m.screenW || pixelY <= 0 || pixelY >= m.screenH {
					skippedOffscreen++
					continue
				}

				renderedCount++
				color := GetColorByDistance(entity.Distance)

				// All Entities ESP: desenho minimalista (bolinha pequena)
				m.drawCircle(pixelX, pixelY, 4, color)

				// Small label with type and distance below the dot
				entityType := "NPC"
				if entity.IsPlayer {
					entityType = "P"
				} else if entity.IsMate {
					entityType = "M"
				} else {
					entityType = "N"
				}
				labelText := fmt.Sprintf("%s %.0fm", entityType, entity.Distance)
				m.drawText(pixelX-15, pixelY+8, labelText, color)

				// Nome apenas se for player ou mate
				if (entity.IsPlayer || entity.IsMate) && entity.Name != "" && len(entity.Name) < 20 {
					m.drawText(pixelX-25, pixelY-10, entity.Name, COLOR_WHITE)
				}
			}

			// Debug output (unused)
			_ = skippedFiltered
			_ = renderedCount
			_ = skippedOffscreen
		}

		// Draw filter checkboxes UI
		if m.uiVisible && m.allEntitiesManager.IsEnabled() {
			m.drawFilterUI()
		}

		// 3. Copy back buffer to screen at once (no flicker)
		m.present()
	}
}

// processMouseInput detecta cliques nos checkboxes
func (m *Manager) processMouseInput() {
	// Get cursor position
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procScreenToClient.Call(m.overlayHwnd, uintptr(unsafe.Pointer(&pt)))

	// Check if mouse is over UI area - only make window clickable if it is
	if m.isMouseOverUI(pt.X, pt.Y) {
		m.setWindowClickable(true)
	} else {
		m.setWindowClickable(false)
	}

	// Check if left mouse button is pressed
	ret, _, _ := procGetAsyncKeyState.Call(0x01) // VK_LBUTTON
	isPressed := (ret & 0x8000) != 0

	// Detect click (press and release)
	if isPressed && !m.lastMouseState {

		// Check if click is on Players checkbox
		if m.isPointInCheckbox(pt.X, pt.Y, m.checkboxPlayerX, m.checkboxPlayerY) {
			showPlayers := m.allEntitiesManager.ToggleShowPlayers()
			fmt.Printf("[UI] Show Players: %v\n", showPlayers)
		}

		// Check if click is on NPCs checkbox
		if m.isPointInCheckbox(pt.X, pt.Y, m.checkboxNPCX, m.checkboxNPCY) {
			showNPCs := m.allEntitiesManager.ToggleShowNPCs()
			fmt.Printf("[UI] Show NPCs: %v\n", showNPCs)
		}

		// Check if click is on Mates checkbox
		if m.isPointInCheckbox(pt.X, pt.Y, m.checkboxMateX, m.checkboxMateY) {
			showMates := m.allEntitiesManager.ToggleShowMates()
			fmt.Printf("[UI] Show Mates: %v\n", showMates)
		}

		// Check if click is on Range Decrease button
		if m.isPointInButton(pt.X, pt.Y, m.rangeDecBtnX, m.rangeDecBtnY, m.rangeBtnSize) {
			maxRange := m.allEntitiesManager.GetMaxRange()
			if maxRange > 50.0 {
				m.allEntitiesManager.SetMaxRange(maxRange - 25.0)
				fmt.Printf("[UI] Max Range: %.0fm\n", maxRange-25.0)
			}
		}

		// Check if click is on Range Increase button
		if m.isPointInButton(pt.X, pt.Y, m.rangeIncBtnX, m.rangeIncBtnY, m.rangeBtnSize) {
			maxRange := m.allEntitiesManager.GetMaxRange()
			if maxRange < 500.0 {
				m.allEntitiesManager.SetMaxRange(maxRange + 25.0)
				fmt.Printf("[UI] Max Range: %.0fm\n", maxRange+25.0)
			}
		}
	}

	m.lastMouseState = isPressed
}

// isPointInCheckbox checks if a point is inside a checkbox
func (m *Manager) isPointInCheckbox(px, py, cx, cy int32) bool {
	// Expand clickable area a bit for easier clicking (checkbox + label)
	expandedW := m.checkboxSize + 80
	expandedH := m.checkboxSize + 4
	return px >= cx && px <= cx+expandedW && py >= cy && py <= cy+expandedH
}

// isPointInButton checks if a point is inside a square button
func (m *Manager) isPointInButton(px, py, bx, by, size int32) bool {
	return px >= bx && px <= bx+size && py >= by && py <= by+size
}

// isMouseOverUI checks if mouse is over UI area (filter panel)
func (m *Manager) isMouseOverUI(px, py int32) bool {
	if !m.uiVisible || !m.allEntitiesManager.IsEnabled() {
		return false
	}

	// UI panel area (top-right corner)
	startX := m.screenW - 150
	startY := int32(10)
	panelX := startX - 5
	panelY := startY - 5
	panelW := int32(140)
	panelH := int32(110)

	return px >= panelX && px <= panelX+panelW && py >= panelY && py <= panelY+panelH
}

// drawFilterUI draws filter checkboxes
func (m *Manager) drawFilterUI() {
	// Position checkboxes in top-right corner
	startX := m.screenW - 150
	startY := int32(10)

	// Update checkbox positions
	m.checkboxPlayerX = startX
	m.checkboxPlayerY = startY
	m.checkboxNPCX = startX
	m.checkboxNPCY = startY + 25
	m.checkboxMateX = startX
	m.checkboxMateY = startY + 50

	// Range controls position
	rangeY := startY + 75
	m.rangeDecBtnX = startX
	m.rangeDecBtnY = rangeY
	m.rangeIncBtnX = startX + 100
	m.rangeIncBtnY = rangeY

	// Draw semi-transparent background panel (increased height for range controls)
	panelX := startX - 5
	panelY := startY - 5
	panelW := int32(140)
	panelH := int32(110) // Increased for Mates checkbox
	m.drawFilledRect(panelX, panelY, panelW, panelH, 0x00000000, 180)

	// Draw Players checkbox and label
	m.drawCheckbox(m.checkboxPlayerX, m.checkboxPlayerY, m.allEntitiesManager.GetShowPlayers())
	m.drawText(m.checkboxPlayerX+m.checkboxSize+5, m.checkboxPlayerY, "Players", COLOR_WHITE)

	// Draw NPCs checkbox and label
	m.drawCheckbox(m.checkboxNPCX, m.checkboxNPCY, m.allEntitiesManager.GetShowNPCs())
	m.drawText(m.checkboxNPCX+m.checkboxSize+5, m.checkboxNPCY, "NPCs", COLOR_WHITE)

	// Draw Mates checkbox and label
	m.drawCheckbox(m.checkboxMateX, m.checkboxMateY, m.allEntitiesManager.GetShowMates())
	m.drawText(m.checkboxMateX+m.checkboxSize+5, m.checkboxMateY, "Mates", COLOR_WHITE)

	// Draw Range label and value
	rangeText := fmt.Sprintf("Range: %.0fm", m.allEntitiesManager.GetMaxRange())
	m.drawText(startX, rangeY-20, rangeText, COLOR_WHITE)

	// Draw Range decrease button (-)
	m.drawButton(m.rangeDecBtnX, m.rangeDecBtnY, m.rangeBtnSize, "-")

	// Draw Range increase button (+)
	m.drawButton(m.rangeIncBtnX, m.rangeIncBtnY, m.rangeBtnSize, "+")
}

// drawCheckbox draws a checkbox
func (m *Manager) drawCheckbox(x, y int32, checked bool) {
	// Draw checkbox outline
	var color uintptr
	if checked {
		color = uintptr(COLOR_GREEN)
	} else {
		color = uintptr(COLOR_WHITE)
	}

	// Outer box
	m.drawLine(x, y, x+m.checkboxSize, y, color, 2)
	m.drawLine(x+m.checkboxSize, y, x+m.checkboxSize, y+m.checkboxSize, color, 2)
	m.drawLine(x+m.checkboxSize, y+m.checkboxSize, x, y+m.checkboxSize, color, 2)
	m.drawLine(x, y+m.checkboxSize, x, y, color, 2)

	// Draw checkmark if checked
	if checked {
		// Inner filled box
		colorU32 := uint32(COLOR_GREEN)
		m.drawFilledRect(x+2, y+2, m.checkboxSize-4, m.checkboxSize-4, colorU32, 255)
	}
}

// drawButton draws a button with text
func (m *Manager) drawButton(x, y, size int32, text string) {
	color := uintptr(COLOR_WHITE)

	// Draw button outline
	m.drawLine(x, y, x+size, y, color, 2)
	m.drawLine(x+size, y, x+size, y+size, color, 2)
	m.drawLine(x+size, y+size, x, y+size, color, 2)
	m.drawLine(x, y+size, x, y, color, 2)

	// Draw button text centered
	textOffset := int32(6)
	if text == "+" {
		textOffset = 5
	}
	m.drawText(x+textOffset, y+2, text, COLOR_WHITE)
}

// drawFilledRect draws a filled rectangle with transparency
func (m *Manager) drawFilledRect(x, y, w, h int32, color uint32, alpha byte) {
	// Create semi-transparent brush
	r := byte((color >> 16) & 0xFF)
	g := byte((color >> 8) & 0xFF)
	b := byte(color & 0xFF)

	// For now, draw a simple filled rectangle (GDI doesn't support alpha blending easily)
	// We'll use a darker color to simulate transparency
	darkColor := uint32(r/3)<<16 | uint32(g/3)<<8 | uint32(b/3)

	brush, _, _ := procCreateSolidBrush.Call(uintptr(darkColor))
	defer procDeleteObject.Call(brush)

	rect := RECT{
		Left:   x,
		Top:    y,
		Right:  x + w,
		Bottom: y + h,
	}

	procFillRect.Call(m.backDC, uintptr(unsafe.Pointer(&rect)), brush)
}

// setWindowClickable toggles window transparency to allow clicks
func (m *Manager) setWindowClickable(clickable bool) {
	const gwlExStyle = ^uintptr(0) - 19 // -20 in two's complement
	exStyle, _, _ := procGetWindowLongW.Call(m.overlayHwnd, gwlExStyle)

	if clickable {
		// Remove WS_EX_TRANSPARENT to allow clicks
		exStyle &^= WS_EX_TRANSPARENT
	} else {
		// Add WS_EX_TRANSPARENT to make clicks pass through
		exStyle |= WS_EX_TRANSPARENT
	}

	procSetWindowLongW.Call(m.overlayHwnd, gwlExStyle, exStyle)
}

// SetStyle changes ESP style
func (m *Manager) SetStyle(style ESPStyle) {
	m.Style = style
}

// CycleStyle cicla entre os estilos disponiveis
func (m *Manager) CycleStyle() ESPStyle {
	m.Style = (m.Style + 1) % 4
	return m.Style
}

// GetStyleName returns current style name
func (m *Manager) GetStyleName() string {
	switch m.Style {
	case StyleCorners:
		return "Corners"
	case StyleFullBox:
		return "Full Box"
	case StyleCircle:
		return "Circle"
	case StyleBrackets:
		return "Brackets"
	default:
		return "Unknown"
	}
}

// ToggleAllEntities toggles all entities ESP
func (m *Manager) ToggleAllEntities() bool {
	if m.allEntitiesManager.IsEnabled() {
		m.allEntitiesManager.Stop()
		// Make sure window is transparent when ESP is disabled
		m.setWindowClickable(false)
	} else {
		m.allEntitiesManager.Start()
	}
	return m.allEntitiesManager.IsEnabled()
}

// IsAllEntitiesEnabled returns if all entities ESP is enabled
func (m *Manager) IsAllEntitiesEnabled() bool {
	return m.allEntitiesManager.IsEnabled()
}

// ToggleShowPlayers toggles players filter
func (m *Manager) ToggleShowPlayers() bool {
	return m.allEntitiesManager.ToggleShowPlayers()
}

// ToggleShowNPCs toggles NPCs filter
func (m *Manager) ToggleShowNPCs() bool {
	return m.allEntitiesManager.ToggleShowNPCs()
}

// GetShowPlayers returns players filter state
func (m *Manager) GetShowPlayers() bool {
	return m.allEntitiesManager.GetShowPlayers()
}

// GetShowNPCs returns NPCs filter state
func (m *Manager) GetShowNPCs() bool {
	return m.allEntitiesManager.GetShowNPCs()
}

// ToggleShowMates toggles mates filter
func (m *Manager) ToggleShowMates() bool {
	return m.allEntitiesManager.ToggleShowMates()
}

// GetShowMates returns mates filter state
func (m *Manager) GetShowMates() bool {
	return m.allEntitiesManager.GetShowMates()
}

// InstallPersistentHook instala o hook permanente
// ============================================================================
// Aimbot Functions
// ============================================================================

// EnableAimbot ativa o aimbot
func (m *Manager) EnableAimbot() {
	m.aimbotEnabled = true
}

// DisableAimbot desativa o aimbot
func (m *Manager) DisableAimbot() {
	m.aimbotEnabled = false
}

// ToggleAimbot toggles aimbot state
func (m *Manager) ToggleAimbot() bool {
	m.aimbotEnabled = !m.aimbotEnabled
	return m.aimbotEnabled
}

// IsAimbotEnabled returns if aimbot is enabled
func (m *Manager) IsAimbotEnabled() bool {
	return m.aimbotEnabled
}

// SetAimbotKeys sets keys that activate aimbot when pressed
func (m *Manager) SetAimbotKeys(keys []int) {
	m.aimbotKeys = keys
}

// AddAimbotKey adds a key to aimbot
func (m *Manager) AddAimbotKey(key int) {
	m.aimbotKeys = append(m.aimbotKeys, key)
}

// GetAimbotKeys returns configured keys
func (m *Manager) GetAimbotKeys() []int {
	return m.aimbotKeys
}

// ClearAimbotKeys clears all aimbot keys
func (m *Manager) ClearAimbotKeys() {
	m.aimbotKeys = nil
}

// AimbotKeyConfig represents a configured key
type AimbotKeyConfig struct {
	Name string `json:"name"`
	Code int    `json:"code"`
}

// AimbotConfig represents aimbot configuration
type AimbotConfig struct {
	Enabled bool              `json:"enabled"`
	Keys    []AimbotKeyConfig `json:"keys"`
}

// LoadAimbotConfig loads aimbot config from JSON file
func (m *Manager) LoadAimbotConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	var config AimbotConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// Aplicar configuracao
	m.aimbotEnabled = config.Enabled
	m.aimbotKeys = nil
	for _, key := range config.Keys {
		m.aimbotKeys = append(m.aimbotKeys, key.Code)
		fmt.Printf("[AIMBOT] Key: %s (0x%02X)\n", key.Name, key.Code)
	}

	return nil
}

// SaveAimbotConfig saves current aimbot config
func (m *Manager) SaveAimbotConfig(filename string) error {
	config := AimbotConfig{
		Enabled: m.aimbotEnabled,
		Keys:    make([]AimbotKeyConfig, len(m.aimbotKeys)),
	}

	for i, code := range m.aimbotKeys {
		config.Keys[i] = AimbotKeyConfig{
			Name: fmt.Sprintf("Key%d", i+1),
			Code: code,
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// isAimbotKeyPressed checks if any aimbot key is pressed
func (m *Manager) isAimbotKeyPressed() bool {
	for _, key := range m.aimbotKeys {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(key))
		if ret&0x8000 != 0 {
			return true
		}
	}
	return false
}

// aimbotLoop runs in separate goroutine with high update rate
func (m *Manager) aimbotLoop() {
	m.aimbotRunning = true
	defer func() { m.aimbotRunning = false }()

	for m.running {
		select {
		case <-m.aimbotStopChan:
			return
		default:
		}

		// Taxa otimizada: ~240 FPS (4ms)
		time.Sleep(4 * time.Millisecond)

		// Aimbot activates only when config key is pressed
		// Mutex in WorldToScreen protects against race condition with ESP
		if m.isAimbotKeyPressed() {
			m.AimAtTarget()
		}
	}
}

// AimAtTarget moves cursor to target position
func (m *Manager) AimAtTarget() bool {
	return m.AimAtTargetDebug(false)
}

// AimAtTargetDebug moves cursor with optional debug
func (m *Manager) AimAtTargetDebug(debug bool) bool {
	targetID := m.readU32(targetBase + targetIDOffset)

	// Check if target is selected
	if targetID == 0 {
		if debug {
			fmt.Println("[AIM] FAIL: No target (ID=0)")
		}
		return false
	}

	// Get player target coordinates
	targetX := m.readFloat32(targetBase + playerTargetPosX)
	targetY := m.readFloat32(targetBase + playerTargetPosY)
	targetZ := m.readFloat32(targetBase + playerTargetPosZ)

	if targetX == 0 && targetY == 0 && targetZ == 0 {
		if debug {
			fmt.Println("[AIM] FAIL: Player coords are 0,0,0 (probably mob target)")
		}
		return false
	}

	// WorldToScreen (order: X, Z, Y)
	screenX, screenY, screenZ := m.WorldToScreen(targetX, targetZ, targetY)

	// Filter target behind camera (screenZ >= 1.0 = behind)
	isInvalidZ := math.IsNaN(float64(screenZ)) || math.IsInf(float64(screenZ), 0)
	if isInvalidZ || screenZ >= 1.0 {
		if debug {
			fmt.Printf("[AIM] FAIL: Behind camera (screenZ=%.4f, invalid=%v)\n", screenZ, isInvalidZ)
		}
		return false
	}

	// Filter target off screen
	if screenX < 0 || screenX > 100 || screenY < 0 || screenY > 100 {
		if debug {
			fmt.Printf("[AIM] FAIL: Off screen (screen: %.1f,%.1f)\n", screenX, screenY)
		}
		return false
	}

	// Convert to pixels
	pixelX := int32(screenX * float32(m.screenW) / 100.0)
	pixelY := int32(screenY * float32(m.screenH) / 100.0)

	// Adjust to aim at character center (in 2D pixels, after projection)
	// Subtract pixels to aim higher (Y grows downward on screen)
	pixelY -= 15

	if pixelX <= 0 || pixelX >= m.screenW || pixelY <= 0 || pixelY >= m.screenH {
		if debug {
			fmt.Printf("[AIM] FAIL: Off screen (pixel: %d,%d)\n", pixelX, pixelY)
		}
		return false
	}

	if debug {
		fmt.Printf("[AIM] OK: ID=0x%X coords=(%.1f,%.1f,%.1f) pixel=(%d,%d)\n",
			targetID, targetX, targetY, targetZ, pixelX, pixelY)
	}

	// SetCursorPos uses absolute screen coordinates
	ret, _, _ := procSetCursorPos.Call(uintptr(pixelX), uintptr(pixelY))

	return ret != 0
}

// IsTargetPlayer returns true if current target is a player
func (m *Manager) IsTargetPlayer() bool {
	if !m.HasTarget() {
		return false
	}
	flag := m.readU32(targetBase + targetTypeFlag)
	return flag == 0
}

// GetTargetScreenPos returns target position on screen
func (m *Manager) GetTargetScreenPos() (int32, int32, bool) {
	if m.lastTargetX <= 0 || m.lastTargetY <= 0 {
		return 0, 0, false
	}
	return m.lastTargetX, m.lastTargetY, true
}

// ============================================================================
// Target Memory Scanner/Debug
// ============================================================================

// TargetScanner monitors changes in target memory region
type TargetScanner struct {
	handle        uintptr
	baseAddr      uintptr
	scanSize      int
	prevSnapshot  []byte
	scanCount     int
	debugFile     *os.File
	scanning      bool
}

// NewTargetScanner creates a new target scanner
func (m *Manager) NewTargetScanner() *TargetScanner {
	return &TargetScanner{
		handle:   m.processHandle,
		baseAddr: targetBase,
		scanSize: 0x800, // Scan 2KB around targetBase
		scanning: false,
	}
}

// StartScanning starts scanning and creates debug file
func (ts *TargetScanner) StartScanning() error {
	filename := fmt.Sprintf("target_scan_%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo de debug: %v", err)
	}
	ts.debugFile = file
	ts.scanning = true
	ts.scanCount = 0

	// File header
	ts.debugFile.WriteString("========================================\n")
	ts.debugFile.WriteString("  TARGET MEMORY SCANNER DEBUG LOG\n")
	ts.debugFile.WriteString(fmt.Sprintf("  Base: 0x%08X  Size: 0x%X\n", ts.baseAddr, ts.scanSize))
	ts.debugFile.WriteString(fmt.Sprintf("  Started: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	ts.debugFile.WriteString("========================================\n\n")

	// Fazer snapshot inicial
	ts.prevSnapshot = ts.readMemoryRegion()
	ts.logSnapshot("INITIAL SNAPSHOT")

	fmt.Printf("[SCANNER] Iniciado! Salvando em: %s\n", filename)
	return nil
}

// StopScanning stops scanning and closes the file
func (ts *TargetScanner) StopScanning() {
	if ts.debugFile != nil {
		ts.debugFile.WriteString("\n========================================\n")
		ts.debugFile.WriteString(fmt.Sprintf("  Scan finalizado: %s\n", time.Now().Format("2006-01-02 15:04:05")))
		ts.debugFile.WriteString(fmt.Sprintf("  Total de scans: %d\n", ts.scanCount))
		ts.debugFile.WriteString("========================================\n")
		ts.debugFile.Close()
		ts.debugFile = nil
	}
	ts.scanning = false
	fmt.Println("[SCANNER] Parado!")
}

// IsScanning returns if scanning is active
func (ts *TargetScanner) IsScanning() bool {
	return ts.scanning
}

// readMemoryRegion reads the memory region
func (ts *TargetScanner) readMemoryRegion() []byte {
	data := make([]byte, ts.scanSize)
	var bytesRead uintptr

	procReadProcessMemory.Call(
		ts.handle,
		ts.baseAddr,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(ts.scanSize),
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	return data
}

// ScanForChanges detects and logs changes
func (ts *TargetScanner) ScanForChanges(label string) {
	if !ts.scanning || ts.debugFile == nil {
		return
	}

	ts.scanCount++
	currentSnapshot := ts.readMemoryRegion()

	// Detect changes
	changes := ts.compareSnapshots(ts.prevSnapshot, currentSnapshot)

	if len(changes) > 0 {
		ts.debugFile.WriteString(fmt.Sprintf("\n--- SCAN #%d: %s [%s] ---\n",
			ts.scanCount, label, time.Now().Format("15:04:05.000")))
		ts.debugFile.WriteString(fmt.Sprintf("Mudanças detectadas: %d\n\n", len(changes)))

		for _, change := range changes {
			ts.debugFile.WriteString(change)
		}

		// Log interesting floats in coordinates region
		ts.logInterestingFloats(currentSnapshot)

		fmt.Printf("[SCANNER] Scan #%d: %d mudanças detectadas\n", ts.scanCount, len(changes))
	}

	ts.prevSnapshot = currentSnapshot
}

// compareSnapshots compares two snapshots and returns differences
func (ts *TargetScanner) compareSnapshots(prev, curr []byte) []string {
	var changes []string

	// Compare in 4-byte blocks (DWORD)
	for i := 0; i+4 <= len(prev) && i+4 <= len(curr); i += 4 {
		prevVal := binary.LittleEndian.Uint32(prev[i : i+4])
		currVal := binary.LittleEndian.Uint32(curr[i : i+4])

		if prevVal != currVal {
			addr := ts.baseAddr + uintptr(i)
			prevFloat := math.Float32frombits(prevVal)
			currFloat := math.Float32frombits(currVal)

			// Check if looks like valid float (coordinates usually between -100000 and 100000)
			isValidFloat := (currFloat > -100000 && currFloat < 100000) &&
				(currFloat != 0) && !math.IsNaN(float64(currFloat)) && !math.IsInf(float64(currFloat), 0)

			change := fmt.Sprintf("  [0x%08X] +0x%03X: 0x%08X -> 0x%08X",
				addr, i, prevVal, currVal)

			if isValidFloat {
				change += fmt.Sprintf("  (float: %.2f -> %.2f)", prevFloat, currFloat)
			}
			change += "\n"

			changes = append(changes, change)
		}
	}

	return changes
}

// logSnapshot logs a complete snapshot
func (ts *TargetScanner) logSnapshot(label string) {
	ts.debugFile.WriteString(fmt.Sprintf("\n=== %s ===\n", label))
	ts.logInterestingFloats(ts.prevSnapshot)
}

// logInterestingFloats logs floats that appear to be coordinates
func (ts *TargetScanner) logInterestingFloats(data []byte) {
	ts.debugFile.WriteString("\nOffsets com floats interessantes (possiveis coordenadas):\n")

	// Offsets conhecidos
	knownOffsets := map[int]string{
		0x320: "mobTargetX",
		0x324: "mobTargetZ",
		0x328: "mobTargetY",
		0x6A4: "playerTargetX",
		0x6A8: "playerTargetZ",
		0x6AC: "playerTargetY",
	}

	// Logar offsets conhecidos
	ts.debugFile.WriteString("\n  -- Offsets Conhecidos --\n")
	for offset, name := range knownOffsets {
		if offset+4 <= len(data) {
			val := binary.LittleEndian.Uint32(data[offset : offset+4])
			floatVal := math.Float32frombits(val)
			ts.debugFile.WriteString(fmt.Sprintf("  +0x%03X %-15s: 0x%08X (%.2f)\n",
				offset, name, val, floatVal))
		}
	}

	// Search for other floats that look like coordinates
	ts.debugFile.WriteString("\n  -- Outros Floats (range 100-50000) --\n")
	count := 0
	for i := 0; i+4 <= len(data) && count < 50; i += 4 {
		val := binary.LittleEndian.Uint32(data[i : i+4])
		floatVal := math.Float32frombits(val)

		// Filter floats that look like coordinates (typical position values in game)
		absVal := float32(math.Abs(float64(floatVal)))
		if absVal > 100 && absVal < 50000 && !math.IsNaN(float64(floatVal)) {
			// Skip known offsets
			_, known := knownOffsets[i]
			if !known {
				ts.debugFile.WriteString(fmt.Sprintf("  +0x%03X: 0x%08X (%.2f)\n",
					i, val, floatVal))
				count++
			}
		}
	}

	// Search for possible flags/enums (small values 0-10)
	ts.debugFile.WriteString("\n  -- Possiveis Flags (0-10) --\n")
	count = 0
	for i := 0; i+4 <= len(data) && count < 30; i += 4 {
		val := binary.LittleEndian.Uint32(data[i : i+4])
		if val > 0 && val <= 10 {
			ts.debugFile.WriteString(fmt.Sprintf("  +0x%03X: %d\n", i, val))
			count++
		}
	}
}

// DumpRegion does a full dump of the region
func (ts *TargetScanner) DumpRegion() {
	if ts.debugFile == nil {
		return
	}

	data := ts.readMemoryRegion()
	ts.debugFile.WriteString("\n\n=== FULL MEMORY DUMP ===\n")
	ts.debugFile.WriteString(fmt.Sprintf("Base: 0x%08X\n\n", ts.baseAddr))

	for i := 0; i < len(data); i += 16 {
		// Address
		ts.debugFile.WriteString(fmt.Sprintf("%08X: ", uint32(ts.baseAddr)+uint32(i)))

		// Hex bytes
		for j := 0; j < 16 && i+j < len(data); j++ {
			ts.debugFile.WriteString(fmt.Sprintf("%02X ", data[i+j]))
		}

		// ASCII
		ts.debugFile.WriteString(" |")
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b <= 126 {
				ts.debugFile.WriteString(string(b))
			} else {
				ts.debugFile.WriteString(".")
			}
		}
		ts.debugFile.WriteString("|\n")
	}
}

// ============================================================================
// Helper functions for entity collection
// ============================================================================

func isValidPtr(ptr uint32) bool {
	return ptr >= 0x10000000 && ptr < 0xF0000000
}

func isValidCoord(coord float32) bool {
	return coord > -100000 && coord < 100000 && !math.IsNaN(float64(coord)) && !math.IsInf(float64(coord), 0)
}

func isValidEntityName(name string) bool {
	if len(name) < 2 || len(name) > 32 {
		return false
	}

	alphaCount := 0
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			alphaCount++
		} else if c < 32 && c != 0 {
			return false
		}
	}
	return alphaCount >= 2
}

func (m *Manager) readU8(addr uintptr) byte {
	var buf [1]byte
	var read uintptr
	procReadProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&buf[0])), 1, uintptr(unsafe.Pointer(&read)))
	return buf[0]
}

func (m *Manager) readString(addr uintptr, maxLen int) string {
	buf := make([]byte, maxLen)
	var read uintptr
	ret, _, _ := procReadProcessMemory.Call(m.processHandle, addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(maxLen), uintptr(unsafe.Pointer(&read)))
	if ret == 0 {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

func (m *Manager) getMaxHP(entityAddr uint32) uint32 {
	base := m.readU32(uintptr(entityAddr + 0x38))
	if !isValidPtr(base) {
		return 0
	}

	esi := m.readU32(uintptr(base + 0x4698))
	if !isValidPtr(esi) {
		return 0
	}

	stats := m.readU32(uintptr(esi + 0x10))
	if !isValidPtr(stats) {
		return 0
	}

	return m.readU32(uintptr(stats + 0x420))
}

func (m *Manager) getEntityName(entityAddr uint32) string {
	namePtr1 := m.readU32(uintptr(entityAddr + 0x0C))
	if !isValidPtr(namePtr1) {
		return ""
	}

	namePtr2 := m.readU32(uintptr(namePtr1 + 0x1C))
	if !isValidPtr(namePtr2) {
		return ""
	}

	return m.readString(uintptr(namePtr2), 32)
}

// debugEntityFlags compares LocalPlayer with other entities to find flags
func (m *Manager) debugEntityFlags() {
	// Get local player entity
	lpPtr1 := m.readU32(m.x2game + 0xE9DC54)
	if !isValidPtr(lpPtr1) {
		return
	}
	lpEntity := m.readU32(uintptr(lpPtr1 + 0x10))
	if !isValidPtr(lpEntity) {
		return
	}

	// Common flag offsets to check
	flagOffsets := []uint32{
		0x04, 0x08, 0x10, 0x14, 0x18, 0x1C, 0x20, 0x24, 0x28, 0x2C,
		0x30, 0x34, 0x38, 0x3C, 0x40, 0x44, 0x48, 0x50, 0x58, 0x60,
		0x80, 0x84, 0x88, 0x8C, 0x90, 0x94, 0x98,
	}

	fmt.Println("\n=== ENTITY FLAGS DEBUG ===")
	fmt.Printf("LocalPlayer Entity: 0x%X\n", lpEntity)

	// Print local player flags
	fmt.Print("LocalPlayer: ")
	for _, off := range flagOffsets {
		val := m.readU32(uintptr(lpEntity + off))
		fmt.Printf("+%03X=%08X ", off, val)
	}
	fmt.Println()

	// Compare with first 3 other entities
	cachedEntities := m.allEntitiesManager.GetCachedEntities()
	count := 0
	for i, entity := range cachedEntities {
		if count >= 3 {
			break
		}

		entityType := "NPC"
		if entity.IsPlayer {
			entityType = "PLAYER"
		} else if entity.IsMate {
			entityType = "MATE"
		}

		fmt.Printf("\n%s %-15s (%.0fm): ", entityType, entity.Name, entity.Distance)
		for _, off := range flagOffsets {
			val := m.readU32(uintptr(entity.Address + off))
			lpVal := m.readU32(uintptr(lpEntity + off))

			// Highlight differences with *
			if val != lpVal {
				fmt.Printf("+%03X=%08X* ", off, val)
			} else {
				fmt.Printf("+%03X=%08X ", off, val)
			}
		}
		fmt.Println()

		count++
		i = i // suppress unused
	}
	fmt.Println("=========================\n")
}