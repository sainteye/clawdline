package http

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
)

// focusPath is POST /v1/sessions/{id}/focus.
//
// A POST, and behind the send gate with it. Bringing a terminal forward on
// somebody's machine moves their keyboard: it is not a read, and a device that
// may only read may not do it.
func focusPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "focus", http.MethodPost)
}

// sessionFocusRoute brings one session's terminal to the front.
//
// **`ok` is the selection, not the window.** On tmux this daemon selects the
// pane and the window, and whether an application comes forward is up to
// whichever emulator is drawing that tmux — under `tmux -CC` iTerm2 is asked,
// on Ghostty, Terminal.app and the rest nothing is raised because there is
// nothing this daemon can ask. That question is settled after this answer has
// gone out, deliberately: the tail is up to four round trips, two of them Apple
// Events, and somebody is waiting on this request.
func (s *Server) sessionFocusRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	// The same sentence and envelope every other session action uses, since the
	// page reads both.
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, err := s.actions().Focus(ctx, id); err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, contract.FocusResult{OK: true, ID: id})
}
