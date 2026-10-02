package orchestrator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A Root Assignment left at `terminal_opened`, or a handoff left at
// `opening`, is one whose opening request never finished: both states last
// only as long as OpenRootAssignment or OpenHandoff is running, which waits
// composerWait (150 s) at most and touches the row as it goes. A daemon that
// stopped in between left the row there with nothing to move it on — Epic
// 914a2435 finding G6 found one that had sat at `terminal_opened` since
// 2026-09-29, its Board item still claiming somebody was being briefed.
//
// So the beat judges a row unchanged in either state for longer than
// OpeningStuckSecondsLimit failed, with a reason code of its own. The state is
// read again inside the write: a row that moved on, or was touched, between
// the list and the write is left as it is, and a row already failed is not in
// either state, so a second pass finds nothing to do.

// OpeningStuckSecondsLimit is how long a Root Assignment may stay at
// terminal_opened, or a handoff at opening, without a change before the beat
// judges it failed. Registered as capacity.OpeningStuckSeconds.
const OpeningStuckSecondsLimit = 30 * 60

// The reason codes a stuck opening is failed with; each is also the suffix of
// the event recorded with it.
const (
	ReasonTerminalOpenTimeout = "terminal_open_timeout"
	ReasonHandoffOpenTimeout  = "handoff_open_timeout"
)

const (
	terminalOpenTimeoutFailure = ReasonTerminalOpenTimeout + ": the terminal was opened and the Feature Root was " +
		"never briefed; nothing changed for 30 minutes, so the opening is over"
	handoffOpenTimeoutFailure = ReasonHandoffOpenTimeout + ": the receiver was never given the handoff line; " +
		"nothing changed for 30 minutes, so the opening is over"
)

// stuckOpening answers whether a row last changed at updated has been left
// longer than the limit.
func (b *Broker) stuckOpening(updated time.Time) bool {
	return b.now().Sub(updated) > OpeningStuckSecondsLimit*time.Second
}

// failStuckOpenings is the beat's look at both states. It answers how many
// rows it failed.
func (b *Broker) failStuckOpenings(ctx context.Context) int {
	return b.failStuckAssignments(ctx) + b.failStuckHandoffs(ctx)
}

func (b *Broker) failStuckAssignments(ctx context.Context) int {
	rows, err := b.Store.ListOpenedIn(ctx, store.TableRootAssignments, AssignmentTerminalOpened, assignmentListLimit)
	if err != nil {
		return 0
	}
	failed := 0
	for _, o := range rows {
		if !b.stuckOpening(o.UpdatedAt) {
			continue
		}
		var out RootAssignment
		changed := false
		_, err := b.Store.UpdateOpened(ctx, store.TableRootAssignments, o.ID, func(cur store.Opened) (*store.Opened, []store.Event, error) {
			var a RootAssignment
			if cur.State != AssignmentTerminalOpened || !b.stuckOpening(cur.UpdatedAt) ||
				json.Unmarshal(cur.Record, &a) != nil || a.State != AssignmentTerminalOpened {
				return nil, nil, nil
			}
			a.State, a.Failure = AssignmentFailed, terminalOpenTimeoutFailure
			out, changed = a, true
			body, _ := json.Marshal(a)
			payload, _ := json.Marshal(map[string]any{"root_assignment": a.ID, "state": a.State,
				"reason": ReasonTerminalOpenTimeout})
			return &store.Opened{ID: cur.ID, State: a.State, Record: body, CreatedAt: cur.CreatedAt, UpdatedAt: b.now()},
				[]store.Event{{Kind: "root_assignment." + ReasonTerminalOpenTimeout, Subject: a.ID, Payload: payload}}, nil
		})
		if err != nil || !changed {
			continue
		}
		failed++
		if b.RootAssignmentSettled != nil {
			b.RootAssignmentSettled(ctx, out)
		}
	}
	return failed
}

func (b *Broker) failStuckHandoffs(ctx context.Context) int {
	rows, err := b.Store.ListOpenedIn(ctx, store.TableHandoffs, HandoffOpening, handoffListLimit)
	if err != nil {
		return 0
	}
	failed := 0
	for _, o := range rows {
		if !b.stuckOpening(o.UpdatedAt) {
			continue
		}
		var out Handoff
		changed := false
		_, err := b.Store.UpdateOpened(ctx, store.TableHandoffs, o.ID, func(cur store.Opened) (*store.Opened, []store.Event, error) {
			h, derr := decodeHandoff(cur)
			if cur.State != HandoffOpening || !b.stuckOpening(cur.UpdatedAt) || derr != nil || h.State != HandoffOpening {
				return nil, nil, nil
			}
			// spawn_failed is the handoff's one failed state on the wire: the
			// receiver was not given the line.
			h.State, h.Failure = HandoffSpawnFailed, handoffOpenTimeoutFailure
			out, changed = h, true
			body, _ := json.Marshal(h)
			payload, _ := json.Marshal(map[string]any{"handoff": h.ID, "state": h.State,
				"reason": ReasonHandoffOpenTimeout})
			return &store.Opened{ID: cur.ID, State: h.State, Record: body, CreatedAt: cur.CreatedAt, UpdatedAt: b.now()},
				[]store.Event{{Kind: "handoff." + ReasonHandoffOpenTimeout, Subject: h.ID, Payload: payload}}, nil
		})
		if err != nil || !changed {
			continue
		}
		failed++
		// The sender was never told how its handoff went; it is now.
		if out.Receipt == "" {
			b.handoffReceipt(ctx, out)
		}
	}
	return failed
}
