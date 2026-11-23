package projectassistanthost

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// The outbox may run after a failed projection or a daemon restart. Reconcile
// the result before queuing a turn that refers to it. A Bot whose work review
// setting is off only publishes the result unless the dispatch asked for the
// turn explicitly.
func (h *Host) completeWork(ctx context.Context, completion orchestrator.AssistantWorkSnapshot) error {
	key := completion.Owner.Fence.Key
	if err := h.bridge.Sync(ctx, completion); err != nil {
		return err
	}
	if !completion.Owner.ContinueOnCompletion {
		x, err := h.store.Export(ctx, key)
		if err != nil {
			return err
		}
		if !x.Bot.ReviewsWork() {
			return nil
		}
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
