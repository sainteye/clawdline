package http

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// recordPath is where an assistant keeps its own account of one session.
//
// Empty means there is nothing to read, which is different from a file that
// could not be read: a session whose conversation id was never recovered from
// its command line has no record to point at, and saying "unreadable" there
// would blame the disk for a missing identifier.
func recordPath(item session.Session) string {
	if item.ConversationID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch item.Assistant {
	case session.AssistantClaude:
		return transcript.ClaudePath(home, item.CWD, item.ConversationID)
	case session.AssistantCodex:
		return transcript.CodexPath(home, item.ConversationID)
	}
	return ""
}

// usageRoute reports what each live session has spent.
//
// It is about what is running now rather than about history, because that is
// the question a fleet screen is asked: a ledger of everything that ever ran is
// a different feature with a different shape, and pretending this is that one
// would be the worse mistake.
func (s *Server) usageRoute(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	inv := s.inventory.Read(ctx)
	rows := make([]contract.UsageRow, 0, len(inv.Sessions))
	var total int64
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		row := contract.UsageRow{
			ID:        item.ID,
			Label:     item.Label,
			Assistant: contract.Assistant(item.Assistant),
			Models:    []contract.ModelUsage{},
		}
		path := recordPath(item)
		if path == "" {
			row.Evidence = contract.EvidenceNone
			row.Note = "this session's own record could not be located"
			rows = append(rows, row)
			continue
		}
		u, err := s.readUsage(item, path)
		if err != nil {
			row.Evidence = contract.EvidenceNone
			row.Note = err.Error()
			rows = append(rows, row)
			continue
		}
		// The assistant's own file, so the reading is as good as the assistant's
		// own account and is labelled as exactly that.
		row.Evidence = contract.EvidenceTranscript
		for _, m := range u.Models {
			row.Models = append(row.Models, contract.ModelUsage{
				Model:            m.Model,
				InputTokens:      m.InputTokens,
				OutputTokens:     m.OutputTokens,
				ThinkingTokens:   m.ThinkingTok,
				CacheReadTokens:  m.CacheReadTok,
				CacheWriteTokens: m.CacheWriteTok,
			})
		}
		row.TotalTokens = u.Total()
		row.Messages = u.Messages
		total += row.TotalTokens
		rows = append(rows, row)
	}
	writeJSON(w, contract.UsageReport{
		Sessions:    rows,
		TotalTokens: total,
		At:          time.Now().Unix(),
	})
}

func (s *Server) readUsage(item session.Session, path string) (transcript.Usage, error) {
	if item.Assistant == session.AssistantCodex {
		return s.ledger.Codex(path)
	}
	return s.ledger.Claude(path)
}

// transcriptRoute returns the most recent turns of one session.
func (s *Server) transcriptRoute(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session")
	if id == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "name a session")
		return
	}
	limit := 40
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}

	page := contract.TranscriptPage{ID: id, Turns: []contract.Turn{}}
	path := recordPath(item)
	if path == "" {
		page.Evidence = contract.EvidenceNone
		page.Note = "this session's own record could not be located"
		writeJSON(w, page)
		return
	}
	page.Path = path

	var turns []transcript.Turn
	if item.Assistant == session.AssistantCodex {
		turns, err = transcript.ReadCodexTurns(path, limit)
	} else {
		turns, err = transcript.ReadClaudeTurns(path, limit)
	}
	if err != nil {
		page.Evidence = contract.EvidenceNone
		page.Note = err.Error()
		writeJSON(w, page)
		return
	}
	page.Evidence = contract.EvidenceTranscript
	for _, t := range turns {
		page.Turns = append(page.Turns, contract.Turn{
			Role: t.Role, At: t.At, Text: t.Text, Tool: t.Tool,
		})
	}
	writeJSON(w, page)
}
