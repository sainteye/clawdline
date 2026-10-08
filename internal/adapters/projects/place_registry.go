package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PlaceRegistryFile is this daemon's durable list of directories a person
// explicitly chose as places. It is separate from provider history: adding a
// project must not invent a Claude transcript or a Codex rollout.
const PlaceRegistryFile = "places.json"

var (
	ErrPlaceRegistryFull = errors.New("the place registry is full")
	ErrNotDirectory      = errors.New("the place is not a directory")
	ErrMachineWorkspace  = errors.New("the machine workspace is not a Project")
)

// RegisteredPlace is one explicit choice and when it was made. The time puts
// a newly registered project at the front of the same recent-first list as
// provider history and live sessions.
type RegisteredPlace struct {
	Path    string `json:"path"`
	AddedAt int64  `json:"added_at"`
}

type placeRegistryDocument struct {
	Version int               `json:"version"`
	Places  []RegisteredPlace `json:"places"`
	Hidden  []string          `json:"hidden,omitempty"`
}

// PlaceRegistry reads and changes one state directory's explicit places.
// Writes are atomic, private to the account, and never touch provider files.
type PlaceRegistry struct {
	dir     string
	foreign []string
	limit   int64
	mu      sync.Mutex
}

func OpenPlaceRegistry(dir string, foreign ...string) *PlaceRegistry {
	return &PlaceRegistry{dir: dir, foreign: foreign}
}

// SetLimit sets the capacity-register value enforced by Add and Remove.
func (r *PlaceRegistry) SetLimit(limit int64) { r.limit = limit }

func (r *PlaceRegistry) Path() string { return filepath.Join(r.dir, PlaceRegistryFile) }

// List returns the last complete registry. A malformed file is an error, not
// an empty list: losing every project because one edit is incomplete would be
// indistinguishable from somebody intentionally removing them.
func (r *PlaceRegistry) List() ([]RegisteredPlace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, err := r.readUnlocked()
	return doc.Places, err
}

// Hidden returns directories deliberately removed from the Project list.
func (r *PlaceRegistry) Hidden() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, err := r.readUnlocked()
	return doc.Hidden, err
}

// Add validates every directory before changing the file, then adds the new
// rows as one atomic change. Re-adding a path is idempotent and keeps its
// original time, so a setup script can safely be run again.
func (r *PlaceRegistry) Add(paths []string, at time.Time) ([]RegisteredPlace, error) {
	if at.Unix() <= 0 {
		return nil, errors.New("the registration time must be after the Unix epoch")
	}
	canonical, err := canonicalDirectories(paths)
	if err != nil {
		return nil, err
	}
	for _, path := range canonical {
		if IsMachineWorkspace(r.dir, path) {
			return nil, ErrMachineWorkspace
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, err := r.readUnlocked()
	if err != nil {
		return nil, err
	}
	current := doc.Places
	seen := map[string]bool{}
	for _, row := range current {
		seen[row.Path] = true
	}
	for _, path := range canonical {
		doc.Hidden = withoutPath(doc.Hidden, path)
		if !seen[path] {
			current = append(current, RegisteredPlace{Path: path, AddedAt: at.Unix()})
			seen[path] = true
		}
	}
	if r.limit > 0 && int64(len(current)+len(doc.Hidden)) > r.limit {
		return nil, fmt.Errorf("%w: %d places would exceed %d", ErrPlaceRegistryFull, len(current)+len(doc.Hidden), r.limit)
	}
	doc.Places = current
	if err := r.write(doc); err != nil {
		return nil, err
	}
	return current, nil
}

// Remove hides the named directories from every Project source without
// removing anything inside them. A directory found only in assistant history
// or a live session can be hidden too.
func (r *PlaceRegistry) Remove(paths []string) ([]RegisteredPlace, error) {
	canonical := map[string]bool{}
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		canonical[comparablePath(abs)] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	doc, err := r.readUnlocked()
	if err != nil {
		return nil, err
	}
	current := doc.Places
	kept := make([]RegisteredPlace, 0, len(current))
	for _, row := range current {
		if !canonical[comparablePath(row.Path)] {
			kept = append(kept, row)
		}
	}
	for path := range canonical {
		if !containsPath(doc.Hidden, path) {
			doc.Hidden = append(doc.Hidden, path)
		}
	}
	if r.limit > 0 && int64(len(kept)+len(doc.Hidden)) > r.limit {
		return nil, fmt.Errorf("%w: %d places would exceed %d", ErrPlaceRegistryFull, len(kept)+len(doc.Hidden), r.limit)
	}
	doc.Places = kept
	if err := r.write(doc); err != nil {
		return nil, err
	}
	return kept, nil
}

func (r *PlaceRegistry) readUnlocked() (placeRegistryDocument, error) {
	if err := r.checkDir(false); err != nil {
		return placeRegistryDocument{}, err
	}
	data, err := os.ReadFile(r.Path())
	if errors.Is(err, os.ErrNotExist) {
		return placeRegistryDocument{Version: 1, Places: []RegisteredPlace{}}, nil
	}
	if err != nil {
		return placeRegistryDocument{}, err
	}
	var doc placeRegistryDocument
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Places == nil {
		return placeRegistryDocument{}, errors.New("the place registry is not a version 1 document")
	}
	seen := map[string]bool{}
	for _, row := range doc.Places {
		if !usable(row.Path) || row.AddedAt <= 0 || seen[row.Path] {
			return placeRegistryDocument{}, errors.New("the place registry contains an invalid row")
		}
		seen[row.Path] = true
	}
	for _, path := range doc.Hidden {
		if !usable(path) || seen[path] {
			return placeRegistryDocument{}, errors.New("the place registry contains an invalid hidden path")
		}
		seen[path] = true
	}
	return doc, nil
}

func containsPath(paths []string, path string) bool {
	for _, item := range paths {
		if comparablePath(item) == comparablePath(path) {
			return true
		}
	}
	return false
}

func withoutPath(paths []string, path string) []string {
	kept := make([]string, 0, len(paths))
	for _, item := range paths {
		if comparablePath(item) != comparablePath(path) {
			kept = append(kept, item)
		}
	}
	return kept
}

func canonicalDirectories(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = resolvedPath(abs)
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			if err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrNotDirectory, path, err)
			}
			return nil, fmt.Errorf("%w: %s", ErrNotDirectory, path)
		}
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out, nil
}

func (r *PlaceRegistry) write(doc placeRegistryDocument) (err error) {
	if err := r.checkDir(true); err != nil {
		return err
	}
	stable := make([]RegisteredPlace, len(doc.Places))
	copy(stable, doc.Places)
	sort.Slice(stable, func(i, j int) bool { return stable[i].Path < stable[j].Path })
	doc.Places = stable
	sort.Strings(doc.Hidden)
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(r.dir, ".places.*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(body)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, r.Path())
	}
	if err != nil {
		return err
	}
	if dir, openErr := os.Open(r.dir); openErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (r *PlaceRegistry) checkDir(create bool) error {
	if r.dir == "" {
		return errors.New("no state directory")
	}
	for _, foreign := range r.foreign {
		if foreign != "" && (withinDirectory(r.dir, foreign) || withinDirectory(resolvedPath(r.dir), resolvedPath(foreign))) {
			return fmt.Errorf("refusing the foreign state directory %s", r.dir)
		}
	}
	if create {
		return os.MkdirAll(r.dir, 0o700)
	}
	return nil
}

func withinDirectory(path, dir string) bool {
	path, err1 := filepath.Abs(path)
	dir, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return true
	}
	return path == dir || len(path) > len(dir) && path[:len(dir)] == dir && path[len(dir)] == filepath.Separator
}
