package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// KeybindsConfig estrutura do keybinds.json
type KeybindsConfig struct {
	Overlay OverlayKeys `json:"overlay"`
	Cheats  CheatsKeys  `json:"cheats"`
	Info    InfoKeys    `json:"info"`
}

type OverlayKeys struct {
	ToggleVisible string `json:"toggle_visible"`
	ReloadConfigs string `json:"reload_configs"`
}

type CheatsKeys struct {
	LootBypass       string `json:"loot_bypass"`
	DoodadBypass     string `json:"doodad_bypass"`
	InputSpamOnce    string `json:"input_spam_once"`
	InputSpamToggle  string `json:"input_spam_toggle"`
}

type InfoKeys struct {
	Description string `json:"description"`
	Note        string `json:"note"`
}

var CurrentKeybinds *KeybindsConfig

// LoadKeybinds carrega as keybinds do arquivo JSON
func LoadKeybinds(filename string) (*KeybindsConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler keybinds.json: %v", err)
	}

	var kb KeybindsConfig
	if err := json.Unmarshal(data, &kb); err != nil {
		return nil, fmt.Errorf("erro ao parsear keybinds.json: %v", err)
	}

	CurrentKeybinds = &kb
	return &kb, nil
}

// SaveKeybinds salva as keybinds em um arquivo JSON
func SaveKeybinds(filename string, kb *KeybindsConfig) error {
	data, err := json.MarshalIndent(kb, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar keybinds: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("erro ao escrever keybinds.json: %v", err)
	}

	return nil
}
