package reaction

import (
	"archefriend/input"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type ReactionConfig struct {
	Type       int    `json:"type"`
	Name       string `json:"name"`
	OnStart    string `json:"onStart"`
	OnEnd      string `json:"onEnd"`
	IsDebuff   bool   `json:"isDebuff"`
	CooldownMS int    `json:"cooldownMs"`
}

type Reaction struct {
	ID          uint32
	Name        string
	OnGain      [][]uint16
	OnLost      [][]uint16
	Enabled     bool
	IsDebuff    bool
	UseString   string
	OnEndString string
	CooldownMS  int
	lastTrigger int64
}

type Manager struct {
	reactions map[uint32]*Reaction
	mu        sync.RWMutex
	cooldown  int64
	enabled   bool
}

func NewManager() *Manager {
	return &Manager{
		reactions: make(map[uint32]*Reaction),
		cooldown:  500,
		enabled:   true,
	}
}

func (m *Manager) AddReaction(r *Reaction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions[r.ID] = r
}

func (m *Manager) RemoveReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reactions, id)
}

func (m *Manager) GetReaction(id uint32) (*Reaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.reactions[id]
	return r, ok
}

func (m *Manager) EnableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = true
	}
}

func (m *Manager) DisableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = false
	}
}

func (m *Manager) ToggleReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = !r.Enabled
	}
}

func (m *Manager) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
}

func (m *Manager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
}

func (m *Manager) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = !m.enabled
	return m.enabled
}

func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *Manager) OnBuffGained(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[buffID]
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}

	if len(reaction.OnGain) == 0 {
		return
	}

	go func() {
		input.SendKeySequence(reaction.OnGain)
	}()
}

func (m *Manager) OnBuffLost(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[buffID]
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}

	if len(reaction.OnLost) == 0 {
		return
	}

	go func() {
		input.SendKeySequence(reaction.OnLost)
	}()
}

func (m *Manager) OnDebuffGained(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[debuffID]
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		return
	}

	if len(reaction.OnGain) == 0 {
		return
	}

	go func() {
		input.SendKeySequence(reaction.OnGain)
	}()
}

func (m *Manager) OnDebuffLost(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[debuffID]
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		return
	}

	if len(reaction.OnLost) == 0 {
		return
	}

	go func() {
		input.SendKeySequence(reaction.OnLost)
	}()
}

func (m *Manager) GetAllReactions() []*Reaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	reactions := make([]*Reaction, 0, len(m.reactions))
	for _, r := range m.reactions {
		reactions = append(reactions, r)
	}

	for i := 0; i < len(reactions)-1; i++ {
		for j := i + 1; j < len(reactions); j++ {
			if reactions[i].ID > reactions[j].ID {
				reactions[i], reactions[j] = reactions[j], reactions[i]
			}
		}
	}

	return reactions
}

func (m *Manager) GetActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, r := range m.reactions {
		if r.Enabled {
			count++
		}
	}
	return count
}

func MakeCombo(keys ...uint16) []uint16 {
	return keys
}

func MakeSequence(combos ...[]uint16) [][]uint16 {
	return combos
}

func NewStunReaction(stunID uint32) *Reaction {
	return &Reaction{
		ID:       stunID,
		Name:     "Anti-Stun",
		OnGain:   [][]uint16{{input.VK_ALT, input.VK_E}},
		OnLost:   nil,
		Enabled:  false,
		IsDebuff: true,
	}
}

func NewSleepReaction(sleepID uint32) *Reaction {
	return &Reaction{
		ID:       sleepID,
		Name:     "Anti-Sleep",
		OnGain:   [][]uint16{{input.VK_ALT, input.VK_Q}},
		OnLost:   nil,
		Enabled:  false,
		IsDebuff: true,
	}
}

func NewBuffReaction(buffID uint32, name string, onGain, onLost [][]uint16) *Reaction {
	return &Reaction{
		ID:       buffID,
		Name:     name,
		OnGain:   onGain,
		OnLost:   onLost,
		Enabled:  false,
		IsDebuff: false,
	}
}

func NewDebuffReaction(debuffID uint32, name string, onGain, onLost [][]uint16) *Reaction {
	return &Reaction{
		ID:       debuffID,
		Name:     name,
		OnGain:   onGain,
		OnLost:   onLost,
		Enabled:  false,
		IsDebuff: true,
	}
}

func (m *Manager) LoadFromJSON(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read file %s: %v", filename, err)
	}

	var configs []ReactionConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	for _, cfg := range configs {
		reaction := &Reaction{
			ID:          uint32(cfg.Type),
			Name:        cfg.Name,
			Enabled:     true,
			IsDebuff:    cfg.IsDebuff,
			UseString:   cfg.OnStart,
			OnEndString: cfg.OnEnd,
			CooldownMS:  cfg.CooldownMS,
		}

		if cfg.OnStart != "" {
			sequences, err := input.ParseKeySequence(cfg.OnStart)
			if err == nil {
				reaction.OnGain = sequences
			}
		}

		if cfg.OnEnd != "" {
			sequences, err := input.ParseKeySequence(cfg.OnEnd)
			if err == nil {
				reaction.OnLost = sequences
			}
		}

		m.AddReaction(reaction)
	}

	return nil
}

func (m *Manager) SaveToJSON() error {
	filename := "reactions.json"
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := []ReactionConfig{}

	reactions := make([]*Reaction, 0, len(m.reactions))
	for _, r := range m.reactions {
		reactions = append(reactions, r)
	}

	for i := 0; i < len(reactions)-1; i++ {
		for j := i + 1; j < len(reactions); j++ {
			if reactions[i].ID > reactions[j].ID {
				reactions[i], reactions[j] = reactions[j], reactions[i]
			}
		}
	}

	for _, r := range reactions {
		if r.Enabled {
			configs = append(configs, ReactionConfig{
				Type:       int(r.ID),
				Name:       r.Name,
				OnStart:    r.UseString,
				OnEnd:      r.OnEndString,
				IsDebuff:   r.IsDebuff,
				CooldownMS: r.CooldownMS,
			})
		}
	}

	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize JSON: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	return nil
}

func (m *Manager) ReloadFromJSON() error {
	m.mu.Lock()
	m.reactions = make(map[uint32]*Reaction)
	m.mu.Unlock()

	m.LoadFromJSON("reactions.json")
	return nil
}

func (m *Manager) AddBuffReaction(r *Reaction) error {
	if r.UseString != "" {
		sequences, err := input.ParseKeySequence(r.UseString)
		if err != nil {
			return fmt.Errorf("failed to parse OnStart '%s': %v", r.UseString, err)
		}
		r.OnGain = sequences
	}

	if r.OnEndString != "" {
		sequences, err := input.ParseKeySequence(r.OnEndString)
		if err != nil {
			return fmt.Errorf("failed to parse OnEnd '%s': %v", r.OnEndString, err)
		}
		r.OnLost = sequences
	}

	r.IsDebuff = false
	r.Enabled = true
	m.AddReaction(r)

	return nil
}

func (m *Manager) AddDebuffReaction(r *Reaction) error {
	if r.UseString != "" {
		sequences, err := input.ParseKeySequence(r.UseString)
		if err != nil {
			return fmt.Errorf("failed to parse OnStart '%s': %v", r.UseString, err)
		}
		r.OnGain = sequences
	}

	if r.OnEndString != "" {
		sequences, err := input.ParseKeySequence(r.OnEndString)
		if err != nil {
			return fmt.Errorf("failed to parse OnEnd '%s': %v", r.OnEndString, err)
		}
		r.OnLost = sequences
	}

	r.IsDebuff = true
	r.Enabled = true
	m.AddReaction(r)

	return nil
}

func (m *Manager) RemoveBuffReaction(id uint32) {
	m.RemoveReaction(id)
}

func (m *Manager) RemoveDebuffReaction(id uint32) {
	m.RemoveReaction(id)
}
