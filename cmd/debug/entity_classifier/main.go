// +build windows

package main

import (
	"archefriend/esp"
	"archefriend/process"
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazyDLL("kernel32.dll")
	procReadProcessMemory = kernel32.NewProc("ReadProcessMemory")
)

type ClassifiedEntity struct {
	Name           string            `json:"name"`
	Faction        string            `json:"faction"`
	UnitID         uint32            `json:"unit_id"`
	HP             uint32            `json:"hp"`
	Distance       float32           `json:"distance"`
	EntityAddr     uint32            `json:"entity_addr"`
	ActorModelAddr uint32            `json:"actor_model_addr"`
	E              map[string]uint32 `json:"e"`
	AM             map[string]uint32 `json:"am"`
}

var (
	processHandle windows.Handle
	espMgr        *esp.Manager
	x2gameBase    uintptr
)

func main() {
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║   FACTION CLASSIFIER - Interactive    ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	pid, err := process.FindProcess("archeage.exe")
	if err != nil {
		fmt.Printf("[ERROR] ArcheAge not found: %v\n", err)
		waitExit()
		return
	}
	fmt.Printf("[OK] Found ArcheAge PID: %d\n", pid)

	handle, err := process.OpenProcess(pid)
	if err != nil {
		fmt.Printf("[ERROR] Failed to open process: %v\n", err)
		waitExit()
		return
	}
	processHandle = handle
	defer windows.CloseHandle(handle)

	x2game, err := process.GetModuleBase(pid, "x2game.dll")
	if err != nil {
		fmt.Printf("[ERROR] x2game.dll not found: %v\n", err)
		waitExit()
		return
	}
	x2gameBase = x2game
	fmt.Printf("[OK] x2game.dll base: 0x%X\n", x2game)

	espMgr, err = esp.NewManager(uintptr(handle), pid, x2game)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create ESP manager: %v\n", err)
		waitExit()
		return
	}
	defer espMgr.Close()

	espMgr.ToggleAllEntities()
	fmt.Println("[OK] AllEntities ESP enabled")

	fmt.Println("\nWaiting for entities...")
	time.Sleep(3 * time.Second)

	samples := loadSamples("faction_samples.json")
	fmt.Printf("[INFO] Loaded %d existing samples\n", len(samples))

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n════════════════════════════════════════")
		fmt.Println("Commands:")
		fmt.Println("  c - Classify players (interactive)")
		fmt.Println("  g - Auto-collect (use race detection)")
		fmt.Println("  a - Analyze samples")
		fmt.Println("  f - Find faction offset (East vs West)")
		fmt.Println("  p - Find PIRATE offset (Pirate vs Non-Pirate)")
		fmt.Println("  m - Dump AM+0x20 (faction struct from IDA)")
		fmt.Println("  x - Read pirate faction ID from memory")
		fmt.Println("  v - Search for value 161 in samples")
		fmt.Println("  s - Show stats")
		fmt.Println("  l - List current players")
		fmt.Println("  d - Delete all samples")
		fmt.Println("  q - Quit")
		fmt.Print("> ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "q", "quit":
			saveSamples("faction_samples.json", samples)
			fmt.Println("Saved. Goodbye!")
			return

		case "c", "classify":
			samples = classifyPlayers(samples, reader)
			saveSamples("faction_samples.json", samples)

		case "g", "auto":
			samples = autoCollectSamples(samples)
			saveSamples("faction_samples.json", samples)

		case "a", "analyze":
			analyzeSamples(samples)

		case "f", "faction":
			findFactionOffset(samples)

		case "p", "pirate":
			findPirateOffset(samples)

		case "m", "memory":
			dumpAM0x20()

		case "x", "xref":
			readPirateFactionID()

		case "v", "value":
			searchForFactionValue(samples, 161)

		case "s", "stats":
			showStats(samples)

		case "l", "list":
			listPlayers()

		case "d", "delete":
			samples = []ClassifiedEntity{}
			saveSamples("faction_samples.json", samples)
			fmt.Println("All samples deleted")

		default:
			fmt.Println("Unknown command")
		}
	}
}

func listPlayers() {
	entities := espMgr.GetAllEntitiesCached()
	var players []esp.EntityInfo
	for _, e := range entities {
		if e.IsPlayer && !e.IsMate && !e.IsNPC {
			players = append(players, e)
		}
	}

	fmt.Printf("\nFound %d players:\n", len(players))
	for i, p := range players {
		race, faction := getRaceAndFaction(p.Address)
		fmt.Printf("  [%d] %s | HP:%d | Dist:%.0fm | Race:%s -> %s\n",
			i, p.Name, p.HP, p.Distance, race, faction)
	}
}

// getRaceAndFaction reads the race string from E+0x370 and determines faction
func getRaceAndFaction(entityAddr uint32) (race, faction string) {
	raceData := readMemory(uintptr(entityAddr+0x370), 32)
	raceStr := readCString(raceData)

	// Parse "foley_<race>" format
	if strings.HasPrefix(raceStr, "foley_") {
		race = strings.TrimPrefix(raceStr, "foley_")
	} else {
		race = raceStr
	}

	// Determine faction by race
	switch race {
	case "nuian", "elf", "dwarf":
		faction = "west"
	case "hariharan", "firran", "ferre", "returned", "warborn":
		faction = "east"
	case "player":
		faction = "npc" // humanoid NPC
	default:
		faction = "?"
	}

	return race, faction
}

func classifyPlayers(samples []ClassifiedEntity, reader *bufio.Reader) []ClassifiedEntity {
	entities := espMgr.GetAllEntitiesCached()

	var players []esp.EntityInfo
	for _, e := range entities {
		if e.IsPlayer && !e.IsMate && !e.IsNPC {
			players = append(players, e)
		}
	}

	if len(players) == 0 {
		fmt.Println("\n[!] No players found")
		return samples
	}

	fmt.Printf("\n[FOUND] %d players. Classifying one by one...\n", len(players))
	fmt.Println()
	fmt.Println("For each player, enter:")
	fmt.Println("  w = West (Nuian)")
	fmt.Println("  e = East (Haranyan)")
	fmt.Println("  p = Pirate")
	fmt.Println("  s = Skip (not a player / don't know)")
	fmt.Println("  q = Stop classifying")
	fmt.Println()

	for i, player := range players {
		race, detectedFaction := getRaceAndFaction(player.Address)

		fmt.Printf("─────────────────────────────────────────\n")
		fmt.Printf("[%d/%d] %s\n", i+1, len(players), player.Name)
		fmt.Printf("       HP: %d | Distance: %.0fm\n", player.HP, player.Distance)
		fmt.Printf("       Race: %s -> Detected: %s\n", race, detectedFaction)

		// Auto-suggest based on race
		suggestion := ""
		if detectedFaction == "west" {
			suggestion = " [Enter=west]"
		} else if detectedFaction == "east" {
			suggestion = " [Enter=east]"
		}
		fmt.Printf("\n  Faction? (w/e/p/s/q)%s: ", suggestion)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		if input == "q" {
			fmt.Println("\n[STOP] Classification stopped")
			break
		}

		if input == "s" {
			fmt.Println("  [SKIP]")
			continue
		}

		// Use detected faction if Enter pressed
		var faction string
		if input == "" {
			if detectedFaction == "west" || detectedFaction == "east" {
				faction = detectedFaction
			} else {
				fmt.Println("  [SKIP] Can't auto-detect, enter w/e/p")
				continue
			}
		} else {
			switch input {
			case "w":
				faction = "west"
			case "e":
				faction = "east"
			case "p":
				faction = "pirate"
			default:
				fmt.Println("  [SKIP] Invalid input")
				continue
			}
		}

		sample := createSample(player, faction)
		samples = append(samples, sample)
		fmt.Printf("  [OK] -> %s (race: %s)\n", faction, race)
	}

	fmt.Printf("\n[DONE] Total samples now: %d\n", len(samples))
	return samples
}

func createSample(entity esp.EntityInfo, faction string) ClassifiedEntity {
	sample := ClassifiedEntity{
		Name:           entity.Name,
		Faction:        faction,
		UnitID:         entity.EntityID,
		HP:             entity.HP,
		Distance:       entity.Distance,
		EntityAddr:     entity.Address,
		ActorModelAddr: entity.ActorModelAddr,
		E:              make(map[string]uint32),
		AM:             make(map[string]uint32),
	}

	// Read Entity memory (0x00 - 0x900) - expanded range
	entityData := readMemory(uintptr(entity.Address), 0x900)
	for off := 0; off < 0x900; off += 4 {
		key := fmt.Sprintf("0x%03X", off)
		if off+4 <= len(entityData) {
			sample.E[key] = binary.LittleEndian.Uint32(entityData[off:])
		}
	}

	// Read ActorModel memory (0x00 - 0x500) - expanded range
	if entity.ActorModelAddr != 0 {
		amData := readMemory(uintptr(entity.ActorModelAddr), 0x500)
		for off := 0; off < 0x500; off += 4 {
			key := fmt.Sprintf("0x%03X", off)
			if off+4 <= len(amData) {
				sample.AM[key] = binary.LittleEndian.Uint32(amData[off:])
			}
		}
	}

	return sample
}

func readMemory(addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var read uintptr
	procReadProcessMemory.Call(
		uintptr(processHandle),
		addr,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&read)),
	)
	return buf
}

func readCString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func analyzeSamples(samples []ClassifiedEntity) {
	var players []ClassifiedEntity
	for _, s := range samples {
		if s.Faction == "west" || s.Faction == "east" || s.Faction == "pirate" {
			players = append(players, s)
		}
	}

	if len(players) < 2 {
		fmt.Println("\n[!] Need at least 2 samples")
		showStats(samples)
		return
	}

	byFaction := make(map[string][]ClassifiedEntity)
	for _, s := range players {
		byFaction[s.Faction] = append(byFaction[s.Faction], s)
	}

	fmt.Println("\n╔══════════════════════════════════════╗")
	fmt.Println("║        FACTION ANALYSIS              ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Printf("\nSamples: %d\n", len(players))
	for f, list := range byFaction {
		fmt.Printf("  %s: %d\n", f, len(list))
	}

	fmt.Println("\n--- Searching for faction offset (0x000 - 0x900) ---")

	foundCount := 0
	for off := 0; off < 0x900; off += 4 {
		key := fmt.Sprintf("0x%03X", off)

		valuesByFaction := make(map[string]map[uint32]int)
		for f := range byFaction {
			valuesByFaction[f] = make(map[uint32]int)
		}

		for _, s := range players {
			val := s.E[key]
			valuesByFaction[s.Faction][val]++
		}

		// Check if all factions have same value
		allSame := true
		var firstVal uint32
		first := true
		for _, vals := range valuesByFaction {
			for v := range vals {
				if first {
					firstVal = v
					first = false
				} else if v != firstVal {
					allSame = false
				}
				break
			}
		}

		if allSame {
			continue
		}

		// Check consistency
		consistent := true
		for _, vals := range valuesByFaction {
			if len(vals) > 2 {
				consistent = false
				break
			}
		}

		if consistent {
			foundCount++
			fmt.Printf("\nE+%s:\n", key)
			for f, vals := range valuesByFaction {
				if len(vals) == 0 {
					continue
				}
				fmt.Printf("  %-7s: ", f)
				for v, c := range vals {
					fmt.Printf("0x%08X(%d) ", v, c)
				}
				fmt.Println()
			}
		}
	}

	if foundCount == 0 {
		fmt.Println("\nNo distinguishing offsets found yet.")
		fmt.Println("Collect more samples from different factions.")
	}

	// Check race string at 0x370
	fmt.Println("\n--- Race String (E+0x370) ---")
	fmt.Println("  Name         User     Race         -> Detected")
	fmt.Println("  ──────────── ──────── ──────────── ─────────────")
	for _, s := range players {
		race, detectedFaction := getRaceAndFaction(s.EntityAddr)
		match := ""
		if s.Faction != detectedFaction && detectedFaction != "?" {
			match = " ** PIRATE? **"
		}
		fmt.Printf("  %-12s %-8s %-12s -> %s%s\n", s.Name, s.Faction, race, detectedFaction, match)
	}

	// Raw data
	fmt.Println("\n--- Raw Data ---")
	offsets := []string{"0x030", "0x034", "0x038", "0x03C", "0x040", "0x044", "0x048", "0x04C"}

	fmt.Printf("%-12s %-6s", "Name", "Fact")
	for _, off := range offsets {
		fmt.Printf(" %-8s", off)
	}
	fmt.Println()

	for _, s := range players {
		name := s.Name
		if len(name) > 11 {
			name = name[:11]
		}
		fmt.Printf("%-12s %-6s", name, s.Faction)
		for _, off := range offsets {
			fmt.Printf(" %08X", s.E[off])
		}
		fmt.Println()
	}
}

func showStats(samples []ClassifiedEntity) {
	byFaction := make(map[string]int)
	for _, s := range samples {
		byFaction[s.Faction]++
	}

	fmt.Printf("\nTotal: %d samples\n", len(samples))
	for f, c := range byFaction {
		fmt.Printf("  %s: %d\n", f, c)
	}
}

func loadSamples(filename string) []ClassifiedEntity {
	data, err := os.ReadFile(filename)
	if err != nil {
		return []ClassifiedEntity{}
	}
	var samples []ClassifiedEntity
	json.Unmarshal(data, &samples)
	return samples
}

func saveSamples(filename string, samples []ClassifiedEntity) {
	data, _ := json.MarshalIndent(samples, "", "  ")
	os.WriteFile(filename, data, 0644)
}

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// searchForFactionValue searches for a specific value in all samples
func searchForFactionValue(samples []ClassifiedEntity, targetValue uint32) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Printf("║          SEARCH FOR VALUE %d (0x%X) IN SAMPLES            ║\n", targetValue, targetValue)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Separate pirates and non-pirates
	var pirates, nonPirates []ClassifiedEntity
	for _, s := range samples {
		if s.Faction == "pirate" {
			pirates = append(pirates, s)
		} else if s.Faction == "west" || s.Faction == "east" {
			nonPirates = append(nonPirates, s)
		}
	}

	fmt.Printf("\nPirates: %d, Non-Pirates: %d\n", len(pirates), len(nonPirates))

	if len(pirates) == 0 {
		fmt.Println("\n[!] No pirate samples. Use 'c' to classify pirates first.")
		return
	}

	// Search in Entity memory
	fmt.Println("\n--- Searching Entity memory for value", targetValue, "---")
	for off := 0; off < 0x900; off += 4 {
		key := fmt.Sprintf("0x%03X", off)

		piratesWithValue := 0
		nonPiratesWithValue := 0

		for _, s := range pirates {
			if s.E[key] == targetValue {
				piratesWithValue++
			}
		}
		for _, s := range nonPirates {
			if s.E[key] == targetValue {
				nonPiratesWithValue++
			}
		}

		if piratesWithValue > 0 || nonPiratesWithValue > 0 {
			fmt.Printf("  E+%s: Pirates=%d/%d, Non-Pirates=%d/%d\n",
				key, piratesWithValue, len(pirates), nonPiratesWithValue, len(nonPirates))

			if piratesWithValue == len(pirates) && nonPiratesWithValue == 0 {
				fmt.Println("    ^^^ PERFECT MATCH! All pirates have 161, no non-pirates do! ^^^")
			}
		}
	}

	// Search in ActorModel memory
	fmt.Println("\n--- Searching ActorModel memory for value", targetValue, "---")
	for off := 0; off < 0x300; off += 4 {
		key := fmt.Sprintf("0x%03X", off)

		piratesWithValue := 0
		nonPiratesWithValue := 0

		for _, s := range pirates {
			if s.AM[key] == targetValue {
				piratesWithValue++
			}
		}
		for _, s := range nonPirates {
			if s.AM[key] == targetValue {
				nonPiratesWithValue++
			}
		}

		if piratesWithValue > 0 || nonPiratesWithValue > 0 {
			fmt.Printf("  AM+%s: Pirates=%d/%d, Non-Pirates=%d/%d\n",
				key, piratesWithValue, len(pirates), nonPiratesWithValue, len(nonPirates))

			if piratesWithValue == len(pirates) && nonPiratesWithValue == 0 {
				fmt.Println("    ^^^ PERFECT MATCH! All pirates have 161, no non-pirates do! ^^^")
			}
		}
	}

	// Show what values pirates have at common faction-like offsets
	fmt.Println("\n--- Values at potential faction offsets ---")
	factionOffsets := []string{"0x028", "0x02C", "0x030", "0x0B8", "0x0BC", "0x0C0", "0x100", "0x104", "0x108"}

	fmt.Println("\nEntity offsets:")
	fmt.Printf("%-12s %-6s", "Name", "Type")
	for _, off := range factionOffsets {
		fmt.Printf(" %-6s", off)
	}
	fmt.Println()

	for _, s := range pirates {
		name := s.Name
		if len(name) > 11 {
			name = name[:11]
		}
		fmt.Printf("%-12s %-6s", name, "PIRAT")
		for _, off := range factionOffsets {
			val := s.E[off]
			if val == targetValue {
				fmt.Printf(" *%d*  ", val)
			} else {
				fmt.Printf(" %-6d", val)
			}
		}
		fmt.Println()
	}
	for _, s := range nonPirates[:min(5, len(nonPirates))] {
		name := s.Name
		if len(name) > 11 {
			name = name[:11]
		}
		fmt.Printf("%-12s %-6s", name, s.Faction[:4])
		for _, off := range factionOffsets {
			val := s.E[off]
			if val == targetValue {
				fmt.Printf(" *%d*  ", val)
			} else {
				fmt.Printf(" %-6d", val)
			}
		}
		fmt.Println()
	}
}

// readPirateFactionID reads the pirate faction ID from game memory
func readPirateFactionID() {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              PIRATE FACTION ID FROM MEMORY                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Offsets from IDA (base 0x39000000)
	// dword_39E9EF18 = pirate faction ID
	// dword_39E9EF14 = threshold?
	const (
		pirateFactionIDOffset = 0xE9EF18
		thresholdOffset       = 0xE9EF14
	)

	fmt.Printf("\nx2game.dll base: 0x%X\n", x2gameBase)

	// Read pirate faction ID
	pirateFactionAddr := x2gameBase + pirateFactionIDOffset
	pirateData := readMemory(pirateFactionAddr, 4)
	pirateFactionID := binary.LittleEndian.Uint32(pirateData)
	fmt.Printf("\n[0x%X] Pirate Faction ID: %d (0x%X)\n", pirateFactionAddr, pirateFactionID, pirateFactionID)

	// Read threshold
	thresholdAddr := x2gameBase + thresholdOffset
	thresholdData := readMemory(thresholdAddr, 4)
	threshold := binary.LittleEndian.Uint32(thresholdData)
	fmt.Printf("[0x%X] Threshold:         %d (0x%X)\n", thresholdAddr, threshold, threshold)

	// Read nearby values for context
	fmt.Println("\n--- Nearby values (0xE9EF00 - 0xE9EF30) ---")
	for off := uintptr(0xE9EF00); off <= 0xE9EF30; off += 4 {
		addr := x2gameBase + off
		data := readMemory(addr, 4)
		val := binary.LittleEndian.Uint32(data)
		marker := ""
		if off == thresholdOffset {
			marker = " <- threshold?"
		} else if off == pirateFactionIDOffset {
			marker = " <- PIRATE FACTION ID"
		}
		fmt.Printf("  [0x%X] = %d (0x%08X)%s\n", off, val, val, marker)
	}

	fmt.Println("\n--- Interpretation ---")
	if pirateFactionID > 0 && pirateFactionID < 1000 {
		fmt.Printf("Pirate faction ID appears to be: %d\n", pirateFactionID)
		fmt.Println("This means: if player.factionID == this value -> player is pirate")
	} else {
		fmt.Println("Value seems unusual. It may not be initialized yet or the offset is wrong.")
	}
}

// autoCollectSamples automatically collects samples using race detection
func autoCollectSamples(samples []ClassifiedEntity) []ClassifiedEntity {
	entities := espMgr.GetAllEntitiesCached()

	var players []esp.EntityInfo
	for _, e := range entities {
		if e.IsPlayer && !e.IsMate && !e.IsNPC {
			players = append(players, e)
		}
	}

	if len(players) == 0 {
		fmt.Println("\n[!] No players found")
		return samples
	}

	fmt.Printf("\n[AUTO-COLLECT] Found %d players\n", len(players))

	added := 0
	skipped := 0
	for _, player := range players {
		race, faction := getRaceAndFaction(player.Address)

		// Only collect if we can detect faction (east or west)
		if faction != "east" && faction != "west" {
			skipped++
			continue
		}

		// Check if already sampled (by name)
		alreadyExists := false
		for _, s := range samples {
			if s.Name == player.Name {
				alreadyExists = true
				break
			}
		}
		if alreadyExists {
			skipped++
			continue
		}

		sample := createSample(player, faction)
		samples = append(samples, sample)
		added++
		fmt.Printf("  [+] %s (%s) -> %s\n", player.Name, race, faction)
	}

	fmt.Printf("\n[DONE] Added: %d | Skipped: %d | Total: %d\n", added, skipped, len(samples))
	return samples
}

// findFactionOffset compares East vs West samples to find distinguishing offsets
func findFactionOffset(samples []ClassifiedEntity) {
	var west, east []ClassifiedEntity
	for _, s := range samples {
		if s.Faction == "west" {
			west = append(west, s)
		} else if s.Faction == "east" {
			east = append(east, s)
		}
	}

	if len(west) < 2 || len(east) < 2 {
		fmt.Printf("\n[!] Need at least 2 West and 2 East samples\n")
		fmt.Printf("    West: %d | East: %d\n", len(west), len(east))
		return
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           FACTION OFFSET FINDER (East vs West)               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nSamples: West=%d, East=%d\n", len(west), len(east))
	fmt.Println("(Skipping race string offsets 0x360-0x400)")

	candidateCount := 0

	// isRaceOffset returns true for offsets related to race strings (skip these)
	isRaceOffset := func(off int) bool {
		return off >= 0x360 && off <= 0x400
	}

	// Search in Entity memory (E+0x00 to E+0x900) - EXPANDED
	fmt.Println("\n--- Searching Entity memory (E+0x000 - E+0x900) ---")
	for off := 0; off < 0x900; off += 4 {
		if isRaceOffset(off) {
			continue // Skip race-related offsets
		}
		key := fmt.Sprintf("0x%03X", off)
		westVal, eastVal, found := findConsistentDifference(west, east, key, true)
		if found {
			candidateCount++
			fmt.Printf("\n*** E+%s ***\n", key)
			fmt.Printf("  West: 0x%08X (%d)\n", westVal, westVal)
			fmt.Printf("  East: 0x%08X (%d)\n", eastVal, eastVal)
			if westVal < 1000 && eastVal < 1000 {
				fmt.Println("  ^^^ LIKELY FACTION ID! ^^^")
			}
		}
	}

	// Search in ActorModel memory (AM+0x00 to AM+0x500) - EXPANDED
	fmt.Println("\n--- Searching ActorModel memory (AM+0x000 - AM+0x500) ---")
	for off := 0; off < 0x500; off += 4 {
		key := fmt.Sprintf("0x%03X", off)
		westVal, eastVal, found := findConsistentDifference(west, east, key, false)
		if found {
			candidateCount++
			fmt.Printf("\n*** AM+%s ***\n", key)
			fmt.Printf("  West: 0x%08X (%d)\n", westVal, westVal)
			fmt.Printf("  East: 0x%08X (%d)\n", eastVal, eastVal)
			if westVal < 100 && eastVal < 100 {
				fmt.Println("  ^^^ LIKELY FACTION ID! ^^^")
			}
		}
	}

	if candidateCount == 0 {
		fmt.Println("\nNo perfect matches found.")
		fmt.Println("\n--- Offsets with >80% consistency per faction ---")

		// Check Entity
		fmt.Println("\nEntity (E):")
		for off := 0; off < 0x200; off += 4 {
			key := fmt.Sprintf("0x%03X", off)
			showPartialMatch(west, east, key, "E", true)
		}

		// Check ActorModel
		fmt.Println("\nActorModel (AM):")
		for off := 0; off < 0x100; off += 4 {
			key := fmt.Sprintf("0x%03X", off)
			showPartialMatch(west, east, key, "AM", false)
		}
	} else {
		fmt.Printf("\n[RESULT] Found %d candidate offsets!\n", candidateCount)
	}
}

func findConsistentDifference(west, east []ClassifiedEntity, key string, useEntity bool) (uint32, uint32, bool) {
	westValues := make(map[uint32]int)
	eastValues := make(map[uint32]int)

	for _, s := range west {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		westValues[val]++
	}

	for _, s := range east {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		eastValues[val]++
	}

	// Need exactly 1 unique value for each faction
	if len(westValues) != 1 || len(eastValues) != 1 {
		return 0, 0, false
	}

	var westVal, eastVal uint32
	for v := range westValues {
		westVal = v
	}
	for v := range eastValues {
		eastVal = v
	}

	// Must be different and not pointers
	if westVal == eastVal || westVal > 0x10000000 || eastVal > 0x10000000 {
		return 0, 0, false
	}

	return westVal, eastVal, true
}

func showPartialMatch(west, east []ClassifiedEntity, key, prefix string, useEntity bool) {
	westValues := make(map[uint32]int)
	eastValues := make(map[uint32]int)

	for _, s := range west {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		westValues[val]++
	}

	for _, s := range east {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		eastValues[val]++
	}

	// Find most common
	var westMostCommon, eastMostCommon uint32
	var westMax, eastMax int
	for v, c := range westValues {
		if c > westMax {
			westMax = c
			westMostCommon = v
		}
	}
	for v, c := range eastValues {
		if c > eastMax {
			eastMax = c
			eastMostCommon = v
		}
	}

	westPct := float64(westMax) / float64(len(west)) * 100
	eastPct := float64(eastMax) / float64(len(east)) * 100

	if westPct >= 80 && eastPct >= 80 && westMostCommon != eastMostCommon {
		if westMostCommon < 0x10000000 && eastMostCommon < 0x10000000 {
			fmt.Printf("  %s+%s: West=0x%X (%.0f%%) | East=0x%X (%.0f%%)\n",
				prefix, key, westMostCommon, westPct, eastMostCommon, eastPct)
		}
	}
}

// findPirateOffset compares Pirate vs Non-Pirate samples to find distinguishing offsets
func findPirateOffset(samples []ClassifiedEntity) {
	var pirates, nonPirates []ClassifiedEntity
	for _, s := range samples {
		if s.Faction == "pirate" {
			pirates = append(pirates, s)
		} else if s.Faction == "west" || s.Faction == "east" {
			nonPirates = append(nonPirates, s)
		}
	}

	if len(pirates) < 1 {
		fmt.Printf("\n[!] Need at least 1 Pirate sample\n")
		fmt.Printf("    Pirates: %d | Non-Pirates: %d\n", len(pirates), len(nonPirates))
		fmt.Println("\n    Use 'c' to classify players and mark pirates with 'p'")
		return
	}

	if len(nonPirates) < 2 {
		fmt.Printf("\n[!] Need at least 2 Non-Pirate samples (East or West)\n")
		fmt.Printf("    Pirates: %d | Non-Pirates: %d\n", len(pirates), len(nonPirates))
		return
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║         PIRATE OFFSET FINDER (Pirate vs Non-Pirate)          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nSamples: Pirates=%d, Non-Pirates=%d\n", len(pirates), len(nonPirates))

	fmt.Println("\n--- Pirate Samples ---")
	for _, p := range pirates {
		race, origFaction := getRaceAndFaction(p.EntityAddr)
		fmt.Printf("  %s (race: %s -> originally %s)\n", p.Name, race, origFaction)
	}

	fmt.Println("\n--- Non-Pirate Samples ---")
	for _, np := range nonPirates {
		race, _ := getRaceAndFaction(np.EntityAddr)
		fmt.Printf("  %s (%s, race: %s)\n", np.Name, np.Faction, race)
	}

	candidateCount := 0

	// Search in Entity memory (E+0x00 to E+0x900)
	fmt.Println("\n--- Searching Entity memory (E+0x000 - E+0x900) ---")
	for off := 0; off < 0x900; off += 4 {
		key := fmt.Sprintf("0x%03X", off)
		pirateVal, nonPirateVal, found := findPirateDifference(pirates, nonPirates, key, true)
		if found {
			candidateCount++
			fmt.Printf("\n*** E+%s ***\n", key)
			fmt.Printf("  Pirates:     0x%08X (%d)\n", pirateVal, pirateVal)
			fmt.Printf("  Non-Pirates: 0x%08X (%d)\n", nonPirateVal, nonPirateVal)
			if pirateVal < 100 || nonPirateVal < 100 {
				fmt.Println("  ^^^ LIKELY PIRATE FLAG! ^^^")
			}
		}
	}

	// Search in ActorModel memory (AM+0x00 to AM+0x300)
	fmt.Println("\n--- Searching ActorModel memory (AM+0x000 - AM+0x300) ---")
	for off := 0; off < 0x300; off += 4 {
		key := fmt.Sprintf("0x%03X", off)
		pirateVal, nonPirateVal, found := findPirateDifference(pirates, nonPirates, key, false)
		if found {
			candidateCount++
			fmt.Printf("\n*** AM+%s ***\n", key)
			fmt.Printf("  Pirates:     0x%08X (%d)\n", pirateVal, pirateVal)
			fmt.Printf("  Non-Pirates: 0x%08X (%d)\n", nonPirateVal, nonPirateVal)
			if pirateVal < 100 || nonPirateVal < 100 {
				fmt.Println("  ^^^ LIKELY PIRATE FLAG! ^^^")
			}
		}
	}

	if candidateCount == 0 {
		fmt.Println("\nNo perfect matches found.")
		fmt.Println("\n--- Offsets with partial consistency ---")

		// Show offsets where pirates have a unique value
		fmt.Println("\nEntity (E):")
		for off := 0; off < 0x200; off += 4 {
			key := fmt.Sprintf("0x%03X", off)
			showPiratePartialMatch(pirates, nonPirates, key, "E", true)
		}

		fmt.Println("\nActorModel (AM):")
		for off := 0; off < 0x100; off += 4 {
			key := fmt.Sprintf("0x%03X", off)
			showPiratePartialMatch(pirates, nonPirates, key, "AM", false)
		}

		// Show detailed comparison for offsets near entity type fields
		fmt.Println("\n--- Values near Entity Type (E+0x000-0x020, AM+0x010-0x030) ---")

		// Entity start (near VTable at E+0x00)
		fmt.Println("\nEntity (E+0x000-0x020):")
		entityStartOffsets := []string{"0x000", "0x004", "0x008", "0x00C", "0x010", "0x014", "0x018", "0x01C", "0x020"}
		fmt.Printf("%-12s %-6s", "Name", "Type")
		for _, off := range entityStartOffsets {
			fmt.Printf(" %-8s", off)
		}
		fmt.Println()
		for _, s := range pirates {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			fmt.Printf("%-12s %-6s", name, "PIRAT")
			for _, off := range entityStartOffsets {
				fmt.Printf(" %08X", s.E[off])
			}
			fmt.Println()
		}
		for _, s := range nonPirates[:min(5, len(nonPirates))] {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			factionLabel := s.Faction
			if len(factionLabel) > 5 {
				factionLabel = factionLabel[:5]
			}
			fmt.Printf("%-12s %-6s", name, factionLabel)
			for _, off := range entityStartOffsets {
				fmt.Printf(" %08X", s.E[off])
			}
			fmt.Println()
		}

		// ActorModel near type field (AM+0x14 is entity type)
		fmt.Println("\nActorModel (AM+0x010-0x030):")
		amTypeOffsets := []string{"0x010", "0x014", "0x018", "0x01C", "0x020", "0x024", "0x028", "0x02C", "0x030"}
		fmt.Printf("%-12s %-6s", "Name", "Type")
		for _, off := range amTypeOffsets {
			fmt.Printf(" %-8s", off)
		}
		fmt.Println()
		for _, s := range pirates {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			fmt.Printf("%-12s %-6s", name, "PIRAT")
			for _, off := range amTypeOffsets {
				fmt.Printf(" %08X", s.AM[off])
			}
			fmt.Println()
		}
		for _, s := range nonPirates[:min(5, len(nonPirates))] {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			factionLabel := s.Faction
			if len(factionLabel) > 5 {
				factionLabel = factionLabel[:5]
			}
			fmt.Printf("%-12s %-6s", name, factionLabel)
			for _, off := range amTypeOffsets {
				fmt.Printf(" %08X", s.AM[off])
			}
			fmt.Println()
		}

		fmt.Println("\n--- Detailed Values for Key Offsets ---")
		keyOffsets := []string{"0x030", "0x034", "0x038", "0x03C", "0x040", "0x044", "0x048", "0x04C", "0x050", "0x054", "0x058", "0x05C"}
		fmt.Printf("\n%-12s %-6s", "Name", "Type")
		for _, off := range keyOffsets {
			fmt.Printf(" %-8s", off)
		}
		fmt.Println()

		for _, s := range pirates {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			fmt.Printf("%-12s %-6s", name, "PIRAT")
			for _, off := range keyOffsets {
				fmt.Printf(" %08X", s.E[off])
			}
			fmt.Println()
		}
		for _, s := range nonPirates {
			name := s.Name
			if len(name) > 11 {
				name = name[:11]
			}
			factionLabel := s.Faction
			if len(factionLabel) > 5 {
				factionLabel = factionLabel[:5]
			}
			fmt.Printf("%-12s %-6s", name, factionLabel)
			for _, off := range keyOffsets {
				fmt.Printf(" %08X", s.E[off])
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("\n[RESULT] Found %d candidate offsets!\n", candidateCount)
	}
}

// findPirateDifference checks if all pirates have one value and all non-pirates have a different value
func findPirateDifference(pirates, nonPirates []ClassifiedEntity, key string, useEntity bool) (uint32, uint32, bool) {
	pirateValues := make(map[uint32]int)
	nonPirateValues := make(map[uint32]int)

	for _, s := range pirates {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		pirateValues[val]++
	}

	for _, s := range nonPirates {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		nonPirateValues[val]++
	}

	// Pirates must have exactly 1 unique value
	if len(pirateValues) != 1 {
		return 0, 0, false
	}

	// Non-pirates must have exactly 1 unique value
	if len(nonPirateValues) != 1 {
		return 0, 0, false
	}

	var pirateVal, nonPirateVal uint32
	for v := range pirateValues {
		pirateVal = v
	}
	for v := range nonPirateValues {
		nonPirateVal = v
	}

	// Must be different and not pointers (avoid dynamic addresses)
	if pirateVal == nonPirateVal || pirateVal > 0x10000000 || nonPirateVal > 0x10000000 {
		return 0, 0, false
	}

	return pirateVal, nonPirateVal, true
}

// showPiratePartialMatch shows offsets where pirates have a consistent unique value
func showPiratePartialMatch(pirates, nonPirates []ClassifiedEntity, key, prefix string, useEntity bool) {
	pirateValues := make(map[uint32]int)
	nonPirateValues := make(map[uint32]int)

	for _, s := range pirates {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		pirateValues[val]++
	}

	for _, s := range nonPirates {
		var val uint32
		if useEntity {
			val = s.E[key]
		} else {
			val = s.AM[key]
		}
		nonPirateValues[val]++
	}

	// Find most common value for each group
	var pirateMostCommon, nonPirateMostCommon uint32
	var pirateMax, nonPirateMax int
	for v, c := range pirateValues {
		if c > pirateMax {
			pirateMax = c
			pirateMostCommon = v
		}
	}
	for v, c := range nonPirateValues {
		if c > nonPirateMax {
			nonPirateMax = c
			nonPirateMostCommon = v
		}
	}

	piratePct := float64(pirateMax) / float64(len(pirates)) * 100
	nonPiratePct := float64(nonPirateMax) / float64(len(nonPirates)) * 100

	// Show if pirates are 100% consistent and different from most non-pirates
	if piratePct == 100 && nonPiratePct >= 70 && pirateMostCommon != nonPirateMostCommon {
		if pirateMostCommon < 0x10000000 && nonPirateMostCommon < 0x10000000 {
			fmt.Printf("  %s+%s: Pirates=0x%X (100%%) | NonPirates=0x%X (%.0f%%)\n",
				prefix, key, pirateMostCommon, nonPirateMostCommon, nonPiratePct)
		}
	}
}

// AM0x20Dump contains detailed dump of ActorModel+0x20 potential faction struct
type AM0x20Dump struct {
	Name          string  `json:"name"`
	Race          string  `json:"race"`
	Faction       string  `json:"faction"`
	Distance      float32 `json:"distance"`
	IsPlayer      bool    `json:"is_player"`
	ActorModelPtr uint32  `json:"actor_model_ptr"`
	EntityPtr     uint32  `json:"entity_ptr"`

	// ActorModel offsets around 0x20
	AM_0x18 uint32 `json:"am_0x18"`
	AM_0x1C uint32 `json:"am_0x1c"`
	AM_0x20 uint32 `json:"am_0x20"`
	AM_0x24 uint32 `json:"am_0x24"`
	AM_0x28 uint32 `json:"am_0x28"`
	AM_0x2C uint32 `json:"am_0x2c"`
	AM_0x30 uint32 `json:"am_0x30"`

	// If AM+0x20 is a pointer, dump the struct it points to
	AM_0x20_IsPtr bool     `json:"am_0x20_is_ptr"`
	AM_0x20_Hex   string   `json:"am_0x20_hex,omitempty"`
	Struct_0x00   uint32   `json:"struct_0x00,omitempty"`
	Struct_0x04   uint32   `json:"struct_0x04,omitempty"`
	Struct_0x08   uint32   `json:"struct_0x08,omitempty"`
	Struct_0x0C   uint32   `json:"struct_0x0c,omitempty"`
	Struct_0x10   uint32   `json:"struct_0x10,omitempty"`
	Struct_0x14   uint32   `json:"struct_0x14,omitempty"`
	Struct_0x18   uint32   `json:"struct_0x18,omitempty"`
	Struct_0x1C   uint32   `json:"struct_0x1c,omitempty"`
}

// isValidPtr checks if a value looks like a valid pointer
func isValidPtr(val uint32) bool {
	return val > 0x10000 && val < 0x7FFFFFFF
}

// dumpAM0x20 dumps ActorModel+0x20 data for all players
func dumpAM0x20() {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║       AM+0x20 FACTION STRUCT DUMP (from IDA analysis)        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	entities := espMgr.GetAllEntitiesCached()

	var players []esp.EntityInfo
	for _, e := range entities {
		if e.IsPlayer && !e.IsMate && !e.IsNPC {
			players = append(players, e)
		}
	}

	if len(players) == 0 {
		fmt.Println("\n[!] No players found. Make sure AllEntities ESP is running.")
		return
	}

	fmt.Printf("\n[FOUND] %d players\n", len(players))

	var dumps []AM0x20Dump

	// Header for comparison table
	fmt.Println("\n--- Comparison Table ---")
	fmt.Printf("%-14s %-8s %-6s | AM+0x20    IsPtr | Struct Values (if pointer)\n", "Name", "Race", "Fact")
	fmt.Println(strings.Repeat("-", 90))

	for _, player := range players {
		race, faction := getRaceAndFaction(player.Address)
		amPtr := player.ActorModelAddr

		if amPtr == 0 {
			continue
		}

		// Read ActorModel offsets around 0x20
		am0x18 := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x18), 4))
		am0x1C := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x1C), 4))
		am0x20 := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x20), 4))
		am0x24 := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x24), 4))
		am0x28 := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x28), 4))
		am0x2C := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x2C), 4))
		am0x30 := binary.LittleEndian.Uint32(readMemory(uintptr(amPtr+0x30), 4))

		dump := AM0x20Dump{
			Name:          player.Name,
			Race:          race,
			Faction:       faction,
			Distance:      player.Distance,
			IsPlayer:      true,
			ActorModelPtr: amPtr,
			EntityPtr:     player.Address,
			AM_0x18:       am0x18,
			AM_0x1C:       am0x1C,
			AM_0x20:       am0x20,
			AM_0x24:       am0x24,
			AM_0x28:       am0x28,
			AM_0x2C:       am0x2C,
			AM_0x30:       am0x30,
		}

		// Check if AM+0x20 is a valid pointer
		ptrStr := "NO"
		if isValidPtr(am0x20) {
			dump.AM_0x20_IsPtr = true
			ptrStr = "YES"

			// Read the struct it points to (64 bytes)
			structData := readMemory(uintptr(am0x20), 64)
			dump.AM_0x20_Hex = bytesToHex(structData)
			dump.Struct_0x00 = binary.LittleEndian.Uint32(structData[0x00:])
			dump.Struct_0x04 = binary.LittleEndian.Uint32(structData[0x04:])
			dump.Struct_0x08 = binary.LittleEndian.Uint32(structData[0x08:])
			dump.Struct_0x0C = binary.LittleEndian.Uint32(structData[0x0C:])
			dump.Struct_0x10 = binary.LittleEndian.Uint32(structData[0x10:])
			dump.Struct_0x14 = binary.LittleEndian.Uint32(structData[0x14:])
			dump.Struct_0x18 = binary.LittleEndian.Uint32(structData[0x18:])
			dump.Struct_0x1C = binary.LittleEndian.Uint32(structData[0x1C:])
		}

		dumps = append(dumps, dump)

		// Print row
		name := player.Name
		if len(name) > 13 {
			name = name[:13]
		}
		if len(race) > 7 {
			race = race[:7]
		}
		if len(faction) > 5 {
			faction = faction[:5]
		}

		fmt.Printf("%-14s %-8s %-6s | 0x%08X %-5s", name, race, faction, am0x20, ptrStr)
		if dump.AM_0x20_IsPtr {
			fmt.Printf(" | 0x%08X 0x%08X 0x%08X 0x%08X",
				dump.Struct_0x00, dump.Struct_0x04, dump.Struct_0x08, dump.Struct_0x0C)
		}
		fmt.Println()
	}

	// Save to JSON
	jsonData, _ := json.MarshalIndent(dumps, "", "  ")
	filename := "am0x20_dump.json"
	os.WriteFile(filename, jsonData, 0644)
	fmt.Printf("\n[SAVED] Detailed dump saved to %s\n", filename)

	// Analyze patterns
	fmt.Println("\n--- Pattern Analysis ---")

	// Group by faction
	byFaction := make(map[string][]AM0x20Dump)
	for _, d := range dumps {
		byFaction[d.Faction] = append(byFaction[d.Faction], d)
	}

	// Check if AM+0x20 values differ by faction
	fmt.Println("\nAM+0x20 values by faction:")
	for faction, list := range byFaction {
		values := make(map[uint32]int)
		for _, d := range list {
			values[d.AM_0x20]++
		}
		fmt.Printf("  %s: ", faction)
		for v, c := range values {
			if isValidPtr(v) {
				fmt.Printf("0x%08X(ptr)x%d ", v, c)
			} else {
				fmt.Printf("0x%08X(%d)x%d ", v, v, c)
			}
		}
		fmt.Println()
	}

	// If AM+0x20 is a pointer, check Struct values
	hasPointers := false
	for _, d := range dumps {
		if d.AM_0x20_IsPtr {
			hasPointers = true
			break
		}
	}

	if hasPointers {
		fmt.Println("\nStruct+0x00 values by faction (if AM+0x20 is pointer):")
		for faction, list := range byFaction {
			values := make(map[uint32]int)
			for _, d := range list {
				if d.AM_0x20_IsPtr {
					values[d.Struct_0x00]++
				}
			}
			if len(values) > 0 {
				fmt.Printf("  %s: ", faction)
				for v, c := range values {
					fmt.Printf("%d(0x%X)x%d ", v, v, c)
				}
				fmt.Println()
			}
		}

		fmt.Println("\nStruct+0x04 values by faction:")
		for faction, list := range byFaction {
			values := make(map[uint32]int)
			for _, d := range list {
				if d.AM_0x20_IsPtr {
					values[d.Struct_0x04]++
				}
			}
			if len(values) > 0 {
				fmt.Printf("  %s: ", faction)
				for v, c := range values {
					fmt.Printf("%d(0x%X)x%d ", v, v, c)
				}
				fmt.Println()
			}
		}

		fmt.Println("\nStruct+0x08 values by faction:")
		for faction, list := range byFaction {
			values := make(map[uint32]int)
			for _, d := range list {
				if d.AM_0x20_IsPtr {
					values[d.Struct_0x08]++
				}
			}
			if len(values) > 0 {
				fmt.Printf("  %s: ", faction)
				for v, c := range values {
					fmt.Printf("%d(0x%X)x%d ", v, v, c)
				}
				fmt.Println()
			}
		}
	}

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
}

// bytesToHex converts bytes to hex string
func bytesToHex(data []byte) string {
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
