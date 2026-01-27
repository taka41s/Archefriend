package buff

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// PresetBuff representa um buff individual em um preset
type PresetBuff struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Permanent bool   `json:"permanent"`
	Hidden    bool   `json:"hidden"` // Se deve injetar como hidden
	Stack     uint32 `json:"stack"`  // Stack count (0 = não modifica)
}

// BuffPreset representa um preset de buffs
type BuffPreset struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Buffs       []PresetBuff `json:"buffs"`
	Enabled     bool         `json:"-"` // Não salvar no JSON
}

// PresetManager gerencia presets de buffs
type PresetManager struct {
	presets           map[string]*BuffPreset
	mu                sync.RWMutex
	injector          *Injector
	quickActionPreset string // Nome do preset para quick action
	quickActionActive bool   // Se o quick action está ativo
}

// NewPresetManager cria um novo preset manager
func NewPresetManager(injector *Injector) *PresetManager {
	return &PresetManager{
		presets:  make(map[string]*BuffPreset),
		injector: injector,
	}
}

// AddPreset adiciona um preset
func (pm *PresetManager) AddPreset(preset *BuffPreset) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.presets[preset.Name] = preset
}

// RemovePreset remove um preset
func (pm *PresetManager) RemovePreset(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.presets, name)
}

// GetPreset retorna um preset pelo nome
func (pm *PresetManager) GetPreset(name string) (*BuffPreset, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	preset, ok := pm.presets[name]
	return preset, ok
}

// GetAllPresets retorna todos os presets
func (pm *PresetManager) GetAllPresets() []*BuffPreset {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	presets := make([]*BuffPreset, 0, len(pm.presets))
	for _, p := range pm.presets {
		presets = append(presets, p)
	}
	return presets
}

// ApplyPreset aplica um preset, injetando todos os buffs
func (pm *PresetManager) ApplyPreset(name string) (int, error) {
	preset, ok := pm.GetPreset(name)
	if !ok {
		return 0, fmt.Errorf("preset not found: %s", name)
	}

	if pm.injector.GetBuffListAddr() == 0 {
		return 0, fmt.Errorf("buff list address not set")
	}

	injected := 0
	for _, buff := range preset.Buffs {
		// Verificar se já existe
		if pm.injector.BuffExists(buff.ID) {
			fmt.Printf("[PRESET] Buff %d (%s) already exists, skipping\n", buff.ID, buff.Name)
			continue
		}

		var success bool
		if buff.Hidden {
			success = pm.injector.InjectFirstAsHidden(buff.ID, buff.Permanent)
		} else {
			success = pm.injector.CloneFirstAndInject(buff.ID, buff.Permanent)
		}

		if success {
			injected++

			// Aplicar stack se especificado
			if buff.Stack > 0 {
				idx := pm.injector.FindBuffByID(buff.ID)
				if idx >= 0 {
					pm.injector.SetBuffStack(idx, buff.Stack)
				}
			}

			fmt.Printf("[PRESET] Injected %s (ID:%d, permanent:%v, hidden:%v)\n",
				buff.Name, buff.ID, buff.Permanent, buff.Hidden)
		} else {
			fmt.Printf("[PRESET] Failed to inject %s (ID:%d)\n", buff.Name, buff.ID)
		}
	}

	fmt.Printf("[PRESET] Applied '%s': %d/%d buffs injected\n", name, injected, len(preset.Buffs))
	return injected, nil
}

// RemovePresetBuffs remove todos os buffs de um preset
func (pm *PresetManager) RemovePresetBuffs(name string) (int, error) {
	preset, ok := pm.GetPreset(name)
	if !ok {
		return 0, fmt.Errorf("preset not found: %s", name)
	}

	removed := 0
	for _, buff := range preset.Buffs {
		if pm.injector.RemoveBuffExtended(buff.ID) {
			removed++
			fmt.Printf("[PRESET] Removed %s (ID:%d)\n", buff.Name, buff.ID)
		}
	}

	fmt.Printf("[PRESET] Removed %d/%d buffs from '%s'\n", removed, len(preset.Buffs), name)
	return removed, nil
}

// LoadFromJSON carrega presets de um arquivo JSON
func (pm *PresetManager) LoadFromJSON(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("erro ao ler arquivo: %v", err)
	}

	var presets []*BuffPreset
	if err := json.Unmarshal(data, &presets); err != nil {
		return fmt.Errorf("erro ao parsear JSON: %v", err)
	}

	pm.mu.Lock()
	pm.presets = make(map[string]*BuffPreset)
	for _, preset := range presets {
		pm.presets[preset.Name] = preset
	}
	pm.mu.Unlock()

	fmt.Printf("[PRESET] Loaded %d presets from %s\n", len(presets), filename)
	return nil
}

// SaveToJSON salva presets em um arquivo JSON
func (pm *PresetManager) SaveToJSON(filename string) error {
	pm.mu.RLock()
	presets := make([]*BuffPreset, 0, len(pm.presets))
	for _, p := range pm.presets {
		presets = append(presets, p)
	}
	pm.mu.RUnlock()

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar JSON: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("erro ao escrever arquivo: %v", err)
	}

	fmt.Printf("[PRESET] Saved %d presets to %s\n", len(presets), filename)
	return nil
}

// CreateDefaultPresets cria alguns presets padrão como exemplo
func (pm *PresetManager) CreateDefaultPresets() {
	// PvP Defensive
	pm.AddPreset(&BuffPreset{
		Name:        "PvP Defensive",
		Description: "Buffs defensivos para PvP",
		Buffs: []PresetBuff{
			{ID: 10001, Name: "God Mode", Permanent: true, Hidden: false, Stack: 0},
			{ID: 10002, Name: "Stun Immunity", Permanent: true, Hidden: false, Stack: 0},
			{ID: 10003, Name: "Fear Immunity", Permanent: true, Hidden: false, Stack: 0},
		},
	})

	// PvP Offensive
	pm.AddPreset(&BuffPreset{
		Name:        "PvP Offensive",
		Description: "Buffs ofensivos para PvP",
		Buffs: []PresetBuff{
			{ID: 20001, Name: "Attack Speed", Permanent: true, Hidden: false, Stack: 10},
			{ID: 20002, Name: "Critical Rate", Permanent: true, Hidden: false, Stack: 10},
			{ID: 20003, Name: "Damage Boost", Permanent: true, Hidden: false, Stack: 10},
		},
	})

	// Farming
	pm.AddPreset(&BuffPreset{
		Name:        "Farming",
		Description: "Buffs para farm eficiente",
		Buffs: []PresetBuff{
			{ID: 30001, Name: "Movement Speed", Permanent: true, Hidden: false, Stack: 0},
			{ID: 30002, Name: "Loot Bonus", Permanent: true, Hidden: false, Stack: 0},
			{ID: 30003, Name: "EXP Boost", Permanent: true, Hidden: false, Stack: 0},
		},
	})

	// Stealth Pack (Hidden buffs)
	pm.AddPreset(&BuffPreset{
		Name:        "Stealth Pack",
		Description: "Buffs invisíveis (não aparecem na UI)",
		Buffs: []PresetBuff{
			{ID: 40001, Name: "Hidden Defense", Permanent: true, Hidden: true, Stack: 0},
			{ID: 40002, Name: "Hidden Regen", Permanent: true, Hidden: true, Stack: 0},
			{ID: 40003, Name: "Hidden Speed", Permanent: true, Hidden: true, Stack: 0},
		},
	})

	fmt.Println("[PRESET] Created default presets")
}

// SetQuickActionPreset define o preset para quick action
func (pm *PresetManager) SetQuickActionPreset(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, ok := pm.presets[name]; !ok {
		return fmt.Errorf("preset not found: %s", name)
	}

	pm.quickActionPreset = name
	pm.quickActionActive = false
	return nil
}

// GetQuickActionPreset retorna o nome do preset de quick action
func (pm *PresetManager) GetQuickActionPreset() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.quickActionPreset
}

// ToggleQuickAction liga/desliga o preset de quick action
func (pm *PresetManager) ToggleQuickAction() error {
	pm.mu.Lock()
	quickPreset := pm.quickActionPreset
	wasActive := pm.quickActionActive
	pm.mu.Unlock()

	if quickPreset == "" {
		return fmt.Errorf("no quick action preset set")
	}

	if wasActive {
		// Desativar: remover buffs
		_, err := pm.RemovePresetBuffs(quickPreset)
		if err != nil {
			return err
		}
		pm.mu.Lock()
		pm.quickActionActive = false
		pm.mu.Unlock()
	} else {
		// Ativar: aplicar buffs
		_, err := pm.ApplyPreset(quickPreset)
		if err != nil {
			return err
		}
		pm.mu.Lock()
		pm.quickActionActive = true
		pm.mu.Unlock()
	}

	return nil
}

// IsQuickActionActive retorna se o quick action está ativo
func (pm *PresetManager) IsQuickActionActive() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.quickActionActive
}

// CreatePresetFromCurrent cria um preset a partir dos buffs atualmente injetados
func (pm *PresetManager) CreatePresetFromCurrent(name, description string) error {
	if pm.injector.GetBuffListAddr() == 0 {
		return fmt.Errorf("buff list address not set")
	}

	// Get all currently injected buffs (max 100 slots)
	buffs := pm.injector.GetAllBuffs(100)
	if len(buffs) == 0 {
		return fmt.Errorf("no buffs currently injected")
	}

	presetBuffs := make([]PresetBuff, 0, len(buffs))
	for _, b := range buffs {
		presetBuffs = append(presetBuffs, PresetBuff{
			ID:        b.ID,
			Name:      fmt.Sprintf("Buff_%d", b.ID),
			Permanent: true,  // Assume permanent by default
			Hidden:    b.IsHidden,
			Stack:     b.Stack,
		})
	}

	preset := &BuffPreset{
		Name:        name,
		Description: description,
		Buffs:       presetBuffs,
	}

	pm.AddPreset(preset)
	return nil
}
