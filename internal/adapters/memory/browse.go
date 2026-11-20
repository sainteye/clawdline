package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// BrowseEntry records only facts the existing store actually owns. Author is
// unknown: its Markdown format has never recorded the author of a write.
type BrowseEntry struct {
	Entry     Entry
	UpdatedAt string
}
type BrowseView struct {
	Revision string
	Entries  []BrowseEntry
	Text     string
}

// Browse uses the same authority and lock as Project memory writes. Names are
// validated by Get, never interpreted as raw paths. Content participates in
// the revision, so a same-size edit cannot leave a stale detail valid.
func (s *Store) Browse(key, name string) (BrowseView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.Dir(key)
	if err != nil {
		return BrowseView{}, err
	}
	entries, err := readEntries(dir, true)
	if err != nil {
		return BrowseView{}, err
	}
	view := BrowseView{Entries: []BrowseEntry{}}
	h := sha256.New()
	for _, entry := range entries {
		if !entry.Storage.archived() || entry.Name == name {
			bytes, _ := json.Marshal(entry)
			h.Write(bytes)
		}
		if entry.Name == name {
			view.Text = entry.Body
		}
		if entry.Storage.archived() {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, entry.Name+".md"))
		if err != nil {
			return BrowseView{}, err
		}
		entry.Body = ""
		view.Entries = append(view.Entries, BrowseEntry{Entry: entry, UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)})
	}
	if name != "" && view.Text == "" {
		return BrowseView{}, ErrNotFound
	}
	view.Revision = hex.EncodeToString(h.Sum(nil))
	return view, nil
}
