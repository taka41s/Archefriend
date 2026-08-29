package target

import (
	"winkit/config"
	"winkit/memory"
	"fmt"
	"math"

	"golang.org/x/sys/windows"
)

// TargetBuff representa um buff/debuff do target
type TargetBuff struct {
	ID       uint32
	TypeID   uint32 // Para debuffs, indica tipo de CC
	Duration uint32 // ms
	TimeLeft uint32 // ms
	Stack    uint32
	Name     string
}

// TargetInfo contém todas as informações do target atual
type TargetInfo struct {
	Valid    bool
	ID       uint32
	Type     uint32
	Level    uint32
	HP       uint32
	MaxHP    uint32
	Mana     uint32
	MaxMana  uint32
	PosX     float32
	PosY     float32
	PosZ     float32
	Distance float32
	Buffs    []TargetBuff
	Debuffs  []TargetBuff
}

// Monitor monitora o target atual
type Monitor struct {
	handle  windows.Handle
	x2game  uintptr
	Target  TargetInfo
	Enabled bool

	// Resolves a target entity ID to its entity address (set by main.go via ESP entity list)
	ResolveEntityAddr func(entityID uint32) uint32

	// Callbacks
	OnBuffGained   func(buff TargetBuff)
	OnBuffLost     func(buffID uint32)
	OnDebuffGained func(debuff TargetBuff)
	OnDebuffLost   func(debuffID uint32)
	OnTargetChange func(oldID, newID uint32)

	// Estado anterior
	prevTargetID  uint32
	prevBuffIDs   map[uint32]bool
	prevDebuffIDs map[uint32]bool
}

// NewMonitor cria um novo monitor de target
func NewMonitor(handle windows.Handle, x2game uintptr) *Monitor {
	return &Monitor{
		handle:        handle,
		x2game:        x2game,
		Enabled:       true,
		prevBuffIDs:   make(map[uint32]bool),
		prevDebuffIDs: make(map[uint32]bool),
	}
}

// GetTargetBase retorna o endereço base da estrutura de target
func (m *Monitor) GetTargetBase() uint32 {
	return memory.ReadU32(m.handle, m.x2game+config.PTR_ENEMY_TARGET)
}

// Update atualiza todas as informações do target
func (m *Monitor) Update(playerX, playerY, playerZ float32) {
	if !m.Enabled {
		return
	}

	targetBase := m.GetTargetBase()
	if targetBase == 0 {
		if m.Target.Valid && m.OnTargetChange != nil {
			m.OnTargetChange(m.prevTargetID, 0)
		}
		m.Target.Valid = false
		m.Target.Buffs = nil
		m.Target.Debuffs = nil
		m.prevTargetID = 0
		return
	}

	base := uintptr(targetBase)
	m.Target.Valid = true

	// Ler informações básicas
	m.Target.ID = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_ID))
	m.Target.Type = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_TYPE))
	m.Target.Level = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_LEVEL))
	m.Target.HP = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_HP))
	m.Target.MaxHP = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_MAXHP))
	m.Target.Mana = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_MANA))
	m.Target.MaxMana = memory.ReadU32(m.handle, base+uintptr(config.OFF_TGT_MAXMANA))

	// Posição
	m.Target.PosX = memory.ReadF32(m.handle, base+uintptr(config.OFF_TGT_POS_X))
	m.Target.PosZ = memory.ReadF32(m.handle, base+uintptr(config.OFF_TGT_POS_Z))
	m.Target.PosY = memory.ReadF32(m.handle, base+uintptr(config.OFF_TGT_POS_Y))

	// Calcular distância
	dx := m.Target.PosX - playerX
	dy := m.Target.PosY - playerY
	dz := m.Target.PosZ - playerZ
	m.Target.Distance = float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))

	// Detectar mudança de target
	if m.Target.ID != m.prevTargetID && m.OnTargetChange != nil {
		m.OnTargetChange(m.prevTargetID, m.Target.ID)
		// Reset buff tracking quando muda de target
		m.prevBuffIDs = make(map[uint32]bool)
		m.prevDebuffIDs = make(map[uint32]bool)
	}
	m.prevTargetID = m.Target.ID

	// Ler buffs e debuffs
	m.updateBuffs()
	m.updateDebuffs()
}

// getBuffManagerAddr resolves the target's entity address via ResolveEntityAddr callback,
// then follows: entityAddr -> [+OFF_ENTITY_BASE] -> base -> [+OFF_DEBUFF_PTR] -> BuffManager
func (m *Monitor) getBuffManagerAddr() uintptr {
	if m.ResolveEntityAddr == nil || m.Target.ID == 0 {
		return 0
	}
	entityAddr := m.ResolveEntityAddr(m.Target.ID)
	if entityAddr == 0 {
		return 0
	}
	base := memory.ReadU32(m.handle, uintptr(entityAddr)+uintptr(config.OFF_ENTITY_BASE))
	if !memory.IsValidPtr(base) {
		return 0
	}
	buffMgr := memory.ReadU32(m.handle, uintptr(base)+uintptr(config.OFF_DEBUFF_PTR))
	if !memory.IsValidPtr(buffMgr) {
		return 0
	}
	return uintptr(buffMgr)
}

// updateBuffs lê os buffs do target via BuffManager
func (m *Monitor) updateBuffs() {
	var buffs []TargetBuff
	currentIDs := make(map[uint32]bool)

	buffMgr := m.getBuffManagerAddr()
	if buffMgr == 0 {
		// No buff manager — clear all
		for id := range m.prevBuffIDs {
			if m.OnBuffLost != nil {
				m.OnBuffLost(id)
			}
		}
		m.prevBuffIDs = make(map[uint32]bool)
		m.Target.Buffs = nil
		return
	}

	count := memory.ReadU32(m.handle, buffMgr+uintptr(config.OFF_BUFF_COUNT))

	if count > 0 && count < 50 {
		arrayAddr := buffMgr + uintptr(config.OFF_BUFF_ARRAY)

		for i := uint32(0); i < count; i++ {
			buffAddr := arrayAddr + uintptr(i*uint32(config.BUFF_SIZE))

			buffID := memory.ReadU32(m.handle, buffAddr+uintptr(config.BUFF_OFF_ID))
			if buffID == 0 || buffID > 9999999 {
				continue
			}

			duration := memory.ReadU32(m.handle, buffAddr+uintptr(config.BUFF_OFF_TIME_MAX))
			timeLeft := memory.ReadU32(m.handle, buffAddr+uintptr(config.BUFF_OFF_TIME_LEFT))
			stack := memory.ReadU32(m.handle, buffAddr+uintptr(config.BUFF_OFF_STACK))

			buff := TargetBuff{
				ID:       buffID,
				Duration: duration,
				TimeLeft: timeLeft,
				Stack:    stack,
			}

			buffs = append(buffs, buff)
			currentIDs[buffID] = true

			if !m.prevBuffIDs[buffID] && m.OnBuffGained != nil {
				m.OnBuffGained(buff)
			}
		}
	}

	for id := range m.prevBuffIDs {
		if !currentIDs[id] && m.OnBuffLost != nil {
			m.OnBuffLost(id)
		}
	}

	m.prevBuffIDs = currentIDs
	m.Target.Buffs = buffs
}

// updateDebuffs lê os debuffs do target via BuffManager
func (m *Monitor) updateDebuffs() {
	var debuffs []TargetBuff
	currentIDs := make(map[uint32]bool)

	buffMgr := m.getBuffManagerAddr()
	if buffMgr == 0 {
		for id := range m.prevDebuffIDs {
			if m.OnDebuffLost != nil {
				m.OnDebuffLost(id)
			}
		}
		m.prevDebuffIDs = make(map[uint32]bool)
		m.Target.Debuffs = nil
		return
	}

	count := memory.ReadU32(m.handle, buffMgr+uintptr(config.OFF_DEBUFF_COUNT))

	if count > 0 && count < 50 {
		arrayAddr := buffMgr + uintptr(config.OFF_DEBUFF_ARRAY)

		for i := uint32(0); i < count; i++ {
			debuffAddr := arrayAddr + uintptr(i*uint32(config.DEBUFF_SIZE))

			debuffID := memory.ReadU32(m.handle, debuffAddr)
			typeID := memory.ReadU32(m.handle, debuffAddr+4)

			if debuffID < 1 || debuffID > 50000 {
				continue
			}

			durMax := memory.ReadU32(m.handle, debuffAddr+0x30)
			durLeft := memory.ReadU32(m.handle, debuffAddr+0x34)

			debuff := TargetBuff{
				ID:       debuffID,
				TypeID:   typeID,
				Duration: durMax,
				TimeLeft: durLeft,
			}

			debuffs = append(debuffs, debuff)
			currentIDs[debuffID] = true

			if !m.prevDebuffIDs[debuffID] && m.OnDebuffGained != nil {
				m.OnDebuffGained(debuff)
			}
		}
	}

	for id := range m.prevDebuffIDs {
		if !currentIDs[id] && m.OnDebuffLost != nil {
			m.OnDebuffLost(id)
		}
	}

	m.prevDebuffIDs = currentIDs
	m.Target.Debuffs = debuffs
}

// HasBuff verifica se o target tem um buff específico
func (m *Monitor) HasBuff(buffID uint32) bool {
	for _, b := range m.Target.Buffs {
		if b.ID == buffID {
			return true
		}
	}
	return false
}

// HasDebuff verifica se o target tem um debuff específico
func (m *Monitor) HasDebuff(debuffID uint32) bool {
	for _, d := range m.Target.Debuffs {
		if d.ID == debuffID {
			return true
		}
	}
	return false
}

// GetHPPercent retorna a porcentagem de HP
func (m *Monitor) GetHPPercent() float32 {
	if m.Target.MaxHP == 0 {
		return 0
	}
	return float32(m.Target.HP) / float32(m.Target.MaxHP)
}

// GetManaPercent retorna a porcentagem de Mana
func (m *Monitor) GetManaPercent() float32 {
	if m.Target.MaxMana == 0 {
		return 0
	}
	return float32(m.Target.Mana) / float32(m.Target.MaxMana)
}

// GetTargetID retorna o ID do target atual
func (m *Monitor) GetTargetID() uint32 {
	if !m.Target.Valid {
		return 0
	}
	return m.Target.ID
}

// GetTargetHP retorna o HP atual e máximo do target
func (m *Monitor) GetTargetHP() (int32, int32) {
	if !m.Target.Valid {
		return -1, -1
	}
	return int32(m.Target.HP), int32(m.Target.MaxHP)
}

// DebugScanHP escaneia diferentes offsets ao redor de 0x300-0x350 procurando por HP
func (m *Monitor) DebugScanHP() {
	fmt.Printf("\n[DEBUG-HP] ========== TARGET DEBUG ==========\n")
	fmt.Printf("[DEBUG-HP] PTR_ENEMY_TARGET = x2game+0x%X\n", config.PTR_ENEMY_TARGET)

	ptrValue := memory.ReadU32(m.handle, m.x2game+config.PTR_ENEMY_TARGET)
	fmt.Printf("[DEBUG-HP] Valor do ponteiro: 0x%X\n", ptrValue)

	targetBase := m.GetTargetBase()
	fmt.Printf("[DEBUG-HP] Target base: 0x%X\n", targetBase)

	if targetBase == 0 {
		fmt.Println("[DEBUG-HP] ❌ Target base é 0! Nenhum target detectado ou ponteiro incorreto.")
		return
	}

	base := uintptr(targetBase)
	fmt.Printf("\n[DEBUG-HP] ✓ Target encontrado! Escaneando offsets...\n")
	fmt.Println("[DEBUG-HP] Offset | Valor (Decimal) | Valor (Hex)")
	fmt.Println("[DEBUG-HP] -------+----------------+-------------")

	// Escanear de 0x000 até 0x400 (range maior)
	for offset := uint32(0x000); offset <= 0x400; offset += 4 {
		val := memory.ReadU32(m.handle, base+uintptr(offset))
		// Mostrar valores que parecem HP/MaxHP (entre 1 e 1.000.000)
		if val > 0 && val < 1000000 {
			marker := ""
			if offset == config.OFF_TGT_HP {
				marker = " <- HP ATUAL (configurado)"
			} else if offset == config.OFF_TGT_MAXHP {
				marker = " <- HP MAX (configurado)"
			}
			fmt.Printf("[DEBUG-HP] 0x%03X  | %10d      | 0x%08X%s\n", offset, val, val, marker)
		}
	}

	fmt.Printf("\n[DEBUG-HP] Offsets atuais configurados:\n")
	fmt.Printf("[DEBUG-HP] OFF_TGT_HP     = 0x%X (valor lido: %d)\n", config.OFF_TGT_HP, m.Target.HP)
	fmt.Printf("[DEBUG-HP] OFF_TGT_MAXHP  = 0x%X (valor lido: %d)\n", config.OFF_TGT_MAXHP, m.Target.MaxHP)
	fmt.Printf("[DEBUG-HP] Target.Valid   = %v\n", m.Target.Valid)
	fmt.Printf("[DEBUG-HP] Target.ID      = %d\n", m.Target.ID)
	fmt.Println("[DEBUG-HP] ===================================\n")
}

// DebugTargetBuffs dumps the buff chain to diagnose target buff reading
func (m *Monitor) DebugTargetBuffs() {
	fmt.Println("\n[DEBUG-TBUFF] ========== TARGET BUFF DEBUG ==========")

	targetBase := m.GetTargetBase()
	fmt.Printf("[DEBUG-TBUFF] targetBase = 0x%X\n", targetBase)
	if targetBase == 0 {
		fmt.Println("[DEBUG-TBUFF] No target selected!")
		return
	}

	fmt.Printf("[DEBUG-TBUFF] Target ID: %d | HP: %d/%d\n", m.Target.ID, m.Target.HP, m.Target.MaxHP)

	// Resolve entity addr via ESP entity list
	if m.ResolveEntityAddr == nil {
		fmt.Println("[DEBUG-TBUFF] ResolveEntityAddr callback NOT SET!")
		return
	}

	entityAddr := m.ResolveEntityAddr(m.Target.ID)
	fmt.Printf("[DEBUG-TBUFF] ResolveEntityAddr(%d) = 0x%X\n", m.Target.ID, entityAddr)
	if entityAddr == 0 {
		fmt.Println("[DEBUG-TBUFF] Could not find entity in ESP list! Is AllEntities ESP enabled?")
		return
	}

	// Chain: entityAddr + OFF_ENTITY_BASE -> base
	base := memory.ReadU32(m.handle, uintptr(entityAddr)+uintptr(config.OFF_ENTITY_BASE))
	fmt.Printf("[DEBUG-TBUFF] entityAddr+0x%X (OFF_ENTITY_BASE) = 0x%X (valid: %v)\n", config.OFF_ENTITY_BASE, base, memory.IsValidPtr(base))
	if !memory.IsValidPtr(base) {
		fmt.Println("[DEBUG-TBUFF] entityBase INVALID - chain broken!")
		return
	}

	// Chain: base + OFF_DEBUFF_PTR -> buffMgr
	buffMgr := memory.ReadU32(m.handle, uintptr(base)+uintptr(config.OFF_DEBUFF_PTR))
	fmt.Printf("[DEBUG-TBUFF] base+0x%X (OFF_DEBUFF_PTR) = 0x%X (valid: %v)\n", config.OFF_DEBUFF_PTR, buffMgr, memory.IsValidPtr(buffMgr))
	if !memory.IsValidPtr(buffMgr) {
		fmt.Println("[DEBUG-TBUFF] buffMgr INVALID - chain broken!")
		return
	}

	// Read buff/debuff counts
	buffCount := memory.ReadU32(m.handle, uintptr(buffMgr)+uintptr(config.OFF_BUFF_COUNT))
	debuffCount := memory.ReadU32(m.handle, uintptr(buffMgr)+uintptr(config.OFF_DEBUFF_COUNT))
	fmt.Printf("[DEBUG-TBUFF] Buff count:   %d (at buffMgr+0x%X)\n", buffCount, config.OFF_BUFF_COUNT)
	fmt.Printf("[DEBUG-TBUFF] Debuff count: %d (at buffMgr+0x%X)\n", debuffCount, config.OFF_DEBUFF_COUNT)

	// Dump first few buffs
	if buffCount > 0 && buffCount < 50 {
		fmt.Println("[DEBUG-TBUFF] --- Buffs ---")
		arrayAddr := uintptr(buffMgr) + uintptr(config.OFF_BUFF_ARRAY)
		max := buffCount
		if max > 10 {
			max = 10
		}
		for i := uint32(0); i < max; i++ {
			entry := arrayAddr + uintptr(i)*uintptr(config.BUFF_SIZE)
			id := memory.ReadU32(m.handle, entry+uintptr(config.BUFF_OFF_ID))
			dur := memory.ReadU32(m.handle, entry+uintptr(config.BUFF_OFF_TIME_LEFT))
			stack := memory.ReadU32(m.handle, entry+uintptr(config.BUFF_OFF_STACK))
			fmt.Printf("[DEBUG-TBUFF]   [%d] ID=%d TimeLeft=%dms Stack=%d\n", i, id, dur, stack)
		}
	}

	if debuffCount > 0 && debuffCount < 50 {
		fmt.Println("[DEBUG-TBUFF] --- Debuffs ---")
		arrayAddr := uintptr(buffMgr) + uintptr(config.OFF_DEBUFF_ARRAY)
		max := debuffCount
		if max > 10 {
			max = 10
		}
		for i := uint32(0); i < max; i++ {
			entry := arrayAddr + uintptr(i)*uintptr(config.DEBUFF_SIZE)
			id := memory.ReadU32(m.handle, entry)
			typeID := memory.ReadU32(m.handle, entry+4)
			fmt.Printf("[DEBUG-TBUFF]   [%d] ID=%d TypeID=%d\n", i, id, typeID)
		}
	}

	fmt.Println("[DEBUG-TBUFF] ==========================================")
}
