//go:build ignore

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procReadProcessMemory  = kernel32.NewProc("ReadProcessMemory")
	procWriteProcessMemory = kernel32.NewProc("WriteProcessMemory")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualProtectEx   = kernel32.NewProc("VirtualProtectEx")
	procModule32FirstW     = kernel32.NewProc("Module32FirstW")
	procModule32NextW      = kernel32.NewProc("Module32NextW")
)

const (
	PROCESS_ALL_ACCESS     = 0x001F0FFF
	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	PAGE_EXECUTE_READWRITE = 0x40
	PAGE_READWRITE         = 0x04
	TARGET_FUNC            = uintptr(0x3925E5B0)
	// Entry: EAX, EBX, ECX, EDX, ESI, EDI, EBP, stack[0], stack[4], stack[8], objId, tmplId = 48 bytes
	ENTRY_SZ = 48
	LOG_MAX  = 32
	HDR_SZ   = 8
)

type MODULEENTRY32W struct {
	Size         uint32
	ModuleID     uint32
	ProcessID    uint32
	GlblcntUsage uint32
	ProccntUsage uint32
	ModBaseAddr  uintptr
	ModBaseSize  uint32
	Module       uintptr
	ModuleName   [256]uint16
	ExePath      [260]uint16
}

type Proc struct {
	handle       syscall.Handle
	x2gameBase   uintptr
	archeageBase uintptr
}

func findPID(name string) (uint32, error) {
	type PE32 struct {
		Size, Usage, ProcessID uint32
		DefaultHeapID          uintptr
		ModuleID, Threads, ParentProcessID uint32
		PriClassBase           int32
		Flags                  uint32
		ExeFile                [260]uint16
	}
	snap, _, _ := kernel32.NewProc("CreateToolhelp32Snapshot").Call(0x2, 0)
	defer procCloseHandle.Call(snap)
	var e PE32
	e.Size = uint32(unsafe.Sizeof(e))
	ret, _, _ := kernel32.NewProc("Process32FirstW").Call(snap, uintptr(unsafe.Pointer(&e)))
	for ret != 0 {
		if syscall.UTF16ToString(e.ExeFile[:]) == name {
			return e.ProcessID, nil
		}
		ret, _, _ = kernel32.NewProc("Process32NextW").Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return 0, fmt.Errorf("not found")
}

func getModuleBases(pid uint32) (x2game, archeage uintptr) {
	snap, _, _ := kernel32.NewProc("CreateToolhelp32Snapshot").Call(0x8|0x10, uintptr(pid))
	if snap == 0 {
		return
	}
	defer procCloseHandle.Call(snap)
	var me MODULEENTRY32W
	me.Size = uint32(unsafe.Sizeof(me))
	ret, _, _ := procModule32FirstW.Call(snap, uintptr(unsafe.Pointer(&me)))
	for ret != 0 {
		name := syscall.UTF16ToString(me.ModuleName[:])
		if name == "x2game.dll" {
			x2game = me.ModBaseAddr
		} else if name == "archeage.exe" {
			archeage = me.ModBaseAddr
		}
		ret, _, _ = procModule32NextW.Call(snap, uintptr(unsafe.Pointer(&me)))
	}
	return
}

func openProc(pid uint32) (*Proc, error) {
	h, _, err := procOpenProcess.Call(PROCESS_ALL_ACCESS, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("OpenProcess: %v", err)
	}
	x2g, arch := getModuleBases(pid)
	return &Proc{handle: syscall.Handle(h), x2gameBase: x2g, archeageBase: arch}, nil
}

func (p *Proc) Close() { procCloseHandle.Call(uintptr(p.handle)) }

func (p *Proc) Read(addr uintptr, b []byte) error {
	var n uintptr
	r, _, _ := procReadProcessMemory.Call(uintptr(p.handle), addr, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return fmt.Errorf("read")
	}
	return nil
}

func (p *Proc) Write(addr uintptr, b []byte) error {
	var n uintptr
	r, _, _ := procWriteProcessMemory.Call(uintptr(p.handle), addr, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return fmt.Errorf("write")
	}
	return nil
}

func (p *Proc) Alloc(sz uintptr, prot uint32) (uintptr, error) {
	a, _, err := procVirtualAllocEx.Call(uintptr(p.handle), 0, sz, MEM_COMMIT|MEM_RESERVE, uintptr(prot))
	if a == 0 {
		return 0, fmt.Errorf("alloc: %v", err)
	}
	return a, nil
}

func (p *Proc) Protect(addr, sz uintptr, prot uint32) (uint32, error) {
	var old uint32
	r, _, err := procVirtualProtectEx.Call(uintptr(p.handle), addr, sz, uintptr(prot), uintptr(unsafe.Pointer(&old)))
	if r == 0 {
		return 0, fmt.Errorf("protect: %v", err)
	}
	return old, nil
}

func (p *Proc) formatAddr(addr uint32) string {
	if p.x2gameBase > 0 && uintptr(addr) >= p.x2gameBase && uintptr(addr) < p.x2gameBase+0x2000000 {
		off := uintptr(addr) - p.x2gameBase
		return fmt.Sprintf("x2game+0x%X", off)
	}
	if addr >= 0xBF000000 {
		return fmt.Sprintf("stack(0x%X)", addr)
	}
	if addr >= 0x10000000 && addr < 0x80000000 {
		return fmt.Sprintf("heap(0x%X)", addr)
	}
	return fmt.Sprintf("0x%X", addr)
}

func buildShellcode(logBuf uintptr, stolen []byte, caveAddr, returnAddr uintptr) []byte {
	var sc []byte
	w := func(b ...byte) { sc = append(sc, b...) }
	w32 := func(v uint32) {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		sc = append(sc, b...)
	}

	// pushad order: EAX, ECX, EDX, EBX, ESP, EBP, ESI, EDI
	// After pushad+pushfd (36 bytes):
	// [ESP+0x00] = EFLAGS
	// [ESP+0x04] = EDI
	// [ESP+0x08] = ESI
	// [ESP+0x0C] = EBP
	// [ESP+0x10] = ESP (original)
	// [ESP+0x14] = EBX
	// [ESP+0x18] = EDX
	// [ESP+0x1C] = ECX
	// [ESP+0x20] = EAX
	// [ESP+0x24] = return addr
	// [ESP+0x28] = arg1 (spawnInfo)
	// [ESP+0x2C] = arg2
	// [ESP+0x30] = arg3

	w(0x60, 0x9C) // pushad, pushfd

	// Get spawnInfo from stack
	w(0x8B, 0x74, 0x24, 0x28) // mov esi, [esp+0x28]
	w(0x85, 0xF6)
	jz := len(sc)
	w(0x74, 0x00)
	w(0x81, 0xFE)
	w32(0x10000)
	jb := len(sc)
	w(0x72, 0x00)

	w(0xBB)
	w32(uint32(logBuf))
	w(0x8B, 0x03)
	w(0x6B, 0xD0, ENTRY_SZ)
	w(0x01, 0xDA)
	w(0x83, 0xC2, HDR_SZ)

	// Save all registers and stack args
	// [edx+0] = EAX
	w(0x8B, 0x4C, 0x24, 0x20)
	w(0x89, 0x0A)
	// [edx+4] = EBX
	w(0x8B, 0x4C, 0x24, 0x14)
	w(0x89, 0x4A, 0x04)
	// [edx+8] = ECX
	w(0x8B, 0x4C, 0x24, 0x1C)
	w(0x89, 0x4A, 0x08)
	// [edx+12] = EDX
	w(0x8B, 0x4C, 0x24, 0x18)
	w(0x89, 0x4A, 0x0C)
	// [edx+16] = ESI
	w(0x8B, 0x4C, 0x24, 0x08)
	w(0x89, 0x4A, 0x10)
	// [edx+20] = EDI
	w(0x8B, 0x4C, 0x24, 0x04)
	w(0x89, 0x4A, 0x14)
	// [edx+24] = EBP
	w(0x8B, 0x4C, 0x24, 0x0C)
	w(0x89, 0x4A, 0x18)
	// [edx+28] = stack arg1 (spawnInfo)
	w(0x89, 0x72, 0x1C)
	// [edx+32] = stack arg2
	w(0x8B, 0x4C, 0x24, 0x2C)
	w(0x89, 0x4A, 0x20)
	// [edx+36] = stack arg3
	w(0x8B, 0x4C, 0x24, 0x30)
	w(0x89, 0x4A, 0x24)
	// [edx+40] = objId
	w(0x8B, 0x0E)
	w(0x89, 0x4A, 0x28)
	// [edx+44] = tmplId
	w(0x8B, 0x4E, 0x04)
	w(0x89, 0x4A, 0x2C)

	w(0x8B, 0x03)
	w(0x40)
	w(0x3D)
	w32(LOG_MAX)
	w(0x7C, 0x02)
	w(0x31, 0xC0)
	w(0x89, 0x03)
	w(0xF0, 0xFF, 0x43, 0x04)

	skip := len(sc)
	sc[jz+1] = byte(skip - jz - 2)
	sc[jb+1] = byte(skip - jb - 2)

	w(0x9D, 0x61)
	sc = append(sc, stolen...)
	w(0xE9)
	from := caveAddr + uintptr(len(sc)) + 4
	w32(uint32(int32(returnAddr) - int32(from)))
	return sc
}

func findStealLen(code []byte, min int) int {
	pos := 0
	for pos < min && pos < len(code) {
		b := code[pos]
		switch {
		case b >= 0x50 && b <= 0x5F:
			pos++
		case b == 0x89 || b == 0x8B:
			pos += 2 + decodeModRM(code[pos+1])
		case b == 0x83:
			pos += 3 + decodeModRM(code[pos+1])
		case b == 0x81:
			pos += 6 + decodeModRM(code[pos+1])
		default:
			pos++
		}
	}
	return pos
}

func decodeModRM(b byte) int {
	mod := (b >> 6) & 3
	rm := b & 7
	x := 0
	if mod == 3 {
		return 0
	}
	if rm == 4 {
		x++
	}
	switch mod {
	case 0:
		if rm == 5 {
			x += 4
		}
	case 1:
		x++
	case 2:
		x += 4
	}
	return x
}

func main() {
	fmt.Println("=== Full Register Capture ===")

	pid, err := findPID("archeage.exe")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("PID: %d\n", pid)

	p, err := openProc(pid)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer p.Close()

	fmt.Printf("x2game.dll: 0x%X\n", p.x2gameBase)

	prologue := make([]byte, 32)
	p.Read(TARGET_FUNC, prologue)
	stealLen := findStealLen(prologue, 5)
	stolen := make([]byte, stealLen)
	copy(stolen, prologue[:stealLen])

	logBuf, _ := p.Alloc(uintptr(HDR_SZ+LOG_MAX*ENTRY_SZ), PAGE_READWRITE)
	p.Write(logBuf, make([]byte, HDR_SZ))
	cave, _ := p.Alloc(256, PAGE_EXECUTE_READWRITE)

	sc := buildShellcode(logBuf, stolen, cave, TARGET_FUNC+uintptr(stealLen))
	p.Write(cave, sc)

	oldProt, _ := p.Protect(TARGET_FUNC, 16, PAGE_EXECUTE_READWRITE)
	jmp := make([]byte, stealLen)
	jmp[0] = 0xE9
	binary.LittleEndian.PutUint32(jmp[1:5], uint32(int32(cave)-int32(TARGET_FUNC+5)))
	for i := 5; i < stealLen; i++ {
		jmp[i] = 0x90
	}
	p.Write(TARGET_FUNC, jmp)

	fmt.Println("Hook instalado!\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\nRemovendo hook...")
		p.Write(TARGET_FUNC, stolen)
		p.Protect(TARGET_FUNC, 16, oldProt)
		os.Exit(0)
	}()

	var lastTotal, lastIdx uint32
	printed := 0

	for {
		hdr := make([]byte, HDR_SZ)
		p.Read(logBuf, hdr)
		wIdx := binary.LittleEndian.Uint32(hdr[0:4])
		total := binary.LittleEndian.Uint32(hdr[4:8])

		if total > lastTotal {
			for lastIdx != wIdx {
				addr := logBuf + uintptr(HDR_SZ) + uintptr(lastIdx)*ENTRY_SZ
				entry := make([]byte, ENTRY_SZ)
				p.Read(addr, entry)

				eax := binary.LittleEndian.Uint32(entry[0:4])
				ebx := binary.LittleEndian.Uint32(entry[4:8])
				ecx := binary.LittleEndian.Uint32(entry[8:12])
				edx := binary.LittleEndian.Uint32(entry[12:16])
				esi := binary.LittleEndian.Uint32(entry[16:20])
				edi := binary.LittleEndian.Uint32(entry[20:24])
				ebp := binary.LittleEndian.Uint32(entry[24:28])
				arg1 := binary.LittleEndian.Uint32(entry[28:32])
				arg2 := binary.LittleEndian.Uint32(entry[32:36])
				arg3 := binary.LittleEndian.Uint32(entry[36:40])
				objId := binary.LittleEndian.Uint32(entry[40:44])
				tmplId := binary.LittleEndian.Uint32(entry[44:48])

				if printed < 15 {
					printed++
					fmt.Printf("\n=== Spawn #%d: obj=%d tmpl=%d ===\n", printed, objId, tmplId)
					fmt.Printf("  EAX=%s  EBX=%s\n", p.formatAddr(eax), p.formatAddr(ebx))
					fmt.Printf("  ECX=%s  EDX=%s\n", p.formatAddr(ecx), p.formatAddr(edx))
					fmt.Printf("  ESI=%s  EDI=%s\n", p.formatAddr(esi), p.formatAddr(edi))
					fmt.Printf("  EBP=%s\n", p.formatAddr(ebp))
					fmt.Printf("  Arg1(spawn)=%s\n", p.formatAddr(arg1))
					fmt.Printf("  Arg2=%s  Arg3=%s\n", p.formatAddr(arg2), p.formatAddr(arg3))
				}

				lastIdx = (lastIdx + 1) % LOG_MAX
			}
			lastTotal = total
		}

		time.Sleep(50 * time.Millisecond)
	}
}