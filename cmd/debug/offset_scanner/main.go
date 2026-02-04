// +build windows

package main

import (
	"archefriend/process"
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazyDLL("kernel32.dll")
	procReadProcessMemory = kernel32.NewProc("ReadProcessMemory")
)

type Scanner struct {
	handle windows.Handle
	x2game uintptr
}

func main() {
	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Println("║     OFFSET SCANNER TOOL               ║")
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

	scanner := &Scanner{handle: handle, x2game: x2game}

	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  dump <addr> [size]    - Hex dump memory (default size=256)")
	fmt.Println("  read32 <addr>         - Read DWORD at address")
	fmt.Println("  readf <addr>          - Read float at address")
	fmt.Println("  ptr <addr> [off...]   - Follow pointer chain")
	fmt.Println("  scan <addr> <size> <value> - Scan for DWORD value")
	fmt.Println("  scanf <addr> <size> <value> - Scan for float value")
	fmt.Println("  diff <addr> <size>    - Monitor changes in memory region")
	fmt.Println("  x2 <offset>           - Read from x2game+offset")
	fmt.Println("  help                  - Show this help")
	fmt.Println("  exit                  - Exit")
	fmt.Println()
	fmt.Println("Addresses can be hex (0x...) or decimal")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "exit", "quit", "q":
			return

		case "help", "h", "?":
			fmt.Println("Commands: dump, read32, readf, ptr, scan, scanf, diff, x2, exit")

		case "dump":
			if len(parts) < 2 {
				fmt.Println("Usage: dump <addr> [size]")
				continue
			}
			addr := parseAddr(parts[1])
			size := uint32(256)
			if len(parts) >= 3 {
				size = uint32(parseAddr(parts[2]))
			}
			scanner.hexDump(addr, size)

		case "read32":
			if len(parts) < 2 {
				fmt.Println("Usage: read32 <addr>")
				continue
			}
			addr := parseAddr(parts[1])
			val := scanner.readU32(uintptr(addr))
			fmt.Printf("0x%08X = %d (0x%08X)\n", addr, val, val)

		case "readf":
			if len(parts) < 2 {
				fmt.Println("Usage: readf <addr>")
				continue
			}
			addr := parseAddr(parts[1])
			val := scanner.readFloat(uintptr(addr))
			fmt.Printf("0x%08X = %f\n", addr, val)

		case "ptr":
			if len(parts) < 2 {
				fmt.Println("Usage: ptr <addr> [off1] [off2] ...")
				continue
			}
			addr := parseAddr(parts[1])
			fmt.Printf("Base: 0x%08X\n", addr)
			for i := 2; i < len(parts); i++ {
				off := parseAddr(parts[i])
				val := scanner.readU32(uintptr(addr))
				if val == 0 {
					fmt.Printf("  +0x%X -> NULL (chain broken)\n", off)
					break
				}
				addr = uint32(val) + uint32(off)
				fmt.Printf("  +0x%X -> 0x%08X\n", off, addr)
			}
			finalVal := scanner.readU32(uintptr(addr))
			fmt.Printf("Final value: %d (0x%08X)\n", finalVal, finalVal)

		case "scan":
			if len(parts) < 4 {
				fmt.Println("Usage: scan <addr> <size> <value>")
				continue
			}
			addr := parseAddr(parts[1])
			size := parseAddr(parts[2])
			value := uint32(parseAddr(parts[3]))
			scanner.scanForValue(uintptr(addr), size, value)

		case "scanf":
			if len(parts) < 4 {
				fmt.Println("Usage: scanf <addr> <size> <value>")
				continue
			}
			addr := parseAddr(parts[1])
			size := parseAddr(parts[2])
			value, _ := strconv.ParseFloat(parts[3], 32)
			scanner.scanForFloat(uintptr(addr), size, float32(value))

		case "diff":
			if len(parts) < 3 {
				fmt.Println("Usage: diff <addr> <size>")
				continue
			}
			addr := parseAddr(parts[1])
			size := parseAddr(parts[2])
			scanner.monitorChanges(uintptr(addr), size)

		case "x2":
			if len(parts) < 2 {
				fmt.Println("Usage: x2 <offset>")
				continue
			}
			off := parseAddr(parts[1])
			addr := scanner.x2game + uintptr(off)
			val := scanner.readU32(addr)
			fmt.Printf("x2game+0x%X (0x%08X) = %d (0x%08X)\n", off, addr, val, val)

		default:
			fmt.Printf("Unknown command: %s\n", cmd)
		}
	}
}

func parseAddr(s string) uint32 {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		// Try decimal
		val, err = strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0
		}
	}
	return uint32(val)
}

func (s *Scanner) readU32(addr uintptr) uint32 {
	var buf [4]byte
	var read uintptr
	procReadProcessMemory.Call(uintptr(s.handle), addr, uintptr(unsafe.Pointer(&buf[0])), 4, uintptr(unsafe.Pointer(&read)))
	return binary.LittleEndian.Uint32(buf[:])
}

func (s *Scanner) readFloat(addr uintptr) float32 {
	val := s.readU32(addr)
	return *(*float32)(unsafe.Pointer(&val))
}

func (s *Scanner) readBytes(addr uintptr, size uint32) []byte {
	buf := make([]byte, size)
	var read uintptr
	procReadProcessMemory.Call(uintptr(s.handle), addr, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&read)))
	return buf
}

func (s *Scanner) hexDump(addr uint32, size uint32) {
	data := s.readBytes(uintptr(addr), size)

	for i := uint32(0); i < size; i += 16 {
		fmt.Printf("0x%08X: ", addr+i)

		// Hex bytes
		for j := uint32(0); j < 16; j++ {
			if i+j < size {
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
		for j := uint32(0); j < 16 && i+j < size; j++ {
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

func (s *Scanner) scanForValue(addr uintptr, size uint32, value uint32) {
	data := s.readBytes(addr, size)
	found := 0

	for i := uint32(0); i+4 <= size; i += 4 {
		val := binary.LittleEndian.Uint32(data[i:])
		if val == value {
			fmt.Printf("Found at 0x%08X (+0x%X)\n", uint32(addr)+i, i)
			found++
			if found >= 20 {
				fmt.Println("... (limited to 20 results)")
				break
			}
		}
	}

	if found == 0 {
		fmt.Println("Value not found")
	} else {
		fmt.Printf("Found %d matches\n", found)
	}
}

func (s *Scanner) scanForFloat(addr uintptr, size uint32, value float32) {
	data := s.readBytes(addr, size)
	found := 0
	tolerance := float32(0.01)

	for i := uint32(0); i+4 <= size; i += 4 {
		bits := binary.LittleEndian.Uint32(data[i:])
		val := *(*float32)(unsafe.Pointer(&bits))

		diff := val - value
		if diff < 0 {
			diff = -diff
		}

		if diff < tolerance {
			fmt.Printf("Found at 0x%08X (+0x%X): %f\n", uint32(addr)+i, i, val)
			found++
			if found >= 20 {
				fmt.Println("... (limited to 20 results)")
				break
			}
		}
	}

	if found == 0 {
		fmt.Println("Value not found")
	} else {
		fmt.Printf("Found %d matches\n", found)
	}
}

func (s *Scanner) monitorChanges(addr uintptr, size uint32) {
	fmt.Println("Monitoring changes... Press Enter to stop")
	fmt.Println()

	baseline := s.readBytes(addr, size)

	go func() {
		bufio.NewReader(os.Stdin).ReadString('\n')
	}()

	for i := 0; i < 100; i++ { // Max 100 iterations
		current := s.readBytes(addr, size)

		for j := uint32(0); j+4 <= size; j += 4 {
			oldVal := binary.LittleEndian.Uint32(baseline[j:])
			newVal := binary.LittleEndian.Uint32(current[j:])

			if oldVal != newVal {
				fmt.Printf("+0x%03X: %08X -> %08X\n", j, oldVal, newVal)
			}
		}

		copy(baseline, current)

		// Small delay
		// time.Sleep(100 * time.Millisecond)
	}
}

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	fmt.Scanln()
}
