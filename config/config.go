package config

import "time"

const (
	PTR_GAME_CLIENT     uintptr = 0xE9DC68
	PTR_LOCALPLAYER     uintptr = 0xE9DC54
	PTR_ENEMY_TARGET    uintptr = 0x19EBF4 // Entity do target (ID, posição, etc)
	PTR_TARGET_UI       uintptr = 0x0      // UI do target (HP correto) - precisa ser encontrado

	OFF_PLAYER_ENTITY  uint32 = 0x10
	OFF_GC_LOCALPLAYER uint32 = 0xFC

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

	OFF_NAME_PTR1 uint32 = 0x0C
	OFF_NAME_PTR2 uint32 = 0x1C

	OFF_IS_DEAD    uint32 = 0x46D6
	OFF_COMBAT_RAW uint32 = 0x458C

	PTR_MANA_BASE    uintptr = 0x130D824
	OFF_MANA_PTR1    uint32  = 0x4
	OFF_MANA_PTR2    uint32  = 0x18
	OFF_MANA_PTR3    uint32  = 0xB0
	OFF_MANA_PTR4    uint32  = 0x10
	OFF_MANA_PTR5    uint32  = 0x5C
	OFF_MANA_PTR6    uint32  = 0x0
	OFF_MANA_CURRENT uint32  = 0x318
	OFF_MANA_MAX     uint32  = 0x314

	OFF_TARGET_ENTITY_ID uint32 = 0x73D0 // TODO

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

	OFF_LOOT_GENERIC_CHECK    uintptr = 0x09C556
	OFF_LOOT_CAN_LOOT         uintptr = 0x68DFAE
	OFF_LOOT_HANDLER_DIST     uintptr = 0x68ECAD
	OFF_DOODAD_DISTANCE_CHECK uintptr = 0x2EAFB0
)

const (
	KEY_SPAM_COUNT    = 5
	KEY_SPAM_INTERVAL = 15 * time.Millisecond
)
