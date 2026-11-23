package projectassistanthost

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// The outbox may run after a failed projection or a daemon restart. Reconcile
// the result before queuing a turn that refers to it.
func (h *Host) completeWork(ctx context.Context, completion orchestrator.AssistantWorkSnapshot) error {
	key := completion.Owner.Fence.Key
	if err := h.bridge.Sync(ctx, completion); err != nil {
		return err
	}
	feature, source, err := h.background(ctx, key, true)
	if err != nil {
		return err
	}
	runtime, err := h.runtime()
	if err != nil {
		return err
	}
	return runtime.WakeWorkCompletion(ctx, completion.Link, feature, source)
}
