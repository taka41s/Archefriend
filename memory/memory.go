package memory

import (
	"math"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	ProcReadProcessMemory     = kernel32.NewProc("ReadProcessMemory")
	ProcWriteProcessMemory    = kernel32.NewProc("WriteProcessMemory")
	ProcVirtualProtectEx      = kernel32.NewProc("VirtualProtectEx")
	ProcFlushInstructionCache = kernel32.NewProc("FlushInstructionCache")
)

const (
	PAGE_EXECUTE_READWRITE = 0x40
)

func ReadU8(handle windows.Handle, addr uintptr) uint8 {
	var val uint8
	var read uintptr
	ProcReadProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 1,
		uintptr(unsafe.Pointer(&read)),
	)
	return val
}

func ReadU16(handle windows.Handle, addr uintptr) uint16 {
	var val uint16
	var read uintptr
	ProcReadProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 2,
		uintptr(unsafe.Pointer(&read)),
	)
	return val
}

func ReadU32(handle windows.Handle, addr uintptr) uint32 {
	var val uint32
	var read uintptr
	ProcReadProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 4,
		uintptr(unsafe.Pointer(&read)),
	)
	return val
}

func ReadF32(handle windows.Handle, addr uintptr) float32 {
	var val float32
	var read uintptr
	ProcReadProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 4,
		uintptr(unsafe.Pointer(&read)),
	)
	return val
}

func ReadBytes(handle windows.Handle, addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var read uintptr
	ProcReadProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(size),
		uintptr(unsafe.Pointer(&read)),
	)
	return buf
}

func ReadString(handle windows.Handle, addr uintptr, maxLen int) string {
	buf := ReadBytes(handle, addr, maxLen)
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

func WriteU8(handle windows.Handle, addr uintptr, val uint8) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 1,
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

func WriteU32(handle windows.Handle, addr uintptr, val uint32) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 4,
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

func WriteBytes(handle windows.Handle, addr uintptr, data []byte) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

func WriteBytesProtected(handle windows.Handle, addr uintptr, data []byte) bool {
	var oldProtect uint32
	size := uintptr(len(data))

	ProcVirtualProtectEx.Call(
		uintptr(handle), addr, size,
		PAGE_EXECUTE_READWRITE,
		uintptr(unsafe.Pointer(&oldProtect)),
	)

	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&data[0])), size,
		uintptr(unsafe.Pointer(&written)),
	)

	ProcVirtualProtectEx.Call(
		uintptr(handle), addr, size,
		uintptr(oldProtect),
		uintptr(unsafe.Pointer(&oldProtect)),
	)

	ProcFlushInstructionCache.Call(uintptr(handle), addr, size)

	return ret != 0
}

func IsValidPtr(ptr uint32) bool {
	return ptr > 0x10000
}

func IsValidCoord(val float32) bool {
	return !math.IsNaN(float64(val)) && !math.IsInf(float64(val), 0) &&
		val > -100000 && val < 100000
}

func CalculateDistance(x1, y1, z1, x2, y2, z2 float32) float32 {
	dx := x2 - x1
	dy := y2 - y1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

func CalculateDistance2D(x1, z1, x2, z2 float32) float32 {
	dx := x2 - x1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

func BytesToUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return *(*uint32)(unsafe.Pointer(&b[0]))
}

func BytesToFloat32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return *(*float32)(unsafe.Pointer(&b[0]))
}
