package projectassistant

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Message sends a Bot's text to the running work Session it dispatched. The
// broker owns ownership, state, receipt and delivery; this only translates its
// answer into the assistant contract, keeping accepted apart from delivered.
func (b *BrokerWorkBridge) Message(ctx context.Context, actor pa.AssistantExecutionActor, taskID, text, key string) (pa.WorkMessageReceipt, error) {
	relayed, err := b.o.Broker.AssistantMessageWork(ctx, actor, taskID, text, key)
	if err != nil {
		return pa.WorkMessageReceipt{}, workMessageFailure(err)
	}
	out := pa.WorkMessageReceipt{TaskID: taskID, IdempotencyKey: key, Stage: pa.WorkMessageAccepted, AcceptedAt: relayed.AcceptedAt, Replayed: relayed.Replayed}
	if relayed.Stage == orchestrator.StageDelivered {
		out.Stage, out.DeliveredAt = pa.WorkMessageDelivered, relayed.At
	}
	return out, nil
}

// workMessageFailure maps the relay route's typed refusals to stable
// assistant codes. An accepted-but-unsettled message is an unknown outcome
// to be read again under the same key, never resent under a new one.
func workMessageFailure(err error) error {
	var refusal orchestrator.Refusal
	if !errors.As(err, &refusal) {
		return err
	}
	f := &pa.Failure{Code: pa.OutcomeUnknown, Field: "work_message"}
	switch refusal.Code {
	case "idempotency_key_reused":
		f.Code, f.Field = pa.IdempotencyConflict, "idempotency_key"
	case "receipt_expired":
		f.Code, f.Field = pa.Expired, "idempotency_key"
	case "request_in_progress":
		f.Field = "work_message_accepted"
		f.RetryAfterSeconds = 1
	case "terminal_busy":
		f.Code, f.Field, f.RetryAfterSeconds = pa.CapacityFull, "work_session_busy", 5
	case "target_busy":
		f.Code, f.Field = pa.CapacityFull, "work_session_menu"
	case "receipts_full":
		f.Code, f.Field = pa.CapacityFull, "work_message_receipts"
	case "target_not_found":
		f.Code, f.Field = pa.Offline, "work_session"
	}
	return f
}
