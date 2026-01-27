package config

import "time"

// ============================================================
// ARCHEAGE MEMORY OFFSETS - MAPEADOS VIA IDA PRO
// ============================================================

const (
	// ===== PONTEIROS GLOBAIS (x2game.dll + offset) =====
	PTR_GAME_CLIENT   uintptr = 0xE9DC68 // GameClient Manager
	PTR_LOCALPLAYER   uintptr = 0xE9DC54 // Ponteiro direto pro player
	PTR_ENEMY_TARGET  uintptr = 0x19EBF4 // Target UI structure

	// ===== LOCAL PLAYER CHAIN =====
	// [PTR_LOCALPLAYER] -> +0x10 = Entity Address
	OFF_PLAYER_ENTITY  uint32 = 0x10
	OFF_GC_LOCALPLAYER uint32 = 0xFC // [GameClient] + 0xFC = LocalPlayer (252)

	// ===== ENTITY STRUCT =====
	OFF_VTABLE    uint32 = 0x00 // VTable pointer
	OFF_ENTITY_ID uint32 = 0x30 // Entity ID (uint32)

	// ===== POSIÇÃO (float) =====
	OFF_POS_X uint32 = 0x830
	OFF_POS_Z uint32 = 0x834
	OFF_POS_Y uint32 = 0x838

	// ===== HP =====
	OFF_HP_CURRENT uint32 = 0x84C // HP atual

	// ===== MAX HP CHAIN =====
	// [Entity + 0x38] -> [+0x4698] -> [+0x10] -> [+0x420] = MaxHP
	OFF_ENTITY_BASE uint32 = 0x38
	OFF_TO_ESI      uint32 = 0x4698
	OFF_TO_STATS    uint32 = 0x10
	OFF_MAXHP       uint32 = 0x420

	// ===== NOME CHAIN =====
	// [Entity + 0x0C] -> [+0x1C] = Nome string
	OFF_NAME_PTR1 uint32 = 0x0C
	OFF_NAME_PTR2 uint32 = 0x1C

	// ===== FLAGS (descobertos via IDA) =====
	OFF_IS_DEAD    uint32 = 0x46D6 // byte - 0=alive, 1=dead (UnitIsDead offset 18134)
	OFF_COMBAT_RAW uint32 = 0x458C // Combat state encrypted (offset 17788)

	// ===== MANA CHAIN =====
	PTR_MANA_BASE    uintptr = 0x130D824
	OFF_MANA_PTR1    uint32  = 0x4
	OFF_MANA_PTR2    uint32  = 0x18
	OFF_MANA_PTR3    uint32  = 0xB0
	OFF_MANA_PTR4    uint32  = 0x10
	OFF_MANA_PTR5    uint32  = 0x5C
	OFF_MANA_PTR6    uint32  = 0x0
	OFF_MANA_CURRENT uint32  = 0x318
	OFF_MANA_MAX     uint32  = 0x314

	// ===== TARGET (do player) =====
	OFF_TARGET_ENTITY_ID uint32 = 0x73D0 // [LocalPlayer + 0x73D0] = target entity ID

	// ===== TARGET UI STRUCT (PTR_ENEMY_TARGET) =====
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

	// ===== BUFF SYSTEM (descoberto via IDA: sub_3913E880, sub_39557F70) =====
	// BuffManager offsets (relativo ao BuffManager pointer)
	OFF_BUFF_COUNT   uint32 = 0x20   // this + 8 * 4 = buff count
	OFF_BUFF_ARRAY   uint32 = 0x28   // this + 10 * 4 = buff array start
	OFF_DEBUFF_COUNT uint32 = 0xD28  // this + 842 * 4 = debuff count
	OFF_DEBUFF_ARRAY uint32 = 0xD30  // this + 844 * 4 = debuff array start
	OFF_HIDDEN_COUNT uint32 = 0x1550 // this + 1364 * 4 = hidden buff count
	OFF_HIDDEN_ARRAY uint32 = 0x1558 // this + 1366 * 4 = hidden buff array

	// BuffManager pointer from entity
	OFF_DEBUFF_PTR uint32 = 0x1898 // [Entity + 0x38] -> [+0x1898] = BuffManager

	// ===== BUFF STRUCT (size = 0x68 / 104 bytes, descoberto via IDA) =====
	BUFF_SIZE          int    = 0x68
	BUFF_OFF_SLOT      uint32 = 0x00 // Slot index
	BUFF_OFF_ID        uint32 = 0x04 // Buff ID
	BUFF_OFF_TIME_MAX  uint32 = 0x30 // Duração total (ms)
	BUFF_OFF_TIME_LEFT uint32 = 0x34 // Tempo restante (ms)
	BUFF_OFF_TICK      uint32 = 0x3C // Tick timer
	BUFF_OFF_STACK     uint32 = 0x40 // Stack count
	BUFF_OFF_TYPE      uint32 = 0x1E0 // Buff type (2,3,4)

	// Debuff struct (mesmo layout)
	DEBUFF_SIZE int = 0x68

	// ===== BUFF FREEZE =====
	PTR_BUFF_FREEZE       uintptr = 0x01325640
	OFF_BUFF_FREEZE_PTR1  uint32  = 0x4
	OFF_BUFF_FREEZE_PTR2  uint32  = 0x20
	OFF_BUFF_FREEZE_PTR3  uint32  = 0x8
	OFF_BUFF_FREEZE_FINAL uint32  = 0x384

	// ===== SKILL CAST DETECTION =====
	// Tentativa de cast (validação)
	OFF_SKILL_TRY_1 uintptr = 0x51C509
	OFF_SKILL_TRY_2 uintptr = 0xB06A57

	// Cast bem sucedido (EBX contém Skill ID)
	OFF_SKILL_SUCCESS_1 uintptr = 0x569E1A // Principal
	OFF_SKILL_SUCCESS_2 uintptr = 0x56CAD5
	OFF_SKILL_SUCCESS_3 uintptr = 0x564717
	OFF_SKILL_SUCCESS_4 uintptr = 0x51C4A9
	OFF_SKILL_SUCCESS_5 uintptr = 0xAED061
	OFF_SKILL_SUCCESS_6 uintptr = 0x56AAD1
	OFF_SKILL_SUCCESS_7 uintptr = 0x56AADE

	// ===== MOUNT =====
	PTR_MOUNT_BASE uintptr = 0x000930BC
	OFF_MOUNT_PTR1 uint32  = 0x3C
	OFF_MOUNT_PTR2 uint32  = 0x4

	// ===== LOOT BYPASS =====
	// Offsets para patches de loot reach (loot from any distance)
	OFF_LOOT_GENERIC_CHECK uintptr = 0x09C556  // Generic Check - necessário para ícone
	OFF_LOOT_CAN_LOOT      uintptr = 0x68DFAE // CanLoot distance check
	OFF_LOOT_HANDLER_DIST  uintptr = 0x68ECAD // Loot Handler distance

	// ===== DOODAD BYPASS =====
	// Offset para patch de distância de doodads (objetos interagíveis)
	OFF_DOODAD_DISTANCE_CHECK uintptr = 0x2EAFB0 // sub_392EAFB0 - distance check function
)

// ===== SKILL IDs CONHECIDOS =====
const (
	SKILL_BONDBREAKER     uint32 = 12034
	SKILL_SHRUG_IT_OFF    uint32 = 11429
	SKILL_HP_POTION       uint32 = 35234 // Desert Fire - 21s
	SKILL_HP_POTION_LARGE uint32 = 35236 // Nui's Nova - 90s
	SKILL_MP_POTION       uint32 = 35235 // Mossy Pool - 21s
	SKILL_MP_POTION_LARGE uint32 = 35237 // Kraken's Might - 90s
)

// ===== BUFF IDs CONHECIDOS =====
const (
	BUFF_SWIM  uint32 = 5909
	BUFF_SPEED uint32 = 4627
	BUFF_LIGHT uint32 = 8284
	BUFF_DARU  uint32 = 9000001
)

// ===== SCREEN SETTINGS =====
const (
	SCREEN_WIDTH  = 1200
	SCREEN_HEIGHT = 800
	RADAR_RADIUS  = 150
	RADAR_RANGE   = 500.0
	SCAN_RANGE    = 500.0
)

// ===== TIMING =====
const (
	KEY_SPAM_COUNT    = 5
	KEY_SPAM_INTERVAL = 15 * time.Millisecond
)

// ===== INPUT MANAGER =====
const (
	DEFAULT_INPUT_KEY      = 0x56 // V key
	DEFAULT_INPUT_INTERVAL = 100 * time.Millisecond
)
