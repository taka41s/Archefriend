package esp

import (
	"encoding/binary"
	"fmt"
	"math"
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

	procCreatePen        = gdi32.NewProc("CreatePen")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procMoveToEx         = gdi32.NewProc("MoveToEx")
	procLineTo           = gdi32.NewProc("LineTo")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procTextOutW         = gdi32.NewProc("TextOutW")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreateFont       = gdi32.NewProc("CreateFontW")
	procEllipse          = gdi32.NewProc("Ellipse")

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
	targetPosX = 0x320
	targetPosY = 0x328
	targetPosZ = 0x324

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

	// Configuracoes visuais
	Style        ESPStyle
	CornerLength int32
	BoxThickness int32

	// Aimbot
	aimbotEnabled bool
	lastTargetX   int32
	lastTargetY   int32
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
		processHandle: processHandle,
		x2game:        x2game,
		enabled:       false,
		running:       false,
		stopChan:      make(chan bool),
		Style:         StyleCorners,
		CornerLength:  12,
		BoxThickness:  2,
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

// GetTarget retorna a posicao do alvo atual
func (m *Manager) GetTarget() (float32, float32, float32, bool) {
	// Primeiro verifica se tem target selecionado
	if !m.HasTarget() {
		return 0, 0, 0, false
	}

	x := m.readFloat32(targetBase + targetPosX)
	y := m.readFloat32(targetBase + targetPosY)
	z := m.readFloat32(targetBase + targetPosZ)
	if x == 0 && y == 0 && z == 0 {
		return 0, 0, 0, false
	}
	return x, y, z, true
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

// ============================================================================
// Drawing Functions
// ============================================================================

func (m *Manager) drawLine(x1, y1, x2, y2 int32, color uintptr, thickness int) {
	pen, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thickness), color)
	oldPen, _, _ := procSelectObject.Call(m.hdc, pen)
	procMoveToEx.Call(m.hdc, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(m.hdc, uintptr(x2), uintptr(y2))
	procSelectObject.Call(m.hdc, oldPen)
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
	oldPen1, _, _ := procSelectObject.Call(m.hdc, pen1)
	nullBrush, _, _ := procGetStockObject.Call(NULL_BRUSH)
	oldBrush, _, _ := procSelectObject.Call(m.hdc, nullBrush)
	procEllipse.Call(m.hdc, uintptr(centerX-radius-1), uintptr(centerY-radius-1), uintptr(centerX+radius+1), uintptr(centerY+radius+1))
	procSelectObject.Call(m.hdc, oldPen1)
	procDeleteObject.Call(pen1)

	// Main circle
	pen2, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thick), color)
	oldPen2, _, _ := procSelectObject.Call(m.hdc, pen2)
	procEllipse.Call(m.hdc, uintptr(centerX-radius), uintptr(centerY-radius), uintptr(centerX+radius), uintptr(centerY+radius))
	procSelectObject.Call(m.hdc, oldPen2)
	procSelectObject.Call(m.hdc, oldBrush)
	procDeleteObject.Call(pen2)
}

func (m *Manager) drawText(x, y int32, text string, color uintptr) {
	if m.font != 0 {
		procSelectObject.Call(m.hdc, m.font)
	}

	// Shadow
	procSetBkMode.Call(m.hdc, TRANSPARENT)
	procSetTextColor.Call(m.hdc, COLOR_BLACK)
	textPtr := syscall.StringToUTF16Ptr(text)
	procTextOutW.Call(m.hdc, uintptr(x+1), uintptr(y+1), uintptr(unsafe.Pointer(textPtr)), uintptr(len(text)))

	// Main text
	procSetTextColor.Call(m.hdc, color)
	procTextOutW.Call(m.hdc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(textPtr)), uintptr(len(text)))
}

func (m *Manager) renderLoop() {
	for m.running {
		select {
		case <-m.stopChan:
			return
		default:
		}

		time.Sleep(16 * time.Millisecond) // ~60 FPS

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

		if !m.enabled {
			continue
		}

		// Clear
		m.clearOverlay()

		// Get player position
		playerX, playerY, playerZ, hasPlayer := m.GetPlayerPosition()
		if !hasPlayer {
			continue
		}

		// Get target - verifica se tem target selecionado
		targetX, targetY, targetZ, hasTarget := m.GetTarget()
		if !hasTarget {
			// Clear aimbot position when no target
			m.lastTargetX = 0
			m.lastTargetY = 0
			continue
		}

		// Double check: se posicao for invalida, nao desenha
		if targetX == 0 && targetY == 0 && targetZ == 0 {
			m.lastTargetX = 0
			m.lastTargetY = 0
			continue
		}

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, targetX, targetY, targetZ)

		// Get color based on distance
		color := GetColorByDistance(distance)

		// WorldToScreen (ordem: X, Z, Y)
		screenX, screenY := m.WorldToScreen(targetX, targetZ, targetY)

		// Convert to pixels
		pixelX := int32(screenX * float32(m.screenW) / 100.0)
		pixelY := int32(screenY * float32(m.screenH) / 100.0)

		if pixelX <= 0 || pixelX >= m.screenW || pixelY <= 0 || pixelY >= m.screenH {
			continue
		}

		// Store target position for aimbot
		m.lastTargetX = pixelX
		m.lastTargetY = pixelY

		// Box dimensions (menor)
		boxW := int32(30)
		boxH := int32(50)
		boxX := pixelX - boxW/2
		boxY := pixelY - boxH/2

		// Draw box based on style with distance color
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

		// Draw distance text
		distText := fmt.Sprintf("%.1fm", distance)
		m.drawText(pixelX-15, boxY-18, distText, color)
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

// AimAtTarget move o cursor para a posicao do alvo
func (m *Manager) AimAtTarget() bool {
	if !m.enabled || m.lastTargetX <= 0 || m.lastTargetY <= 0 {
		return false
	}

	// SetCursorPos usa coordenadas absolutas da tela
	ret, _, _ := procSetCursorPos.Call(uintptr(m.lastTargetX), uintptr(m.lastTargetY))
	return ret != 0
}

// GetTargetScreenPos retorna a posicao do alvo na tela
func (m *Manager) GetTargetScreenPos() (int32, int32, bool) {
	if m.lastTargetX <= 0 || m.lastTargetY <= 0 {
		return 0, 0, false
	}
	return m.lastTargetX, m.lastTargetY, true
}
