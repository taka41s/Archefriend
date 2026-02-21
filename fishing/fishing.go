package fishing

import (
	"archefriend/input"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// FishingAction maps a target buff ID to a keybind
type FishingAction struct {
	BuffID  uint32 `json:"buffId"`
	Name    string `json:"name"`
	Keybind string `json:"keybind"`
}

// Config holds the fishing bot configuration
type Config struct {
	Actions []FishingAction `json:"actions"`
}

// Bot is the fishing bot that reacts to target buff events
type Bot struct {
	mu       sync.RWMutex
	running  bool
	config   Config
	actions  map[uint32]*FishingAction // buffID -> action (for fast lookup)
	sendKey func(keyStr string)
	stats   Stats
}

type Stats struct {
	ReactionsTriggered int
}

func DefaultConfig() Config {
	return Config{
		Actions: []FishingAction{
			{BuffID: 5264, Name: "RIGHT", Keybind: "D"},
			{BuffID: 5508, Name: "BIG REEL IN", Keybind: "SPACE"},
			{BuffID: 5267, Name: "ESCAPE", Keybind: "S"},
			{BuffID: 5265, Name: "LEFT", Keybind: "A"},
			{BuffID: 5266, Name: "SMALL REEL IN", Keybind: "W"},
		},
	}
}

func New(sendKey func(keyStr string)) *Bot {
	return &Bot{
		actions: make(map[uint32]*FishingAction),
		sendKey: sendKey,
	}
}

func (b *Bot) LoadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			b.config = DefaultConfig()
			b.rebuildIndex()
			return b.SaveConfig(filename)
		}
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	b.mu.Lock()
	b.config = cfg
	b.mu.Unlock()
	b.rebuildIndex()
	return nil
}

func (b *Bot) SaveConfig(filename string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	data, err := json.MarshalIndent(b.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func (b *Bot) rebuildIndex() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.actions = make(map[uint32]*FishingAction, len(b.config.Actions))
	for i := range b.config.Actions {
		b.actions[b.config.Actions[i].BuffID] = &b.config.Actions[i]
	}
}

func (b *Bot) SetConfig(cfg Config) {
	b.mu.Lock()
	b.config = cfg
	b.mu.Unlock()
	b.rebuildIndex()
}

func (b *Bot) GetConfig() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

func (b *Bot) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running = true
	b.stats = Stats{}
	fmt.Println("[FISHING] Bot started")
}

func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running = false
	fmt.Printf("[FISHING] Bot stopped | Reactions: %d\n", b.stats.ReactionsTriggered)
}

func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

func (b *Bot) Toggle() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running = !b.running
	if b.running {
		b.stats = Stats{}
		fmt.Println("[FISHING] Bot started")
	} else {
		fmt.Printf("[FISHING] Bot stopped | Reactions: %d\n", b.stats.ReactionsTriggered)
	}
	return b.running
}

func (b *Bot) GetStats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stats
}

// OnTargetBuffGained is called when a target buff is detected.
// If the buff ID matches a fishing action, the corresponding key is sent.
func (b *Bot) OnTargetBuffGained(buffID uint32) {
	b.mu.RLock()
	running := b.running
	action, exists := b.actions[buffID]
	b.mu.RUnlock()

	if !running || !exists {
		return
	}

	if action.Keybind == "" {
		return
	}

	fmt.Printf("[FISHING] %s (buff:%d) -> %s\n", action.Name, buffID, action.Keybind)

	b.mu.Lock()
	b.stats.ReactionsTriggered++
	b.mu.Unlock()

	if b.sendKey != nil {
		// Spam the key 3x to ensure it registers
		go func() {
			for i := 0; i < 3; i++ {
				b.sendKey(action.Keybind)
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
}

// UpdateAction updates or adds a fishing action
func (b *Bot) UpdateAction(buffID uint32, name, keybind string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.config.Actions {
		if b.config.Actions[i].BuffID == buffID {
			b.config.Actions[i].Name = name
			b.config.Actions[i].Keybind = keybind
			b.actions[buffID] = &b.config.Actions[i]
			return
		}
	}

	// New action
	b.config.Actions = append(b.config.Actions, FishingAction{
		BuffID:  buffID,
		Name:    name,
		Keybind: keybind,
	})
	b.actions[buffID] = &b.config.Actions[len(b.config.Actions)-1]
}

// SetSendKey sets the key sender function
func (b *Bot) SetSendKey(fn func(keyStr string)) {
	b.sendKey = fn
}

// GetKeybindForBuff returns the keybind string for a given buff ID (for display purposes)
func (b *Bot) GetKeybindForBuff(buffID uint32) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if action, ok := b.actions[buffID]; ok {
		return action.Keybind
	}
	return ""
}

// ParseAndSendKey parses and sends a key string using SendInput
func ParseAndSendKey(keyStr string) {
	keys, err := input.ParseKeyString(keyStr)
	if err != nil {
		fmt.Printf("[FISHING] Invalid key: %s - %v\n", keyStr, err)
		return
	}
	if err := input.SendKeyCombo(keys); err != nil {
		fmt.Printf("[FISHING] SendKey failed: %v\n", err)
	}
}
