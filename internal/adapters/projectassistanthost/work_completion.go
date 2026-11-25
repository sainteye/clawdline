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
	if err := h.bridge.Sync(ctx, completion); err != nil {
		return err
	}
	return h.completeLink(ctx, completion.Link, completion.Owner.ContinueOnCompletion, completion.LastAttempt)
}

// completeLink queues the review turn for a persisted link, unless the Bot's
// work review setting is off and the dispatch did not ask for one.
func (h *Host) completeLink(ctx context.Context, link pa.WorkLink, requested, lastAttempt bool) error {
	key := link.Fence.Key
	if !requested {
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
		err = runtime.WakeWorkCompletion(ctx, link, feature, source)
	}
	// A refusal ends the outbox row for good. The person reads why in the
	// chat instead of finding a result nobody took over. A full Bot is
	// retried and only told on the last attempt.
	if err != nil && orchestrator.AssistantWorkGivenUp(orchestrator.AssistantWorkSnapshot{Link: link, LastAttempt: lastAttempt}, pa.Code(err)) {
		if e := runtime.RefuseWorkCompletion(ctx, link, err); e != nil {
			return errors.Join(err, e)
		}
	}
	return err
}

// rootDelivered tells the Bot that opened a Root that the Root reported
// delivery: the result lands in its chat as the Root's report, and the
// Bot's work review setting decides whether one review turn follows, as for
// any dispatched work. The broker calls this once per Root.
func (h *Host) rootDelivered(ctx context.Context, d orchestrator.AssistantRootDelivery) error {
	link, err := h.bridge.SyncRootDelivery(ctx, d)
	if err != nil {
		return err
	}
	return h.completeLink(ctx, link, false, d.LastAttempt)
}
