package memory

import (
	"context"
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

const (
	DreamInterval  = 7 * 24 * time.Hour
	DreamQuietTime = 5 * time.Minute
	dreamPressure  = (EntryCountLimit*4 + 4) / 5
	dreamFile      = "DREAM.json"
	storageKey     = "clawdline-storage"
	replacementKey = "clawdline-replacement"
)

type Storage string

const (
	Pending            Storage = "pending"
	pendingRetained    Storage = "pending_retained"
	retained           Storage = "retained"
	ArchivedDuplicate  Storage = "archived_duplicate"
	ArchivedSuperseded Storage = "archived_superseded"
)

func (s Storage) active() bool   { return s == "" || s == retained }
func (s Storage) archived() bool { return s == ArchivedDuplicate || s == ArchivedSuperseded }

func (e Entry) IsPending() bool { return e.Storage == Pending || e.Storage == pendingRetained }

func (e Entry) validStorage() bool {
	switch e.Storage {
	case "", retained, Pending, pendingRetained:
		return e.Replacement == ""
	case ArchivedDuplicate, ArchivedSuperseded:
		return e.Name != e.Replacement && ValidName(e.Replacement)
	}
	return false
}

func (e Entry) storageFields() string {
	if e.Storage == "" {
		return ""
	}
	fields := "\n  " + storageKey + ": " + string(e.Storage)
	if e.Replacement != "" {
		fields += "\n  " + replacementKey + ": " + e.Replacement
	}
	return fields
}

type StorageCounts struct {
	Active   int `json:"active"`
	Pending  int `json:"pending"`
	Archived int `json:"archived"`
}

func countStorage(entries []Entry) (counts StorageCounts) {
	for _, e := range entries {
		switch {
		case e.Storage.active():
			counts.Active++
		case e.Storage.archived():
			counts.Archived++
		default:
			counts.Pending++
		}
	}
	return counts
}

type DreamChange struct {
	Name        string `json:"name"`
	Replacement string `json:"replacement"`
	Reason      string `json:"reason"`
}

type DreamPlan struct {
	Counts       StorageCounts `json:"counts"`
	ActiveLimit  int           `json:"active_limit"`
	PendingLimit int           `json:"pending_limit"`
	ArchiveLimit int           `json:"archive_limit"`
	LastRun      *time.Time    `json:"last_run,omitempty"`
	Archive      []DreamChange `json:"archive"`
	Review       []DreamChange `json:"review"`
	Archived     []DreamChange `json:"archived"`
}

type dreamStamp struct {
	At          time.Time `json:"at"`
	Fingerprint string    `json:"fingerprint"`
}

func (s *Store) projectKeys() ([]string, error) {
	names, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, name := range names {
		if name.IsDir() && keyShape.MatchString(name.Name()) {
			keys = append(keys, name.Name())
		}
	}
	return keys, nil
}

func readDreamStamp(dir string) (dreamStamp, error) {
	var stamp dreamStamp
	f, err := os.Open(filepath.Join(dir, dreamFile))
	if errors.Is(err, os.ErrNotExist) {
		return stamp, nil
	}
	if err != nil {
		return stamp, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, DescriptionByteLimit+1))
	if err != nil {
		return stamp, err
	}
	if len(raw) > DescriptionByteLimit {
		return stamp, errors.New("memory maintenance stamp is too large")
	}
	return stamp, json.Unmarshal(raw, &stamp)
}

// PlanDream reads the proposed changes and the reversible archives. A preview
// is informational; the background pass computes its plan again under the
// same lock as ordinary edits.
func (s *Store) PlanDream(key string) (DreamPlan, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return DreamPlan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := readEntries(dir, true)
	if err != nil {
		return DreamPlan{}, err
	}
	stamp, err := readDreamStamp(dir)
	if err != nil {
		return DreamPlan{}, err
	}
	return planDream(entries, stamp), nil
}

func planDream(entries []Entry, stamp dreamStamp) DreamPlan {
	p := DreamPlan{Counts: countStorage(entries), ActiveLimit: EntryCountLimit,
		PendingLimit: PendingEntryLimit, ArchiveLimit: ArchiveEntryLimit,
		Archive: []DreamChange{}, Review: []DreamChange{}, Archived: []DreamChange{}}
	if !stamp.At.IsZero() {
		p.LastRun = &stamp.At
	}
	type subject struct {
		kind               Type
		group, description string
	}
	type exact struct {
		subject
		body [sha256.Size]byte
	}
	byName := make(map[string]Entry)
	byContent := make(map[exact]string)
	bySubject := make(map[subject]string)
	replacements := make(map[string]string)
	targets := make(map[string]bool)
	for _, e := range entries {
		if e.Storage.archived() {
			p.Archived = append(p.Archived, DreamChange{e.Name, e.Replacement, string(e.Storage)})
			continue
		}
		byName[e.Name] = e
		if e.Type == TypeProject && e.Storage != retained && e.Storage != pendingRetained {
			body := strings.TrimSpace(e.Body)
			if line := strings.LastIndexByte(body, '\n'); line >= 0 {
				if next, found := strings.CutPrefix(body[line+1:], "Superseded-by: "); found && next != e.Name && ValidName(next) {
					replacements[e.Name] = next
				}
			}
		}
	}
	for name, next := range replacements {
		target, ok := byName[next]
		if !ok || target.Type != TypeProject || target.Group != byName[name].Group {
			delete(replacements, name)
			continue
		}
		targets[next] = true
	}
	// Keep a successor in the live set. Chains and cycles are reviewed
	// conservatively, and restoring an entry takes it out of automatic moves.
	for _, e := range entries {
		if e.Storage.archived() || e.Type == TypeUser || e.Type == TypeFeedback {
			continue
		}
		topic := subject{e.Type, e.Group, e.Description}
		key := exact{topic, sha256.Sum256([]byte(e.Body))}
		safeToMove := e.Storage != retained && e.Storage != pendingRetained && !targets[e.Name]
		reason, next := "", replacements[e.Name]
		_, successorHasSuccessor := replacements[next]
		switch {
		case safeToMove && next != "" && !successorHasSuccessor:
			reason = string(ArchivedSuperseded)
		case safeToMove && next == "" && byContent[key] != "":
			reason, next = string(ArchivedDuplicate), byContent[key]
		}
		if reason != "" {
			if p.Counts.Archived+len(p.Archive) < ArchiveEntryLimit {
				p.Archive = append(p.Archive, DreamChange{e.Name, next, reason})
			} else {
				p.Review = append(p.Review, DreamChange{e.Name, next, "archive_full"})
			}
			continue
		}
		if next != "" {
			p.Review = append(p.Review, DreamChange{e.Name, next, "successor_chain"})
		} else if other := bySubject[topic]; other != "" && byContent[key] == "" {
			p.Review = append(p.Review, DreamChange{e.Name, other, "same_description"})
		}
		if byContent[key] == "" {
			byContent[key] = e.Name
		}
		if bySubject[topic] == "" {
			bySubject[topic] = e.Name
		}
	}
	return p
}

// Restore puts an archived original back into the live set. It keeps the
// original name and text, and prevents automatic archival of that entry again.
func (s *Store) Restore(key, name string) (Outcome, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.Get(key, name)
	if err != nil {
		return "", err
	}
	if !entry.Storage.archived() {
		return Unchanged, nil
	}
	entries, err := list(dir)
	if err != nil {
		return "", err
	}
	counts := countStorage(entries)
	storage := retained
	if counts.Active >= EntryCountLimit {
		if counts.Pending >= PendingEntryLimit {
			return "", fmt.Errorf("%w: the Project's active and pending memory are full", ErrFull)
		}
		storage = pendingRetained
	}
	if err := writeStorage(dir, entry, storage, ""); err != nil {
		return "", err
	}
	if storage.active() {
		return Updated, writeIndex(dir)
	}
	return Updated, nil
}

// MaintainDreams checks each Project independently. Busy is based on the
// daemon's observed Project sessions. Recent memory activity also postpones
// a pass, and an unchanged near-full store waits for its next weekly review.
func (s *Store) MaintainDreams(ctx context.Context, now time.Time, busy func(string) bool) error {
	keys, err := s.projectKeys()
	if err != nil {
		return err
	}
	var failures error
	for _, key := range keys {
		if ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
		if busy(key) || !s.mu.TryLock() {
			continue
		}
		err := maintainDream(filepath.Join(s.root, key), now)
		s.mu.Unlock()
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("%s: %w", key, err))
		}
	}
	return failures
}

func maintainDream(dir string, now time.Time) error {
	stamp, err := readDreamStamp(dir)
	if err != nil {
		return err
	}
	signature, newest, count, err := dreamSignature(dir)
	if err != nil || count == 0 || now.Sub(newest) < DreamQuietTime {
		return err
	}
	weekly := now.Sub(stamp.At) >= DreamInterval
	if !weekly && (stamp.Fingerprint == signature || count < dreamPressure) {
		return nil
	}
	entries, err := readEntries(dir, true)
	if err != nil {
		return err
	}
	plan := planDream(entries, stamp)
	if !weekly && plan.Counts.Active+plan.Counts.Pending < dreamPressure {
		return nil
	}
	changes := make(map[string]DreamChange, len(plan.Archive))
	for _, change := range plan.Archive {
		changes[change.Name] = change
	}
	var writeErr error
	active := plan.Counts.Active
	for _, entry := range entries {
		if change, ok := changes[entry.Name]; ok {
			if writeErr = writeStorage(dir, entry, Storage(change.Reason), change.Replacement); writeErr != nil {
				break
			}
			if entry.Storage.active() {
				active--
			}
		}
	}
	if writeErr == nil {
		for _, entry := range entries {
			if active >= EntryCountLimit {
				break
			}
			if _, moved := changes[entry.Name]; moved || entry.Storage.active() || entry.Storage.archived() {
				continue
			}
			storage := Storage("")
			if entry.Storage == pendingRetained {
				storage = retained
			}
			if writeErr = writeStorage(dir, entry, storage, ""); writeErr != nil {
				break
			}
			active++
		}
	}
	if err := errors.Join(writeErr, writeIndex(dir)); err != nil {
		return err
	}
	signature, _, _, err = dreamSignature(dir)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(dreamStamp{At: now, Fingerprint: signature})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, dreamFile), raw)
}

func dreamSignature(dir string) (string, time.Time, int, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	hash := sha256.New()
	var latest time.Time
	count := 0
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), ".md")
		if !ok || !ValidName(name) || !f.Type().IsRegular() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			return "", time.Time{}, 0, err
		}
		at := info.ModTime()
		if at.After(latest) {
			latest = at
		}
		fmt.Fprintf(hash, "%s %d %d\n", name, at.UnixNano(), info.Size())
		count++
	}
	return hex.EncodeToString(hash.Sum(nil)), latest, count, nil
}

func writeStorage(dir string, entry Entry, storage Storage, replacement string) error {
	path := filepath.Join(dir, entry.Name+".md")
	raw, err := readEntryData(path)
	if err != nil {
		return err
	}
	current, err := Parse(raw)
	if err != nil {
		return err
	}
	if current != entry {
		return fmt.Errorf("%s changed during maintenance", entry.Name)
	}
	entry.Storage, entry.Replacement = storage, replacement
	// Rewrite only the maintenance metadata in the header. Imported or
	// hand-restored Markdown may contain whitespace or other metadata.
	lines := strings.SplitAfter(raw, "\n")
	var output strings.Builder
	output.WriteString(lines[0])
	for i, line := range lines[1:] {
		if strings.TrimRight(line, "\r\n") == "---" {
			fields := entry.storageFields()
			if strings.HasSuffix(lines[0], "\r\n") {
				fields = strings.ReplaceAll(fields, "\n", "\r\n")
			}
			if len(fields) != 0 {
				output.WriteString(fields[1:])
				if strings.HasSuffix(lines[0], "\r\n") {
					output.WriteString("\r\n")
				} else {
					output.WriteByte('\n')
				}
			}
			for _, rest := range lines[i+1:] {
				output.WriteString(rest)
			}
			return writeAtomic(path, []byte(output.String()))
		}
		key, _, _ := strings.Cut(strings.TrimSpace(line), ":")
		if strings.TrimSpace(key) != storageKey && strings.TrimSpace(key) != replacementKey {
			output.WriteString(line)
		}
	}
	return fmt.Errorf("%w: missing memory header terminator", ErrInvalid)
}
