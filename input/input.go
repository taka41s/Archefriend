package input

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procSendInput   = user32.NewProc("SendInput")
	procBeep        = syscall.NewLazyDLL("kernel32.dll").NewProc("Beep")
)

const (
	INPUT_KEYBOARD    = 1
	KEYEVENTF_KEYUP   = 0x0002
	KEYEVENTF_UNICODE = 0x0004
)

// Virtual Key Codes - Modificadores
const (
	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_ALT     = 0x12
	VK_LSHIFT  = 0xA0
	VK_RSHIFT  = 0xA1
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_LMENU   = 0xA4 // Left ALT
	VK_RMENU   = 0xA5 // Right ALT
)

// Virtual Key Codes - Letras
const (
	VK_A = 0x41
	VK_B = 0x42
	VK_C = 0x43
	VK_D = 0x44
	VK_E = 0x45
	VK_F = 0x46
	VK_G = 0x47
	VK_H = 0x48
	VK_I = 0x49
	VK_J = 0x4A
	VK_K = 0x4B
	VK_L = 0x4C
	VK_M = 0x4D
	VK_N = 0x4E
	VK_O = 0x4F
	VK_P = 0x50
	VK_Q = 0x51
	VK_R = 0x52
	VK_S = 0x53
	VK_T = 0x54
	VK_U = 0x55
	VK_V = 0x56
	VK_W = 0x57
	VK_X = 0x58
	VK_Y = 0x59
	VK_Z = 0x5A
)

// Virtual Key Codes - Números
const (
	VK_0 = 0x30
	VK_1 = 0x31
	VK_2 = 0x32
	VK_3 = 0x33
	VK_4 = 0x34
	VK_5 = 0x35
	VK_6 = 0x36
	VK_7 = 0x37
	VK_8 = 0x38
	VK_9 = 0x39
)

// Virtual Key Codes - Especiais
const (
	VK_SPACE     = 0x20
	VK_ESCAPE    = 0x1B
	VK_RETURN    = 0x0D
	VK_TAB       = 0x09
	VK_BACK      = 0x08
	VK_DELETE    = 0x2E
	VK_INSERT    = 0x2D
	VK_HOME      = 0x24
	VK_END       = 0x23
	VK_PAGEUP    = 0x21
	VK_PAGEDOWN  = 0x22
)

// Virtual Key Codes - F-Keys
const (
	VK_F1  = 0x70
	VK_F2  = 0x71
	VK_F3  = 0x72
	VK_F4  = 0x73
	VK_F5  = 0x74
	VK_F6  = 0x75
	VK_F7  = 0x76
	VK_F8  = 0x77
	VK_F9  = 0x78
	VK_F10 = 0x79
	VK_F11 = 0x7A
	VK_F12 = 0x7B
)

// KEYBDINPUT representa uma entrada de teclado
type KEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// INPUT representa uma estrutura de input genérica
type INPUT struct {
	Type uint32
	Ki   KEYBDINPUT
	_    [8]byte // padding para união
}

// SendKey envia um pressionamento de tecla (down + up)
func SendKey(vk uint16) error {
	// Key down
	inputDown := INPUT{
		Type: INPUT_KEYBOARD,
		Ki: KEYBDINPUT{
			Vk:    vk,
			Flags: 0,
		},
	}

	// Key up
	inputUp := INPUT{
		Type: INPUT_KEYBOARD,
		Ki: KEYBDINPUT{
			Vk:    vk,
			Flags: KEYEVENTF_KEYUP,
		},
	}

	// Envia key down
	ret, _, _ := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&inputDown)),
		unsafe.Sizeof(inputDown),
	)
	if ret == 0 {
		return fmt.Errorf("falha ao enviar key down")
	}

	// Pequeno delay
	time.Sleep(50 * time.Millisecond)

	// Envia key up
	ret, _, _ = procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&inputUp)),
		unsafe.Sizeof(inputUp),
	)
	if ret == 0 {
		return fmt.Errorf("falha ao enviar key up")
	}

	return nil
}

// SendKeyMultiple envia uma tecla múltiplas vezes
func SendKeyMultiple(vk uint16, count int, interval time.Duration) error {
	for i := 0; i < count; i++ {
		if err := SendKey(vk); err != nil {
			return err
		}
		if i < count-1 {
			time.Sleep(interval)
		}
	}
	return nil
}

// SendKeyCombo envia uma combinação de teclas (ex: ALT+E)
// Os modificadores devem vir primeiro: [VK_ALT, VK_E]
func SendKeyCombo(keys []uint16) error {
	if len(keys) == 0 {
		return nil
	}

	// Press all keys down
	for _, vk := range keys {
		input := INPUT{
			Type: INPUT_KEYBOARD,
			Ki: KEYBDINPUT{
				Vk:    vk,
				Flags: 0,
			},
		}
		ret, _, _ := procSendInput.Call(
			1,
			uintptr(unsafe.Pointer(&input)),
			unsafe.Sizeof(input),
		)
		if ret == 0 {
			return fmt.Errorf("falha ao enviar key down para vk %d", vk)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Release all keys up (reverse order)
	for i := len(keys) - 1; i >= 0; i-- {
		input := INPUT{
			Type: INPUT_KEYBOARD,
			Ki: KEYBDINPUT{
				Vk:    keys[i],
				Flags: KEYEVENTF_KEYUP,
			},
		}
		ret, _, _ := procSendInput.Call(
			1,
			uintptr(unsafe.Pointer(&input)),
			unsafe.Sizeof(input),
		)
		if ret == 0 {
			return fmt.Errorf("falha ao enviar key up para vk %d", keys[i])
		}
		time.Sleep(20 * time.Millisecond)
	}

	return nil
}

// SendKeySequence envia uma sequência de combos de teclas
// Ex: [[VK_ALT, VK_E], [VK_CONTROL, VK_Q]] -> pressiona ALT+E, depois CTRL+Q
func SendKeySequence(combos [][]uint16) error {
	for i, combo := range combos {
		if err := SendKeyCombo(combo); err != nil {
			return fmt.Errorf("falha no combo %d: %v", i, err)
		}
		// Delay entre combos
		if i < len(combos)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

// Beep emite um som
func Beep(frequency, duration uint32) {
	procBeep.Call(uintptr(frequency), uintptr(duration))
}

// ParseKeyString converte uma string de tecla para VK codes
// Exemplos: "F12" -> [VK_F12], "LSHIFT+4" -> [VK_LSHIFT, VK_4], "LALT+2" -> [VK_LALT, VK_2]
func ParseKeyString(keyStr string) ([]uint16, error) {
	if keyStr == "" {
		return nil, fmt.Errorf("key string vazia")
	}

	// Map de strings para VK codes
	keyMap := map[string]uint16{
		// Modificadores
		"LSHIFT":   VK_LSHIFT,
		"RSHIFT":   VK_RSHIFT,
		"SHIFT":    VK_SHIFT,
		"LCTRL":    VK_LCONTROL,
		"RCTRL":    VK_RCONTROL,
		"CTRL":     VK_CONTROL,
		"LALT":     VK_LMENU,
		"RALT":     VK_RMENU,
		"ALT":      VK_ALT,
		"LCONTROL": VK_LCONTROL,
		"RCONTROL": VK_RCONTROL,
		"CONTROL":  VK_CONTROL,

		// F-Keys
		"F1":  VK_F1,
		"F2":  VK_F2,
		"F3":  VK_F3,
		"F4":  VK_F4,
		"F5":  VK_F5,
		"F6":  VK_F6,
		"F7":  VK_F7,
		"F8":  VK_F8,
		"F9":  VK_F9,
		"F10": VK_F10,
		"F11": VK_F11,
		"F12": VK_F12,

		// Números
		"0": VK_0,
		"1": VK_1,
		"2": VK_2,
		"3": VK_3,
		"4": VK_4,
		"5": VK_5,
		"6": VK_6,
		"7": VK_7,
		"8": VK_8,
		"9": VK_9,

		// Letras
		"A": VK_A,
		"B": VK_B,
		"C": VK_C,
		"D": VK_D,
		"E": VK_E,
		"F": VK_F,
		"G": VK_G,
		"H": VK_H,
		"I": VK_I,
		"J": VK_J,
		"K": VK_K,
		"L": VK_L,
		"M": VK_M,
		"N": VK_N,
		"O": VK_O,
		"P": VK_P,
		"Q": VK_Q,
		"R": VK_R,
		"S": VK_S,
		"T": VK_T,
		"U": VK_U,
		"V": VK_V,
		"W": VK_W,
		"X": VK_X,
		"Y": VK_Y,
		"Z": VK_Z,

		// Especiais
		"SPACE":  VK_SPACE,
		"ESC":    VK_ESCAPE,
		"ESCAPE": VK_ESCAPE,
		"ENTER":  VK_RETURN,
		"RETURN": VK_RETURN,
		"TAB":    VK_TAB,
		"BACK":   VK_BACK,
		"DELETE": VK_DELETE,
		"INSERT": VK_INSERT,
		"HOME":   VK_HOME,
		"END":    VK_END,
		"PAGEUP": VK_PAGEUP,
		"PAGEDOWN": VK_PAGEDOWN,
	}

	// Split por +
	parts := []string{}
	current := ""
	for _, ch := range keyStr {
		if ch == '+' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	// Converte cada parte para VK code
	vkCodes := make([]uint16, 0, len(parts))
	for _, part := range parts {
		vk, ok := keyMap[part]
		if !ok {
			return nil, fmt.Errorf("tecla desconhecida: %s", part)
		}
		vkCodes = append(vkCodes, vk)
	}

	return vkCodes, nil
}

// ParseKeySequence converte uma string de sequência de teclas para múltiplos combos
// Suporta múltiplas sequências separadas por & ou ,
// Exemplos:
//   "F12" -> [[VK_F12]]
//   "ALT+E" -> [[VK_ALT, VK_E]]
//   "ALT+Q & ALT+E" -> [[VK_ALT, VK_Q], [VK_ALT, VK_E]]
//   "F1, F2, F3" -> [[VK_F1], [VK_F2], [VK_F3]]
func ParseKeySequence(sequenceStr string) ([][]uint16, error) {
	if sequenceStr == "" {
		return nil, nil
	}

	// Split por & ou ,
	var sequences []string
	currentSeq := ""

	for i := 0; i < len(sequenceStr); i++ {
		ch := sequenceStr[i]

		// Check for separator (& or ,)
		if ch == '&' || ch == ',' {
			trimmed := strings.TrimSpace(currentSeq)
			if trimmed != "" {
				sequences = append(sequences, trimmed)
			}
			currentSeq = ""
		} else {
			currentSeq += string(ch)
		}
	}

	// Add last sequence
	trimmed := strings.TrimSpace(currentSeq)
	if trimmed != "" {
		sequences = append(sequences, trimmed)
	}

	// Se não encontrou nenhum separador, é uma sequência única
	if len(sequences) == 0 {
		sequences = []string{sequenceStr}
	}

	// Parse cada sequência individual
	result := make([][]uint16, 0, len(sequences))
	for _, seq := range sequences {
		keys, err := ParseKeyString(seq)
		if err != nil {
			return nil, fmt.Errorf("erro na sequência '%s': %v", seq, err)
		}
		result = append(result, keys)
	}

	return result, nil
}

// Manager gerencia o envio automático de teclas
type Manager struct {
	enabled      bool
	key          uint16
	interval     time.Duration
	stopChan     chan bool
	autoSpamming bool
}

// NewManager cria um novo input manager
func NewManager() *Manager {
	return &Manager{
		enabled:  false,
		key:      VK_V,
		interval: 100 * time.Millisecond,
		stopChan: make(chan bool),
	}
}

// SetKey define a tecla a ser enviada
func (m *Manager) SetKey(vk uint16) {
	m.key = vk
}

// SetInterval define o intervalo entre envios
func (m *Manager) SetInterval(interval time.Duration) {
	m.interval = interval
}

// SendSingle envia a tecla uma vez
func (m *Manager) SendSingle() error {
	fmt.Printf("[INPUT] Enviando tecla 0x%X\n", m.key)
	return SendKey(m.key)
}

// StartAutoSpam inicia o envio automático da tecla
func (m *Manager) StartAutoSpam() {
	if m.autoSpamming {
		return
	}

	m.autoSpamming = true
	fmt.Printf("[INPUT] Auto-spam iniciado - Tecla: 0x%X, Intervalo: %v\n", m.key, m.interval)

	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopChan:
				return
			case <-ticker.C:
				SendKey(m.key)
			}
		}
	}()
}

// StopAutoSpam para o envio automático
func (m *Manager) StopAutoSpam() {
	if !m.autoSpamming {
		return
	}

	m.autoSpamming = false
	m.stopChan <- true
	fmt.Println("[INPUT] Auto-spam parado")
}

// IsAutoSpamming retorna se está em auto-spam
func (m *Manager) IsAutoSpamming() bool {
	return m.autoSpamming
}

// ToggleAutoSpam liga/desliga o auto-spam
func (m *Manager) ToggleAutoSpam() {
	if m.autoSpamming {
		m.StopAutoSpam()
	} else {
		m.StartAutoSpam()
	}
}

// ================== KEY COMBO ==================

// KeyCombo representa uma combinação de teclas parseada
type KeyCombo struct {
	RawString  string
	Modifiers  []uint16
	MainKey    uint16
	AllKeys    []uint16
}

// ParseKeyCombo converte uma string como "SHIFT+F10" ou "CTRL+ALT+1" em KeyCombo
func ParseKeyCombo(keyStr string) KeyCombo {
	combo := KeyCombo{RawString: keyStr}

	if keyStr == "" {
		return combo
	}

	keys, err := ParseKeyString(strings.ToUpper(keyStr))
	if err != nil {
		fmt.Printf("[INPUT] Erro ao parsear combo '%s': %v\n", keyStr, err)
		return combo
	}

	if len(keys) == 0 {
		return combo
	}

	combo.AllKeys = keys

	// Separar modificadores da tecla principal
	for i, vk := range keys {
		if isModifier(vk) {
			combo.Modifiers = append(combo.Modifiers, vk)
		} else {
			// Última tecla não-modificadora é a principal
			if i == len(keys)-1 {
				combo.MainKey = vk
			}
		}
	}

	// Se não encontrou tecla principal, usar a última
	if combo.MainKey == 0 && len(keys) > 0 {
		combo.MainKey = keys[len(keys)-1]
	}

	return combo
}

// isModifier verifica se é uma tecla modificadora
func isModifier(vk uint16) bool {
	return vk == VK_SHIFT || vk == VK_CONTROL || vk == VK_ALT ||
		vk == VK_LSHIFT || vk == VK_RSHIFT ||
		vk == VK_LCONTROL || vk == VK_RCONTROL ||
		vk == VK_LMENU || vk == VK_RMENU
}

// SpamKey envia uma combinação de teclas múltiplas vezes
func SpamKey(keyStr string, count int, interval time.Duration) {
	combo := ParseKeyCombo(keyStr)
	if combo.MainKey == 0 {
		fmt.Printf("[INPUT] SpamKey: tecla inválida '%s'\n", keyStr)
		return
	}

	for i := 0; i < count; i++ {
		if len(combo.Modifiers) > 0 {
			// Com modificadores
			SendKeyCombo(combo.AllKeys)
		} else {
			// Tecla simples
			SendKey(combo.MainKey)
		}
		if i < count-1 {
			time.Sleep(interval)
		}
	}
}
