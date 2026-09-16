package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// ownsSessions reports whether this daemon answers /v1/sessions itself.
//
// It is off by default. Taking a route over is the one change that can break
// the console for a person who is working, so it is a switch that can be turned
// back within a second rather than a property of the build.
func ownsSessions() bool { return os.Getenv("CLAWDLINE_NEXT_OWN_SESSIONS") == "1" }

// epoch identifies this process. A client that reconnects to a restarted daemon
// must be able to tell that the generation counter started over rather than
// went backwards.
var epoch = time.Now().Unix()

var generation atomic.Int64

// sessions answers the route the console actually reads.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	if !ownsSessions() {
		s.proxy.ServeHTTP(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if h, ok := s.inventory.Identity.(interface{ Refresh() }); ok {
		h.Refresh()
	}
	inv := s.inventory.Read(ctx)

	rows := make([]map[string]any, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		rows = append(rows, sessionRow(item))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessions": rows,
		"at":       time.Now().Unix(),
		"scan": map[string]any{
			"epoch":      epoch,
			"generation": generation.Add(1),
			"complete":   inv.Complete,
			"provenance": inv.Provenance,
			// An empty list is only authoritative when the reading was
			// complete. Saying so here is what stops a client from treating a
			// failed scan as "every session went away".
			"emptyAuthoritative": inv.Complete && len(rows) == 0,
			"completed": map[string]any{
				"sequence": generation.Load(),
				"complete": inv.Complete,
			},
		},
	})
}

// sessionRow renders one session in the shape the console reads.
//
// Fields this daemon cannot yet support are absent rather than invented. The
// contract already says several of them are absent in ordinary cases, so a
// reader that handles absence handles this too — and a guessed value would be
// worse than a missing one.
func sessionRow(item session.Session) map[string]any {
	row := map[string]any{
		"id":       item.ID,
		"backend":  string(item.Backend),
		"state":    string(item.State),
		"isClaude": item.Assistant == session.AssistantClaude,
		// Not in the Swift contract: how this row's state was learned. A
		// registry reading and a screen guess are different kinds of fact.
		"evidence":   string(item.Evidence),
		"work_state": string(workState(item)),
	}
	if item.TTY != "" {
		row["tty"] = item.TTY
	}
	if item.Assistant != "" {
		row["assistant"] = string(item.Assistant)
	}
	if item.Label != "" {
		row["label"] = item.Label
	}
	if item.CWD != "" {
		row["cwd"] = item.CWD
	}
	if item.ConversationID != "" {
		row["sessionId"] = item.ConversationID
	}
	return row
}

// workState projects the one closed user-facing value.
//
// `ready` is deliberately not produced here. The contract requires positive
// evidence for it — an assistant-free prompt or the session's own declaration —
// and an idle assistant with neither is `unknown`, which asks nothing of the
// reader. This daemon has no broker projection yet, so everything that would
// need one resolves to `unknown` rather than to a flattering guess.
func workState(item session.Session) session.State {
	switch item.State {
	case session.StateWorking:
		return "working"
	case session.StateWaiting:
		return "waiting_you"
	default:
		return "unknown"
	}
}
