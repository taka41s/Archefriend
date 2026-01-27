package buff

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Buff structure offsets (0x68 bytes total)
const (
	BUFF_SIZE = 0x68

	BUFF_OFF_SLOT     = 0x00
	BUFF_OFF_ID       = 0x04
	BUFF_OFF_DURATION = 0x30
	BUFF_OFF_ELAPSED  = 0x34
	BUFF_OFF_TICK     = 0x3C
	BUFF_OFF_STACK    = 0x40
)

// InjectedBuff representa um buff injetado
type InjectedBuff struct {
	ID        uint32
	SourceID  uint32 // ID do buff original clonado
	Permanent bool
	Index     int // índice na lista de buffs
}

// Injector gerencia injeção de buffs
type Injector struct {
	handle       windows.Handle
	buffListAddr uintptr

	injectedBuffs map[uint32]*InjectedBuff
	mutex         sync.RWMutex

	freezeEnabled bool
	freezeStop    chan bool
	freezeRunning bool
}

// NewInjector cria um novo injector
func NewInjector(handle windows.Handle) *Injector {
	return &Injector{
		handle:        handle,
		injectedBuffs: make(map[uint32]*InjectedBuff),
		freezeStop:    make(chan bool),
	}
}

// SetBuffListAddr atualiza o endereço da lista de buffs
func (inj *Injector) SetBuffListAddr(addr uintptr) {
	inj.buffListAddr = addr
}

// GetBuffListAddr retorna o endereço atual
func (inj *Injector) GetBuffListAddr() uintptr {
	return inj.buffListAddr
}

// readU32 lê um uint32 da memória
func (inj *Injector) readU32(addr uintptr) uint32 {
	var val uint32
	var read uintptr
	windows.ReadProcessMemory(inj.handle, addr, (*byte)(unsafe.Pointer(&val)), 4, &read)
	return val
}

// writeU32 escreve um uint32 na memória
func (inj *Injector) writeU32(addr uintptr, val uint32) bool {
	var written uintptr
	err := windows.WriteProcessMemory(inj.handle, addr, (*byte)(unsafe.Pointer(&val)), 4, &written)
	return err == nil
}

// readBytes lê bytes da memória
func (inj *Injector) readBytes(addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var read uintptr
	windows.ReadProcessMemory(inj.handle, addr, &buf[0], uintptr(size), &read)
	return buf
}

// writeBytes escreve bytes na memória
func (inj *Injector) writeBytes(addr uintptr, data []byte) bool {
	var written uintptr
	err := windows.WriteProcessMemory(inj.handle, addr, &data[0], uintptr(len(data)), &written)
	return err == nil
}

// GetBuffCount retorna a quantidade de buffs ativos
func (inj *Injector) GetBuffCount() uint32 {
	if inj.buffListAddr == 0 {
		return 0
	}
	return inj.readU32(inj.buffListAddr + 0x20)
}

// GetBuffAddress retorna o endereço de um buff pelo índice
func (inj *Injector) GetBuffAddress(index int) uintptr {
	return inj.buffListAddr + 0x28 + uintptr(index*BUFF_SIZE)
}

// GetBuffID retorna o ID de um buff pelo índice
func (inj *Injector) GetBuffID(index int) uint32 {
	addr := inj.GetBuffAddress(index)
	return inj.readU32(addr + BUFF_OFF_ID)
}

// FindBuffByID encontra o índice de um buff pelo ID
func (inj *Injector) FindBuffByID(buffID uint32) int {
	count := inj.GetBuffCount()
	for i := uint32(0); i < count && i < 30; i++ {
		if inj.GetBuffID(int(i)) == buffID {
			return int(i)
		}
	}
	return -1
}

// CloneAndInject clona um buff existente e injeta com novo ID
func (inj *Injector) CloneAndInject(sourceIndex int, newBuffID uint32, permanent bool) bool {
	if inj.buffListAddr == 0 {
		fmt.Println("[INJECT] Buff list not found")
		return false
	}

	count := inj.GetBuffCount()
	if count >= 30 {
		fmt.Println("[INJECT] Buff list full")
		return false
	}

	// Ler estrutura do buff fonte
	sourceAddr := inj.GetBuffAddress(sourceIndex)
	buffData := inj.readBytes(sourceAddr, BUFF_SIZE)
	if buffData == nil {
		fmt.Println("[INJECT] Failed to read source buff")
		return false
	}

	sourceID := *(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ID]))

	// Modificar dados
	*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_SLOT])) = count
	*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ID])) = newBuffID

	if permanent {
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_DURATION])) = 0
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ELAPSED])) = 0
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_TICK])) = 0
	} else {
		// Reset elapsed time
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ELAPSED])) = 0
	}

	// Escrever no novo slot
	newAddr := inj.GetBuffAddress(int(count))
	if !inj.writeBytes(newAddr, buffData) {
		fmt.Println("[INJECT] Failed to write buff")
		return false
	}

	// Incrementar count
	inj.writeU32(inj.buffListAddr+0x20, count+1)

	// Registrar buff injetado
	inj.mutex.Lock()
	inj.injectedBuffs[newBuffID] = &InjectedBuff{
		ID:        newBuffID,
		SourceID:  sourceID,
		Permanent: permanent,
		Index:     int(count),
	}
	inj.mutex.Unlock()

	fmt.Printf("[INJECT] Cloned buff %d -> %d at slot %d (permanent: %v)\n", sourceID, newBuffID, count, permanent)
	return true
}

// CloneFirstAndInject clona o primeiro buff válido disponível e injeta com novo ID
func (inj *Injector) CloneFirstAndInject(newBuffID uint32, permanent bool) bool {
	if inj.buffListAddr == 0 {
		fmt.Println("[INJECT] Buff list address not set!")
		return false
	}

	count := inj.GetBuffCount()
	fmt.Printf("[INJECT] BuffListAddr: 0x%X, Count: %d\n", inj.buffListAddr, count)

	if count == 0 {
		fmt.Println("[INJECT] No buffs to clone - use a skill that gives a buff first!")
		return false
	}

	// Tenta clonar a partir de cada buff até encontrar um válido
	for i := 0; i < int(count) && i < 10; i++ {
		buffID := inj.GetBuffID(i)
		fmt.Printf("[INJECT] Slot %d: BuffID=%d\n", i, buffID)

		// Pular buffs inválidos
		if buffID == 0 || buffID > 9999999 {
			fmt.Printf("[INJECT] Slot %d has invalid buffID, skipping\n", i)
			continue
		}

		if inj.CloneAndInject(i, newBuffID, permanent) {
			return true
		}
	}

	fmt.Println("[INJECT] Could not find valid source buff")
	return false
}

// RemoveBuff remove um buff pelo ID
func (inj *Injector) RemoveBuff(buffID uint32) bool {
	idx := inj.FindBuffByID(buffID)
	if idx < 0 {
		return false
	}

	count := inj.GetBuffCount()
	if count == 0 {
		return false
	}

	// Se não for o último, mover o último pro lugar deste
	if idx < int(count)-1 {
		lastAddr := inj.GetBuffAddress(int(count) - 1)
		lastData := inj.readBytes(lastAddr, BUFF_SIZE)

		// Atualizar slot
		*(*uint32)(unsafe.Pointer(&lastData[BUFF_OFF_SLOT])) = uint32(idx)

		targetAddr := inj.GetBuffAddress(idx)
		inj.writeBytes(targetAddr, lastData)
	}

	// Decrementar count
	inj.writeU32(inj.buffListAddr+0x20, count-1)

	// Remover do registro
	inj.mutex.Lock()
	delete(inj.injectedBuffs, buffID)
	inj.mutex.Unlock()

	fmt.Printf("[INJECT] Removed buff %d\n", buffID)
	return true
}

// MakeBuffPermanent torna um buff permanente
func (inj *Injector) MakeBuffPermanent(index int) {
	addr := inj.GetBuffAddress(index)
	inj.writeU32(addr+BUFF_OFF_DURATION, 0)
	inj.writeU32(addr+BUFF_OFF_ELAPSED, 0)
	inj.writeU32(addr+BUFF_OFF_TICK, 0)
}

// ResetBuffElapsed reseta o tempo decorrido de um buff
func (inj *Injector) ResetBuffElapsed(index int) {
	addr := inj.GetBuffAddress(index)
	inj.writeU32(addr+BUFF_OFF_ELAPSED, 0)
	inj.writeU32(addr+BUFF_OFF_TICK, 0)
}

// SetBuffStack define o stack count de um buff
func (inj *Injector) SetBuffStack(index int, stacks uint32) {
	addr := inj.GetBuffAddress(index)
	inj.writeU32(addr+BUFF_OFF_STACK, stacks)
}

// GetBuffStack retorna o stack count de um buff
func (inj *Injector) GetBuffStack(index int) uint32 {
	addr := inj.GetBuffAddress(index)
	return inj.readU32(addr + BUFF_OFF_STACK)
}

// StartFreezeLoop inicia o loop que mantém buffs permanentes
func (inj *Injector) StartFreezeLoop() {
	if inj.freezeRunning {
		return
	}
	inj.freezeRunning = true
	inj.freezeEnabled = true

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-inj.freezeStop:
				inj.freezeRunning = false
				return
			case <-ticker.C:
				if !inj.freezeEnabled || inj.buffListAddr == 0 {
					continue
				}

				inj.mutex.RLock()
				for buffID, info := range inj.injectedBuffs {
					if info.Permanent {
						idx := inj.FindBuffByID(buffID)
						if idx >= 0 {
							inj.ResetBuffElapsed(idx)
						}
					}
				}
				inj.mutex.RUnlock()
			}
		}
	}()
}

// StopFreezeLoop para o loop de freeze
func (inj *Injector) StopFreezeLoop() {
	if inj.freezeRunning {
		inj.freezeStop <- true
	}
}

// SetFreezeEnabled ativa/desativa o freeze
func (inj *Injector) SetFreezeEnabled(enabled bool) {
	inj.freezeEnabled = enabled
}

// IsFreezeEnabled retorna se o freeze está ativo
func (inj *Injector) IsFreezeEnabled() bool {
	return inj.freezeEnabled
}

// GetInjectedBuffs retorna lista de buffs injetados
func (inj *Injector) GetInjectedBuffs() map[uint32]*InjectedBuff {
	inj.mutex.RLock()
	defer inj.mutex.RUnlock()

	result := make(map[uint32]*InjectedBuff)
	for k, v := range inj.injectedBuffs {
		result[k] = v
	}
	return result
}

// GetInjectedCount retorna quantidade de buffs injetados
func (inj *Injector) GetInjectedCount() int {
	inj.mutex.RLock()
	defer inj.mutex.RUnlock()
	return len(inj.injectedBuffs)
}

// BuffExists verifica se um buff existe
func (inj *Injector) BuffExists(buffID uint32) bool {
	return inj.FindBuffByID(buffID) >= 0
}

// ValidateInjectedBuffs remove buffs que não existem mais
func (inj *Injector) ValidateInjectedBuffs() {
	inj.mutex.Lock()
	defer inj.mutex.Unlock()

	for buffID := range inj.injectedBuffs {
		if inj.FindBuffByID(buffID) < 0 {
			delete(inj.injectedBuffs, buffID)
		}
	}
}

// === HIDDEN BUFF SUPPORT ===

// BuffInfo representa informações de um buff para display
type BuffInfo struct {
	Index      int
	Slot       uint32
	ID         uint32
	Duration   uint32
	Elapsed    uint32
	Stack      uint32
	Flags      uint32
	IsHidden   bool // true se está além do count
	IsInjected bool
}

// GetAllBuffs retorna todos os buffs, incluindo hidden (além do count)
func (inj *Injector) GetAllBuffs(maxSlots int) []BuffInfo {
	if inj.buffListAddr == 0 {
		return nil
	}

	count := inj.GetBuffCount()
	var buffs []BuffInfo

	for i := 0; i < maxSlots; i++ {
		addr := inj.GetBuffAddress(i)

		id := inj.readU32(addr + BUFF_OFF_ID)

		// Pular slots vazios
		if id == 0 || id > 99999999 {
			continue
		}

		info := BuffInfo{
			Index:    i,
			Slot:     inj.readU32(addr + BUFF_OFF_SLOT),
			ID:       id,
			Duration: inj.readU32(addr + BUFF_OFF_DURATION),
			Elapsed:  inj.readU32(addr + BUFF_OFF_ELAPSED),
			Stack:    inj.readU32(addr + BUFF_OFF_STACK),
			Flags:    inj.readU32(addr + 0x2C),
			IsHidden: uint32(i) >= count,
		}

		// Verificar se é injetado
		inj.mutex.RLock()
		_, info.IsInjected = inj.injectedBuffs[id]
		inj.mutex.RUnlock()

		buffs = append(buffs, info)
	}

	return buffs
}

// InjectAsHidden injeta um buff que não aparece na UI do jogo
// Escreve o buff APÓS o count atual, sem incrementar o count
func (inj *Injector) InjectAsHidden(sourceIndex int, newBuffID uint32, permanent bool) bool {
	if inj.buffListAddr == 0 {
		fmt.Println("[INJECT-HIDDEN] Buff list not found")
		return false
	}

	count := inj.GetBuffCount()
	if count >= 30 {
		fmt.Println("[INJECT-HIDDEN] Buff list full")
		return false
	}

	// Ler estrutura do buff fonte
	sourceAddr := inj.GetBuffAddress(sourceIndex)
	buffData := inj.readBytes(sourceAddr, BUFF_SIZE)
	if buffData == nil {
		fmt.Println("[INJECT-HIDDEN] Failed to read source buff")
		return false
	}

	sourceID := *(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ID]))

	// Modificar dados - usar count como slot (além do count visível)
	*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_SLOT])) = count
	*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ID])) = newBuffID

	if permanent {
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_DURATION])) = 0
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ELAPSED])) = 0
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_TICK])) = 0
	} else {
		*(*uint32)(unsafe.Pointer(&buffData[BUFF_OFF_ELAPSED])) = 0
	}

	// Escrever no slot APÓS o count atual (hidden)
	newAddr := inj.GetBuffAddress(int(count))
	if !inj.writeBytes(newAddr, buffData) {
		fmt.Println("[INJECT-HIDDEN] Failed to write buff")
		return false
	}

	// NÃO incrementar count - isso faz o buff ser "hidden"
	// O buff existe na memória mas o jogo não vê

	// Registrar buff injetado
	inj.mutex.Lock()
	inj.injectedBuffs[newBuffID] = &InjectedBuff{
		ID:        newBuffID,
		SourceID:  sourceID,
		Permanent: permanent,
		Index:     int(count),
	}
	inj.mutex.Unlock()

	fmt.Printf("[INJECT-HIDDEN] Injected hidden buff %d -> %d at slot %d (beyond count, permanent: %v)\n",
		sourceID, newBuffID, count, permanent)
	return true
}

// InjectFirstAsHidden clona o primeiro buff e injeta como hidden
func (inj *Injector) InjectFirstAsHidden(newBuffID uint32, permanent bool) bool {
	if inj.buffListAddr == 0 {
		fmt.Println("[INJECT-HIDDEN] Buff list address not set!")
		return false
	}

	count := inj.GetBuffCount()
	fmt.Printf("[INJECT-HIDDEN] BuffListAddr: 0x%X, Count: %d\n", inj.buffListAddr, count)

	if count == 0 {
		fmt.Println("[INJECT-HIDDEN] No buffs to clone - use a skill that gives a buff first!")
		return false
	}

	for i := 0; i < int(count) && i < 10; i++ {
		buffID := inj.GetBuffID(i)
		fmt.Printf("[INJECT-HIDDEN] Slot %d: BuffID=%d\n", i, buffID)

		if buffID == 0 || buffID > 9999999 {
			fmt.Printf("[INJECT-HIDDEN] Slot %d has invalid buffID, skipping\n", i)
			continue
		}

		if inj.InjectAsHidden(i, newBuffID, permanent) {
			return true
		}
	}

	fmt.Println("[INJECT-HIDDEN] Could not find valid source buff")
	return false
}

// FindBuffByIDExtended procura um buff pelo ID, incluindo além do count
func (inj *Injector) FindBuffByIDExtended(buffID uint32, maxSlots int) int {
	for i := 0; i < maxSlots; i++ {
		if inj.GetBuffID(i) == buffID {
			return i
		}
	}
	return -1
}

// RemoveBuffExtended remove um buff, mesmo se estiver além do count
func (inj *Injector) RemoveBuffExtended(buffID uint32) bool {
	idx := inj.FindBuffByIDExtended(buffID, 50)
	if idx < 0 {
		return false
	}

	count := inj.GetBuffCount()

	// Se está dentro do count, usar lógica normal
	if uint32(idx) < count {
		return inj.RemoveBuff(buffID)
	}

	// Se está além do count (hidden), apenas zerar o ID
	addr := inj.GetBuffAddress(idx)
	inj.writeU32(addr+BUFF_OFF_ID, 0)

	// Remover do registro
	inj.mutex.Lock()
	delete(inj.injectedBuffs, buffID)
	inj.mutex.Unlock()

	fmt.Printf("[INJECT] Removed hidden buff %d at index %d\n", buffID, idx)
	return true
}

// ClearAllInjected remove todos os buffs injetados
func (inj *Injector) ClearAllInjected() int {
	inj.mutex.Lock()
	buffIDs := make([]uint32, 0, len(inj.injectedBuffs))
	for id := range inj.injectedBuffs {
		buffIDs = append(buffIDs, id)
	}
	inj.mutex.Unlock()

	removed := 0
	for _, id := range buffIDs {
		if inj.RemoveBuffExtended(id) {
			removed++
		}
	}

	fmt.Printf("[INJECT] Cleared %d injected buffs\n", removed)
	return removed
}
