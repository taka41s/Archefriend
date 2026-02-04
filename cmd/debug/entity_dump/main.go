// +build windows

package main

import (
	"archefriend/esp"
	"archefriend/process"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║     ENTITY MEMORY DUMP TOOL           ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	// Find ArcheAge process
	pid, err := process.FindProcess("archeage.exe")
	if err != nil {
		fmt.Printf("[ERROR] ArcheAge not found: %v\n", err)
		fmt.Println("Make sure ArcheAge is running!")
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
	fmt.Printf("[OK] Process handle: 0x%X\n", handle)

	// Get x2game.dll base
	x2game, err := process.GetModuleBase(pid, "x2game.dll")
	if err != nil {
		fmt.Printf("[ERROR] x2game.dll not found: %v\n", err)
		waitExit()
		return
	}
	fmt.Printf("[OK] x2game.dll base: 0x%X\n", x2game)

	// Create ESP manager (needed for memory reading and hook)
	espMgr, err := esp.NewManager(uintptr(handle), pid, x2game)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create ESP manager: %v\n", err)
		waitExit()
		return
	}
	defer espMgr.Close()

	// Enable AllEntities ESP (installs the hook)
	espMgr.ToggleAllEntities()
	fmt.Println("[OK] AllEntities hook installed")
	fmt.Println()

	// Wait for entities to be collected
	fmt.Println("Collecting entities for 3 seconds...")
	time.Sleep(3 * time.Second)

	// Do the dump
	fmt.Println()
	espMgr.DumpEntityMemoryCompare()

	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Println("Press Ctrl+C to exit or wait 30s...")
	fmt.Println("════════════════════════════════════════")

	// Wait for signal or timeout
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
	case <-time.After(30 * time.Second):
	}

	fmt.Println("Cleaning up...")
}

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	fmt.Scanln()
}
