package monitor

import (
	"archefriend/config"
	"archefriend/memory"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// BuffInfo representa informações de um buff
type BuffInfo struct {
	Index    int
	ID       uint32
	Duration uint32
	TimeLeft uint32
	Stack    uint32
	Name     string
}

// DebuffInfo representa informações de um debuff
type DebuffInfo struct {
	Index   int
	ID      uint32
	TypeID  uint32
	DurMax  uint32
	DurLeft uint32
	CCName  string
}

// BuffEvent representa um evento de buff/debuff
type BuffEvent struct {
	Time      time.Time
	Action    string // "+" ou "-"
	ID        uint32
	Name      string
	Reacted   bool
}

// BuffMonitor monitora buffs do player
type BuffMonitor struct {
	handle        windows.Handle
	x2game        uintptr
	Enabled       bool
	BuffListAddr  uintptr
	Buffs         []BuffInfo
	Events        []BuffEvent
	KnownIDs      map[uint32]bool
	RawCount      uint32

	// Callbacks
	OnBuffGained func(buff BuffInfo)
	OnBuffLost   func(buffID uint32)

	// Buffers
	buffBuffer []byte
}

// DebuffMonitor monitora debuffs do player
type DebuffMonitor struct {
	handle      windows.Handle
	x2game      uintptr
	Enabled     bool
	DebuffBase  uintptr
	Debuffs     []DebuffInfo
	Events      []BuffEvent
	KnownIDs    map[uint64]bool
	RawCount    uint32

	// Callbacks
	OnDebuffGained func(debuff DebuffInfo)
	OnDebuffLost   func(debuffID uint32)

	// Buffers
	debuffBuffer []byte
}

// NewBuffMonitor cria um novo monitor de buffs
func NewBuffMonitor(handle windows.Handle, x2game uintptr) *BuffMonitor {
	return &BuffMonitor{
		handle:     handle,
		x2game:     x2game,
		Enabled:    true,
		KnownIDs:   make(map[uint32]bool),
		Events:     make([]BuffEvent, 0, 100),
		buffBuffer: make([]byte, 30*config.BUFF_SIZE),
	}
}

// NewDebuffMonitor cria um novo monitor de debuffs
func NewDebuffMonitor(handle windows.Handle, x2game uintptr) *DebuffMonitor {
	return &DebuffMonitor{
		handle:       handle,
		x2game:       x2game,
		Enabled:      true,
		KnownIDs:     make(map[uint64]bool),
		Events:       make([]BuffEvent, 0, 100),
		debuffBuffer: make([]byte, 30*config.DEBUFF_SIZE),
	}
}

// MakeKey cria uma chave única para debuff (ID + TypeID)
func MakeKey(id, typeID uint32) uint64 {
	return uint64(id)<<32 | uint64(typeID)
}

// GetBuffListAddr obtém o endereço da lista de buffs
func (m *BuffMonitor) GetBuffListAddr(playerAddr uint32) uintptr {
	base := memory.ReadU32(m.handle, uintptr(playerAddr)+uintptr(config.OFF_ENTITY_BASE))
	if !memory.IsValidPtr(base) {
		return 0
	}
	listPtr := memory.ReadU32(m.handle, uintptr(base)+uintptr(config.OFF_DEBUFF_PTR))
	if !memory.IsValidPtr(listPtr) {
		return 0
	}
	return uintptr(listPtr)
}

// Update atualiza os buffs do player
func (m *BuffMonitor) Update(playerAddr uint32) {
	if !m.Enabled || playerAddr == 0 {
		return
	}

	m.BuffListAddr = m.GetBuffListAddr(playerAddr)
	if m.BuffListAddr == 0 {
		return
	}

	count := memory.ReadU32(m.handle, m.BuffListAddr+uintptr(config.OFF_BUFF_COUNT))
	m.RawCount = count

	if count == 0 || count > 50 {
		// Notificar sobre todos os buffs perdidos
		for buffID := range m.KnownIDs {
			if m.OnBuffLost != nil {
				m.OnBuffLost(buffID)
			}
			delete(m.KnownIDs, buffID)
		}
		m.Buffs = m.Buffs[:0]
		return
	}

	arrayAddr := m.BuffListAddr + uintptr(config.OFF_BUFF_ARRAY)
	totalSize := int(count) * config.BUFF_SIZE
	if totalSize > len(m.buffBuffer) {
		totalSize = len(m.buffBuffer)
	}

	var bytesRead uintptr
	ret, _, _ := memory.ProcReadProcessMemory.Call(
		uintptr(m.handle), arrayAddr,
		uintptr(unsafe.Pointer(&m.buffBuffer[0])),
		uintptr(totalSize),
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	if ret == 0 {
		return
	}

	newBuffs := m.Buffs[:0]
	currentIDs := make(map[uint32]bool, count)

	maxItems := int(bytesRead) / config.BUFF_SIZE
	if maxItems > 30 {
		maxItems = 30
	}

	for i := 0; i < maxItems; i++ {
		offset := i * config.BUFF_SIZE

		buffID := memory.BytesToUint32(m.buffBuffer[offset+int(config.BUFF_OFF_ID) : offset+int(config.BUFF_OFF_ID)+4])
		duration := memory.BytesToUint32(m.buffBuffer[offset+int(config.BUFF_OFF_TIME_MAX) : offset+int(config.BUFF_OFF_TIME_MAX)+4])
		timeLeft := memory.BytesToUint32(m.buffBuffer[offset+int(config.BUFF_OFF_TIME_LEFT) : offset+int(config.BUFF_OFF_TIME_LEFT)+4])
		stack := memory.BytesToUint32(m.buffBuffer[offset+int(config.BUFF_OFF_STACK) : offset+int(config.BUFF_OFF_STACK)+4])

		if buffID < 1000 || buffID > 9999999 {
			continue
		}

		currentIDs[buffID] = true

		buff := BuffInfo{
			Index:    i,
			ID:       buffID,
			Duration: duration,
			TimeLeft: timeLeft,
			Stack:    stack,
		}

		// Detectar novo buff
		if !m.KnownIDs[buffID] {
			m.KnownIDs[buffID] = true
			m.AddEvent("+", buffID, buff.Name, false)
			if m.OnBuffGained != nil {
				m.OnBuffGained(buff)
			}
		}

		newBuffs = append(newBuffs, buff)
	}

	// Detectar buffs perdidos
	for id := range m.KnownIDs {
		if !currentIDs[id] {
			delete(m.KnownIDs, id)
			m.AddEvent("-", id, "", false)
			if m.OnBuffLost != nil {
				m.OnBuffLost(id)
			}
		}
	}

	m.Buffs = newBuffs
}

// AddEvent adiciona um evento ao histórico
func (m *BuffMonitor) AddEvent(action string, id uint32, name string, reacted bool) {
	event := BuffEvent{
		Time:    time.Now(),
		Action:  action,
		ID:      id,
		Name:    name,
		Reacted: reacted,
	}
	m.Events = append(m.Events, event)
	if len(m.Events) > 100 {
		m.Events = m.Events[1:]
	}
}

// GetDebuffBase obtém o endereço base dos debuffs
func (m *DebuffMonitor) GetDebuffBase(playerAddr uint32) uintptr {
	base := memory.ReadU32(m.handle, uintptr(playerAddr)+uintptr(config.OFF_ENTITY_BASE))
	if !memory.IsValidPtr(base) {
		return 0
	}
	debuffBase := memory.ReadU32(m.handle, uintptr(base)+uintptr(config.OFF_DEBUFF_PTR))
	if !memory.IsValidPtr(debuffBase) {
		return 0
	}
	return uintptr(debuffBase)
}

// Update atualiza os debuffs do player
func (m *DebuffMonitor) Update(playerAddr uint32) {
	if !m.Enabled {
		return
	}

	if playerAddr == 0 {
		// Não printa aqui pois é esperado quando não está conectado
		return
	}

	m.DebuffBase = m.GetDebuffBase(playerAddr)
	if m.DebuffBase == 0 {
		return
	}

	count := memory.ReadU32(m.handle, m.DebuffBase+uintptr(config.OFF_DEBUFF_COUNT))
	m.RawCount = count

	if count == 0 || count > 50 {
		// Notificar sobre todos os debuffs perdidos
		for key := range m.KnownIDs {
			id := uint32(key >> 32)
			typeID := uint32(key & 0xFFFFFFFF)
			fmt.Printf("[MONITOR] Debuff removido (count=0)! ID:%d TypeID:%d\n", id, typeID)
			if m.OnDebuffLost != nil {
				m.OnDebuffLost(typeID)
			}
			delete(m.KnownIDs, key)
		}
		m.Debuffs = m.Debuffs[:0]
		return
	}

	arrayAddr := m.DebuffBase + uintptr(config.OFF_DEBUFF_ARRAY)
	totalSize := int(count) * config.DEBUFF_SIZE
	if totalSize > len(m.debuffBuffer) {
		totalSize = len(m.debuffBuffer)
	}

	var bytesRead uintptr
	ret, _, _ := memory.ProcReadProcessMemory.Call(
		uintptr(m.handle), arrayAddr,
		uintptr(unsafe.Pointer(&m.debuffBuffer[0])),
		uintptr(totalSize),
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	if ret == 0 {
		return
	}

	newDebuffs := m.Debuffs[:0]
	currentIDs := make(map[uint64]bool, count)

	maxItems := int(bytesRead) / config.DEBUFF_SIZE

	for i := 0; i < maxItems; i++ {
		offset := i * config.DEBUFF_SIZE

		id := memory.BytesToUint32(m.debuffBuffer[offset : offset+4])
		typeID := memory.BytesToUint32(m.debuffBuffer[offset+4 : offset+8])
		durMax := memory.BytesToUint32(m.debuffBuffer[offset+0x30 : offset+0x34])
		durLeft := memory.BytesToUint32(m.debuffBuffer[offset+0x34 : offset+0x38])

		if id < 1 || id > 50000 || durMax < 1000 || durMax > 300000 {
			continue
		}

		key := MakeKey(id, typeID)
		currentIDs[key] = true

		debuff := DebuffInfo{
			Index:   i,
			ID:      id,
			TypeID:  typeID,
			DurMax:  durMax,
			DurLeft: durLeft,
		}

		// Detectar novo debuff
		if !m.KnownIDs[key] {
			m.KnownIDs[key] = true
			m.AddEvent("+", id, typeID, "", false)
			fmt.Printf("[MONITOR] Novo debuff! ID:%d TypeID:%d Key:0x%X DurMax:%d DurLeft:%d\n",
				id, typeID, key, durMax, durLeft)
			if m.OnDebuffGained != nil {
				m.OnDebuffGained(debuff)
			}
		} else {
			// Debuff já conhecido, apenas atualizando duração
			// fmt.Printf("[MONITOR] Debuff existente TypeID:%d DurLeft:%d\n", typeID, durLeft)
		}

		newDebuffs = append(newDebuffs, debuff)
	}

	// Detectar debuffs perdidos
	for key := range m.KnownIDs {
		if !currentIDs[key] {
			delete(m.KnownIDs, key)
			id := uint32(key >> 32)
			typeID := uint32(key & 0xFFFFFFFF)
			m.AddEvent("-", id, typeID, "", false)
			fmt.Printf("[MONITOR] Debuff removido! ID:%d TypeID:%d Key:0x%X\n", id, typeID, key)
			if m.OnDebuffLost != nil {
				fmt.Printf("[MONITOR] Chamando OnDebuffLost para TypeID:%d\n", typeID)
				m.OnDebuffLost(typeID)
			} else {
				fmt.Printf("[MONITOR] OnDebuffLost callback é nil!\n")
			}
		}
	}

	m.Debuffs = newDebuffs
}

// AddEvent adiciona um evento ao histórico
func (m *DebuffMonitor) AddEvent(action string, id, typeID uint32, name string, reacted bool) {
	event := BuffEvent{
		Time:    time.Now(),
		Action:  action,
		ID:      id,
		Name:    fmt.Sprintf("%s (T:%d)", name, typeID),
		Reacted: reacted,
	}
	m.Events = append(m.Events, event)
	if len(m.Events) > 100 {
		m.Events = m.Events[1:]
	}
}

// HasBuff verifica se tem um buff específico
func (m *BuffMonitor) HasBuff(buffID uint32) bool {
	for _, b := range m.Buffs {
		if b.ID == buffID {
			return true
		}
	}
	return false
}

// HasDebuff verifica se tem um debuff específico
func (m *DebuffMonitor) HasDebuff(debuffID uint32) bool {
	for _, d := range m.Debuffs {
		if d.ID == debuffID {
			return true
		}
	}
	return false
}
