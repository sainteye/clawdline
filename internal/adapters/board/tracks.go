package board

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The old cards, as the three-track projection reads them
// (internal/domain/work; docs/board-redesign.md §9 step 1).
//
// Two things are read here and nothing is written: the board this package
// already reads (source.go), and the Swift app's per-card event logs beside
// it. The rules in docs/plan.md §4 apply unchanged — O_RDONLY, no rename, no
// lock file — and a log that cannot be read is said to be unreadable rather
// than read as a card that never moved.

// MaximumHistoryFileBytes is the largest card log this reader opens. The
// largest on this machine was 143,619 bytes on 2026-09-18; a bigger one is
// reported unreadable, not cut short, because the newest events are at the end.
const MaximumHistoryFileBytes = 8 * 1024 * 1024

// HistoryDir is where the Swift app keeps one append-only log per card:
// beside its board, whatever CLAWDLINE_BOARD_STORE moved that to.
func HistoryDir(boardPath string) string {
	if boardPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(boardPath), "project-board-history")
}

// History reads the card logs and remembers each by size and time, so a
// read of the tracks reopens only the logs that moved since the last one.
type History struct {
	dir string

	mu    sync.Mutex
	files map[string]historyFile
}

type historyFile struct {
	stamp   fileStamp
	history work.History
}

// OpenHistory prepares a reader. It opens nothing yet.
func OpenHistory(dir string) *History {
	return &History{dir: dir, files: map[string]historyFile{}}
}

// Read answers for the named cards. A card with no log is absent from the
// map; a log that could not be read is present and says so. With no log
// directory at all, every card is answered unreadable: the directory missing
// is not evidence that no card ever moved.
func (h *History) Read(ids []string) (map[string]*work.History, work.HistorySource) {
	out := map[string]*work.History{}
	source := work.HistorySource{Status: "ok"}
	if h == nil || h.dir == "" {
		source.Status = "absent"
		for _, id := range ids {
			out[id] = &work.History{}
		}
		return out, source
	}
	if info, err := os.Stat(h.dir); err != nil || !info.IsDir() {
		source.Status = "absent"
		for _, id := range ids {
			out[id] = &work.History{}
		}
		return out, source
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		if !plainName(id) {
			// A card id is a file name here, so one that could step out of the
			// directory is never opened.
			out[id] = &work.History{}
			source.Unreadable++
			continue
		}
		path := filepath.Join(h.dir, id+".jsonl")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			delete(h.files, id)
			continue
		}
		source.Files++
		if err != nil {
			out[id] = &work.History{}
			source.Unreadable++
			continue
		}
		stamp := fileStamp{size: info.Size(), mtime: info.ModTime()}
		cached, ok := h.files[id]
		if !ok || cached.stamp != stamp {
			cached = historyFile{stamp: stamp, history: readHistory(path, info.Size())}
			h.files[id] = cached
		}
		history := cached.history
		if !history.Readable {
			source.Unreadable++
		}
		out[id] = &history
	}
	// The cache holds what the last read asked for and nothing else, so it is
	// never larger than one board.
	for id := range h.files {
		if _, asked := out[id]; !asked {
			delete(h.files, id)
		}
	}
	return out, source
}

// plainName is a single path segment that names no directory.
func plainName(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`+"\x00")
}

// readHistory reads one log. The Swift app appends a line at a time, so a
// last line with no newline may be a write in progress and is left for the
// next read; any other line that does not parse makes the log unreadable.
func readHistory(path string, size int64) work.History {
	if size > MaximumHistoryFileBytes {
		return work.History{}
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return work.History{}
	}
	body, err := io.ReadAll(io.LimitReader(file, MaximumHistoryFileBytes+1))
	file.Close()
	if err != nil || len(body) > MaximumHistoryFileBytes {
		return work.History{}
	}
	history := work.History{Readable: true}
	for len(body) > 0 {
		line, rest, complete := bytes.Cut(body, []byte("\n"))
		body = rest
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e struct {
			Kind string   `json:"kind"`
			At   *float64 `json:"at"`
		}
		if err := json.Unmarshal(line, &e); err != nil || e.Kind == "" || e.At == nil {
			if !complete {
				break
			}
			return work.History{}
		}
		history.Events = append(history.Events, work.Event{Kind: e.Kind, At: *e.At})
	}
	return history
}

// TrackCards turns the Swift app's cards into what the projection reads. The
// progress and the audience are this package's own projections, the ones the
// old card view shows, so a card lands in a track for the reason a person
// looking at that card can see.
func TrackCards(state *StoredState, histories map[string]*work.History) []work.Card {
	if state == nil {
		return nil
	}
	cards := make([]work.Card, 0, len(state.Items))
	for _, item := range state.Items {
		progress := ProgressOf(item, state.Items)
		c := work.Card{
			ID: item.ID, Key: item.Key, ProjectID: item.ProjectID, Title: item.Title, Type: item.Type,
			Audience:   audienceView(item).Audience,
			Progress:   work.Progress{State: progress.State, Group: progress.Group, Active: progress.Active},
			Deliveries: len(item.SessionDeliveries), Evidence: len(item.Evidence),
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			History: histories[item.ID],
		}
		for _, o := range item.Obligations {
			kind := ""
			if o.ActorKind != nil {
				kind = *o.ActorKind
			}
			c.Obligations = append(c.Obligations, work.Obligation{Resolved: o.Resolved, ActorKind: kind})
		}
		for _, l := range item.Links {
			if l.Kind == "task" {
				c.Attempts = append(c.Attempts, work.Attempt{Source: l.source(), State: l.attemptState()})
			}
		}
		for _, s := range item.Spans {
			c.Spans = append(c.Spans, work.Span{SessionID: s.SessionID, Open: s.EndedAt == nil})
		}
		for _, row := range item.Checklist {
			c.Checklist = append(c.Checklist, row.Status)
		}
		cards = append(cards, c)
	}
	return cards
}
