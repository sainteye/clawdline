package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// Runs issues a run for each message a person sends a session through this
// daemon, and answers what a run was (design-decisions U4; the rules are
// internal/domain/work/runs.go).
//
// A run is a fact that already happened — bytes a person had typed into a
// terminal — so it is recorded the way the store records such facts, as an
// event appended after the typing (store.Append), and it is never taken back.
// Two events say it: `run.issued` under the run's id, which is how a relay is
// checked, and `run.latest` under the session's conversation id and its
// terminal id, which is how a session finds the run of the message it is
// answering. Both are one indexed seek (the events table's kind-and-subject
// index); nothing is scanned, and nothing is kept in memory, so a run issued
// before a restart is still a run after it.
type Runs struct {
	Store *store.Store
	// Now is the clock a run is stamped with; nil is the wall clock.
	Now func() time.Time
}

// The events a run is recorded as.
const (
	EventRunIssued = "run.issued"
	EventRunLatest = "run.latest"
)

type runPayload struct {
	Run       string `json:"run"`
	Session   string `json:"session_id"`
	Terminal  string `json:"terminal_id"`
	Assistant string `json:"assistant,omitempty"`
	Principal string `json:"principal"`
	At        int64  `json:"at"`
}

func (r *Runs) now() time.Time {
	if r.Now != nil {
		return seconds(r.Now())
	}
	return seconds(time.Now())
}

// newRunID is a run's id: a lowercase UUID nobody can guess, so naming one is
// evidence of having been told it.
func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Issue records that a person, by principal, sent sess a message that was
// typed into it, and answers the run.
func (r *Runs) Issue(ctx context.Context, sess session.Session, principal string) (work.Run, error) {
	run := work.Run{ID: newRunID(), Session: sess.ConversationID, Terminal: sess.ID,
		Assistant: string(sess.Assistant), Principal: principal, At: r.now()}
	raw, _ := json.Marshal(runPayload{Run: run.ID, Session: run.Session, Terminal: run.Terminal,
		Assistant: run.Assistant, Principal: run.Principal, At: run.At.Unix()})
	if err := r.Store.Append(ctx, store.Event{Kind: EventRunIssued, Subject: run.ID, Payload: raw}); err != nil {
		return work.Run{}, err
	}
	// Found by the session under either name it knows itself by. Written
	// after the run itself, so the worst a failure here leaves is a run the
	// session cannot find — it relays nothing — never one it can find that is
	// not there.
	for _, subject := range []string{run.Session, run.Terminal} {
		if subject == "" {
			continue
		}
		if err := r.Store.Append(ctx, store.Event{Kind: EventRunLatest, Subject: subject, Payload: raw}); err != nil {
			return run, err
		}
	}
	return run, nil
}

func runOf(e store.Event) (work.Run, bool) {
	var p runPayload
	if json.Unmarshal(e.Payload, &p) != nil || p.Run == "" {
		return work.Run{}, false
	}
	at := time.Unix(p.At, 0)
	if p.At == 0 {
		at = e.At
	}
	return work.Run{ID: p.Run, Session: p.Session, Terminal: p.Terminal, Assistant: p.Assistant,
		Principal: p.Principal, At: at}, true
}

func (r *Runs) read(ctx context.Context, kind, subject string) (work.Run, bool, error) {
	got, err := r.Store.LatestEvents(ctx, kind, []string{subject})
	if err != nil {
		return work.Run{}, false, err
	}
	e, ok := got[subject]
	if !ok {
		return work.Run{}, false, nil
	}
	run, ok := runOf(e)
	return run, ok, nil
}

// Find answers the run with that id, and whether this daemon ever issued it.
func (r *Runs) Find(ctx context.Context, id string) (work.Run, bool, error) {
	if !work.RunShaped(id) {
		return work.Run{}, false, nil
	}
	return r.read(ctx, EventRunIssued, id)
}

// Latest answers the run of the newest message a person sent the session
// named by its conversation id or its terminal id.
func (r *Runs) Latest(ctx context.Context, session string) (work.Run, bool, error) {
	if session == "" {
		return work.Run{}, false, nil
	}
	return r.read(ctx, EventRunLatest, session)
}

// Relay is the run a relay names, checked as a relay's run: issued by this
// daemon, and recent. A store that could not be read is not an absent run: it
// is refused as unavailable, so a relay is never believed on no evidence nor
// refused as a lie on none.
func (r *Runs) Relay(ctx context.Context, id string) (work.Run, error) {
	run, found, err := r.Find(ctx, id)
	if err != nil {
		return work.Run{}, workRefusal(503, "store_unavailable", "The run could not be read; nothing was done. Retry.")
	}
	if err := work.CheckRun(run, found, r.now()); err != nil {
		var refused *work.Refusal
		if errors.As(err, &refused) {
			return work.Run{}, workRefusal(refused.Status, refused.Code, refused.Message)
		}
		return work.Run{}, err
	}
	return run, nil
}
