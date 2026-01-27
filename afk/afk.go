package afk

import (
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

type Monitor struct {
	mu              sync.RWMutex
	enabled         bool
	timeoutSeconds  int
	lastInputTime   time.Time
	checkInterval   time.Duration
	stopChan        chan struct{}
	running         bool
	wasAFK          bool
	OnStateChange   func(isAFK bool)
}

func NewMonitor(timeoutSeconds int) *Monitor {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	return &Monitor{
		enabled:        true,
		timeoutSeconds: timeoutSeconds,
		checkInterval:  1 * time.Second,
		stopChan:       make(chan struct{}),
		lastInputTime:  time.Now(),
		wasAFK:         false,
	}
}

func (m *Monitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopChan = make(chan struct{})
	m.mu.Unlock()

	go m.monitorLoop()
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		close(m.stopChan)
		m.running = false
	}
}

func (m *Monitor) monitorLoop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.updateLastInput()
		}
	}
}

func (m *Monitor) updateLastInput() {
	idleTime := getIdleTime()

	m.mu.Lock()
	enabled := m.enabled
	timeout := m.timeoutSeconds
	wasAFK := m.wasAFK
	callback := m.OnStateChange
	m.mu.Unlock()

	if !enabled {
		return
	}

	isAFK := idleTime >= uint32(timeout*1000)

	if isAFK != wasAFK {
		m.mu.Lock()
		m.wasAFK = isAFK
		m.mu.Unlock()

		if callback != nil {
			callback(isAFK)
		}
	}

	m.mu.Lock()
	if idleTime < uint32(timeout*1000) {
		m.lastInputTime = time.Now().Add(-time.Duration(idleTime) * time.Millisecond)
	}
	m.mu.Unlock()
}

func getIdleTime() uint32 {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))

	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		return 0
	}

	tickCount, _, _ := procGetTickCount.Call()

	return uint32(tickCount) - lii.DwTime
}

func (m *Monitor) IsAFK() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return false
	}

	idleMs := getIdleTime()
	return idleMs >= uint32(m.timeoutSeconds*1000)
}

func (m *Monitor) GetIdleSeconds() int {
	return int(getIdleTime() / 1000)
}

func (m *Monitor) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
}

func (m *Monitor) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
}

func (m *Monitor) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = !m.enabled
	return m.enabled
}

func (m *Monitor) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *Monitor) SetTimeout(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if seconds > 0 {
		m.timeoutSeconds = seconds
	}
}

func (m *Monitor) GetTimeout() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.timeoutSeconds
}
