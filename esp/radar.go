package esp

import (
	"fmt"
	"math"
	"unsafe"
)

// ============================================================================
// Radar 2D (minimapa)
//
// Porta features/radar.cpp do port C++ para o overlay GDI do Go.
// Desenhado no back buffer (m.backDC) todo frame pelo renderLoop quando
// Visible == true. Orientação norte-para-cima (fixa, não gira com o jogador),
// igual ao C++.
//
// Como a janela usa colorkey (magenta = transparente, sem alpha), o fundo do
// radar é sólido escuro em vez de semi-transparente — aceitável para minimapa.
// ============================================================================

var procPolygon = gdi32.NewProc("Polygon")

// Radar guarda config/estado do minimapa.
type Radar struct {
	Visible        bool
	RangeM         float32 // metros de mundo mapeados até a borda do radar
	ShowPlayers    bool
	ShowNPCs       bool
	ShowMates      bool
	ShowChests     bool
	ShowPacks      bool
	ShowGinseng    bool
	ShowAllDoodads bool

	// Route builder: while BuildMode is on, clicking inside the radar places a
	// waypoint at that world position (game coords, in-app — no map image).
	BuildMode bool

	// Geometry cached from the last draw, so a click can be inverse-mapped
	// (screen pixel -> local world coord).
	geoCx, geoCy        int32
	geoRadius, geoScale float32
	geoPX, geoPY        float32
	geoValid            bool
}

func NewRadar() *Radar {
	return &Radar{
		Visible:     false,
		RangeM:      300.0,
		ShowPlayers: true,
		ShowNPCs:    false,
		ShowMates:   true,
		ShowChests:  true,
		ShowPacks:   true,
		ShowGinseng: true,
	}
}

// rgb monta um COLORREF GDI (0x00BBGGRR).
func rgb(r, g, b byte) uintptr {
	return uintptr(b)<<16 | uintptr(g)<<8 | uintptr(r)
}

// ----------------------------------------------------------------------------
// Helpers de GDI (usam os procs package-level declarados em esp.go)
// ----------------------------------------------------------------------------

func fillEllipse(dc uintptr, cx, cy, r int32, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, color)
	oldBrush, _, _ := procSelectObject.Call(dc, brush)
	oldPen, _, _ := procSelectObject.Call(dc, pen)
	procEllipse.Call(dc, uintptr(cx-r), uintptr(cy-r), uintptr(cx+r), uintptr(cy+r))
	procSelectObject.Call(dc, oldPen)
	procSelectObject.Call(dc, oldBrush)
	procDeleteObject.Call(pen)
	procDeleteObject.Call(brush)
}

func strokeEllipse(dc uintptr, cx, cy, r int32, color uintptr, thick int) {
	nullBrush, _, _ := procGetStockObject.Call(NULL_BRUSH)
	pen, _, _ := procCreatePen.Call(PS_SOLID, uintptr(thick), color)
	oldPen, _, _ := procSelectObject.Call(dc, pen)
	oldBrush, _, _ := procSelectObject.Call(dc, nullBrush)
	procEllipse.Call(dc, uintptr(cx-r), uintptr(cy-r), uintptr(cx+r), uintptr(cy+r))
	procSelectObject.Call(dc, oldPen)
	procSelectObject.Call(dc, oldBrush)
	procDeleteObject.Call(pen)
}

func fillPoly(dc uintptr, pts []POINT, fill, outline uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(fill)
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, outline)
	oldBrush, _, _ := procSelectObject.Call(dc, brush)
	oldPen, _, _ := procSelectObject.Call(dc, pen)
	procPolygon.Call(dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	procSelectObject.Call(dc, oldPen)
	procSelectObject.Call(dc, oldBrush)
	procDeleteObject.Call(pen)
	procDeleteObject.Call(brush)
}

func diamondPts(cx, cy, s int32) []POINT {
	return []POINT{{cx, cy - s}, {cx + s, cy}, {cx, cy + s}, {cx - s, cy}}
}

// entityRadarColor devolve cor + raio do ponto conforme tipo/facção.
func entityRadarColor(e EntityInfo) (uintptr, int32) {
	switch {
	case e.IsNPC:
		return rgb(140, 140, 140), 3
	case e.IsMate:
		return rgb(60, 180, 230), 4
	case e.Faction == "west":
		return rgb(50, 230, 50), 5
	case e.Faction == "east":
		return rgb(230, 50, 50), 5
	default:
		return rgb(230, 210, 60), 4 // pirata / facção desconhecida
	}
}

// ----------------------------------------------------------------------------
// drawRadar — desenha o minimapa no back buffer.
// playerX/playerZ = posição do jogador (X leste-oeste, Z norte-sul).
// ----------------------------------------------------------------------------

func (m *Manager) drawRadar(playerX, playerY float32, entities []EntityInfo, doodads []DoodadEntry) {
	r := m.radar
	if r == nil || !r.Visible {
		return
	}
	dc := m.backDC

	const margin = int32(16)
	radius := int32(130)
	if m.screenW < 700 || m.screenH < 500 {
		radius = 90 // telas pequenas
	}
	cx := m.screenW - margin - radius
	cy := margin + radius + 44 // abaixo do label de coordenadas do topo
	rf := float32(radius)
	scale := rf / r.RangeM
	if r.RangeM <= 0 {
		scale = rf / 300.0
	}

	// Cache geometry so a click on the radar can be inverse-mapped to a world
	// position (see handleRadarClick).
	r.geoCx, r.geoCy = cx, cy
	r.geoRadius, r.geoScale = rf, scale
	r.geoPX, r.geoPY = playerX, playerY
	r.geoValid = true

	// clampToCircle prende um ponto (px,py) à borda do círculo do radar.
	clampToCircle := func(px, py float32) (float32, float32, bool) {
		dx := px - float32(cx)
		dy := py - float32(cy)
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if dist <= rf {
			return px, py, false
		}
		inv := (rf - 2) / dist
		return float32(cx) + dx*inv, float32(cy) + dy*inv, true
	}

	// worldToRadar: X cresce para leste (direita), Y cresce para norte (cima).
	// Z-up: o plano horizontal do radar é X/Y (Z é altura, não usada aqui).
	worldToRadar := func(wx, wy float32) (float32, float32) {
		return float32(cx) + (wx-playerX)*scale, float32(cy) - (wy-playerY)*scale
	}

	// ── Fundo + anéis + cruz ────────────────────────────────────────────────
	fillEllipse(dc, cx, cy, radius, rgb(12, 18, 30))
	for i := int32(1); i <= 3; i++ {
		strokeEllipse(dc, cx, cy, radius*i/3, rgb(40, 80, 40), 1)
	}
	m.drawLine(cx, cy-radius, cx, cy+radius, rgb(35, 65, 35), 1)
	m.drawLine(cx-radius, cy, cx+radius, cy, rgb(35, 65, 35), 1)

	// ── Rótulos cardeais ────────────────────────────────────────────────────
	card := rgb(120, 220, 120)
	m.drawText(cx-4, cy-radius+2, "N", card)
	m.drawText(cx-4, cy+radius-16, "S", card)
	m.drawText(cx-radius+3, cy-8, "W", card)
	m.drawText(cx+radius-12, cy-8, "E", card)

	// ── Entidades ───────────────────────────────────────────────────────────
	for _, e := range entities {
		if e.IsNPC && !r.ShowNPCs {
			continue
		}
		if e.IsMate && !r.ShowMates {
			continue
		}
		if e.IsPlayer && !r.ShowPlayers {
			continue
		}
		color, dotR := entityRadarColor(e)
		rx, ry := worldToRadar(e.PosX, e.PosY)
		rx, ry, clamped := clampToCircle(rx, ry)
		fillEllipse(dc, int32(rx), int32(ry), dotR, color)
		if !clamped && e.IsPlayer && e.Name != "" {
			m.drawText(int32(rx)+dotR+2, int32(ry)-7, e.Name, rgb(220, 220, 220))
		}
	}

	// ── Doodads (baús / packs / ginseng) ────────────────────────────────────
	var nChests, nPacks, nGinseng int
	var nearestChest *DoodadEntry
	for i := range doodads {
		d := &doodads[i]
		show := (d.IsChest && r.ShowChests) ||
			(d.IsPack && r.ShowPacks) ||
			(d.IsGinseng && r.ShowGinseng) ||
			r.ShowAllDoodads
		if d.IsChest {
			nChests++
			if nearestChest == nil || d.Distance < nearestChest.Distance {
				nearestChest = d
			}
		} else if d.IsPack {
			nPacks++
		} else if d.IsGinseng {
			nGinseng++
		}
		if !show {
			continue
		}
		rx, ry := worldToRadar(d.PosX, d.PosY)
		rxc, ryc, _ := clampToCircle(rx, ry)
		ix, iy := int32(rxc), int32(ryc)
		lbl := fmt.Sprintf("%.0f", d.Distance)
		switch {
		case d.IsChest:
			fillPoly(dc, diamondPts(ix, iy, 6), rgb(255, 210, 0), COLOR_BLACK)
			m.drawText(ix+8, iy-7, lbl, rgb(255, 235, 80))
		case d.IsPack:
			fillPoly(dc, diamondPts(ix, iy, 6), rgb(255, 140, 0), COLOR_BLACK)
			m.drawText(ix+8, iy-7, lbl, rgb(255, 180, 60))
		case d.IsGinseng:
			fillPoly(dc, diamondPts(ix, iy, 6), rgb(180, 60, 255), COLOR_BLACK)
			m.drawText(ix+8, iy-7, lbl, rgb(200, 100, 255))
		default:
			fillEllipse(dc, ix, iy, 2, rgb(70, 70, 70))
		}
	}

	// ── Linha até o baú mais próximo ────────────────────────────────────────
	if nearestChest != nil && r.ShowChests {
		rx, ry := worldToRadar(nearestChest.PosX, nearestChest.PosY)
		rxc, ryc, _ := clampToCircle(rx, ry)
		m.drawLine(cx, cy, int32(rxc), int32(ryc), rgb(140, 110, 0), 1)
	}

	// ── Rota (waypoints em coord absoluta -> local; só os dentro do radar) ──
	m.routeMu.Lock()
	rwp := m.routeWP
	m.routeMu.Unlock()
	if len(rwp) > 0 {
		ox := float32(m.originGridX) * 1024
		oy := float32(m.originGridY) * 1024
		var prx, pry float32
		have := false
		for i := 0; i < len(rwp); i++ {
			sxf, syf := worldToRadar(rwp[i][0]-ox, rwp[i][1]-oy)
			ddx := sxf - float32(cx)
			ddy := syf - float32(cy)
			if ddx*ddx+ddy*ddy > rf*rf {
				have = false
				continue
			}
			if have {
				m.drawLine(int32(prx), int32(pry), int32(sxf), int32(syf), rgb(0, 200, 255), 2)
			}
			fillEllipse(dc, int32(sxf), int32(syf), 3, rgb(90, 255, 90))
			prx, pry = sxf, syf
			have = true
		}
	}
	if r.BuildMode {
		m.drawText(cx-radius, cy-radius-16, "BUILD: clique = waypoint", rgb(255, 220, 80))
	}

	// ── Jogador (triângulo branco no centro) + borda ────────────────────────
	fillPoly(dc, []POINT{{cx, cy - 7}, {cx + 5, cy + 5}, {cx - 5, cy + 5}},
		rgb(255, 255, 255), COLOR_BLACK)
	strokeEllipse(dc, cx, cy, radius, rgb(50, 140, 50), 2)

	// ── Legenda / contadores abaixo do radar ────────────────────────────────
	ly := cy + radius + 6
	m.drawText(cx-radius, ly, fmt.Sprintf("Range %.0fm", r.RangeM), rgb(150, 200, 150))
	m.drawText(cx-radius, ly+16, fmt.Sprintf("Chests %d", nChests), rgb(255, 210, 0))
	m.drawText(cx-radius+80, ly+16, fmt.Sprintf("Packs %d", nPacks), rgb(255, 140, 0))
	m.drawText(cx-radius, ly+32, fmt.Sprintf("Ginseng %d", nGinseng), rgb(200, 100, 255))
}

// ============================================================================
// Controles públicos do radar (no Manager)
// ============================================================================

// ToggleRadar liga/desliga o radar. Ao ligar, sobe o scanner de doodads;
// ao desligar, para-o (o walk da árvore só custa enquanto o radar está visível).
func (m *Manager) ToggleRadar() bool {
	if m.radar == nil {
		return false
	}
	m.radar.Visible = !m.radar.Visible
	if m.radar.Visible {
		if m.doodadManager != nil {
			m.doodadManager.Start()
		}
	} else {
		if m.doodadManager != nil {
			m.doodadManager.Stop()
		}
	}
	return m.radar.Visible
}

func (m *Manager) IsRadarEnabled() bool {
	return m.radar != nil && m.radar.Visible
}

// SetRadarRange ajusta o alcance visual (metros) do radar.
func (m *Manager) SetRadarRange(meters float32) {
	if m.radar == nil {
		return
	}
	if meters < 50 {
		meters = 50
	}
	if meters > 2000 {
		meters = 2000
	}
	m.radar.RangeM = meters
}

func (m *Manager) GetRadarRange() float32 {
	if m.radar == nil {
		return 0
	}
	return m.radar.RangeM
}

// ============================================================================
// Route builder (click the radar to place waypoints — game coords, in-app)
// ============================================================================

// SetWaypointPlacedCallback registers the func fired when a click on the radar
// places a waypoint. It receives ABSOLUTE world coords (same frame as
// GetPlayerPositionAbsolute / the route).
func (m *Manager) SetWaypointPlacedCallback(cb func(wxAbs, wyAbs float32)) {
	m.onWaypointPlaced = cb
}

// ToggleRadarBuildMode turns the click-to-place route builder on/off (also shows
// the radar + starts the doodad scanner when enabling).
func (m *Manager) ToggleRadarBuildMode() bool {
	if m.radar == nil {
		return false
	}
	m.radar.BuildMode = !m.radar.BuildMode
	if m.radar.BuildMode && !m.radar.Visible {
		m.radar.Visible = true
		if m.doodadManager != nil {
			m.doodadManager.Start()
		}
	}
	return m.radar.BuildMode
}

func (m *Manager) IsRadarBuildMode() bool {
	return m.radar != nil && m.radar.BuildMode
}

// handleRadarClick inverse-maps a click inside the radar (build mode) to an
// ABSOLUTE world waypoint and fires the callback. Returns true if it consumed
// the click.
func (m *Manager) handleRadarClick(clickX, clickY int32) bool {
	r := m.radar
	if r == nil || !r.Visible || !r.BuildMode || !r.geoValid || r.geoScale == 0 || m.onWaypointPlaced == nil {
		return false
	}
	dx := float32(clickX - r.geoCx)
	dy := float32(clickY - r.geoCy)
	if dx*dx+dy*dy > r.geoRadius*r.geoRadius {
		return false // outside the radar circle
	}
	// Inverse of worldToRadar: sx=cx+(wx-px)*scale ; sy=cy-(wy-py)*scale.
	localX := r.geoPX + dx/r.geoScale
	localY := r.geoPY - dy/r.geoScale
	// Local -> absolute (add the seamless-origin grid), matching the route frame.
	absX := localX + float32(m.originGridX)*1024
	absY := localY + float32(m.originGridY)*1024
	m.onWaypointPlaced(absX, absY)
	return true
}
