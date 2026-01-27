package reaction

import (
	"archefriend/input"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ReactionConfig é a estrutura do JSON
type ReactionConfig struct {
	Type       int    `json:"type"`        // Buff/Debuff ID
	Name       string `json:"name"`        // Nome descritivo
	OnStart    string `json:"onStart"`     // String de teclas quando ganha (ex: "F12", "ALT+E", "ALT+Q & ALT+E")
	OnEnd      string `json:"onEnd"`       // String de teclas quando perde (suporta múltiplas sequências com & ou ,)
	IsDebuff   bool   `json:"isDebuff"`    // true = debuff, false = buff
	CooldownMS int    `json:"cooldownMs"`  // Cooldown em milliseconds
}

// Reaction define uma reação automática a um buff/debuff
type Reaction struct {
	ID          uint32     // Buff/Debuff ID
	Name        string     // Nome descritivo
	OnGain      [][]uint16 // Sequência de combos quando ganha o buff/debuff
	OnLost      [][]uint16 // Sequência de combos quando perde o buff/debuff
	Enabled     bool       // Se está ativo
	IsDebuff    bool       // true = debuff, false = buff
	UseString   string     // String original da tecla onStart (para exibição)
	OnEndString string     // String original da tecla onEnd (para exibição)
	CooldownMS  int        // Cooldown personalizado em milliseconds
	lastTrigger int64      // Unix timestamp do último trigger (para evitar spam)
}

// Manager gerencia todas as reações
type Manager struct {
	reactions map[uint32]*Reaction // Map de ID -> Reaction
	mu        sync.RWMutex
	cooldown  int64 // Cooldown mínimo entre triggers (ms)
	enabled   bool  // Se o sistema de reactions está ativo
}

// NewManager cria um novo reaction manager
func NewManager() *Manager {
	return &Manager{
		reactions: make(map[uint32]*Reaction),
		cooldown:  500, // 500ms cooldown default
		enabled:   true, // Reactions ativas por padrão
	}
}

// AddReaction adiciona ou atualiza uma reação
func (m *Manager) AddReaction(r *Reaction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions[r.ID] = r
}

// RemoveReaction remove uma reação
func (m *Manager) RemoveReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reactions, id)
}

// GetReaction retorna uma reação pelo ID
func (m *Manager) GetReaction(id uint32) (*Reaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.reactions[id]
	return r, ok
}

// EnableReaction ativa uma reação
func (m *Manager) EnableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = true
	}
}

// DisableReaction desativa uma reação
func (m *Manager) DisableReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = false
	}
}

// ToggleReaction liga/desliga uma reação
func (m *Manager) ToggleReaction(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reactions[id]; ok {
		r.Enabled = !r.Enabled
		status := "desativada"
		if r.Enabled {
			status = "ativada"
		}
		fmt.Printf("[REACTION] %s (ID:%d) %s\n", r.Name, r.ID, status)
	}
}

// Enable ativa o sistema de reactions
func (m *Manager) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
	fmt.Println("[REACTION] Sistema de reactions ATIVADO")
}

// Disable desativa o sistema de reactions
func (m *Manager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
	fmt.Println("[REACTION] Sistema de reactions DESATIVADO")
}

// Toggle alterna o estado do sistema de reactions
func (m *Manager) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = !m.enabled
	status := "DESATIVADO"
	if m.enabled {
		status = "ATIVADO"
	}
	fmt.Printf("[REACTION] Sistema de reactions %s\n", status)
	return m.enabled
}

// IsEnabled retorna se o sistema de reactions está ativo
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// OnBuffGained é chamado quando um buff é ganho
func (m *Manager) OnBuffGained(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[buffID]
	m.mu.RUnlock()

	// Se existe uma reaction configurada, sempre printa
	if exists {
		fmt.Printf("[REACTION] ⭐ Buff detectado: %s (ID:%d)\n", reaction.Name, buffID)
	}

	if !enabled || !exists || !reaction.Enabled || reaction.IsDebuff {
		return
	}

	// Verifica se tem ação OnGain
	if len(reaction.OnGain) == 0 {
		return
	}

	fmt.Printf("[REACTION] → Executando ação para buff %s...\n", reaction.Name)

	// Executa a sequência de teclas em goroutine para não bloquear
	go func() {
		if err := input.SendKeySequence(reaction.OnGain); err != nil {
			fmt.Printf("[REACTION] Erro ao executar OnGain: %v\n", err)
		} else {
			fmt.Printf("[REACTION] OnGain executado com sucesso\n")
		}
	}()
}

// OnBuffLost é chamado quando um buff é perdido
func (m *Manager) OnBuffLost(buffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[buffID]
	m.mu.RUnlock()

	// Se existe uma reaction configurada, sempre printa
	if exists {
		fmt.Printf("[REACTION] ⭐ Buff perdido: %s (ID:%d)\n", reaction.Name, buffID)
		fmt.Printf("[DEBUG] Sistema enabled=%v, reaction.Enabled=%v, isDebuff=%v, OnLost=%d combos\n",
			enabled, reaction.Enabled, reaction.IsDebuff, len(reaction.OnLost))
	}

	if !exists {
		return
	}

	if !enabled {
		fmt.Printf("[DEBUG] Sistema de reactions está DESATIVADO (F6 para ativar)\n")
		return
	}

	if !reaction.Enabled {
		fmt.Printf("[DEBUG] Reaction específica para %s está desativada\n", reaction.Name)
		return
	}

	if reaction.IsDebuff {
		fmt.Printf("[WARN] ID:%d está marcado como DEBUFF mas foi chamado como BUFF\n", buffID)
		return
	}

	// Verifica se tem ação OnLost
	if len(reaction.OnLost) == 0 {
		fmt.Printf("[DEBUG] OnEnd vazio para %s (OnEndString='%s')\n", reaction.Name, reaction.OnEndString)
		return
	}

	fmt.Printf("[REACTION] → Executando ação OnEnd para buff %s...\n", reaction.Name)

	go func() {
		if err := input.SendKeySequence(reaction.OnLost); err != nil {
			fmt.Printf("[REACTION] Erro ao executar OnLost: %v\n", err)
		} else {
			fmt.Printf("[REACTION] OnLost executado com sucesso\n")
		}
	}()
}

// OnDebuffGained é chamado quando um debuff é ganho
func (m *Manager) OnDebuffGained(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[debuffID]
	m.mu.RUnlock()

	// Se existe uma reaction configurada, sempre printa
	if exists {
		fmt.Printf("[REACTION] ⭐ Debuff detectado: %s (ID:%d)\n", reaction.Name, debuffID)
	}

	if !exists {
		return
	}

	if !enabled {
		return
	}

	if !reaction.Enabled {
		return
	}

	if !reaction.IsDebuff {
		fmt.Printf("[WARN] ID:%d está marcado como BUFF mas foi chamado como DEBUFF\n", debuffID)
		return
	}

	if len(reaction.OnGain) == 0 {
		return
	}

	fmt.Printf("[REACTION] → Executando ação para debuff %s...\n", reaction.Name)

	go func() {
		if err := input.SendKeySequence(reaction.OnGain); err != nil {
			fmt.Printf("[REACTION] Erro ao executar OnGain: %v\n", err)
		} else {
			fmt.Printf("[REACTION] OnGain executado com sucesso\n")
		}
	}()
}

// OnDebuffLost é chamado quando um debuff é perdido
func (m *Manager) OnDebuffLost(debuffID uint32) {
	m.mu.RLock()
	enabled := m.enabled
	reaction, exists := m.reactions[debuffID]
	m.mu.RUnlock()

	// Se existe uma reaction configurada, sempre printa
	if exists {
		fmt.Printf("[REACTION] ⭐ Debuff perdido: %s (ID:%d)\n", reaction.Name, debuffID)
		fmt.Printf("[DEBUG] Sistema enabled=%v, reaction.Enabled=%v, isDebuff=%v, OnLost=%d combos\n",
			enabled, reaction.Enabled, reaction.IsDebuff, len(reaction.OnLost))
	}

	if !exists {
		return
	}

	if !enabled {
		fmt.Printf("[DEBUG] Sistema de reactions está DESATIVADO (F6 para ativar)\n")
		return
	}

	if !reaction.Enabled {
		fmt.Printf("[DEBUG] Reaction específica para %s está desativada\n", reaction.Name)
		return
	}

	if !reaction.IsDebuff {
		fmt.Printf("[WARN] ID:%d está marcado como BUFF mas foi chamado como DEBUFF\n", debuffID)
		return
	}

	if len(reaction.OnLost) == 0 {
		fmt.Printf("[DEBUG] OnEnd vazio para %s (OnEndString='%s')\n", reaction.Name, reaction.OnEndString)
		return
	}

	fmt.Printf("[REACTION] → Executando ação OnEnd para debuff %s...\n", reaction.Name)

	go func() {
		if err := input.SendKeySequence(reaction.OnLost); err != nil {
			fmt.Printf("[REACTION] Erro ao executar OnLost: %v\n", err)
		} else {
			fmt.Printf("[REACTION] OnLost executado com sucesso\n")
		}
	}()
}

// GetAllReactions retorna todas as reações ordenadas por ID
func (m *Manager) GetAllReactions() []*Reaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	reactions := make([]*Reaction, 0, len(m.reactions))
	for _, r := range m.reactions {
		reactions = append(reactions, r)
	}

	// Ordenar por ID para garantir consistência
	for i := 0; i < len(reactions)-1; i++ {
		for j := i + 1; j < len(reactions); j++ {
			if reactions[i].ID > reactions[j].ID {
				reactions[i], reactions[j] = reactions[j], reactions[i]
			}
		}
	}

	return reactions
}

// GetActiveCount retorna quantas reações estão ativas
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

// Helper functions para criar combos comuns

// MakeCombo cria um combo simples (ex: ALT+E)
func MakeCombo(keys ...uint16) []uint16 {
	return keys
}

// MakeSequence cria uma sequência de combos
func MakeSequence(combos ...[]uint16) [][]uint16 {
	return combos
}

// Exemplos de reações pré-definidas

// NewStunReaction cria uma reação para quando leva stun
func NewStunReaction(stunID uint32) *Reaction {
	return &Reaction{
		ID:       stunID,
		Name:     "Anti-Stun",
		OnGain:   [][]uint16{{input.VK_ALT, input.VK_E}}, // ALT+E quando leva stun
		OnLost:   nil,
		Enabled:  false,
		IsDebuff: true,
	}
}

// NewSleepReaction cria uma reação para quando leva sleep
func NewSleepReaction(sleepID uint32) *Reaction {
	return &Reaction{
		ID:       sleepID,
		Name:     "Anti-Sleep",
		OnGain:   [][]uint16{{input.VK_ALT, input.VK_Q}}, // ALT+Q quando leva sleep
		OnLost:   nil,
		Enabled:  false,
		IsDebuff: true,
	}
}

// NewBuffReaction cria uma reação customizada para buff
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

// NewDebuffReaction cria uma reação customizada para debuff
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

// LoadFromJSON carrega reações de um arquivo JSON
func (m *Manager) LoadFromJSON(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		// Se o arquivo não existe, não é erro - apenas não há reações
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("erro ao ler arquivo %s: %v", filename, err)
	}

	var configs []ReactionConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("erro ao parsear JSON: %v", err)
	}

	for _, cfg := range configs {
		reaction := &Reaction{
			ID:         uint32(cfg.Type),
			Name:       cfg.Name,
			Enabled:    true,
			IsDebuff:   cfg.IsDebuff,
			UseString:  cfg.OnStart,
			OnEndString: cfg.OnEnd,
			CooldownMS: cfg.CooldownMS,
		}

		// Parse OnStart (suporta múltiplas sequências com &)
		if cfg.OnStart != "" {
			sequences, err := input.ParseKeySequence(cfg.OnStart)
			if err != nil {
				fmt.Printf("[WARN] Falha ao parsear OnStart '%s' para %s: %v\n", cfg.OnStart, cfg.Name, err)
			} else {
				reaction.OnGain = sequences
			}
		}

		// Parse OnEnd (suporta múltiplas sequências com &)
		if cfg.OnEnd != "" {
			sequences, err := input.ParseKeySequence(cfg.OnEnd)
			if err != nil {
				fmt.Printf("[WARN] Falha ao parsear OnEnd '%s' para %s: %v\n", cfg.OnEnd, cfg.Name, err)
			} else {
				reaction.OnLost = sequences
				fmt.Printf("[REACTION] OnEnd carregado para %s: '%s' -> %d combos\n", cfg.Name, cfg.OnEnd, len(sequences))
			}
		}

		m.AddReaction(reaction)

		typeStr := "BUFF"
		if cfg.IsDebuff {
			typeStr = "DEBUFF"
		}
		fmt.Printf("[%s] Carregado: %s (ID:%d)\n", typeStr, cfg.Name, cfg.Type)
	}

	return nil
}

// SaveToJSON salva todas as reações em reactions.json
func (m *Manager) SaveToJSON() error {
	filename := "reactions.json"
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := []ReactionConfig{}

	// Ordena por ID para manter consistência
	reactions := make([]*Reaction, 0, len(m.reactions))
	for _, r := range m.reactions {
		reactions = append(reactions, r)
	}

	// Bubble sort por ID
	for i := 0; i < len(reactions)-1; i++ {
		for j := i + 1; j < len(reactions); j++ {
			if reactions[i].ID > reactions[j].ID {
				reactions[i], reactions[j] = reactions[j], reactions[i]
			}
		}
	}

	// Converte para configs (apenas as habilitadas)
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
		return fmt.Errorf("erro ao serializar JSON: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("erro ao escrever arquivo: %v", err)
	}

	fmt.Printf("[REACTION] Salvas %d reações em %s\n", len(configs), filename)
	return nil
}

// ReloadFromJSON recarrega o arquivo JSON
func (m *Manager) ReloadFromJSON() error {
	// Limpa reações atuais
	m.mu.Lock()
	m.reactions = make(map[uint32]*Reaction)
	m.mu.Unlock()

	// Carrega reações
	if err := m.LoadFromJSON("reactions.json"); err != nil {
		fmt.Printf("[WARN] Não foi possível carregar reactions.json: %v\n", err)
	}

	fmt.Println("[REACTION] Configurações recarregadas")
	return nil
}

// AddBuffReaction adiciona uma reação de buff
func (m *Manager) AddBuffReaction(r *Reaction) error {
	// Parse OnStart string (suporta múltiplas sequências com & ou ,)
	if r.UseString != "" {
		sequences, err := input.ParseKeySequence(r.UseString)
		if err != nil {
			return fmt.Errorf("erro ao parsear OnStart '%s': %v", r.UseString, err)
		}
		r.OnGain = sequences
	}

	// Parse OnEnd string (suporta múltiplas sequências com & ou ,)
	if r.OnEndString != "" {
		sequences, err := input.ParseKeySequence(r.OnEndString)
		if err != nil {
			return fmt.Errorf("erro ao parsear OnEnd '%s': %v", r.OnEndString, err)
		}
		r.OnLost = sequences
	}

	r.IsDebuff = false
	r.Enabled = true
	m.AddReaction(r)

	return nil
}

// AddDebuffReaction adiciona uma reação de debuff
func (m *Manager) AddDebuffReaction(r *Reaction) error {
	// Parse OnStart string (suporta múltiplas sequências com & ou ,)
	if r.UseString != "" {
		sequences, err := input.ParseKeySequence(r.UseString)
		if err != nil {
			return fmt.Errorf("erro ao parsear OnStart '%s': %v", r.UseString, err)
		}
		r.OnGain = sequences
	}

	// Parse OnEnd string (suporta múltiplas sequências com & ou ,)
	if r.OnEndString != "" {
		sequences, err := input.ParseKeySequence(r.OnEndString)
		if err != nil {
			return fmt.Errorf("erro ao parsear OnEnd '%s': %v", r.OnEndString, err)
		}
		r.OnLost = sequences
	}

	r.IsDebuff = true
	r.Enabled = true
	m.AddReaction(r)

	return nil
}

// RemoveBuffReaction remove uma reação de buff
func (m *Manager) RemoveBuffReaction(id uint32) {
	m.RemoveReaction(id)
}

// RemoveDebuffReaction remove uma reação de debuff
func (m *Manager) RemoveDebuffReaction(id uint32) {
	m.RemoveReaction(id)
}

