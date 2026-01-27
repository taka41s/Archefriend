package monitor

import (
	"archefriend/config"
	"archefriend/input"
	"archefriend/memory"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type BuffInfo struct {
	Index    int
	ID       uint32
	Duration uint32
	TimeLeft uint32
	Stack    uint32
	Name     string
}

type DebuffInfo struct {
	Index   int
	ID      uint32
	TypeID  uint32
	DurMax  uint32
	DurLeft uint32
	CCName  string
}

type BuffEvent struct {
	Time      time.Time
	Action    string // "+" ou "-"
	ID        uint32
	Name      string
	Reacted   bool
}


type BuffWhitelistEntry struct {
	Type     uint32         `json:"type"`
	Name     string         `json:"name"`
	Use      string         `json:"use"`
	KeyCombo input.KeyCombo `json:"-"`
}

type BuffWhitelist struct {
	Entries      []BuffWhitelistEntry
	TypeMap      map[uint32]*BuffWhitelistEntry
	Enabled      bool
	Reactions    int
	SpamCount    int
	SpamInterval time.Duration
	lastSpamTime time.Time
	spamCooldown time.Duration
}

func NewBuffWhitelist() *BuffWhitelist {
	wl := &BuffWhitelist{
		Entries:      make([]BuffWhitelistEntry, 0),
		TypeMap:      make(map[uint32]*BuffWhitelistEntry),
		Enabled:      true,
		SpamCount:    config.KEY_SPAM_COUNT,
		SpamInterval: config.KEY_SPAM_INTERVAL,
		spamCooldown: 100 * time.Millisecond,
	}
	wl.LoadFromFile("buff_whitelist.json")
	return wl
}

func (wl *BuffWhitelist) LoadFromFile(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		wl.createDefaultFile(filename)
		return
	}

	var entries []BuffWhitelistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}

	wl.Entries = entries
	wl.TypeMap = make(map[uint32]*BuffWhitelistEntry)

	for i := range wl.Entries {
		wl.Entries[i].KeyCombo = input.ParseKeyCombo(wl.Entries[i].Use)
		if wl.Entries[i].KeyCombo.MainKey != 0 {
			wl.TypeMap[wl.Entries[i].Type] = &wl.Entries[i]
		}
	}
}

func (wl *BuffWhitelist) createDefaultFile(filename string) {
	defaultEntries := []BuffWhitelistEntry{
		{Type: 87, Name: "Hell Spear", Use: "F10"},
		{Type: 243, Name: "stun", Use: "SHIFT+1"},
		{Type: 156, Name: "Fear", Use: "CTRL+2"},
		{Type: 21402, Name: "Deafened", Use: "ALT+F1"},
		{Type: 8000210, Name: "Clash Dummy", Use: "SHIFT+5"},
		{Type: 21, Name: "Tripped (Strong)", Use: "CTRL+SHIFT+1"},
		{Type: 141, Name: "Tripped", Use: "9"},
		{Type: 6860, Name: "Impaled", Use: "SHIFT+F10"},
		{Type: 18396, Name: "Skewer", Use: "F10"},
		{Type: 2458, Name: "Snare (charge)", Use: "F11"},
		{Type: 6829, Name: "Throw Dagger", Use: "CTRL+F11"},
		{Type: 501, Name: "Shield Slam", Use: "F10"},
		{Type: 3601, Name: "Overrun", Use: "SHIFT+F12"},
	}

	data, _ := json.MarshalIndent(defaultEntries, "", "  ")
	os.WriteFile(filename, data, 0644)

	wl.Entries = defaultEntries
	wl.TypeMap = make(map[uint32]*BuffWhitelistEntry)
	for i := range wl.Entries {
		wl.Entries[i].KeyCombo = input.ParseKeyCombo(wl.Entries[i].Use)
		if wl.Entries[i].KeyCombo.MainKey != 0 {
			wl.TypeMap[wl.Entries[i].Type] = &wl.Entries[i]
		}
	}
}

func (wl *BuffWhitelist) ReactInstant(buffID uint32) (bool, string) {
	if !wl.Enabled {
		return false, ""
	}

	entry, exists := wl.TypeMap[buffID]
	if !exists {
		return false, ""
	}

	if time.Since(wl.lastSpamTime) < wl.spamCooldown {
		return false, ""
	}

	wl.lastSpamTime = time.Now()
	go input.SpamKey(entry.KeyCombo.RawString, wl.SpamCount, wl.SpamInterval)

	wl.Reactions++
	return true, entry.Name
}

func (wl *BuffWhitelist) GetName(buffID uint32) string {
	if entry, exists := wl.TypeMap[buffID]; exists {
		return entry.Name
	}
	return ""
}


type CCWhitelistEntry struct {
	Type     uint32         `json:"type"`
	Name     string         `json:"name"`
	Use      string         `json:"use"`
	KeyCombo input.KeyCombo `json:"-"`
}

type CCWhitelist struct {
	Entries      []CCWhitelistEntry
	TypeMap      map[uint32]*CCWhitelistEntry
	Enabled      bool
	Reactions    int
	SpamCount    int
	SpamInterval time.Duration
	lastSpamTime time.Time
	spamCooldown time.Duration
}

func NewCCWhitelist() *CCWhitelist {
	wl := &CCWhitelist{
		Entries:      make([]CCWhitelistEntry, 0),
		TypeMap:      make(map[uint32]*CCWhitelistEntry),
		Enabled:      true,
		SpamCount:    config.KEY_SPAM_COUNT,
		SpamInterval: config.KEY_SPAM_INTERVAL,
		spamCooldown: 100 * time.Millisecond,
	}
	wl.LoadFromFile("cc_whitelist.json")
	return wl
}

func (wl *CCWhitelist) LoadFromFile(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		wl.createDefaultFile(filename)
		return
	}

	var entries []CCWhitelistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}

	wl.Entries = entries
	wl.TypeMap = make(map[uint32]*CCWhitelistEntry)

	for i := range wl.Entries {
		wl.Entries[i].KeyCombo = input.ParseKeyCombo(wl.Entries[i].Use)
		if wl.Entries[i].KeyCombo.MainKey != 0 {
			wl.TypeMap[wl.Entries[i].Type] = &wl.Entries[i]
		}
	}
}

func (wl *CCWhitelist) createDefaultFile(filename string) {
	defaultEntries := []CCWhitelistEntry{
		{Type: 3601, Name: "stun", Use: "F12"},
		{Type: 509, Name: "knockdown", Use: "SHIFT+F12"},
		{Type: 4622, Name: "sleep", Use: "CTRL+F11"},
		{Type: 6800, Name: "fear", Use: "F12"},
		{Type: 20121, Name: "silence", Use: "SHIFT+1"},
		{Type: 22290, Name: "root", Use: "CTRL+2"},
	}

	data, _ := json.MarshalIndent(defaultEntries, "", "  ")
	os.WriteFile(filename, data, 0644)

	wl.Entries = defaultEntries
	wl.TypeMap = make(map[uint32]*CCWhitelistEntry)
	for i := range wl.Entries {
		wl.Entries[i].KeyCombo = input.ParseKeyCombo(wl.Entries[i].Use)
		if wl.Entries[i].KeyCombo.MainKey != 0 {
			wl.TypeMap[wl.Entries[i].Type] = &wl.Entries[i]
		}
	}
}

func (wl *CCWhitelist) ReactInstant(typeID uint32) (bool, string) {
	if !wl.Enabled {
		return false, ""
	}
	entry, exists := wl.TypeMap[typeID]
	if !exists {
		return false, ""
	}

	if time.Since(wl.lastSpamTime) < wl.spamCooldown {
		return false, ""
	}

	wl.lastSpamTime = time.Now()
	go input.SpamKey(entry.KeyCombo.RawString, wl.SpamCount, wl.SpamInterval)

	wl.Reactions++
	return true, entry.Name
}

func (wl *CCWhitelist) GetName(typeID uint32) string {
	if entry, exists := wl.TypeMap[typeID]; exists {
		return entry.Name
	}
	return ""
}

type ReactionHandler interface {
	OnBuffGained(buffID uint32)
	OnBuffLost(buffID uint32)
	OnDebuffGained(debuffID uint32)
	OnDebuffLost(debuffID uint32)
	IsEnabled() bool
}

type BuffMonitor struct {
	handle          windows.Handle
	x2game          uintptr
	Enabled         bool
	BuffListAddr    uintptr
	Buffs           []BuffInfo
	Events          []BuffEvent
	KnownIDs        map[uint32]bool
	RawCount        uint32
	Whitelist       *BuffWhitelist
	ReactionHandler ReactionHandler

	OnBuffGained func(buff BuffInfo)
	OnBuffLost   func(buffID uint32)

	buffBuffer []byte
}

type DebuffMonitor struct {
	handle          windows.Handle
	x2game          uintptr
	Enabled         bool
	DebuffBase      uintptr
	Debuffs         []DebuffInfo
	Events          []BuffEvent
	KnownIDs        map[uint64]bool
	RawCount        uint32
	CCWhitelist     *CCWhitelist
	ReactionHandler ReactionHandler

	OnDebuffGained func(debuff DebuffInfo)
	OnDebuffLost   func(debuffID uint32)

	debuffBuffer []byte
}

func NewBuffMonitor(handle windows.Handle, x2game uintptr) *BuffMonitor {
	return &BuffMonitor{
		handle:     handle,
		x2game:     x2game,
		Enabled:    true,
		KnownIDs:   make(map[uint32]bool),
		Events:     make([]BuffEvent, 0, 100),
		buffBuffer: make([]byte, 30*config.BUFF_SIZE),
		Whitelist:  NewBuffWhitelist(),
	}
}

func (m *BuffMonitor) SetReactionHandler(handler ReactionHandler) {
	m.ReactionHandler = handler
}

func NewDebuffMonitor(handle windows.Handle, x2game uintptr) *DebuffMonitor {
	return &DebuffMonitor{
		handle:       handle,
		x2game:       x2game,
		Enabled:      true,
		KnownIDs:     make(map[uint64]bool),
		Events:       make([]BuffEvent, 0, 100),
		debuffBuffer: make([]byte, 30*config.DEBUFF_SIZE),
		CCWhitelist:  NewCCWhitelist(),
	}
}

func (m *DebuffMonitor) SetReactionHandler(handler ReactionHandler) {
	m.ReactionHandler = handler
}

func MakeKey(id, typeID uint32) uint64 {
	return uint64(id)<<32 | uint64(typeID)
}

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

		if !m.KnownIDs[buffID] {
			m.KnownIDs[buffID] = true

			reacted := false
			if m.Whitelist != nil {
				if ok, name := m.Whitelist.ReactInstant(buffID); ok {
					reacted = true
					buff.Name = name
				}
			}

			if m.ReactionHandler != nil && m.ReactionHandler.IsEnabled() {
				m.ReactionHandler.OnBuffGained(buffID)
			}

			m.AddEvent("+", buffID, buff.Name, reacted)
			if m.OnBuffGained != nil {
				m.OnBuffGained(buff)
			}
		}

		newBuffs = append(newBuffs, buff)
	}

	for id := range m.KnownIDs {
		if !currentIDs[id] {
			delete(m.KnownIDs, id)

			if m.ReactionHandler != nil && m.ReactionHandler.IsEnabled() {
				m.ReactionHandler.OnBuffLost(id)
			}

			m.AddEvent("-", id, "", false)
			if m.OnBuffLost != nil {
				m.OnBuffLost(id)
			}
		}
	}

	m.Buffs = newBuffs
}

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

func (m *DebuffMonitor) Update(playerAddr uint32) {
	if !m.Enabled || playerAddr == 0 {
		return
	}

	m.DebuffBase = m.GetDebuffBase(playerAddr)
	if m.DebuffBase == 0 {
		return
	}

	count := memory.ReadU32(m.handle, m.DebuffBase+uintptr(config.OFF_DEBUFF_COUNT))
	m.RawCount = count

	if count == 0 || count > 50 {
		for key := range m.KnownIDs {
			typeID := uint32(key & 0xFFFFFFFF)
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

		if !m.KnownIDs[key] {
			m.KnownIDs[key] = true

			reacted := false
			ccName := ""
			if m.CCWhitelist != nil {
				if ok, name := m.CCWhitelist.ReactInstant(typeID); ok {
					reacted = true
					ccName = name
					debuff.CCName = name
				}
			}

			if m.ReactionHandler != nil && m.ReactionHandler.IsEnabled() {
				m.ReactionHandler.OnDebuffGained(typeID)
			}

			m.AddEvent("+", id, typeID, ccName, reacted)
			if m.OnDebuffGained != nil {
				m.OnDebuffGained(debuff)
			}
		}

		newDebuffs = append(newDebuffs, debuff)
	}

	for key := range m.KnownIDs {
		if !currentIDs[key] {
			delete(m.KnownIDs, key)
			id := uint32(key >> 32)
			typeID := uint32(key & 0xFFFFFFFF)

			if m.ReactionHandler != nil && m.ReactionHandler.IsEnabled() {
				m.ReactionHandler.OnDebuffLost(typeID)
			}

			m.AddEvent("-", id, typeID, "", false)
			if m.OnDebuffLost != nil {
				m.OnDebuffLost(typeID)
			}
		}
	}

	m.Debuffs = newDebuffs
}

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

func (m *BuffMonitor) HasBuff(buffID uint32) bool {
	for _, b := range m.Buffs {
		if b.ID == buffID {
			return true
		}
	}
	return false
}

func (m *DebuffMonitor) HasDebuff(debuffID uint32) bool {
	for _, d := range m.Debuffs {
		if d.ID == debuffID {
			return true
		}
	}
	return false
}
