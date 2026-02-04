package esp

import (
	"encoding/binary"
	"fmt"
	"sort"
	"time"
	"unsafe"
)

const (
	// Update function hook location
	OFF_UPDATE_HOOK = 0x0E3FD0
	STOLEN_BYTES    = 9

	// ActorModel offsets
	OFF_ACTORMODEL_UNITID    = 0x0C
	OFF_ACTORMODEL_ENTITYPTR = 0x1F8

	// Entity offsets
	OFF_ENTITY_POS_X = 0x830
	OFF_ENTITY_POS_Z = 0x834
	OFF_ENTITY_POS_Y = 0x838
	OFF_ENTITY_HP    = 0x84C
)

// CollectEntitiesViaHook uses update hook to collect all entities
func (m *Manager) CollectEntitiesViaHook() []EntityInfo {
	var entities []EntityInfo

	updateAddr := m.x2game + OFF_UPDATE_HOOK

	// Save original bytes
	originalBytes := make([]byte, 16)
	var bytesRead uintptr
	procReadProcessMemory.Call(m.processHandle, updateAddr, uintptr(unsafe.Pointer(&originalBytes[0])), 16, uintptr(unsafe.Pointer(&bytesRead)))

	// Allocate buffer for collected pointers
	// uint32 writeIdx + 256 slots * 4 bytes
	buffer, _, _ := procVirtualAllocEx.Call(m.processHandle, 0, 4+256*4, 0x1000|0x2000, 0x40)
	if buffer == 0 {
		fmt.Println("[HOOK] Failed to allocate buffer")
		return entities
	}
	defer procVirtualFreeEx.Call(m.processHandle, buffer, 0, 0x8000)

	// Zero the buffer
	zeros := make([]byte, 4+256*4)
	var written uintptr
	procWriteProcessMemory.Call(m.processHandle, buffer, uintptr(unsafe.Pointer(&zeros[0])), uintptr(len(zeros)), uintptr(unsafe.Pointer(&written)))

	// Allocate trampoline
	trampoline, _, _ := procVirtualAllocEx.Call(m.processHandle, 0, 64, 0x1000|0x2000, 0x40)
	if trampoline == 0 {
		fmt.Println("[HOOK] Failed to allocate trampoline")
		return entities
	}
	defer procVirtualFreeEx.Call(m.processHandle, trampoline, 0, 0x8000)

	// Build trampoline shellcode
	code := make([]byte, 64)
	i := 0

	// push eax
	code[i] = 0x50
	i++

	// push ebx
	code[i] = 0x53
	i++

	// mov eax, [buffer]  ; read write index
	code[i] = 0xA1
	i++
	binary.LittleEndian.PutUint32(code[i:], uint32(buffer))
	i += 4

	// and eax, 0xFF  ; wrap to 256 slots
	code[i] = 0x25
	i++
	code[i] = 0xFF
	i++
	code[i] = 0x00
	i++
	code[i] = 0x00
	i++
	code[i] = 0x00
	i++

	// lea ebx, [eax*4 + buffer+4]  ; calculate slot address
	arrayBase := uint32(buffer) + 4
	code[i] = 0x8D
	i++
	code[i] = 0x1C
	i++
	code[i] = 0x85
	i++
	binary.LittleEndian.PutUint32(code[i:], arrayBase)
	i += 4

	// mov [ebx], ecx  ; store ActorModel pointer
	code[i] = 0x89
	i++
	code[i] = 0x0B
	i++

	// inc dword [buffer]  ; increment write index
	code[i] = 0xFF
	i++
	code[i] = 0x05
	i++
	binary.LittleEndian.PutUint32(code[i:], uint32(buffer))
	i += 4

	// pop ebx
	code[i] = 0x5B
	i++

	// pop eax
	code[i] = 0x58
	i++

	// Stolen bytes: push ebp; mov ebp, esp; mov eax, fs:[0]
	code[i] = 0x55
	i++ // push ebp
	code[i] = 0x8B
	i++
	code[i] = 0xEC
	i++ // mov ebp, esp
	code[i] = 0x64
	i++
	code[i] = 0xA1
	i++
	code[i] = 0x00
	i++
	code[i] = 0x00
	i++
	code[i] = 0x00
	i++
	code[i] = 0x00
	i++ // mov eax, fs:[0]

	// jmp back to original + STOLEN_BYTES
	jmpPos := i
	code[i] = 0xE9
	i++
	jmpTarget := uint32(updateAddr) + STOLEN_BYTES
	jmpFrom := uint32(trampoline) + uint32(jmpPos) + 5
	jmpOffset := int32(jmpTarget) - int32(jmpFrom)
	binary.LittleEndian.PutUint32(code[jmpPos+1:], uint32(jmpOffset))
	i += 4

	// Write trampoline
	procWriteProcessMemory.Call(m.processHandle, trampoline, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), uintptr(unsafe.Pointer(&written)))

	// Build hook
	hook := make([]byte, STOLEN_BYTES)
	hook[0] = 0xE9 // jmp
	hookOffset := int32(uint32(trampoline)) - int32(uint32(updateAddr)+5)
	binary.LittleEndian.PutUint32(hook[1:], uint32(hookOffset))
	// NOP padding
	for j := 5; j < STOLEN_BYTES; j++ {
		hook[j] = 0x90
	}

	fmt.Println("[HOOK] Installing hook...")

	// Change protection and write hook
	var oldProtect uint32
	procVirtualProtectEx.Call(m.processHandle, updateAddr, STOLEN_BYTES, 0x40, uintptr(unsafe.Pointer(&oldProtect)))
	procWriteProcessMemory.Call(m.processHandle, updateAddr, uintptr(unsafe.Pointer(&hook[0])), STOLEN_BYTES, uintptr(unsafe.Pointer(&written)))
	procVirtualProtectEx.Call(m.processHandle, updateAddr, STOLEN_BYTES, uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))

	// Collect for 2 seconds
	collected := make(map[uint32]bool)
	lastIdx := uint32(0)
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		writeIdx := m.readU32(buffer)
		for lastIdx != writeIdx {
			slot := lastIdx & 0xFF
			ptr := m.readU32(buffer + 4 + uintptr(slot*4))
			if ptr != 0 {
				collected[ptr] = true
			}
			lastIdx++
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Restore original bytes
	fmt.Println("[HOOK] Removing hook...")
	procVirtualProtectEx.Call(m.processHandle, updateAddr, STOLEN_BYTES, 0x40, uintptr(unsafe.Pointer(&oldProtect)))
	procWriteProcessMemory.Call(m.processHandle, updateAddr, uintptr(unsafe.Pointer(&originalBytes[0])), STOLEN_BYTES, uintptr(unsafe.Pointer(&written)))
	procVirtualProtectEx.Call(m.processHandle, updateAddr, STOLEN_BYTES, uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))

	fmt.Printf("[HOOK] Collected %d unique ActorModel pointers\n", len(collected))

	// Get player position for distance calc
	playerX, playerY, playerZ, hasPlayer := m.GetPlayerPosition()
	if !hasPlayer {
		return entities
	}

	// Process collected entities
	for actorModel := range collected {
		unitId := m.readU32(uintptr(actorModel + OFF_ACTORMODEL_UNITID))
		entityPtr := m.readU32(uintptr(actorModel + OFF_ACTORMODEL_ENTITYPTR))

		if !isValidPtr(entityPtr) {
			continue
		}

		// Read position
		posX := m.readFloat32(uintptr(entityPtr + OFF_ENTITY_POS_X))
		posZ := m.readFloat32(uintptr(entityPtr + OFF_ENTITY_POS_Z))
		posY := m.readFloat32(uintptr(entityPtr + OFF_ENTITY_POS_Y))

		// Validate position
		if posX < 100 || posX > 50000 {
			continue
		}
		if !isValidCoord(posX) || !isValidCoord(posY) || !isValidCoord(posZ) {
			continue
		}

		// Read HP
		hp := m.readU32(uintptr(entityPtr + OFF_ENTITY_HP))
		if hp < 100 || hp > 10000000 {
			continue
		}

		// Read name
		name := m.getEntityName(entityPtr)
		if !isValidEntityName(name) {
			// Try alternative name reading
			name = ""
			for _, off := range []uint32{0x1C, 0x20, 0x24, 0x28} {
				ptr := m.readU32(uintptr(entityPtr + off))
				if isValidPtr(ptr) {
					s := m.readString(uintptr(ptr), 32)
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

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, posX, posY, posZ)
		if distance > 200.0 {
			continue
		}

		// Read MaxHP
		maxHP := m.getMaxHP(entityPtr)

		// Detect entity type using discovered offsets
		// AM+0x14: 0x04 = NPC, 0x00 = Player/Mount
		actorModelType := m.readU32(uintptr(actorModel + 0x14))
		entityVTable := m.readU32(uintptr(entityPtr))

		isNPC := actorModelType == 0x04
		isMate := false
		isPlayer := !isNPC

		// Check for mount by VTable pattern
		vtableLowByte := (entityVTable >> 8) & 0xFF
		if vtableLowByte == 0xDF {
			isMate = true
			isPlayer = false
			isNPC = false
		}

		entities = append(entities, EntityInfo{
			Address:  entityPtr,
			VTable:   entityVTable,
			EntityID: unitId,
			Name:     name,
			PosX:     posX,
			PosY:     posY,
			PosZ:     posZ,
			HP:       hp,
			MaxHP:    maxHP,
			Distance: distance,
			IsPlayer: isPlayer,
			IsNPC:    isNPC,
			IsMate:   isMate,
		})
	}

	// Sort by distance
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Distance < entities[j].Distance
	})

	fmt.Printf("[HOOK] Found %d valid entities\n", len(entities))
	return entities
}
