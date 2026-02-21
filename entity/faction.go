package entity

import (
	"archefriend/config"
	"archefriend/memory"

	"golang.org/x/sys/windows"
)

// Faction IDs conhecidos (Race Faction via FactionManager)
const (
	FACTION_NUIAN   uint32 = 102
	FACTION_ELF     uint32 = 103
	FACTION_DWARF   uint32 = 104
	FACTION_HARANI  uint32 = 109
	FACTION_FIRRAN  uint32 = 113
	FACTION_WARBORN uint32 = 114
)

// GetPirateFactionID retorna o FactionID que define piratas
func GetPirateFactionID(handle windows.Handle, x2game uintptr) uint32 {
	return memory.ReadU32(handle, x2game+config.PTR_PIRATE_FACTION_ID)
}

// GetFactionThreshold retorna o threshold de divisão West/East
func GetFactionThreshold(handle windows.Handle, x2game uintptr) uint32 {
	return memory.ReadU32(handle, x2game+config.PTR_FACTION_THRESHOLD)
}

// IsWesternFaction verifica se o FactionID é do lado West
func IsWesternFaction(factionID uint32) bool {
	return factionID >= FACTION_NUIAN && factionID <= FACTION_DWARF
}

// IsEasternFaction verifica se o FactionID é do lado East
func IsEasternFaction(factionID uint32) bool {
	return factionID >= FACTION_HARANI && factionID <= FACTION_WARBORN
}

// IsPirate verifica se o jogador é pirata
func IsPirate(factionID, pirateFactionID uint32) bool {
	return factionID == pirateFactionID
}

// GetFactionSide retorna "west", "east", "pirate" ou "unknown"
func GetFactionSide(handle windows.Handle, x2game uintptr, factionID uint32) string {
	pirateFactionID := GetPirateFactionID(handle, x2game)

	if IsPirate(factionID, pirateFactionID) {
		return "pirate"
	}
	if IsWesternFaction(factionID) {
		return "west"
	}
	if IsEasternFaction(factionID) {
		return "east"
	}
	return "unknown"
}

// IsEnemy verifica se dois FactionIDs são inimigos
func IsEnemy(myFactionID, targetFactionID, pirateFactionID uint32) bool {
	// Piratas são inimigos de todos (exceto outros piratas)
	myPirate := IsPirate(myFactionID, pirateFactionID)
	targetPirate := IsPirate(targetFactionID, pirateFactionID)

	if myPirate && targetPirate {
		return false // Piratas são aliados entre si
	}
	if myPirate || targetPirate {
		return true // Pirata vs não-pirata = inimigo
	}

	// West vs East
	return IsWesternFaction(myFactionID) != IsWesternFaction(targetFactionID)
}
