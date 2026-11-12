package http

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The voice copilot's call journal (docs/voice.md, "The call journal").
//
// What a call heard, said and did is kept only in the browser that made it,
// so "it did not understand me" on a phone could not be looked into on the
// machine (2026-11-12). The call now hands each turn's lines to the machine
// that gave it its pass, which appends them to one JSON-lines file a day in
// <state>/copilot-calls and keeps maxCopilotJournalAge of them. Nothing leaves
// this machine; Cloud only carries the sealed request.

const (
	// copilotJournalBodyLimit is one request: a turn or two of lines.
	copilotJournalBodyLimit = 64 << 10
	// maxCopilotJournalEntries is how many lines one request may append.
	maxCopilotJournalEntries = 200
	// maxCopilotJournalTextRunes is how much of one line is kept.
	maxCopilotJournalTextRunes = 4000
	// maxCopilotJournalAge is how long a day of calls is kept.
	maxCopilotJournalAge = 14 * 24 * time.Hour
)

var (
	copilotJournalMu   sync.Mutex
	copilotJournalKind = map[string]bool{"heard": true, "said": true, "tool": true, "error": true, "notice": true, "start": true, "end": true}
	copilotJournalFile = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)
	copilotJournalCall = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
)

type copilotJournalEntry struct {
	At   int64           `json:"at"`
	Kind string          `json:"kind"`
	Text string          `json:"text"`
	Tool json.RawMessage `json:"tool,omitempty"`
}

func (s *Server) copilotJournalDir() string { return filepath.Join(s.cfg.Dir, "copilot-calls") }

func (s *Server) copilotJournalRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, copilotJournalBodyLimit+1))
	if err != nil || len(raw) > copilotJournalBodyLimit {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "journal_too_large", "Send a call's lines a turn at a time.")
		return
	}
	var body struct {
		Call    string                `json:"call"`
		Entries []copilotJournalEntry `json:"entries"`
	}
	if json.Unmarshal(raw, &body) != nil || !copilotJournalCall.MatchString(body.Call) {
		writeRefusal(w, http.StatusBadRequest, "invalid_body", "Send {\"call\": \"<id>\", \"entries\": [...]}.")
		return
	}
	if len(body.Entries) > maxCopilotJournalEntries {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "journal_too_many", "Send a call's lines a turn at a time.")
		return
	}
	lines := make([]byte, 0, len(raw)+len(body.Entries)*64)
	for _, e := range body.Entries {
		if !copilotJournalKind[e.Kind] {
			writeRefusal(w, http.StatusBadRequest, "journal_bad_kind", "A line is heard, said, tool, error, notice, start or end.")
			return
		}
		if utf8.RuneCountInString(e.Text) > maxCopilotJournalTextRunes {
			e.Text = string([]rune(e.Text)[:maxCopilotJournalTextRunes])
		}
		if len(e.Tool) > 0 && !json.Valid(e.Tool) {
			e.Tool = nil
		}
		line, _ := json.Marshal(struct {
			Call string `json:"call"`
			copilotJournalEntry
		}{body.Call, e})
		lines = append(append(lines, line...), '\n')
	}
	if err := s.appendCopilotJournal(time.Now(), lines); err != nil {
		writeRefusal(w, http.StatusInternalServerError, "journal_unwritable", "This machine could not keep the call's lines.")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "kept": len(body.Entries)})
}

// appendCopilotJournal appends to today's file and removes files older than
// copilotJournalDays.
func (s *Server) appendCopilotJournal(now time.Time, lines []byte) error {
	copilotJournalMu.Lock()
	defer copilotJournalMu.Unlock()
	dir := s.copilotJournalDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if len(lines) > 0 {
		f, err := os.OpenFile(filepath.Join(dir, now.Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Write(lines); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	names, _ := os.ReadDir(dir)
	var days []string
	for _, n := range names {
		if copilotJournalFile.MatchString(n.Name()) {
			days = append(days, n.Name())
		}
	}
	sort.Strings(days)
	oldest := now.Add(-maxCopilotJournalAge).Add(24 * time.Hour).Format("2006-01-02")
	for _, d := range days {
		if strings.TrimSuffix(d, ".jsonl") < oldest {
			_ = os.Remove(filepath.Join(dir, d))
		}
	}
	return nil
}
