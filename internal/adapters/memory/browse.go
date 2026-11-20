package memory

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// BrowsePage counts and orders live entries by frontmatter. It reads only
// the selected page's bodies, including when validating a detail revision.
func (s *Store) BrowsePage(key, name string, page, size int) (BrowseView, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	view := BrowseView{Entries: []BrowseEntry{}}
	dir, err := s.Dir(key)
	if err != nil {
		return view, 0, err
	}
	if page < 1 || size < 1 || page > LiveEntryLimit || size > LiveEntryLimit {
		return view, 0, ErrInvalid
	}
	files, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return view, 0, err
	}
	entries := make([]Entry, 0, len(files))
	h := sha256.New()
	for _, file := range files {
		entry, ok := strings.CutSuffix(file.Name(), ".md")
		if !ok || file.Name() == IndexFile || !file.Type().IsRegular() || !ValidName(entry) {
			continue
		}
		header, err := browseHeader(filepath.Join(dir, file.Name()))
		if err != nil {
			return view, 0, fmt.Errorf("%s: %w", file.Name(), err)
		}
		if header.Name != entry {
			return view, 0, ErrInvalid
		}
		if !header.Storage.archived() {
			entries = append(entries, header)
		}
	}
	sortEntries(entries)
	for _, header := range entries {
		fmt.Fprintln(h, header.Name)
	}
	start, total := (page-1)*size, len(entries)
	if page > 1 && start >= total {
		return view, total, ErrNotFound
	}
	found := name == ""
	for _, header := range entries[start:min(start+size, total)] {
		path := filepath.Join(dir, header.Name+".md")
		entry, err := readEntry(path)
		if err != nil {
			return view, total, err
		}
		if entry.Name != header.Name || entry.Storage.archived() {
			return view, total, ErrInvalid
		}
		info, err := os.Stat(path)
		if err != nil {
			return view, total, err
		}
		if err = json.NewEncoder(h).Encode(entry); err != nil {
			return view, total, err
		}
		if entry.Name == name {
			view.Text, found = entry.Body, true
		}
		entry.Body = ""
		view.Entries = append(view.Entries, BrowseEntry{Entry: entry, UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)})
	}
	if !found {
		return view, total, ErrNotFound
	}
	view.Revision = hex.EncodeToString(h.Sum(nil))
	return view, total, nil
}

// Use the existing parser and file limit for metadata. Stop at the closing
// delimiter so an unselected entry's body is never loaded or hashed.
func browseHeader(path string) (Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, entryFileReadLimit+1))
	first, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = ErrInvalid
		}
		return Entry{}, err
	}
	if strings.TrimRight(first, "\r\n") != "---" {
		return Entry{}, ErrInvalid
	}
	var front strings.Builder
	front.WriteString(first)
	for {
		line, err := reader.ReadString('\n')
		front.WriteString(line)
		if strings.TrimRight(line, "\r\n") == "---" {
			return Parse(front.String())
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = ErrInvalid
			}
			return Entry{}, err
		}
	}
}
