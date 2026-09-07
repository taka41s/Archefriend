package bot

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ====================
// Constants (GetCurrentTargetId — pure memory read, no injection needed)
// ====================

const (
	PTR_ENEMY_TARGET_BASE uintptr = 0x19EBF4
	OFF_TARGET_ID         uintptr = 0x08
)

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procReadProcessMem = kernel32.NewProc("ReadProcessMemory")
)

// ====================
// Bot State
// ====================

type BotState int

const (
	StateIdle      BotState = iota
	StateTargeting
	StateCombat
	StateLooting
)

func (s BotState) String() string {
	switch s {
	case StateIdle:
		return "IDLE"
	case StateTargeting:
		return "TARGETING"
	case StateCombat:
		return "COMBAT"
	case StateLooting:
		return "LOOTING"
	default:
		return "UNKNOWN"
	}
}

// ====================
// EntityProvider interface
// ====================

// EntityInfo - mesma shape do esp.EntityInfo pra não criar import circular.
// O adapter no game/ converte esp.EntityInfo -> bot.EntityInfo.
type EntityInfo struct {
	Address  uint32
	EntityID uint32
	Name     string
	PosX     float32
	PosY     float32
	PosZ     float32
	HP       uint32
	MaxHP    uint32
	Distance float32
	IsPlayer bool
	IsNPC    bool
	IsMate   bool
}

// EntityProvider fornece entidades pro bot.
// Implementado via adapter que wrapa AllEntitiesManager.GetCachedEntities().
type EntityProvider interface {
	GetEntities() []EntityInfo
}

// RangeProvider fornece range dinâmica (sincroniza com ESP overlay).
type RangeProvider interface {
	GetMaxRange() float32
}

// ESPAdapter implementa EntityProvider usando uma função customizada.
// Permite conectar o bot a qualquer fonte de entidades (ex: ESP manager).
type ESPAdapter struct {
	GetEntitiesFn func() []EntityInfo
	GetRangeFn    func() float32 // Optional: dynamic range from ESP
}

func (a *ESPAdapter) GetEntities() []EntityInfo {
	if a.GetEntitiesFn == nil {
		return nil
	}
	return a.GetEntitiesFn()
}

func (a *ESPAdapter) GetMaxRange() float32 {
	if a.GetRangeFn == nil {
		return 0 // 0 = use config range
	}
	return a.GetRangeFn()
}

// ====================
// Config
// ====================

type Config struct {
	MobNames     []string      // nomes de mobs alvo
	MaxRange     float32       // distância máxima (metros)
	ScanInterval time.Duration // intervalo entre scans
	TargetDelay  time.Duration // delay após setar target
	PartialMatch bool          // contains vs exact match

	// Auto-combat settings
	AttackKey    string        // tecla de ataque (ex: "1", "F")
	LootKey      string        // tecla de loot (ex: "F", "E")
	AttackDelay  time.Duration // delay entre ataques
	LootDelay    time.Duration // delay para loot após kill
	AutoAttack   bool          // atacar automaticamente
	AutoLoot     bool          // lootar automaticamente
	LootViaPacket bool         // usa loot.RequestSender (pacote direto) em vez de keyspam

	// Potion settings
	HPPotionKey       string        // tecla HP potion
	HPPotionThreshold float32       // % HP para usar
	HPPotionEnabled   bool          // usar HP potion automaticamente
	MPPotionKey       string        // tecla MP potion
	MPPotionThreshold float32       // % MP para usar
	MPPotionEnabled   bool          // usar MP potion automaticamente
	PotionCooldown    time.Duration // cooldown entre potions (21s)

	// Callbacks (opcionais)
	OnTargetAcquired func(target EntityInfo)
	OnTargetDead     func(target EntityInfo)
	OnCombatTick     func(target EntityInfo)

	// Key sender (injetado pelo main)
	SendKey func(key string)

	// LootAll chama LootMgr_RequestLoot + LootMgr_TakeItem por item, direto
	// (injetado pelo main via loot.RequestSender.LootAll), usado quando
	// LootViaPacket=true em vez de keyspam. Retorna quantos itens pegou.
	LootAll func(targetId uint32) (int, error)

	// SetTargetFn chama X2::GameClient::SetTarget(unitId). Injetado pelo main:
	// tenta primeiro via Tick-hook (loot.RequestSender.SetTarget, roda na
	// thread principal do jogo) e cai para o CreateRemoteThread antigo
	// (target.SetTarget) só se o hook de Tick estiver ocupado por outra
	// feature (ex: House ESP CS222) — ver main.go.
	SetTargetFn func(unitId uint32) error

	// Player stats provider (injetado pelo main)
	GetPlayerHP func() (current, max uint32) // retorna HP atual e máximo
	GetPlayerMP func() (current, max uint32) // retorna MP atual e máximo

	// Exclusão por buff: mobs que possuem ExcludeBuffID são removidos da fila
	// (nunca alvejados enquanto tiverem o buff). Ex: buff 851 = mob
	// protegido/reivindicado. A verificação de proximidade continua sendo a
	// prioridade primária entre os mobs restantes (mais perto primeiro).
	ExcludeBuffID      uint32        // buff que exclui o mob da fila (0 = desativado)
	ExcludeBuffEnabled bool          // liga/desliga a exclusão por buff
	QueueReorgInterval time.Duration // cadência da reorganização da fila (ex: 50ms)

	// BuffBlacklistTTL: quando o alvo é largado por ter o buff de exclusão
	// (detectado APÓS selecionar, já que muitos clientes só sincronizam os
	// buffs do alvo selecionado), o EntityID entra numa blacklist por esse
	// tempo pra não ser re-selecionado em loop. 0 = usa 15s.
	BuffBlacklistTTL time.Duration

	// HasBuff verifica se a entidade (pelo Address do struct) possui o buff dado.
	// Injetado pelo main: lê a cadeia entityAddr+0x38 -> +0x1898 -> buff array.
	// Nil desativa a exclusão por buff (degrada graciosamente).
	HasBuff func(entityAddr uint32, buffID uint32) bool
}

func DefaultConfig() Config {
	return Config{
		MobNames:     []string{},
		MaxRange:     30.0,
		ScanInterval: 20 * time.Millisecond,  // Fast: ~50 scans/sec
		TargetDelay:  50 * time.Millisecond,  // Fast: quick target confirm
		PartialMatch: false,
		AttackKey:    "1",
		LootKey:      "F",
		AttackDelay:  500 * time.Millisecond,
		LootDelay:    300 * time.Millisecond,
		AutoAttack:   true,
		AutoLoot:     true,
		LootViaPacket: false,
		// Potion defaults
		HPPotionKey:       "5",
		HPPotionThreshold: 50.0,
		HPPotionEnabled:   false,
		MPPotionKey:       "6",
		MPPotionThreshold: 30.0,
		MPPotionEnabled:   false,
		PotionCooldown:    21 * time.Second,
		// Exclusão por buff (default: 851 ligado, reorg da fila a 50ms)
		ExcludeBuffID:      851,
		ExcludeBuffEnabled: true,
		QueueReorgInterval: 50 * time.Millisecond,
		BuffBlacklistTTL:   15 * time.Second,
	}
}

// ====================
// Stats
// ====================

type Stats struct {
	MobsKilled   int
	TargetsSet   int
	StartTime    time.Time
	LastTargetAt time.Time
}

// ====================
// Bot
// ====================

type Bot struct {
	handle   windows.Handle
	x2game   uintptr
	config   Config
	state    BotState
	mu       sync.RWMutex
	running  bool
	provider EntityProvider

	currentTarget   *EntityInfo
	killQueue       map[uint32]EntityInfo // Dados dos mobs (lookup rápido)
	killQueueOrder  []uint32              // Ordem FIFO (primeiro a entrar, primeiro a sair)
	buffBlacklist   map[uint32]time.Time  // EntityID -> expiry (mobs largados por buff de exclusão)
	targetAttempts  map[uint32]int        // EntityID -> falhas consecutivas de seleção (anti-loop)
	stats           Stats
	stopChan        chan struct{}
	lastAttackTime  time.Time
	lastLootTime    time.Time
	lastReorgTime   time.Time // última reorganização da fila (throttle QueueReorgInterval)

	// Potion cooldown tracking
	lastHPPotionTime time.Time
	lastMPPotionTime time.Time
}

// getEffectiveRange returns the bot's configured range.
// Always uses bot config, not ESP range (they are independent).
func (b *Bot) getEffectiveRange() float32 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config.MaxRange
}

func New(handle windows.Handle, x2game uintptr, provider EntityProvider, cfg Config) *Bot {
	return &Bot{
		handle:         handle,
		x2game:         x2game,
		config:         cfg,
		state:          StateIdle,
		provider:       provider,
		killQueue:      make(map[uint32]EntityInfo),
		killQueueOrder: make([]uint32, 0),
		buffBlacklist:  make(map[uint32]time.Time),
		targetAttempts: make(map[uint32]int),
		stopChan:       make(chan struct{}),
	}
}

// ====================
// Control
// ====================

func (b *Bot) Start() {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.stats.StartTime = time.Now()
	// Recria o canal para cada nova execução
	b.stopChan = make(chan struct{})
	// Limpa a kill queue ao reiniciar
	b.killQueue = make(map[uint32]EntityInfo)
	b.killQueueOrder = make([]uint32, 0)
	b.buffBlacklist = make(map[uint32]time.Time)
	b.targetAttempts = make(map[uint32]int)
	b.currentTarget = nil
	b.mu.Unlock()

	go b.loop()
	fmt.Println("[BOT] Started")
	fmt.Printf("[BOT] Mobs: %v | Range: %.0fm | Match: %s\n",
		b.config.MobNames, b.config.MaxRange, matchMode(b.config.PartialMatch))
}

func (b *Bot) Stop() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.running = false
	stopChan := b.stopChan
	b.mu.Unlock()

	// Fecha o canal fora do lock para evitar deadlock
	if stopChan != nil {
		close(stopChan)
	}
	fmt.Println("[BOT] Stopped")
	b.PrintStats()
}

func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

func (b *Bot) GetState() BotState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *Bot) GetCurrentTarget() *EntityInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.currentTarget == nil {
		return nil
	}
	cpy := *b.currentTarget
	return &cpy
}

func (b *Bot) GetStats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stats
}

func (b *Bot) PrintStats() {
	s := b.GetStats()
	elapsed := time.Since(s.StartTime)
	fmt.Printf("[BOT] Stats: %d killed | %d targets | uptime %s\n",
		s.MobsKilled, s.TargetsSet, elapsed.Round(time.Second))
}

// ====================
// Runtime config
// ====================

func (b *Bot) SetMobNames(names []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.MobNames = names
	fmt.Printf("[BOT] Mob list: %v\n", names)
}

func (b *Bot) AddMobName(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.MobNames = append(b.config.MobNames, name)
	fmt.Printf("[BOT] +mob: %s\n", name)
}

func (b *Bot) RemoveMobName(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, n := range b.config.MobNames {
		if strings.EqualFold(n, name) {
			b.config.MobNames = append(b.config.MobNames[:i], b.config.MobNames[i+1:]...)
			fmt.Printf("[BOT] -mob: %s\n", name)
			return
		}
	}
}

func (b *Bot) SetMaxRange(r float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.MaxRange = r
	fmt.Printf("[BOT] Range: %.0fm\n", r)
}

func (b *Bot) SetPartialMatch(partial bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.PartialMatch = partial
}

func (b *Bot) SetAttackKey(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.AttackKey = key
	fmt.Printf("[BOT] Attack key: %s\n", key)
}

func (b *Bot) SetLootKey(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.LootKey = key
	fmt.Printf("[BOT] Loot key: %s\n", key)
}

func (b *Bot) SetAutoAttack(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.AutoAttack = enabled
}

func (b *Bot) SetAutoLoot(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.AutoLoot = enabled
}

func (b *Bot) SetLootViaPacket(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.LootViaPacket = enabled
	fmt.Printf("[BOT] Loot via pacote: %v\n", enabled)
}

func (b *Bot) SetAttackDelay(ms int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.AttackDelay = time.Duration(ms) * time.Millisecond
}

func (b *Bot) SetLootDelay(ms int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.LootDelay = time.Duration(ms) * time.Millisecond
}

func (b *Bot) SetKeySender(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.SendKey = fn
}

func (b *Bot) SetHPPotion(key string, threshold float32, enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.HPPotionKey = key
	b.config.HPPotionThreshold = threshold
	b.config.HPPotionEnabled = enabled
	if enabled {
		fmt.Printf("[BOT] HP Potion: %s (< %.0f%%)\n", key, threshold)
	}
}

func (b *Bot) SetMPPotion(key string, threshold float32, enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.MPPotionKey = key
	b.config.MPPotionThreshold = threshold
	b.config.MPPotionEnabled = enabled
	if enabled {
		fmt.Printf("[BOT] MP Potion: %s (< %.0f%%)\n", key, threshold)
	}
}

func (b *Bot) SetPotionCooldown(ms int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.PotionCooldown = time.Duration(ms) * time.Millisecond
	fmt.Printf("[BOT] Potion cooldown: %dms\n", ms)
}

// SetExcludeBuff configura a exclusão de mobs por buff (ex: 851).
func (b *Bot) SetExcludeBuff(buffID uint32, enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.ExcludeBuffID = buffID
	b.config.ExcludeBuffEnabled = enabled
	fmt.Printf("[BOT] Exclusão por buff: id=%d enabled=%v\n", buffID, enabled)
}

// SetQueueReorgInterval define a cadência da reorganização da fila (ms).
func (b *Bot) SetQueueReorgInterval(ms int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ms > 0 {
		b.config.QueueReorgInterval = time.Duration(ms) * time.Millisecond
	}
}

// SetHasBuffFn injeta o leitor de buff por-entidade (usado pela exclusão 851).
func (b *Bot) SetHasBuffFn(fn func(entityAddr uint32, buffID uint32) bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.HasBuff = fn
}

// SetBuffBlacklistTTL define por quanto tempo (ms) um mob largado por buff fica
// na blacklist antes de poder ser re-selecionado.
func (b *Bot) SetBuffBlacklistTTL(ms int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ms > 0 {
		b.config.BuffBlacklistTTL = time.Duration(ms) * time.Millisecond
	}
}

func (b *Bot) SetPlayerHPProvider(fn func() (uint32, uint32)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.GetPlayerHP = fn
}

func (b *Bot) SetPlayerMPProvider(fn func() (uint32, uint32)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.GetPlayerMP = fn
}

func (b *Bot) GetConfig() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// ====================
// Kill Queue
// ====================

// UpdateKillQueue atualiza a fila de mobs para matar com base nas entidades atuais.
// Ordena por distância: o mob mais perto é sempre o primeiro da fila.
func (b *Bot) UpdateKillQueue(entities []EntityInfo, maxRange float32, mobNames []string, partial bool) {
	// Snapshot da config de exclusão por buff. As leituras de memória do HasBuff
	// são feitas ANTES de segurar b.mu, pra não prender o lock durante os RPM.
	b.mu.RLock()
	excludeEnabled := b.config.ExcludeBuffEnabled
	excludeBuffID := b.config.ExcludeBuffID
	hasBuff := b.config.HasBuff
	// Snapshot da blacklist temporária (mobs largados por terem o buff de
	// exclusão, detectado pós-seleção). Só os ainda não expirados.
	now := time.Now()
	blacklisted := make(map[uint32]bool, len(b.buffBlacklist))
	for id, exp := range b.buffBlacklist {
		if now.Before(exp) {
			blacklisted[id] = true
		}
	}
	b.mu.RUnlock()

	// Cria set de IDs atuais válidos
	currentValid := make(map[uint32]EntityInfo)
	for _, e := range entities {
		if e.Distance > maxRange || e.HP == 0 {
			continue
		}
		if !matchName(e.Name, mobNames, partial) {
			continue
		}
		// Blacklist temporário: mob largado recentemente por ter o buff.
		if blacklisted[e.EntityID] {
			continue
		}
		// Exclusão total (pré-seleção): funciona SE o cliente expõe os buffs de
		// mobs não-alvo. Caso contrário, a rede de segurança é o drop+blacklist
		// pós-seleção em tickCombat.
		if excludeEnabled && excludeBuffID != 0 && hasBuff != nil && e.Address != 0 &&
			hasBuff(e.Address, excludeBuffID) {
			continue
		}
		currentValid[e.EntityID] = e
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Purga entradas expiradas da blacklist
	for id, exp := range b.buffBlacklist {
		if !now.Before(exp) {
			delete(b.buffBlacklist, id)
		}
	}

	// Remove mobs que não são mais válidos (mortos, fora de range, etc)
	newOrder := make([]uint32, 0, len(b.killQueueOrder))
	for _, id := range b.killQueueOrder {
		if _, ok := currentValid[id]; ok {
			newOrder = append(newOrder, id)
		} else {
			delete(b.killQueue, id)
		}
	}
	b.killQueueOrder = newOrder

	// Adiciona novos mobs à queue
	for id, e := range currentValid {
		if _, exists := b.killQueue[id]; !exists {
			b.killQueue[id] = e
			b.killQueueOrder = append(b.killQueueOrder, id)
			fmt.Printf("[BOT] +Queue[%d]: %s (ID:%d HP:%d Dist:%.0fm)\n",
				len(b.killQueueOrder), e.Name, e.EntityID, e.HP, e.Distance)
		} else {
			// Atualiza info do mob existente (distância, HP, posição)
			b.killQueue[id] = e
		}
	}

	// Ordena a queue por distância (mais perto primeiro)
	sort.Slice(b.killQueueOrder, func(i, j int) bool {
		ei := b.killQueue[b.killQueueOrder[i]]
		ej := b.killQueue[b.killQueueOrder[j]]
		return ei.Distance < ej.Distance
	})
}

// RemoveFromKillQueue remove um mob da fila pelo EntityID.
func (b *Bot) RemoveFromKillQueue(entityID uint32) {
	b.removeFromQueueWithReason(entityID, "KILLED")
}

// RemoveFromKillQueueOutOfRange remove mob que saiu da range.
func (b *Bot) RemoveFromKillQueueOutOfRange(entityID uint32) {
	b.removeFromQueueWithReason(entityID, "OUT OF RANGE")
}

// removeFromQueueWithReason remove um mob da fila com motivo especificado.
func (b *Bot) removeFromQueueWithReason(entityID uint32, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.killQueue[entityID]; ok {
		fmt.Printf("[BOT] -Queue: %s (ID:%d) - %s\n", e.Name, entityID, reason)
		delete(b.killQueue, entityID)

		// Remove da ordem FIFO
		for i, id := range b.killQueueOrder {
			if id == entityID {
				b.killQueueOrder = append(b.killQueueOrder[:i], b.killQueueOrder[i+1:]...)
				break
			}
		}
	}
}

// BlacklistMob coloca um EntityID na blacklist temporária por ttl (ou o
// BuffBlacklistTTL configurado se ttl<=0). Enquanto ativo, o mob é ignorado
// pela fila. Usado quando o alvo é largado por ter o buff de exclusão.
func (b *Bot) BlacklistMob(entityID uint32, ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ttl <= 0 {
		ttl = b.config.BuffBlacklistTTL
	}
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	if b.buffBlacklist == nil {
		b.buffBlacklist = make(map[uint32]time.Time)
	}
	b.buffBlacklist[entityID] = time.Now().Add(ttl)
}

// GetKillQueue retorna uma cópia da fila de mobs na ordem FIFO.
func (b *Bot) GetKillQueue() []EntityInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]EntityInfo, 0, len(b.killQueueOrder))
	for _, id := range b.killQueueOrder {
		if e, ok := b.killQueue[id]; ok {
			result = append(result, e)
		}
	}
	return result
}

// GetKillQueueCount retorna o número de mobs na fila.
func (b *Bot) GetKillQueueCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.killQueue)
}

// GetNextTarget retorna o mob mais perto da fila (excluindo o target atual).
func (b *Bot) GetNextTarget() *EntityInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var currentTargetID uint32
	if b.currentTarget != nil {
		currentTargetID = b.currentTarget.EntityID
	}

	var closest *EntityInfo
	for _, id := range b.killQueueOrder {
		if id == currentTargetID {
			continue
		}
		if e, ok := b.killQueue[id]; ok && e.HP > 0 {
			if closest == nil || e.Distance < closest.Distance {
				cpy := e
				closest = &cpy
			}
		}
	}
	return closest
}

// ====================
// Main loop
// ====================

func (b *Bot) loop() {
	ticker := time.NewTicker(b.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopChan:
			return
		case <-ticker.C:
			b.tick()
		}
	}
}

func (b *Bot) tick() {
	// Always check potions regardless of state
	b.tickPotions()

	// Reorganiza a fila de forma independente do estado, na cadência
	// QueueReorgInterval (ex: 50ms): aplica exclusão por buff (851) +
	// prioridade de proximidade. Assim a fila continua correta mesmo em combate.
	b.maybeReorgQueue()

	b.mu.RLock()
	state := b.state
	b.mu.RUnlock()

	switch state {
	case StateIdle:
		b.tickIdle()
	case StateTargeting:
		b.tickTargeting()
	case StateCombat:
		b.tickCombat()
	case StateLooting:
		b.setState(StateIdle)
	}
}

// tickPotions verifica HP/MP e usa potions se necessário
func (b *Bot) tickPotions() {
	b.mu.RLock()
	hpEnabled := b.config.HPPotionEnabled
	mpEnabled := b.config.MPPotionEnabled
	hpKey := b.config.HPPotionKey
	mpKey := b.config.MPPotionKey
	hpThreshold := b.config.HPPotionThreshold
	mpThreshold := b.config.MPPotionThreshold
	cooldown := b.config.PotionCooldown
	sendKey := b.config.SendKey
	getHP := b.config.GetPlayerHP
	getMP := b.config.GetPlayerMP
	lastHP := b.lastHPPotionTime
	lastMP := b.lastMPPotionTime
	b.mu.RUnlock()

	if sendKey == nil {
		return
	}

	now := time.Now()

	// Check HP Potion
	if hpEnabled && getHP != nil && hpKey != "" {
		if now.Sub(lastHP) >= cooldown {
			current, max := getHP()
			if max > 0 {
				percent := float32(current) / float32(max) * 100
				if percent < hpThreshold && percent > 0 {
					sendKeySpam(sendKey, hpKey)
					b.mu.Lock()
					b.lastHPPotionTime = now
					b.mu.Unlock()
					fmt.Printf("[BOT] HP Potion used (%.0f%% < %.0f%%) [x%d]\n", percent, hpThreshold, KeySpamCount)
				}
			}
		}
	}

	// Check MP Potion
	if mpEnabled && getMP != nil && mpKey != "" {
		if now.Sub(lastMP) >= cooldown {
			current, max := getMP()
			if max > 0 {
				percent := float32(current) / float32(max) * 100
				if percent < mpThreshold && percent > 0 {
					sendKeySpam(sendKey, mpKey)
					b.mu.Lock()
					b.lastMPPotionTime = now
					b.mu.Unlock()
					fmt.Printf("[BOT] MP Potion used (%.0f%% < %.0f%%) [x%d]\n", percent, mpThreshold, KeySpamCount)
				}
			}
		}
	}
}

// maybeReorgQueue reorganiza a kill queue na cadência QueueReorgInterval,
// independente do estado do bot. É o ÚNICO ponto que chama UpdateKillQueue,
// então a exclusão por buff (851) e a ordenação por proximidade acontecem numa
// taxa controlada (~50ms), sem martelar os RPM de leitura de buff a cada tick.
func (b *Bot) maybeReorgQueue() {
	b.mu.RLock()
	interval := b.config.QueueReorgInterval
	last := b.lastReorgTime
	mobNames := b.config.MobNames
	partial := b.config.PartialMatch
	b.mu.RUnlock()

	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	if time.Since(last) < interval {
		return
	}
	if len(mobNames) == 0 {
		return
	}

	entities := b.provider.GetEntities()
	if len(entities) == 0 {
		return
	}

	maxRange := b.getEffectiveRange()
	b.UpdateKillQueue(entities, maxRange, mobNames, partial)

	b.mu.Lock()
	b.lastReorgTime = time.Now()
	b.mu.Unlock()
}

// frontOfQueue retorna uma cópia do melhor alvo da fila (primeiro válido com
// HP > 0). Como a fila já está ordenada por proximidade e sem os mobs
// excluídos por buff, o primeiro elemento é o mob mais perto "limpo".
func (b *Bot) frontOfQueue() *EntityInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, id := range b.killQueueOrder {
		if e, ok := b.killQueue[id]; ok && e.HP > 0 {
			cpy := e
			return &cpy
		}
	}
	return nil
}

func (b *Bot) tickIdle() {
	// A fila é reorganizada por maybeReorgQueue (throttle QueueReorgInterval),
	// já com exclusão por buff + ordenação por proximidade. Aqui só pegamos
	// o melhor alvo da frente da fila.
	closest := b.frontOfQueue()
	if closest == nil {
		return
	}

	b.mu.Lock()
	b.currentTarget = closest
	b.state = StateTargeting
	b.mu.Unlock()

	queueCount := b.GetKillQueueCount()
	fmt.Printf("[BOT] Target: %s (ID:%d HP:%d Dist:%.0fm) [Queue: %d]\n",
		closest.Name, closest.EntityID, closest.HP, closest.Distance, queueCount)
}

func (b *Bot) tickTargeting() {
	b.mu.RLock()
	target := b.currentTarget
	setTargetFn := b.config.SetTargetFn
	b.mu.RUnlock()

	if target == nil {
		b.setState(StateIdle)
		return
	}

	if setTargetFn == nil {
		fmt.Printf("[BOT] SetTargetFn não configurado\n")
		b.clearTarget()
		return
	}

	if err := setTargetFn(target.EntityID); err != nil {
		fmt.Printf("[BOT] SetTarget failed: %v\n", err)
		// Não remove da queue aqui - deixa UpdateKillQueue validar o estado.
		// Espera antes de liberar pro próximo retry (evita martelar o
		// injector a cada ScanInterval quando a chamada está falhando).
		b.noteTargetFailure(target.EntityID, "SetTarget erro")
		time.Sleep(b.config.TargetDelay)
		b.clearTarget()
		return
	}

	time.Sleep(b.config.TargetDelay)

	// Confirma que pegou
	if b.getCurrentTargetId() != target.EntityID {
		// Não fica preso: se o mesmo mob falha repetidas vezes (ex: Untouchable
		// que o client recusa selecionar), ele é blacklistado e a fila segue
		// pro próximo em vez de esperar o buff cair.
		if !b.noteTargetFailure(target.EntityID, "mismatch") {
			fmt.Printf("[BOT] Target mismatch - tentando novamente\n")
		}
		time.Sleep(b.config.TargetDelay)
		b.clearTarget()
		return
	}

	// Selecionou com sucesso: zera o contador de falhas desse mob.
	b.clearTargetAttempts(target.EntityID)

	// Antes de entrar em combate: se o alvo tem a aura de exclusão (ex:
	// 815/Untouchable — buff OU debuff), pula pro próximo SEM gastar ataque.
	b.mu.RLock()
	excludeEnabled := b.config.ExcludeBuffEnabled
	excludeBuffID := b.config.ExcludeBuffID
	hasBuff := b.config.HasBuff
	tgtAddr := target.Address
	b.mu.RUnlock()
	if excludeEnabled && excludeBuffID != 0 && hasBuff != nil && tgtAddr != 0 &&
		hasBuff(tgtAddr, excludeBuffID) {
		fmt.Printf("[BOT] Alvo com aura %d (Untouchable) - pulando p/ próximo: %s (ID:%d)\n",
			excludeBuffID, target.Name, target.EntityID)
		b.BlacklistMob(target.EntityID, 0)
		b.RemoveFromKillQueue(target.EntityID)
		b.clearTarget()
		return
	}

	b.mu.Lock()
	b.stats.TargetsSet++
	b.stats.LastTargetAt = time.Now()
	b.mu.Unlock()

	fmt.Printf("[BOT] Targeting: %s (ID:%d)\n", target.Name, target.EntityID)

	b.setState(StateCombat)

	if b.config.OnTargetAcquired != nil {
		b.config.OnTargetAcquired(*target)
	}
}

// maxTargetAttempts: falhas consecutivas de seleção do MESMO mob antes de
// blacklistá-lo e pular. Alto o bastante pra tolerar latência normal do
// SetTarget (que resolve em 1-3 tentativas), baixo o bastante pra não travar.
const maxTargetAttempts = 5

// noteTargetFailure incrementa o contador de falhas de seleção do mob. Se
// passar do limite, blacklista + remove da fila e devolve true (pular).
func (b *Bot) noteTargetFailure(entityID uint32, reason string) bool {
	b.mu.Lock()
	if b.targetAttempts == nil {
		b.targetAttempts = make(map[uint32]int)
	}
	b.targetAttempts[entityID]++
	n := b.targetAttempts[entityID]
	b.mu.Unlock()

	if n >= maxTargetAttempts {
		fmt.Printf("[BOT] Não consegui selecionar ID:%d após %d tentativas (%s) - blacklist + próximo\n",
			entityID, n, reason)
		b.BlacklistMob(entityID, 0)
		b.RemoveFromKillQueue(entityID)
		b.mu.Lock()
		delete(b.targetAttempts, entityID)
		b.mu.Unlock()
		return true
	}
	return false
}

// clearTargetAttempts zera o contador de falhas de um mob (após seleção OK).
func (b *Bot) clearTargetAttempts(entityID uint32) {
	b.mu.Lock()
	delete(b.targetAttempts, entityID)
	b.mu.Unlock()
}

func (b *Bot) tickCombat() {
	b.mu.RLock()
	target := b.currentTarget
	autoAttack := b.config.AutoAttack
	attackKey := b.config.AttackKey
	attackDelay := b.config.AttackDelay
	sendKey := b.config.SendKey
	b.mu.RUnlock()

	if target == nil {
		b.setState(StateIdle)
		return
	}

	// Primeiro verifica se o mob ainda está vivo na entity list
	entities := b.provider.GetEntities()
	alive := false
	maxRange := b.getEffectiveRange()

	for _, e := range entities {
		if e.EntityID == target.EntityID && e.HP > 0 {
			alive = true
			b.mu.Lock()
			b.currentTarget.HP = e.HP
			b.currentTarget.Distance = e.Distance
			b.currentTarget.PosX = e.PosX
			b.currentTarget.PosY = e.PosY
			b.currentTarget.PosZ = e.PosZ
			b.mu.Unlock()

			// Validar se ainda está na range
			if e.Distance > maxRange {
				fmt.Printf("[BOT] Target out of range: %s (%.0fm > %.0fm)\n", target.Name, e.Distance, maxRange)
				b.RemoveFromKillQueueOutOfRange(target.EntityID)
				b.clearTarget()
				return
			}
			break
		}
	}

	if !alive {
		fmt.Printf("[BOT] Dead: %s\n", target.Name)
		b.onMobDead(*target)
		return
	}

	// Se o alvo atual ganhou o buff de exclusão (ex: 851) durante o combate,
	// abandona — consistente com a exclusão total da fila.
	b.mu.RLock()
	excludeEnabled := b.config.ExcludeBuffEnabled
	excludeBuffID := b.config.ExcludeBuffID
	hasBuff := b.config.HasBuff
	tgtAddr := target.Address
	b.mu.RUnlock()
	if excludeEnabled && excludeBuffID != 0 && hasBuff != nil && tgtAddr != 0 &&
		hasBuff(tgtAddr, excludeBuffID) {
		fmt.Printf("[BOT] Target com buff %d (Untouchable) - largando + blacklist: %s (ID:%d)\n",
			excludeBuffID, target.Name, target.EntityID)
		b.BlacklistMob(target.EntityID, 0)
		b.RemoveFromKillQueue(target.EntityID)
		b.clearTarget()
		return
	}

	// Mob vivo mas target perdido no client? Re-targetar o MESMO mob
	currentId := b.getCurrentTargetId()
	if currentId == 0 || currentId != target.EntityID {
		fmt.Printf("[BOT] Target lost in client, re-targeting: %s (ID:%d)\n", target.Name, target.EntityID)
		b.setState(StateTargeting)
		return
	}

	// Auto-attack: pressiona tecla de ataque periodicamente (keyspam)
	if autoAttack && sendKey != nil && attackKey != "" {
		if time.Since(b.lastAttackTime) >= attackDelay {
			sendKeySpam(sendKey, attackKey)
			b.lastAttackTime = time.Now()
		}
	}

	if b.config.OnCombatTick != nil {
		b.mu.RLock()
		t := *b.currentTarget
		b.mu.RUnlock()
		b.config.OnCombatTick(t)
	}
}

// ====================
// Internal helpers
// ====================

func (b *Bot) setState(s BotState) {
	b.mu.Lock()
	b.state = s
	b.mu.Unlock()
}

func (b *Bot) clearTarget() {
	b.mu.Lock()
	b.currentTarget = nil
	b.state = StateIdle
	b.mu.Unlock()
}

func (b *Bot) onMobDead(target EntityInfo) {
	b.mu.RLock()
	autoLoot := b.config.AutoLoot
	lootKey := b.config.LootKey
	lootDelay := b.config.LootDelay
	sendKey := b.config.SendKey
	lootViaPacket := b.config.LootViaPacket
	lootAll := b.config.LootAll
	b.mu.RUnlock()

	// Remove da kill queue
	b.RemoveFromKillQueue(target.EntityID)

	b.mu.Lock()
	b.stats.MobsKilled++
	b.currentTarget = nil
	b.state = StateLooting
	b.mu.Unlock()

	queueCount := b.GetKillQueueCount()
	fmt.Printf("[BOT] Killed: %s [Queue remaining: %d]\n", target.Name, queueCount)

	// Auto-loot via pacote: chama LootMgr_RequestLoot + TakeItem direto no
	// EntityID que acabou de morrer, sem keyspam nem UI/Lua.
	if autoLoot && lootViaPacket && lootAll != nil && target.EntityID != 0 {
		go func() {
			time.Sleep(lootDelay)
			taken, err := lootAll(target.EntityID)
			if err != nil {
				fmt.Printf("[BOT] Loot via pacote falhou (%s): %v\n", target.Name, err)
			} else {
				fmt.Printf("[BOT] Loot via pacote: %s (%d itens)\n", target.Name, taken)
			}
			b.lastLootTime = time.Now()

			time.Sleep(200 * time.Millisecond)
			b.setState(StateIdle)
		}()
	} else if autoLoot && sendKey != nil && lootKey != "" {
		// Auto-loot: pressiona tecla de loot após delay (keyspam)
		go func() {
			time.Sleep(lootDelay)
			sendKeySpam(sendKey, lootKey)
			b.lastLootTime = time.Now()
			fmt.Printf("[BOT] Looting: %s [x%d]\n", target.Name, KeySpamCount)

			// Volta pro idle após loot
			time.Sleep(200 * time.Millisecond)
			b.setState(StateIdle)
		}()
	} else {
		b.setState(StateIdle)
	}

	if b.config.OnTargetDead != nil {
		b.config.OnTargetDead(target)
	}
}

func (b *Bot) getCurrentTargetId() uint32 {
	ptr := readU32(b.handle, b.x2game+PTR_ENEMY_TARGET_BASE)
	if ptr == 0 {
		return 0
	}
	return readU32(b.handle, uintptr(ptr)+OFF_TARGET_ID)
}

// ====================
// Memory helpers (minimal, só o que o bot precisa)
// ====================

func readU32(handle windows.Handle, addr uintptr) uint32 {
	var val uint32
	var n uintptr
	procReadProcessMem.Call(uintptr(handle), addr, uintptr(unsafe.Pointer(&val)), 4, uintptr(unsafe.Pointer(&n)))
	return val
}

// ====================
// Name matching
// ====================

func matchName(entityName string, mobNames []string, partial bool) bool {
	lower := strings.ToLower(entityName)
	for _, name := range mobNames {
		t := strings.ToLower(name)
		if partial {
			if strings.Contains(lower, t) {
				return true
			}
		} else {
			if lower == t {
				return true
			}
		}
	}
	return false
}

func matchMode(partial bool) string {
	if partial {
		return "partial"
	}
	return "exact"
}

// ====================
// Key spam helper
// ====================

const (
	KeySpamCount    = 5             // Número de vezes que cada tecla é pressionada
	KeySpamInterval = 30 * time.Millisecond // Intervalo entre key presses
)

// sendKeySpam envia uma tecla múltiplas vezes para garantir registro
func sendKeySpam(sendKey func(string), key string) {
	if sendKey == nil || key == "" {
		return
	}
	for i := 0; i < KeySpamCount; i++ {
		sendKey(key)
		if i < KeySpamCount-1 {
			time.Sleep(KeySpamInterval)
		}
	}
}