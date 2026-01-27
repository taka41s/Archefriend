package process

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	TH32CS_SNAPPROCESS  = 0x2
	TH32CS_SNAPMODULE   = 0x8
	TH32CS_SNAPMODULE32 = 0x10
	PROCESS_ALL_ACCESS  = 0x1F0FFF
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procModule32FirstW           = kernel32.NewProc("Module32FirstW")
	procModule32NextW            = kernel32.NewProc("Module32NextW")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

type PROCESSENTRY32W struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriClassBase      int32
	Flags             uint32
	ExeFile           [260]uint16
}

type MODULEENTRY32W struct {
	Size         uint32
	ModuleID     uint32
	ProcessID    uint32
	GlblcntUsage uint32
	ProccntUsage uint32
	ModBaseAddr  uintptr
	ModBaseSize  uint32
	HModule      uintptr
	Module       [256]uint16
	ExePath      [260]uint16
}

func utf16ToString(s []uint16) string {
	for i, v := range s {
		if v == 0 {
			s = s[:i]
			break
		}
	}
	runes := make([]rune, len(s))
	for i, v := range s {
		runes[i] = rune(v)
	}
	return string(runes)
}

// FindProcess encontra um processo pelo nome
func FindProcess(name string) (uint32, error) {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == ^uintptr(0) {
		return 0, fmt.Errorf("failed to create snapshot")
	}
	defer procCloseHandle.Call(snap)

	var pe PROCESSENTRY32W
	pe.Size = uint32(unsafe.Sizeof(pe))

	ret, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	if ret == 0 {
		return 0, fmt.Errorf("no processes found")
	}

	for {
		procName := utf16ToString(pe.ExeFile[:])
		if procName == name {
			return pe.ProcessID, nil
		}

		ret, _, _ := procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
		if ret == 0 {
			break
		}
	}

	return 0, fmt.Errorf("process %s not found", name)
}

// GetModuleBase obtém o endereço base de um módulo
func GetModuleBase(pid uint32, moduleName string) (uintptr, error) {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(
		TH32CS_SNAPMODULE|TH32CS_SNAPMODULE32,
		uintptr(pid),
	)
	if snap == 0 || snap == ^uintptr(0) {
		return 0, fmt.Errorf("failed to create module snapshot")
	}
	defer procCloseHandle.Call(snap)

	var me MODULEENTRY32W
	me.Size = uint32(unsafe.Sizeof(me))

	ret, _, _ := procModule32FirstW.Call(snap, uintptr(unsafe.Pointer(&me)))
	if ret == 0 {
		return 0, fmt.Errorf("no modules found")
	}

	for {
		name := utf16ToString(me.Module[:])
		if name == moduleName {
			return me.ModBaseAddr, nil
		}

		ret, _, _ := procModule32NextW.Call(snap, uintptr(unsafe.Pointer(&me)))
		if ret == 0 {
			break
		}
	}

	return 0, fmt.Errorf("module %s not found", moduleName)
}

// OpenProcess abre um processo para leitura/escrita
func OpenProcess(pid uint32) (windows.Handle, error) {
	handle, err := windows.OpenProcess(PROCESS_ALL_ACCESS, false, pid)
	if err != nil {
		return 0, err
	}
	return handle, nil
}
