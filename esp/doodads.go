package esp

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"
)

// ============================================================================
// Doodad ESP / classification
//
// Porta a lógica de features/doodad_esp.cpp do port C++ para Go.
// Reaproveita o walk da árvore do DoodadManager e a descriptografia de
// coordenadas que já existem no package esp (houses.go): mesmos offsets
// (OFF_DOODAD_MGR, DOODAD_TYPE_OFF, DOODAD_ENCRYPTED_OFF, DOODAD_TREE_*).
//
// Roda numa goroutine própria (mesmo padrão do AllEntitiesManager): varre a
// árvore, descriptografa a posição, lê templateId+typeField e classifica cada
// doodad como baú / trade pack / ginseng. O resultado alimenta o radar 2D.
// ============================================================================

// templateId dentro do struct de dados do doodad (dataPtr+0x144), confirmado
// no port C++ (doodad_esp.cpp, s_knownOff).
const DOODAD_TEMPLATE_OFF uint32 = 0x144

// typeField que marca um container "lootável" (baú). Confirmado no C++:
//   e.isChest = !ginseng && !pack && (typeField == 254)
const DOODAD_TYPE_CHEST uint32 = 254

// ----------------------------------------------------------------------------
// Tabelas de template IDs (de AAEmu Doodads.xml / ArcheAge Codex), idênticas
// às do doodad_esp.cpp.
// ----------------------------------------------------------------------------

// Baús de tesouro conhecidos (usado como sinal secundário; o principal é
// typeField == 254).
var chestTemplateIDs = map[uint32]bool{
	// Baús de mar / submarinos
	3484: true, 10712: true, 1978: true, 3012: true, 6340: true,
	6697: true, 6718: true, 6927: true,
	// Baús genéricos / de zona
	692: true, 1019: true, 1125: true, 1198: true, 1327: true, 1328: true,
	5173: true, 5174: true, 6779: true, 6784: true, 6945: true,
}

// Ervas raras colhíveis (roxo no radar).
var ginsengTemplateIDs = map[uint32]bool{
	492: true, // Wild Ginseng
}

// Trade / specialty packs colocados no chão por jogadores.
var packTemplateIDs = map[uint32]bool{
	// Specialty packs padrão (5200-5243, sem 5235)
	5200: true, 5201: true, 5202: true, 5203: true, 5204: true, 5205: true,
	5206: true, 5207: true, 5208: true, 5209: true, 5210: true, 5211: true,
	5212: true, 5213: true, 5214: true, 5215: true, 5216: true, 5217: true,
	5218: true, 5219: true, 5220: true, 5221: true, 5222: true, 5223: true,
	5224: true, 5225: true, 5226: true, 5227: true, 5228: true, 5229: true,
	5230: true, 5231: true, 5232: true, 5233: true, 5234: true, 5236: true,
	5237: true, 5238: true, 5239: true, 5240: true, 5241: true, 5242: true,
	5243: true,
	// Cargo packs
	12448: true, 12449: true,
	// Commercial packs
	11793: true, 11795: true, 11797: true, 11800: true, 11805: true,
	11810: true, 11813: true, 11815: true, 11817: true, 11819: true,
	11821: true, 11823: true,
}

// classifyDoodad segue a MESMA ordem de prioridade do C++: ginseng > pack > baú.
func classifyDoodad(templateID, typeField uint32) (chest, pack, ginseng bool) {
	if ginsengTemplateIDs[templateID] {
		return false, false, true
	}
	if packTemplateIDs[templateID] {
		return false, true, false
	}
	if typeField == DOODAD_TYPE_CHEST || chestTemplateIDs[templateID] {
		return true, false, false
	}
	return false, false, false
}

// ----------------------------------------------------------------------------
// DoodadEntry — um doodad classificado, pronto para o radar.
// ----------------------------------------------------------------------------

type DoodadEntry struct {
	ObjID      uint32
	DataPtr    uint32
	TemplateID uint32
	TypeField  uint32
	PosX       float32 // X (leste-oeste)
	PosY       float32 // Y (norte-sul)
	PosZ       float32 // Z (altura/up)
	Distance   float32
	IsChest    bool
	IsPack     bool
	IsGinseng  bool
}

// ----------------------------------------------------------------------------
// DoodadESPManager
// ----------------------------------------------------------------------------

type DoodadESPManager struct {
	mainManager *Manager // referência para leitura de memória (readU32/readBytes)
	x2game      uintptr

	mu        sync.Mutex
	enabled   bool
	running   bool
	firstScan bool
	rangeM    float32

	cache      []DoodadEntry
	cacheMutex sync.Mutex

	stopChan chan bool
}

func NewDoodadESPManager(x2game uintptr, mainManager *Manager) *DoodadESPManager {
	return &DoodadESPManager{
		mainManager: mainManager,
		x2game:      x2game,
		rangeM:      2000.0, // varredura ampla; o radar aplica seu próprio alcance visual
		firstScan:   true,
		stopChan:    make(chan bool, 1),
	}
}

func (d *DoodadESPManager) IsEnabled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.enabled
}

func (d *DoodadESPManager) Start() {
	d.mu.Lock()
	if d.enabled {
		d.mu.Unlock()
		return
	}
	d.enabled = true
	d.firstScan = true
	d.mu.Unlock()
	go d.updateLoop()
}

func (d *DoodadESPManager) Stop() {
	d.mu.Lock()
	if !d.enabled {
		d.mu.Unlock()
		return
	}
	d.enabled = false
	running := d.running
	d.mu.Unlock()

	if running {
		select {
		case d.stopChan <- true:
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (d *DoodadESPManager) Toggle() bool {
	if d.IsEnabled() {
		d.Stop()
		return false
	}
	d.Start()
	return true
}

func (d *DoodadESPManager) SetRange(r float32) {
	d.mu.Lock()
	d.rangeM = r
	d.mu.Unlock()
}

func (d *DoodadESPManager) GetRange() float32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rangeM
}

// GetCachedDoodads retorna uma cópia thread-safe do último scan.
func (d *DoodadESPManager) GetCachedDoodads() []DoodadEntry {
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()
	out := make([]DoodadEntry, len(d.cache))
	copy(out, d.cache)
	return out
}

func (d *DoodadESPManager) updateLoop() {
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()

	ticker := time.NewTicker(500 * time.Millisecond) // ~2 Hz
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			if !d.IsEnabled() {
				return
			}
			d.updateCache()
		}
	}
}

func (d *DoodadESPManager) updateCache() {
	px, py, _, hasPlayer := d.mainManager.GetPlayerPosition() // px=east, py=north

	nodes := d.walkDoodadTree(5000)
	if len(nodes) == 0 {
		return
	}

	rng := d.GetRange()
	out := make([]DoodadEntry, 0, len(nodes))

	for _, n := range nodes {
		x, y, z, ok := d.decryptCoords(n.DataPtr)
		if !ok {
			continue
		}

		var dist float32
		if hasPlayer {
			dx := x - px
			dy := y - py // horizontal plane is X/Y in Z-up
			dist = float32(math.Sqrt(float64(dx*dx + dy*dy)))
			if dist > rng {
				continue
			}
		}

		typeField := d.mainManager.readU32(uintptr(n.DataPtr) + uintptr(DOODAD_TYPE_OFF))
		templateID := d.mainManager.readU32(uintptr(n.DataPtr) + uintptr(DOODAD_TEMPLATE_OFF))
		chest, pack, ginseng := classifyDoodad(templateID, typeField)

		out = append(out, DoodadEntry{
			ObjID:      n.ObjID,
			DataPtr:    n.DataPtr,
			TemplateID: templateID,
			TypeField:  typeField,
			PosX:       x,
			PosY:       y,
			PosZ:       z,
			Distance:   dist,
			IsChest:    chest,
			IsPack:     pack,
			IsGinseng:  ginseng,
		})
	}

	d.cacheMutex.Lock()
	d.cache = out
	d.cacheMutex.Unlock()

	if d.firstScan {
		var chests, packs, ginseng int
		for i := range out {
			switch {
			case out[i].IsChest:
				chests++
			case out[i].IsPack:
				packs++
			case out[i].IsGinseng:
				ginseng++
			}
		}
		fmt.Printf("[DOODAD] first scan: nodes=%d inRange=%d chests=%d packs=%d ginseng=%d\n",
			len(nodes), len(out), chests, packs, ginseng)
		d.firstScan = false
	}
}

// ----------------------------------------------------------------------------
// Walk da árvore do DoodadManager (rubro-negra). Mesmo layout de nó (22 bytes)
// e mesma cadeia de ponteiros usados em houses.go::walkDoodadTree.
// ----------------------------------------------------------------------------

type doodadNodePtr struct {
	ObjID   uint32
	DataPtr uint32
}

func (d *DoodadESPManager) walkDoodadTree(maxNodes int) []doodadNodePtr {
	globalAddr := d.mainManager.readU32(d.x2game + OFF_DOODAD_MGR)
	if globalAddr == 0 {
		return nil
	}
	mgrAddr := d.mainManager.readU32(uintptr(globalAddr))
	if mgrAddr == 0 {
		return nil
	}
	hdrPtr := d.mainManager.readU32(uintptr(mgrAddr) + DOODAD_TREE_HDR_OFF)
	if hdrPtr == 0 {
		return nil
	}
	root := d.mainManager.readU32(uintptr(hdrPtr) + 4)
	if root == 0 || root == hdrPtr {
		return nil
	}

	var results []doodadNodePtr
	visited := make(map[uint32]bool)

	var inorder func(nodeAddr uint32, depth int)
	inorder = func(nodeAddr uint32, depth int) {
		if depth > 50 || len(results) >= maxNodes {
			return
		}
		if nodeAddr == 0 || nodeAddr == hdrPtr || visited[nodeAddr] {
			return
		}
		visited[nodeAddr] = true

		// left(4) + parent(4) + right(4) + key/objId(4) + data(4) + color(1) + isNil(1)
		buf := make([]byte, 22)
		if !d.mainManager.readBytes(uintptr(nodeAddr), buf) {
			return
		}
		if buf[DOODAD_TREE_ISNIL_OFF] != 0 {
			return
		}
		left := binary.LittleEndian.Uint32(buf[0:4])
		right := binary.LittleEndian.Uint32(buf[8:12])
		objID := binary.LittleEndian.Uint32(buf[12:16])
		dataPtr := binary.LittleEndian.Uint32(buf[16:20])

		inorder(left, depth+1)
		if dataPtr > 0x10000 {
			results = append(results, doodadNodePtr{ObjID: objID, DataPtr: dataPtr})
		}
		inorder(right, depth+1)
	}

	inorder(root, 0)
	return results
}

// decryptCoords — idêntico a houses.go::decryptDoodadCoords (XOR key derivado
// do próprio ponteiro do doodad).
func (d *DoodadESPManager) decryptCoords(doodadPtr uint32) (posX, posY, posZ float32, ok bool) {
	raw := make([]byte, 32)
	if !d.mainManager.readBytes(uintptr(doodadPtr)+uintptr(DOODAD_ENCRYPTED_OFF), raw) {
		return
	}

	byte1 := (doodadPtr >> 8) & 0xFF
	keyBase := doodadPtr * byte1

	dec := make([]byte, 32)
	for i := 0; i < 32; i++ {
		dec[i] = raw[i] ^ byte(keyBase*uint32(i+32))
	}

	decX := math.Float32frombits(binary.LittleEndian.Uint32(dec[16:20]))
	decY := math.Float32frombits(binary.LittleEndian.Uint32(dec[20:24]))
	decZ := math.Float32frombits(binary.LittleEndian.Uint32(dec[24:28]))

	// Z-up (mesma convenção de EntityInfo / GetPlayerPosition):
	//   PosX = X (leste-oeste)  = decX
	//   PosY = Y (norte-sul)    = decY
	//   PosZ = Z (altura/up)    = decZ
	posX = decX
	posY = decY
	posZ = decZ

	if decX > 0 && decX < 50000 && decY > 0 && decY < 50000 {
		ok = true
	}
	return
}
