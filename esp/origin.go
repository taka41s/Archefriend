package esp

import (
	"encoding/binary"
	"sync"
	"unsafe"
)

// ============================================================================
// Seamless (floating) origin reader.
//
// ArcheAge recenters the world by whole 1024-unit grid steps on load / far
// travel, so the raw entity coords (0x830/0x834) shift between sessions. The
// STABLE coordinate is  absolute = local + gridX*1024 (and gridY*1024).
//
// From Ghidra (GetSeamlessOrigin, FUN_3966b580), the grid is behind a vtable
// getter:
//   gEnv   = *(0x39EA2074)          // gEnvPtr
//   obj0   = *(gEnv + 0x84)
//   segObj = obj0->vtable[0xC]()    // __thiscall(ecx=obj0)
//   gridX  = *(int*)(segObj + 0)
//   gridY  = *(int*)(segObj + 4)
//
// The vtable call is replicated in a tiny shellcode run via CreateRemoteThread
// (same mechanism as WorldToScreen — accepted here where SendInput is not).
// ============================================================================

const (
	originOutX = 0x100 // output slots inside the shellcode buffer
	originOutY = 0x104
)

var (
	originShellcode uintptr
	originMu        sync.Mutex
)

// ensureOriginShellcode allocates + writes the reader shellcode once. Caller
// holds originMu.
func (m *Manager) ensureOriginShellcode() {
	if originShellcode != 0 {
		return
	}
	addr, _, _ := procVirtualAllocEx.Call(m.processHandle, 0, ALLOC_SIZE,
		MEM_COMMIT|MEM_RESERVE, PAGE_EXECUTE_READWRITE)
	if addr == 0 {
		return
	}

	// 32-bit shellcode. Byte offsets of the two output-address imm32 fields are
	// 32 and 41 (patched below).
	sc := []byte{
		0xA1, 0x74, 0x20, 0xEA, 0x39, // mov eax,[0x39EA2074]   ; gEnv
		0x8B, 0x80, 0x84, 0x00, 0x00, 0x00, // mov eax,[eax+0x84]     ; obj0
		0x85, 0xC0, // test eax,eax
		0x74, 0x1F, // jz  fail
		0x8B, 0xC8, // mov ecx,eax            ; this
		0x8B, 0x11, // mov edx,[ecx]          ; vtable
		0x8B, 0x42, 0x0C, // mov eax,[edx+0x0C]     ; getter
		0xFF, 0xD0, // call eax               ; segObj -> eax
		0x85, 0xC0, // test eax,eax
		0x74, 0x12, // jz  fail
		0x8B, 0x10, // mov edx,[eax]          ; gridX
		0x89, 0x15, 0, 0, 0, 0, // mov [OUTX],edx         ; imm32 @ idx 32
		0x8B, 0x50, 0x04, // mov edx,[eax+4]        ; gridY
		0x89, 0x15, 0, 0, 0, 0, // mov [OUTY],edx         ; imm32 @ idx 41
		0xC3, // ret
		0xC3, // fail: ret
	}
	binary.LittleEndian.PutUint32(sc[32:36], uint32(addr+originOutX))
	binary.LittleEndian.PutUint32(sc[41:45], uint32(addr+originOutY))

	var written uintptr
	procWriteProcessMemory.Call(m.processHandle, addr,
		uintptr(unsafe.Pointer(&sc[0])), uintptr(len(sc)), uintptr(unsafe.Pointer(&written)))
	originShellcode = addr
}

// GetSeamlessOrigin returns the current floating-origin grid (gridX, gridY).
// ok=false if the chain isn't resolvable yet.
func (m *Manager) GetSeamlessOrigin() (gridX, gridY int32, ok bool) {
	originMu.Lock()
	defer originMu.Unlock()

	m.ensureOriginShellcode()
	if originShellcode == 0 {
		return 0, 0, false
	}

	th, _, _ := procCreateRemoteThread.Call(m.processHandle, 0, 0, originShellcode, 0, 0, 0)
	if th == 0 {
		return 0, 0, false
	}
	procWaitForSingleObject.Call(th, 5000)
	procCloseHandle.Call(th)

	gridX = int32(m.readU32(originShellcode + originOutX))
	gridY = int32(m.readU32(originShellcode + originOutY))
	return gridX, gridY, true
}

// OriginOffset returns the world-space origin offset in meters (gridX*1024).
func (m *Manager) OriginOffset() (ox, oy float32, ok bool) {
	gx, gy, gok := m.GetSeamlessOrigin()
	if !gok {
		return 0, 0, false
	}
	return float32(gx) * 1024.0, float32(gy) * 1024.0, true
}
