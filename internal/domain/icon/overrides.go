package icon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

const MaxIconSide = 64
const MaxIconOverrides = 512
const MaxIconRequestBytes = 96 << 10

var ErrIconChanged = errors.New("the project icon changed; reload before applying")
var ErrIconCapacity = errors.New("the saved project icon registry is full")
var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Validate accepts only a bounded raster of literal colours. No URLs, SVG,
// source paths, labels or executable content travel with an icon.
func Validate(g Grid) error {
	if !hexColor.MatchString(g.Accent) || len(g.Cells) == 0 || len(g.Cells) > MaxIconSide {
		return errors.New("an icon needs an RGB accent and 1 to 64 rows")
	}
	width := len(g.Cells[0])
	if width == 0 || width > MaxIconSide {
		return errors.New("an icon needs 1 to 64 columns")
	}
	for _, row := range g.Cells {
		if len(row) != width {
			return errors.New("icon rows must have equal widths")
		}
		for _, c := range row {
			if c != nil && !hexColor.MatchString(*c) {
				return errors.New("icon cells must be RGB colours or null")
			}
		}
	}
	return nil
}

// NewRegistryWithOverrides keeps copied marks in this daemon's state directory.
// The imported legacy registry remains read-only. An unreadable override file
// refuses startup instead of silently losing a person's saved marks.
func NewRegistryWithOverrides(dir string) (*Registry, error) {
	r := NewRegistry()
	r.overridePath = filepath.Join(dir, "project-icon-overrides.json")
	r.overrides = map[string]Grid{}
	data, err := os.ReadFile(r.overridePath)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &r.overrides); err != nil {
		return nil, err
	}
	if r.overrides == nil {
		r.overrides = map[string]Grid{}
	}
	if len(r.overrides) > MaxIconOverrides {
		return nil, ErrIconCapacity
	}
	for path, grid := range r.overrides {
		if !filepath.IsAbs(path) {
			return nil, errors.New("saved icon path is not absolute")
		}
		if err := Validate(grid); err != nil {
			return nil, fmt.Errorf("invalid saved icon: %w", err)
		}
	}
	return r, nil
}

func (r *Registry) override(cwd string) (Grid, bool) {
	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()
	return r.overrideLocked(cwd)
}

func (r *Registry) overrideLocked(cwd string) (Grid, bool) {
	best := ""
	var result Grid
	cwd = filepath.ToSlash(filepath.Clean(cwd))
	for path, grid := range r.overrides {
		path = filepath.ToSlash(filepath.Clean(path))
		if (cwd == path || strings.HasPrefix(cwd, strings.TrimSuffix(path, "/")+"/")) && len(path) > len(best) {
			best, result = path, grid
		}
	}
	return result, best != ""
}

// Save compares the displayed target mark before replacing it. Repeating an
// identical write succeeds, including after a lost reply. Rename publishes the
// whole registry only after the private temporary file has been synced.
func (r *Registry) Save(path string, grid, expected Grid) error {
	if err := Validate(grid); err != nil {
		return err
	}
	if err := Validate(expected); err != nil {
		return err
	}
	if r.overridePath == "" || !filepath.IsAbs(path) {
		return errors.New("icon storage is unavailable")
	}
	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()
	current, ok := r.overrideLocked(path)
	if !ok {
		current = r.legacyFor(path)
	}
	if saved, exists := r.overrides[path]; exists && reflect.DeepEqual(saved, grid) {
		return nil
	}
	if !reflect.DeepEqual(current, expected) {
		return ErrIconChanged
	}
	if _, exists := r.overrides[path]; !exists && len(r.overrides) >= MaxIconOverrides {
		return ErrIconCapacity
	}
	next := make(map[string]Grid, len(r.overrides)+1)
	for k, v := range r.overrides {
		next[k] = v
	}
	next[path] = grid
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.overridePath), ".project-icons-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), r.overridePath); err != nil {
		return err
	}
	// Detach the cache from caller-owned slices and colour pointers.
	if err = json.Unmarshal(data, &next); err != nil {
		return err
	}
	r.overrides = next
	return nil
}

// SavedCount is the evidence retained by this registry, for capacity diagnostics.
func (r *Registry) SavedCount() int64 {
	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()
	return int64(len(r.overrides))
}
