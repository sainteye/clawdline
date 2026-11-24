package projectassistant

import (
	"context"
	"errors"
	"strings"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// WorkReceiptState answers whether a dispatch_work key reached the broker.
type WorkReceiptState string

const (
	// WorkReceiptAccepted means the broker holds the task; TaskID and Link
	// are the same facts work_status returns.
	WorkReceiptAccepted WorkReceiptState = "accepted"
	// WorkReceiptNotAccepted means the bridge never prepared a broker call
	// for this key, so resending the same key and content creates it once.
	WorkReceiptNotAccepted WorkReceiptState = "not_accepted"
	// WorkReceiptUndetermined means the facts cannot settle it yet; Reason
	// says which, and a resend is not known to be safe.
	WorkReceiptUndetermined WorkReceiptState = "undetermined"
)

// WorkReceipt is the answer to a lookup by the dispatcher's own key.
type WorkReceipt struct {
	State          WorkReceiptState `json:"state"`
	IdempotencyKey string           `json:"idempotency_key"`
	TaskID         string           `json:"task_id,omitempty"`
	Link           *pa.WorkLink     `json:"work,omitempty"`
	SafeToResend   bool             `json:"safe_to_resend_same_key"`
	Reason         string           `json:"reason,omitempty"`
}

// WorkReceiptTools is the optional native capability behind
// work_status(idempotency_key).
type WorkReceiptTools interface {
	WorkReceipt(context.Context, string) (WorkReceipt, error)
}

// Receipt finds what happened to one dispatch_work key of the authenticated
// actor. The Bot identity comes from the actor's fence, never from the model:
// the derived task ID and every receipt row live in that Bot's own
// namespace, so another Bot's key cannot address another Bot's work. A key
// this Bot never used, and a task owned by another conversation or Project,
// both answer the same typed pa_not_found without revealing which it was.
func (b *BrokerWorkBridge) Receipt(ctx context.Context, actor pa.AssistantExecutionActor, idempotencyKey string) (WorkReceipt, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return WorkReceipt{}, bridgeFailure(pa.Invalid, "idempotency_key")
	}
	key := actor.Fence.Key
	command := pa.Command{Key: key, Operation: pa.DispatchOperation, IdempotencyKey: idempotencyKey}
	out := WorkReceipt{IdempotencyKey: idempotencyKey}
	// Holding the dispatch lock proves no dispatch of this process is between
	// its preparation receipt and the broker's admission while we read.
	if !b.dispatchMu.TryLock() {
		out.State, out.Reason = WorkReceiptUndetermined, "dispatch_in_progress"
		return out, nil
	}
	defer b.dispatchMu.Unlock()
	id := orchestrator.AssistantTaskID(command)
	snapshot, err := b.o.Broker.AssistantStatus(ctx, actor, id)
	switch {
	case err == nil:
		if snapshot.Owner.Command.Key != key || snapshot.Owner.Command.IdempotencyKey != idempotencyKey {
			return WorkReceipt{}, bridgeFailure(pa.NotFound, "work_receipt")
		}
		link := snapshot.Link
		if syncErr := b.Sync(ctx, snapshot); syncErr == nil {
			if current, statusErr := b.o.Broker.AssistantStatus(ctx, actor, id); statusErr == nil {
				link = current.Link
			}
		}
		out.State, out.TaskID, out.Link = WorkReceiptAccepted, link.TaskID, &link
		return out, nil
	case pa.Code(err) == pa.ScopeDenied:
		var failure *pa.Failure
		if errors.As(err, &failure) && failure.Field == "work_owner" {
			return WorkReceipt{}, bridgeFailure(pa.NotFound, "work_receipt")
		}
		return WorkReceipt{}, err
	case !bridgeTaskMissing(err):
		if pa.Code(err) != pa.OutcomeUnknown {
			return WorkReceipt{}, err
		}
		out.State, out.Reason = WorkReceiptUndetermined, "broker_unreadable"
		return out, nil
	}
	used, err := b.receiptExists(ctx, command)
	if err != nil {
		out.State, out.Reason = WorkReceiptUndetermined, "receipt_unreadable"
		return out, nil
	}
	prepared, err := b.receiptExists(ctx, pa.Command{Key: key, Operation: pa.DispatchOperation, IdempotencyKey: "bridge-prepare-" + id})
	if err != nil {
		out.State, out.Reason = WorkReceiptUndetermined, "receipt_unreadable"
		return out, nil
	}
	switch {
	case prepared:
		// The broker was asked and holds no task: it refused, or the call
		// was cut off. Bridge.Dispatch never replays this, so neither do we.
		out.State, out.Reason = WorkReceiptUndetermined, "broker_admission_unknown"
	case used:
		out.State, out.SafeToResend = WorkReceiptNotAccepted, true
	default:
		return WorkReceipt{}, bridgeFailure(pa.NotFound, "work_receipt")
	}
	return out, nil
}

// receiptExists reads a receipt row by its key alone; the lookup does not
// know the original content, so a content conflict still proves the row.
func (b *BrokerWorkBridge) receiptExists(ctx context.Context, c pa.Command) (bool, error) {
	c.ContentHash = strings.Repeat("0", 64)
	x, err := b.o.Repository.Receipt(ctx, c)
	if x != nil || pa.Code(err) == pa.IdempotencyConflict {
		return true, nil
	}
	return false, err
}

// A dispatch intent is immutable, including when it reaches the broker.
// Only a task link for that specific command settles admission for recovery.
func restartDispatchUnconfirmed(x Export, turnID string) bool {
	linked := map[string]bool{}
	for _, link := range x.WorkLinks {
		if link.TurnID == turnID {
			linked[link.TaskID] = true
		}
	}
	for _, rec := range x.Receipts {
		if rec.Command.Operation == pa.DispatchOperation && rec.ResourceID == turnID &&
			rec.State == pa.EffectUnknown && !linked[orchestrator.AssistantTaskID(rec.Command)] {
			return true
		}
	}
	return false
}
