package esp

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// AllEntitiesManager manages all entities ESP separately
type AllEntitiesManager struct {
	// Process info
	processHandle uintptr
	x2game        uintptr
	mainManager   *Manager // Reference to main manager (for memory reading)

	// State
	enabled bool
	running bool
	mu      sync.Mutex

	// Cache
	cachedEntities []EntityInfo
	cacheMutex     sync.Mutex

	// Filters
	showPlayers bool
	showNPCs    bool
	showMates   bool
	maxRange    float32

	// Hook state
	hookInstalled     bool
	hookBuffer        uintptr
	hookTrampoline    uintptr
	hookOriginalBytes []byte

	// Control channels
	stopChan   chan bool
	pauseChan  chan bool
	resumeChan chan bool

	// Pause state
	paused bool
}

// NewAllEntitiesManager creates a new All Entities ESP manager
func NewAllEntitiesManager(processHandle uintptr, x2game uintptr, mainManager *Manager) *AllEntitiesManager {
	return &AllEntitiesManager{
		processHandle: processHandle,
		x2game:        x2game,
		mainManager:   mainManager,
		enabled:       false,
		showPlayers:   true,
		showNPCs:      true,
		showMates:     true,
		maxRange:      200.0,
		stopChan:      make(chan bool, 1),
		pauseChan:     make(chan bool, 1),
		resumeChan:    make(chan bool, 1),
	}
}

// Start inicia o All Entities ESP
func (aem *AllEntitiesManager) Start() {
	aem.mu.Lock()
	defer aem.mu.Unlock()

	if aem.enabled {
		return
	}

	aem.enabled = true
	aem.installHook()

	// Inicia goroutine dedicada
	go aem.updateLoop()

	fmt.Println("[ALL_ENTITIES] Started")
}

// Stop stops All Entities ESP
func (aem *AllEntitiesManager) Stop() {
	aem.mu.Lock()
	defer aem.mu.Unlock()

	if !aem.enabled {
		return
	}

	aem.enabled = false

	// Para a goroutine
	if aem.running {
		aem.stopChan <- true
		time.Sleep(20 * time.Millisecond)
	}

	aem.removeHook()

	fmt.Println("[ALL_ENTITIES] Stopped")
}

// Pause pausa temporariamente o All Entities ESP (para aimbot)
func (aem *AllEntitiesManager) Pause() {
	if !aem.enabled {
		return
	}
	// Limpa o cache imediatamente
	aem.cacheMutex.Lock()
	aem.cachedEntities = nil
	aem.cacheMutex.Unlock()

	select {
	case aem.pauseChan <- true:
	default:
	}
}

// Resume resume o All Entities ESP
func (aem *AllEntitiesManager) Resume() {
	if !aem.enabled {
		return
	}
	select {
	case aem.resumeChan <- true:
	default:
	}
}

// IsEnabled returns if enabled
func (aem *AllEntitiesManager) IsEnabled() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	return aem.enabled
}

// GetCachedEntities retorna entidades cacheadas (thread-safe)
func (aem *AllEntitiesManager) GetCachedEntities() []EntityInfo {
	aem.cacheMutex.Lock()
	defer aem.cacheMutex.Unlock()
	result := make([]EntityInfo, len(aem.cachedEntities))
	copy(result, aem.cachedEntities)
	return result
}

// ToggleShowPlayers toggles players filter
func (aem *AllEntitiesManager) ToggleShowPlayers() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	aem.showPlayers = !aem.showPlayers
	return aem.showPlayers
}

// ToggleShowNPCs toggles NPCs filter
func (aem *AllEntitiesManager) ToggleShowNPCs() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	aem.showNPCs = !aem.showNPCs
	return aem.showNPCs
}

// ToggleShowMates toggles mates filter
func (aem *AllEntitiesManager) ToggleShowMates() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	aem.showMates = !aem.showMates
	return aem.showMates
}

// GetShowPlayers returns players filter state
func (aem *AllEntitiesManager) GetShowPlayers() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	return aem.showPlayers
}

// GetShowNPCs returns NPCs filter state
func (aem *AllEntitiesManager) GetShowNPCs() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	return aem.showNPCs
}

// GetShowMates returns mates filter state
func (aem *AllEntitiesManager) GetShowMates() bool {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	return aem.showMates
}

// SetMaxRange sets max range
func (aem *AllEntitiesManager) SetMaxRange(r float32) {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	aem.maxRange = r
}

// GetMaxRange returns max range
func (aem *AllEntitiesManager) GetMaxRange() float32 {
	aem.mu.Lock()
	defer aem.mu.Unlock()
	return aem.maxRange
}

// updateLoop is the dedicated goroutine that continuously updates the cache
func (aem *AllEntitiesManager) updateLoop() {
	aem.running = true
	defer func() {
		aem.running = false
	}()

	ticker := time.NewTicker(16 * time.Millisecond) // 60 FPS
	defer ticker.Stop()

	for {
		select {
		case <-aem.stopChan:
			return

		case <-aem.pauseChan:
			aem.paused = true

		case <-aem.resumeChan:
			aem.paused = false

		case <-ticker.C:
			if !aem.paused {
				aem.updateCache()
			}
		}
	}
}

// updateCache atualiza o cache de entidades
func (aem *AllEntitiesManager) updateCache() {
	aem.mu.Lock()
	if !aem.enabled || !aem.hookInstalled {
		aem.mu.Unlock()
		return
	}
	hookBuffer := aem.hookBuffer
	aem.mu.Unlock()

	// Read ALL pointers from buffer (all 256 slots)
	collected := make(map[uint32]bool)
	for slot := uint32(0); slot < 256; slot++ {
		ptr := aem.mainManager.readU32(hookBuffer + 4 + uintptr(slot*4))
		if ptr != 0 && isValidPtr(ptr) {
			collected[ptr] = true
		}
	}

	// Process all collected entities
	if len(collected) > 0 {
		newEntities := aem.processCollectedEntities(collected)

		// Thread-safe update of cached entities
		aem.cacheMutex.Lock()
		aem.cachedEntities = newEntities
		aem.cacheMutex.Unlock()
	}
}

// processCollectedEntities processa ActorModel pointers em EntityInfo
func (aem *AllEntitiesManager) processCollectedEntities(collected map[uint32]bool) []EntityInfo {
	var entities []EntityInfo

	// Get player position for distance calc
	playerX, playerY, playerZ, hasPlayer := aem.mainManager.GetPlayerPosition()
	if !hasPlayer {
		return entities
	}

	aem.mu.Lock()
	maxRange := aem.maxRange
	aem.mu.Unlock()

	// Process collected entities
	for actorModel := range collected {
		unitId := aem.mainManager.readU32(uintptr(actorModel + 0x0C))
		entityPtr := aem.mainManager.readU32(uintptr(actorModel + 0x1F8))

		if !isValidPtr(entityPtr) {
			continue
		}

		// Read position
		posX := aem.mainManager.readFloat32(uintptr(entityPtr + 0x830))
		posZ := aem.mainManager.readFloat32(uintptr(entityPtr + 0x834))
		posY := aem.mainManager.readFloat32(uintptr(entityPtr + 0x838))

		// Validate position
		if posX < 100 || posX > 50000 {
			continue
		}
		if !isValidCoord(posX) || !isValidCoord(posY) || !isValidCoord(posZ) {
			continue
		}

		// Read HP
		hp := aem.mainManager.readU32(uintptr(entityPtr + 0x84C))
		if hp < 100 || hp > 10000000 {
			continue
		}

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, posX, posY, posZ)

		// Skip local player (distance too close)
		if distance < 1.0 {
			continue
		}

		// Filter by max range
		if distance > maxRange {
			continue
		}

		// Read name
		name := aem.mainManager.getEntityName(entityPtr)
		if !isValidEntityName(name) {
			// Try alternative name reading
			name = ""
			for _, off := range []uint32{0x1C, 0x20, 0x24, 0x28} {
				ptr := aem.mainManager.readU32(uintptr(entityPtr + off))
				if isValidPtr(ptr) {
					s := aem.mainManager.readString(uintptr(ptr), 32)
					if len(s) > 2 && len(s) < 32 {
						alphaCount := 0
						valid := true
						for _, c := range s {
							if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
								alphaCount++
							} else if c < 32 {
								valid = false
								break
							}
						}
						if valid && alphaCount >= 2 {
							name = s
							break
						}
					}
				}
			}
		}

		// Read MaxHP
		maxHP := aem.mainManager.getMaxHP(entityPtr)

		// Determine type
		isNPC := len(name) > 0 && (name[0] < 'A' || name[0] > 'Z')
		if len(name) > 0 {
			for _, c := range name {
				if c == ' ' {
					isNPC = true
					break
				}
			}
		}

		// Check mate status (simplified)
		isMate := false
		// TODO: Add mate detection logic if needed

		entities = append(entities, EntityInfo{
			Address:  entityPtr,
			VTable:   0,
			EntityID: unitId,
			Name:     name,
			PosX:     posX,
			PosY:     posY,
			PosZ:     posZ,
			HP:       hp,
			MaxHP:    maxHP,
			Distance: distance,
			IsPlayer: !isNPC,
			IsNPC:    isNPC,
			IsMate:   isMate,
		})
	}

	return entities
}

// installHook installs the memory hook
func (aem *AllEntitiesManager) installHook() {
	if aem.hookInstalled {
		return
	}

	updateAddr := aem.x2game + 0x0E3FD0

	// Save original bytes
	aem.hookOriginalBytes = make([]byte, 16)
	var bytesRead uintptr
	procReadProcessMemory.Call(aem.processHandle, updateAddr, uintptr(unsafe.Pointer(&aem.hookOriginalBytes[0])), 16, uintptr(unsafe.Pointer(&bytesRead)))

	// Allocate buffer
	aem.hookBuffer, _, _ = procVirtualAllocEx.Call(aem.processHandle, 0, 4+256*4, 0x1000|0x2000, 0x40)
	if aem.hookBuffer == 0 {
		fmt.Println("[ALL_ENTITIES] Failed to allocate buffer")
		return
	}

	// Zero the buffer
	zeros := make([]byte, 4+256*4)
	var written uintptr
	procWriteProcessMemory.Call(aem.processHandle, aem.hookBuffer, uintptr(unsafe.Pointer(&zeros[0])), uintptr(len(zeros)), uintptr(unsafe.Pointer(&written)))

	// Allocate trampoline
	aem.hookTrampoline, _, _ = procVirtualAllocEx.Call(aem.processHandle, 0, 64, 0x1000|0x2000, 0x40)
	if aem.hookTrampoline == 0 {
		fmt.Println("[ALL_ENTITIES] Failed to allocate trampoline")
		procVirtualFreeEx.Call(aem.processHandle, aem.hookBuffer, 0, 0x8000)
		return
	}

	// Build trampoline shellcode
	code := make([]byte, 64)
	i := 0
	code[i] = 0x50; i++ // push eax
	code[i] = 0x53; i++ // push ebx
	code[i] = 0xA1; i++ // mov eax, [buffer]
	binary.LittleEndian.PutUint32(code[i:], uint32(aem.hookBuffer))
	i += 4
	code[i] = 0x25; i++ // and eax, 0xFF
	code[i] = 0xFF; i++
	code[i] = 0x00; i++
	code[i] = 0x00; i++
	code[i] = 0x00; i++
	arrayBase := uint32(aem.hookBuffer) + 4
	code[i] = 0x8D; i++ // lea ebx, [eax*4 + buffer+4]
	code[i] = 0x1C; i++
	code[i] = 0x85; i++
	binary.LittleEndian.PutUint32(code[i:], arrayBase)
	i += 4
	code[i] = 0x89; i++ // mov [ebx], ecx
	code[i] = 0x0B; i++
	code[i] = 0xFF; i++ // inc dword [buffer]
	code[i] = 0x05; i++
	binary.LittleEndian.PutUint32(code[i:], uint32(aem.hookBuffer))
	i += 4
	code[i] = 0x5B; i++ // pop ebx
	code[i] = 0x58; i++ // pop eax
	// Stolen bytes
	code[i] = 0x55; i++ // push ebp
	code[i] = 0x8B; i++
	code[i] = 0xEC; i++ // mov ebp, esp
	code[i] = 0x64; i++
	code[i] = 0xA1; i++
	code[i] = 0x00; i++
	code[i] = 0x00; i++
	code[i] = 0x00; i++
	code[i] = 0x00; i++ // mov eax, fs:[0]
	// jmp back
	jmpPos := i
	code[i] = 0xE9; i++
	jmpTarget := uint32(updateAddr) + 9
	jmpFrom := uint32(aem.hookTrampoline) + uint32(jmpPos) + 5
	jmpOffset := int32(jmpTarget) - int32(jmpFrom)
	binary.LittleEndian.PutUint32(code[jmpPos+1:], uint32(jmpOffset))

	// Write trampoline
	procWriteProcessMemory.Call(aem.processHandle, aem.hookTrampoline, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), uintptr(unsafe.Pointer(&written)))

	// Build hook
	hook := make([]byte, 9)
	hook[0] = 0xE9
	hookOffset := int32(uint32(aem.hookTrampoline)) - int32(uint32(updateAddr)+5)
	binary.LittleEndian.PutUint32(hook[1:], uint32(hookOffset))
	for j := 5; j < 9; j++ {
		hook[j] = 0x90
	}

	// Install hook
	var oldProtect uint32
	procVirtualProtectEx.Call(aem.processHandle, updateAddr, 9, 0x40, uintptr(unsafe.Pointer(&oldProtect)))
	procWriteProcessMemory.Call(aem.processHandle, updateAddr, uintptr(unsafe.Pointer(&hook[0])), 9, uintptr(unsafe.Pointer(&written)))
	procVirtualProtectEx.Call(aem.processHandle, updateAddr, 9, uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))

	aem.hookInstalled = true

	fmt.Println("[ALL_ENTITIES] Hook installed")
}

// removeHook removes the memory hook
func (aem *AllEntitiesManager) removeHook() {
	if !aem.hookInstalled {
		return
	}

	updateAddr := aem.x2game + 0x0E3FD0

	// Restore original bytes
	var written uintptr
	var oldProtect uint32
	procVirtualProtectEx.Call(aem.processHandle, updateAddr, 9, 0x40, uintptr(unsafe.Pointer(&oldProtect)))
	procWriteProcessMemory.Call(aem.processHandle, updateAddr, uintptr(unsafe.Pointer(&aem.hookOriginalBytes[0])), 9, uintptr(unsafe.Pointer(&written)))
	procVirtualProtectEx.Call(aem.processHandle, updateAddr, 9, uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))

	// Free resources
	if aem.hookBuffer != 0 {
		procVirtualFreeEx.Call(aem.processHandle, aem.hookBuffer, 0, 0x8000)
	}
	if aem.hookTrampoline != 0 {
		procVirtualFreeEx.Call(aem.processHandle, aem.hookTrampoline, 0, 0x8000)
	}

	aem.hookInstalled = false
	aem.hookBuffer = 0
	aem.hookTrampoline = 0

	fmt.Println("[ALL_ENTITIES] Hook removed")
}
