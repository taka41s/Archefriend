package esp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
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

	// Code cave layout
	CAVE_SIZE           uintptr = 0x8000    // 32KB
	CAVE_CAPT_COUNT     uintptr = 0x000     // uint32: captured response count
	CAVE_CAPT_CODE      uintptr = 0x080     // OnHouseTaxInfo hook shellcode
	CAVE_SEND_REQ       uintptr = 0x1000    // byte: send request flag
	CAVE_SEND_RES       uintptr = 0x1001    // byte: send response flag
	CAVE_SEND_PKT       uintptr = 0x1010    // 64-byte packet buffer
	CAVE_SEND_CODE      uintptr = 0x1200    // Tick hook shellcode
	CAVE_LOG_BASE       uintptr = 0x2000    // captured responses (256 × 64 bytes)

	// Auto-scan: only query houses within 15m of player
	AUTOSCAN_RANGE      float32 = 15.0
)

// ============================================================================
// House data structs
// ============================================================================

// HouseEntry represents a house in houses.json
type HouseEntry struct {
	TlId      uint32  `json:"tl_id"`
	ObjId     uint32  `json:"obj_id,omitempty"`
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	Z         float32 `json:"z"`
	Owner     string  `json:"owner,omitempty"`
	TemplID   uint32  `json:"templ_id,omitempty"`
	Protected int64   `json:"protected,omitempty"`
	Deposit   uint32  `json:"deposit,omitempty"`
	Tax       uint32  `json:"tax,omitempty"`
	Scanned   bool    `json:"scanned"`
	ScanTime  int64   `json:"scan_time,omitempty"`
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

	// houses.json cross-reference
	scannedByObjId map[uint32]*HouseEntry
	scannedByTlId  map[uint32]*HouseEntry
	dbFile         string
	dbLoaded       bool

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
}

func NewHouseESPManager(mainManager *Manager, processHandle uintptr, x2game uintptr) *HouseESPManager {
	return &HouseESPManager{
		mainManager:    mainManager,
		processHandle:  processHandle,
		x2game:         x2game,
		enabled:        false,
		dbFile:         "houses.json",
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

	// ── Shellcode 2: Tick hook — send CS 222 ──
	sc2 := []byte{}
	sc2 = append(sc2, 0x9C, 0x60)               // pushfd, pushad
	sc2 = append(sc2, 0x80, 0x3D)               // cmp byte [sendReqAddr], 1
	sc2 = append(sc2, le32(uint32(sendReqAddr))...)
	sc2 = append(sc2, 0x01)
	sc2 = append(sc2, 0x75, 0x00)               // jne skip (patched below)
	jnePos := len(sc2) - 1

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

	sc2[jnePos] = byte(len(sc2) - (jnePos + 1)) // patch jne offset
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

			// Store with SENT TlId (not response TlId — they differ)
			h.taxMutex.Lock()
			h.taxInfo[house.TlId] = taxData{
				Protected: int64(protTs),
				Deposit:   deposit,
				BaseTax:   baseTax,
				PosX:      house.PosX,
				PosY:      house.PosY,
				PosZ:      house.PosZ,
				ObjId:     house.ObjId,
				Owner:     house.Owner,
			}
			h.taxMutex.Unlock()

			received++
			gotResponse = true
			fmt.Printf("[HOUSES] Response for TlId=%d: deposit=%d tax=%d protectedTs=%d",
				house.TlId, deposit, baseTax, protTs)
			if protTs > 0 {
				protTime := time.Unix(int64(protTs), 0)
				remaining := time.Until(protTime)
				days := int(remaining.Hours() / 24)
				hours := int(remaining.Hours()) % 24
				fmt.Printf(" (until %s = %dd%dh)", protTime.Format("2006-01-02"), days, hours)
			}
			fmt.Println()

			// Alert: beep if < 3 days of protection remaining
			if protTs > 0 {
				remaining := time.Until(time.Unix(int64(protTs), 0))
				if remaining <= 0 {
					fmt.Printf("[HOUSES] ALERT: TlId=%d owner=%q protection EXPIRED!\n", house.TlId, house.Owner)
					procBeep.Call(1000, 300)
					time.Sleep(100 * time.Millisecond)
					procBeep.Call(1000, 300)
				} else if remaining < 72*time.Hour {
					days := int(remaining.Hours() / 24)
					hours := int(remaining.Hours()) % 24
					fmt.Printf("[HOUSES] ALERT: TlId=%d owner=%q protection expires in %dd%dh!\n", house.TlId, house.Owner, days, hours)
					procBeep.Call(800, 200)
				}
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

// saveScannedHouses persists all taxInfo entries to houses.json,
// merging with existing cached render data for coordinates/owner.
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

	// Load existing DB
	var db HouseDB
	data, err := os.ReadFile(h.dbFile)
	if err == nil {
		json.Unmarshal(data, &db)
	}
	db.Version = 1
	db.Updated = time.Now().Format("2006-01-02 15:04:05")

	// Index existing entries by TlId
	existingByTlId := make(map[uint32]int, len(db.Houses))
	for i, e := range db.Houses {
		if e.TlId > 0 {
			existingByTlId[e.TlId] = i
		}
	}

	changed := false
	for tlId, td := range taxCopy {
		entry := HouseEntry{
			TlId:      tlId,
			Protected: td.Protected,
			Deposit:   td.Deposit,
			Tax:       td.BaseTax,
			Scanned:   true,
			ScanTime:  time.Now().Unix(),
			X:         td.PosX,
			Y:         td.PosY,
			Z:         td.PosZ,
			ObjId:     td.ObjId,
			Owner:     td.Owner,
		}

		var entryIdx int
		if idx, exists := existingByTlId[tlId]; exists {
			db.Houses[idx] = entry
			entryIdx = idx
		} else {
			db.Houses = append(db.Houses, entry)
			entryIdx = len(db.Houses) - 1
		}
		changed = true

		// Update in-memory DB for immediate green rendering
		h.scannedByTlId[tlId] = &db.Houses[entryIdx]
	}

	if !changed {
		return
	}

	out, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		fmt.Printf("[HOUSES] Failed to marshal houses.json: %v\n", err)
		return
	}
	if err := os.WriteFile(h.dbFile, out, 0644); err != nil {
		fmt.Printf("[HOUSES] Failed to write houses.json: %v\n", err)
		return
	}
	fmt.Printf("[HOUSES] Saved %d houses to %s\n", len(db.Houses), h.dbFile)
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
// houses.json loading (for cross-reference)
// ============================================================================

func (h *HouseESPManager) loadHouseDB() {
	data, err := os.ReadFile(h.dbFile)
	if err != nil {
		fmt.Printf("[HOUSES] No %s found (will show all as 'To Map')\n", h.dbFile)
		h.dbLoaded = false
		return
	}

	var db HouseDB
	if err := json.Unmarshal(data, &db); err != nil {
		fmt.Printf("[HOUSES] Failed to parse %s: %v\n", h.dbFile, err)
		h.dbLoaded = false
		return
	}

	h.scannedByObjId = make(map[uint32]*HouseEntry)
	h.scannedByTlId = make(map[uint32]*HouseEntry)

	for i := range db.Houses {
		entry := &db.Houses[i]
		if entry.ObjId > 0 {
			h.scannedByObjId[entry.ObjId] = entry
		}
		if entry.TlId > 0 {
			h.scannedByTlId[entry.TlId] = entry
		}
	}

	h.dbLoaded = true
	fmt.Printf("[HOUSES] Loaded %d entries from %s\n", len(db.Houses), h.dbFile)
}

func (h *HouseESPManager) isHouseScanned(objId, tlId uint32) (*HouseEntry, bool) {
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
		// Lookup scanned status
		var protectedTs int64
		scanned := false
		h.taxMutex.Lock()
		if td, ok := h.taxInfo[hi.TlId]; ok {
			protectedTs = td.Protected
			scanned = true
		}
		h.taxMutex.Unlock()
		if !scanned {
			if entry, ok := h.scannedByTlId[hi.TlId]; ok && entry.Scanned {
				protectedTs = entry.Protected
				scanned = true
			}
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
	h.loadHouseDB()
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
