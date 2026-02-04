// +build windows

package main

import (
	"archefriend/esp"
	"archefriend/process"
	"bufio"
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

var processHandle windows.Handle

func main() {
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║      RACE ANALYZER - Debug Tool       ║")
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
	fmt.Printf("[OK] x2game.dll base: 0x%X\n", x2game)

	espMgr, err := esp.NewManager(uintptr(handle), pid, x2game)
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

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n════════════════════════════════════════")
		fmt.Println("Commands:")
		fmt.Println("  a - Analyze all entities (show race strings)")
		fmt.Println("  u - Show unknown races only (?)")
		fmt.Println("  d - Dump memory around race offset for unknowns")
		fmt.Println("  r - Refresh")
		fmt.Println("  q - Quit")
		fmt.Print("> ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "q", "quit":
			fmt.Println("Goodbye!")
			return
		case "a", "all":
			analyzeAllEntities(espMgr)
		case "u", "unknown":
			showUnknownRaces(espMgr)
		case "d", "dump":
			dumpUnknownRaceMemory(espMgr)
		case "r", "refresh":
			continue
		default:
			fmt.Println("Unknown command")
		}
	}
}

func analyzeAllEntities(espMgr *esp.Manager) {
	entities := espMgr.GetAllEntitiesCached()

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    ALL ENTITIES RACE ANALYSIS                ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Group by type
	var players, npcs, mates, unknown []esp.EntityInfo
	for _, e := range entities {
		if e.IsPlayer {
			players = append(players, e)
		} else if e.IsNPC {
			npcs = append(npcs, e)
		} else if e.IsMate {
			mates = append(mates, e)
		} else {
			unknown = append(unknown, e)
		}
	}

	fmt.Printf("\nTotal: %d entities\n", len(entities))
	fmt.Printf("  Players: %d\n", len(players))
	fmt.Printf("  NPCs: %d\n", len(npcs))
	fmt.Printf("  Mates: %d\n", len(mates))
	fmt.Printf("  Unknown type: %d\n", len(unknown))

	// Analyze players
	if len(players) > 0 {
		fmt.Println("\n--- PLAYERS ---")
		fmt.Println("Name                 | Race Raw                 | Race    | Faction")
		fmt.Println("---------------------|--------------------------|---------|--------")

		raceCount := make(map[string]int)
		for _, p := range players {
			raceRaw, race, faction := getRaceInfo(p.Address)
			raceCount[race]++

			name := p.Name
			if len(name) > 20 {
				name = name[:20]
			}
			fmt.Printf("%-20s | %-24s | %-7s | %s\n", name, raceRaw, race, faction)
		}

		fmt.Println("\nRace Summary:")
		for race, count := range raceCount {
			fmt.Printf("  %s: %d\n", race, count)
		}
	}

	// Analyze NPCs (sample)
	if len(npcs) > 0 {
		fmt.Println("\n--- NPCs (first 10) ---")
		limit := 10
		if len(npcs) < limit {
			limit = len(npcs)
		}
		for i := 0; i < limit; i++ {
			n := npcs[i]
			raceRaw, _, _ := getRaceInfo(n.Address)
			name := n.Name
			if len(name) > 20 {
				name = name[:20]
			}
			fmt.Printf("  %-20s | %s\n", name, raceRaw)
		}
	}
}

func showUnknownRaces(espMgr *esp.Manager) {
	entities := espMgr.GetAllEntitiesCached()

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    UNKNOWN RACES (?)                         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	count := 0
	for _, e := range entities {
		if !e.IsPlayer {
			continue
		}

		_, race, faction := getRaceInfo(e.Address)
		if faction == "?" {
			count++
			raceRaw, _, _ := getRaceInfo(e.Address)
			fmt.Printf("\n[%d] %s\n", count, e.Name)
			fmt.Printf("    Address: 0x%X\n", e.Address)
			fmt.Printf("    Race Raw: '%s'\n", raceRaw)
			fmt.Printf("    Race Parsed: '%s'\n", race)
			fmt.Printf("    HP: %d | Distance: %.0fm\n", e.HP, e.Distance)
		}
	}

	if count == 0 {
		fmt.Println("\nNo unknown races found!")
	} else {
		fmt.Printf("\nTotal unknown: %d\n", count)
	}
}

func dumpUnknownRaceMemory(espMgr *esp.Manager) {
	entities := espMgr.GetAllEntitiesCached()

	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              MEMORY DUMP FOR UNKNOWN RACES                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	count := 0
	for _, e := range entities {
		if !e.IsPlayer {
			continue
		}

		_, _, faction := getRaceInfo(e.Address)
		if faction == "?" {
			count++
			fmt.Printf("\n========== %s (0x%X) ==========\n", e.Name, e.Address)

			// Dump E+0x360 to E+0x400 (race region)
			fmt.Println("\n--- E+0x360 to E+0x400 (Race Region) ---")
			data := readMemory(uintptr(e.Address+0x360), 0xA0)
			printHexDump(data, 0x360)

			// Also check nearby offsets
			fmt.Println("\n--- E+0x300 to E+0x360 (Before Race) ---")
			data2 := readMemory(uintptr(e.Address+0x300), 0x60)
			printHexDump(data2, 0x300)
		}
	}

	if count == 0 {
		fmt.Println("\nNo unknown races found!")
	}
}

func getRaceInfo(entityAddr uint32) (raceRaw, race, faction string) {
	// Read raw data at E+0x370
	raceData := readMemory(uintptr(entityAddr+0x370), 32)
	raceRaw = readCString(raceData)

	// Parse "foley_<race>" format
	if strings.HasPrefix(raceRaw, "foley_") {
		race = strings.TrimPrefix(raceRaw, "foley_")
	} else {
		race = raceRaw
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

	return raceRaw, race, faction
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

func printHexDump(data []byte, baseOffset uint32) {
	for i := 0; i < len(data); i += 16 {
		fmt.Printf("0x%03X: ", baseOffset+uint32(i))

		// Hex bytes
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

		// ASCII
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

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
