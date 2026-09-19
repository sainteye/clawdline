// Package icon derives a project's mark from its path.
//
// This is a port of the Swift app's ProjectIcon, and it is a port rather than a
// new design because the mark has to be the same one. A project a person knows
// by its colour and shape is not recognisable if the second app draws it
// differently, and "close enough" is the worst outcome: near-identical marks
// are harder to tell apart than obviously different ones.
//
// Everything here is deterministic from the path plus an optional registry
// file, so both apps reach the same answer without either asking the other.
package icon

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
)

// Grid is a mark: rows of colours, with nil for transparent.
type Grid struct {
	Accent string
	Cells  [][]*string
}

// hues are the sixteen the generator picks between, in degrees.
var hues = []float64{8, 28, 45, 62, 85, 118, 145, 165, 188, 205, 222, 242, 262, 282, 305, 330}

// tones is (body saturation, body lightness, limb saturation, limb lightness).
var tones = [][4]float64{
	{0.66, 0.62, 0.62, 0.44},
	{0.38, 0.80, 0.34, 0.62},
}

// StableHash is FNV-1a, kept because Swift's own hashValue is seeded per
// process: the same project would change colour every time the app started.
func StableHash(text string) int64 {
	var h uint64 = 0xcbf29ce484222325
	for _, b := range []byte(text) {
		h = (h ^ uint64(b)) * 0x100000001b3
	}
	return int64(h % 0x7fffffff)
}

// creatureCells is 5 wide and 4 tall, mirrored. `ears` and `legs` are three
// pixels each (1…7, never empty) and `eye` picks which row the eyes sit in, so
// 7 × 7 × 2 = 98 shapes.
func creatureCells(shape int) [][]int {
	type s struct{ ears, legs, eye int }
	shapes := make([]s, 0, 98)
	for e := 1; e <= 7; e++ {
		for l := 1; l <= 7; l++ {
			shapes = append(shapes, s{e, l, 0}, s{e, l, 1})
		}
	}
	pick := shapes[((shape%len(shapes))+len(shapes))%len(shapes)]
	grid := make([][]int, 4)
	for i := range grid {
		grid[i] = make([]int, 5)
	}
	for c := 0; c < 3; c++ {
		if pick.ears>>c&1 == 1 {
			grid[0][c] = 2
			grid[0][4-c] = 2
		}
		if pick.legs>>c&1 == 1 {
			grid[3][c] = 2
			grid[3][4-c] = 2
		}
	}
	for r := 1; r <= 2; r++ {
		for c := 0; c < 5; c++ {
			grid[r][c] = 1
		}
	}
	eyeRow := 2
	if pick.eye != 0 {
		eyeRow = 1
	}
	grid[eyeRow][1] = 0
	grid[eyeRow][3] = 0
	return grid
}

func creature(hueIndex, toneIndex, shape int) Grid {
	h := hues[((hueIndex%len(hues))+len(hues))%len(hues)]
	t := tones[((toneIndex%len(tones))+len(tones))%len(tones)]
	body := hsl(h, t[0], t[1])
	limb := hsl(h, t[2], t[3])
	src := creatureCells(shape)
	cells := make([][]*string, len(src))
	for y, row := range src {
		cells[y] = make([]*string, len(row))
		for x, v := range row {
			switch v {
			case 1:
				c := body
				cells[y][x] = &c
			case 2:
				c := limb
				cells[y][x] = &c
			}
		}
	}
	return Grid{Accent: body, Cells: cells}
}

func creatureFromSeed(seed int64) Grid {
	return creature(int(seed%int64(len(hues))), int((seed/16)%int64(len(tones))), int((seed/512)%98))
}

// hsl converts to an sRGB hex string. `#RRGGBB` is a promise about sRGB, which
// is why the conversion is spelled out rather than left to a colour object.
func hsl(hDegrees, s, l float64) string {
	h := hDegrees / 360
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch int(h*6) % 6 {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return hex(r+m, g+m, b+m)
}

func hex(r, g, b float64) string {
	to := func(v float64) int {
		n := int(math.Round(v * 255))
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return n
	}
	return fmt.Sprintf("#%02X%02X%02X", to(r), to(g), to(b))
}

// ---------- registry ----------

type entry struct {
	Hue   *int            `json:"hue"`
	Tone  *int            `json:"tone"`
	Shape *int            `json:"shape"`
	Label string          `json:"label"`
	Emoji string          `json:"emoji"`
	Art   json.RawMessage `json:"art"`
}

type art struct {
	Accent  string            `json:"accent"`
	BG      string            `json:"bg"`
	Palette map[string]string `json:"palette"`
	Rows    []string          `json:"rows"`
}

// Registry is the machine's record of which mark belongs to which project.
//
// The file is shared with the Swift app rather than copied, because the marks
// are a fact about this machine's projects and two copies would drift the first
// time somebody edited one. Reading it is all this does; it is never written.
type Registry struct {
	mu       sync.Mutex
	path     string
	stamp    string
	projects map[string]entry
}

func NewRegistry() *Registry {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Registry{}
	}
	return &Registry{path: home + "/.claude/project-icons.json"}
}

func (r *Registry) load() map[string]entry {
	if r.path == "" {
		return nil
	}
	st, err := os.Stat(r.path)
	if err != nil {
		return nil
	}
	// Through the symlink deliberately: this file is normally a link into a
	// checkout, and stat'ing the link gives a number that never changes.
	stamp := fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stamp == stamp && r.projects != nil {
		return r.projects
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return r.projects
	}
	var root struct {
		Projects map[string]entry `json:"projects"`
	}
	if json.Unmarshal(data, &root) != nil {
		return r.projects
	}
	r.projects = root.Projects
	r.stamp = stamp
	return r.projects
}

// For returns the mark for a working directory.
//
// The registry row that wins is the longest registered path containing it, not
// an exact match: a session sits in `shop/frontend` while the entry naming
// the project is `shop` — and `shop/backend` may have a row of its own,
// which should win for anything inside it.
func (r *Registry) For(cwd string) Grid {
	if cwd == "" {
		return Grid{Cells: [][]*string{}}
	}
	best := ""
	var bestRow entry
	for path, row := range r.load() {
		if cwd == path || strings.HasPrefix(cwd, path+"/") {
			if len(path) > len(best) {
				best, bestRow = path, row
			}
		}
	}
	if best != "" {
		if g, ok := fromEntry(bestRow); ok {
			return g
		}
	}
	// No registry, or a project it has never seen. The path still decides the
	// colour, so it is at least stable from one launch to the next.
	return creatureFromSeed(StableHash(cwd))
}

// Label returns the name the registry gives a directory, if it gives one.
func (r *Registry) Label(cwd string) string {
	best := ""
	var bestRow entry
	for path, row := range r.load() {
		if cwd == path || strings.HasPrefix(cwd, path+"/") {
			if len(path) > len(best) {
				best, bestRow = path, row
			}
		}
	}
	if best == "" {
		return ""
	}
	return bestRow.Label
}

func fromEntry(e entry) (Grid, bool) {
	if len(e.Art) > 0 {
		var a art
		if json.Unmarshal(e.Art, &a) == nil {
			if g, ok := artGrid(a); ok {
				return g, true
			}
		}
	}
	if e.Hue == nil {
		return Grid{}, false
	}
	tone, shape := 0, 0
	if e.Tone != nil {
		tone = *e.Tone
	}
	if e.Shape != nil {
		shape = *e.Shape
	}
	return creature(*e.Hue, tone, shape), true
}

// artGrid renders a hand-drawn mark.
//
// Rows need not be the same length; they are padded. A dot, a space or a middle
// dot is the background, and an unknown character is too — an icon with a typo
// in it should come out slightly wrong, not blank.
func artGrid(a art) (Grid, bool) {
	if len(a.Rows) == 0 {
		return Grid{}, false
	}
	look := map[rune]string{}
	for key, value := range a.Palette {
		for _, ch := range key {
			look[ch] = value
			break
		}
	}
	width := 0
	rows := make([][]rune, len(a.Rows))
	for i, row := range a.Rows {
		rows[i] = []rune(row)
		if len(rows[i]) > width {
			width = len(rows[i])
		}
	}
	cells := make([][]*string, len(rows))
	for y, row := range rows {
		cells[y] = make([]*string, width)
		for x := 0; x < width; x++ {
			ch := ' '
			if x < len(row) {
				ch = row[x]
			}
			var chosen string
			if strings.ContainsRune(".· ", ch) {
				chosen = a.BG
			} else if c, ok := look[ch]; ok {
				chosen = c
			} else {
				chosen = a.BG
			}
			if chosen == "" {
				continue
			}
			c := chosen
			cells[y][x] = &c
		}
	}
	accent := a.Accent
	if accent == "" {
		for _, v := range a.Palette {
			accent = v
			break
		}
	}
	if accent == "" {
		accent = "#FFFFFF"
	}
	return Grid{Accent: accent, Cells: cells}, true
}
