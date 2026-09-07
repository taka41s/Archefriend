package config

import "time"

const (
	PTR_GAME_CLIENT     uintptr = 0xE9DC68
	PTR_LOCALPLAYER     uintptr = 0xE9DC54
	PTR_ENEMY_TARGET    uintptr = 0x19EBF4

	OFF_PLAYER_ENTITY  uint32 = 0x10

	OFF_VTABLE    uint32 = 0x00
	OFF_ENTITY_ID uint32 = 0x30

	OFF_POS_X uint32 = 0x830
	OFF_POS_Z uint32 = 0x834
	OFF_POS_Y uint32 = 0x838

	OFF_HP_CURRENT uint32 = 0x84C

	OFF_ENTITY_BASE uint32 = 0x38
	OFF_TO_ESI      uint32 = 0x4698
	OFF_TO_STATS    uint32 = 0x10
	OFF_MAXHP       uint32 = 0x420

	PTR_FACTION_MANAGER    uintptr = 0x13287A0
	OFF_FACTION_LOOKUP     uint32  = 0xBC
	PTR_PIRATE_FACTION_ID  uintptr = 0xE9EF18
	PTR_FACTION_THRESHOLD  uintptr = 0xE9EF14

	OFF_NAME_PTR1 uint32 = 0x0C
	OFF_NAME_PTR2 uint32 = 0x1C

	OFF_IS_DEAD    uint32 = 0x46D6

	OFF_TGT_ID      uint32 = 0x008
	OFF_TGT_TYPE    uint32 = 0x020
	OFF_TGT_LEVEL   uint32 = 0x024
	OFF_TGT_POS_X   uint32 = 0x320
	OFF_TGT_POS_Z   uint32 = 0x324
	OFF_TGT_POS_Y   uint32 = 0x328

	OFF_TGT_HP      uint32 = 0x318
	OFF_TGT_MAXHP   uint32 = 0x314
	OFF_TGT_MANA    uint32 = 0xD50
	OFF_TGT_MAXMANA uint32 = 0xD4C

	OFF_BUFF_COUNT   uint32 = 0x20
	OFF_BUFF_ARRAY   uint32 = 0x28
	OFF_DEBUFF_COUNT uint32 = 0xD28
	OFF_DEBUFF_ARRAY uint32 = 0xD30

	OFF_DEBUFF_PTR uint32 = 0x1898

	BUFF_SIZE          int    = 0x68
	BUFF_OFF_SLOT      uint32 = 0x00
	BUFF_OFF_ID        uint32 = 0x04
	BUFF_OFF_TIME_MAX  uint32 = 0x30
	BUFF_OFF_TIME_LEFT uint32 = 0x34
	BUFF_OFF_TICK      uint32 = 0x3C
	BUFF_OFF_STACK     uint32 = 0x40
	BUFF_OFF_TYPE      uint32 = 0x1E0 // TODO

	DEBUFF_SIZE int = 0x68

	OFF_LOOT_GENERIC_CHECK    uintptr = 0x09C556 // NOT applied by loot.Bypass anymore — shared body, 21+ callers incl. non-loot; see loot/loot.go
	OFF_LOOT_CAN_LOOT         uintptr = 0x68DFAE
	OFF_LOOT_HANDLER_DIST     uintptr = 0x68ECAD
	OFF_DOODAD_DISTANCE_CHECK uintptr = 0x2EAFB0

	// Direct loot-packet call (Ghidra-mapped, see memory/loot-request-handler.md):
	// LootMgr_RequestLoot(this, targetId, lootAll) — thiscall — builds the loot
	// request packet and sends it via the shared NetChannel_SendPacket. this is
	// resolved through the double-indirect g_pLootMgrIndirect chain below.
	OFF_LOOTMGR_INDIRECT uintptr = 0xE9B83C // g_pLootMgrIndirect: this = *(*OFF_LOOTMGR_INDIRECT)
	OFF_LOOT_REQUEST_FUNC uintptr = 0x68EBC0 // LootMgr_RequestLoot(this, targetId, lootAll)
	OFF_LOOT_TAKE_ITEM_FUNC uintptr = 0x01AAD0 // LootMgr_TakeItem(this, slotIndex) -> bool (thiscall, ret 4)
	// LootMgr struct fields (this = *(*OFF_LOOTMGR_INDIRECT)):
	//   +0x0/+0x4  = currently-open loot window's (type, targetId) — set async by server response
	//   +0xC/+0x10 = item array [begin, end) pointers, stride OFF_LOOT_ITEM_STRIDE
	OFF_LOOTMGR_WINDOW_TYPE  uintptr = 0x0
	OFF_LOOTMGR_WINDOW_TARGET uintptr = 0x4
	OFF_LOOTMGR_ITEMS_BEGIN  uintptr = 0xC
	OFF_LOOTMGR_ITEMS_END    uintptr = 0x10
	OFF_LOOT_ITEM_STRIDE     uintptr = 0xD8
	LOOT_WINDOW_TYPE_OPEN    uint32  = 0x10000 // this+0x0 value when a loot window is open
	// Same Tick() hook point esp/houses.go uses for its CS222 send — only one
	// feature can have Tick hooked at a time (see loot/packet.go RequestSender).
	OFF_LOOT_TICK_FUNC uintptr = 0x087200
	OFF_CURRENT_SELECTION uintptr = 0x175E830 // g_pCurrentSelection: pointer TO the selection object (double indirection). targetId = *(*OFF_CURRENT_SELECTION)+4

	// Loot animation call site (Ghidra-mapped, see memory/loot-anim-suppress.md).
	// Server-driven, not local: the "OnLootingBag" packet handler calls
	// LootMgr_OnLootBagOpened(this, itemArrayPtr, lootAllFlag), which does
	// `push state; call CActor_SetLootingAnimState` on the LOCAL player's
	// actor (state: 1=loot pose, 2=loot-all gesture). CActor_SetLootingAnimState
	// itself has 2 other callers (loot-close cleanup + the OTHER-players'
	// "OnUnitLootingState" broadcast), so patching happens at this specific
	// call site — not inside the function — to avoid muting other players'
	// loot animations too. 6 bytes (push edx=1; call rel32=5), thiscall
	// ret-4 callee — NOPing both together is stack-neutral.
	OFF_LOOT_ANIM_CALL_SITE uintptr = 0x68E99F

	// SendSocialActionPacket(commandId) — thiscall-shaped (RET 4) but needs no
	// `this`/ECX, resolves the local player internally via g_pGameClientIndirect.
	// Confirmed via 2 independent call sites in the chat-command dispatcher
	// (Ghidra); commandId=0x5c is a confirmed real value (targeted "greet"-like
	// action) but the ID for a self/untargeted emote (wave, dance, ...) is not
	// yet known — needs live discovery. See memory/loot-anim-suppress.md.
	OFF_SOCIAL_ACTION_FUNC uintptr = 0x40C460

	// X2::GameClient::SetTarget(unitId, flag) — cdecl, 2 dword args, caller
	// cleans the stack (confirmed via Ghidra decompile+string literal at this
	// address). flag=0 is normal select. Called via the Tick-hook cave (see
	// loot.RequestSender.SetTarget), not CreateRemoteThread: a remote thread
	// has a different TID than the game's main thread, which is believed to
	// trip the same main-thread-only gate documented for NetChannel_SendPacket
	// (see memory/loot-request-handler.md, memory/skill-use-handler.md).
	OFF_SET_TARGET_FUNC uintptr = 0x1BE090
)

const (
	KEY_SPAM_COUNT    = 5
	KEY_SPAM_INTERVAL = 15 * time.Millisecond
)
