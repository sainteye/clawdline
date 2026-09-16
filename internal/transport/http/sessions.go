package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/icon"
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
	// One generation for the whole snapshot, taken before the rows are built,
	// so every row's closeability names the same reading it arrived with.
	gen := generation.Add(1)
	rows := make([]contract.SessionRow, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		rows = append(rows, sessionRow(item, owed, owedErr, live, s.icons, gen))
	}

	return contract.SessionsSnapshot{
		At:       time.Now().Unix(),
		Sessions: rows,
		Scan: contract.Scan{
			Epoch:      epoch,
			Generation: gen,
			Complete:   inv.Complete,
			Provenance: inv.Provenance,
			// An empty list is only authoritative when the reading was
			// complete. Saying so here is what stops a client from treating a
			// failed scan as "every session went away".
			EmptyAuthoritative: inv.Complete && len(rows) == 0,
			Completed: contract.ScanCompleted{
				Sequence: gen,
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
func sessionRow(item session.Session, owed []task.Obligation, owedErr error, live []task.Task, marks *icon.Registry, gen int64) contract.SessionRow {
	return contract.SessionRow{
		Icon:     wireIcon(marks.For(item.CWD)),
		ID:       item.ID,
		Backend:  contract.Backend(item.Backend),
		State:    contract.SessionState(item.State),
		IsClaude: item.Assistant == session.AssistantClaude,
		// Not in the Swift contract: how this row's state was learned. A
		// registry reading and a screen guess are different kinds of fact.
		Evidence:  contract.Evidence(item.Evidence),
		WorkState: contract.WorkState(workState(item, owed, owedErr, live)),
		// This daemon projected it; nothing here takes a session's own word for
		// what it needs, so `self` is never claimed.
		WorkProvenance: contract.WorkProvenanceBroker,
		Closeability:   closeability(item, owed, owedErr, time.Now(), gen),
		TTY:            item.TTY,
		Assistant:      contract.Assistant(item.Assistant),
		Label:          item.Label,
		CWD:            item.CWD,
		SessionID:      item.ConversationID,
	}
}

// closeability carries the domain's answer across to the wire, with what the
// reader needs in order to believe it.
//
// The decision is not made here; the fleet list and the close action ask one
// function. What is added is the evidence around it, because `safe` is a
// positive claim and the console refuses to draw it as one without a current
// source, an attestation id and a version. That bar is the right way round: a
// daemon that has not proved anything should not be able to say `safe` merely
// by leaving fields out.
func closeability(item session.Session, owed []task.Obligation, err error, now time.Time, gen int64) contract.Closeability {
	c := task.Closeability(item.ID, owed, err)
	reasons := make([]contract.CloseReason, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		reasons = append(reasons, contract.CloseReason{
			Kind:        "obligation",
			Code:        string(r.Kind),
			SubjectID:   r.Mover.ID,
			SubjectKind: string(r.Mover.Kind),
			Mover:       wireCloseMover(r.Mover, item.ID),
		})
	}
	out := contract.Closeability{
		SessionGeneration: gen,
		State:             contract.CloseabilityState(c.State),
		Reasons:           reasons,
		Mover:             overallMover(c, item.ID),
		ObservedAt:        now.Unix(),
		// One entry, and it is this daemon. The list exists because a reading
		// can have more than one source and a reader should be able to see
		// which; claiming more than one here would be the lie.
		Provenance: []string{"broker"},
		Version:    task.Version,
		Source: contract.CloseSource{
			Provenance:    "broker",
			ObservedAt:    now.Unix(),
			MaxAgeSeconds: int64(task.MaxAge / time.Second),
			Freshness:     task.Freshness(now, now),
		},
	}
	if id := task.Attest(item.ID, c.State, owed, now); id != "" {
		out.AttestationID = &id
	}
	return out
}

// wireCloseMover says who clears one reason.
//
// `self` is the distinction that matters to a reader: a thing this session has
// to do is a thing they can do now, and a thing another session has to do is
// only something to wait for.
func wireCloseMover(m task.Mover, subject string) contract.CloseMover {
	out := contract.CloseMover{Kind: "session"}
	switch m.Kind {
	case task.MoverPerson:
		out.Kind = "person"
		out.PersonNeeded = true
	case task.MoverBroker:
		out.Kind = "broker"
	case task.MoverTask:
		out.Kind = "task"
	}
	out.Self = m.ID == subject
	return out
}

// overallMover is who has to move for the session as a whole.
//
// A person, if any reason needs one: the loudest obligation decides, because a
// row that says "this session can clear it" while one of its reasons needs a
// person would send somebody away from the thing only they can do.
func overallMover(c task.Close, subject string) contract.CloseMover {
	out := contract.CloseMover{Kind: "session", Self: true}
	for _, r := range c.Reasons {
		m := wireCloseMover(r.Mover, subject)
		if m.PersonNeeded {
			return m
		}
		if !m.Self {
			out = m
		}
	}
	return out
}

// wireIcon carries a mark across to the wire in the shape the console draws.
func wireIcon(g icon.Grid) *contract.Icon {
	if len(g.Cells) == 0 {
		return nil
	}
	return &contract.Icon{Accent: g.Accent, Cells: g.Cells}
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
