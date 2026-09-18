package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Linger: closing a finished child's tab a little after it finished
// (broker-design #26, E5; docs/design-decisions.md D12).
//
// The Swift app kept this deadline in memory, and a restart forgot it:
// "Seventeen of the eighteen tabs left standing had a restart inside their
// three minutes" (`3a7adb8e`). Its fix is carried whole:
//
//   - the deadline is a row in the store, written by the settlement that owes
//     it, in the same transaction (store/handover.go);
//   - a deadline that passed while no broker was running is given twenty
//     seconds from this broker's first pass, so the first reading of the
//     machine is in before anything is decided — D12 puts that grace here,
//     and nowhere near the task's own timeout;
//   - "a reading with no terminals in it at all is no longer allowed to
//     decide": a tab is closed, or its linger dropped as already gone, only
//     on a reading that saw terminals and whose source for that tab answered
//     completely. Anything else waits.
//
// Only a tmux child is lingered: its session is one this broker made, named
// for the task, and closed only while the pane it recorded is still one of its
// panes (runCloseChild). An iTerm2 tab has no close this daemon can prove.

// LingerDefault is the Swift app's `orchestrator_child_linger`, 180 seconds
// (Config.swift:478).
const LingerDefault = 180 * time.Second

// lingerRestartGrace is the Swift app's `restartGrace` (Orchestrator.swift:6567).
const lingerRestartGrace = 20 * time.Second

func (b *Broker) childLinger() time.Duration {
	if b.ChildLinger == nil {
		return LingerDefault
	}
	return b.ChildLinger()
}

// lingerFor is the close a settlement owes its child's tab, if any: a tmux
// child that ended in success or failure, on a machine whose setting does not
// keep finished children open.
func (b *Broker) lingerFor(r Record, now time.Time) (store.Linger, bool) {
	if r.State != StateSuccess && r.State != StateFailure {
		return store.Linger{}, false
	}
	if r.ChildBackend != "tmux" || r.ChildTerminalID == "" || b.Launcher == nil {
		return store.Linger{}, false
	}
	d := b.childLinger()
	if d < 0 {
		return store.Linger{}, false
	}
	return store.Linger{Task: r.ID, Backend: r.ChildBackend, Pane: r.ChildTerminalID,
		Session: ChildSessionName(r.ID), Deadline: now.Add(d), CreatedAt: now}, true
}

// closeLingers closes the tabs whose linger is over, and answers how many
// closes it recorded. It runs on the beat, against the pass's one reading.
func (b *Broker) closeLingers(ctx context.Context, rd reading) int {
	now := b.now()
	if b.lingerStarted.IsZero() {
		b.lingerStarted = now
	}
	rows, err := b.Store.Lingers(ctx)
	if err != nil || len(rows) == 0 {
		return 0
	}
	n := 0
	for _, l := range rows {
		due := l.Deadline
		if !due.After(b.lingerStarted) {
			// It passed while no broker was watching: the first reading of
			// this one decides, not the clock that ran out in the dark.
			due = b.lingerStarted.Add(lingerRestartGrace)
		}
		if now.Before(due) {
			continue
		}
		if len(rd.sessions) == 0 || !rd.sourceComplete(l.Backend) {
			continue
		}
		s, present := rd.session(l.Pane)
		if present && s.State != session.StateIdle {
			// Only a tab that is plainly at rest is closed. Working is the
			// child still finishing a turn; waiting is a question on its
			// screen somebody may be about to answer; unknown is a screen
			// this daemon could not read — and a close on unknown is the
			// removal on unknown DG-7 forbids. The linger stays owed and is
			// asked again next pass.
			continue
		}
		payload, _ := json.Marshal(map[string]any{"task": l.Task, "pane": l.Pane, "session": l.Session,
			"deadline": l.Deadline.Unix(), "present": present})
		var effects []store.Effect
		kind := "task.child.linger.gone"
		if present {
			kind = "task.child.linger.due"
			body, _ := json.Marshal(closeChildEffect{Pane: l.Pane, Session: l.Session})
			effects = []store.Effect{{Kind: EffectCloseChild, Subject: l.Task, Payload: body}}
		}
		ids, err := b.Store.TakeLinger(ctx, l.Task, []store.Event{{Kind: kind, Subject: l.Task, Payload: payload}}, effects)
		if errors.Is(err, store.ErrNoLinger) || err != nil {
			continue
		}
		for _, res := range b.runRecorded(ctx, ids) {
			if res.state == store.EffectDone && res.outcome == "closed" {
				n++
			}
		}
	}
	return n
}
