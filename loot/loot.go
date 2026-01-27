package loot

import (
	"archefriend/config"
	"archefriend/memory"
	"fmt"

	"golang.org/x/sys/windows"
)

// PatchInfo armazena informação sobre um patch de memória
type PatchInfo struct {
	Offset   uintptr
	Patch    []byte // bytes do patch
	Original []byte // bytes originais (para restaurar)
}

// Bypass gerencia os patches de loot reach e doodad distance
type Bypass struct {
	handle         windows.Handle
	x2game         uintptr
	lootEnabled    bool
	doodadEnabled  bool
	lootPatches    []PatchInfo
	doodadPatches  []PatchInfo
}

// NewBypass cria um novo loot bypass
func NewBypass(handle windows.Handle, x2game uintptr) *Bypass {
	bypass := &Bypass{
		handle: handle,
		x2game: x2game,
	}

	// Define os patches de LOOT
	bypass.lootPatches = []PatchInfo{
		{
			Offset:   config.OFF_LOOT_GENERIC_CHECK,
			Patch:    []byte{0xD9, 0xEE, 0x90, 0x90, 0x90}, // fldz (push 0.0)
			Original: make([]byte, 5),
		},
		{
			Offset:   config.OFF_LOOT_CAN_LOOT,
			Patch:    []byte{0xB0, 0x01, 0x90, 0x90, 0x90}, // mov al, 1 (sempre retorna true)
			Original: make([]byte, 5),
		},
		{
			Offset:   config.OFF_LOOT_HANDLER_DIST,
			Patch:    []byte{0xD9, 0xEE, 0x90, 0x90, 0x90}, // fldz (push 0.0)
			Original: make([]byte, 5),
		},
	}

	// Define os patches de DOODAD
	bypass.doodadPatches = []PatchInfo{
		{
			Offset:   config.OFF_DOODAD_DISTANCE_CHECK,
			Patch:    []byte{0xB0, 0x01, 0xC3}, // mov al, 1; ret (sempre retorna true)
			Original: make([]byte, 3),
		},
	}

	// Salva bytes originais dos patches de loot
	for i := range bypass.lootPatches {
		bypass.lootPatches[i].Original = memory.ReadBytes(handle, x2game+bypass.lootPatches[i].Offset, len(bypass.lootPatches[i].Original))
	}

	// Salva bytes originais dos patches de doodad
	for i := range bypass.doodadPatches {
		bypass.doodadPatches[i].Original = memory.ReadBytes(handle, x2game+bypass.doodadPatches[i].Offset, len(bypass.doodadPatches[i].Original))
	}

	return bypass
}

// ToggleLoot liga/desliga o bypass de loot
func (b *Bypass) ToggleLoot() bool {
	if b.lootEnabled {
		// Restaura bytes originais
		success := true
		for _, p := range b.lootPatches {
			addr := b.x2game + p.Offset
			if !memory.WriteBytesProtected(b.handle, addr, p.Original) {
				fmt.Printf("[ERROR] LOOT: Falha ao restaurar patch em 0x%X\n", addr)
				success = false
			}
		}
		if success {
			b.lootEnabled = false
		} else {
			fmt.Println("[ERROR] LOOT: Falha ao desativar - alguns patches falharam")
		}
		return success
	} else {
		// Aplica patches
		success := true
		for _, p := range b.lootPatches {
			addr := b.x2game + p.Offset
			if !memory.WriteBytesProtected(b.handle, addr, p.Patch) {
				fmt.Printf("[ERROR] LOOT: Falha ao aplicar patch em 0x%X\n", addr)
				success = false
			}
		}
		if success {
			b.lootEnabled = true
		} else {
			fmt.Println("[ERROR] LOOT: Falha ao ativar - alguns patches falharam")
		}
		return success
	}
}

// ToggleDoodad liga/desliga o bypass de doodad
func (b *Bypass) ToggleDoodad() bool {
	if b.doodadEnabled {
		// Restaura bytes originais
		success := true
		for _, p := range b.doodadPatches {
			addr := b.x2game + p.Offset
			if !memory.WriteBytesProtected(b.handle, addr, p.Original) {
				fmt.Printf("[ERROR] DOODAD: Falha ao restaurar patch em 0x%X\n", addr)
				success = false
			}
		}
		if success {
			b.doodadEnabled = false
		} else {
			fmt.Println("[ERROR] DOODAD: Falha ao desativar - alguns patches falharam")
		}
		return success
	} else {
		// Aplica patches
		success := true
		for _, p := range b.doodadPatches {
			addr := b.x2game + p.Offset
			if !memory.WriteBytesProtected(b.handle, addr, p.Patch) {
				fmt.Printf("[ERROR] DOODAD: Falha ao aplicar patch em 0x%X\n", addr)
				success = false
			}
		}
		if success {
			b.doodadEnabled = true
		} else {
			fmt.Println("[ERROR] DOODAD: Falha ao ativar - alguns patches falharam")
		}
		return success
	}
}

// IsLootEnabled retorna se o bypass de loot está ativo
func (b *Bypass) IsLootEnabled() bool {
	return b.lootEnabled
}

// IsDoodadEnabled retorna se o bypass de doodad está ativo
func (b *Bypass) IsDoodadEnabled() bool {
	return b.doodadEnabled
}

// Cleanup restaura os bytes originais se necessário
func (b *Bypass) Cleanup() {
	if b.lootEnabled {
		for _, p := range b.lootPatches {
			memory.WriteBytesProtected(b.handle, b.x2game+p.Offset, p.Original)
		}
		b.lootEnabled = false
	}
	if b.doodadEnabled {
		for _, p := range b.doodadPatches {
			memory.WriteBytesProtected(b.handle, b.x2game+p.Offset, p.Original)
		}
		b.doodadEnabled = false
	}
}
