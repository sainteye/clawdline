package orchestrator

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// The manual path a dead letter needs, as the Swift app has it
// (RemoteServer.swift `/v1/orchestrator/completions`, `…/reconcile`).
//
// A notice goes to dead letter when eight attempts found nobody to
// acknowledge it. That is the end of the machine's patience, not of the
// person's: they can list what nobody acknowledged, fix what was wrong (the
// root was closed, or kept a menu open for an hour), and put an envelope back
// at the start of its ladder with its notice id kept, so the ACK it asks for
// is the one the root would have sent. Or they can acknowledge it themselves
// through the ordinary ACK route, which takes a dead letter as it takes any
// other state.

const (
	// reconcileBatch is the Swift app's per-call ceiling on re-arming.
	reconcileBatch = 25
	// completionsLookback is how far back `pending=false` reads acknowledged
	// envelopes (the Swift app's lookback_seconds).
	completionsLookback = 7 * 24 * time.Hour
	// completionsPage bounds one listing.
	completionsPage = 500
)

// Completion is one envelope with the task it announces.
type Completion struct {
	Record Record
	Notice Notice
}

// Completions lists the completion ledger: every envelope nobody has
// acknowledged — dead letters included, which the Swift app's `pending=true`
// left out and which are the ones a person is looking for — or, with all,
// the acknowledged ones of the last week as well.
func (b *Broker) Completions(ctx context.Context, all bool) ([]Completion, error) {
	rows, err := b.Store.CompletionNotices(ctx, b.now().Add(-completionsLookback), all, completionsPage)
	if err != nil {
		return nil, storeError(err)
	}
	out := make([]Completion, 0, len(rows))
	for _, n := range rows {
		r, _, err := b.Record(ctx, n.TaskID)
		if err != nil {
			// Listed from the envelope alone: an envelope whose task cannot
			// be read is still an envelope nobody acknowledged.
			r = Record{ID: n.TaskID, State: StateUnreadable}
		}
		out = append(out, Completion{Record: r, Notice: *noticeOf(n)})
	}
	return out, nil
}

// Rearm puts envelopes back at the start of their ladder: pending, zero
// attempts, due now, no error, notice id kept. Without includeDeadLetter only
// envelopes still on the ladder are touched — re-arming a dead letter is the
// person's decision, taken once its cause is fixed. At most reconcileBatch
// per call; limited says there were more.
func (b *Broker) Rearm(ctx context.Context, taskID string, includeDeadLetter bool) (rearmed []string, limited bool, err error) {
	if taskID != "" && !IsTaskID(taskID) {
		return nil, false, refuse(http.StatusBadRequest, "bad_request", "task_id must be one lowercase UUID.")
	}
	var rows []store.BrokerNotice
	if taskID != "" {
		if _, _, err := b.Record(ctx, taskID); err != nil {
			return nil, false, err
		}
		n, err := b.Store.BrokerNotice(ctx, taskID)
		if err != nil {
			return nil, false, refuse(http.StatusNotFound, "not_found", "That task has no completion envelope.")
		}
		rows = []store.BrokerNotice{n}
	} else {
		rows, err = b.Store.CompletionNotices(ctx, time.Time{}, false, completionsPage)
		if err != nil {
			return nil, false, storeError(err)
		}
	}
	rearmed = []string{}
	for _, n := range rows {
		seen := *noticeOf(n)
		if seen.State == NoticeAcknowledged || (seen.State == NoticeDeadLetter && !includeDeadLetter) {
			continue
		}
		if len(rearmed) >= reconcileBatch {
			return rearmed, true, nil
		}
		now := b.now()
		if b.moveNotice(ctx, n.TaskID, seen, "task.completion.rearmed", func(x *Notice) {
			x.State = NoticePending
			x.Attempts = 0
			x.NextRetryAt = now
			x.LastError = nil
			x.DeadLetterAt = zeroTime
		}) {
			b.observed.forgetNotice(seen.ID)
			rearmed = append(rearmed, n.TaskID)
		}
	}
	return rearmed, false, nil
}
