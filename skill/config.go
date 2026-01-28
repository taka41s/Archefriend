package skill

import (
	"encoding/json"
	"fmt"
	"os"
)

// SkillConfig representa a configuração de uma skill
type SkillConfig struct {
	ID         uint32 `json:"id"`
	Name       string `json:"name"`
	CooldownMS int    `json:"cooldownMs"`
	Category   string `json:"category"`
	KeyBind    string `json:"keyBind,omitempty"`
	Track      bool   `json:"track"`
}

// SkillsConfig representa o arquivo de configuração completo
type SkillsConfig struct {
	Skills []SkillConfig `json:"skills"`
}

// LoadSkillsConfig carrega configuração de um arquivo JSON
func LoadSkillsConfig(filepath string) (*SkillsConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler arquivo: %v", err)
	}

	var config SkillsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("erro ao parsear JSON: %v", err)
	}

	return &config, nil
}

// SaveSkillsConfig salva a configuração em um arquivo JSON
func (c *SkillsConfig) Save(filepath string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("erro ao escrever arquivo: %v", err)
	}

	return nil
}

// GetTrackedSkills retorna apenas skills marcadas para tracking
func (c *SkillsConfig) GetTrackedSkills() []SkillConfig {
	tracked := make([]SkillConfig, 0)
	for _, skill := range c.Skills {
		if skill.Track {
			tracked = append(tracked, skill)
		}
	}
	return tracked
}

// GetSkillName retorna o nome de uma skill pelo ID
func (c *SkillsConfig) GetSkillName(skillID uint32) string {
	for _, skill := range c.Skills {
		if skill.ID == skillID {
			return skill.Name
		}
	}
	return fmt.Sprintf("Skill#%d", skillID)
}

// GetSkillByID retorna uma skill pelo ID
func (c *SkillsConfig) GetSkillByID(skillID uint32) *SkillConfig {
	for i := range c.Skills {
		if c.Skills[i].ID == skillID {
			return &c.Skills[i]
		}
	}
	return nil
}

// AddSkill adiciona uma nova skill à configuração
func (c *SkillsConfig) AddSkill(skill SkillConfig) {
	// Verificar se já existe
	for i, s := range c.Skills {
		if s.ID == skill.ID {
			c.Skills[i] = skill
			return
		}
	}
	c.Skills = append(c.Skills, skill)
}

// CreateDefaultConfig cria uma configuração padrão
func CreateDefaultConfig() *SkillsConfig {
	return &SkillsConfig{
		Skills: []SkillConfig{
			{ID: 10005, Name: "Fireball", CooldownMS: 8000, Category: "Magic", Track: true},
			{ID: 10006, Name: "Meteor", CooldownMS: 45000, Category: "Magic", Track: true},
			{ID: 10010, Name: "Charge", CooldownMS: 18000, Category: "Melee", Track: true},
			{ID: 10015, Name: "Heal", CooldownMS: 15000, Category: "Support", Track: true},
		},
	}
}
