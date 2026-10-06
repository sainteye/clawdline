package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"net/http"
	"strings"
)

// Cancelling a child its root dispatched by mistake: a wrong brief, a wrong
// scope, a duplicate. `cancelled` was a state every reader knew and nothing
// wrote, so such a child could only be waited out — holding a child slot, its
// claims and a tab — until it finished or timed out.
//
// It is one more way into Settle, and nothing else: the settlement that
// records a timeout records this, with the same landing, notice and tab plan in
// one transaction. What a cancel adds is the close of the child's tab
// (closeChild, recorded as an outbox effect in that transaction, so a restart
// between the commit and the close runs it then rather than never) and the
// caller's key on the record, which is what answers a resend as the cancel
// that already succeeded.
//
// Who may: the root Session that dispatched the task, or the person through
// the console's own credential. The machine token alone is not enough — every
// Session on this machine holds it — so a Session caller is named by its
// squad capability when it has one, and otherwise by the conversation id it
// sends. That second form is the legacy path every capability-less Session
// already takes for its Board writes (transport/http requireSquadWorkActor),
// and it is refused for a root that does carry a capability: naming a bound
// root's conversation without its capability cancels nothing.

// CancelReasonLimit is the longest reason a cancel may carry, in bytes after
// its whitespace is collapsed. It becomes the task's verdict and a clause of
// the completion line typed into the root, so it is a sentence, not a report.
const CancelReasonLimit = 500

// cancelKeyLimit is the longest Idempotency-Key a cancel keeps, as the board's
// receipts do (transport/http workKey).
const cancelKeyLimit = 200

// Cancellation is who cancelled a task and under which request key.
type Cancellation struct {
	// By is "root:<conversation id>" or "person:<principal>".
	By string `json:"by"`
	// Key is the Idempotency-Key of the request that cancelled it; empty when
	// it carried none, and then no resend is a replay.
	Key string `json:"key,omitempty"`
}

// CancelCaller is who is asking, as the route learned it.
type CancelCaller struct {
	// Person is the console's own credential: this machine's browser or a
	// paired device that may send. Principal names it ("local", "device:…").
	Person    bool
	Principal string
	// Capability is the Session's squad capability, when it sent one;
	// SessionID is the conversation id it named, used only without one.
	Capability string
	SessionID  string
}

// Cancelled is what a cancel answers: the task as it now stands, and whether
// this was a resend of a cancel that had already succeeded.
type Cancelled struct {
	Record   Record
	Replayed bool
}

// CancelTask settles a live task `cancelled`, stopping its child's tab.
func (b *Broker) CancelTask(ctx context.Context, id, reason, key string, c CancelCaller) (Cancelled, error) {
	if !IsTaskID(id) {
		return Cancelled{}, refuse(http.StatusUnprocessableEntity, "bad_task", "task_id must be a lowercase UUID.")
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return Cancelled{}, refuse(http.StatusBadRequest, "reason_required",
			"Say why the task is cancelled: --reason \"wrong brief\", \"wrong scope\", \"duplicate of <id>\".")
	}
	if len(reason) > CancelReasonLimit {
		return Cancelled{}, refuseWith(http.StatusUnprocessableEntity, "reason_too_long",
			fmt.Sprintf("The reason is %d bytes; a cancel's reason is at most %d.", len(reason), CancelReasonLimit),
			map[string]any{"limit": CancelReasonLimit})
	}
	key = strings.TrimSpace(key)
	if len(key) > cancelKeyLimit {
		return Cancelled{}, refuse(http.StatusBadRequest, "bad_idempotency_key",
			fmt.Sprintf("An Idempotency-Key is at most %d characters.", cancelKeyLimit))
	}
	r, _, err := b.Record(ctx, id)
	if err != nil {
		return Cancelled{}, err
	}
	by, who, err := b.cancelCaller(ctx, r, c)
	if err != nil {
		return Cancelled{}, err
	}
	if r.State.Terminal() {
		return cancelledAlready(r, key)
	}
	if r.Gate != nil || r.Kind == TaskKindVerificationGate {
		return Cancelled{}, refuseWith(http.StatusConflict, "gate_task_not_cancellable",
			"This is a verification gate's task; its round is the Board's to decide, and it is not cancelled from here.",
			map[string]any{"state": string(r.State)})
	}
	verdict := "Cancelled by " + who + ": " + reason
	stop := b.closeChild(r, r.ChildTerminalID != "")
	if r.Callback != nil {
		stop = []store.Effect{callbackStopEffect(r)}
	}
	settled, ids, err := b.settleWith(ctx, id, StateCancelled, verdict, nil, func(rec *Record) {
		rec.Cancellation = &Cancellation{By: by, Key: key}
	}, stop...)
	if errors.Is(err, errAlreadyTerminal) {
		// Somebody settled it between the read and the write — a result
		// collected, the timeout, or this same cancel sent twice at once.
		now, _, rerr := b.Record(ctx, id)
		if rerr != nil {
			return Cancelled{}, rerr
		}
		return cancelledAlready(now, key)
	}
	if err != nil {
		return Cancelled{}, err
	}
	b.runRecorded(ctx, ids)
	return Cancelled{Record: settled}, nil
}

// cancelledAlready answers a cancel of a task that has already ended: the
// same success when it is a resend of the cancel that ended it, a conflict
// naming the state otherwise.
func cancelledAlready(r Record, key string) (Cancelled, error) {
	if r.State == StateCancelled && r.Cancellation != nil && key != "" && r.Cancellation.Key == key {
		return Cancelled{Record: r, Replayed: true}, nil
	}
	return Cancelled{}, refuseWith(http.StatusConflict, "task_already_terminal",
		fmt.Sprintf("Task %s has already ended (%s); there is nothing to cancel.", r.ID, r.State),
		map[string]any{"state": string(r.State)})
}

// cancelCaller decides whether c may cancel r, and answers how the record and
// the verdict name the caller.
func (b *Broker) cancelCaller(ctx context.Context, r Record, c CancelCaller) (by, who string, err error) {
	if c.Person {
		return "person:" + c.Principal, "the person", nil
	}
	root := ""
	if r.Root != nil {
		root = r.Root.SessionID
	}
	conversation, bound := strings.TrimSpace(c.SessionID), false
	if c.Capability != "" {
		actor, ok, err := b.Store.AuthenticateSquadActor(ctx, c.Capability)
		if err != nil {
			return "", "", refuse(http.StatusServiceUnavailable, "store_unavailable",
				"The calling Session's capability could not be checked.")
		}
		if !ok {
			return "", "", refuse(http.StatusForbidden, "session_actor_required",
				"That squad capability is not a current Session's.")
		}
		conversation, bound = actor.ConversationID, true
	}
	if root == "" {
		return "", "", refuse(http.StatusForbidden, "not_task_root",
			"This task has no root Session; only the person, from the console, may cancel it.")
	}
	if conversation == "" {
		return "", "", refuse(http.StatusBadRequest, "session_required",
			"Name the calling Session: send its squad capability, or its conversation id as session_id.")
	}
	if conversation != root {
		return "", "", refuseWith(http.StatusForbidden, "not_task_root",
			"Only the root Session that dispatched this task ("+rootName(r)+") or the person, from the console, "+
				"may cancel it.", map[string]any{"root_session": root})
	}
	if !bound {
		_, _, hasSnapshot, err := b.Store.SquadSnapshotForConversation(ctx, root)
		if err != nil {
			return "", "", refuse(http.StatusServiceUnavailable, "store_unavailable",
				"Whether the root Session carries a squad capability could not be checked.")
		}
		if hasSnapshot {
			return "", "", refuse(http.StatusForbidden, "session_actor_required",
				"The root Session that dispatched this task carries a squad capability; a cancel from it must send it.")
		}
	}
	return "root:" + root, "its root", nil
}

// rootName is how a refusal names a task's root Session for a person: its
// label when it has one, with the conversation id.
func rootName(r Record) string {
	if r.Root == nil {
		return "none"
	}
	if r.Root.Label != "" {
		return r.Root.Label + ", conversation " + r.Root.SessionID
	}
	return "conversation " + r.Root.SessionID
}

// cancelledBranchNote is a cancelled task's pending landing when its branch
// carries commits: the branch is kept, and the root is told how much is on it.
func cancelledBranchNote(branch string, commits int) string {
	return fmt.Sprintf("cancelled; branch %s was kept and has %d commit(s) to look at", branch, commits)
}
