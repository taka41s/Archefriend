package bot

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ====================
// Constants (SetTarget)
// ====================

const (
	OFFSET_SET_TARGET     uintptr = 0x1BE090
	PTR_ENEMY_TARGET_BASE uintptr = 0x19EBF4
	OFF_TARGET_ID         uintptr = 0x08

	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	MEM_RELEASE            = 0x8000
	PAGE_EXECUTE_READWRITE = 0x40
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procWriteProcessMem    = kernel32.NewProc("WriteProcessMemory")
	procReadProcessMem     = kernel32.NewProc("ReadProcessMemory")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
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

	// Callbacks (opcionais)
	OnTargetAcquired func(target EntityInfo)
	OnTargetDead     func(target EntityInfo)
	OnCombatTick     func(target EntityInfo)

	// Key sender (injetado pelo main)
	SendKey func(key string)
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
	stats           Stats
	stopChan        chan struct{}
	lastAttackTime  time.Time
	lastLootTime    time.Time
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

func (b *Bot) GetConfig() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// ====================
// Kill Queue
// ====================

// UpdateKillQueue atualiza a fila de mobs para matar com base nas entidades atuais.
// Usa FIFO: primeiro a entrar na range é o primeiro a ser atacado.
// Novos mobs vão para o final da fila.
func (b *Bot) UpdateKillQueue(entities []EntityInfo, maxRange float32, mobNames []string, partial bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Cria set de IDs atuais válidos
	currentValid := make(map[uint32]EntityInfo)
	for _, e := range entities {
		if e.Distance > maxRange || e.HP == 0 {
			continue
		}
		if !matchName(e.Name, mobNames, partial) {
			continue
		}
		currentValid[e.EntityID] = e
	}

	// Remove mobs que não são mais válidos (mortos, fora de range, etc)
	// Também remove da ordem FIFO
	newOrder := make([]uint32, 0, len(b.killQueueOrder))
	for _, id := range b.killQueueOrder {
		if _, ok := currentValid[id]; ok {
			newOrder = append(newOrder, id)
		} else {
			// Mob saiu da range ou morreu
			delete(b.killQueue, id)
		}
	}
	b.killQueueOrder = newOrder

	// Adiciona novos mobs ao FINAL da queue (FIFO)
	for id, e := range currentValid {
		if _, exists := b.killQueue[id]; !exists {
			b.killQueue[id] = e
			b.killQueueOrder = append(b.killQueueOrder, id) // Vai pro final
			fmt.Printf("[BOT] +Queue[%d]: %s (ID:%d HP:%d Dist:%.0fm)\n",
				len(b.killQueueOrder), e.Name, e.EntityID, e.HP, e.Distance)
		} else {
			// Atualiza info do mob existente (posição na fila não muda)
			b.killQueue[id] = e
		}
	}
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

// GetNextTarget retorna o primeiro mob da fila FIFO (excluindo o target atual).
func (b *Bot) GetNextTarget() *EntityInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var currentTargetID uint32
	if b.currentTarget != nil {
		currentTargetID = b.currentTarget.EntityID
	}

	// FIFO: retorna o primeiro da fila que não seja o target atual
	for _, id := range b.killQueueOrder {
		if id == currentTargetID {
			continue
		}
		if e, ok := b.killQueue[id]; ok && e.HP > 0 {
			cpy := e
			return &cpy
		}
	}
	return nil
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

func (b *Bot) tickIdle() {
	entities := b.provider.GetEntities()
	if len(entities) == 0 {
		return
	}

	// Use dynamic range from ESP if available
	maxRange := b.getEffectiveRange()

	b.mu.RLock()
	mobNames := b.config.MobNames
	partial := b.config.PartialMatch
	b.mu.RUnlock()

	if len(mobNames) == 0 {
		return
	}

	// Atualiza kill queue com mobs válidos na range
	b.UpdateKillQueue(entities, maxRange, mobNames, partial)

	// Cria lookup rápido das entidades atuais (para validar se mob ainda existe)
	currentEntities := make(map[uint32]EntityInfo)
	for _, e := range entities {
		currentEntities[e.EntityID] = e
	}

	// Pega o próximo target da queue (FIFO - primeiro a entrar)
	// Só seleciona mobs que existem na lista atual E tem HP > 0
	b.mu.Lock()
	var first *EntityInfo

	for _, id := range b.killQueueOrder {
		if _, ok := b.killQueue[id]; ok {
			// Verifica se o mob ainda existe na lista de entidades atual
			currentEntity, exists := currentEntities[id]
			if !exists {
				// Mob despawnou - será removido pelo UpdateKillQueue na próxima iteração
				continue
			}
			if currentEntity.HP == 0 {
				// Mob morto - pular, UpdateKillQueue vai remover
				continue
			}
			// Mob válido: existe e tem HP > 0
			cpy := currentEntity
			first = &cpy
			break
		}
	}
	b.mu.Unlock()

	if first != nil {
		b.mu.Lock()
		b.currentTarget = first
		b.state = StateTargeting
		b.mu.Unlock()

		queueCount := b.GetKillQueueCount()
		fmt.Printf("[BOT] Target: %s (ID:%d HP:%d Dist:%.0fm) [Queue: %d]\n",
			first.Name, first.EntityID, first.HP, first.Distance, queueCount)
	}
}

func (b *Bot) tickTargeting() {
	b.mu.RLock()
	target := b.currentTarget
	b.mu.RUnlock()

	if target == nil {
		b.setState(StateIdle)
		return
	}

	if err := b.setTarget(target.EntityID); err != nil {
		fmt.Printf("[BOT] SetTarget failed: %v\n", err)
		// Não remove da queue aqui - deixa UpdateKillQueue validar o estado
		b.clearTarget()
		return
	}

	time.Sleep(b.config.TargetDelay)

	// Confirma que pegou
	if b.getCurrentTargetId() != target.EntityID {
		fmt.Printf("[BOT] Target mismatch - tentando novamente\n")
		// Não remove da queue - pode ser lag do client, tenta de novo
		b.clearTarget()
		return
	}

	b.mu.Lock()
	b.stats.TargetsSet++
	b.stats.LastTargetAt = time.Now()
	b.state = StateCombat
	b.mu.Unlock()

	fmt.Printf("[BOT] Targeting: %s (ID:%d)\n", target.Name, target.EntityID)

	if b.config.OnTargetAcquired != nil {
		b.config.OnTargetAcquired(*target)
	}
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

	// Target perdido no client?
	currentId := b.getCurrentTargetId()
	if currentId == 0 || currentId != target.EntityID {
		fmt.Printf("[BOT] Target lost: %s\n", target.Name)
		b.onMobDead(*target)
		return
	}

	// Ainda vivo na entity list?
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
				// Remove da kill queue também para não ser selecionado de novo
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

	// Auto-attack: pressiona tecla de ataque periodicamente
	if autoAttack && sendKey != nil && attackKey != "" {
		if time.Since(b.lastAttackTime) >= attackDelay {
			sendKey(attackKey)
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

	// Auto-loot: pressiona tecla de loot após delay
	if autoLoot && sendKey != nil && lootKey != "" {
		go func() {
			time.Sleep(lootDelay)
			sendKey(lootKey)
			b.lastLootTime = time.Now()
			fmt.Printf("[BOT] Looting: %s\n", target.Name)

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

// ====================
// SetTarget (shellcode)
// ====================

func (b *Bot) setTarget(unitId uint32) error {
	addr := b.x2game + OFFSET_SET_TARGET

	shellcode := []byte{
		0x6A, 0x00,                   // push 0 (flag)
		0x68, 0x00, 0x00, 0x00, 0x00, // push unitId
		0xB8, 0x00, 0x00, 0x00, 0x00, // mov eax, addr
		0xFF, 0xD0,                   // call eax
		0x83, 0xC4, 0x08,             // add esp, 8
		0xC3,                         // ret
	}

	*(*uint32)(unsafe.Pointer(&shellcode[3])) = unitId
	*(*uint32)(unsafe.Pointer(&shellcode[8])) = uint32(addr)

	alloc, err := virtualAllocEx(b.handle, 256)
	if err != nil {
		return err
	}
	defer virtualFreeEx(b.handle, alloc)

	if err := writeProcessMemory(b.handle, alloc, shellcode); err != nil {
		return err
	}

	th, err := createRemoteThread(b.handle, alloc)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(th)

	windows.WaitForSingleObject(th, 5000)
	return nil
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

func virtualAllocEx(handle windows.Handle, size uint32) (uintptr, error) {
	r, _, err := procVirtualAllocEx.Call(uintptr(handle), 0, uintptr(size), MEM_COMMIT|MEM_RESERVE, PAGE_EXECUTE_READWRITE)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func virtualFreeEx(handle windows.Handle, addr uintptr) {
	procVirtualFreeEx.Call(uintptr(handle), addr, 0, MEM_RELEASE)
}

func writeProcessMemory(handle windows.Handle, addr uintptr, data []byte) error {
	var n uintptr
	r, _, err := procWriteProcessMem.Call(uintptr(handle), addr, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return err
	}
	return nil
}

func createRemoteThread(handle windows.Handle, addr uintptr) (windows.Handle, error) {
	r, _, err := procCreateRemoteThread.Call(uintptr(handle), 0, 0, addr, 0, 0, 0)
	if r == 0 {
		return 0, err
	}
	return windows.Handle(r), nil
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