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

	"golang.org/x/sys/windows"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║     ENTITY MONITOR TOOL               ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	// Find ArcheAge process
	pid, err := process.FindProcess("archeage.exe")
	if err != nil {
		fmt.Printf("[ERROR] ArcheAge not found: %v\n", err)
		waitExit()
		return
	}
	fmt.Printf("[OK] Found ArcheAge PID: %d\n", pid)

	// Open process
	handle, err := process.OpenProcess(pid)
	if err != nil {
		fmt.Printf("[ERROR] Failed to open process: %v\n", err)
		waitExit()
		return
	}
	defer windows.CloseHandle(handle)

	// Get x2game.dll base
	x2game, err := process.GetModuleBase(pid, "x2game.dll")
	if err != nil {
		fmt.Printf("[ERROR] x2game.dll not found: %v\n", err)
		waitExit()
		return
	}
	fmt.Printf("[OK] x2game.dll base: 0x%X\n", x2game)

	// Create ESP manager
	espMgr, err := esp.NewManager(uintptr(handle), pid, x2game)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create ESP manager: %v\n", err)
		waitExit()
		return
	}
	defer espMgr.Close()

	// Enable AllEntities ESP
	espMgr.ToggleAllEntities()
	fmt.Println("[OK] AllEntities hook installed")
	fmt.Println()

	fmt.Println("Collecting entities...")
	time.Sleep(2 * time.Second)

	reader := bufio.NewReader(os.Stdin)

	for {
		// Get cached entities
		entities := espMgr.GetAllEntitiesCached()

		fmt.Println()
		fmt.Println("════════════════════════════════════════")
		fmt.Printf("Found %d entities:\n", len(entities))
		fmt.Println("════════════════════════════════════════")

		for i, e := range entities {
			fmt.Printf("[%d] %s | HP:%d | Dist:%.0fm | Addr:0x%X\n",
				i, e.Name, e.HP, e.Distance, e.Address)
		}

		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  <number>  - Monitor entity by index")
		fmt.Println("  r         - Refresh list")
		fmt.Println("  q         - Quit")
		fmt.Print("> ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "q" || input == "quit" || input == "exit" {
			break
		}

		if input == "r" || input == "refresh" {
			continue
		}

		// Parse index
		var idx int
		_, err := fmt.Sscanf(input, "%d", &idx)
		if err != nil || idx < 0 || idx >= len(entities) {
			fmt.Println("Invalid index")
			continue
		}

		// Monitor selected entity
		entity := entities[idx]
		monitorEntity(espMgr, entity)
	}

	fmt.Println("Cleaning up...")
}

func monitorEntity(espMgr *esp.Manager, entity esp.EntityInfo) {
	fmt.Println()
	fmt.Printf("Monitoring: %s (0x%X)\n", entity.Name, entity.Address)
	fmt.Println("Press Enter to stop monitoring...")
	fmt.Println()

	stopChan := make(chan bool)

	// Start goroutine to wait for Enter
	go func() {
		bufio.NewReader(os.Stdin).ReadString('\n')
		stopChan <- true
	}()

	// Monitor loop
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastDump := espMgr.DumpSingleEntity(entity.Address)

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			currentDump := espMgr.DumpSingleEntity(entity.Address)
			if currentDump == nil {
				fmt.Println("[WARN] Entity no longer valid")
				return
			}

			// Compare and show changes
			if lastDump != nil {
				showChanges(lastDump, currentDump)
			}

			lastDump = currentDump
		}
	}
}

func showChanges(old, new *esp.EntityDump) {
	// Compare Entity MID region (0x800-0x900)
	for i := 0; i < len(old.EntityMid) && i < len(new.EntityMid); i += 4 {
		if i+4 > len(old.EntityMid) || i+4 > len(new.EntityMid) {
			break
		}

		oldVal := uint32(old.EntityMid[i]) | uint32(old.EntityMid[i+1])<<8 |
			uint32(old.EntityMid[i+2])<<16 | uint32(old.EntityMid[i+3])<<24
		newVal := uint32(new.EntityMid[i]) | uint32(new.EntityMid[i+1])<<8 |
			uint32(new.EntityMid[i+2])<<16 | uint32(new.EntityMid[i+3])<<24

		if oldVal != newVal {
			offset := 0x800 + i
			label := ""
			switch offset {
			case 0x830:
				label = " (PosX)"
			case 0x834:
				label = " (PosZ)"
			case 0x838:
				label = " (PosY)"
			case 0x84C:
				label = " (HP)"
			}
			fmt.Printf("[CHANGE] 0x%03X: %08X -> %08X%s\n", offset, oldVal, newVal, label)
		}
	}
}

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
