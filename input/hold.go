package input

import (
	"fmt"
	"unsafe"
)

// ============================================================================
// Held-key primitives for continuous movement (WASD).
//
// Unlike SendKey (down -> sleep 50ms -> up), these inject a SINGLE key event
// with no delay. The movement controller manages hold duration across its own
// ~60Hz ticks, so a blocking sleep here would stall the loop.
//
// We inject the HARDWARE SCAN CODE (KEYEVENTF_SCANCODE), not the virtual key.
// ArcheAge reads movement via DirectInput/raw-input polling, which keys off
// scan codes — a plain wVk SendInput is often ignored for movement. Sending the
// scan code makes it look like real hardware to both DirectInput and normal
// window-message readers.
//
// IMPORTANT: SendInput injects into the FOREGROUND window, so the game must be
// focused. Also, if the game runs at a higher integrity level than this tool,
// UIPI silently drops the input — run this tool as Administrator (same as the
// game). Works identically on foot or driving a slave (same WASD keys).
// ============================================================================

const (
	KEYEVENTF_SCANCODE   = 0x0008
	KEYEVENTF_EXTENDEDKEY = 0x0001
	mapvkVKToVSC         = 0
)

var procMapVirtualKey = user32.NewProc("MapVirtualKeyW")

// vkToScan resolves a virtual-key to its hardware scan code.
func vkToScan(vk uint16) uint16 {
	r, _, _ := procMapVirtualKey.Call(uintptr(vk), uintptr(mapvkVKToVSC))
	return uint16(r)
}

func sendScan(vk uint16, keyUp bool) error {
	sc := vkToScan(vk)
	flags := uint32(KEYEVENTF_SCANCODE)
	if keyUp {
		flags |= KEYEVENTF_KEYUP
	}
	in := INPUT{
		Type: INPUT_KEYBOARD,
		Ki:   KEYBDINPUT{Vk: 0, Scan: sc, Flags: flags},
	}
	ret, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret == 0 {
		return fmt.Errorf("SendInput scan=0x%X up=%v failed (foreground? elevation/UIPI?)", sc, keyUp)
	}
	return nil
}

// HoldKey presses a key DOWN and leaves it held (no matching up). Idempotent at
// the OS level — repeated down events just look like key auto-repeat.
func HoldKey(vk uint16) error { return sendScan(vk, false) }

// ReleaseKey releases a previously held key (single key-UP, no delay).
func ReleaseKey(vk uint16) error { return sendScan(vk, true) }

// ---------------------------------------------------------------------------
// PostMessage path — targets the game WINDOW directly (works unfocused). This
// is the SAME path the reactions/bot use successfully, so the game accepts it
// where external SendInput gets filtered. For movement the game likely needs
// continuous WM_KEYDOWN (auto-repeat), so the controller re-posts every tick.
// lParam layout: bits 0-15 repeat=1, bits 16-23 scan code, bit30 prev-state,
// bit31 transition (both set on key-up), mirroring input.go's sendComboToWindow.
// ---------------------------------------------------------------------------

const (
	wmKeyDown = 0x0100
	wmKeyUp   = 0x0101
)

var procPostMsg = user32.NewProc("PostMessageW")

// HoldKeyWindow posts a WM_KEYDOWN for vk to the game window.
func HoldKeyWindow(hwnd uintptr, vk uint16) error {
	if hwnd == 0 {
		return fmt.Errorf("HoldKeyWindow: no game window")
	}
	sc := uintptr(vkToScan(vk))
	lParam := (sc << 16) | 1
	procPostMsg.Call(hwnd, wmKeyDown, uintptr(vk), lParam)
	return nil
}

// ReleaseKeyWindow posts a WM_KEYUP for vk to the game window.
func ReleaseKeyWindow(hwnd uintptr, vk uint16) error {
	if hwnd == 0 {
		return fmt.Errorf("ReleaseKeyWindow: no game window")
	}
	sc := uintptr(vkToScan(vk))
	lParam := (sc << 16) | 1 | (1 << 30) | (1 << 31)
	procPostMsg.Call(hwnd, wmKeyUp, uintptr(vk), lParam)
	return nil
}
