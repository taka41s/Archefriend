package esp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
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
	procCreateRemoteThread       = kernel32.NewProc("CreateRemoteThread")
	procWaitForSingleObject      = kernel32.NewProc("WaitForSingleObject")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procShowWindow                 = user32.NewProc("ShowWindow")
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
	WS_POPUP          = 0x80000000

	LWA_COLORKEY = 0x00000001
	SW_SHOW      = 5
	SW_HIDE      = 0
	PM_REMOVE    = 0x0001

	TRANSPARENT_COLOR = 0x00FF00FF

	PS_SOLID    = 0
	TRANSPARENT = 1
	NULL_BRUSH  = 5
	FW_BOLD     = 700
	SRCCOPY     = 0x00CC0020
)

// Cores para distância
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

	// Flag de tipo de target: 1 = mob/NPC, 0 = player
	targetTypeFlag = 0x028

	// ID do target atual (0 = sem target, != 0 = target selecionado)
	targetIDOffset = 0x008

	// Target pointer (para verificar se tem target selecionado)
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

// ESPStyle define o estilo visual do ESP
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

	// Aimbot
	aimbotEnabled   bool
	aimbotRunning   bool
	aimbotStopChan  chan bool
	aimbotKeys      []int // Teclas que ativam o aimbot quando pressionadas
	lastTargetX     int32
	lastTargetY     int32
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

// NewManager cria um novo ESP manager
func NewManager(processHandle uintptr, pid uint32, x2game uintptr) (*Manager, error) {
	m := &Manager{
		processHandle:  processHandle,
		x2game:         x2game,
		enabled:        false,
		running:        false,
		stopChan:       make(chan bool),
		aimbotStopChan: make(chan bool),
		Style:          StyleCorners,
		CornerLength:   12,
		BoxThickness:   2,
	}

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

	// Create overlay
	className := syscall.StringToUTF16Ptr("ArcheFriendESP")
	wc := WNDCLASSEXW{
		Size:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		WndProc:   syscall.NewCallback(wndProc),
		ClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TRANSPARENT|WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
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

// GetPlayerPosition retorna a posicao do jogador local
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

// WorldToScreen converte coordenadas do mundo para tela
func (m *Manager) WorldToScreen(x, y, z float32) (float32, float32) {
	m.writeFloat32(m.shellcodeBase+INPUT_X_OFFSET, x)
	m.writeFloat32(m.shellcodeBase+INPUT_Y_OFFSET, y)
	m.writeFloat32(m.shellcodeBase+INPUT_Z_OFFSET, z)

	var threadID uint32
	th, _, _ := procCreateRemoteThread.Call(m.processHandle, 0, 0, m.shellcodeBase, 0, 0, uintptr(unsafe.Pointer(&threadID)))
	if th == 0 {
		return 0, 0
	}
	procWaitForSingleObject.Call(th, 5000)
	procCloseHandle.Call(th)

	screenX := m.readFloat32(m.shellcodeBase + OUTPUT_X_OFFSET)
	screenY := m.readFloat32(m.shellcodeBase + OUTPUT_Y_OFFSET)
	return screenX, screenY
}

// HasTarget verifica se tem um target selecionado
func (m *Manager) HasTarget() bool {
	targetPtr := m.readU32(m.x2game + PTR_ENEMY_TARGET)
	return targetPtr != 0
}

// GetTarget retorna a posicao do alvo atual (apenas player targets)
func (m *Manager) GetTarget() (float32, float32, float32, bool) {
	// Verifica se tem target usando o ID
	if !m.HasTargetByID() {
		return 0, 0, 0, false
	}

	// Player target coords (se for mob, essas coords estarao zeradas)
	x := m.readFloat32(targetBase + playerTargetPosX)
	y := m.readFloat32(targetBase + playerTargetPosY)
	z := m.readFloat32(targetBase + playerTargetPosZ)

	if x == 0 && y == 0 && z == 0 {
		return 0, 0, 0, false
	}
	return x, y, z, true
}

// DebugTargetInfo printa informacoes de debug sobre o target atual
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

// HasTargetByID verifica se tem target usando o ID
func (m *Manager) HasTargetByID() bool {
	targetID := m.readU32(targetBase + targetIDOffset)
	return targetID != 0
}

// HasPlayerTarget verifica se tem um player como target
func (m *Manager) HasPlayerTarget() bool {
	if !m.HasTargetByID() {
		return false
	}
	flag := m.readU32(targetBase + targetTypeFlag)
	return flag == 0
}

// CalculateDistance calcula a distancia 3D entre dois pontos
func CalculateDistance(x1, y1, z1, x2, y2, z2 float32) float32 {
	dx := x2 - x1
	dy := y2 - y1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// GetColorByDistance retorna a cor baseada na distancia
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
		go m.aimbotLoop() // Aimbot em goroutine separada
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

// Toggle alterna o estado do ESP
func (m *Manager) Toggle() bool {
	if m.enabled {
		m.Disable()
	} else {
		m.Enable()
	}
	return m.enabled
}

// IsEnabled retorna se o ESP esta ativo
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// Stop para o loop de renderizacao
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
	// Copia o back buffer para a tela de uma vez (elimina flickering)
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

	// Shadow
	procSetBkMode.Call(m.backDC, TRANSPARENT)
	procSetTextColor.Call(m.backDC, COLOR_BLACK)
	textPtr := syscall.StringToUTF16Ptr(text)
	procTextOutW.Call(m.backDC, uintptr(x+1), uintptr(y+1), uintptr(unsafe.Pointer(textPtr)), uintptr(len(text)))

	// Main text
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

		time.Sleep(8 * time.Millisecond) // ~120 FPS para aimbot mais responsivo

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

		// Aimbot agora roda em goroutine separada (aimbotLoop)

		if !m.enabled {
			m.clearOverlay() // Limpa quando desabilitado
			continue
		}

		// Get player position
		playerX, playerY, playerZ, hasPlayer := m.GetPlayerPosition()
		if !hasPlayer {
			m.clearOverlay()
			continue
		}

		// Get player target (ignora mobs)
		targetX, targetY, targetZ, hasTarget := m.GetTarget()
		if !hasTarget {
			m.lastTargetX = 0
			m.lastTargetY = 0
			m.clearOverlay()
			continue
		}

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, targetX, targetY, targetZ)

		// Cor baseada na distancia
		color := GetColorByDistance(distance)

		// WorldToScreen (ordem: X, Z, Y)
		screenX, screenY := m.WorldToScreen(targetX, targetZ, targetY)

		// Convert to pixels
		pixelX := int32(screenX * float32(m.screenW) / 100.0)
		pixelY := int32(screenY * float32(m.screenH) / 100.0)

		if pixelX <= 0 || pixelX >= m.screenW || pixelY <= 0 || pixelY >= m.screenH {
			m.clearOverlay()
			continue
		}

		// Store target position for aimbot
		m.lastTargetX = pixelX
		m.lastTargetY = pixelY

		// === DOUBLE BUFFERING ===
		// 1. Limpa o back buffer
		m.clearBackBuffer()

		// Box dimensions (menor)
		boxW := int32(30)
		boxH := int32(50)
		boxX := pixelX - boxW/2
		boxY := pixelY - boxH/2

		// 2. Desenha tudo no back buffer
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

		// Draw label com distancia
		labelText := fmt.Sprintf("PLAYER %.0fm", distance)
		m.drawText(pixelX-25, boxY-18, labelText, color)

		// 3. Copia o back buffer para a tela de uma vez (sem flicker)
		m.present()
	}
}

// SetStyle muda o estilo do ESP
func (m *Manager) SetStyle(style ESPStyle) {
	m.Style = style
}

// CycleStyle cicla entre os estilos disponiveis
func (m *Manager) CycleStyle() ESPStyle {
	m.Style = (m.Style + 1) % 4
	return m.Style
}

// GetStyleName retorna o nome do estilo atual
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

// ToggleAimbot alterna o estado do aimbot
func (m *Manager) ToggleAimbot() bool {
	m.aimbotEnabled = !m.aimbotEnabled
	return m.aimbotEnabled
}

// IsAimbotEnabled retorna se o aimbot esta ativo
func (m *Manager) IsAimbotEnabled() bool {
	return m.aimbotEnabled
}

// SetAimbotKeys configura as teclas que ativam o aimbot quando pressionadas
func (m *Manager) SetAimbotKeys(keys []int) {
	m.aimbotKeys = keys
}

// AddAimbotKey adiciona uma tecla ao aimbot
func (m *Manager) AddAimbotKey(key int) {
	m.aimbotKeys = append(m.aimbotKeys, key)
}

// GetAimbotKeys retorna as teclas configuradas
func (m *Manager) GetAimbotKeys() []int {
	return m.aimbotKeys
}

// ClearAimbotKeys limpa todas as teclas do aimbot
func (m *Manager) ClearAimbotKeys() {
	m.aimbotKeys = nil
}

// AimbotKeyConfig representa uma tecla configurada
type AimbotKeyConfig struct {
	Name string `json:"name"`
	Code int    `json:"code"`
}

// AimbotConfig representa a configuracao do aimbot
type AimbotConfig struct {
	Enabled bool              `json:"enabled"`
	Keys    []AimbotKeyConfig `json:"keys"`
}

// LoadAimbotConfig carrega a configuracao do aimbot de um arquivo JSON
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

// SaveAimbotConfig salva a configuracao atual do aimbot
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

// isAimbotKeyPressed verifica se alguma tecla do aimbot esta pressionada
func (m *Manager) isAimbotKeyPressed() bool {
	for _, key := range m.aimbotKeys {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(key))
		if ret&0x8000 != 0 {
			return true
		}
	}
	return false
}

// aimbotLoop roda em goroutine separada com alta taxa de atualizacao
func (m *Manager) aimbotLoop() {
	m.aimbotRunning = true
	defer func() { m.aimbotRunning = false }()

	for m.running {
		select {
		case <-m.aimbotStopChan:
			return
		default:
		}

		// Taxa alta: ~500 FPS (2ms)
		time.Sleep(2 * time.Millisecond)

		// Aimbot ativa apenas quando tecla do config esta pressionada
		if m.isAimbotKeyPressed() {
			m.AimAtTarget()
		}
	}
}

// AimAtTarget move o cursor para a posicao do alvo
func (m *Manager) AimAtTarget() bool {
	return m.AimAtTargetDebug(false)
}

// AimAtTargetDebug move o cursor com debug opcional
func (m *Manager) AimAtTargetDebug(debug bool) bool {
	targetID := m.readU32(targetBase + targetIDOffset)

	// Verifica se tem target selecionado
	if targetID == 0 {
		if debug {
			fmt.Println("[AIM] FAIL: No target (ID=0)")
		}
		return false
	}

	// Pega coordenadas do player target (ignora flag, usa coords diretamente)
	targetX := m.readFloat32(targetBase + playerTargetPosX)
	targetY := m.readFloat32(targetBase + playerTargetPosY)
	targetZ := m.readFloat32(targetBase + playerTargetPosZ)

	if targetX == 0 && targetY == 0 && targetZ == 0 {
		if debug {
			fmt.Println("[AIM] FAIL: Player coords are 0,0,0 (probably mob target)")
		}
		return false
	}

	// WorldToScreen (ordem: X, Z, Y)
	screenX, screenY := m.WorldToScreen(targetX, targetZ, targetY)

	// Convert to pixels
	pixelX := int32(screenX * float32(m.screenW) / 100.0)
	pixelY := int32(screenY * float32(m.screenH) / 100.0)

	// Ajuste para mirar no centro do personagem (em pixels 2D, após projeção)
	// Subtrai pixels para mirar mais alto (Y cresce para baixo na tela)
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

	// SetCursorPos usa coordenadas absolutas da tela
	ret, _, _ := procSetCursorPos.Call(uintptr(pixelX), uintptr(pixelY))
	return ret != 0
}

// IsTargetPlayer retorna true se o target atual eh um player
func (m *Manager) IsTargetPlayer() bool {
	if !m.HasTarget() {
		return false
	}
	flag := m.readU32(targetBase + targetTypeFlag)
	return flag == 0
}

// GetTargetScreenPos retorna a posicao do alvo na tela
func (m *Manager) GetTargetScreenPos() (int32, int32, bool) {
	if m.lastTargetX <= 0 || m.lastTargetY <= 0 {
		return 0, 0, false
	}
	return m.lastTargetX, m.lastTargetY, true
}

// ============================================================================
// Target Memory Scanner/Debug
// ============================================================================

// TargetScanner monitora mudanças na região de memória do target
type TargetScanner struct {
	handle        uintptr
	baseAddr      uintptr
	scanSize      int
	prevSnapshot  []byte
	scanCount     int
	debugFile     *os.File
	scanning      bool
}

// NewTargetScanner cria um novo scanner de target
func (m *Manager) NewTargetScanner() *TargetScanner {
	return &TargetScanner{
		handle:   m.processHandle,
		baseAddr: targetBase,
		scanSize: 0x800, // Scan 2KB ao redor do targetBase
		scanning: false,
	}
}

// StartScanning inicia o scan e cria arquivo de debug
func (ts *TargetScanner) StartScanning() error {
	filename := fmt.Sprintf("target_scan_%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo de debug: %v", err)
	}
	ts.debugFile = file
	ts.scanning = true
	ts.scanCount = 0

	// Header do arquivo
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

// StopScanning para o scan e fecha o arquivo
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

// IsScanning retorna se está escaneando
func (ts *TargetScanner) IsScanning() bool {
	return ts.scanning
}

// readMemoryRegion lê a região de memória
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

// ScanForChanges detecta e loga mudanças
func (ts *TargetScanner) ScanForChanges(label string) {
	if !ts.scanning || ts.debugFile == nil {
		return
	}

	ts.scanCount++
	currentSnapshot := ts.readMemoryRegion()

	// Detectar mudanças
	changes := ts.compareSnapshots(ts.prevSnapshot, currentSnapshot)

	if len(changes) > 0 {
		ts.debugFile.WriteString(fmt.Sprintf("\n--- SCAN #%d: %s [%s] ---\n",
			ts.scanCount, label, time.Now().Format("15:04:05.000")))
		ts.debugFile.WriteString(fmt.Sprintf("Mudanças detectadas: %d\n\n", len(changes)))

		for _, change := range changes {
			ts.debugFile.WriteString(change)
		}

		// Log floats interessantes na região de coordenadas
		ts.logInterestingFloats(currentSnapshot)

		fmt.Printf("[SCANNER] Scan #%d: %d mudanças detectadas\n", ts.scanCount, len(changes))
	}

	ts.prevSnapshot = currentSnapshot
}

// compareSnapshots compara dois snapshots e retorna as diferenças
func (ts *TargetScanner) compareSnapshots(prev, curr []byte) []string {
	var changes []string

	// Comparar em blocos de 4 bytes (DWORD)
	for i := 0; i+4 <= len(prev) && i+4 <= len(curr); i += 4 {
		prevVal := binary.LittleEndian.Uint32(prev[i : i+4])
		currVal := binary.LittleEndian.Uint32(curr[i : i+4])

		if prevVal != currVal {
			addr := ts.baseAddr + uintptr(i)
			prevFloat := math.Float32frombits(prevVal)
			currFloat := math.Float32frombits(currVal)

			// Verificar se parece ser float válido (coordenadas geralmente entre -100000 e 100000)
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

// logSnapshot loga um snapshot completo
func (ts *TargetScanner) logSnapshot(label string) {
	ts.debugFile.WriteString(fmt.Sprintf("\n=== %s ===\n", label))
	ts.logInterestingFloats(ts.prevSnapshot)
}

// logInterestingFloats loga floats que parecem ser coordenadas
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

	// Procurar outros floats que parecem coordenadas
	ts.debugFile.WriteString("\n  -- Outros Floats (range 100-50000) --\n")
	count := 0
	for i := 0; i+4 <= len(data) && count < 50; i += 4 {
		val := binary.LittleEndian.Uint32(data[i : i+4])
		floatVal := math.Float32frombits(val)

		// Filtrar floats que parecem coordenadas (valores típicos de posição no jogo)
		absVal := float32(math.Abs(float64(floatVal)))
		if absVal > 100 && absVal < 50000 && !math.IsNaN(float64(floatVal)) {
			// Pular offsets já conhecidos
			_, known := knownOffsets[i]
			if !known {
				ts.debugFile.WriteString(fmt.Sprintf("  +0x%03X: 0x%08X (%.2f)\n",
					i, val, floatVal))
				count++
			}
		}
	}

	// Procurar possíveis flags/enums (valores pequenos 0-10)
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

// DumpRegion faz um dump completo da região
func (ts *TargetScanner) DumpRegion() {
	if ts.debugFile == nil {
		return
	}

	data := ts.readMemoryRegion()
	ts.debugFile.WriteString("\n\n=== FULL MEMORY DUMP ===\n")
	ts.debugFile.WriteString(fmt.Sprintf("Base: 0x%08X\n\n", ts.baseAddr))

	for i := 0; i < len(data); i += 16 {
		// Endereço
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
