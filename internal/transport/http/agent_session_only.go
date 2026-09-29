package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app"
)

// refuseOrdinaryShell answers 409 not_an_agent_session, and true, when the
// session named by `/v1/sessions/{id}/…` is a terminal running an ordinary
// shell rather than Claude or Codex.
//
// The list has only ever shown assistants (sessions.go), but every route below
// an id resolved it against the whole inventory, which holds every tmux pane
// and iTerm2 session: a device that may send could type a command into a
// person's bash, and a read-only one could read the Git diff of the directory
// that bash sat in. So the rule is asked where the routes are dispatched —
// the `/v1/sessions/` handler, and withDocuments, which answers in front of
// the mux — rather than route by route, where the next route would be the one
// that forgot.
//
// Only a row that is there and is not an assistant is refused. A reading that
// cannot say (`session_unknown`), or that proves the id absent, is left to the
// route, which answers it as it did before this existed: unseen is never
// called a shell.
//
// The broker's own typing (orchestrator_wiring.go) goes through app.Actions and
// never through here: a child's pane runs its shell for the first seconds
// before claude replaces it, and the briefing has to reach it.
func (s *Server) refuseOrdinaryShell(w http.ResponseWriter, r *http.Request) bool {
	rest, _ := strings.CutPrefix(routePath(r), "/v1/sessions/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" {
		return false
	}
	id := decodeSegment(parts[0])
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// A read may use the row the list already showed, as the reads themselves
	// do (Actions.FindForRead); anything else is about to act on the terminal
	// and asks a reading taken now, as the action will.
	actions := s.actions()
	var err error
	var assistant bool
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		item, readErr := actions.FindForRead(ctx, id)
		if ref, ok := readErr.(app.Refusal); ok && ref.Code == "session_unknown" {
			item, readErr = actions.Find(ctx, id)
		}
		assistant, err = item.IsAssistant(), readErr
	} else {
		item, findErr := actions.Find(ctx, id)
		assistant, err = item.IsAssistant(), findErr
	}
	if err != nil || assistant {
		return false
	}
	writeRefusal(w, http.StatusConflict, "not_an_agent_session",
		"That terminal is running an ordinary shell, not Claude or Codex, so it is not a Session here.")
	return true
}
