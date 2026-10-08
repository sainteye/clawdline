package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// agentPath reads /v1/sessions/{session}/agents/{agent}. Both names are split
// before they are decoded, so an encoded separator can never change the route.
func agentPath(r *http.Request) (sessionID, agentID string, ok bool) {
	rest, cut := strings.CutPrefix(routePath(r), "/v1/sessions/")
	if !cut {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "agents" || parts[2] == "" {
		return "", "", false
	}
	return decodeSegment(parts[0]), decodeSegment(parts[2]), true
}

func (s *Server) sessionAgentRoute(w http.ResponseWriter, r *http.Request, sessionID, agentID string) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "an agent transcript is a GET")
		return
	}
	limit := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		limit = min(max(n, 1), 1000)
	}
	before := int64(0)
	if r.URL.Query().Has("before") {
		raw := r.URL.Query().Get("before")
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			writeRefusal(w, http.StatusBadRequest, "invalid_cursor", "before must be a positive byte cursor")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, sessionID)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	path, ok := s.agents.AgentPath(item, agentID)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "not_found", "that background agent is not part of this session")
		return
	}
	writeJSON(w, s.agentTranscriptPageBefore(agentID, item, path, limit, before))
}

func (s *Server) agentTranscriptPage(id string, item session.Session, path string, limit int) contract.TranscriptPage {
	return s.agentTranscriptPageBefore(id, item, path, limit, 0)
}

func (s *Server) agentTranscriptPageBefore(id string, item session.Session, path string, limit int, before int64) contract.TranscriptPage {
	page := contract.TranscriptPage{ID: id, Entries: []contract.TranscriptEntry{}}
	var read transcript.Page
	var err error
	if item.Assistant == session.AssistantClaude {
		read, err = transcript.ReadClaudeAgentBefore(path, limit, before)
	} else {
		read, err = transcript.ReadCodexBefore(path, limit, before)
	}
	if errors.Is(err, transcript.ErrNoRecord) {
		page.Evidence = contract.EvidenceTranscript
		return page
	}
	if err != nil {
		page.Evidence = contract.EvidenceNone
		page.Note = recordNote(err)
		return page
	}
	page.Evidence = contract.EvidenceTranscript
	page.Signature = read.Signature
	entries := make([]contract.TranscriptEntry, 0, len(read.Entries))
	now := time.Now()
	for _, entry := range read.Entries {
		row := transcriptEntry(entry)
		row.Artifacts = s.pictures.wireArtifacts(entry, now)
		entries = append(entries, row)
	}
	page.Entries, _ = boundedTranscriptRows(entries, read.Entries)
	page.NextBefore = read.NextBefore
	if omitted := len(entries) - len(page.Entries); omitted > 0 {
		page.NextBefore = read.Entries[omitted].Before
		page.Truncation = &contract.TranscriptTruncation{Reason: "transcript_byte_budget", EntriesOmittedCount: int64(omitted), BudgetBytes: transcriptBudget}
	}
	if read.Unread > 0 {
		page.Unread = &contract.TranscriptUnread{Reason: "transcript_read_window", Bytes: read.Unread, WindowBytes: transcript.ReadBudget}
	}
	return page
}
