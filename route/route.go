package route

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ============================================================================
// Route persistence — routes are saved as JSON under <exe-dir>/routes/<name>.json.
// Waypoints are stored in ABSOLUTE world coords (local + seamless origin), so a
// route is invariant across the game's floating-origin rebases on reload.
// ============================================================================

// Point is one waypoint in absolute world coords (X=east, Y=north, Z=height).
type Point struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

// Route is a named sequence of waypoints.
type Route struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

// Dir returns a STABLE routes directory that survives rebuilds and moving the
// exe: %APPDATA%\winkit\routes (falling back to %LOCALAPPDATA%, then next to the
// exe). Saving next to the exe is unsafe because the build lives in dist\, which
// the build script wipes (Remove-Item -Recurse) on every rebuild.
func Dir() string {
	var base string
	if ad := os.Getenv("APPDATA"); ad != "" {
		base = filepath.Join(ad, "winkit")
	} else if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
		base = filepath.Join(lad, "winkit")
	} else if exe, err := os.Executable(); err == nil {
		base = filepath.Dir(exe)
	} else {
		base = "."
	}
	d := filepath.Join(base, "routes")
	os.MkdirAll(d, 0755)
	return d
}

// sanitize strips path separators / unsafe chars from a route name.
func sanitize(name string) string {
	name = strings.TrimSpace(name)
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_").Replace(name)
	return name
}

func pathFor(name string) string {
	return filepath.Join(Dir(), sanitize(name)+".json")
}

// FromXYZ builds a Route from a slice of [X,Y,Z] absolute points.
func FromXYZ(name string, pts [][3]float32) Route {
	r := Route{Name: name, Points: make([]Point, len(pts))}
	for i, p := range pts {
		r.Points[i] = Point{X: p[0], Y: p[1], Z: p[2]}
	}
	return r
}

// Decimate simplifies a dense polyline using the Ramer–Douglas–Peucker
// algorithm on the 2D (X,Y) plane. The perpendicular-distance test uses X,Y
// only; the Z of each kept point is carried through unchanged. The first and
// last points are always preserved. Slices shorter than 3 points are returned
// as-is. epsilon is the maximum perpendicular deviation (in world units) a
// point may have from the simplified segment before it must be kept.
func Decimate(pts [][3]float32, epsilon float32) [][3]float32 {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0] = true
	keep[len(pts)-1] = true
	rdp(pts, 0, len(pts)-1, epsilon, keep)
	out := make([][3]float32, 0, len(pts))
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// rdp is the recursive Ramer–Douglas–Peucker step: within the inclusive index
// range [lo,hi] it keeps the point farthest from the lo→hi segment when that
// distance exceeds epsilon, then recurses on the two sub-segments.
func rdp(pts [][3]float32, lo, hi int, epsilon float32, keep []bool) {
	if hi <= lo+1 {
		return
	}
	var (
		maxDist float32
		idx     int
	)
	for i := lo + 1; i < hi; i++ {
		if d := perpDist2D(pts[i], pts[lo], pts[hi]); d > maxDist {
			maxDist = d
			idx = i
		}
	}
	if maxDist > epsilon {
		keep[idx] = true
		rdp(pts, lo, idx, epsilon, keep)
		rdp(pts, idx, hi, epsilon, keep)
	}
}

// perpDist2D returns the perpendicular distance from point p to the line
// through a and b, using X,Y only (Z ignored).
func perpDist2D(p, a, b [3]float32) float32 {
	ax, ay := float64(a[0]), float64(a[1])
	bx, by := float64(b[0]), float64(b[1])
	px, py := float64(p[0]), float64(p[1])
	dx, dy := bx-ax, by-ay
	segLen := math.Hypot(dx, dy)
	if segLen == 0 {
		// a and b coincide: fall back to the straight-line distance p→a.
		return float32(math.Hypot(px-ax, py-ay))
	}
	// |(b-a) × (a-p)| / |b-a|
	cross := math.Abs(dx*(ay-py) - dy*(ax-px))
	return float32(cross / segLen)
}

// XYZ returns the route's waypoints as [X,Y,Z] tuples.
func (r Route) XYZ() [][3]float32 {
	out := make([][3]float32, len(r.Points))
	for i, p := range r.Points {
		out[i] = [3]float32{p.X, p.Y, p.Z}
	}
	return out
}

// Save writes the route to <routes>/<name>.json.
func Save(r Route) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("route name vazio")
	}
	if len(r.Points) == 0 {
		return fmt.Errorf("rota sem waypoints")
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pathFor(r.Name), data, 0644)
}

// Load reads <routes>/<name>.json.
func Load(name string) (Route, error) {
	var r Route
	data, err := os.ReadFile(pathFor(name))
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Name == "" {
		r.Name = name
	}
	return r, nil
}

// List returns the names of all saved routes (sorted).
func List() []string {
	matches, _ := filepath.Glob(filepath.Join(Dir(), "*.json"))
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		names = append(names, strings.TrimSuffix(base, ".json"))
	}
	sort.Strings(names)
	return names
}

// Delete removes a saved route.
func Delete(name string) error {
	return os.Remove(pathFor(name))
}
