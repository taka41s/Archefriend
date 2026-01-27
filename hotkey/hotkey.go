package hotkey

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procPeekMessage      = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
)

const (
	WM_HOTKEY = 0x0312

	// Modifier keys
	MOD_ALT     = 0x0001
	MOD_CONTROL = 0x0002
	MOD_SHIFT   = 0x0004
	MOD_WIN     = 0x0008
	MOD_NOREPEAT = 0x4000
)

// Virtual key codes
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

type HotkeyCallback func()

type Manager struct {
	hwnd      uintptr
	callbacks map[int]HotkeyCallback
	mu        sync.RWMutex
	running   bool
	stopChan  chan bool
}

func NewManager(hwnd uintptr) *Manager {
	return &Manager{
		hwnd:      hwnd,
		callbacks: make(map[int]HotkeyCallback),
		stopChan:  make(chan bool),
	}
}

// Register registra uma hotkey global
func (m *Manager) Register(id int, modifiers uint32, vk uint32, callback HotkeyCallback) error {
	fmt.Printf("[HOTKEY] Registering ID:%d VK:0x%X (hwnd:0x%X)\n", id, vk, m.hwnd)

	ret, _, err := procRegisterHotKey.Call(
		m.hwnd,
		uintptr(id),
		uintptr(modifiers),
		uintptr(vk),
	)

	if ret == 0 {
		return fmt.Errorf("falha ao registrar hotkey ID:%d VK:0x%X: %v", id, vk, err)
	}

	m.mu.Lock()
	m.callbacks[id] = callback
	m.mu.Unlock()

	fmt.Printf("[HOTKEY] Registered ID:%d successfully\n", id)
	return nil
}

// Unregister remove uma hotkey
func (m *Manager) Unregister(id int) error {
	ret, _, err := procUnregisterHotKey.Call(
		m.hwnd,
		uintptr(id),
	)

	if ret == 0 {
		return fmt.Errorf("falha ao remover hotkey: %v", err)
	}

	m.mu.Lock()
	delete(m.callbacks, id)
	m.mu.Unlock()

	return nil
}

// Start inicia o loop de mensagens em uma goroutine
func (m *Manager) Start() {
	if m.running {
		return
	}

	m.running = true
	go m.messageLoop()
}

// Stop para o loop de mensagens
func (m *Manager) Stop() {
	if !m.running {
		return
	}

	m.running = false
	m.stopChan <- true
}

// messageLoop processa mensagens de hotkey
func (m *Manager) messageLoop() {
	type MSG struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      struct{ x, y int32 }
	}

	msg := &MSG{}
	const PM_REMOVE = 0x0001

	for m.running {
		select {
		case <-m.stopChan:
			return
		default:
			// PeekMessage non-blocking
			ret, _, _ := procPeekMessage.Call(
				uintptr(unsafe.Pointer(msg)),
				0,
				0,
				0,
				PM_REMOVE,
			)

			if ret != 0 {
				if msg.message == WM_HOTKEY {
					hotkeyID := int(msg.wParam)
					fmt.Printf("[HOTKEY] Received WM_HOTKEY ID:%d\n", hotkeyID)

					m.mu.RLock()
					callback, exists := m.callbacks[hotkeyID]
					m.mu.RUnlock()

					if exists && callback != nil {
						fmt.Printf("[HOTKEY] Executing callback for ID:%d\n", hotkeyID)
						go callback() // Execute em goroutine para não bloquear
					} else {
						fmt.Printf("[HOTKEY] No callback for ID:%d\n", hotkeyID)
					}
				}

				// Translate and dispatch
				procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
				procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
			} else {
				// No messages, sleep to avoid 100% CPU
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

// Cleanup remove todas as hotkeys
func (m *Manager) Cleanup() {
	m.Stop()

	m.mu.Lock()
	for id := range m.callbacks {
		procUnregisterHotKey.Call(m.hwnd, uintptr(id))
	}
	m.callbacks = make(map[int]HotkeyCallback)
	m.mu.Unlock()
}
