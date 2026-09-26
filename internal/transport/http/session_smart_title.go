package http

import (
	"context"
	"errors"
	"fmt"
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

	started := time.Now()
	title, namedBy, tried, err := s.nameWith(r.Context(), first, assistant)
	ms := time.Since(started).Milliseconds()
	if errors.Is(err, planner.ErrNoPlanner) {
		log.Printf("audit session.smart_title assistant=%s tried=%s ms=%d ok=0 why=no_planner", assistant, tried, ms)
		writeRefusal(w, http.StatusServiceUnavailable, "no_namer",
			"The naming assistant selected in Settings is not installed on this machine.")
		return
	}
	if errors.Is(err, planner.ErrOutOfQuota) {
		log.Printf("audit session.smart_title assistant=%s tried=%s ms=%d ok=0 why=out_of_quota", assistant, tried, ms)
		writeRefusal(w, http.StatusServiceUnavailable, "namer_out_of_quota",
			"The naming assistant selected in Settings has no usage left on its account. No title was changed.")
		return
	}
	if err != nil {
		log.Printf("audit session.smart_title assistant=%s tried=%s ms=%d ok=0 why=failed", assistant, tried, ms)
		writeRefusal(w, http.StatusBadGateway, "naming_failed",
			"The naming assistant did not return a usable title. No title was changed.")
		return
	}
	title = normalizedSessionTitle(title)
	if title == "" || !titleWithinLimit(title) {
		log.Printf("audit session.smart_title assistant=%s tried=%s ms=%d ok=0 why=invalid_title", assistant, tried, ms)
		writeRefusal(w, http.StatusBadGateway, "naming_failed",
			"The naming assistant did not return a usable title. No title was changed.")
		return
	}
	title, err = s.saveSessionTitle(item, title, time.Now())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "title_not_saved", "the session title could not be saved")
		return
	}
	log.Printf("audit session.smart_title assistant=%s tried=%s ms=%d ok=1", assistant, tried, ms)
	writeJSON(w, contract.SessionTitleReply{
		OK: true, Title: title, DisplayTitle: s.sessionDisplayLabel(findContext, item), NamedBy: contract.Assistant(namedBy),
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

// autoNamingOrder is who `auto` asks, in order. Claude Code is first; Codex
// is asked only when Claude Code cannot answer at all — no usage left, or not
// installed — never after a turn that ran and produced nothing usable, which
// would spend twice for one press.
var autoNamingOrder = []string{"claude", "codex"}

// nameWith runs the naming turn for the assistant chosen in Settings, each
// attempt under its own deadline. tried is every assistant asked and how it
// ended, for the audit line. When every `auto` candidate is out of usage the
// error is ErrOutOfQuota; when none is installed, ErrNoPlanner.
func (s *Server) nameWith(ctx context.Context, first, assistant string) (title, namedBy, tried string, err error) {
	order := []string{assistant}
	if assistant == "auto" {
		order = autoNamingOrder
	}
	var said []string
	quota := false
	for _, candidate := range order {
		turn, cancel := context.WithTimeout(ctx, time.Duration(CapacityLimit(capacity.IntentPlannerSeconds))*time.Second)
		title, err = s.namer()(turn, first, candidate)
		cancel()
		switch {
		case err == nil:
			said = append(said, candidate+":ok")
			return title, candidate, strings.Join(said, ","), nil
		case errors.Is(err, planner.ErrOutOfQuota):
			quota = true
			said = append(said, candidate+":out_of_quota")
		case errors.Is(err, planner.ErrNoPlanner):
			said = append(said, candidate+":not_installed")
		default:
			said = append(said, candidate+":failed")
			return "", "", strings.Join(said, ","), err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if quota {
		err = fmt.Errorf("%w: %v", planner.ErrOutOfQuota, err)
	}
	return "", "", strings.Join(said, ","), err
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
	if named, ok := values.String("auto_name_assistant"); ok && (named == "claude" || named == "codex" || named == "auto") {
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
