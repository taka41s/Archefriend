package esp

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ============================================================================
// Game Offsets — DoodadManager & Housing
// ============================================================================

const (
	// DoodadManager singleton: *(*x2game+OFF_DOODAD_MGR)
	OFF_DOODAD_MGR uintptr = 0xE9BA30

	// DoodadManager tree0 (main doodad tree)
	DOODAD_TREE_HDR_OFF   uintptr = 8
	DOODAD_TREE_COUNT_OFF uintptr = 12
	DOODAD_TREE_ISNIL_OFF         = 21

	// Doodad struct offsets
	DOODAD_TYPE_OFF      uint32 = 0x454 // type=3 means housing doodad
	DOODAD_ENCRYPTED_OFF uint32 = 0x5C  // encrypted transform (32 bytes)
	DOODAD_DBID_OFF      uint32 = 0x450 // shared DbId linking to housing info

	// Housing Manager: *(*x2game+OFF_HOUSING_MGR)
	OFF_HOUSING_MGR uintptr = 0xE9B4F4

	// Housing info struct offsets
	HOUSING_OWNER_OFF   uintptr = 0x28  // null-terminated owner name
	HOUSING_TEMPLID_OFF uintptr = 0x08  // template ID
	HOUSING_DBID_OFF    uintptr = 0x1C4 // shared DbId linking to doodad

	// Packet sending offsets (from x2game.dll base, IDA base 0x39000000)
	OFF_SENDPKT         uintptr = 0x1C2B80  // SendPacket (thiscall)
	OFF_NETMGR          uintptr = 0xE9DC68  // NetManager pointer
	OFF_TICK_FUNC       uintptr = 0x087200  // Tick function (hook target)
	OFF_ON_HOUSE_TAX    uintptr = 0x3F9EF0  // OnHouseTaxInfo handler
	OFF_VTABLE_CS222    uintptr = 0xD352D8  // vtable for CS 222 packet

	// World→Map calibration (game functions, IDA base 0x39000000)
	OFF_GETPOS_FUNC     uintptr = 0x05A680  // sub_3905A680 (thiscall: get internal position)
	OFF_WORLD_MGR       uintptr = 0xEA2074  // World/Environment manager ptr

	// Code cave layout
	CAVE_SIZE           uintptr = 0x8000    // 32KB
	CAVE_CAPT_COUNT     uintptr = 0x000     // uint32: captured response count
	CAVE_CAPT_CODE      uintptr = 0x080     // OnHouseTaxInfo hook shellcode
	CAVE_SEND_REQ       uintptr = 0x1000    // byte: send request flag
	CAVE_SEND_RES       uintptr = 0x1001    // byte: send response flag
	CAVE_SEND_PKT       uintptr = 0x1010    // 64-byte packet buffer
	CAVE_CAL_REQ        uintptr = 0x1100    // byte: calibration request flag
	CAVE_CAL_DONE       uintptr = 0x1101    // byte: calibration done flag
	CAVE_CAL_OUT        uintptr = 0x1104    // 3x float32: map X, Z, Y output
	CAVE_CAL_TMP        uintptr = 0x1120    // 16-byte temp buffer for getPos
	CAVE_SEND_CODE      uintptr = 0x1200    // Tick hook shellcode
	CAVE_LOG_BASE       uintptr = 0x2000    // captured responses (256 × 64 bytes)

	// Auto-scan: only query houses within 20m of player
	AUTOSCAN_RANGE      float32 = 17.0

	// MarkerManager (map marker slots) — offsets from x2game.dll base
	OFF_MARKER_MGR      uintptr = 0x1329518 // MarkerManager static object (IDA 0x3A329518)
)

// ============================================================================
// House data structs
// ============================================================================

// HouseEntry represents a scanned house in the in-memory database
type HouseEntry struct {
	TlId      uint32  `json:"tl_id"`
	ObjId     uint32  `json:"obj_id,omitempty"`
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	Z         float32 `json:"z"`
	MapX      float32 `json:"map_x,omitempty"`
	MapZ      float32 `json:"map_z,omitempty"`
	Sextant   string  `json:"sextant,omitempty"`
	Owner     string  `json:"owner,omitempty"`
	TemplID   uint32  `json:"templ_id,omitempty"`
	Protected int64   `json:"protected,omitempty"`
	Deposit   uint32  `json:"deposit,omitempty"`
	Tax       uint32  `json:"tax,omitempty"`
	Scanned    bool    `json:"scanned"`
	ScanTime   int64   `json:"scan_time,omitempty"`
	Demolition bool    `json:"demolition"`
	RecheckAt  int64   `json:"recheck_at,omitempty"`
	CS222Raw   string  `json:"cs222_raw,omitempty"`
}

// HouseDB is the JSON file structure
type HouseDB struct {
	Version int          `json:"version"`
	Updated string       `json:"updated"`
	Houses  []HouseEntry `json:"houses"`
}

// HouseRenderInfo stores data for rendering a single house
// Coordinate convention: same as game entities
//   PosX = game X (east-west)     — from decrypted[16:20]
//   PosY = game Y (height)        — from decrypted[24:28]
//   PosZ = game Z (north-south)   — from decrypted[20:24]
type HouseRenderInfo struct {
	ObjId          uint32
	TlId           uint32
	PosX, PosY, PosZ float32 // same convention as EntityInfo
	Owner          string
	Distance       float32
	Scanned        bool
	Protected      int64
}

// taxData stores CS 222 response data for a house
type taxData struct {
	Protected int64
	Deposit   uint32
	BaseTax   uint32
	// Coordinates captured at query time (so save never loses them)
	PosX, PosY, PosZ float32
	ObjId            uint32
	Owner            string
	RawResponse      []byte
	Demolition       bool
}

// ============================================================================
// Tree node for walking DoodadManager/Housing trees
// ============================================================================

type doodadTreeNode struct {
	Key     uint32 // node+12 (ObjId)
	DataPtr uint32 // node+16 (doodad pointer)
}

// ============================================================================
// HouseESPManager
// ============================================================================

type HouseESPManager struct {
	mainManager   *Manager
	processHandle uintptr
	x2game        uintptr

	mu      sync.Mutex
	enabled bool
	running bool

	// Cached render data
	cachedHouses []HouseRenderInfo
	cacheMutex   sync.RWMutex

	// In-memory house database (source of truth, exported to Lua)
	houseDB        []HouseEntry          // all known houses
	scannedByObjId map[uint32]*HouseEntry
	scannedByTlId  map[uint32]*HouseEntry
	luaExportPath  string // path to export Lua table for addon (e.g. Navigate/data/scan.lua)
	dbLoaded       bool
	dbMutex        sync.RWMutex // protects houseDB, scannedByObjId and scannedByTlId

	// Filters
	showScanned   bool
	showUnscanned bool
	maxRange      float32
	filterType    int32 // current doodad type filter (-1 = all)

	// Control
	stopChan chan bool

	// Debug: print once on first scan
	firstScan bool

	// Packet sending infrastructure
	caveAddr       uintptr            // VirtualAllocEx'd code cave
	handlerHooked  bool               // OnHouseTaxInfo hook installed
	tickHooked     bool               // Tick hook installed
	handlerOrig    []byte             // original bytes at OnHouseTaxInfo
	tickOrig       []byte             // original bytes at Tick function
	taxInfo        map[uint32]taxData // TlId → response data
	taxMutex       sync.Mutex
	sending        bool               // true while background sender is running

	// World→Map calibration
	calOffsetX float64
	calOffsetZ float64
	calReady   bool

	// All houses cache (used by GetAllDemolitionCount)
	allHousesCache     []HouseEntry
	allHousesLastCheck time.Time

	// Recheck panel
	showRecheckPanel bool
	recheckCache     []HouseEntry
	recheckLastCheck time.Time
	recheckScroll    int

	// Demolition panel
	showDemolitionPanel bool
	demolitionDayOffset int       // 0=today, 1=tomorrow, -1=yesterday...
	demolitionCache     []HouseEntry
	demolitionCacheDay  int       // cached day offset
	demolitionLastCheck time.Time
	demolitionScroll    int
}

func NewHouseESPManager(mainManager *Manager, processHandle uintptr, x2game uintptr) *HouseESPManager {
	return &HouseESPManager{
		mainManager:    mainManager,
		processHandle:  processHandle,
		x2game:         x2game,
		enabled:        false,
		showScanned:    true,
		showUnscanned:  true,
		maxRange:       500.0,
		filterType:     6,
		scannedByObjId: make(map[uint32]*HouseEntry),
		scannedByTlId:  make(map[uint32]*HouseEntry),
		stopChan:       make(chan bool, 1),
		firstScan:      true,
		taxInfo:        make(map[uint32]taxData),
	}
}

// ============================================================================
// Memory reading helpers
// ============================================================================

func (h *HouseESPManager) readU32(addr uintptr) uint32 {
	return h.mainManager.readU32(addr)
}

func (h *HouseESPManager) readBytes(addr uintptr, buf []byte) bool {
	return h.mainManager.readBytes(addr, buf)
}

func (h *HouseESPManager) readString(addr uintptr, maxLen int) string {
	return h.mainManager.readString(addr, maxLen)
}

// ============================================================================
// Memory write helpers
// ============================================================================

func (h *HouseESPManager) writeBytes(addr uintptr, buf []byte) bool {
	var written uintptr
	ret, _, _ := procWriteProcessMemory.Call(h.processHandle, addr,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&written)))
	return ret != 0
}

func (h *HouseESPManager) writeU32(addr uintptr, val uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	h.writeBytes(addr, buf)
}

func (h *HouseESPManager) writeU8(addr uintptr, val byte) {
	h.writeBytes(addr, []byte{val})
}

func (h *HouseESPManager) readU8(addr uintptr) byte {
	buf := make([]byte, 1)
	h.readBytes(addr, buf)
	return buf[0]
}

// writePatch writes bytes at a code address (changes protection first)
func (h *HouseESPManager) writePatch(addr uintptr, buf []byte) {
	var oldProtect uint32
	procVirtualProtectEx.Call(h.processHandle, addr, uintptr(len(buf)),
		PAGE_EXECUTE_READWRITE, uintptr(unsafe.Pointer(&oldProtect)))
	h.writeBytes(addr, buf)
	procVirtualProtectEx.Call(h.processHandle, addr, uintptr(len(buf)),
		uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// ============================================================================
// Code cave setup for CS 222 packet sending
// ============================================================================

func (h *HouseESPManager) setupCodeCave() error {
	if h.caveAddr != 0 {
		return nil // already set up
	}

	// Allocate code cave
	cave, _, _ := procVirtualAllocEx.Call(h.processHandle, 0, uintptr(CAVE_SIZE),
		MEM_COMMIT|MEM_RESERVE, PAGE_EXECUTE_READWRITE)
	if cave == 0 {
		return fmt.Errorf("VirtualAllocEx failed")
	}
	h.caveAddr = cave

	// Zero it out
	zeros := make([]byte, CAVE_SIZE)
	h.writeBytes(cave, zeros)

	realHandler := h.x2game + OFF_ON_HOUSE_TAX
	realTick := h.x2game + OFF_TICK_FUNC
	realSendPkt := h.x2game + OFF_SENDPKT
	realNetMgr := h.x2game + OFF_NETMGR

	// Verify prologues
	handlerPrologue := make([]byte, 8)
	h.readBytes(realHandler, handlerPrologue)
	if handlerPrologue[0] != 0x55 || handlerPrologue[1] != 0x8B || handlerPrologue[2] != 0xEC {
		procVirtualFreeEx.Call(h.processHandle, cave, 0, MEM_RELEASE)
		h.caveAddr = 0
		return fmt.Errorf("OnHouseTaxInfo prologue mismatch: %X", handlerPrologue[:5])
	}
	h.handlerOrig = make([]byte, 5)
	copy(h.handlerOrig, handlerPrologue[:5])

	tickPrologue := make([]byte, 8)
	h.readBytes(realTick, tickPrologue)
	expected := []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x08}
	for i := range expected {
		if tickPrologue[i] != expected[i] {
			procVirtualFreeEx.Call(h.processHandle, cave, 0, MEM_RELEASE)
			h.caveAddr = 0
			return fmt.Errorf("Tick prologue mismatch: %X", tickPrologue[:6])
		}
	}
	h.tickOrig = make([]byte, 6)
	copy(h.tickOrig, tickPrologue[:6])

	captCountAddr := cave + CAVE_CAPT_COUNT
	captCodeAddr := cave + CAVE_CAPT_CODE
	captLogBase := cave + CAVE_LOG_BASE
	sendReqAddr := cave + CAVE_SEND_REQ
	sendResAddr := cave + CAVE_SEND_RES
	sendPktAddr := cave + CAVE_SEND_PKT
	sendCodeAddr := cave + CAVE_SEND_CODE

	// ── Shellcode 1: OnHouseTaxInfo hook — copy 64 bytes from a2 ──
	sc1 := []byte{}
	sc1 = append(sc1, 0x50, 0x51, 0x56, 0x57) // push eax,ecx,esi,edi
	sc1 = append(sc1, 0xA1)                     // mov eax, [captCountAddr]
	sc1 = append(sc1, le32(uint32(captCountAddr))...)
	sc1 = append(sc1, 0x3D)                     // cmp eax, 256
	sc1 = append(sc1, le32(256)...)
	sc1 = append(sc1, 0x7D, 0x00)               // jge skip (patched below)
	jgePos := len(sc1) - 1

	sc1 = append(sc1, 0x89, 0xC7)               // mov edi, eax
	sc1 = append(sc1, 0xC1, 0xE7, 0x06)         // shl edi, 6  (×64)
	sc1 = append(sc1, 0x81, 0xC7)               // add edi, captLogBase
	sc1 = append(sc1, le32(uint32(captLogBase))...)
	sc1 = append(sc1, 0x8B, 0x74, 0x24, 24)     // mov esi, [esp+24] (a2)
	sc1 = append(sc1, 0xFC)                      // cld
	sc1 = append(sc1, 0xB9)                      // mov ecx, 16
	sc1 = append(sc1, le32(16)...)
	sc1 = append(sc1, 0xF3, 0xA5)               // rep movsd
	sc1 = append(sc1, 0x40)                      // inc eax
	sc1 = append(sc1, 0xA3)                      // mov [captCountAddr], eax
	sc1 = append(sc1, le32(uint32(captCountAddr))...)

	sc1[jgePos] = byte(len(sc1) - (jgePos + 1)) // patch jge offset
	sc1 = append(sc1, 0x5F, 0x5E, 0x59, 0x58)   // pop edi,esi,ecx,eax
	sc1 = append(sc1, h.handlerOrig...)           // original bytes
	sc1 = append(sc1, 0xE9)                       // jmp back
	jb1 := int32(realHandler+5) - int32(captCodeAddr+uintptr(len(sc1))+4)
	sc1 = append(sc1, le32(uint32(jb1))...)
	h.writeBytes(captCodeAddr, sc1)

	// ── Shellcode 2: Tick hook — calibration + CS 222 ──
	realWorldMgr := h.x2game + OFF_WORLD_MGR
	realGetPos := h.x2game + OFF_GETPOS_FUNC
	calReqAddr := cave + CAVE_CAL_REQ
	calDoneAddr := cave + CAVE_CAL_DONE
	calOutAddr := cave + CAVE_CAL_OUT
	calTmpAddr := cave + CAVE_CAL_TMP

	sc2 := []byte{}
	sc2 = append(sc2, 0x9C, 0x60)               // pushfd, pushad

	// === World→Map calibration check ===
	sc2 = append(sc2, 0x80, 0x3D)               // cmp byte [calReqAddr], 1
	sc2 = append(sc2, le32(uint32(calReqAddr))...)
	sc2 = append(sc2, 0x01)
	sc2 = append(sc2, 0x75, 0x00)               // jne .skipCal (patched below)
	jneCalPos := len(sc2) - 1

	// Step 1: ECX = *(*netMgr) + 0xFC, call getPos(calTmpAddr)
	sc2 = append(sc2, 0xA1)                     // mov eax, [realNetMgr]
	sc2 = append(sc2, le32(uint32(realNetMgr))...)
	sc2 = append(sc2, 0x8B, 0x00)               // mov eax, [eax]
	sc2 = append(sc2, 0x8B, 0x88, 0xFC, 0x00, 0x00, 0x00) // mov ecx, [eax+0xFC]
	sc2 = append(sc2, 0x68)                     // push calTmpAddr
	sc2 = append(sc2, le32(uint32(calTmpAddr))...)
	sc2 = append(sc2, 0xB8)                     // mov eax, realGetPos
	sc2 = append(sc2, le32(uint32(realGetPos))...)
	sc2 = append(sc2, 0xFF, 0xD0)               // call eax

	// Step 2: obj = *(*worldMgr + 0x84), vtable[10](obj, calTmpAddr)
	sc2 = append(sc2, 0xA1)                     // mov eax, [realWorldMgr]
	sc2 = append(sc2, le32(uint32(realWorldMgr))...)
	sc2 = append(sc2, 0x8B, 0x00)               // mov eax, [eax]
	sc2 = append(sc2, 0x8B, 0x88, 0x84, 0x00, 0x00, 0x00) // mov ecx, [eax+0x84]
	sc2 = append(sc2, 0x8B, 0x01)               // mov eax, [ecx] (vtable)
	sc2 = append(sc2, 0x68)                     // push calTmpAddr
	sc2 = append(sc2, le32(uint32(calTmpAddr))...)
	sc2 = append(sc2, 0xFF, 0x50, 0x28)         // call [eax+0x28] (vtable[10])

	// Step 3: copy calTmpAddr → calOutAddr (3 dwords = 12 bytes)
	sc2 = append(sc2, 0xFC)                     // cld
	sc2 = append(sc2, 0xBE)                     // mov esi, calTmpAddr
	sc2 = append(sc2, le32(uint32(calTmpAddr))...)
	sc2 = append(sc2, 0xBF)                     // mov edi, calOutAddr
	sc2 = append(sc2, le32(uint32(calOutAddr))...)
	sc2 = append(sc2, 0xA5, 0xA5, 0xA5)         // movsd ×3

	// req=0, done=1
	sc2 = append(sc2, 0xC6, 0x05)               // mov byte [calReqAddr], 0
	sc2 = append(sc2, le32(uint32(calReqAddr))...)
	sc2 = append(sc2, 0x00)
	sc2 = append(sc2, 0xC6, 0x05)               // mov byte [calDoneAddr], 1
	sc2 = append(sc2, le32(uint32(calDoneAddr))...)
	sc2 = append(sc2, 0x01)

	// .skipCal:
	sc2[jneCalPos] = byte(len(sc2) - (jneCalPos + 1))

	// === CS 222 send check ===
	sc2 = append(sc2, 0x80, 0x3D)               // cmp byte [sendReqAddr], 1
	sc2 = append(sc2, le32(uint32(sendReqAddr))...)
	sc2 = append(sc2, 0x01)
	sc2 = append(sc2, 0x75, 0x00)               // jne .skipSend (patched below)
	jneSendPos := len(sc2) - 1

	sc2 = append(sc2, 0xC6, 0x05)               // mov byte [sendReqAddr], 0
	sc2 = append(sc2, le32(uint32(sendReqAddr))...)
	sc2 = append(sc2, 0x00)
	sc2 = append(sc2, 0xA1)                     // mov eax, [realNetMgr]
	sc2 = append(sc2, le32(uint32(realNetMgr))...)
	sc2 = append(sc2, 0x8B, 0x08)               // mov ecx, [eax] (this ptr)
	sc2 = append(sc2, 0x68)                     // push sendPktAddr
	sc2 = append(sc2, le32(uint32(sendPktAddr))...)
	sc2 = append(sc2, 0xB8)                     // mov eax, realSendPkt
	sc2 = append(sc2, le32(uint32(realSendPkt))...)
	sc2 = append(sc2, 0xFF, 0xD0)               // call eax
	sc2 = append(sc2, 0xC6, 0x05)               // mov byte [sendResAddr], 1
	sc2 = append(sc2, le32(uint32(sendResAddr))...)
	sc2 = append(sc2, 0x01)

	// .skipSend:
	sc2[jneSendPos] = byte(len(sc2) - (jneSendPos + 1))

	sc2 = append(sc2, 0x61, 0x9D)               // popad, popfd
	sc2 = append(sc2, h.tickOrig...)              // original bytes
	sc2 = append(sc2, 0xE9)                       // jmp back
	jb2 := int32(realTick+6) - int32(sendCodeAddr+uintptr(len(sc2))+4)
	sc2 = append(sc2, le32(uint32(jb2))...)
	h.writeBytes(sendCodeAddr, sc2)

	fmt.Printf("[HOUSES] Code cave @ 0x%X, shellcode1=%d, shellcode2=%d bytes\n",
		cave, len(sc1), len(sc2))
	return nil
}

func (h *HouseESPManager) installHandlerHook() {
	if h.handlerHooked || h.caveAddr == 0 {
		return
	}
	realHandler := h.x2game + OFF_ON_HOUSE_TAX
	captCodeAddr := h.caveAddr + CAVE_CAPT_CODE
	h.writeU32(h.caveAddr+CAVE_CAPT_COUNT, 0)

	patch := []byte{0xE9}
	jmp := int32(captCodeAddr) - int32(realHandler+5)
	patch = append(patch, le32(uint32(jmp))...)
	h.writePatch(realHandler, patch)
	h.handlerHooked = true
}

func (h *HouseESPManager) removeHandlerHook() {
	if !h.handlerHooked || h.caveAddr == 0 {
		return
	}
	realHandler := h.x2game + OFF_ON_HOUSE_TAX
	h.writePatch(realHandler, h.handlerOrig)
	h.handlerHooked = false
}

func (h *HouseESPManager) installTickHook() {
	if h.tickHooked || h.caveAddr == 0 {
		return
	}
	realTick := h.x2game + OFF_TICK_FUNC
	sendCodeAddr := h.caveAddr + CAVE_SEND_CODE

	patch := []byte{0xE9}
	jmp := int32(sendCodeAddr) - int32(realTick+5)
	patch = append(patch, le32(uint32(jmp))...)
	patch = append(patch, 0x90) // NOP (6th byte)
	h.writePatch(realTick, patch)
	h.tickHooked = true
}

func (h *HouseESPManager) removeTickHook() {
	if !h.tickHooked || h.caveAddr == 0 {
		return
	}
	h.writeU8(h.caveAddr+CAVE_SEND_REQ, 0)
	time.Sleep(50 * time.Millisecond)
	realTick := h.x2game + OFF_TICK_FUNC
	h.writePatch(realTick, h.tickOrig)
	h.tickHooked = false
}

func (h *HouseESPManager) cleanupCodeCave() {
	h.removeHandlerHook()
	h.removeTickHook()
	if h.caveAddr != 0 {
		time.Sleep(100 * time.Millisecond)
		procVirtualFreeEx.Call(h.processHandle, h.caveAddr, 0, MEM_RELEASE)
		h.caveAddr = 0
	}
}

// calibrateOnce triggers the main code cave's calibration shellcode once
// to compute the world→map offset. After this, WorldToMap is pure math.
// Follows mark.go Calibrator pattern: calibrate once, then offset is reused.
func (h *HouseESPManager) calibrateOnce() error {
	if h.caveAddr == 0 || !h.tickHooked {
		return fmt.Errorf("code cave not ready")
	}

	playerX, _, playerZ, ok := h.mainManager.GetPlayerPosition()
	if !ok {
		return fmt.Errorf("no player position")
	}

	calReqAddr := h.caveAddr + CAVE_CAL_REQ
	calDoneAddr := h.caveAddr + CAVE_CAL_DONE
	calOutAddr := h.caveAddr + CAVE_CAL_OUT

	h.writeU8(calDoneAddr, 0)
	h.writeU8(calReqAddr, 1)

	for i := 0; i < 500; i++ {
		time.Sleep(10 * time.Millisecond)
		if h.readU8(calDoneAddr) == 1 {
			outBuf := make([]byte, 12)
			if !h.readBytes(calOutAddr, outBuf) {
				return fmt.Errorf("read output failed")
			}

			mapX := float64(math.Float32frombits(binary.LittleEndian.Uint32(outBuf[0:4])))
			mapZ := float64(math.Float32frombits(binary.LittleEndian.Uint32(outBuf[4:8])))

			h.calOffsetX = mapX - float64(playerX)
			h.calOffsetZ = mapZ - float64(playerZ)
			h.calReady = true

			fmt.Printf("[MAP] Calibrated: player(%.1f,%.1f) → map(%.1f,%.1f) offset=(%.1f,%.1f)\n",
				playerX, playerZ, mapX, mapZ, h.calOffsetX, h.calOffsetZ)
			return nil
		}
	}
	return fmt.Errorf("calibration timeout")
}

// worldToMapCoords converts world coordinates to map coordinates using calibration offset.
// Pure math — no shellcode needed. Must call calibrateOnce() first.
func (h *HouseESPManager) worldToMapCoords(wx, wz float32) (float32, float32) {
	return float32(float64(wx) + h.calOffsetX), float32(float64(wz) + h.calOffsetZ)
}

// mapToSextant converts map coordinates to sextant string (e.g. "W 3°12'5\" N 18°7'30\"")
func mapToSextant(mapX, mapZ float64) string {
	distX := math.Abs(mapX - 21504.0)
	degX := int(distX) / 1024
	remX := (distX - float64(degX)*1024) * 60.0
	minX := int(remX) / 1024
	secX := int((remX-float64(minX)*1024)*60.0) / 1024

	distZ := math.Abs(mapZ - 28672.0)
	degZ := int(distZ) / 1024
	remZ := (distZ - float64(degZ)*1024) * 60.0
	minZ := int(remZ) / 1024
	secZ := int((remZ-float64(minZ)*1024)*60.0) / 1024

	dirX := "W"
	if mapX >= 21504.0 {
		dirX = "E"
	}
	dirZ := "S"
	if mapZ >= 28672.0 {
		dirZ = "N"
	}
	return fmt.Sprintf("%s %d*%d'%d\" %s %d*%d'%d\"", dirX, degX, minX, secX, dirZ, degZ, minZ, secZ)
}

// sextantToMapCoords converts a sextant string to map coordinates using the
// exact game coefficient (same formula as the game's Lua conversion).
// Format: "W 3*12'5\" N 18*7'30\""
func sextantToMapCoords(sextant string) (mapX, mapZ float32, ok bool) {
	const coordCoef = 0.00097657363894522145695357130138029

	var dirX, dirZ string
	var degX, minX, secX, degZ, minZ, secZ int
	n, _ := fmt.Sscanf(sextant, "%s %d*%d'%d\" %s %d*%d'%d\"",
		&dirX, &degX, &minX, &secX, &dirZ, &degZ, &minZ, &secZ)
	if n != 8 {
		return 0, 0, false
	}

	// Longitude (E/W → map_x)
	xCoords := float64(degX) + float64(minX)/60.0 + float64(secX)/3600.0
	if dirX == "W" {
		xCoords = -xCoords
	}
	mx := (xCoords + 21.0) / coordCoef

	// Latitude (N/S → map_z)
	zCoords := float64(degZ) + float64(minZ)/60.0 + float64(secZ)/3600.0
	if dirZ == "S" {
		zCoords = -zCoords
	}
	mz := (zCoords + 28.0) / coordCoef

	return float32(mx), float32(mz), true
}

// GetPlayerSextant returns the player's current sextant position if calibrated.
func (h *HouseESPManager) GetPlayerSextant(playerX, playerZ float32) (string, bool) {
	if !h.calReady {
		return "", false
	}
	mx, mz := h.worldToMapCoords(playerX, playerZ)
	return mapToSextant(float64(mx), float64(mz)), true
}

// sendCS222 sends a CS 222 packet to query house tax/protection info
func (h *HouseESPManager) sendCS222(tlId uint32) error {
	if h.caveAddr == 0 {
		return fmt.Errorf("no code cave")
	}

	vtableAddr := uint32(h.x2game + OFF_VTABLE_CS222)

	// Build packet
	pkt := make([]byte, 64)
	binary.LittleEndian.PutUint32(pkt[0:4], vtableAddr)
	binary.LittleEndian.PutUint32(pkt[4:8], 222)
	binary.LittleEndian.PutUint32(pkt[12:16], tlId)
	// ObjId=0 works

	h.writeBytes(h.caveAddr+CAVE_SEND_PKT, pkt)
	h.writeU8(h.caveAddr+CAVE_SEND_RES, 0)

	h.installTickHook()
	h.writeU8(h.caveAddr+CAVE_SEND_REQ, 1)

	// Wait for send confirmation
	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		if h.readU8(h.caveAddr+CAVE_SEND_RES) == 1 {
			return nil
		}
	}
	return fmt.Errorf("send timeout")
}


// backgroundSendAndCollect runs in a goroutine: sends CS222 one at a time,
// collects each response and keys it by the SENT TlId (response TlId differs).
// This never blocks the scan loop / ESP rendering.
func (h *HouseESPManager) backgroundSendAndCollect(toSend []HouseRenderInfo) {
	defer func() { h.sending = false }()

	// Recalibrate before each batch (offset can change between ESP restarts)
	if err := h.calibrateOnce(); err != nil {
		fmt.Printf("[HOUSES] WARNING: recalibration failed: %v\n", err)
	}

	const maxRetries = 3
	received := 0
	for _, house := range toSend {
		var gotResponse bool
		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Reset capture counter before each send
			h.writeU32(h.caveAddr+CAVE_CAPT_COUNT, 0)

			if err := h.sendCS222(house.TlId); err != nil {
				fmt.Printf("[HOUSES] SEND CS222 TlId=%d FAILED: %v (attempt %d/%d)\n", house.TlId, err, attempt, maxRetries)
				continue
			}
			if attempt == 1 {
				fmt.Printf("[HOUSES] SEND CS222 TlId=%d owner=%q\n", house.TlId, house.Owner)
			} else {
				fmt.Printf("[HOUSES] RETRY CS222 TlId=%d attempt %d/%d\n", house.TlId, attempt, maxRetries)
			}

			// Wait for server response
			time.Sleep(180 * time.Millisecond)

			// Check if we got a response
			count := h.readU32(h.caveAddr + CAVE_CAPT_COUNT)
			if count == 0 {
				fmt.Printf("[HOUSES] No response for TlId=%d (attempt %d/%d)\n", house.TlId, attempt, maxRetries)
				continue
			}

			raw := make([]byte, 64)
			if !h.readBytes(h.caveAddr+CAVE_LOG_BASE, raw) {
				continue
			}

			deposit := binary.LittleEndian.Uint32(raw[24:28])
			baseTax := binary.LittleEndian.Uint32(raw[28:32])
			protTs := binary.LittleEndian.Uint32(raw[32:36])

			// +0x28 = isProtected (1=protected, 0=not), +0x29 = isDemolition (1=demolition, 0=not)
			isDemolition := raw[0x29] == 0x01

			// Store with SENT TlId (not response TlId — they differ)
			rawCopy := make([]byte, len(raw))
			copy(rawCopy, raw)

			h.taxMutex.Lock()
			h.taxInfo[house.TlId] = taxData{
				Protected:   int64(protTs),
				Deposit:     deposit,
				BaseTax:     baseTax,
				PosX:        house.PosX,
				PosY:        house.PosY,
				PosZ:        house.PosZ,
				ObjId:       house.ObjId,
				Owner:       house.Owner,
				RawResponse: rawCopy,
				Demolition:  isDemolition,
			}
			h.taxMutex.Unlock()

			received++
			gotResponse = true

			if isDemolition {
				tsTime := time.Unix(int64(protTs), 0)
				remaining := time.Until(tsTime)
				days := int(remaining.Hours() / 24)
				hours := int(remaining.Hours()) % 24
				fmt.Printf("[HOUSES] DEMOLITION TlId=%d owner=%q deposit=%d tax=%d demolitionAt=%s (%dd%dh)\n",
					house.TlId, house.Owner, deposit, baseTax, tsTime.Format("2006-01-02 15:04"), days, hours)
				// Alert: urgent siren for demolition houses
				procBeep.Call(1500, 200)
				time.Sleep(80 * time.Millisecond)
				procBeep.Call(1800, 200)
				time.Sleep(80 * time.Millisecond)
				procBeep.Call(2000, 200)
			} else {
				fmt.Printf("[HOUSES] PROTECTED TlId=%d owner=%q deposit=%d tax=%d",
					house.TlId, house.Owner, deposit, baseTax)
				if protTs > 0 {
					protTime := time.Unix(int64(protTs), 0)
					remaining := time.Until(protTime)
					days := int(remaining.Hours() / 24)
					hours := int(remaining.Hours()) % 24
					fmt.Printf(" protectedUntil=%s (%dd%dh)", protTime.Format("2006-01-02"), days, hours)
				}
				fmt.Println()
			}
			break // got response, move to next house
		}
		if !gotResponse {
			fmt.Printf("[HOUSES] GAVE UP on TlId=%d after %d attempts\n", house.TlId, maxRetries)
		}
	}

	fmt.Printf("[HOUSES] Sent %d, received %d responses\n", len(toSend), received)

	if received > 0 {
		h.saveScannedHouses()
	}
}

// saveScannedHouses merges taxInfo into the in-memory houseDB and exports to Lua.
func (h *HouseESPManager) saveScannedHouses() {
	h.taxMutex.Lock()
	taxCopy := make(map[uint32]taxData, len(h.taxInfo))
	for k, v := range h.taxInfo {
		taxCopy[k] = v
	}
	h.taxMutex.Unlock()

	if len(taxCopy) == 0 {
		return
	}

	h.dbMutex.Lock()

	// Index existing entries by TlId
	existingByTlId := make(map[uint32]int, len(h.houseDB))
	for i, e := range h.houseDB {
		if e.TlId > 0 {
			existingByTlId[e.TlId] = i
		}
	}

	for tlId, td := range taxCopy {
		var rawHex string
		if len(td.RawResponse) > 0 {
			rawHex = hex.EncodeToString(td.RawResponse)
		}

		if idx, exists := existingByTlId[tlId]; exists {
			// Re-scan: only update CS222 data, preserve coordinates
			e := &h.houseDB[idx]
			e.Protected = td.Protected
			e.Deposit = td.Deposit
			e.Tax = td.BaseTax
			e.Owner = td.Owner
			e.Demolition = td.Demolition
			e.CS222Raw = rawHex
			e.ScanTime = time.Now().Unix()
			if !td.Demolition {
				e.RecheckAt = time.Now().Unix() + 7*24*3600
			}
		} else {
			// First scan: create full entry with coordinates
			entry := HouseEntry{
				TlId:       tlId,
				Protected:  td.Protected,
				Deposit:    td.Deposit,
				Tax:        td.BaseTax,
				Scanned:    true,
				ScanTime:   time.Now().Unix(),
				X:          td.PosX,
				Y:          td.PosY,
				Z:          td.PosZ,
				ObjId:      td.ObjId,
				Owner:      td.Owner,
				Demolition: td.Demolition,
				CS222Raw:   rawHex,
			}
			if !td.Demolition {
				entry.RecheckAt = time.Now().Unix() + 7*24*3600
			}
			if h.calReady {
				entry.MapX, entry.MapZ = h.worldToMapCoords(td.PosX, td.PosZ)
				entry.Sextant = mapToSextant(float64(entry.MapX), float64(entry.MapZ))
			}
			h.houseDB = append(h.houseDB, entry)
		}
	}

	// Rebuild lookup maps from the full slice
	h.scannedByObjId = make(map[uint32]*HouseEntry, len(h.houseDB))
	h.scannedByTlId = make(map[uint32]*HouseEntry, len(h.houseDB))
	for i := range h.houseDB {
		e := &h.houseDB[i]
		if e.ObjId > 0 {
			h.scannedByObjId[e.ObjId] = e
		}
		if e.TlId > 0 {
			h.scannedByTlId[e.TlId] = e
		}
	}

	// Invalidate panel caches so they pick up new data
	h.allHousesLastCheck = time.Time{}
	h.recheckLastCheck = time.Time{}
	h.demolitionLastCheck = time.Time{}

	h.dbMutex.Unlock()

	fmt.Printf("[HOUSES] Updated in-memory DB: %d houses\n", len(h.houseDB))

	// Export to Lua for in-game addon
	if h.luaExportPath != "" {
		h.dbMutex.RLock()
		houses := make([]HouseEntry, len(h.houseDB))
		copy(houses, h.houseDB)
		h.dbMutex.RUnlock()
		h.exportToLua(houses)
	}
}

// SetLuaExportPath sets the path for exporting house data as a Lua table
// that can be read by the in-game addon via api.File:Read().
func (h *HouseESPManager) SetLuaExportPath(path string) {
	h.luaExportPath = path
	fmt.Printf("[HOUSES] Lua export path set to: %s\n", path)
}

// exportToLua writes all scanned houses to a Lua table file.
// Format: { {tl_id=N, sextant=[[...]], owner=[[...]], ...}, ... }
func (h *HouseESPManager) exportToLua(houses []HouseEntry) {
	var b strings.Builder
	b.WriteString("{\n")

	count := 0
	for _, e := range houses {
		if !e.Scanned || e.Sextant == "" {
			continue
		}

		b.WriteString("    {\n")
		fmt.Fprintf(&b, "        tl_id = %d,\n", e.TlId)
		fmt.Fprintf(&b, "        sextant = [[%s]],\n", e.Sextant)
		fmt.Fprintf(&b, "        owner = [[%s]],\n", e.Owner)
		fmt.Fprintf(&b, "        timestamp = %d,\n", e.Protected)
		fmt.Fprintf(&b, "        deposit = %d,\n", e.Deposit)
		fmt.Fprintf(&b, "        tax = %d,\n", e.Tax)
		if e.Demolition {
			b.WriteString("        is_protected = false,\n")
			b.WriteString("        is_demolition = true,\n")
		} else {
			b.WriteString("        is_protected = true,\n")
			b.WriteString("        is_demolition = false,\n")
		}
		if e.CS222Raw != "" {
			fmt.Fprintf(&b, "        cs222_raw = [[%s]],\n", e.CS222Raw)
		}
		b.WriteString("    },\n")
		count++
	}

	b.WriteString("}\n")

	if err := os.WriteFile(h.luaExportPath, []byte(b.String()), 0644); err != nil {
		fmt.Printf("[HOUSES] Failed to export Lua: %v\n", err)
		return
	}
	fmt.Printf("[HOUSES] Exported %d houses to %s\n", count, h.luaExportPath)
}

// ============================================================================
// DoodadManager tree walking
// ============================================================================

func (h *HouseESPManager) walkDoodadTree(maxNodes int) []doodadTreeNode {
	// Step 1: doodadMgrPtr = *(*x2game + OFF_DOODAD_MGR)
	ptrAddr := h.x2game + OFF_DOODAD_MGR
	globalAddr := h.readU32(ptrAddr)
	if globalAddr == 0 {
		if h.firstScan {
			fmt.Printf("[HOUSES] FAIL: readU32(x2game+0x%X) = 0\n", OFF_DOODAD_MGR)
		}
		return nil
	}

	mgrAddr := h.readU32(uintptr(globalAddr))
	if mgrAddr == 0 {
		if h.firstScan {
			fmt.Printf("[HOUSES] FAIL: readU32(0x%X) = 0 (DoodadManager NULL)\n", globalAddr)
		}
		return nil
	}

	if h.firstScan {
		fmt.Printf("[HOUSES] DoodadManager @ 0x%X\n", mgrAddr)
	}

	// Step 2: tree0_header = readU32(mgrAddr + 8), count = readU32(mgrAddr + 12)
	hdrPtr := h.readU32(uintptr(mgrAddr) + DOODAD_TREE_HDR_OFF)
	treeCount := h.readU32(uintptr(mgrAddr) + DOODAD_TREE_COUNT_OFF)

	if h.firstScan {
		fmt.Printf("[HOUSES] Tree0: header=0x%X count=%d\n", hdrPtr, treeCount)
	}

	if hdrPtr == 0 {
		if h.firstScan {
			fmt.Println("[HOUSES] FAIL: tree header is NULL")
		}
		return nil
	}

	// root = readU32(header + 4)
	root := h.readU32(uintptr(hdrPtr) + 4)
	if root == 0 || root == hdrPtr {
		if h.firstScan {
			fmt.Printf("[HOUSES] FAIL: root=0x%X (header=0x%X)\n", root, hdrPtr)
		}
		return nil
	}

	if h.firstScan {
		fmt.Printf("[HOUSES] Tree0 root: 0x%X\n", root)
	}

	var results []doodadTreeNode
	visited := make(map[uint32]bool)

	var inorder func(nodeAddr uint32, depth int)
	inorder = func(nodeAddr uint32, depth int) {
		if depth > 50 || len(results) >= maxNodes {
			return
		}
		if nodeAddr == 0 || nodeAddr == hdrPtr || visited[nodeAddr] {
			return
		}
		visited[nodeAddr] = true

		// Read node: left(4) + parent(4) + right(4) + key(4) + data(4) + color(1) + isNil(1) = 22 bytes
		buf := make([]byte, 22)
		if !h.readBytes(uintptr(nodeAddr), buf) {
			return
		}

		// isNil flag at offset 21
		if buf[DOODAD_TREE_ISNIL_OFF] != 0 {
			return
		}

		left := binary.LittleEndian.Uint32(buf[0:4])
		right := binary.LittleEndian.Uint32(buf[8:12])
		key := binary.LittleEndian.Uint32(buf[12:16])
		dataPtr := binary.LittleEndian.Uint32(buf[16:20])

		inorder(left, depth+1)
		results = append(results, doodadTreeNode{Key: key, DataPtr: dataPtr})
		inorder(right, depth+1)
	}

	inorder(root, 0)

	if h.firstScan {
		fmt.Printf("[HOUSES] Walked %d doodad nodes\n", len(results))
	}

	return results
}

// ============================================================================
// Housing Manager tree walking (for owner names)
// ============================================================================

type housingTreeNode struct {
	TlId    uint32
	InfoPtr uint32
}

func (h *HouseESPManager) walkHousingTree() []housingTreeNode {
	globalAddr := h.readU32(h.x2game + OFF_HOUSING_MGR)
	if globalAddr == 0 {
		return nil
	}
	mgrAddr := h.readU32(uintptr(globalAddr))
	if mgrAddr == 0 {
		return nil
	}

	sentinel := h.readU32(uintptr(mgrAddr) + 0x110)

	var bestNodes []housingTreeNode
	for _, hdrOff := range []uintptr{0x108, 0x10C, 0x110} {
		hdrPtr := h.readU32(uintptr(mgrAddr) + hdrOff)
		if hdrPtr == 0 {
			continue
		}
		root := h.readU32(uintptr(hdrPtr) + 4)
		if root == 0 || root == hdrPtr {
			continue
		}

		var nodes []housingTreeNode
		visited := make(map[uint32]bool)

		var walk func(uint32, int)
		walk = func(nodeAddr uint32, depth int) {
			if depth > 50 || len(nodes) >= 10000 {
				return
			}
			if nodeAddr == 0 || nodeAddr == sentinel || nodeAddr == hdrPtr || visited[nodeAddr] {
				return
			}
			visited[nodeAddr] = true

			buf := make([]byte, 22)
			if !h.readBytes(uintptr(nodeAddr), buf) {
				return
			}
			if buf[21] != 0 {
				return
			}

			left := binary.LittleEndian.Uint32(buf[0:4])
			right := binary.LittleEndian.Uint32(buf[8:12])

			walk(left, depth+1)
			nodes = append(nodes, housingTreeNode{
				TlId:    binary.LittleEndian.Uint32(buf[12:16]),
				InfoPtr: binary.LittleEndian.Uint32(buf[16:20]),
			})
			walk(right, depth+1)
		}
		walk(root, 0)

		if len(nodes) > len(bestNodes) {
			bestNodes = nodes
		}
	}

	if h.firstScan {
		fmt.Printf("[HOUSES] Housing tree: %d entries\n", len(bestNodes))
	}

	return bestNodes
}

// ============================================================================
// Coordinate decryption
// ============================================================================

func (h *HouseESPManager) decryptDoodadCoords(doodadPtr uint32) (posX, posY, posZ float32, ok bool) {
	raw := make([]byte, 32)
	if !h.readBytes(uintptr(doodadPtr)+uintptr(DOODAD_ENCRYPTED_OFF), raw) {
		return
	}

	addr32 := doodadPtr
	byte1 := (addr32 >> 8) & 0xFF
	keyBase := addr32 * byte1

	dec := make([]byte, 32)
	for i := 0; i < 32; i++ {
		dec[i] = raw[i] ^ byte(keyBase*uint32(i+32))
	}

	// Decrypted layout:
	//   [0:16]  = quaternion (rotation)
	//   [16:20] = decX → game X (east-west)
	//   [20:24] = decY → game Z (north-south)
	//   [24:28] = decZ → game Y (height/altitude)
	decX := math.Float32frombits(binary.LittleEndian.Uint32(dec[16:20]))
	decY := math.Float32frombits(binary.LittleEndian.Uint32(dec[20:24]))
	decZ := math.Float32frombits(binary.LittleEndian.Uint32(dec[24:28]))

	// Map to game entity convention (same as EntityInfo / GetPlayerPosition):
	//   PosX = game X (east-west)      = decX [16:20]
	//   PosY = game Y (height)         = decZ [24:28]
	//   PosZ = game Z (north-south)    = decY [20:24]
	posX = decX
	posY = decZ  // height
	posZ = decY  // north-south

	// Sanity: horizontal coords (decX, decY) should be in world range
	if decX > 0 && decX < 50000 && decY > 0 && decY < 50000 {
		ok = true
	}
	return
}

// ============================================================================
// loadHouseDB loads from houses.json on startup (migration)
// ============================================================================

func (h *HouseESPManager) loadHouseDB() {
	// Migration: load from houses.json if it exists (one-time import)
	data, err := os.ReadFile("houses.json")
	if err != nil {
		fmt.Println("[HOUSES] No houses.json found, starting with empty DB")
		h.dbLoaded = false
		return
	}

	var db HouseDB
	if err := json.Unmarshal(data, &db); err != nil {
		fmt.Printf("[HOUSES] Failed to parse houses.json: %v\n", err)
		h.dbLoaded = false
		return
	}

	h.dbMutex.Lock()
	h.houseDB = db.Houses
	h.scannedByObjId = make(map[uint32]*HouseEntry, len(h.houseDB))
	h.scannedByTlId = make(map[uint32]*HouseEntry, len(h.houseDB))
	for i := range h.houseDB {
		entry := &h.houseDB[i]
		if entry.ObjId > 0 {
			h.scannedByObjId[entry.ObjId] = entry
		}
		if entry.TlId > 0 {
			h.scannedByTlId[entry.TlId] = entry
		}
	}
	h.dbMutex.Unlock()

	h.dbLoaded = true
	fmt.Printf("[HOUSES] Loaded %d entries from houses.json (migration)\n", len(h.houseDB))
}

func (h *HouseESPManager) isHouseScanned(objId, tlId uint32) (*HouseEntry, bool) {
	h.dbMutex.RLock()
	defer h.dbMutex.RUnlock()

	if entry, ok := h.scannedByObjId[objId]; ok && entry.Scanned {
		return entry, true
	}
	if tlId > 0 {
		if entry, ok := h.scannedByTlId[tlId]; ok && entry.Scanned {
			return entry, true
		}
	}
	return nil, false
}

// ============================================================================
// Background scan loop
// ============================================================================

func (h *HouseESPManager) scanLoop() {
	h.running = true
	defer func() {
		h.running = false
		if r := recover(); r != nil {
			fmt.Printf("[HOUSES] PANIC in scanLoop: %v\n", r)
		}
	}()

	h.loadHouseDB()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopChan:
			return
		case <-ticker.C:
			if h.enabled {
				h.scanOnce()
			}
		}
	}
}

func (h *HouseESPManager) scanOnce() {
	playerX, playerY, playerZ, hasPlayer := h.mainManager.GetPlayerPosition()
	if !hasPlayer {
		if h.firstScan {
			fmt.Println("[HOUSES] FAIL: no player position")
		}
		return
	}

	if h.firstScan {
		fmt.Printf("[HOUSES] Player pos: (%.1f, %.1f, %.1f)\n", playerX, playerY, playerZ)
	}

	// Step 1: Housing Manager → map[DbId] = {TlId, owner}
	housingNodes := h.walkHousingTree()
	type housingInfo struct {
		TlId  uint32
		Owner string
	}
	housingByDbId := make(map[uint32]housingInfo, len(housingNodes))
	for _, hn := range housingNodes {
		if hn.InfoPtr < 0x10000 {
			continue
		}
		dbId := h.readU32(uintptr(hn.InfoPtr) + HOUSING_DBID_OFF)
		if dbId == 0 {
			continue
		}
		owner := h.readString(uintptr(hn.InfoPtr)+HOUSING_OWNER_OFF, 64)
		if len(owner) >= 50 {
			owner = ""
		}
		housingByDbId[dbId] = housingInfo{TlId: hn.TlId, Owner: owner}
	}

	// Step 2: DoodadManager → filter type=3 → read DbId at +0x450 → lookup in housing map
	// Deduplicate: only first doodad per DbId (furniture shares same DbId as the house plot)
	doodadNodes := h.walkDoodadTree(5000)
	var houses []HouseRenderInfo
	matched := 0
	usedDbIds := make(map[uint32]bool, len(housingByDbId))

	for _, node := range doodadNodes {
		if node.DataPtr < 0x10000 {
			continue
		}
		doodadType := h.readU32(uintptr(node.DataPtr) + uintptr(DOODAD_TYPE_OFF))
		if doodadType != 3 {
			continue
		}
		dbId := h.readU32(uintptr(node.DataPtr) + uintptr(DOODAD_DBID_OFF))
		hi, found := housingByDbId[dbId]
		if !found {
			continue
		}
		matched++
		if usedDbIds[dbId] {
			continue // already have a doodad for this house
		}
		usedDbIds[dbId] = true

		posX, posY, posZ, ok := h.decryptDoodadCoords(node.DataPtr)
		if !ok {
			continue
		}
		dist := CalculateDistance(playerX, playerY, playerZ, posX, posY, posZ)
		if dist > h.maxRange {
			continue
		}
		// Lookup scanned status: check taxInfo first, then DB (both under their respective locks)
		var protectedTs int64
		scanned := false

		h.taxMutex.Lock()
		if td, ok := h.taxInfo[hi.TlId]; ok {
			protectedTs = td.Protected
			scanned = true
		}
		h.taxMutex.Unlock()

		if !scanned {
			h.dbMutex.RLock()
			if entry, ok := h.scannedByTlId[hi.TlId]; ok && entry.Scanned {
				protectedTs = entry.Protected
				scanned = true
			}
			h.dbMutex.RUnlock()
		}

		houses = append(houses, HouseRenderInfo{
			ObjId:     node.Key,
			TlId:      hi.TlId,
			PosX:      posX,
			PosY:      posY,
			PosZ:      posZ,
			Owner:     hi.Owner,
			Distance:  dist,
			Protected: protectedTs,
			Scanned:   scanned,
		})
	}

	if h.firstScan {
		fmt.Printf("[HOUSES] Housing: %d, Doodads: %d, type3+dbid match: %d, unique houses: %d, in range: %d\n",
			len(housingByDbId), len(doodadNodes), matched, len(usedDbIds), len(houses))
		for i, house := range houses {
			if i >= 5 {
				break
			}
			fmt.Printf("[HOUSES]   TlId=%d owner=%q pos=(%.1f,%.1f,%.1f) dist=%.0fm\n",
				house.TlId, house.Owner, house.PosX, house.PosY, house.PosZ, house.Distance)
		}
		h.firstScan = false
	}

	// Update cache immediately (ESP rendering reads from this)
	h.cacheMutex.Lock()
	h.cachedHouses = houses
	h.cacheMutex.Unlock()

	// Fire-and-forget: queue nearby houses for CS222 query in background
	if h.caveAddr != 0 && !h.sending {
		var toSend []HouseRenderInfo
		h.taxMutex.Lock()
		for _, house := range houses {
			if house.Distance > AUTOSCAN_RANGE {
				continue
			}
			// Skip only if already got a successful response
			if _, ok := h.taxInfo[house.TlId]; ok {
				continue
			}
			toSend = append(toSend, house)
		}
		h.taxMutex.Unlock()
		if len(toSend) > 0 {
			h.sending = true
			fmt.Printf("[HOUSES] Queuing %d CS222 sends\n", len(toSend))
			go h.backgroundSendAndCollect(toSend)
		}
	}
}

// ============================================================================
// Public API
// ============================================================================

func (h *HouseESPManager) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.enabled {
		return
	}

	// Setup code cave for packet sending
	if err := h.setupCodeCave(); err != nil {
		fmt.Printf("[HOUSES] WARNING: code cave setup failed: %v (auto-scan disabled)\n", err)
	} else {
		h.installHandlerHook()
		h.installTickHook()

		// Calibrate world→map offset once (for saving map coords in JSON)
		if err := h.calibrateOnce(); err != nil {
			fmt.Printf("[HOUSES] WARNING: calibration failed: %v (MAP coords won't be saved)\n", err)
		}
	}

	h.enabled = true
	h.firstScan = true
	go h.scanLoop()
	fmt.Println("[HOUSES] Started")
}

func (h *HouseESPManager) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.enabled {
		return
	}
	h.enabled = false
	select {
	case h.stopChan <- true:
	default:
	}

	h.cleanupCodeCave()
	fmt.Println("[HOUSES] Stopped")
}

func (h *HouseESPManager) IsEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

func (h *HouseESPManager) Toggle() bool {
	h.mu.Lock()
	wasEnabled := h.enabled
	h.mu.Unlock()

	if wasEnabled {
		h.Stop()
	} else {
		h.Start()
	}
	return !wasEnabled
}

func (h *HouseESPManager) NextFilterType() int32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.filterType++
	if h.filterType > 30 {
		h.filterType = 0
	}
	h.firstScan = true // re-print debug
	fmt.Printf("[HOUSES] Filter type = %d\n", h.filterType)
	return h.filterType
}

func (h *HouseESPManager) ReloadDB() {
	// Invalidate caches so panels refresh
	h.allHousesLastCheck = time.Time{}
	h.recheckLastCheck = time.Time{}
	h.demolitionLastCheck = time.Time{}
	h.firstScan = true
	go h.scanOnce()
}

func (h *HouseESPManager) GetVisibleHouses() []HouseRenderInfo {
	h.cacheMutex.RLock()
	defer h.cacheMutex.RUnlock()

	if len(h.cachedHouses) == 0 {
		return nil
	}

	var result []HouseRenderInfo
	for _, house := range h.cachedHouses {
		if house.Scanned && !h.showScanned {
			continue
		}
		if !house.Scanned && !h.showUnscanned {
			continue
		}
		result = append(result, house)
	}
	return result
}

func (h *HouseESPManager) GetMaxRange() float32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.maxRange
}

func (h *HouseESPManager) SetMaxRange(r float32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.maxRange = r
}

func (h *HouseESPManager) ToggleShowScanned() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.showScanned = !h.showScanned
	return h.showScanned
}

func (h *HouseESPManager) ToggleShowUnscanned() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.showUnscanned = !h.showUnscanned
	return h.showUnscanned
}

func (h *HouseESPManager) GetFilters() (scanned, unscanned bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.showScanned, h.showUnscanned
}

func (h *HouseESPManager) GetHouseCount() (total, scanned int) {
	h.cacheMutex.RLock()
	defer h.cacheMutex.RUnlock()
	total = len(h.cachedHouses)
	for _, e := range h.cachedHouses {
		if e.Scanned {
			scanned++
		}
	}
	return
}

// GetAllHouses returns all houses from in-memory DB, cached for 30s.
// Sorted by protection time ascending (soonest expiry first).
func (h *HouseESPManager) GetAllHouses() []HouseEntry {
	now := time.Now()
	if now.Sub(h.allHousesLastCheck) < 30*time.Second && h.allHousesCache != nil {
		return h.allHousesCache
	}

	h.dbMutex.RLock()
	houses := make([]HouseEntry, len(h.houseDB))
	copy(houses, h.houseDB)
	h.dbMutex.RUnlock()

	// Sort by protection time (soonest first, unprotected last)
	for i := 0; i < len(houses); i++ {
		for j := i + 1; j < len(houses); j++ {
			pi, pj := houses[i].Protected, houses[j].Protected
			if pi > 0 && pj > 0 {
				if pj < pi {
					houses[i], houses[j] = houses[j], houses[i]
				}
			} else if pi == 0 && pj > 0 {
				houses[i], houses[j] = houses[j], houses[i]
			}
		}
	}

	h.allHousesCache = houses
	h.allHousesLastCheck = now
	return houses
}

// ============================================================================
// Recheck Panel — protected houses that need rescanning
// ============================================================================

func (h *HouseESPManager) ToggleRecheckPanel() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.showRecheckPanel = !h.showRecheckPanel
	h.recheckScroll = 0
	return h.showRecheckPanel
}

func (h *HouseESPManager) IsRecheckPanelVisible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.showRecheckPanel
}

func (h *HouseESPManager) ScrollRecheck(delta int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recheckScroll += delta
	if h.recheckScroll < 0 {
		h.recheckScroll = 0
	}
}

func (h *HouseESPManager) GetRecheckScroll() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.recheckScroll
}

// GetRecheckHouses returns protected (non-demolition) houses where recheck_at <= now.
// Sorted by recheck_at ascending (most overdue first).
func (h *HouseESPManager) GetRecheckHouses() []HouseEntry {
	now := time.Now()
	if now.Sub(h.recheckLastCheck) < 30*time.Second && h.recheckCache != nil {
		return h.recheckCache
	}

	h.dbMutex.RLock()
	nowUnix := now.Unix()
	var recheck []HouseEntry
	for _, e := range h.houseDB {
		if !e.Demolition && e.RecheckAt > 0 && e.RecheckAt <= nowUnix {
			recheck = append(recheck, e)
		}
	}
	h.dbMutex.RUnlock()

	// Sort by recheck_at ascending (most overdue first)
	for i := 0; i < len(recheck); i++ {
		for j := i + 1; j < len(recheck); j++ {
			if recheck[j].RecheckAt < recheck[i].RecheckAt {
				recheck[i], recheck[j] = recheck[j], recheck[i]
			}
		}
	}

	h.recheckCache = recheck
	h.recheckLastCheck = now
	return recheck
}

// ============================================================================
// Demolition Panel — houses in demolition, filtered by day
// ============================================================================

func (h *HouseESPManager) ToggleDemolitionPanel() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.showDemolitionPanel = !h.showDemolitionPanel
	h.demolitionDayOffset = 0
	h.demolitionScroll = 0
	return h.showDemolitionPanel
}

func (h *HouseESPManager) IsDemolitionPanelVisible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.showDemolitionPanel
}

func (h *HouseESPManager) NextDemolitionDay() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.demolitionDayOffset++
	h.demolitionScroll = 0
	h.demolitionLastCheck = time.Time{} // invalidate cache
	return h.demolitionDayOffset
}

func (h *HouseESPManager) PrevDemolitionDay() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.demolitionDayOffset--
	h.demolitionScroll = 0
	h.demolitionLastCheck = time.Time{} // invalidate cache
	return h.demolitionDayOffset
}

func (h *HouseESPManager) GetDemolitionDayOffset() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.demolitionDayOffset
}

func (h *HouseESPManager) ScrollDemolition(delta int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.demolitionScroll += delta
	if h.demolitionScroll < 0 {
		h.demolitionScroll = 0
	}
}

func (h *HouseESPManager) GetDemolitionScroll() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.demolitionScroll
}

// GetDemolitionHouses returns demolition houses whose timestamp falls on the selected day.
// dayOffset=0 means today, 1=tomorrow, -1=yesterday, etc.
// Passing -999 means "all demolition houses" (no day filter).
func (h *HouseESPManager) GetDemolitionHouses() []HouseEntry {
	h.mu.Lock()
	dayOffset := h.demolitionDayOffset
	h.mu.Unlock()

	now := time.Now()
	if now.Sub(h.demolitionLastCheck) < 10*time.Second && h.demolitionCache != nil && h.demolitionCacheDay == dayOffset {
		return h.demolitionCache
	}

	targetDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, dayOffset)
	nextDay := targetDay.AddDate(0, 0, 1)

	h.dbMutex.RLock()
	var result []HouseEntry
	for _, e := range h.houseDB {
		if !e.Demolition || e.Protected == 0 {
			continue
		}
		demTime := time.Unix(e.Protected, 0)
		if demTime.After(targetDay) && demTime.Before(nextDay) {
			result = append(result, e)
		}
	}
	h.dbMutex.RUnlock()

	// Sort by demolition time ascending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Protected < result[i].Protected {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	h.demolitionCache = result
	h.demolitionCacheDay = dayOffset
	h.demolitionLastCheck = now
	return result
}

// GetAllDemolitionCount returns the total number of demolition houses in the DB.
func (h *HouseESPManager) GetAllDemolitionCount() int {
	houses := h.GetAllHouses()
	count := 0
	for _, e := range houses {
		if e.Demolition {
			count++
		}
	}
	return count
}

// GetProtectionText returns protection status text
func GetProtectionText(entry HouseRenderInfo) string {
	if entry.Protected == 0 {
		return ""
	}
	protTime := time.Unix(entry.Protected, 0)
	remaining := time.Until(protTime)
	if remaining <= 0 {
		return "EXP"
	}
	days := int(remaining.Hours() / 24)
	hours := int(remaining.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	return fmt.Sprintf("%dh", hours)
}

// mapBtnEntry stores position of a MAP button for panels
type mapBtnEntry struct {
	x, y, w, h int32
	sextant    string
}

// MarkOnMap converts sextant to map coordinates using the exact game formula
// and places a map marker at slot 1.
func (h *HouseESPManager) MarkOnMap(sextant string) {
	mapX, mapZ, ok := sextantToMapCoords(sextant)
	if !ok {
		fmt.Printf("[MAP] skipped: invalid sextant %q\n", sextant)
		return
	}

	markerMgr := h.x2game + OFF_MARKER_MGR
	slotAddr := markerMgr + uintptr((5*1+165)*4) // slot 1

	h.writeBytes(slotAddr, make([]byte, 20))

	buf := make([]byte, 20)
	buf[0] = 1
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(mapX))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(mapZ))
	wrote := h.writeBytes(slotAddr, buf)

	fmt.Printf("[MAP] sextant=%s → map(%.1f,%.1f) ok=%v\n", sextant, mapX, mapZ, wrote)

	// Debug: read back after 500ms to check if game overwrites the slot
	go func() {
		time.Sleep(500 * time.Millisecond)
		readBuf := make([]byte, 20)
		h.readBytes(slotAddr, readBuf)
		flag := readBuf[0]
		rx := math.Float32frombits(binary.LittleEndian.Uint32(readBuf[4:8]))
		rz := math.Float32frombits(binary.LittleEndian.Uint32(readBuf[8:12]))
		if rx != mapX || rz != mapZ {
			fmt.Printf("[MAP] ⚠ SLOT OVERWRITTEN! wrote(%.1f,%.1f) but now(%.1f,%.1f) flag=%d\n", mapX, mapZ, rx, rz, flag)
		} else {
			fmt.Printf("[MAP] ✓ slot intact after 500ms (%.1f,%.1f) flag=%d\n", rx, rz, flag)
		}
	}()
}

// ============================================================================
// Rendering (called from Manager.renderLoop)
// ============================================================================

func (m *Manager) renderHouses(playerX, playerY, playerZ float32) {
	if m.houseManager == nil || !m.houseManager.IsEnabled() {
		return
	}

	houses := m.houseManager.GetVisibleHouses()

	for _, hi := range houses {
		// Recalculate distance in real-time using current player position
		dist := CalculateDistance(playerX, playerY, playerZ, hi.PosX, hi.PosY, hi.PosZ)

		// WorldToScreen uses entity convention: (PosX, PosZ, PosY)
		// PosX = game X, PosZ = game Z (north-south), PosY = game Y (height)
		screenX, screenY, screenZ := m.WorldToScreen(hi.PosX, hi.PosZ, hi.PosY)

		isInvalidZ := math.IsNaN(float64(screenZ)) || math.IsInf(float64(screenZ), 0)
		if isInvalidZ || screenZ >= 1.0 {
			continue
		}

		if screenX < 0 || screenX > 100 || screenY < 0 || screenY > 100 {
			continue
		}

		pixelX := int32(screenX * float32(m.screenW) / 100.0)
		pixelY := int32(screenY * float32(m.screenH) / 100.0)

		if pixelX <= 0 || pixelX >= m.screenW || pixelY <= 0 || pixelY >= m.screenH {
			continue
		}

		// Green = scanned/mapped, Red = not yet mapped
		var color uintptr
		if hi.Scanned {
			color = COLOR_GREEN
		} else {
			color = COLOR_RED
		}

		// Draw house icon: small diamond
		size := int32(5)
		m.drawLine(pixelX, pixelY-size, pixelX+size, pixelY, color, 2)
		m.drawLine(pixelX+size, pixelY, pixelX, pixelY+size, color, 2)
		m.drawLine(pixelX, pixelY+size, pixelX-size, pixelY, color, 2)
		m.drawLine(pixelX-size, pixelY, pixelX, pixelY-size, color, 2)

		// Label
		owner := hi.Owner
		if len(owner) > 15 {
			owner = owner[:15]
		}

		protText := GetProtectionText(hi)
		if owner != "" {
			if protText != "" {
				label := fmt.Sprintf("%s %.0fm [%s]", owner, dist, protText)
				m.drawText(pixelX+8, pixelY-7, label, color)
			} else {
				label := fmt.Sprintf("%s %.0fm", owner, dist)
				m.drawText(pixelX+8, pixelY-7, label, color)
			}
		} else {
			label := fmt.Sprintf("H %.0fm", dist)
			m.drawText(pixelX+8, pixelY-7, label, color)
		}
	}
}

func (m *Manager) drawHouseFilterUI() {
	if m.houseManager == nil || !m.houseManager.IsEnabled() {
		return
	}

	panelW := int32(150)
	panelH := int32(120)
	panelX := int32(10)
	panelY := m.screenH - panelH - 10

	startX := panelX + 5
	startY := panelY + 5

	m.drawFilledRect(panelX, panelY, panelW, panelH, 0x00000000, 180)

	total, scanned := m.houseManager.GetHouseCount()
	m.drawText(startX, startY, fmt.Sprintf("Houses %d/%d", scanned, total), COLOR_WHITE)

	m.checkboxHouseScannedX = startX
	m.checkboxHouseScannedY = startY + 20
	m.checkboxHouseUnscannedX = startX
	m.checkboxHouseUnscannedY = startY + 42

	showScanned, showUnscanned := m.houseManager.GetFilters()

	m.drawCheckbox(m.checkboxHouseScannedX, m.checkboxHouseScannedY, showScanned)
	m.drawText(m.checkboxHouseScannedX+m.checkboxSize+5, m.checkboxHouseScannedY, "Mapped", COLOR_GREEN)

	m.drawCheckbox(m.checkboxHouseUnscannedX, m.checkboxHouseUnscannedY, showUnscanned)
	m.drawText(m.checkboxHouseUnscannedX+m.checkboxSize+5, m.checkboxHouseUnscannedY, "To Map", COLOR_RED)

	rangeY := startY + 68
	m.houseRangeDecBtnX = startX
	m.houseRangeDecBtnY = rangeY + 20
	m.houseRangeIncBtnX = startX + 110
	m.houseRangeIncBtnY = rangeY + 20

	rangeText := fmt.Sprintf("Range: %.0fm", m.houseManager.GetMaxRange())
	m.drawText(startX, rangeY, rangeText, COLOR_WHITE)
	m.drawButton(m.houseRangeDecBtnX, m.houseRangeDecBtnY, m.rangeBtnSize, "-")
	m.drawButton(m.houseRangeIncBtnX, m.houseRangeIncBtnY, m.rangeBtnSize, "+")
}

func (m *Manager) drawRecheckPanel() {
	if m.houseManager == nil || !m.houseManager.IsRecheckPanelVisible() {
		return
	}

	houses := m.houseManager.GetRecheckHouses()
	if len(houses) == 0 {
		// Show empty message
		panelX := int32(480)
		panelY := int32(150)
		panelW := int32(460)
		panelH := int32(30)
		m.recheckPanelX = panelX
		m.recheckPanelY = panelY
		m.recheckPanelW = panelW
		m.recheckPanelH = panelH
		m.drawFilledRect(panelX, panelY, panelW, panelH, 0x00000000, 200)
		m.drawText(panelX+5, panelY+4, "RECHECK - no houses due for recheck", COLOR_WHITE)
		return
	}

	const maxVisible = 20
	lineH := int32(16)
	btnW := int32(36)
	btnH := int32(14)
	scrollBtnW := int32(24)
	scrollBtnH := int32(16)
	panelW := int32(500)

	scroll := m.houseManager.GetRecheckScroll()
	if scroll > len(houses)-maxVisible {
		scroll = len(houses) - maxVisible
	}
	if scroll < 0 {
		scroll = 0
	}

	visible := houses
	if len(visible) > scroll {
		visible = visible[scroll:]
	}
	if len(visible) > maxVisible {
		visible = visible[:maxVisible]
	}

	panelH := lineH*int32(len(visible)+1) + 12 + scrollBtnH + 4
	panelX := int32(480)
	panelY := int32(150)

	m.recheckPanelX = panelX
	m.recheckPanelY = panelY
	m.recheckPanelW = panelW
	m.recheckPanelH = panelH

	m.drawFilledRect(panelX, panelY, panelW, panelH, 0x00000000, 200)

	titleX := panelX + 5
	titleY := panelY + 4
	m.drawText(titleX, titleY, fmt.Sprintf("RECHECK (%d) [%d-%d]", len(houses), scroll+1, scroll+len(visible)), COLOR_YELLOW)

	m.recheckMapBtns = m.recheckMapBtns[:0]

	now := time.Now()
	for i, e := range visible {
		y := titleY + lineH*int32(i+1)

		owner := e.Owner
		if len(owner) > 10 {
			owner = owner[:10]
		}

		sext := e.Sextant
		if len(sext) > 22 {
			sext = sext[:22]
		}
		if sext == "" {
			sext = fmt.Sprintf("%.0f,%.0f", e.X, e.Z)
		}

		// How overdue is the recheck
		overdue := now.Sub(time.Unix(e.RecheckAt, 0))
		overdueDays := int(overdue.Hours() / 24)
		var overdueLabel string
		if overdueDays > 0 {
			overdueLabel = fmt.Sprintf("%dd overdue", overdueDays)
		} else {
			overdueLabel = fmt.Sprintf("%dh overdue", int(overdue.Hours()))
		}

		// Last scan info
		scanAge := now.Sub(time.Unix(e.ScanTime, 0))
		scanDays := int(scanAge.Hours() / 24)
		scanLabel := fmt.Sprintf("scanned %dd ago", scanDays)

		label := fmt.Sprintf("%-10s %-22s %s (%s)", owner, sext, overdueLabel, scanLabel)
		m.drawText(titleX, y, label, COLOR_YELLOW)

		if e.Sextant != "" {
			bx := panelX + panelW - btnW - 5
			by := y
			m.drawFilledRect(bx, by, btnW, btnH, 0x00444444, 220)
			m.drawText(bx+6, by, "MAP", COLOR_WHITE)

			m.recheckMapBtns = append(m.recheckMapBtns, mapBtnEntry{
				x: bx, y: by, w: btnW, h: btnH,
				sextant: e.Sextant,
			})
		}
	}

	// Scroll buttons
	scrollY := titleY + lineH*int32(len(visible)+1) + 2
	upX := panelX + panelW/2 - scrollBtnW - 2
	downX := panelX + panelW/2 + 2

	m.drawFilledRect(upX, scrollY, scrollBtnW, scrollBtnH, 0x00444444, 220)
	m.drawText(upX+8, scrollY, "UP", COLOR_WHITE)
	m.recheckScrollUpBtn = [4]int32{upX, scrollY, scrollBtnW, scrollBtnH}

	m.drawFilledRect(downX, scrollY, scrollBtnW, scrollBtnH, 0x00444444, 220)
	m.drawText(downX+4, scrollY, "DN", COLOR_WHITE)
	m.recheckScrollDownBtn = [4]int32{downX, scrollY, scrollBtnW, scrollBtnH}
}

func (m *Manager) drawDemolitionPanel() {
	if m.houseManager == nil || !m.houseManager.IsDemolitionPanelVisible() {
		return
	}

	dayOffset := m.houseManager.GetDemolitionDayOffset()
	houses := m.houseManager.GetDemolitionHouses()
	totalDemo := m.houseManager.GetAllDemolitionCount()

	const maxVisible = 20
	lineH := int32(16)
	btnW := int32(36)
	btnH := int32(14)
	dayBtnW := int32(30)
	dayBtnH := int32(16)
	scrollBtnW := int32(24)
	scrollBtnH := int32(16)
	panelW := int32(500)

	scroll := m.houseManager.GetDemolitionScroll()
	if len(houses) > maxVisible && scroll > len(houses)-maxVisible {
		scroll = len(houses) - maxVisible
	}
	if scroll < 0 {
		scroll = 0
	}

	visible := houses
	if len(visible) > scroll {
		visible = visible[scroll:]
	}
	if len(visible) > maxVisible {
		visible = visible[:maxVisible]
	}

	rows := len(visible)
	if rows == 0 {
		rows = 1 // for "no houses" message
	}
	panelH := lineH*int32(rows+1) + 12 + scrollBtnH + 4
	panelX := m.screenW - panelW - 10
	panelY := int32(10)

	m.demolitionPanelX = panelX
	m.demolitionPanelY = panelY
	m.demolitionPanelW = panelW
	m.demolitionPanelH = panelH

	m.drawFilledRect(panelX, panelY, panelW, panelH, 0x00000000, 200)

	titleX := panelX + 5
	titleY := panelY + 4

	// Day label
	targetDay := time.Now().AddDate(0, 0, dayOffset)
	var dayLabel string
	switch dayOffset {
	case 0:
		dayLabel = "TODAY"
	case 1:
		dayLabel = "TOMORROW"
	case -1:
		dayLabel = "YESTERDAY"
	default:
		dayLabel = targetDay.Format("02/01")
	}

	title := fmt.Sprintf("DEMOLITION %s (%d) [total: %d]", dayLabel, len(houses), totalDemo)
	m.drawText(titleX, titleY, title, COLOR_RED)

	// PREV / NEXT day buttons
	prevX := panelX + panelW - dayBtnW*2 - 12
	nextX := panelX + panelW - dayBtnW - 5
	m.drawFilledRect(prevX, titleY, dayBtnW, dayBtnH, 0x00444444, 220)
	m.drawText(prevX+5, titleY, "<", COLOR_WHITE)
	m.demolitionPrevBtn = [4]int32{prevX, titleY, dayBtnW, dayBtnH}

	m.drawFilledRect(nextX, titleY, dayBtnW, dayBtnH, 0x00444444, 220)
	m.drawText(nextX+5, titleY, ">", COLOR_WHITE)
	m.demolitionNextBtn = [4]int32{nextX, titleY, dayBtnW, dayBtnH}

	// Reset MAP buttons
	m.demolitionMapBtns = m.demolitionMapBtns[:0]

	if len(houses) == 0 {
		y := titleY + lineH
		m.drawText(titleX, y, fmt.Sprintf("No houses demolishing on %s", targetDay.Format("02/01/2006")), COLOR_WHITE)
	} else {
		now := time.Now()
		for i, e := range visible {
			y := titleY + lineH*int32(i+1)

			owner := e.Owner
			if len(owner) > 10 {
				owner = owner[:10]
			}

			demTime := time.Unix(e.Protected, 0)
			remaining := demTime.Sub(now)
			timeStr := demTime.Format("15:04")

			var remLabel string
			var color uintptr
			if remaining <= 0 {
				remLabel = "FALLEN"
				color = 0x00888888 // gray
			} else {
				hours := int(remaining.Hours())
				mins := int(remaining.Minutes()) % 60
				remLabel = fmt.Sprintf("%dh%dm", hours, mins)
				color = COLOR_RED
			}

			sext := e.Sextant
			if len(sext) > 22 {
				sext = sext[:22]
			}
			if sext == "" {
				sext = fmt.Sprintf("%.0f,%.0f", e.X, e.Z)
			}

			label := fmt.Sprintf("%-10s %s %-7s %-22s", owner, timeStr, remLabel, sext)
			m.drawText(titleX, y, label, color)

			if e.Sextant != "" {
				bx := panelX + panelW - btnW - 5
				by := y
				m.drawFilledRect(bx, by, btnW, btnH, 0x00444444, 220)
				m.drawText(bx+6, by, "MAP", COLOR_WHITE)

				m.demolitionMapBtns = append(m.demolitionMapBtns, mapBtnEntry{
					x: bx, y: by, w: btnW, h: btnH,
					sextant: e.Sextant,
				})
			}
		}
	}

	// Scroll buttons
	scrollY := titleY + lineH*int32(rows+1) + 2
	upX := panelX + panelW/2 - scrollBtnW - 2
	downX := panelX + panelW/2 + 2

	m.drawFilledRect(upX, scrollY, scrollBtnW, scrollBtnH, 0x00444444, 220)
	m.drawText(upX+8, scrollY, "UP", COLOR_WHITE)
	m.demolitionScrollUpBtn = [4]int32{upX, scrollY, scrollBtnW, scrollBtnH}

	m.drawFilledRect(downX, scrollY, scrollBtnW, scrollBtnH, 0x00444444, 220)
	m.drawText(downX+4, scrollY, "DN", COLOR_WHITE)
	m.demolitionScrollDownBtn = [4]int32{downX, scrollY, scrollBtnW, scrollBtnH}
}