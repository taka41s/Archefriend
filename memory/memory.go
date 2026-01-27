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

// ReadU8 lê um byte da memória
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

// ReadU16 lê 2 bytes da memória
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

// ReadU32 lê 4 bytes da memória
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

// ReadF32 lê um float32 da memória
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

// ReadBytes lê N bytes da memória
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

// ReadString lê uma string da memória
func ReadString(handle windows.Handle, addr uintptr, maxLen int) string {
	buf := ReadBytes(handle, addr, maxLen)
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// WriteU8 escreve um byte na memória
func WriteU8(handle windows.Handle, addr uintptr, val uint8) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 1,
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

// WriteU32 escreve 4 bytes na memória
func WriteU32(handle windows.Handle, addr uintptr, val uint32) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&val)), 4,
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

// WriteBytes escreve bytes na memória
func WriteBytes(handle windows.Handle, addr uintptr, data []byte) bool {
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	return ret != 0
}

// WriteBytesProtected escreve bytes com proteção de memória (para patches de código)
func WriteBytesProtected(handle windows.Handle, addr uintptr, data []byte) bool {
	var oldProtect uint32
	size := uintptr(len(data))

	// Muda proteção para executar+ler+escrever
	ProcVirtualProtectEx.Call(
		uintptr(handle), addr, size,
		PAGE_EXECUTE_READWRITE,
		uintptr(unsafe.Pointer(&oldProtect)),
	)

	// Escreve os bytes
	var written uintptr
	ret, _, _ := ProcWriteProcessMemory.Call(
		uintptr(handle), addr,
		uintptr(unsafe.Pointer(&data[0])), size,
		uintptr(unsafe.Pointer(&written)),
	)

	// Restaura proteção original
	ProcVirtualProtectEx.Call(
		uintptr(handle), addr, size,
		uintptr(oldProtect),
		uintptr(unsafe.Pointer(&oldProtect)),
	)

	// Flush instruction cache
	ProcFlushInstructionCache.Call(uintptr(handle), addr, size)

	return ret != 0
}

// IsValidPtr verifica se um ponteiro é válido
func IsValidPtr(ptr uint32) bool {
	return ptr > 0x10000 && ptr < 0x7FFFFFFF
}

// IsValidCoord verifica se uma coordenada é válida
func IsValidCoord(val float32) bool {
	return !math.IsNaN(float64(val)) && !math.IsInf(float64(val), 0) &&
		val > -100000 && val < 100000
}

// CalculateDistance calcula distância 3D
func CalculateDistance(x1, y1, z1, x2, y2, z2 float32) float32 {
	dx := x2 - x1
	dy := y2 - y1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

// CalculateDistance2D calcula distância 2D (ignora altura)
func CalculateDistance2D(x1, z1, x2, z2 float32) float32 {
	dx := x2 - x1
	dz := z2 - z1
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}

// BytesToUint32 converte bytes para uint32
func BytesToUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return *(*uint32)(unsafe.Pointer(&b[0]))
}

// BytesToFloat32 converte bytes para float32
func BytesToFloat32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return *(*float32)(unsafe.Pointer(&b[0]))
}
