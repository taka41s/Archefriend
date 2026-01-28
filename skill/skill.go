package skill

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	PAGE_EXECUTE_READWRITE = 0x40
	MEM_RELEASE            = 0x8000
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procWriteProcessMemory = kernel32.NewProc("WriteProcessMemory")
	procReadProcessMemory  = kernel32.NewProc("ReadProcessMemory")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
)

// SkillCooldown representa o cooldown de uma skill
type SkillCooldown struct {
	SkillID  uint32
	Name     string
	Duration time.Duration
	LastUsed time.Time
}

// SkillEvent representa um evento de skill para a UI
type SkillEvent struct {
	Time     time.Time
	SkillID  uint32
	Name     string
	Type     string // "CAST", "READY"
}

// SkillMonitor detecta quando skills são castadas com sucesso
type SkillMonitor struct {
	handle     windows.Handle
	x2gameBase uintptr
	hookAddr   uintptr // Endereço da instrução a hookar (x2game + offset)
	caveAddr   uintptr // Endereço da code cave alocada
	origBytes  []byte  // Bytes originais (8 bytes)

	// Dados compartilhados na code cave
	flagAddr    uintptr // Endereço da flag (skill foi usada)
	skillIDAddr uintptr // Endereço onde guardamos o ponteiro do skill ID

	Enabled bool
	Hooked  bool

	// Última skill detectada
	LastSkillID  uint32
	LastCastTime time.Time
	CastCount    uint32

	// Cooldown tracking
	Cooldowns map[uint32]*SkillCooldown

	// Config
	Config *SkillsConfig

	// Eventos para UI
	Events    []SkillEvent
	MaxEvents int

	// Callbacks
	OnSkillCast func(skillID uint32)
	OnSkillTry  func(skillID uint32) // Chamado quando tenta usar skill (antes de executar)

	// Skill Try Hook
	tryHookCaveAddr uintptr
	tryHookFlagAddr uintptr
	tryHookEDIAddr  uintptr
	tryHookEDXAddr  uintptr
	tryHookECXAddr  uintptr
	tryHookEBPAddr  uintptr
	tryHookESPAddr  uintptr

	// Cached values
	cachedSkillManager uintptr

	// Pending skill execution
	pendingSkillStruct uintptr
	pendingSkillID     uint32

	// Exec hook fields
	execCaveAddr      uintptr
	execFlagAddr      uintptr
	execSkillAddr     uintptr
	execThisAddr      uintptr
	execFuncAddr      uintptr
	execLocalBuf      uintptr
	execHookInstalled bool

	mu sync.Mutex
}

// GetCooldownDuration retorna a duração total do cooldown em segundos
func (sm *SkillMonitor) GetCooldownDuration(skillID uint32) float64 {
	cd, exists := sm.Cooldowns[skillID]
	if !exists {
		return 0
	}
	return cd.Duration.Seconds()
}

// NewSkillMonitor cria um novo monitor de skills
func NewSkillMonitor(handle windows.Handle, x2gameBase uintptr, offset uintptr) *SkillMonitor {
	sm := &SkillMonitor{
		handle:     handle,
		x2gameBase: x2gameBase,
		hookAddr:   x2gameBase + offset,
		origBytes:  make([]byte, 8),
		Enabled:    false,
		Hooked:     false,
		Cooldowns:  make(map[uint32]*SkillCooldown),
		Events:     make([]SkillEvent, 0),
		MaxEvents:  20,
	}

	return sm
}

// LoadConfig carrega configurações de skills do JSON
func (sm *SkillMonitor) LoadConfig(filepath string) error {
	config, err := LoadSkillsConfig(filepath)
	if err != nil {
		return err
	}
	sm.Config = config

	// Inicializar cooldowns do config
	for _, skill := range config.GetTrackedSkills() {
		sm.Cooldowns[skill.ID] = &SkillCooldown{
			SkillID:  skill.ID,
			Name:     skill.Name,
			Duration: time.Duration(skill.CooldownMS) * time.Millisecond,
			LastUsed: time.Time{},
		}
	}

	return nil
}

// AddEvent adiciona um evento à lista
func (sm *SkillMonitor) AddEvent(eventType string, skillID uint32, name string) {
	event := SkillEvent{
		Time:    time.Now(),
		SkillID: skillID,
		Name:    name,
		Type:    eventType,
	}

	sm.Events = append(sm.Events, event)

	// Limitar tamanho
	if len(sm.Events) > sm.MaxEvents {
		sm.Events = sm.Events[len(sm.Events)-sm.MaxEvents:]
	}
}

// GetSkillName retorna o nome de uma skill
func (sm *SkillMonitor) GetSkillName(skillID uint32) string {
	if cd, exists := sm.Cooldowns[skillID]; exists && cd.Name != "" {
		return cd.Name
	}
	if sm.Config != nil {
		return sm.Config.GetSkillName(skillID)
	}
	return fmt.Sprintf("Skill#%d", skillID)
}

// InstallHook instala o hook na instrução alvo
func (sm *SkillMonitor) InstallHook() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.Hooked {
		return fmt.Errorf("hook já instalado")
	}

	// 1. Alocar memória para a code cave
	caveSize := 128
	caveAddr, _, err := procVirtualAllocEx.Call(
		uintptr(sm.handle),
		0,
		uintptr(caveSize),
		MEM_COMMIT|MEM_RESERVE,
		PAGE_EXECUTE_READWRITE,
	)
	if caveAddr == 0 {
		return fmt.Errorf("falha ao alocar code cave: %v", err)
	}
	sm.caveAddr = caveAddr

	// Layout da cave
	sm.flagAddr = caveAddr + 0x50
	sm.skillIDAddr = caveAddr + 0x54

	// 2. Ler bytes originais
	origBytes := make([]byte, 8)
	var bytesRead uintptr
	ret, _, _ := procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.hookAddr,
		uintptr(unsafe.Pointer(&origBytes[0])),
		8,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		return fmt.Errorf("falha ao ler bytes originais")
	}
	sm.origBytes = origBytes

	fmt.Printf("[SKILL] Bytes originais em %08X: %02X %02X %02X %02X %02X %02X %02X %02X\n",
		sm.hookAddr, origBytes[0], origBytes[1], origBytes[2], origBytes[3],
		origBytes[4], origBytes[5], origBytes[6], origBytes[7])

	// 3. Construir shellcode
	returnAddr := sm.hookAddr + 8

	shellcode := []byte{
		0x60, // pushad
		0x9C, // pushfd
		// mov dword ptr [flagAddr], 1
		0xC7, 0x05,
		byte(sm.flagAddr), byte(sm.flagAddr >> 8), byte(sm.flagAddr >> 16), byte(sm.flagAddr >> 24),
		0x01, 0x00, 0x00, 0x00,
		// mov [skillIDAddr], ebx
		0x89, 0x1D,
		byte(sm.skillIDAddr), byte(sm.skillIDAddr >> 8), byte(sm.skillIDAddr >> 16), byte(sm.skillIDAddr >> 24),
		0x9D, // popfd
		0x61, // popad
		// Instruções originais (8 bytes)
		origBytes[0], origBytes[1], origBytes[2], origBytes[3],
		origBytes[4], origBytes[5], origBytes[6], origBytes[7],
		// jmp returnAddr
		0xE9, 0x00, 0x00, 0x00, 0x00,
	}

	// Calcular offset do JMP
	jmpLocation := caveAddr + uintptr(len(shellcode)) - 4
	jmpOffset := int32(returnAddr) - int32(jmpLocation) - 4
	shellcode[len(shellcode)-4] = byte(jmpOffset)
	shellcode[len(shellcode)-3] = byte(jmpOffset >> 8)
	shellcode[len(shellcode)-2] = byte(jmpOffset >> 16)
	shellcode[len(shellcode)-1] = byte(jmpOffset >> 24)

	// 4. Escrever shellcode na cave
	var bytesWritten uintptr
	ret, _, _ = procWriteProcessMemory.Call(
		uintptr(sm.handle),
		caveAddr,
		uintptr(unsafe.Pointer(&shellcode[0])),
		uintptr(len(shellcode)),
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		return fmt.Errorf("falha ao escrever shellcode")
	}

	// 5. Inicializar flag
	zeroData := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.flagAddr,
		uintptr(unsafe.Pointer(&zeroData[0])),
		8,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)

	// 6. Escrever JMP para a cave
	jmpToCave := make([]byte, 8)
	jmpToCave[0] = 0xE9
	jmpOffsetToCave := int32(caveAddr) - int32(sm.hookAddr) - 5
	jmpToCave[1] = byte(jmpOffsetToCave)
	jmpToCave[2] = byte(jmpOffsetToCave >> 8)
	jmpToCave[3] = byte(jmpOffsetToCave >> 16)
	jmpToCave[4] = byte(jmpOffsetToCave >> 24)
	jmpToCave[5] = 0x90
	jmpToCave[6] = 0x90
	jmpToCave[7] = 0x90

	ret, _, _ = procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.hookAddr,
		uintptr(unsafe.Pointer(&jmpToCave[0])),
		8,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		return fmt.Errorf("falha ao escrever JMP")
	}

	sm.Hooked = true
	sm.Enabled = true
	fmt.Printf("[SKILL] Hook instalado em %08X -> cave em %08X\n", sm.hookAddr, caveAddr)

	return nil
}

// RemoveHook remove o hook e restaura os bytes originais
func (sm *SkillMonitor) RemoveHook() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.Hooked {
		return nil
	}

	var bytesWritten uintptr
	ret, _, _ := procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.hookAddr,
		uintptr(unsafe.Pointer(&sm.origBytes[0])),
		8,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if ret == 0 {
		return fmt.Errorf("falha ao restaurar bytes originais")
	}

	if sm.caveAddr != 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), sm.caveAddr, 0, MEM_RELEASE)
		sm.caveAddr = 0
	}

	sm.Hooked = false
	sm.Enabled = false
	fmt.Printf("[SKILL] Hook removido de %08X\n", sm.hookAddr)

	return nil
}

// CheckSkillCast verifica se uma skill foi castada e retorna o ID
func (sm *SkillMonitor) CheckSkillCast() (bool, uint32) {
	if !sm.Hooked || !sm.Enabled {
		return false, 0
	}

	var flag uint32
	var bytesRead uintptr
	ret, _, _ := procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.flagAddr,
		uintptr(unsafe.Pointer(&flag)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if ret == 0 || flag == 0 {
		return false, 0
	}

	var skillPtrAddr uint32
	procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.skillIDAddr,
		uintptr(unsafe.Pointer(&skillPtrAddr)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	var skillID uint32
	if skillPtrAddr != 0 {
		procReadProcessMemory.Call(
			uintptr(sm.handle),
			uintptr(skillPtrAddr),
			uintptr(unsafe.Pointer(&skillID)),
			4,
			uintptr(unsafe.Pointer(&bytesRead)),
		)
	}

	var zero uint32 = 0
	var bytesWritten uintptr
	procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.flagAddr,
		uintptr(unsafe.Pointer(&zero)),
		4,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)

	sm.LastSkillID = skillID
	sm.LastCastTime = time.Now()
	sm.CastCount++

	if cd, exists := sm.Cooldowns[skillID]; exists {
		cd.LastUsed = time.Now()
		fmt.Printf("[SKILL] %s usado! (CD: %.1fs)\n", cd.Name, cd.Duration.Seconds())
		sm.AddEvent("CAST", skillID, cd.Name)
	} else {
		fmt.Printf("[SKILL] Skill %d usada\n", skillID)
		sm.AddEvent("CAST", skillID, sm.GetSkillName(skillID))
	}

	if sm.OnSkillCast != nil {
		sm.OnSkillCast(skillID)
	}

	return true, skillID
}

// Update deve ser chamado a cada frame para verificar casts e tentativas
func (sm *SkillMonitor) Update() {
	sm.CheckSkillTry()
	sm.CheckSkillCast()
}

// Toggle liga/desliga o hook
func (sm *SkillMonitor) Toggle() error {
	if sm.Hooked {
		return sm.RemoveHook()
	}
	return sm.InstallHook()
}

// IsSkillReady verifica se uma skill está fora de cooldown
func (sm *SkillMonitor) IsSkillReady(skillID uint32) bool {
	cd, exists := sm.Cooldowns[skillID]
	if !exists {
		return true
	}
	if cd.LastUsed.IsZero() {
		return true
	}
	return time.Since(cd.LastUsed) >= cd.Duration
}

// GetCooldownRemaining retorna o tempo restante de cooldown
func (sm *SkillMonitor) GetCooldownRemaining(skillID uint32) float64 {
	cd, exists := sm.Cooldowns[skillID]
	if !exists {
		return 0
	}
	if cd.LastUsed.IsZero() {
		return 0
	}
	elapsed := time.Since(cd.LastUsed)
	remaining := cd.Duration - elapsed
	if remaining <= 0 {
		return 0
	}
	return remaining.Seconds()
}

// RegisterCooldown adiciona ou atualiza um cooldown de skill
func (sm *SkillMonitor) RegisterCooldown(skillID uint32, name string, duration time.Duration) {
	sm.Cooldowns[skillID] = &SkillCooldown{
		SkillID:  skillID,
		Name:     name,
		Duration: duration,
		LastUsed: time.Time{},
	}
}

// GetSkillStatus retorna o status da skill
func (sm *SkillMonitor) GetSkillStatus(skillID uint32) string {
	if sm.IsSkillReady(skillID) {
		return "READY"
	}
	remaining := sm.GetCooldownRemaining(skillID)
	return fmt.Sprintf("%.1fs", remaining)
}

// InstallTryHook instala hook para detectar tentativa de uso de skill
func (sm *SkillMonitor) InstallTryHook(offset uintptr) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.tryHookCaveAddr != 0 {
		return fmt.Errorf("try hook já instalado")
	}

	tryHookAddr := sm.x2gameBase + offset

	// 1. Alocar memória para a code cave
	caveSize := 128
	caveAddr, _, err := procVirtualAllocEx.Call(
		uintptr(sm.handle),
		0,
		uintptr(caveSize),
		MEM_COMMIT|MEM_RESERVE,
		PAGE_EXECUTE_READWRITE,
	)
	if caveAddr == 0 {
		return fmt.Errorf("falha ao alocar try hook cave: %v", err)
	}
	sm.tryHookCaveAddr = caveAddr

	// Layout da cave
	sm.tryHookFlagAddr = caveAddr + 0x50
	sm.tryHookEDIAddr = caveAddr + 0x54
	sm.tryHookEDXAddr = caveAddr + 0x58
	sm.tryHookECXAddr = caveAddr + 0x5C
	sm.tryHookEBPAddr = caveAddr + 0x60
	sm.tryHookESPAddr = caveAddr + 0x64

	// 2. Ler bytes originais (8 bytes)
	origBytes := make([]byte, 8)
	var bytesRead uintptr
	ret, _, _ := procReadProcessMemory.Call(
		uintptr(sm.handle),
		tryHookAddr,
		uintptr(unsafe.Pointer(&origBytes[0])),
		8,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		sm.tryHookCaveAddr = 0
		return fmt.Errorf("falha ao ler bytes originais do try hook")
	}

	fmt.Printf("[SKILL-TRY] Bytes originais em %08X: %02X %02X %02X %02X %02X %02X %02X %02X\n",
		tryHookAddr, origBytes[0], origBytes[1], origBytes[2], origBytes[3],
		origBytes[4], origBytes[5], origBytes[6], origBytes[7])

	// 3. Construir shellcode
	returnAddr := tryHookAddr + 8

	shellcode := []byte{
		0x60, // pushad
		0x9C, // pushfd
		// mov dword ptr [flagAddr], 1
		0xC7, 0x05,
		byte(sm.tryHookFlagAddr), byte(sm.tryHookFlagAddr >> 8), byte(sm.tryHookFlagAddr >> 16), byte(sm.tryHookFlagAddr >> 24),
		0x01, 0x00, 0x00, 0x00,
		// mov [ediAddr], edi - salva EDI (pode conter skill info)
		0x89, 0x3D,
		byte(sm.tryHookEDIAddr), byte(sm.tryHookEDIAddr >> 8), byte(sm.tryHookEDIAddr >> 16), byte(sm.tryHookEDIAddr >> 24),
		// mov [edxAddr], edx
		0x89, 0x15,
		byte(sm.tryHookEDXAddr), byte(sm.tryHookEDXAddr >> 8), byte(sm.tryHookEDXAddr >> 16), byte(sm.tryHookEDXAddr >> 24),
		// mov [ecxAddr], ecx
		0x89, 0x0D,
		byte(sm.tryHookECXAddr), byte(sm.tryHookECXAddr >> 8), byte(sm.tryHookECXAddr >> 16), byte(sm.tryHookECXAddr >> 24),
		0x9D, // popfd
		0x61, // popad
		// Instruções originais (8 bytes)
		origBytes[0], origBytes[1], origBytes[2], origBytes[3],
		origBytes[4], origBytes[5], origBytes[6], origBytes[7],
		// jmp returnAddr
		0xE9, 0x00, 0x00, 0x00, 0x00,
	}

	// Calcular offset do JMP
	jmpLocation := caveAddr + uintptr(len(shellcode)) - 4
	jmpOffset := int32(returnAddr) - int32(jmpLocation) - 4
	shellcode[len(shellcode)-4] = byte(jmpOffset)
	shellcode[len(shellcode)-3] = byte(jmpOffset >> 8)
	shellcode[len(shellcode)-2] = byte(jmpOffset >> 16)
	shellcode[len(shellcode)-1] = byte(jmpOffset >> 24)

	// 4. Escrever shellcode na cave
	var bytesWritten uintptr
	ret, _, _ = procWriteProcessMemory.Call(
		uintptr(sm.handle),
		caveAddr,
		uintptr(unsafe.Pointer(&shellcode[0])),
		uintptr(len(shellcode)),
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		sm.tryHookCaveAddr = 0
		return fmt.Errorf("falha ao escrever try hook shellcode")
	}

	// 5. Inicializar flag
	zeroData := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookFlagAddr,
		uintptr(unsafe.Pointer(&zeroData[0])),
		uintptr(len(zeroData)),
		uintptr(unsafe.Pointer(&bytesWritten)),
	)

	// 6. Escrever JMP para a cave
	jmpToCave := make([]byte, 8)
	jmpToCave[0] = 0xE9
	jmpOffsetToCave := int32(caveAddr) - int32(tryHookAddr) - 5
	jmpToCave[1] = byte(jmpOffsetToCave)
	jmpToCave[2] = byte(jmpOffsetToCave >> 8)
	jmpToCave[3] = byte(jmpOffsetToCave >> 16)
	jmpToCave[4] = byte(jmpOffsetToCave >> 24)
	jmpToCave[5] = 0x90
	jmpToCave[6] = 0x90
	jmpToCave[7] = 0x90

	ret, _, _ = procWriteProcessMemory.Call(
		uintptr(sm.handle),
		tryHookAddr,
		uintptr(unsafe.Pointer(&jmpToCave[0])),
		8,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if ret == 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), caveAddr, 0, MEM_RELEASE)
		sm.tryHookCaveAddr = 0
		return fmt.Errorf("falha ao escrever JMP do try hook")
	}

	fmt.Printf("[SKILL-TRY] Hook instalado em %08X -> cave em %08X\n", tryHookAddr, caveAddr)
	return nil
}

// CheckSkillTry verifica se houve tentativa de uso de skill
func (sm *SkillMonitor) CheckSkillTry() (bool, uint32) {
	if sm.tryHookCaveAddr == 0 {
		return false, 0
	}

	var flag uint32
	var bytesRead uintptr
	ret, _, _ := procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookFlagAddr,
		uintptr(unsafe.Pointer(&flag)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if ret == 0 || flag == 0 {
		return false, 0
	}

	// Ler EDI (pode conter skill struct ou ID)
	var edi uint32
	procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookEDIAddr,
		uintptr(unsafe.Pointer(&edi)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	// Ler ECX (this pointer do SkillManager)
	var ecx uint32
	procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookECXAddr,
		uintptr(unsafe.Pointer(&ecx)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)

	// Tentar extrair skill ID do EDI (pode ser ponteiro ou ID direto)
	var skillID uint32
	if edi > 0x10000 {
		// Provavelmente é um ponteiro, tentar ler o ID
		procReadProcessMemory.Call(
			uintptr(sm.handle),
			uintptr(edi),
			uintptr(unsafe.Pointer(&skillID)),
			4,
			uintptr(unsafe.Pointer(&bytesRead)),
		)
	} else {
		skillID = edi
	}

	// Resetar flag
	var zero uint32 = 0
	var bytesWritten uintptr
	procWriteProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookFlagAddr,
		uintptr(unsafe.Pointer(&zero)),
		4,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)

	fmt.Printf("[SKILL-TRY] Tentativa detectada! EDI=%08X ECX=%08X SkillID=%d\n", edi, ecx, skillID)

	if sm.OnSkillTry != nil && skillID != 0 {
		sm.OnSkillTry(skillID)
	}

	return true, skillID
}

// Close limpa recursos
func (sm *SkillMonitor) Close() {
	sm.RemoveHook()
	if sm.tryHookCaveAddr != 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), sm.tryHookCaveAddr, 0, MEM_RELEASE)
	}
	if sm.execCaveAddr != 0 {
		procVirtualFreeEx.Call(uintptr(sm.handle), sm.execCaveAddr, 0, MEM_RELEASE)
	}
}

// GetSkillManagerAddr retorna o endereço do SkillManager capturado
func (sm *SkillMonitor) GetSkillManagerAddr() uintptr {
	if sm.cachedSkillManager != 0 {
		return sm.cachedSkillManager
	}
	if sm.tryHookECXAddr == 0 {
		return 0
	}
	var ecx uint32
	var bytesRead uintptr
	procReadProcessMemory.Call(
		uintptr(sm.handle),
		sm.tryHookECXAddr,
		uintptr(unsafe.Pointer(&ecx)),
		4,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if ecx != 0 {
		sm.cachedSkillManager = uintptr(ecx)
	}
	return uintptr(ecx)
}

// GetTrackedSkills retorna lista de skills sendo monitoradas
func (sm *SkillMonitor) GetTrackedSkills() []*SkillCooldown {
	skills := make([]*SkillCooldown, 0, len(sm.Cooldowns))
	for _, cd := range sm.Cooldowns {
		skills = append(skills, cd)
	}
	return skills
}

// GetRecentEvents retorna os eventos recentes
func (sm *SkillMonitor) GetRecentEvents(count int) []SkillEvent {
	if count >= len(sm.Events) {
		return sm.Events
	}
	return sm.Events[len(sm.Events)-count:]
}
