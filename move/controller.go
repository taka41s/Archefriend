package move

import (
	"fmt"
	"math"
	"sync"

	"winkit/input"
)

// ============================================================================
// WASD movement controller — drive to a point or follow a route.
//
// Ported from the C++ route_follower.cpp steering core. Works in ABSOLUTE world
// coords (local + seamless origin), so it's continuous across floating-origin
// rebases. Output (hold/release key) is injected so movement goes via
// PostMessage-to-window (accepted by the game where SendInput is filtered).
// Held keys are re-asserted every tick (the game needs continuous WM_KEYDOWN).
//
// Horizontal plane is (X=east, Y=north); Z=height is ignored for steering.
// Feed Tick() the (x, y) from esp.GetPlayerPositionAbsolute(). Heading is
// inferred from recent position deltas.
//
// Steering uses a pure-pursuit CARROT rather than aiming straight at the raw
// waypoint: the player is projected onto the current path segment and the aim
// point is placed kLookahead meters further along that segment (clamped to the
// waypoint). This leads curves early (decisive turns) and cancels cross-track
// error. A single code path handles both dense (~1m) and sparse routes — the
// carrot self-scales, no dense/sparse flag needed.
// ============================================================================

// KeyBindings maps the four movement directions to virtual-key codes.
type KeyBindings struct {
	Forward, Back, Left, Right uint16
}

// DefaultKeys returns WASD.
func DefaultKeys() KeyBindings {
	return KeyBindings{Forward: input.VK_W, Back: input.VK_S, Left: input.VK_A, Right: input.VK_D}
}

type state int

const (
	stIdle state = iota
	stMoving
	stReversing
)

// Tuning constants (kept from route_follower.cpp; ticks assume ~16ms/tick).
const (
	kArriveRadius = 4.5  // meters — reached a waypoint (bumped 3->4.5)
	kTrackBand    = 10.0 // meters — advance-by-passing only when this close to the path
	kTurnDeg      = 20.0
	kMinMoveMagSq = 0.01

	// Pure-pursuit carrot: aim this far along the current segment ahead of the
	// player's projection onto it (clamped to the segment's end waypoint).
	kLookahead = 10.0

	kHistoryTicks = 15

	kFreezeSampleTicks    = 60
	kFreezeMoveThreshold  = 0.15
	kFrozenSamplesToStuck = 5

	// Stuck-escape (angled detour). First stuck at a spot = a short straight
	// reverse (a transient snag). Stuck AGAIN near the same waypoint (or a
	// distance-progress stall) escalates to an angled detour: reverse a bit
	// while holding a turn key, alternating left/right on successive stucks so
	// it doesn't re-approach the same obstacle the same way and loop.
	kReverseShort    = 2.5  // meters — straight reverse, first attempt
	kReverseAngled   = 3.0  // meters — angled detour
	kReverseMaxTicks = 1875 // ~30s cap (safety; normally exits on distance)
	kStuckSameSpot   = 20   // index window that counts as "the same spot"

	// Distance-progress backstop (scale-free): if the distance to the current
	// waypoint hasn't improved by more than kProgressEps within
	// kProgressWindowTicks, we're grinding a wall (sliding just enough that the
	// hard freeze detector never fires) — force an angled detour.
	kProgressEps         = 2.0 // meters of net closing required within the window
	kProgressWindowTicks = 500 // ~8s at 16ms/tick

	// 1024-zone re-sync: a whole-multiple-of-1024 deviation with a small
	// residual is a floating-origin rebase, not being lost.
	kZoneUnit     = 1024.0
	kZoneResidual = 45.0 // meters — max residual for a deviation to count as a rebase

	kMaxAdvancePerTick = 8
)

// Controller is safe for concurrent Start/Stop vs Tick — all take c.mu.
type Controller struct {
	mu   sync.Mutex
	keys KeyBindings

	hold    func(vk uint16)
	release func(vk uint16)

	active bool
	st     state

	route [][3]float32 // ABSOLUTE waypoints; a single-point GoTo is a 1-elem route
	idx   int

	forwardHeld bool
	backHeld    bool
	turnKeyHeld uint16

	history [][2]float32

	// Persistent coordinate-frame offset, added to every incoming position.
	// Starts at 0 and absorbs any whole-1024 zone rebase detected mid-route so
	// the live position stays in the recorded route's frame.
	frameOffX float32
	frameOffY float32

	freezeTick   int
	frozen       int
	freezeLastX  float32
	freezeLastY  float32
	freezeInited bool

	// Distance-progress backstop tracking (per current waypoint).
	progInited bool
	progIdx    int
	progBest   float32 // smallest distance-to-current-waypoint seen this window
	progTick   int     // ticks since the last >kProgressEps improvement

	// Stuck-escape escalation state.
	lastStuckIdx    int
	reverseAttempts int
	stuckTurnToggle bool // alternates the angled-detour bias L/R

	reverseTicks   int
	reverseStartX  float32
	reverseStartY  float32
	reverseGoal    float32
	reverseTurnKey uint16 // 0 = straight reverse, else a turn key held during reverse

	logTick int
}

// New builds a controller. hold(vk)/release(vk) are the key-output funcs.
func New(keys KeyBindings, hold, release func(vk uint16)) *Controller {
	return &Controller{keys: keys, hold: hold, release: release}
}

// GoTo drives to a single absolute point.
func (c *Controller) GoTo(x, y float32) {
	c.startRoute([][3]float32{{x, y, 0}})
}

// FollowRoute drives through a sequence of absolute waypoints.
func (c *Controller) FollowRoute(pts [][3]float32) {
	if len(pts) == 0 {
		return
	}
	cp := make([][3]float32, len(pts))
	copy(cp, pts)
	c.startRoute(cp)
}

func (c *Controller) startRoute(pts [][3]float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseAllLocked()
	c.route = pts
	c.idx = 0
	c.st = stMoving
	c.active = true
	c.history = c.history[:0]
	c.frameOffX, c.frameOffY = 0, 0
	c.freezeTick, c.frozen = 0, 0
	c.freezeInited = false
	c.progInited = false
	c.progIdx = -1
	c.progTick = 0
	c.lastStuckIdx = -1000
	c.reverseAttempts = 0
	c.stuckTurnToggle = false
	c.reverseTicks = 0
	c.reverseGoal = 0
	c.reverseTurnKey = 0
	c.logTick = 0
	fmt.Printf("[MOVE] start route: %d waypoints\n", len(pts))
}

// Stop halts movement and releases all keys.
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		fmt.Println("[MOVE] stop")
	}
	c.releaseAllLocked()
	c.active = false
	c.st = stIdle
}

func (c *Controller) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// Progress returns (currentWaypoint, total). 0,0 if idle.
func (c *Controller) Progress() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return 0, 0
	}
	return c.idx + 1, len(c.route)
}

func (c *Controller) releaseAllLocked() {
	if c.forwardHeld {
		c.release(c.keys.Forward)
		c.forwardHeld = false
	}
	if c.backHeld {
		c.release(c.keys.Back)
		c.backHeld = false
	}
	if c.turnKeyHeld != 0 {
		c.release(c.turnKeyHeld)
		c.turnKeyHeld = 0
	}
}

// Tick advances the controller one step. px/py = player ABSOLUTE position
// (X=east, Y=north). Call at ~60Hz (16ms).
func (c *Controller) Tick(px, py float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || len(c.route) == 0 {
		return
	}

	// Apply the persistent frame offset (absorbs any detected 1024 rebase) so
	// the live position is in the recorded route's coordinate frame.
	px += c.frameOffX
	py += c.frameOffY

	// --- Reverse / angled-detour recovery: back up reverseGoal meters, optionally
	// holding a turn key for an angled escape. No forward steering. ---
	if c.st == stReversing {
		dx := px - c.reverseStartX
		dy := py - c.reverseStartY
		if dx*dx+dy*dy >= c.reverseGoal*c.reverseGoal || c.reverseTicks >= kReverseMaxTicks {
			c.releaseAllLocked()
			c.st = stMoving
			c.history = c.history[:0]
			c.resetProgressLocked()
			return
		}
		c.reverseTicks++
		c.backHeld = true
		c.hold(c.keys.Back)
		if c.reverseTurnKey != 0 {
			c.turnKeyHeld = c.reverseTurnKey
			c.hold(c.reverseTurnKey)
		}
		return
	}

	// --- Per-tick 1024 offset re-sync (BEFORE advancement). A whole-multiple-of-
	// 1024 deviation from the current waypoint with a small residual is a
	// floating-origin zone rebase mid-segment (the cached origin is momentarily
	// 1024 off), not being lost — fold it into the persistent frame offset so the
	// cart snaps right back onto the route. (We deliberately do NOT treat a plain
	// >200m gap as a desync jump — that's a normal sparse chord.) ---
	{
		devX := px - c.route[c.idx][0]
		devY := py - c.route[c.idx][1]
		corrX := float32(math.Round(float64(devX)/kZoneUnit)) * kZoneUnit
		corrY := float32(math.Round(float64(devY)/kZoneUnit)) * kZoneUnit
		if (corrX != 0 || corrY != 0) &&
			math.Abs(float64(devX-corrX)) < kZoneResidual &&
			math.Abs(float64(devY-corrY)) < kZoneResidual {
			c.frameOffX -= corrX
			c.frameOffY -= corrY
			px -= corrX
			py -= corrY
			c.history = c.history[:0]
			c.resetProgressLocked()
			fmt.Printf("[MOVE] zone re-sync by (%.0f,%.0f) at wp %d\n", -corrX, -corrY, c.idx)
		}
	}

	// --- Waypoint advancement (arrive OR pass while near the path). Unchanged. ---
	arriveSq := float32(kArriveRadius * kArriveRadius)
	bandSq := float32(kTrackBand * kTrackBand)
	for adv := 0; adv < kMaxAdvancePerTick && c.idx < len(c.route)-1; adv++ {
		cdx := c.route[c.idx][0] - px
		cdy := c.route[c.idx][1] - py
		dCur := cdx*cdx + cdy*cdy
		if dCur < arriveSq {
			c.idx++
			continue
		}
		if dCur < bandSq {
			ndx := c.route[c.idx+1][0] - px
			ndy := c.route[c.idx+1][1] - py
			if ndx*ndx+ndy*ndy < dCur {
				c.idx++
				continue
			}
		}
		break
	}

	// --- Completion: reached the final waypoint. ---
	last := len(c.route) - 1
	fdx := c.route[last][0] - px
	fdy := c.route[last][1] - py
	if c.idx >= last && fdx*fdx+fdy*fdy < arriveSq {
		fmt.Println("[MOVE] route complete")
		c.releaseAllLocked()
		c.active = false
		c.st = stIdle
		return
	}

	// --- Steering target = pure-pursuit carrot along the current segment. ---
	tx, ty := c.carrotLocked(px, py)
	dx := tx - px
	dy := ty - py

	// --- Freeze detection (hard dead-stop; kept as-is). ---
	c.freezeTick++
	if c.freezeTick >= kFreezeSampleTicks {
		c.freezeTick = 0
		if !c.freezeInited {
			c.freezeLastX, c.freezeLastY = px, py
			c.freezeInited = true
			c.frozen = 0
		} else {
			mdx := px - c.freezeLastX
			mdy := py - c.freezeLastY
			c.freezeLastX, c.freezeLastY = px, py
			if mdx*mdx+mdy*mdy >= kFreezeMoveThreshold*kFreezeMoveThreshold {
				c.frozen = 0
			} else {
				c.frozen++
				if c.frozen >= kFrozenSamplesToStuck {
					c.frozen = 0
					fmt.Printf("[MOVE] frozen at wp %d -> reverse recovery\n", c.idx)
					c.enterReverseLocked(px, py, false)
					return
				}
			}
		}
	}

	// --- Distance-progress backstop (scale-free wall-grind catcher). ---
	wpdx := c.route[c.idx][0] - px
	wpdy := c.route[c.idx][1] - py
	dWp := float32(math.Sqrt(float64(wpdx*wpdx + wpdy*wpdy)))
	if !c.progInited || c.idx != c.progIdx {
		c.progInited = true
		c.progIdx = c.idx
		c.progBest = dWp
		c.progTick = 0
	} else if dWp < c.progBest-kProgressEps {
		c.progBest = dWp
		c.progTick = 0
	} else {
		if dWp < c.progBest {
			c.progBest = dWp
		}
		c.progTick++
		if c.progTick >= kProgressWindowTicks {
			fmt.Printf("[MOVE] no distance progress at wp %d -> angled detour\n", c.idx)
			c.enterReverseLocked(px, py, true)
			return
		}
	}

	// --- Drive forward (re-assert every tick for PostMessage auto-repeat). ---
	c.forwardHeld = true
	c.hold(c.keys.Forward)

	// --- Proportional steering: heading from ~240ms of position history, aimed
	// at the carrot. HOLD left/right continuously while off-course, release when
	// aligned. ---
	c.history = append(c.history, [2]float32{px, py})
	if len(c.history) > kHistoryTicks {
		c.history = c.history[1:]
	}

	var wantKey uint16
	if len(c.history) >= kHistoryTicks {
		oldX, oldY := c.history[0][0], c.history[0][1]
		mx := px - oldX
		my := py - oldY
		if mx*mx+my*my > kMinMoveMagSq {
			cross := mx*dy - my*dx
			dot := mx*dx + my*dy
			angleOff := math.Atan2(math.Abs(float64(cross)), float64(dot))
			if angleOff > kTurnDeg*math.Pi/180.0 {
				if cross > 0 {
					wantKey = c.keys.Left
				} else {
					wantKey = c.keys.Right
				}
			}
		}
	}

	if wantKey != 0 {
		if c.turnKeyHeld != wantKey && c.turnKeyHeld != 0 {
			c.release(c.turnKeyHeld)
		}
		c.turnKeyHeld = wantKey
		c.hold(wantKey)
	} else if c.turnKeyHeld != 0 {
		c.release(c.turnKeyHeld)
		c.turnKeyHeld = 0
	}

	// Throttled diagnostic (~3x/sec).
	c.logTick++
	if c.logTick >= 20 {
		c.logTick = 0
		turn := "-"
		if c.turnKeyHeld == c.keys.Left {
			turn = "L"
		} else if c.turnKeyHeld == c.keys.Right {
			turn = "R"
		}
		fmt.Printf("[MOVE] wp %d/%d pos=(%.1f,%.1f) carrot=(%.1f,%.1f) wpDist=%.1f turn=%s\n",
			c.idx+1, len(c.route), px, py, tx, ty, dWp, turn)
	}
}

// carrotLocked returns the pure-pursuit aim point: the point kLookahead meters
// ahead of the player ALONG THE POLYLINE (accumulating arc-length across
// waypoints), not just within the current segment. This is what makes it work
// on dense (~1m) routes — a per-segment carrot clamped to route[idx] would sit
// ~1m ahead and jump every tick as idx advances, which oscillates (zig-zag). By
// walking ~kLookahead m forward over many waypoints the aim point is a stable,
// distant target (like the single-point prototype) so steering is smooth.
// Caller holds c.mu.
func (c *Controller) carrotLocked(px, py float32) (float32, float32) {
	n := len(c.route)

	// Starting point on the path = the player's projection onto the current
	// segment [route[idx-1] -> route[idx]] (or [player -> route[0]] for idx 0).
	var sx, sy float32
	if c.idx == 0 {
		sx, sy = px, py
	} else {
		sx, sy = c.route[c.idx-1][0], c.route[c.idx-1][1]
	}
	ex, ey := c.route[c.idx][0], c.route[c.idx][1]
	segX, segY := ex-sx, ey-sy
	segLenSq := segX*segX + segY*segY
	var t float32
	if segLenSq > 1e-6 {
		t = ((px-sx)*segX + (py-sy)*segY) / segLenSq
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	ax, ay := sx+t*segX, sy+t*segY // current point on the path

	// Walk forward toward route[idx], route[idx+1], ... consuming kLookahead.
	remaining := float32(kLookahead)
	i := c.idx
	for {
		bx, by := c.route[i][0], c.route[i][1]
		dx, dy := bx-ax, by-ay
		d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if d >= remaining {
			if d < 1e-6 {
				return bx, by
			}
			f := remaining / d
			return ax + f*dx, ay + f*dy
		}
		remaining -= d
		ax, ay = bx, by
		i++
		if i >= n {
			return ax, ay // ran out of path -> aim at the last waypoint
		}
	}
}

func (c *Controller) resetProgressLocked() {
	c.progInited = false
	c.progIdx = -1
	c.progTick = 0
}

// enterReverseLocked starts a recovery reverse. forceAngled (used by the
// distance-progress backstop) goes straight to an angled detour; otherwise the
// first stuck at a spot does a short straight reverse and a REPEAT stuck near
// the same waypoint escalates to an angled detour, alternating the L/R bias.
func (c *Controller) enterReverseLocked(px, py float32, forceAngled bool) {
	diff := c.idx - c.lastStuckIdx
	if diff < 0 {
		diff = -diff
	}
	if diff < kStuckSameSpot {
		c.reverseAttempts++
	} else {
		c.reverseAttempts = 1
	}
	c.lastStuckIdx = c.idx

	angled := forceAngled || c.reverseAttempts >= 2

	c.releaseAllLocked()
	c.st = stReversing
	c.reverseTicks = 0
	c.reverseStartX, c.reverseStartY = px, py
	c.frozen = 0
	c.resetProgressLocked()

	if angled {
		if c.stuckTurnToggle {
			c.reverseTurnKey = c.keys.Left
		} else {
			c.reverseTurnKey = c.keys.Right
		}
		c.stuckTurnToggle = !c.stuckTurnToggle
		c.reverseGoal = kReverseAngled
	} else {
		c.reverseTurnKey = 0
		c.reverseGoal = kReverseShort
	}
}
