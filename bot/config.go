package bot

import (
	"encoding/json"
	"fmt"
	"os"
)

// FileConfig é a config carregável de arquivo JSON
type FileConfig struct {
	Enabled      bool     `json:"enabled"`
	MobNames     []string `json:"mob_names"`
	MaxRange     float32  `json:"max_range"`
	PartialMatch bool     `json:"partial_match"`
	ScanIntervalMs int    `json:"scan_interval_ms"`
	TargetDelayMs  int    `json:"target_delay_ms"`

	// Keys para ações automáticas
	AttackKey    string `json:"attack_key"`    // Ex: "1", "F", "SPACE"
	LootKey      string `json:"loot_key"`      // Ex: "F", "E"
	AttackDelay  int    `json:"attack_delay"`  // ms entre ataques
	LootDelay    int    `json:"loot_delay"`    // ms para loot após kill
	AutoAttack   bool   `json:"auto_attack"`   // Atacar automaticamente
	AutoLoot     bool   `json:"auto_loot"`     // Lootar automaticamente

	// Potion settings
	HPPotionKey       string  `json:"hp_potion_key"`       // Ex: "5", "H"
	HPPotionThreshold float32 `json:"hp_potion_threshold"` // % HP para usar (ex: 50.0 = 50%)
	HPPotionEnabled   bool    `json:"hp_potion_enabled"`
	MPPotionKey       string  `json:"mp_potion_key"`       // Ex: "6", "M"
	MPPotionThreshold float32 `json:"mp_potion_threshold"` // % Mana para usar
	MPPotionEnabled   bool    `json:"mp_potion_enabled"`
	PotionCooldownMs  int     `json:"potion_cooldown_ms"`  // Cooldown em ms (21000 = 21s)

	// Presets de mob lists (troca rápida via hotkey)
	Presets map[string][]string `json:"presets"`
}

func DefaultFileConfig() FileConfig {
	return FileConfig{
		Enabled:        false,
		MobNames:       []string{},
		MaxRange:       30.0,
		PartialMatch:   false,
		ScanIntervalMs: 20,  // Faster: ~50 scans/sec (matches ESP cache rate)
		TargetDelayMs:  50,  // Faster: reduced from 150ms
		AttackKey:      "1",    // Tecla padrão de ataque
		LootKey:        "F",    // Tecla padrão de loot
		AttackDelay:    500,    // 500ms entre ataques
		LootDelay:      300,    // 300ms para lootar após kill
		AutoAttack:     true,   // Auto-attack ativado por padrão
		AutoLoot:       true,   // Auto-loot ativado por padrão
		// Potion defaults
		HPPotionKey:       "5",     // Tecla padrão HP potion
		HPPotionThreshold: 50.0,    // Usar quando HP < 50%
		HPPotionEnabled:   false,   // Desabilitado por padrão
		MPPotionKey:       "6",     // Tecla padrão MP potion
		MPPotionThreshold: 30.0,    // Usar quando MP < 30%
		MPPotionEnabled:   false,   // Desabilitado por padrão
		PotionCooldownMs:  21000,   // 21 segundos de cooldown
		Presets: map[string][]string{
			"preset1": {"Young Flamingo"},
			"preset2": {"Wandering Imp", "Forest Spider"},
			"preset3": {},
		},
	}
}

// LoadFileConfig carrega config do JSON
func LoadFileConfig(filename string) (*FileConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cfg := DefaultFileConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveFileConfig salva config no JSON
func SaveFileConfig(filename string, cfg *FileConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// SaveDefaultConfig cria o arquivo de config padrão
func SaveDefaultConfig(filename string) error {
	cfg := DefaultFileConfig()
	return SaveFileConfig(filename, &cfg)
}

// ApplyFileConfig aplica FileConfig no Bot em runtime
func (b *Bot) ApplyFileConfig(fc *FileConfig) {
	b.SetMobNames(fc.MobNames)
	b.SetMaxRange(fc.MaxRange)
	b.SetPartialMatch(fc.PartialMatch)
	b.SetAttackKey(fc.AttackKey)
	b.SetLootKey(fc.LootKey)
	b.SetAutoAttack(fc.AutoAttack)
	b.SetAutoLoot(fc.AutoLoot)
	if fc.AttackDelay > 0 {
		b.SetAttackDelay(fc.AttackDelay)
	}
	if fc.LootDelay > 0 {
		b.SetLootDelay(fc.LootDelay)
	}
	// Potion settings
	b.SetHPPotion(fc.HPPotionKey, fc.HPPotionThreshold, fc.HPPotionEnabled)
	b.SetMPPotion(fc.MPPotionKey, fc.MPPotionThreshold, fc.MPPotionEnabled)
	if fc.PotionCooldownMs > 0 {
		b.SetPotionCooldown(fc.PotionCooldownMs)
	}
}

// LoadConfig carrega e aplica config do arquivo
func (b *Bot) LoadConfig(filename string) error {
	fc, err := LoadFileConfig(filename)
	if err != nil {
		return err
	}
	b.ApplyFileConfig(fc)
	fmt.Printf("[BOT] Config loaded from %s\n", filename)
	return nil
}

// LoadPreset carrega um preset de mob names do config
func (b *Bot) LoadPreset(filename string, presetName string) error {
	fc, err := LoadFileConfig(filename)
	if err != nil {
		return err
	}

	names, ok := fc.Presets[presetName]
	if !ok {
		return fmt.Errorf("preset '%s' not found", presetName)
	}

	b.SetMobNames(names)
	fmt.Printf("[BOT] Preset '%s' loaded: %v\n", presetName, names)
	return nil
}