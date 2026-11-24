package projectassistanthost

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
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
	runtime, err := h.runtime()
	if err != nil {
		return err
	}
	feature, source, err := h.background(ctx, key, true)
	if err == nil {
		err = runtime.WakeWorkCompletion(ctx, completion.Link, feature, source)
	}
	// A refusal ends the outbox row for good. The person reads why in the
	// chat instead of finding a result nobody took over. A full Bot is
	// retried and only told on the last attempt.
	if err != nil && orchestrator.AssistantWorkGivenUp(completion, pa.Code(err)) {
		if e := runtime.RefuseWorkCompletion(ctx, completion.Link, err); e != nil {
			return errors.Join(err, e)
		}
	}
	return err
}
