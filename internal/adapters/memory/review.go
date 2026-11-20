package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const RequestBodyLimit = 2*EntryBodyLimit + 4<<10

var ErrReviewStale = errors.New("memory_review_stale")

func EntryRevision(entry Entry) string {
	sum := sha256.Sum256([]byte(Format(entry)))
	return hex.EncodeToString(sum[:])
}

// Search matches stored text, including archived originals. Only the stable
// names are returned so a full result never carries the corpus over the wire.
func (s *Store) Search(key, query string) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || len(query) > DescriptionByteLimit {
		return nil, ErrInvalid
	}
	dir, err := s.Dir(key)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := readEntries(dir, true)
	if err != nil {
		return nil, err
	}
	matches := []string{}
	for _, entry := range entries {
		for _, value := range []string{entry.Name, entry.Description, entry.Group, entry.Body} {
			if strings.Contains(strings.ToLower(value), query) {
				matches = append(matches, entry.Name)
				break
			}
		}
	}
	return matches, nil
}

type ReviewRequest struct {
	Action              string `json:"action"`
	Expected            string `json:"expected"`
	Replacement         string `json:"replacement"`
	ExpectedReplacement string `json:"expected_replacement"`
	Body                string `json:"body"`
}

// Review fences both originals the person compared. It preserves the source
// in the archive and keeps the original name readable under memory show.
func (s *Store) Review(key, name string, request ReviewRequest) (Outcome, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return "", err
	}
	if request.Action != "keep" && request.Action != "archive" && request.Action != "merge" {
		return "", ErrInvalid
	}
	if request.Action != "merge" && request.Body != "" ||
		request.Action == "keep" && (request.Replacement != "" || request.ExpectedReplacement != "") ||
		request.Action != "keep" && (name == request.Replacement || !ValidName(request.Replacement) || request.ExpectedReplacement == "") || request.Expected == "" {
		return "", ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.Get(key, name)
	if err != nil {
		return "", err
	}
	if entry.Type != TypeProject && entry.Type != TypeReference {
		return "", ErrInvalid
	}
	if request.Action == "keep" {
		if entry.Storage == retained || entry.Storage == pendingRetained {
			return Unchanged, nil
		}
		if entry.Storage.archived() || EntryRevision(entry) != request.Expected {
			return "", ErrReviewStale
		}
		storage := retained
		if entry.IsPending() {
			storage = pendingRetained
		}
		if err := writeStorage(dir, entry, storage, ""); err != nil {
			return "", err
		}
		return Updated, nil
	}

	target, err := s.Get(key, request.Replacement)
	if err != nil {
		return "", err
	}
	if target.Storage.archived() || entry.Type != target.Type {
		return "", ErrReviewStale
	}
	body := strings.Trim(strings.ReplaceAll(request.Body, "\r\n", "\n"), "\n") + "\n"
	targetMatches := request.Action == "merge" && target.Body == body
	if entry.Storage == archivedReviewed && entry.Replacement == request.Replacement &&
		(request.Action == "archive" || targetMatches) {
		return Unchanged, nil
	}
	if entry.Storage.archived() || EntryRevision(entry) != request.Expected ||
		EntryRevision(target) != request.ExpectedReplacement {
		return "", ErrReviewStale
	}
	entries, err := readEntries(dir, false)
	if err != nil {
		return "", err
	}
	if countStorage(entries).Archived >= ArchiveEntryLimit {
		return "", fmt.Errorf("%w: the Project archive is full", ErrFull)
	}
	if request.Action == "merge" && !targetMatches {
		target.Body = body
		if err := Validate(target); err != nil {
			return "", err
		}
		if err := writeAtomic(filepath.Join(dir, target.Name+".md"), []byte(Format(target))); err != nil {
			return "", err
		}
	}
	// Save the replacement first. If archiving fails, both records remain
	// readable and the next review must compare the new replacement revision.
	if err := writeStorage(dir, entry, archivedReviewed, target.Name); err != nil {
		return "", err
	}
	return Updated, writeIndex(dir)
}

var deliverySuffix = regexp.MustCompile(`-(?:20[0-9]{6})$`)

func deliveryStage(name string) (string, int) {
	name = deliverySuffix.ReplaceAllString(name, "")
	for i, suffix := range []string{"-implemented-local", "-local", "-implementation", "-deployed", "-verified", "-shipped"} {
		if key, ok := strings.CutSuffix(name, suffix); ok {
			if i < 3 {
				return key, 1
			}
			return key, i - 1
		}
	}
	return "", 0
}

// Names are only evidence of a related delivery. They never authorize an
// automatic move. A person compares the complete originals before acting.
func relatedDeliveries(entries []Entry, plan DreamPlan) []DreamChange {
	type delivery struct {
		name  string
		stage int
	}
	latest := map[string]delivery{}
	covered := map[string]bool{}
	for _, row := range plan.Archive {
		covered[row.Name] = true
	}
	for _, row := range plan.Review {
		covered[row.Name] = true
	}
	for _, entry := range entries {
		if entry.Type != TypeProject || entry.Storage.archived() {
			continue
		}
		key, stage := deliveryStage(entry.Name)
		current := latest[key]
		if stage >= 2 && (stage > current.stage || stage == current.stage && entry.Name > current.name) {
			latest[key] = delivery{entry.Name, stage}
		}
	}
	var out []DreamChange
	for _, entry := range entries {
		if entry.Type != TypeProject || entry.Storage.archived() || entry.Storage == retained || entry.Storage == pendingRetained || covered[entry.Name] {
			continue
		}
		key, stage := deliveryStage(entry.Name)
		if next := latest[key]; stage != 0 && next.stage > stage {
			out = append(out, DreamChange{Name: entry.Name, Replacement: next.name, Reason: "related_delivery"})
		}
	}
	return out
}
