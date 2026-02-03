package patch

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const PAGE_EXECUTE_READWRITE = 0x40

type PatchEntry struct {
	Name     string
	Addr     uintptr
	Bytes    []byte
	Original []byte
	Active   bool
}

type Manager struct {
	handle  windows.Handle
	x2game  uintptr
	patches []PatchEntry
}

func NewManager(handle windows.Handle, x2game uintptr) *Manager {
	return &Manager{
		handle: handle,
		x2game: x2game,
	}
}

func (m *Manager) readBytes(addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var read uintptr
	windows.ReadProcessMemory(m.handle, addr, &buf[0], uintptr(size), &read)
	return buf
}

func (m *Manager) writeBytes(addr uintptr, data []byte) bool {
	size := uintptr(len(data))
	var oldProtect uint32
	kernel32 := windows.NewLazyDLL("kernel32.dll")
	vProtect := kernel32.NewProc("VirtualProtectEx")

	vProtect.Call(uintptr(m.handle), addr, size, PAGE_EXECUTE_READWRITE, uintptr(unsafe.Pointer(&oldProtect)))
	var written uintptr
	err := windows.WriteProcessMemory(m.handle, addr, &data[0], size, &written)
	vProtect.Call(uintptr(m.handle), addr, size, uintptr(oldProtect), uintptr(unsafe.Pointer(&oldProtect)))
	return err == nil
}

func (m *Manager) apply(name string, addr uintptr, patch []byte) bool {
	original := m.readBytes(addr, len(patch))

	entry := PatchEntry{
		Name:     name,
		Addr:     addr,
		Bytes:    patch,
		Original: original,
	}

	if m.writeBytes(addr, patch) {
		entry.Active = true
		m.patches = append(m.patches, entry)
		fmt.Printf("[PATCH] %s @ 0x%X [OK]\n", name, addr)
		return true
	}

	fmt.Printf("[PATCH] %s @ 0x%X [ERRO]\n", name, addr)
	return false
}

// ApplyAll aplica todos os patches de mount e GCD
func (m *Manager) ApplyAll() {
	fmt.Println("[PATCH] Aplicando patches...")

	// === MOUNT ===
	m.apply("MountValidation", 0x39526770, []byte{
		0x8B, 0x44, 0x24, 0x04,
		0xC7, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xC3,
	})

	m.apply("MountBlockCheck", 0x39127200, []byte{0x31, 0xC0, 0xC3})
	m.apply("CanMount", 0x390D64B0, []byte{0xB0, 0x01, 0xC3})
	m.apply("CanDismiss", 0x390217E0, []byte{0xB0, 0x01, 0xC3})

	// === GCD ===

	// Timer call NOP (add esp,8; nop; nop)
	m.apply("GCD timer NOP", 0x39053D05, []byte{0x83, 0xC4, 0x08, 0x90, 0x90})

	// Flag write 1→0
	flagCtx := m.readBytes(0x39053C83, 8)
	if flagCtx[0] == 0xC6 && flagCtx[1] == 0x86 && flagCtx[2] == 0xD9 &&
		flagCtx[3] == 0x05 && flagCtx[6] == 0x01 {
		m.apply("GCD flag 1→0", 0x39053C89, []byte{0x00})
	}

	// External check bypass (xor eax,eax; cmp al,0; nop*3)
	extCtx := m.readBytes(0x390DCC61, 8)
	if extCtx[0] == 0x80 && extCtx[1] == 0xBF && extCtx[2] == 0xD9 && extCtx[3] == 0x05 {
		m.apply("GCD ext bypass", 0x390DCC61, []byte{
			0x33, 0xC0, 0x3C, 0x00, 0x90, 0x90, 0x90,
		})
	}

	fmt.Printf("[PATCH] %d patches aplicados\n", len(m.patches))
}

// RestoreAll restaura todos os patches pros bytes originais
func (m *Manager) RestoreAll() {
	for i := len(m.patches) - 1; i >= 0; i-- {
		p := &m.patches[i]
		if p.Active {
			m.writeBytes(p.Addr, p.Original)
			p.Active = false
			fmt.Printf("[PATCH] Restaurado: %s\n", p.Name)
		}
	}
}

// GetStatus retorna resumo dos patches
func (m *Manager) GetStatus() string {
	active := 0
	for _, p := range m.patches {
		if p.Active {
			active++
		}
	}
	return fmt.Sprintf("Patches: %d/%d", active, len(m.patches))
}