package entity

import (
	"archefriend/config"
	"archefriend/memory"

	"golang.org/x/sys/windows"
)

// Entity representa uma entidade do jogo (player, NPC, mob)
type Entity struct {
	Address      uint32
	EntityID     uint32
	Name         string
	PosX         float32
	PosY         float32
	PosZ         float32
	HP           uint32
	MaxHP        uint32
	MP           uint32
	MaxMP        uint32
	IsDead       bool
	Distance     float32
	VTable       uint32
	IsPlayer     bool
	IsNPC        bool
	IsTargetable bool
}

// Buff representa um buff/debuff
type Buff struct {
	Index    int
	ID       uint32
	Duration uint32 // ms
	TimeLeft uint32 // ms
	Stack    uint32
	Name     string
}

// GetPlayerEntityAddr retorna o endereço da entity do player local
func GetPlayerEntityAddr(handle windows.Handle, x2game uintptr) uint32 {
	ptr1 := memory.ReadU32(handle, x2game+config.PTR_LOCALPLAYER)
	if ptr1 == 0 {
		return 0
	}
	return memory.ReadU32(handle, uintptr(ptr1)+uintptr(config.OFF_PLAYER_ENTITY))
}

// GetEntityName lê o nome de uma entity
func GetEntityName(handle windows.Handle, entityAddr uint32) string {
	namePtr1 := memory.ReadU32(handle, uintptr(entityAddr)+uintptr(config.OFF_NAME_PTR1))
	if !memory.IsValidPtr(namePtr1) {
		return ""
	}
	namePtr2 := memory.ReadU32(handle, uintptr(namePtr1)+uintptr(config.OFF_NAME_PTR2))
	if !memory.IsValidPtr(namePtr2) {
		return ""
	}
	return memory.ReadString(handle, uintptr(namePtr2), 32)
}

// GetMaxHP lê o HP máximo seguindo a cadeia de ponteiros
func GetMaxHP(handle windows.Handle, entityAddr uint32) uint32 {
	base := memory.ReadU32(handle, uintptr(entityAddr)+uintptr(config.OFF_ENTITY_BASE))
	if !memory.IsValidPtr(base) {
		return 0
	}
	esi := memory.ReadU32(handle, uintptr(base)+uintptr(config.OFF_TO_ESI))
	if !memory.IsValidPtr(esi) {
		return 0
	}
	stats := memory.ReadU32(handle, uintptr(esi)+uintptr(config.OFF_TO_STATS))
	if !memory.IsValidPtr(stats) {
		return 0
	}
	return memory.ReadU32(handle, uintptr(stats)+uintptr(config.OFF_MAXHP))
}

// GetLocalPlayerMana lê a mana do player local
func GetLocalPlayerMana(handle windows.Handle, x2game uintptr) (current, max uint32) {
	p1 := memory.ReadU32(handle, x2game+config.PTR_MANA_BASE)
	if p1 == 0 {
		return 0, 0
	}
	p2 := memory.ReadU32(handle, uintptr(p1)+uintptr(config.OFF_MANA_PTR1))
	if p2 == 0 {
		return 0, 0
	}
	p3 := memory.ReadU32(handle, uintptr(p2)+uintptr(config.OFF_MANA_PTR2))
	if p3 == 0 {
		return 0, 0
	}
	p4 := memory.ReadU32(handle, uintptr(p3)+uintptr(config.OFF_MANA_PTR3))
	if p4 == 0 {
		return 0, 0
	}
	p5 := memory.ReadU32(handle, uintptr(p4)+uintptr(config.OFF_MANA_PTR4))
	if p5 == 0 {
		return 0, 0
	}
	p6 := memory.ReadU32(handle, uintptr(p5)+uintptr(config.OFF_MANA_PTR5))
	if p6 == 0 {
		return 0, 0
	}
	p7 := memory.ReadU32(handle, uintptr(p6)+uintptr(config.OFF_MANA_PTR6))
	if p7 == 0 {
		return 0, 0
	}

	current = memory.ReadU32(handle, uintptr(p7)+uintptr(config.OFF_MANA_CURRENT))
	max = memory.ReadU32(handle, uintptr(p7)+uintptr(config.OFF_MANA_MAX))
	return current, max
}

// GetLocalPlayer retorna todas as informações do player local
func GetLocalPlayer(handle windows.Handle, x2game uintptr) Entity {
	var player Entity

	player.Address = GetPlayerEntityAddr(handle, x2game)
	if player.Address == 0 {
		return player
	}

	player.VTable = memory.ReadU32(handle, uintptr(player.Address))
	player.EntityID = memory.ReadU32(handle, uintptr(player.Address)+uintptr(config.OFF_ENTITY_ID))
	player.Name = GetEntityName(handle, player.Address)
	player.PosX = memory.ReadF32(handle, uintptr(player.Address)+uintptr(config.OFF_POS_X))
	player.PosZ = memory.ReadF32(handle, uintptr(player.Address)+uintptr(config.OFF_POS_Z))
	player.PosY = memory.ReadF32(handle, uintptr(player.Address)+uintptr(config.OFF_POS_Y))
	player.HP = memory.ReadU32(handle, uintptr(player.Address)+uintptr(config.OFF_HP_CURRENT))
	player.MaxHP = GetMaxHP(handle, player.Address)
	player.MP, player.MaxMP = GetLocalPlayerMana(handle, x2game)
	player.IsDead = memory.ReadU8(handle, uintptr(player.Address)+uintptr(config.OFF_IS_DEAD)) != 0
	player.IsTargetable = player.EntityID > 0

	return player
}

// GetBuffManagerAddr retorna o endereço do BuffManager do player
func GetBuffManagerAddr(handle windows.Handle, entityAddr uint32) uintptr {
	base := memory.ReadU32(handle, uintptr(entityAddr)+uintptr(config.OFF_ENTITY_BASE))
	if !memory.IsValidPtr(base) {
		return 0
	}
	buffMgr := memory.ReadU32(handle, uintptr(base)+uintptr(config.OFF_DEBUFF_PTR))
	if !memory.IsValidPtr(buffMgr) {
		return 0
	}
	return uintptr(buffMgr)
}
