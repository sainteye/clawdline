package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// Actions carries out what a person asks of one session: type into it, stop
// what it is doing, or take it away.
//
// Every one of them goes through here rather than straight to a terminal host,
// because each has a precondition that is not the terminal's to check. The
// terminal will happily close a session that still owes somebody a landing.
type Actions struct {
	Inventory Inventory
	Terminals []ports.TerminalHost
	Store     *store.Store
}

// Refusal is a typed no, in the shape every refusal on this daemon has.
type Refusal struct {
	Code   string
	Detail string
	// Reasons is filled when a close is refused, so the caller can say what is
	// in the way rather than only that something is.
	Reasons []task.CloseReason
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

// Find resolves one session id against a current reading of the machine.
//
// The two ways this fails are deliberately different codes. A complete reading
// that does not contain the id has proved the session is gone. An incomplete
// one has proved nothing, and answering `not_found` there would turn a terminal
// that lost accessibility for a moment into a session somebody deleted.
func (a Actions) Find(ctx context.Context, id string) (session.Session, error) {
	inv := a.Inventory.Read(ctx)
	for _, item := range inv.Sessions {
		if item.ID == id {
			return item, nil
		}
	}
	if !inv.Complete {
		return session.Session{}, Refusal{
			Code:   "session_unknown",
			Detail: fmt.Sprintf("this reading of the machine was incomplete (%s), so %s is not absent, it is unseen", inv.Provenance, id),
		}
	}
	return session.Session{}, Refusal{
		Code:   "session_not_found",
		Detail: fmt.Sprintf("no session %s in a complete reading of this machine", id),
	}
}

// host returns the terminal that owns this session.
func (a Actions) host(s session.Session) (ports.TerminalHost, error) {
	for _, h := range a.Terminals {
		if h.Name() == string(s.Backend) {
			return h, nil
		}
	}
	return nil, Refusal{
		Code:   "backend_unsupported",
		Detail: fmt.Sprintf("nothing on this machine drives a %q session", s.Backend),
	}
}

// Send types one line into a session and submits it.
//
// A nil error means the bytes reached the tty. It never means the assistant
// read them, and callers must not report it as delivery: whether a turn was
// taken is a separate fact with its own evidence, and the fleet list is where
// that answer lives.
func (a Actions) Send(ctx context.Context, id, text string) (session.Session, error) {
	if text == "" {
		return session.Session{}, Refusal{Code: "empty_text", Detail: "there is nothing to type"}
	}
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	if err := h.Send(ctx, s, text); err != nil {
		return s, Refusal{Code: "send_failed", Detail: err.Error()}
	}
	a.record(ctx, "session.typed", s.ID, map[string]any{"bytes": len(text)})
	return s, nil
}

// Interrupt stops the current turn without closing the session.
func (a Actions) Interrupt(ctx context.Context, id string) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	if err := h.Interrupt(ctx, s); err != nil {
		return s, Refusal{Code: "interrupt_failed", Detail: err.Error()}
	}
	a.record(ctx, "session.interrupted", s.ID, nil)
	return s, nil
}

// Close takes the session away, once nothing is owed.
//
// The precondition is the same projection the fleet list draws, asked of the
// same function, so a row that reads `safe` closes and a row that reads
// `blocked` refuses with the very reasons it was showing. `force` exists
// because a person may know something the obligations do not, but it cannot
// override `unknown`: overriding a refusal is a decision, and there is nothing
// to decide about when the list could not be read.
func (a Actions) Close(ctx context.Context, id string, force bool) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	owed, owedErr := a.Store.OpenObligations(ctx)
	c := task.Closeability(s.ID, owed, owedErr)
	switch c.State {
	case task.CloseUnknown:
		return s, Refusal{
			Code:   "closeability_unknown",
			Detail: "what this session owes could not be read, and an unreadable list is not an empty one",
		}
	case task.CloseBlocked:
		if !force {
			return s, Refusal{
				Code:    "close_blocked",
				Detail:  fmt.Sprintf("still owed: %s", summarise(c.Reasons)),
				Reasons: c.Reasons,
			}
		}
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	if err := h.Close(ctx, s); err != nil {
		return s, Refusal{Code: "close_failed", Detail: err.Error()}
	}
	a.record(ctx, "session.closed", s.ID, map[string]any{"forced": force, "owed": len(c.Reasons)})
	return s, nil
}

// summarise names what is in the way, rather than counting it.
//
// A count sends a person looking; a list is the answer. The reasons travel on
// the refusal too, so this is the sentence, not the data.
func summarise(reasons []task.CloseReason) string {
	kinds := make([]string, 0, len(reasons))
	seen := map[task.Kind]bool{}
	for _, r := range reasons {
		if seen[r.Kind] {
			continue
		}
		seen[r.Kind] = true
		kinds = append(kinds, string(r.Kind))
	}
	return strings.Join(kinds, ", ")
}

// record writes what happened. A failure to record is logged by the store and
// does not undo the action: the bytes are already typed, and pretending
// otherwise would make the record less true, not more.
func (a Actions) record(ctx context.Context, kind, subject string, payload map[string]any) {
	if a.Store == nil {
		return
	}
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	_ = a.Store.Append(ctx, store.Event{Kind: kind, Subject: subject, Payload: raw})
}
