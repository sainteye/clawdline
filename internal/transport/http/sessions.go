package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/task"
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.sessionsPayload(ctx))
}

// sessionsPayload builds the one snapshot both the route and the event stream
// publish. They are the same payload, so they are the same code: two builders
// would drift, and the client would have no way to tell which one it got.
func (s *Server) sessionsPayload(ctx context.Context) contract.SessionsSnapshot {
	if h, ok := s.inventory.Identity.(interface{ Refresh() }); ok {
		h.Refresh()
	}
	inv := s.inventory.Read(ctx)

	// One reading of what is owed, for the whole list. Asking per row would
	// ask the same question eight times and let two rows disagree about the
	// same moment.
	owed, owedErr := s.store.OpenObligations(ctx)
	live, liveErr := s.store.LiveTasks(ctx)
	if liveErr != nil {
		// One unreadable input makes the projection incomplete, not wrong in
		// one place: it is carried as missing evidence rather than as zero.
		owedErr = liveErr
	}

	// Only assistant sessions are rows. A terminal running an ordinary shell is
	// not a session in this contract — the Swift app carries shells as an
	// attribute of the session that left them running, and publishing them as
	// rows of their own turned eight cards into eighteen, ten of which the
	// console could only describe as unreadable.
	rows := make([]contract.SessionRow, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		rows = append(rows, sessionRow(item, owed, owedErr, live))
	}

	return contract.SessionsSnapshot{
		At:       time.Now().Unix(),
		Sessions: rows,
		Scan: contract.Scan{
			Epoch:      epoch,
			Generation: generation.Add(1),
			Complete:   inv.Complete,
			Provenance: inv.Provenance,
			// An empty list is only authoritative when the reading was
			// complete. Saying so here is what stops a client from treating a
			// failed scan as "every session went away".
			EmptyAuthoritative: inv.Complete && len(rows) == 0,
			Completed: contract.ScanCompleted{
				Sequence: generation.Load(),
				Complete: inv.Complete,
			},
		},
	}
}

// sessionRow renders one session in the shape the console reads.
//
// Fields this daemon cannot yet support are left at their zero value and the
// contract marks them optional, so they are absent rather than invented. The
// contract already says several of them are absent in ordinary cases, so a
// reader that handles absence handles this too.
func sessionRow(item session.Session, owed []task.Obligation, owedErr error, live []task.Task) contract.SessionRow {
	return contract.SessionRow{
		ID:       item.ID,
		Backend:  contract.Backend(item.Backend),
		State:    contract.SessionState(item.State),
		IsClaude: item.Assistant == session.AssistantClaude,
		// Not in the Swift contract: how this row's state was learned. A
		// registry reading and a screen guess are different kinds of fact.
		Evidence:     contract.Evidence(item.Evidence),
		WorkState:    contract.WorkState(workState(item, owed, owedErr, live)),
		Closeability: closeability(item, owed, owedErr),
		TTY:          item.TTY,
		Assistant:    contract.Assistant(item.Assistant),
		Label:        item.Label,
		CWD:          item.CWD,
		SessionID:    item.ConversationID,
	}
}

// closeability carries the domain's answer across to the wire.
//
// The decision is not made here. The fleet list and the close action must give
// one answer, so they ask one function; this only renames its parts.
func closeability(item session.Session, owed []task.Obligation, err error) contract.Closeability {
	c := task.Closeability(item.ID, owed, err)
	reasons := make([]contract.CloseReason, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		reasons = append(reasons, contract.CloseReason{
			Kind:  string(r.Kind),
			Mover: wireMover(r.Mover),
			Note:  r.Note,
		})
	}
	return contract.Closeability{State: contract.CloseabilityState(c.State), Reasons: reasons}
}

// wireMover is the one place a mover crosses from the domain to the wire. It is
// a function rather than a literal in three handlers because a mover that means
// something different in the obligation list than in a close reason is exactly
// the drift this contract exists to prevent.
func wireMover(m task.Mover) contract.Mover {
	return contract.Mover{Kind: contract.MoverKind(m.Kind), ID: m.ID}
}

// workState gathers this session's axes and hands them to the projection.
func workState(item session.Session, owed []task.Obligation, owedErr error, live []task.Task) task.WorkState {
	in := task.WorkInputs{
		AskedOnScreen:          item.State == session.StateWaiting,
		EvidenceMissing:        owedErr != nil || item.State == session.StateUnknown,
		Active:                 item.State == session.StateWorking,
		PromptWithoutAssistant: !item.IsAssistant() && item.State == session.StateIdle,
	}
	for _, o := range owed {
		switch {
		case o.Mover.Kind == task.MoverOtherSession && o.Subject == item.ID:
			in.WaitingOnPeer = true
		case o.Mover.Kind == task.MoverTask && o.Subject != "":
			// A task obligation belongs to whoever dispatched it; without that
			// link recorded yet this cannot claim the session, so it does not.
		}
	}
	_ = live
	return task.ProjectWorkState(in)
}
