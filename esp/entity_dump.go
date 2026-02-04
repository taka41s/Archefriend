package esp

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// EntityDump contains raw memory dump of an entity
type EntityDump struct {
	ActorModelPtr uint32
	EntityPtr     uint32
	UnitID        uint32
	Name          string
	HP            uint32
	PosX, PosY, PosZ float32
	Distance      float32

	// Raw memory dumps
	ActorModelData []byte // 0x00 - 0x200 from ActorModel
	EntityStart    []byte // 0x00 - 0x100 from Entity (beginning of struct)
	EntityMid      []byte // 0x800 - 0x900 from Entity (position/HP region)
}

// DumpEntityMemory dumps memory from multiple entities for comparison
func (aem *AllEntitiesManager) DumpEntityMemory(maxEntities int) []EntityDump {
	var dumps []EntityDump

	aem.mu.Lock()
	if !aem.enabled || !aem.hookInstalled {
		aem.mu.Unlock()
		fmt.Println("[DUMP] Hook not installed")
		return dumps
	}
	hookBuffer := aem.hookBuffer
	aem.mu.Unlock()

	// Collect pointers from buffer
	collected := make(map[uint32]bool)
	for slot := uint32(0); slot < 256; slot++ {
		ptr := aem.mainManager.readU32(hookBuffer + 4 + uintptr(slot*4))
		if ptr != 0 && isValidPtr(ptr) {
			collected[ptr] = true
		}
	}

	// Get player position
	playerX, playerY, playerZ, hasPlayer := aem.mainManager.GetPlayerPosition()
	if !hasPlayer {
		fmt.Println("[DUMP] No player position")
		return dumps
	}

	count := 0
	for actorModel := range collected {
		if count >= maxEntities {
			break
		}

		unitId := aem.mainManager.readU32(uintptr(actorModel + 0x0C))
		entityPtr := aem.mainManager.readU32(uintptr(actorModel + 0x1F8))

		if !isValidPtr(entityPtr) {
			continue
		}

		// Read position
		posX := aem.mainManager.readFloat32(uintptr(entityPtr + 0x830))
		posZ := aem.mainManager.readFloat32(uintptr(entityPtr + 0x834))
		posY := aem.mainManager.readFloat32(uintptr(entityPtr + 0x838))

		if posX < 100 || posX > 50000 {
			continue
		}

		// Read HP
		hp := aem.mainManager.readU32(uintptr(entityPtr + 0x84C))
		if hp < 100 || hp > 10000000 {
			continue
		}

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, posX, posY, posZ)

		// Read name
		name := aem.mainManager.getEntityName(entityPtr)

		// Dump ActorModel memory (0x00 - 0x200)
		actorModelData := make([]byte, 0x200)
		aem.mainManager.readBytes(uintptr(actorModel), actorModelData)

		// Dump Entity START (0x00 - 0x100) - may contain type info
		entityStart := make([]byte, 0x100)
		aem.mainManager.readBytes(uintptr(entityPtr), entityStart)

		// Dump Entity MID (0x800 - 0x900) - position and HP region
		entityMid := make([]byte, 0x100)
		aem.mainManager.readBytes(uintptr(entityPtr+0x800), entityMid)

		dumps = append(dumps, EntityDump{
			ActorModelPtr:  actorModel,
			EntityPtr:      entityPtr,
			UnitID:         unitId,
			Name:           name,
			HP:             hp,
			PosX:           posX,
			PosY:           posY,
			PosZ:           posZ,
			Distance:       distance,
			ActorModelData: actorModelData,
			EntityStart:    entityStart,
			EntityMid:      entityMid,
		})

		count++
	}

	return dumps
}

// PrintEntityDump prints a formatted hex dump of an entity
func PrintEntityDump(dump EntityDump) {
	fmt.Printf("\n========== %s ==========\n", dump.Name)
	fmt.Printf("ActorModel: 0x%08X | Entity: 0x%08X\n", dump.ActorModelPtr, dump.EntityPtr)
	fmt.Printf("UnitID: %d | HP: %d | Distance: %.1f\n", dump.UnitID, dump.HP, dump.Distance)
	fmt.Printf("Position: X=%.2f Y=%.2f Z=%.2f\n", dump.PosX, dump.PosY, dump.PosZ)

	// Print ActorModel dump (first 0x50 bytes)
	fmt.Println("\n--- ActorModel (0x00 - 0x50) ---")
	printHexDump(dump.ActorModelData[:0x50], 0)

	// Print around EntityPtr offset (0x1F0 - 0x200)
	fmt.Println("\n--- ActorModel (0x1F0 - 0x200) - EntityPtr region ---")
	printHexDump(dump.ActorModelData[0x1F0:0x200], 0x1F0)

	// Print Entity START (0x00 - 0x40) - VTable and type info
	fmt.Println("\n--- Entity (0x00 - 0x40) - VTable/Type region ---")
	printHexDump(dump.EntityStart[0x00:0x40], 0)

	// Print Entity (0x30 - 0x60) - possible type fields
	fmt.Println("\n--- Entity (0x30 - 0x80) - Possible type/flags ---")
	printHexDump(dump.EntityStart[0x30:0x80], 0x30)

	// Print Entity MID (0x800 - 0x860) - position region
	fmt.Println("\n--- Entity (0x800 - 0x860) - Position Region ---")
	fmt.Println("       0x830=PosX  0x834=PosZ  0x838=PosY  0x84C=HP")
	printHexDump(dump.EntityMid[0x00:0x60], 0x800)
}

// printHexDump prints a hex dump with offset
func printHexDump(data []byte, baseOffset uint32) {
	for i := 0; i < len(data); i += 16 {
		fmt.Printf("0x%03X: ", baseOffset+uint32(i))

		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				fmt.Printf("%02X ", data[i+j])
			} else {
				fmt.Print("   ")
			}
			if j == 7 {
				fmt.Print(" ")
			}
		}

		fmt.Print(" |")
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b < 127 {
				fmt.Printf("%c", b)
			} else {
				fmt.Print(".")
			}
		}
		fmt.Println("|")
	}
}

// CompareAllEntities compares all entities side by side at specific offsets
func CompareAllEntities(dumps []EntityDump) {
	if len(dumps) < 2 {
		fmt.Println("[COMPARE] Need at least 2 entities")
		return
	}

	fmt.Println("\n########## ENTITY COMPARISON TABLE ##########")
	fmt.Println("Looking for fields that distinguish entity types...\n")

	// Header
	fmt.Printf("%-20s", "Name")
	for _, off := range []uint32{0x00, 0x04, 0x08, 0x0C, 0x10, 0x14, 0x18, 0x30, 0x34, 0x38, 0x3C} {
		fmt.Printf(" | E+0x%02X  ", off)
	}
	fmt.Println()
	fmt.Println(string(make([]byte, 150)))

	// Data rows - Entity offsets
	for _, d := range dumps {
		name := d.Name
		if len(name) > 18 {
			name = name[:18]
		}
		fmt.Printf("%-20s", name)

		for _, off := range []uint32{0x00, 0x04, 0x08, 0x0C, 0x10, 0x14, 0x18, 0x30, 0x34, 0x38, 0x3C} {
			if int(off)+4 <= len(d.EntityStart) {
				val := readU32FromBytes(d.EntityStart, off)
				fmt.Printf(" | %08X", val)
			}
		}
		fmt.Println()
	}

	fmt.Println("\n--- ActorModel comparison ---")
	fmt.Printf("%-20s", "Name")
	for _, off := range []uint32{0x08, 0x0C, 0x10, 0x14, 0x18, 0x1C, 0x20} {
		fmt.Printf(" | A+0x%02X  ", off)
	}
	fmt.Println()

	for _, d := range dumps {
		name := d.Name
		if len(name) > 18 {
			name = name[:18]
		}
		fmt.Printf("%-20s", name)

		for _, off := range []uint32{0x08, 0x0C, 0x10, 0x14, 0x18, 0x1C, 0x20} {
			if int(off)+4 <= len(d.ActorModelData) {
				val := readU32FromBytes(d.ActorModelData, off)
				fmt.Printf(" | %08X", val)
			}
		}
		fmt.Println()
	}
}

func readU32FromBytes(data []byte, offset uint32) uint32 {
	if int(offset)+4 > len(data) {
		return 0
	}
	return uint32(data[offset]) | uint32(data[offset+1])<<8 |
		uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24
}

// FactionDump contains detailed faction investigation data
type FactionDump struct {
	Timestamp   string `json:"timestamp"`
	Name        string `json:"name"`
	Race        string `json:"race"`
	InferredFac string `json:"inferred_faction"` // Based on race
	IsPlayer    bool   `json:"is_player"`
	IsNPC       bool   `json:"is_npc"`
	IsMate      bool   `json:"is_mate"`
	Distance    float32 `json:"distance"`

	// Pointers
	ActorModelPtr uint32 `json:"actor_model_ptr"`
	EntityPtr     uint32 `json:"entity_ptr"`

	// ActorModel+0x20 investigation (potential faction struct)
	AM_0x20_Value    uint32   `json:"am_0x20_value"`     // Raw value at AM+0x20
	AM_0x20_IsPtr    bool     `json:"am_0x20_is_ptr"`    // Is it a valid pointer?
	AM_0x20_Data     []byte   `json:"am_0x20_data"`      // If pointer, first 64 bytes
	AM_0x20_Hex      string   `json:"am_0x20_hex"`       // Hex string for readability

	// Additional ActorModel offsets around 0x20
	AM_0x18_Value uint32 `json:"am_0x18_value"`
	AM_0x1C_Value uint32 `json:"am_0x1c_value"`
	AM_0x24_Value uint32 `json:"am_0x24_value"`
	AM_0x28_Value uint32 `json:"am_0x28_value"`
	AM_0x2C_Value uint32 `json:"am_0x2c_value"`
	AM_0x30_Value uint32 `json:"am_0x30_value"`

	// If AM+0x20 points to a struct, read values from that struct
	Struct_0x00 uint32 `json:"struct_0x00,omitempty"`
	Struct_0x04 uint32 `json:"struct_0x04,omitempty"`
	Struct_0x08 uint32 `json:"struct_0x08,omitempty"`
	Struct_0x0C uint32 `json:"struct_0x0c,omitempty"`
	Struct_0x10 uint32 `json:"struct_0x10,omitempty"`
	Struct_0x14 uint32 `json:"struct_0x14,omitempty"`
	Struct_0x18 uint32 `json:"struct_0x18,omitempty"`
	Struct_0x1C uint32 `json:"struct_0x1c,omitempty"`
}

// DumpFactionData dumps faction-related memory for investigation
func (aem *AllEntitiesManager) DumpFactionData(maxEntities int) []FactionDump {
	var dumps []FactionDump

	aem.mu.Lock()
	if !aem.enabled || !aem.hookInstalled {
		aem.mu.Unlock()
		fmt.Println("[FACTION_DUMP] Hook not installed")
		return dumps
	}
	hookBuffer := aem.hookBuffer
	aem.mu.Unlock()

	// Collect pointers from buffer
	collected := make(map[uint32]bool)
	for slot := uint32(0); slot < 256; slot++ {
		ptr := aem.mainManager.readU32(hookBuffer + 4 + uintptr(slot*4))
		if ptr != 0 && isValidPtr(ptr) {
			collected[ptr] = true
		}
	}

	// Get player position
	playerX, playerY, playerZ, hasPlayer := aem.mainManager.GetPlayerPosition()
	if !hasPlayer {
		fmt.Println("[FACTION_DUMP] No player position")
		return dumps
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	count := 0

	for actorModel := range collected {
		if count >= maxEntities {
			break
		}

		entityPtr := aem.mainManager.readU32(uintptr(actorModel + 0x1F8))
		if !isValidPtr(entityPtr) {
			continue
		}

		// Read position
		posX := aem.mainManager.readFloat32(uintptr(entityPtr + 0x830))
		posZ := aem.mainManager.readFloat32(uintptr(entityPtr + 0x834))
		posY := aem.mainManager.readFloat32(uintptr(entityPtr + 0x838))

		if posX < 100 || posX > 50000 {
			continue
		}

		// Read HP
		hp := aem.mainManager.readU32(uintptr(entityPtr + 0x84C))
		if hp < 100 || hp > 10000000 {
			continue
		}

		// Calculate distance
		distance := CalculateDistance(playerX, playerY, playerZ, posX, posY, posZ)
		if distance < 1.0 || distance > 200.0 {
			continue
		}

		// Read name and race
		name := aem.mainManager.getEntityName(entityPtr)
		race, faction := aem.getRaceAndFaction(entityPtr)

		// Detect entity type
		actorModelType := aem.mainManager.readU32(uintptr(actorModel + 0x14))
		entityVTable := aem.mainManager.readU32(uintptr(entityPtr))

		isNPC := actorModelType == 0x04
		isMate := false
		isPlayer := !isNPC

		vtableLowByte := (entityVTable >> 8) & 0xFF
		if vtableLowByte == 0xDF {
			isMate = true
			isPlayer = false
			isNPC = false
		}
		if faction == "npc" {
			isPlayer = false
			isNPC = true
		}

		// ===== FACTION INVESTIGATION =====
		// Read ActorModel+0x20 (potential faction pointer/struct)
		am0x20 := aem.mainManager.readU32(uintptr(actorModel + 0x20))

		dump := FactionDump{
			Timestamp:     timestamp,
			Name:          name,
			Race:          race,
			InferredFac:   faction,
			IsPlayer:      isPlayer,
			IsNPC:         isNPC,
			IsMate:        isMate,
			Distance:      distance,
			ActorModelPtr: actorModel,
			EntityPtr:     entityPtr,
			AM_0x20_Value: am0x20,

			// Read surrounding offsets
			AM_0x18_Value: aem.mainManager.readU32(uintptr(actorModel + 0x18)),
			AM_0x1C_Value: aem.mainManager.readU32(uintptr(actorModel + 0x1C)),
			AM_0x24_Value: aem.mainManager.readU32(uintptr(actorModel + 0x24)),
			AM_0x28_Value: aem.mainManager.readU32(uintptr(actorModel + 0x28)),
			AM_0x2C_Value: aem.mainManager.readU32(uintptr(actorModel + 0x2C)),
			AM_0x30_Value: aem.mainManager.readU32(uintptr(actorModel + 0x30)),
		}

		// Check if AM+0x20 is a valid pointer
		if isValidPtr(am0x20) {
			dump.AM_0x20_IsPtr = true

			// Read 64 bytes from the struct it points to
			structData := make([]byte, 64)
			aem.mainManager.readBytes(uintptr(am0x20), structData)
			dump.AM_0x20_Data = structData
			dump.AM_0x20_Hex = bytesToHexString(structData)

			// Extract individual values from the struct
			dump.Struct_0x00 = readU32FromBytes(structData, 0x00)
			dump.Struct_0x04 = readU32FromBytes(structData, 0x04)
			dump.Struct_0x08 = readU32FromBytes(structData, 0x08)
			dump.Struct_0x0C = readU32FromBytes(structData, 0x0C)
			dump.Struct_0x10 = readU32FromBytes(structData, 0x10)
			dump.Struct_0x14 = readU32FromBytes(structData, 0x14)
			dump.Struct_0x18 = readU32FromBytes(structData, 0x18)
			dump.Struct_0x1C = readU32FromBytes(structData, 0x1C)
		}

		dumps = append(dumps, dump)
		count++
	}

	return dumps
}

// bytesToHexString converts bytes to hex string for JSON readability
func bytesToHexString(data []byte) string {
	result := ""
	for i, b := range data {
		if i > 0 && i%16 == 0 {
			result += " | "
		} else if i > 0 && i%4 == 0 {
			result += " "
		}
		result += fmt.Sprintf("%02X", b)
	}
	return result
}

// SaveFactionDumpToJSON saves faction dumps to a JSON file
func (aem *AllEntitiesManager) SaveFactionDumpToJSON(filename string) error {
	dumps := aem.DumpFactionData(30) // Get up to 30 entities

	if len(dumps) == 0 {
		return fmt.Errorf("no entities found")
	}

	// Create JSON with indentation for readability
	jsonData, err := json.MarshalIndent(dumps, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("[FACTION_DUMP] Saved %d entities to %s\n", len(dumps), filename)
	return nil
}

// PrintFactionComparison prints a comparison table of faction data
func (aem *AllEntitiesManager) PrintFactionComparison() {
	dumps := aem.DumpFactionData(20)

	if len(dumps) == 0 {
		fmt.Println("[FACTION] No entities found")
		return
	}

	fmt.Println("\n============ FACTION DATA COMPARISON ============")
	fmt.Printf("Found %d entities\n\n", len(dumps))

	// Header
	fmt.Printf("%-16s %-8s %-8s %-6s | AM+0x20    IsPtr? | Struct Values (if pointer)\n",
		"Name", "Race", "Faction", "Type")
	fmt.Println(string(make([]byte, 100)))

	for _, d := range dumps {
		name := d.Name
		if len(name) > 15 {
			name = name[:15]
		}
		race := d.Race
		if len(race) > 7 {
			race = race[:7]
		}

		entityType := "?"
		if d.IsPlayer {
			entityType = "Player"
		} else if d.IsNPC {
			entityType = "NPC"
		} else if d.IsMate {
			entityType = "Mount"
		}

		ptrStr := "NO"
		if d.AM_0x20_IsPtr {
			ptrStr = "YES"
		}

		fmt.Printf("%-16s %-8s %-8s %-6s | 0x%08X %-5s",
			name, race, d.InferredFac, entityType, d.AM_0x20_Value, ptrStr)

		if d.AM_0x20_IsPtr {
			fmt.Printf(" | 0x%08X 0x%08X 0x%08X 0x%08X",
				d.Struct_0x00, d.Struct_0x04, d.Struct_0x08, d.Struct_0x0C)
		}
		fmt.Println()
	}

	fmt.Println("\n============ END FACTION COMPARISON ============")
}

// DumpSingleEntityByAddr dumps a single entity by address
func (aem *AllEntitiesManager) DumpSingleEntityByAddr(entityAddr uint32) *EntityDump {
	if !isValidPtr(entityAddr) {
		return nil
	}

	posX := aem.mainManager.readFloat32(uintptr(entityAddr + 0x830))
	posZ := aem.mainManager.readFloat32(uintptr(entityAddr + 0x834))
	posY := aem.mainManager.readFloat32(uintptr(entityAddr + 0x838))
	hp := aem.mainManager.readU32(uintptr(entityAddr + 0x84C))
	name := aem.mainManager.getEntityName(entityAddr)

	entityStart := make([]byte, 0x100)
	aem.mainManager.readBytes(uintptr(entityAddr), entityStart)

	entityMid := make([]byte, 0x100)
	aem.mainManager.readBytes(uintptr(entityAddr+0x800), entityMid)

	return &EntityDump{
		EntityPtr:   entityAddr,
		Name:        name,
		HP:          hp,
		PosX:        posX,
		PosY:        posY,
		PosZ:        posZ,
		EntityStart: entityStart,
		EntityMid:   entityMid,
	}
}

// DumpAndCompare dumps all entities and shows comparison table
func (aem *AllEntitiesManager) DumpAndCompare() {
	dumps := aem.DumpEntityMemory(15)

	if len(dumps) == 0 {
		fmt.Println("[DUMP] No entities found")
		return
	}

	fmt.Printf("\n[DUMP] Found %d entities\n", len(dumps))

	// Print individual dumps
	for _, d := range dumps {
		PrintEntityDump(d)
	}

	// Show comparison table
	CompareAllEntities(dumps)
}
