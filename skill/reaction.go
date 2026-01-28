package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SkillReaction representa uma reação a ser executada quando uma skill é usada
type SkillReaction struct {
	SkillID      uint32 `json:"skillId"`
	Name         string `json:"name"`
	OnCast       string `json:"onCast"`       // Teclas a pressionar quando a skill é usada
	Enabled      bool   `json:"enabled"`
	CooldownMS   int    `json:"cooldownMs"`   // Cooldown antes de poder reagir novamente
	UseAimbot    bool   `json:"useAimbot"`    // Usar aimbot antes de executar a reação
	AimbotOnTry  bool   `json:"aimbotOnTry"`  // Executar aimbot na tentativa (antes do cast)

	// Runtime
	lastTriggered time.Time
	parsedKeys    [][]uint16
}

// SkillReactionsConfig representa o arquivo de configuração de reações
type SkillReactionsConfig struct {
	Reactions []SkillReaction `json:"reactions"`
}

// ReactionManager gerencia as reações de skills
type ReactionManager struct {
	reactions map[uint32]*SkillReaction
	enabled   bool
	mu        sync.RWMutex

	// Callback para executar teclas
	ExecuteKeys func(keys [][]uint16) error

	// Callback para aimbot
	AimAtTarget func() bool
}

// NewReactionManager cria um novo gerenciador de reações
func NewReactionManager() *ReactionManager {
	return &ReactionManager{
		reactions: make(map[uint32]*SkillReaction),
		enabled:   true,
	}
}

// LoadFromJSON carrega reações de um arquivo JSON
func (rm *ReactionManager) LoadFromJSON(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("erro ao ler arquivo: %v", err)
	}

	var config SkillReactionsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("erro ao parsear JSON: %v", err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Limpar reações existentes
	rm.reactions = make(map[uint32]*SkillReaction)

	// Adicionar novas reações
	for i := range config.Reactions {
		r := &config.Reactions[i]
		rm.reactions[r.SkillID] = r
		fmt.Printf("[SKILL-REACT] Carregada reação: %s (ID:%d) -> %s\n", r.Name, r.SkillID, r.OnCast)
	}

	fmt.Printf("[SKILL-REACT] %d reações carregadas\n", len(rm.reactions))
	return nil
}

// SaveToJSON salva as reações em um arquivo JSON
func (rm *ReactionManager) SaveToJSON(filepath string) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	config := SkillReactionsConfig{
		Reactions: make([]SkillReaction, 0, len(rm.reactions)),
	}

	for _, r := range rm.reactions {
		config.Reactions = append(config.Reactions, *r)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("erro ao escrever arquivo: %v", err)
	}

	return nil
}

// AddReaction adiciona uma nova reação
func (rm *ReactionManager) AddReaction(r *SkillReaction) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.reactions[r.SkillID] = r
}

// RemoveReaction remove uma reação
func (rm *ReactionManager) RemoveReaction(skillID uint32) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.reactions, skillID)
}

// GetReaction retorna uma reação pelo ID da skill
func (rm *ReactionManager) GetReaction(skillID uint32) *SkillReaction {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.reactions[skillID]
}

// GetAllReactions retorna todas as reações
func (rm *ReactionManager) GetAllReactions() []*SkillReaction {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make([]*SkillReaction, 0, len(rm.reactions))
	for _, r := range rm.reactions {
		result = append(result, r)
	}
	return result
}

// Enable ativa o sistema de reações
func (rm *ReactionManager) Enable() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.enabled = true
	fmt.Println("[SKILL-REACT] Sistema de reações ATIVADO")
}

// Disable desativa o sistema de reações
func (rm *ReactionManager) Disable() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.enabled = false
	fmt.Println("[SKILL-REACT] Sistema de reações DESATIVADO")
}

// Toggle alterna o estado do sistema
func (rm *ReactionManager) Toggle() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.enabled = !rm.enabled
	status := "DESATIVADO"
	if rm.enabled {
		status = "ATIVADO"
	}
	fmt.Printf("[SKILL-REACT] Sistema de reações %s\n", status)
	return rm.enabled
}

// IsEnabled retorna se o sistema está ativo
func (rm *ReactionManager) IsEnabled() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.enabled
}

// OnSkillTry é chamado quando uma skill é tentada (antes de executar)
func (rm *ReactionManager) OnSkillTry(skillID uint32) {
	rm.mu.RLock()
	enabled := rm.enabled
	reaction, exists := rm.reactions[skillID]
	rm.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled {
		return
	}

	// Só executa aimbot se AimbotOnTry estiver ativo
	if !reaction.UseAimbot || !reaction.AimbotOnTry {
		return
	}

	// Verificar cooldown
	if reaction.CooldownMS > 0 && !reaction.lastTriggered.IsZero() {
		elapsed := time.Since(reaction.lastTriggered)
		if elapsed < time.Duration(reaction.CooldownMS)*time.Millisecond {
			return
		}
	}

	fmt.Printf("[SKILL-REACT] Try %s (ID:%d) - Executando aimbot\n", reaction.Name, skillID)

	// Executar aimbot ANTES do cast
	if rm.AimAtTarget != nil {
		if rm.AimAtTarget() {
			fmt.Printf("[SKILL-REACT] Aimbot (on try) para %s\n", reaction.Name)
		}
	}
}

// OnSkillCast é chamado quando uma skill é usada com sucesso
func (rm *ReactionManager) OnSkillCast(skillID uint32) {
	rm.mu.RLock()
	enabled := rm.enabled
	reaction, exists := rm.reactions[skillID]
	rm.mu.RUnlock()

	if !enabled || !exists || !reaction.Enabled {
		return
	}

	// Verificar cooldown
	if reaction.CooldownMS > 0 && !reaction.lastTriggered.IsZero() {
		elapsed := time.Since(reaction.lastTriggered)
		if elapsed < time.Duration(reaction.CooldownMS)*time.Millisecond {
			return
		}
	}

	// Verificar se tem teclas configuradas
	if reaction.OnCast == "" {
		return
	}

	fmt.Printf("[SKILL-REACT] Skill %s (ID:%d) detectada! Executando: %s\n",
		reaction.Name, skillID, reaction.OnCast)

	// Atualizar timestamp
	rm.mu.Lock()
	reaction.lastTriggered = time.Now()
	useAimbot := reaction.UseAimbot
	aimbotOnTry := reaction.AimbotOnTry
	rm.mu.Unlock()

	// Executar aimbot se configurado (e não já executou no try)
	if useAimbot && !aimbotOnTry && rm.AimAtTarget != nil {
		if rm.AimAtTarget() {
			fmt.Printf("[SKILL-REACT] Aimbot (on cast) para %s\n", reaction.Name)
		}
	}

	// Executar teclas via callback
	if rm.ExecuteKeys != nil && reaction.parsedKeys != nil {
		go func() {
			if err := rm.ExecuteKeys(reaction.parsedKeys); err != nil {
				fmt.Printf("[SKILL-REACT] Erro ao executar teclas: %v\n", err)
			}
		}()
	}
}

// SetKeyParser define a função para parsear teclas
func (rm *ReactionManager) SetKeyParser(parser func(string) ([][]uint16, error)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Parsear todas as reações existentes
	for _, r := range rm.reactions {
		if r.OnCast != "" {
			keys, err := parser(r.OnCast)
			if err != nil {
				fmt.Printf("[SKILL-REACT] Erro ao parsear teclas '%s': %v\n", r.OnCast, err)
				continue
			}
			r.parsedKeys = keys
		}
	}
}

// ReloadAndParse recarrega e parseia as reações
func (rm *ReactionManager) ReloadAndParse(filepath string, parser func(string) ([][]uint16, error)) error {
	if err := rm.LoadFromJSON(filepath); err != nil {
		return err
	}
	rm.SetKeyParser(parser)
	return nil
}

// CreateDefaultReactions cria reações padrão de exemplo
func CreateDefaultReactions() *SkillReactionsConfig {
	return &SkillReactionsConfig{
		Reactions: []SkillReaction{
			{
				SkillID:    10005,
				Name:       "Fireball",
				OnCast:     "ALT+Q",
				Enabled:    true,
				CooldownMS: 500,
			},
			{
				SkillID:    10010,
				Name:       "Charge",
				OnCast:     "F1",
				Enabled:    true,
				CooldownMS: 1000,
			},
		},
	}
}

// SaveDefaultReactions salva reações padrão em um arquivo
func SaveDefaultReactions(filepath string) error {
	config := CreateDefaultReactions()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}
