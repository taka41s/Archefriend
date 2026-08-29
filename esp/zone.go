package esp

// ============================================================================
// SubZoneManager — current zone (sub-zone) id of the local character.
//
// Derivado via Ghidra no x2game.dll 32-bit. Mesmo padrão de resolução do
// DoodadManager (global -> deref -> deref):
//
//   holder = *(x2game + 0xE9BB80)   // Manager holder
//   mgr    = *(holder)              // SubZoneManager*
//   zoneId = *(mgr + 0x10)          // current sub-zone id (uint32)
//
// Referências (no binário):
//   - X2::GameClient::SubZoneManager::Update  @ FUN_39179240
//     chamado no update loop:  MOV EDX,[0x39e9bb80]; MOV ECX,[EDX]; CALL Update
//   - debug print lê  [this+0x10]  -> "current sub zone : %u"  (0x3917dcfe)
//   - OnEnter grava o novo id em  [this+0x10]  e o anterior em  [this+0x20]
// ============================================================================

const (
	// x2game-relative: aponta para o holder do SubZoneManager.
	OFF_SUBZONE_MGR uintptr = 0xE9BB80
	// offset do sub-zone id atual dentro do SubZoneManager.
	OFF_SUBZONE_CURRENT uint32 = 0x10
	// sub-zone id anterior (para detectar troca de zona).
	OFF_SUBZONE_PREVIOUS uint32 = 0x20
)

// GetZoneID retorna o sub-zone id atual do personagem local.
// ok=false se a cadeia de ponteiros ainda não estiver resolvida (loading/etc).
func (m *Manager) GetZoneID() (uint32, bool) {
	holder := m.readU32(m.x2game + OFF_SUBZONE_MGR)
	if holder == 0 {
		return 0, false
	}
	mgr := m.readU32(uintptr(holder))
	if mgr == 0 {
		return 0, false
	}
	return m.readU32(uintptr(mgr) + uintptr(OFF_SUBZONE_CURRENT)), true
}
