package loot

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"winkit/config"
	"winkit/memory"

	"golang.org/x/sys/windows"
)

var (
	kernel32DLL       = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx = kernel32DLL.NewProc("VirtualAllocEx")
	procVirtualFreeEx  = kernel32DLL.NewProc("VirtualFreeEx")
)

const (
	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	MEM_RELEASE            = 0x8000
	PAGE_EXECUTE_READWRITE = 0x40

	caveSize       uintptr = 0x1000
	caveReqOff     uintptr = 0x000 // byte: request flag (Go sets 1, shellcode clears)
	caveResOff     uintptr = 0x001 // byte: response flag (shellcode sets 1 when done)
	caveTargetOff  uintptr = 0x004 // uint32: targetId param
	caveLootAllOff uintptr = 0x008 // uint32: lootAll param (0/1)
	caveRetValOff  uintptr = 0x00C // uint32: captured EAX (LootMgr_RequestLoot return code)

	caveTakeReqOff  uintptr = 0x010 // byte: TakeItem request flag
	caveTakeResOff  uintptr = 0x011 // byte: TakeItem response flag
	caveTakeSlotOff uintptr = 0x014 // uint32: slotIndex param
	caveTakeRetOff  uintptr = 0x018 // uint32: captured AL (0/1, zero-extended)

	// SendSocialActionPacket request block. Lives here (not its own package)
	// because it reuses this same Tick-hook code cave — only one feature can
	// own Tick's hook at a time, see RequestSender doc comment below. Not
	// loot-specific: used to mask the loot pose from observers by firing a
	// second, normally-replicated action (see memory/loot-anim-suppress.md).
	caveSocialReqOff uintptr = 0x020 // byte: request flag
	caveSocialResOff uintptr = 0x021 // byte: response flag
	caveSocialCmdOff uintptr = 0x024 // uint32: commandId param

	// SetTarget request block. Also not loot-specific — reuses this cave for
	// the same reason PlaySocialAction does (see RequestSender.SetTarget doc
	// comment below). Replaces bot.setTarget's old CreateRemoteThread call.
	caveTgtReqOff    uintptr = 0x028 // byte: request flag
	caveTgtResOff    uintptr = 0x029 // byte: response flag
	caveTgtUnitIdOff uintptr = 0x02C // uint32: unitId param
	caveTgtFlagOff   uintptr = 0x030 // uint32: flag param (0 = normal select)

	caveCodeOff uintptr = 0x100 // Tick-hook shellcode
)

// RequestSender calls LootMgr_RequestLoot(this, targetId, lootAll) directly,
// skipping the Lua/UI click and the keyspam that bot.onMobDead uses today.
//
// It reuses the same technique as esp/houses.go's CS222 sender: a small
// inline hook on the game's own Tick() function runs our shellcode once per
// frame, on the game's real main thread. This avoids the client's
// main-thread-only allocation gate that a raw CreateRemoteThread call into
// the packet-send path trips (confirmed via Ghidra: LootMgr_RequestLoot
// funnels into the same shared NetChannel_SendPacket used by skill use and
// slave summon — see memory/loot-request-handler.md and
// memory/skill-use-handler.md). CreateRemoteThread creates a thread with a
// different TID than the game's main thread, which fails that gate;
// hooking Tick runs the call from the real main thread instead.
//
// NOTE: only one feature can own Tick's hook at a time. If House ESP's CS222
// sender is active, Start() will fail rather than corrupt its hook.
type RequestSender struct {
	handle   windows.Handle
	x2game   uintptr
	caveAddr uintptr
	hooked   bool
	origTick []byte

	// lootMu serializes LootAll: RequestLoot/TakeItem share the same code
	// cave and LootMgr singleton, so two callers racing (e.g. the F1
	// death-watcher and the manual V hotkey firing on different targets at
	// once) would corrupt each other's request/response fields.
	lootMu sync.Mutex

	// targetMu serializes SetTarget for the same reason lootMu exists: the
	// request/response fields live in shared cave memory, so two concurrent
	// callers would stomp each other's unitId/flag before the Tick hook reads
	// them.
	targetMu sync.Mutex
}

func NewRequestSender(handle windows.Handle, x2game uintptr) *RequestSender {
	return &RequestSender{handle: handle, x2game: x2game}
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// Start allocates the code cave and installs the Tick hook.
func (r *RequestSender) Start() error {
	if r.hooked {
		return nil
	}

	realTick := r.x2game + config.OFF_LOOT_TICK_FUNC

	prologue := memory.ReadBytes(r.handle, realTick, 6)
	expected := []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x08}
	for i := range expected {
		if prologue[i] != expected[i] {
			if prologue[0] == 0xE9 {
				return fmt.Errorf("Tick já está hookado por outra feature (House ESP CS222?) — desative-a antes de usar loot via pacote")
			}
			return fmt.Errorf("Tick prologue mismatch: % X", prologue)
		}
	}
	r.origTick = append([]byte{}, prologue...)

	cave, _, _ := procVirtualAllocEx.Call(uintptr(r.handle), 0, uintptr(caveSize), MEM_COMMIT|MEM_RESERVE, PAGE_EXECUTE_READWRITE)
	if cave == 0 {
		return fmt.Errorf("VirtualAllocEx falhou")
	}
	r.caveAddr = cave
	memory.WriteBytes(r.handle, cave, make([]byte, caveSize))

	reqAddr := cave + caveReqOff
	resAddr := cave + caveResOff
	targetAddr := cave + caveTargetOff
	lootAllAddr := cave + caveLootAllOff
	retValAddr := cave + caveRetValOff
	codeAddr := cave + caveCodeOff

	lootMgrIndirect := r.x2game + config.OFF_LOOTMGR_INDIRECT
	requestLootFunc := r.x2game + config.OFF_LOOT_REQUEST_FUNC
	takeItemFunc := r.x2game + config.OFF_LOOT_TAKE_ITEM_FUNC

	takeReqAddr := cave + caveTakeReqOff
	takeResAddr := cave + caveTakeResOff
	takeSlotAddr := cave + caveTakeSlotOff
	takeRetAddr := cave + caveTakeRetOff

	socialActionFunc := r.x2game + config.OFF_SOCIAL_ACTION_FUNC
	socialReqAddr := cave + caveSocialReqOff
	socialResAddr := cave + caveSocialResOff
	socialCmdAddr := cave + caveSocialCmdOff

	setTargetFunc := r.x2game + config.OFF_SET_TARGET_FUNC
	tgtReqAddr := cave + caveTgtReqOff
	tgtResAddr := cave + caveTgtResOff
	tgtUnitIdAddr := cave + caveTgtUnitIdOff
	tgtFlagAddr := cave + caveTgtFlagOff

	sc := []byte{}
	sc = append(sc, 0x9C, 0x60) // pushfd, pushad

	sc = append(sc, 0x80, 0x3D) // cmp byte [reqAddr], 1
	sc = append(sc, le32(uint32(reqAddr))...)
	sc = append(sc, 0x01)
	sc = append(sc, 0x75, 0x00) // jne skip (patched below)
	jnePos := len(sc) - 1

	sc = append(sc, 0xC6, 0x05) // mov byte [reqAddr], 0
	sc = append(sc, le32(uint32(reqAddr))...)
	sc = append(sc, 0x00)

	// this = *(*lootMgrIndirect) — double-indirect singleton chain
	sc = append(sc, 0xA1) // mov eax, [lootMgrIndirect]
	sc = append(sc, le32(uint32(lootMgrIndirect))...)
	sc = append(sc, 0x8B, 0x08) // mov ecx, [eax]  -> this

	// thiscall(this, targetId, lootAll) pushes right-to-left: lootAll first
	sc = append(sc, 0xFF, 0x35) // push dword ptr [lootAllAddr]
	sc = append(sc, le32(uint32(lootAllAddr))...)
	sc = append(sc, 0xFF, 0x35) // push dword ptr [targetAddr]
	sc = append(sc, le32(uint32(targetAddr))...)

	sc = append(sc, 0xB8) // mov eax, requestLootFunc
	sc = append(sc, le32(uint32(requestLootFunc))...)
	sc = append(sc, 0xFF, 0xD0) // call eax  (callee does ret 8, no cleanup needed here)

	sc = append(sc, 0xA3) // mov [retValAddr], eax
	sc = append(sc, le32(uint32(retValAddr))...)

	sc = append(sc, 0xC6, 0x05) // mov byte [resAddr], 1
	sc = append(sc, le32(uint32(resAddr))...)
	sc = append(sc, 0x01)

	sc[jnePos] = byte(len(sc) - (jnePos + 1)) // patch jne offset

	// ── TakeItem block ──
	sc = append(sc, 0x80, 0x3D) // cmp byte [takeReqAddr], 1
	sc = append(sc, le32(uint32(takeReqAddr))...)
	sc = append(sc, 0x01)
	sc = append(sc, 0x75, 0x00) // jne skip (patched below)
	jneTakePos := len(sc) - 1

	sc = append(sc, 0xC6, 0x05) // mov byte [takeReqAddr], 0
	sc = append(sc, le32(uint32(takeReqAddr))...)
	sc = append(sc, 0x00)

	sc = append(sc, 0xA1) // mov eax, [lootMgrIndirect]
	sc = append(sc, le32(uint32(lootMgrIndirect))...)
	sc = append(sc, 0x8B, 0x08) // mov ecx, [eax]  -> this

	sc = append(sc, 0xFF, 0x35) // push dword ptr [takeSlotAddr]
	sc = append(sc, le32(uint32(takeSlotAddr))...)

	sc = append(sc, 0xB8) // mov eax, takeItemFunc
	sc = append(sc, le32(uint32(takeItemFunc))...)
	sc = append(sc, 0xFF, 0xD0) // call eax (callee does ret 4)

	sc = append(sc, 0x25, 0xFF, 0x00, 0x00, 0x00) // and eax, 0xFF (AL is the only defined byte)

	sc = append(sc, 0xA3) // mov [takeRetAddr], eax
	sc = append(sc, le32(uint32(takeRetAddr))...)

	sc = append(sc, 0xC6, 0x05) // mov byte [takeResAddr], 1
	sc = append(sc, le32(uint32(takeResAddr))...)
	sc = append(sc, 0x01)

	sc[jneTakePos] = byte(len(sc) - (jneTakePos + 1)) // patch jne offset

	// ── SendSocialActionPacket block ──
	// No `this`/ECX needed: the function resolves the local player internally
	// via g_pGameClientIndirect. RET 4, so no stack cleanup needed after call.
	sc = append(sc, 0x80, 0x3D) // cmp byte [socialReqAddr], 1
	sc = append(sc, le32(uint32(socialReqAddr))...)
	sc = append(sc, 0x01)
	sc = append(sc, 0x75, 0x00) // jne skip (patched below)
	jneSocialPos := len(sc) - 1

	sc = append(sc, 0xC6, 0x05) // mov byte [socialReqAddr], 0
	sc = append(sc, le32(uint32(socialReqAddr))...)
	sc = append(sc, 0x00)

	sc = append(sc, 0xFF, 0x35) // push dword ptr [socialCmdAddr]
	sc = append(sc, le32(uint32(socialCmdAddr))...)

	sc = append(sc, 0xB8) // mov eax, socialActionFunc
	sc = append(sc, le32(uint32(socialActionFunc))...)
	sc = append(sc, 0xFF, 0xD0) // call eax (callee does ret 4)

	sc = append(sc, 0xC6, 0x05) // mov byte [socialResAddr], 1
	sc = append(sc, le32(uint32(socialResAddr))...)
	sc = append(sc, 0x01)

	sc[jneSocialPos] = byte(len(sc) - (jneSocialPos + 1)) // patch jne offset

	// ── SetTarget block ──
	// cdecl, 2 dword args, caller cleans stack (add esp,8) — mirrors the
	// exact push order/cleanup the old CreateRemoteThread shellcode used.
	sc = append(sc, 0x80, 0x3D) // cmp byte [tgtReqAddr], 1
	sc = append(sc, le32(uint32(tgtReqAddr))...)
	sc = append(sc, 0x01)
	sc = append(sc, 0x75, 0x00) // jne skip (patched below)
	jneTgtPos := len(sc) - 1

	sc = append(sc, 0xC6, 0x05) // mov byte [tgtReqAddr], 0
	sc = append(sc, le32(uint32(tgtReqAddr))...)
	sc = append(sc, 0x00)

	sc = append(sc, 0xFF, 0x35) // push dword ptr [tgtFlagAddr]
	sc = append(sc, le32(uint32(tgtFlagAddr))...)
	sc = append(sc, 0xFF, 0x35) // push dword ptr [tgtUnitIdAddr]
	sc = append(sc, le32(uint32(tgtUnitIdAddr))...)

	sc = append(sc, 0xB8) // mov eax, setTargetFunc
	sc = append(sc, le32(uint32(setTargetFunc))...)
	sc = append(sc, 0xFF, 0xD0) // call eax
	sc = append(sc, 0x83, 0xC4, 0x08) // add esp, 8 (cdecl cleanup, callee doesn't)

	sc = append(sc, 0xC6, 0x05) // mov byte [tgtResAddr], 1
	sc = append(sc, le32(uint32(tgtResAddr))...)
	sc = append(sc, 0x01)

	sc[jneTgtPos] = byte(len(sc) - (jneTgtPos + 1)) // patch jne offset

	sc = append(sc, 0x61, 0x9D) // popad, popfd
	sc = append(sc, r.origTick...)
	sc = append(sc, 0xE9) // jmp back
	jb := int32(realTick+6) - int32(codeAddr+uintptr(len(sc))+4)
	sc = append(sc, le32(uint32(jb))...)

	memory.WriteBytes(r.handle, codeAddr, sc)

	patch := []byte{0xE9}
	jmp := int32(codeAddr) - int32(realTick+5)
	patch = append(patch, le32(uint32(jmp))...)
	patch = append(patch, 0x90)
	if !memory.WriteBytesProtected(r.handle, realTick, patch) {
		procVirtualFreeEx.Call(uintptr(r.handle), cave, 0, MEM_RELEASE)
		r.caveAddr = 0
		return fmt.Errorf("falha ao instalar hook em Tick")
	}

	r.hooked = true
	fmt.Printf("[LOOT] RequestSender: code cave @ 0x%X, shellcode=%d bytes\n", cave, len(sc))
	return nil
}

// Stop removes the Tick hook and frees the code cave.
func (r *RequestSender) Stop() {
	if r.hooked {
		realTick := r.x2game + config.OFF_LOOT_TICK_FUNC
		memory.WriteBytesProtected(r.handle, realTick, r.origTick)
		r.hooked = false
	}
	if r.caveAddr != 0 {
		time.Sleep(50 * time.Millisecond)
		procVirtualFreeEx.Call(uintptr(r.handle), r.caveAddr, 0, MEM_RELEASE)
		r.caveAddr = 0
	}
}

// PlaySocialAction calls SendSocialActionPacket(commandId) on the game's real
// main thread (next Tick). commandId is whatever numeric ID the client's chat
// command table maps an emote/social action to (see memory/loot-anim-suppress.md
// — only 0x5c, a targeted "greet"-style action, is confirmed so far; a
// self/untargeted emote ID like /wave or /dance still needs live discovery
// via x64dbg: breakpoint x2game+OFF_SOCIAL_ACTION_FUNC, type the emote's chat
// command in-game, read the pushed commandId off the stack at entry).
//
// Not loot-specific — this exists on RequestSender purely to reuse its
// Tick-hook code cave (only one feature can own Tick's hook at a time).
func (r *RequestSender) PlaySocialAction(commandId uint32) error {
	if !r.hooked {
		if err := r.Start(); err != nil {
			return err
		}
	}

	memory.WriteU32(r.handle, r.caveAddr+caveSocialCmdOff, commandId)
	memory.WriteU8(r.handle, r.caveAddr+caveSocialResOff, 0)
	memory.WriteU8(r.handle, r.caveAddr+caveSocialReqOff, 1)

	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		if memory.ReadU8(r.handle, r.caveAddr+caveSocialResOff) == 1 {
			return nil
		}
	}
	return fmt.Errorf("timeout esperando Tick processar PlaySocialAction(%d)", commandId)
}

// SetTarget calls X2::GameClient::SetTarget(unitId, 0) on the game's real
// main thread (next Tick). Replaces bot.setTarget's old CreateRemoteThread
// implementation: a remote thread has a different TID than the game's main
// thread, which is believed to trip the same main-thread-only gate already
// confirmed for the shared NetChannel_SendPacket path (see
// memory/loot-request-handler.md, memory/skill-use-handler.md) — and, worse,
// bot.tickTargeting retried that call every ScanInterval (20ms) on mismatch,
// turning a single failing call into a CreateRemoteThread/VirtualAllocEx
// storm. Not loot-specific — reuses this cave purely for Tick-hook access,
// same as PlaySocialAction above.
func (r *RequestSender) SetTarget(unitId uint32) error {
	r.targetMu.Lock()
	defer r.targetMu.Unlock()

	if !r.hooked {
		if err := r.Start(); err != nil {
			return err
		}
	}

	memory.WriteU32(r.handle, r.caveAddr+caveTgtUnitIdOff, unitId)
	memory.WriteU32(r.handle, r.caveAddr+caveTgtFlagOff, 0)
	memory.WriteU8(r.handle, r.caveAddr+caveTgtResOff, 0)
	memory.WriteU8(r.handle, r.caveAddr+caveTgtReqOff, 1)

	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		if memory.ReadU8(r.handle, r.caveAddr+caveTgtResOff) == 1 {
			return nil
		}
	}
	return fmt.Errorf("timeout esperando Tick processar SetTarget(%d)", unitId)
}

// RequestLoot calls LootMgr_RequestLoot(this, targetId, lootAll) on the
// game's real main thread (next Tick) and returns its EAX return code.
//
// Return-code semantics are a hypothesis, not yet confirmed at runtime
// (see memory/loot-request-handler.md): the request is believed to be
// fire-and-forget (client sends, server answers async and populates the
// loot window), so a single call may not be enough to also harvest items —
// callers may need to poll/retry like LootMgr_OnLootMenuCommand does.
func (r *RequestSender) RequestLoot(targetId uint32, lootAll bool) (uint32, error) {
	if !r.hooked {
		if err := r.Start(); err != nil {
			return 0, err
		}
	}

	lootAllVal := uint32(0)
	if lootAll {
		lootAllVal = 1
	}
	memory.WriteU32(r.handle, r.caveAddr+caveTargetOff, targetId)
	memory.WriteU32(r.handle, r.caveAddr+caveLootAllOff, lootAllVal)
	memory.WriteU8(r.handle, r.caveAddr+caveResOff, 0)
	memory.WriteU8(r.handle, r.caveAddr+caveReqOff, 1)

	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		if memory.ReadU8(r.handle, r.caveAddr+caveResOff) == 1 {
			return memory.ReadU32(r.handle, r.caveAddr+caveRetValOff), nil
		}
	}
	return 0, fmt.Errorf("timeout esperando Tick processar o pedido de loot")
}

// resolveLootMgrThis reads the LootMgr singleton pointer (pure memory read,
// no native call needed): this = *(*g_pLootMgrIndirect).
func (r *RequestSender) resolveLootMgrThis() uint32 {
	p1 := memory.ReadU32(r.handle, r.x2game+config.OFF_LOOTMGR_INDIRECT)
	if p1 == 0 {
		return 0
	}
	return memory.ReadU32(r.handle, uintptr(p1))
}

// isWindowOpenFor reports whether the LootMgr's loot window is currently
// open for targetId (this+0x0==LOOT_WINDOW_TYPE_OPEN && this+0x4==targetId).
// Pure memory read — these fields are populated by the server's async
// response to RequestLoot, not by anything we call.
func (r *RequestSender) isWindowOpenFor(targetId uint32) bool {
	this := r.resolveLootMgrThis()
	if this == 0 {
		return false
	}
	winType := memory.ReadU32(r.handle, uintptr(this)+config.OFF_LOOTMGR_WINDOW_TYPE)
	winTarget := memory.ReadU32(r.handle, uintptr(this)+config.OFF_LOOTMGR_WINDOW_TARGET)
	return winType == config.LOOT_WINDOW_TYPE_OPEN && winTarget == targetId
}

// getItemCount reads the loot window's item count directly from memory —
// LootMgr_GetItemCount is just (this+0x10 - this+0xC) / stride, no need to
// call the native function for it.
func (r *RequestSender) getItemCount() uint32 {
	this := r.resolveLootMgrThis()
	if this == 0 {
		return 0
	}
	begin := memory.ReadU32(r.handle, uintptr(this)+config.OFF_LOOTMGR_ITEMS_BEGIN)
	end := memory.ReadU32(r.handle, uintptr(this)+config.OFF_LOOTMGR_ITEMS_END)
	if end <= begin {
		return 0
	}
	return (end - begin) / uint32(config.OFF_LOOT_ITEM_STRIDE)
}

// takeItem calls LootMgr_TakeItem(this, slotIndex) on the main thread and
// returns whether it succeeded.
func (r *RequestSender) takeItem(slotIndex uint32) (bool, error) {
	if !r.hooked {
		if err := r.Start(); err != nil {
			return false, err
		}
	}

	memory.WriteU32(r.handle, r.caveAddr+caveTakeSlotOff, slotIndex)
	memory.WriteU8(r.handle, r.caveAddr+caveTakeResOff, 0)
	memory.WriteU8(r.handle, r.caveAddr+caveTakeReqOff, 1)

	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		if memory.ReadU8(r.handle, r.caveAddr+caveTakeResOff) == 1 {
			return memory.ReadU32(r.handle, r.caveAddr+caveTakeRetOff) != 0, nil
		}
	}
	return false, fmt.Errorf("timeout esperando Tick processar TakeItem(%d)", slotIndex)
}

// LootAll requests loot for targetId and, once the loot window is open,
// takes every item — the same sequence "Take All" does in Lua
// (LootMgr_Lua_LootItem looping over LootItem(i)), but driven entirely by
// direct native calls. No distance/range patch is touched at any point:
// LootMgr_RequestLoot has no client-side range gate (only a data field in
// the outgoing packet, see memory/loot-request-handler.md), and TakeItem
// has none either.
func (r *RequestSender) LootAll(targetId uint32) (int, error) {
	r.lootMu.Lock()
	defer r.lootMu.Unlock()

	ret, err := r.RequestLoot(targetId, true)
	if err != nil {
		return 0, err
	}

	switch ret {
	case 1:
		return 0, fmt.Errorf("player ocupado/morto (ret=1)")
	case 2:
		return 0, fmt.Errorf("corpo ainda não está marcado como lootável pelo cliente (ret=2) — selecione o alvo de novo")
	}

	if ret == 0 {
		// Request sent, server populates the window async — poll for it
		// directly via memory instead of re-sending RequestLoot.
		opened := false
		for i := 0; i < 100; i++ {
			time.Sleep(20 * time.Millisecond)
			if r.isWindowOpenFor(targetId) {
				opened = true
				break
			}
		}
		if !opened {
			return 0, fmt.Errorf("timeout esperando o servidor abrir a janela de loot")
		}
	}

	count := r.getItemCount()
	taken := 0
	for i := uint32(0); i < count; i++ {
		ok, err := r.takeItem(i)
		if err != nil {
			fmt.Printf("[LOOT] TakeItem(%d) erro: %v\n", i, err)
			continue
		}
		if ok {
			taken++
		}
		time.Sleep(30 * time.Millisecond) // stagger, avoid bursting the server
	}
	return taken, nil
}
