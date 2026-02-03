package target

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	OFFSET_SET_TARGET     uintptr = 0x1BE090
	PTR_ENEMY_TARGET_BASE uintptr = 0x19EBF4
	OFF_TARGET_ID         uintptr = 0x08

	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	MEM_RELEASE            = 0x8000
	PAGE_EXECUTE_READWRITE = 0x40
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx    = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx     = kernel32.NewProc("VirtualFreeEx")
	procWriteProcessMem   = kernel32.NewProc("WriteProcessMemory")
	procReadProcessMem    = kernel32.NewProc("ReadProcessMemory")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
)

// SetTarget seleciona um target pelo UnitId usando CreateRemoteThread
// para chamar a função SetTarget do x2game.dll (__cdecl SetTarget(int unitId, int flag))
func SetTarget(handle windows.Handle, x2game uintptr, unitId uint32) error {
	setTargetAddr := x2game + OFFSET_SET_TARGET

	shellcode := []byte{
		// push 0          ; a2 = 0 (flag)
		0x6A, 0x00,
		// push unitId     ; a1 = unitId
		0x68, 0x00, 0x00, 0x00, 0x00,
		// mov eax, setTargetAddr
		0xB8, 0x00, 0x00, 0x00, 0x00,
		// call eax
		0xFF, 0xD0,
		// add esp, 8      ; limpa stack (cdecl)
		0x83, 0xC4, 0x08,
		// ret
		0xC3,
	}

	// Preenche unitId (offset 3)
	*(*uint32)(unsafe.Pointer(&shellcode[3])) = unitId
	// Preenche endereço da função (offset 8)
	*(*uint32)(unsafe.Pointer(&shellcode[8])) = uint32(setTargetAddr)

	allocAddr, err := virtualAllocEx(handle, 256)
	if err != nil {
		return fmt.Errorf("VirtualAllocEx falhou: %w", err)
	}
	defer virtualFreeEx(handle, allocAddr)

	if err := writeProcessMemory(handle, allocAddr, shellcode); err != nil {
		return fmt.Errorf("WriteProcessMemory falhou: %w", err)
	}

	threadHandle, err := createRemoteThread(handle, allocAddr)
	if err != nil {
		return fmt.Errorf("CreateRemoteThread falhou: %w", err)
	}
	defer windows.CloseHandle(threadHandle)

	windows.WaitForSingleObject(threadHandle, 5000)
	return nil
}

// GetCurrentTargetId retorna o UnitId do target atual (0 se nenhum)
func GetCurrentTargetId(handle windows.Handle, x2game uintptr) (uint32, error) {
	targetPtr := readU32(handle, x2game+PTR_ENEMY_TARGET_BASE)
	if targetPtr == 0 {
		return 0, nil
	}
	unitId := readU32(handle, uintptr(targetPtr)+OFF_TARGET_ID)
	return unitId, nil
}

// ClearTarget limpa o target atual (seta unitId 0)
func ClearTarget(handle windows.Handle, x2game uintptr) error {
	return SetTarget(handle, x2game, 0)
}

// --- helpers internos ---

func virtualAllocEx(handle windows.Handle, size uint32) (uintptr, error) {
	addr, _, err := procVirtualAllocEx.Call(
		uintptr(handle),
		0,
		uintptr(size),
		MEM_COMMIT|MEM_RESERVE,
		PAGE_EXECUTE_READWRITE,
	)
	if addr == 0 {
		return 0, err
	}
	return addr, nil
}

func virtualFreeEx(handle windows.Handle, addr uintptr) {
	procVirtualFreeEx.Call(uintptr(handle), addr, 0, MEM_RELEASE)
}

func writeProcessMemory(handle windows.Handle, addr uintptr, data []byte) error {
	var written uintptr
	ret, _, err := procWriteProcessMem.Call(
		uintptr(handle),
		addr,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

func readU32(handle windows.Handle, addr uintptr) uint32 {
	var val uint32
	var bytesRead uintptr
	procReadProcessMem.Call(
		uintptr(handle),
		addr,
		uintptr(unsafe.Pointer(&val)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	return val
}

func createRemoteThread(handle windows.Handle, addr uintptr) (windows.Handle, error) {
	ret, _, err := procCreateRemoteThread.Call(
		uintptr(handle),
		0,
		0,
		addr,
		0,
		0,
		0,
	)
	if ret == 0 {
		return 0, err
	}
	return windows.Handle(ret), nil
}