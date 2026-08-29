package entity

import (
	"winkit/config"
	"winkit/memory"
	"golang.org/x/sys/windows"
)

// Entity representa uma entidade do jogo (player, NPC, mob)
type Entity struct {
	Address      uint32
	EntityID     uint32
	Name         string
	FactionID    uint32
	FactionSide  string
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
	player.IsDead = memory.ReadU8(handle, uintptr(player.Address)+uintptr(config.OFF_IS_DEAD)) != 0
	player.IsTargetable = player.EntityID > 0
	player.FactionID = GetLocalPlayerFactionID(handle, x2game)
	player.FactionSide = GetFactionSide(handle, x2game, player.FactionID)

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

// GetTargetEntityAddr retorna o endereço da entity do target
func GetTargetEntityAddr(handle windows.Handle, x2game uintptr) uint32 {
	return memory.ReadU32(handle, x2game+config.PTR_ENEMY_TARGET)
}

// GetTargetBuffs retorna buffs ou debuffs do target.
// Se debuff=false → buffs normais. Se debuff=true → debuffs.
func GetTargetBuffs(handle windows.Handle, x2game uintptr, debuff bool) []Buff {
	targetAddr := GetTargetEntityAddr(handle, x2game)
	if targetAddr == 0 {
		return nil
	}

	buffMgr := GetBuffManagerAddr(handle, targetAddr)
	if buffMgr == 0 {
		return nil
	}

	countOff := uintptr(config.OFF_BUFF_COUNT)
	arrayOff := uintptr(config.OFF_BUFF_ARRAY)
	if debuff {
		countOff = uintptr(config.OFF_DEBUFF_COUNT)
		arrayOff = uintptr(config.OFF_DEBUFF_ARRAY)
	}

	count := memory.ReadU32(handle, buffMgr+countOff)
	if count == 0 || count > 100 {
		return nil
	}

	buffs := make([]Buff, 0, count)
	for i := uint32(0); i < count; i++ {
		entry := buffMgr + arrayOff + uintptr(i)*uintptr(config.BUFF_SIZE)
		id := memory.ReadU32(handle, entry+uintptr(config.BUFF_OFF_ID))
		if id == 0 {
			continue
		}
		buffs = append(buffs, Buff{
			Index:    int(i),
			ID:       id,
			TimeLeft: memory.ReadU32(handle, entry+uintptr(config.BUFF_OFF_TIME_LEFT)),
			Stack:    memory.ReadU32(handle, entry+uintptr(config.BUFF_OFF_STACK)),
		})
	}
	return buffs
}

func GetLocalPlayerFactionID(handle windows.Handle, x2game uintptr) uint32 {
	managerAddr := x2game + config.PTR_FACTION_MANAGER
	return memory.ReadU32(handle, managerAddr+uintptr(config.OFF_FACTION_LOOKUP))
}
