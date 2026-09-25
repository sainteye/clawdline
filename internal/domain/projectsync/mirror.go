package projectsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

// MirrorFile is the mirror's record under the daemon's state directory.
const MirrorFile = "project-mirror.json"

// ErrMirrorCapacity is a new repository arriving at a full mirror.
var ErrMirrorCapacity = errors.New("this machine already mirrors the most repositories it keeps")

// Source names the machine a mirror reads from. Machine is the Cloud machine
// id when the settings arrived through Cloud, and empty for a file import.
type Source struct {
	Machine string `json:"machine"`
	Name    string `json:"name"`
}

// Record is one repository this machine mirrors: where its checkout is here,
// who owns its settings, and what was last applied. Files maps each file this
// mirror wrote to the hash it wrote, which is how a later apply tells a file
// it may replace from one somebody here has since edited.
type Record struct {
	Repo      string            `json:"repo"`
	Path      string            `json:"path"`
	Source    Source            `json:"source"`
	Revision  string            `json:"revision"`
	Label     string            `json:"label"`
	Icon      icon.Grid         `json:"icon"`
	Files     map[string]string `json:"files"`
	AppliedAt int64             `json:"applied_at"`
}

// Mirror is this machine's set of mirrored repositories. One daemon owns it;
// writes are serialized, synced to a private temporary file and renamed.
type Mirror struct {
	mu      sync.Mutex
	path    string
	records map[string]Record
}

// OpenMirror reads the saved mirror. A missing file is an empty mirror; an
// unreadable or invalid one is an error, because treating it as empty would
// quietly turn every mirrored project back into a local one.
func OpenMirror(dir string) (*Mirror, error) {
	m := &Mirror{path: filepath.Join(dir, MirrorFile), records: map[string]Record{}}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var saved struct {
		Version  int      `json:"version"`
		Projects []Record `json:"projects"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("unreadable %s: %w", MirrorFile, err)
	}
	if saved.Version != Version {
		return nil, fmt.Errorf("%s has version %d; this build reads %d", MirrorFile, saved.Version, Version)
	}
	if len(saved.Projects) > MaxMirrorRecords {
		return nil, ErrMirrorCapacity
	}
	for _, r := range saved.Projects {
		if !ValidRepo(r.Repo) || !filepath.IsAbs(r.Path) {
			return nil, fmt.Errorf("%s holds an invalid record for %q", MirrorFile, r.Repo)
		}
		if err := icon.Validate(r.Icon); err != nil {
			return nil, fmt.Errorf("%s holds an invalid icon for %q: %w", MirrorFile, r.Repo, err)
		}
		m.records[r.Repo] = r
	}
	return m, nil
}

// Get is the record for one repository.
func (m *Mirror) Get(repo string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[repo]
	return r, ok
}

// Records are every mirrored repository, by name.
func (m *Mirror) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

// Count is how many repositories are mirrored, for capacity diagnostics.
func (m *Mirror) Count() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.records))
}

// Lookup is the mirrored record whose checkout contains cwd, longest first,
// so a session in a subdirectory draws its project's mirrored mark.
func (m *Mirror) Lookup(cwd string) (Record, bool) {
	if cwd == "" {
		return Record{}, false
	}
	cwd = filepath.ToSlash(filepath.Clean(cwd))
	m.mu.Lock()
	defer m.mu.Unlock()
	var best Record
	found := false
	for _, r := range m.records {
		p := filepath.ToSlash(filepath.Clean(r.Path))
		if cwd == p || strings.HasPrefix(cwd, strings.TrimSuffix(p, "/")+"/") {
			if !found || len(p) > len(best.Path) {
				best, found = r, true
			}
		}
	}
	return best, found
}

// Mirrored is whether a checkout root is bound to a mirror record, which is
// what keeps a mirror from republishing what it was given.
func (m *Mirror) Mirrored(root string) bool {
	root = filepath.Clean(root)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if filepath.Clean(r.Path) == root {
			return true
		}
	}
	return false
}

// Put saves one record, replacing any for the same repository.
func (m *Mirror) Put(r Record) error {
	if !ValidRepo(r.Repo) || !filepath.IsAbs(r.Path) {
		return errors.New("a mirror record needs a repository and an absolute checkout path")
	}
	if err := icon.Validate(r.Icon); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.records[r.Repo]; !exists && len(m.records) >= MaxMirrorRecords {
		return ErrMirrorCapacity
	}
	next := make(map[string]Record, len(m.records)+1)
	for k, v := range m.records {
		next[k] = v
	}
	next[r.Repo] = r
	if err := m.save(next); err != nil {
		return err
	}
	m.records = next
	return nil
}

// Remove forgets one repository. The files it wrote stay where they are; the
// project becomes this machine's own again.
func (m *Mirror) Remove(repo string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[repo]; !ok {
		return false, nil
	}
	next := make(map[string]Record, len(m.records))
	for k, v := range m.records {
		if k != repo {
			next[k] = v
		}
	}
	if err := m.save(next); err != nil {
		return false, err
	}
	m.records = next
	return true, nil
}

func (m *Mirror) save(records map[string]Record) error {
	rows := make([]Record, 0, len(records))
	for _, r := range records {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Repo < rows[j].Repo })
	data, err := json.MarshalIndent(struct {
		Version  int      `json:"version"`
		Projects []Record `json:"projects"`
	}{Version, rows}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".project-mirror-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), m.path)
}
