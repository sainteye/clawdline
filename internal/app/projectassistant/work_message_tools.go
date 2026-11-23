package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// WorkMessageTools lets a Bot conversation message a running work Session it
// dispatched. The host binds the execution actor; the model names only the task.
type WorkMessageTools interface {
	MessageWork(ctx context.Context, taskID, text, idempotencyKey string) (pa.WorkMessageReceipt, error)
}
