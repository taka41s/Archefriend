package reaction

import (
	"winkit/input"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

type ReactionConfig struct {
	Type       int    `json:"type"`
	Name       string `json:"name"`
	OnStart    string `json:"onStart"`
	OnEnd      string `json:"onEnd"`
	IsDebuff   bool   `json:"isDebuff"`
	CooldownMS int    `json:"cooldownMs"`
	Source     string `json:"source,omitempty"` // "player" (default) or "target"
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
	Source      string // "player" (default) or "target"
	lastTrigger int64
}

type AFKChecker interface {
	IsAFK() bool
	IsEnabled() bool
}

type Manager struct {
	// Sorted slice for O(log n) binary search - cache friendly
	sorted     []*Reaction
	mu         sync.RWMutex
	cooldown   int64
	enabled    bool
	afkChecker AFKChecker
}

func NewManager() *Manager {
	return &Manager{
		sorted:   make([]*Reaction, 0, 32),
		cooldown: 500,
		enabled:  true,
	}
}

// fastSearch finds reaction by ID using the fastest algorithm for the list size
// For small lists (<16): linear search with early exit (better cache/branch prediction)
// For larger lists: binary search O(log n)
func (m *Manager) binarySearch(id uint32) (*Reaction, bool) {
	n := len(m.sorted)
	if n == 0 {
		return nil, false
	}

	// Small list: linear search with early exit (faster due to no branch misprediction)
	if n < 16 {
		for i := 0; i < n; i++ {
			if m.sorted[i].ID == id {
				return m.sorted[i], true
			}
			// Early exit: list is sorted, if we passed the ID it's not here
			if m.sorted[i].ID > id {
				return nil, false
			}
		}
		return nil, false
	}

	// Larger list: binary search
	left, right := 0, n-1
	for left <= right {
		mid := (left + right) >> 1 // bit shift is faster than division
		midID := m.sorted[mid].ID
		if midID == id {
			return m.sorted[mid], true
		}
		if midID < id {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}
	return nil, false
}

// binarySearchIndex finds the index where id should be inserted
func (m *Manager) binarySearchIndex(id uint32) int {
	return sort.Search(len(m.sorted), func(i int) bool {
		return m.sorted[i].ID >= id
	})
}

func (m *Manager) AddReaction(r *Reaction) {
	if r.Source == "" {
		r.Source = "player"
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists with same ID+Source
	for i, existing := range m.sorted {
		if existing.ID == r.ID && existing.Source == r.Source {
			m.sorted[i] = r
			return
		}
	}

	// Find insertion point to maintain sorted order by ID
	idx := m.binarySearchIndex(r.ID)

	// Insert at correct position
	m.sorted = append(m.sorted, nil)
	copy(m.sorted[idx+1:], m.sorted[idx:])
	m.sorted[idx] = r
}

func (m *Manager) RemoveReaction(id uint32) {
	m.RemoveReactionBySource(id, "")
}

func (m *Manager) RemoveReactionBySource(id uint32, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, r := range m.sorted {
		if r.ID == id && (source == "" || r.Source == source) {
			m.sorted = append(m.sorted[:i], m.sorted[i+1:]...)
			return
		}
	}
}

func (m *Manager) GetReaction(id uint32) (*Reaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.binarySearch(id)
}

func (m *Manager) EnableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.binarySearch(id); ok {
		r.Enabled = true
	}
}

func (m *Manager) DisableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.binarySearch(id); ok {
		r.Enabled = false
	}
}

func (m *Manager) ToggleReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.binarySearch(id); ok {
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

func (m *Manager) SetAFKChecker(checker AFKChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.afkChecker = checker
}

func (m *Manager) isAFK() bool {
	if m.afkChecker == nil {
		return false
	}
	if !m.afkChecker.IsEnabled() {
		return false
	}
	return m.afkChecker.IsAFK()
}

// findBySource finds a reaction matching ID and source ("player" or "target")
func (m *Manager) findBySource(id uint32, source string) (*Reaction, bool) {
	for _, r := range m.sorted {
		if r.ID == id && r.Source == source {
			return r, true
		}
	}
	return nil, false
}

func (m *Manager) OnBuffGained(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(buffID, "player")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}

	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}

	if len(reaction.OnGain) == 0 {
		return
	}

	fmt.Printf("[REACTION] BUFF %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, buffID, reaction.UseString)

	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnGain, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		} else {
			fmt.Printf("[REACTION] DONE sending keys for %s\n", reaction.Name)
		}
	}()
}

func (m *Manager) OnBuffLost(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(buffID, "player")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}

	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}

	if len(reaction.OnLost) == 0 {
		return
	}

	fmt.Printf("[REACTION] BUFF LOST %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, buffID, reaction.OnEndString)

	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnLost, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		} else {
			fmt.Printf("[REACTION] DONE sending keys for %s\n", reaction.Name)
		}
	}()
}

// Debug flag
var DebugReaction = false

func ToggleDebugReaction() bool {
	DebugReaction = !DebugReaction
	if DebugReaction {
		fmt.Println("[DEBUG] Reaction debug ENABLED")
	} else {
		fmt.Println("[DEBUG] Reaction debug DISABLED")
	}
	return DebugReaction
}

func (m *Manager) OnDebuffGained(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(debuffID, "player")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if DebugReaction {
		if !exists {
			fmt.Printf("[REACT-DBG] debuffID=%d NOT FOUND in player reactions\n", debuffID)
		} else {
			fmt.Printf("[REACT-DBG] debuffID=%d FOUND: name=%s enabled=%v isDebuff=%v hasOnGain=%v\n",
				debuffID, reaction.Name, reaction.Enabled, reaction.IsDebuff, len(reaction.OnGain) > 0)
		}
	}

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		if DebugReaction && exists {
			fmt.Printf("[REACT-DBG] debuffID=%d SKIPPED: managerEnabled=%v reactionEnabled=%v isDebuff=%v\n",
				debuffID, enabled, reaction.Enabled, reaction.IsDebuff)
		}
		return
	}

	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		if DebugReaction {
			fmt.Printf("[REACT-DBG] debuffID=%d SKIPPED: AFK\n", debuffID)
		}
		return
	}

	if len(reaction.OnGain) == 0 {
		if DebugReaction {
			fmt.Printf("[REACT-DBG] debuffID=%d SKIPPED: no OnGain keys\n", debuffID)
		}
		return
	}

	fmt.Printf("[REACTION] DEBUFF %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, debuffID, reaction.UseString)

	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnGain, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		} else {
			fmt.Printf("[REACTION] DONE sending keys for %s\n", reaction.Name)
		}
	}()
}

func (m *Manager) OnDebuffLost(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(debuffID, "player")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		return
	}

	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}

	if len(reaction.OnLost) == 0 {
		return
	}

	fmt.Printf("[REACTION] DEBUFF LOST %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, debuffID, reaction.OnEndString)

	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnLost, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		} else {
			fmt.Printf("[REACTION] DONE sending keys for %s\n", reaction.Name)
		}
	}()
}

// ============================================================================
// Target buff/debuff reactions
// ============================================================================

func (m *Manager) OnTargetBuffGained(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(buffID, "target")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}
	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}
	if len(reaction.OnGain) == 0 {
		return
	}

	fmt.Printf("[REACTION] TARGET BUFF %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, buffID, reaction.UseString)
	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnGain, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		}
	}()
}

func (m *Manager) OnTargetBuffLost(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(buffID, "target")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}
	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}
	if len(reaction.OnLost) == 0 {
		return
	}

	fmt.Printf("[REACTION] TARGET BUFF LOST %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, buffID, reaction.OnEndString)
	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnLost, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		}
	}()
}

func (m *Manager) OnTargetDebuffGained(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(debuffID, "target")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		return
	}
	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}
	if len(reaction.OnGain) == 0 {
		return
	}

	fmt.Printf("[REACTION] TARGET DEBUFF %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, debuffID, reaction.UseString)
	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnGain, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		}
	}()
}

func (m *Manager) OnTargetDebuffLost(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.findBySource(debuffID, "target")
	afkChecker := m.afkChecker
	m.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled || !reaction.IsDebuff {
		return
	}
	if afkChecker != nil && afkChecker.IsEnabled() && afkChecker.IsAFK() {
		return
	}
	if len(reaction.OnLost) == 0 {
		return
	}

	fmt.Printf("[REACTION] TARGET DEBUFF LOST %s (ID:%d) -> Sending: %s (5x spam)\n", reaction.Name, debuffID, reaction.OnEndString)
	go func() {
		if err := input.SendKeySequenceSpam(reaction.OnLost, 5); err != nil {
			fmt.Printf("[REACTION] ERROR sending keys for %s: %v\n", reaction.Name, err)
		}
	}()
}

func (m *Manager) GetAllReactions() []*Reaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Already sorted, just return a copy
	result := make([]*Reaction, len(m.sorted))
	copy(result, m.sorted)
	return result
}

func (m *Manager) GetActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, r := range m.sorted {
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
		source := cfg.Source
		if source == "" {
			source = "player"
		}
		reaction := &Reaction{
			ID:          uint32(cfg.Type),
			Name:        cfg.Name,
			Enabled:     true,
			IsDebuff:    cfg.IsDebuff,
			UseString:   cfg.OnStart,
			OnEndString: cfg.OnEnd,
			CooldownMS:  cfg.CooldownMS,
			Source:      source,
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

	// Already sorted, no need to sort again
	for _, r := range m.sorted {
		if r.Enabled {
			source := r.Source
			if source == "" {
				source = "player"
			}
			configs = append(configs, ReactionConfig{
				Type:       int(r.ID),
				Name:       r.Name,
				OnStart:    r.UseString,
				OnEnd:      r.OnEndString,
				IsDebuff:   r.IsDebuff,
				CooldownMS: r.CooldownMS,
				Source:     source,
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
	m.sorted = make([]*Reaction, 0, 32)
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
	if r.Source == "" {
		r.Source = "player"
	}
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
	if r.Source == "" {
		r.Source = "player"
	}
	m.AddReaction(r)

	return nil
}

func (m *Manager) RemoveBuffReaction(id uint32) {
	m.RemoveReaction(id)
}

func (m *Manager) RemoveDebuffReaction(id uint32) {
	m.RemoveReaction(id)
}

// TriggerForTest simulates the full buff/debuff lifecycle for testing:
// 1. OnStart (gained) -> sends OnGain keys immediately
// 2. Wait 2 seconds (simulates buff/debuff duration)
// 3. OnEnd (lost) -> sends OnLost keys
// Uses custom keyExecutor (e.g., PostMessage to game window)
// Ignores AFK check since it's only a test
func (m *Manager) TriggerForTest(id uint32, keyExecutor func([][]uint16) error) error {
	m.mu.RLock()
	reaction, exists := m.binarySearch(id)
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("reaction ID %d not found", id)
	}

	if len(reaction.OnGain) == 0 && len(reaction.OnLost) == 0 {
		return fmt.Errorf("reaction '%s' has no OnStart or OnEnd keys configured", reaction.Name)
	}

	rType := "BUFF"
	if reaction.IsDebuff {
		rType = "DEBUFF"
	}

	// Phase 1: OnStart (buff/debuff gained)
	if len(reaction.OnGain) > 0 {
		fmt.Printf("[TEST] Emulating %s START: %s (ID:%d) -> %s\n", rType, reaction.Name, id, reaction.UseString)
		if err := keyExecutor(reaction.OnGain); err != nil {
			return fmt.Errorf("failed to execute OnStart keys: %v", err)
		}
	}

	// Phase 2: OnEnd (buff/debuff lost) - runs after 2s delay in goroutine
	if len(reaction.OnLost) > 0 {
		go func() {
			time.Sleep(2 * time.Second)
			fmt.Printf("[TEST] Emulating %s END: %s (ID:%d) -> %s\n", rType, reaction.Name, id, reaction.OnEndString)
			if err := keyExecutor(reaction.OnLost); err != nil {
				fmt.Printf("[TEST] Error executing OnEnd keys: %v\n", err)
			}
		}()
	}

	return nil
}
