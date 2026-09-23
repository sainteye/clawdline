package http

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// smartSessionTitle spends one explicitly confirmed, receipted model turn and
// makes its answer durable through the same local title store as manual edits.
func (s *Server) smartSessionTitle(w http.ResponseWriter, r *http.Request, id string, findContext context.Context) {
	item, err := s.actions().Find(findContext, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	first, err := s.firstRequest(item)
	if errors.Is(err, transcript.ErrNoRecord) || errors.Is(err, transcript.ErrNotFound) || strings.TrimSpace(first) == "" {
		writeRefusal(w, http.StatusConflict, "conversation_empty",
			"This session has no first request to name yet. Send its first message, then try again.")
		return
	}
	if err != nil {
		writeRefusal(w, http.StatusConflict, "conversation_unreadable",
			"This session's first request could not be read, so no model turn was started.")
		return
	}
	first = namingInput(first)
	if first == "" {
		writeRefusal(w, http.StatusConflict, "conversation_empty",
			"This session has no first request to name yet. Send its first message, then try again.")
		return
	}

	assistant, err := s.namingAssistant()
	if err != nil {
		writeSettingsFailure(w, s.settingsFile(), err)
		return
	}
	if !s.enterIntent() {
		writeRefusal(w, http.StatusTooManyRequests, "busy",
			"This machine is already using both small-turn slots. Try again in a moment.")
		return
	}
	defer s.leaveIntent()
	s.intentRun.Lock()
	defer s.intentRun.Unlock()

	turn, cancel := context.WithTimeout(r.Context(), time.Duration(CapacityLimit(capacity.IntentPlannerSeconds))*time.Second)
	defer cancel()
	started := time.Now()
	title, err := s.namer()(turn, first, assistant)
	ms := time.Since(started).Milliseconds()
	if errors.Is(err, planner.ErrNoPlanner) {
		log.Printf("audit session.smart_title assistant=%s ms=%d ok=0 why=no_planner", assistant, ms)
		writeRefusal(w, http.StatusServiceUnavailable, "no_namer",
			"The naming assistant selected in Settings is not installed on this machine.")
		return
	}
	if err != nil {
		log.Printf("audit session.smart_title assistant=%s ms=%d ok=0 why=failed", assistant, ms)
		writeRefusal(w, http.StatusBadGateway, "naming_failed",
			"The naming assistant did not return a usable title. No title was changed.")
		return
	}
	title = normalizedSessionTitle(title)
	if title == "" || !titleWithinLimit(title) {
		log.Printf("audit session.smart_title assistant=%s ms=%d ok=0 why=invalid_title", assistant, ms)
		writeRefusal(w, http.StatusBadGateway, "naming_failed",
			"The naming assistant did not return a usable title. No title was changed.")
		return
	}
	title, err = s.saveSessionTitle(item, title, time.Now())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "title_not_saved", "the session title could not be saved")
		return
	}
	log.Printf("audit session.smart_title assistant=%s ms=%d ok=1", assistant, ms)
	writeJSON(w, contract.SessionTitleReply{
		OK: true, Title: title, DisplayTitle: s.sessionDisplayLabel(findContext, item),
		LocalApplied: true, Downstream: "local_only", DownstreamSynced: false,
	})
}

func (s *Server) firstRequest(item session.Session) (string, error) {
	if s.firstSessionRequest != nil {
		return s.firstSessionRequest(item)
	}
	path := recordPath(item)
	if path == "" {
		return "", transcript.ErrNoRecord
	}
	return transcript.FirstUser(path, string(item.Assistant))
}

func (s *Server) namer() func(context.Context, string, string) (string, error) {
	if s.nameSession != nil {
		return s.nameSession
	}
	p := planner.New()
	p.Timeout = time.Duration(CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second
	return p.Name
}

func (s *Server) namingAssistant() (string, error) {
	values, err := s.settingsFile().Read()
	if err != nil {
		return "", err
	}
	if named, ok := values.String("auto_name_assistant"); ok && (named == "claude" || named == "codex") {
		return named, nil
	}
	return "codex", nil
}

// namingInput uses the existing spoken-intent request bound. The first request
// can be a pasted document, but naming it must remain one small, capped turn.
func namingInput(text string) string {
	text = strings.TrimSpace(text)
	limit := int(CapacityLimit(capacity.IntentRequestBytes))
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return strings.TrimSpace(text)
}
