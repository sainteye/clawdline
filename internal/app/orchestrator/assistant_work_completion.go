package orchestrator

import (
	"context"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const (
	EffectAssistantWorkCompleted = "bot.work.completed"
	// EffectAssistantWorkWaiting is written when a Bot-owned task first
	// starts waiting for a person. The host's review turn shares the
	// completion receipt, so one task never gets a second review turn.
	EffectAssistantWorkWaiting = "bot.work.waiting"
)

func init() {
	effectHandlers[EffectAssistantWorkCompleted] = effectHandler{idempotent: true, run: runAssistantWorkCompleted}
	effectHandlers[EffectAssistantWorkWaiting] = effectHandler{idempotent: true, run: runAssistantWorkWaiting}
}

func runAssistantWorkCompleted(ctx context.Context, b *Broker, e store.Effect) effectResult {
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil {
		if isNotFound(err) {
			return effectResult{state: store.EffectFailed, outcome: "worker_missing"}
		}
		return effectResult{deferErr: err}
	}
	if r.AssistantOwner == nil || !r.State.Terminal() {
		return effectResult{state: store.EffectFailed, outcome: "worker_not_settled"}
	}
	// The host decides from the Bot's work review setting and the dispatch's
	// explicit request whether to queue a turn. Without that host, only an
	// unrequested review can fall back to publishing the result.
	sink := b.AssistantWorkCompleted
	if sink == nil && !r.AssistantOwner.ContinueOnCompletion {
		sink = b.AssistantWorkChanged
	}
	return deliverAssistantWork(ctx, b, r, sink, r.FinishedAt, "bot_work_completion_delivered")
}

func runAssistantWorkWaiting(ctx context.Context, b *Broker, e store.Effect) effectResult {
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil {
		if isNotFound(err) {
			return effectResult{state: store.EffectFailed, outcome: "worker_missing"}
		}
		return effectResult{deferErr: err}
	}
	switch {
	case r.AssistantOwner == nil:
		return effectResult{state: store.EffectFailed, outcome: "worker_not_bot_owned"}
	case r.State.Terminal():
		// The terminal effect delivers the same task once.
		return effectResult{state: store.EffectDone, outcome: "worker_settled_first"}
	case r.AwaitingDialogSince.IsZero():
		return effectResult{state: store.EffectDone, outcome: "person_already_answered"}
	}
	return deliverAssistantWork(ctx, b, r, b.AssistantWorkCompleted, r.AwaitingDialogSince, "bot_work_waiting_delivered")
}

func deliverAssistantWork(ctx context.Context, b *Broker, r Record, sink func(context.Context, AssistantWorkSnapshot) error, since time.Time, outcome string) effectResult {
	if sink == nil {
		if b.now().Sub(since) >= pa.DeliveredWorkWindow {
			return effectResult{state: store.EffectFailed, outcome: "bot_host_unavailable"}
		}
		return effectResult{deferErr: errors.New("Bot completion host is unavailable")}
	}
	err := sink(ctx, AssistantWorkSnapshotOf(r))
	if err == nil {
		return effectResult{state: store.EffectDone, outcome: outcome}
	}
	code := pa.Code(err)
	switch code {
	case pa.ScopeDenied, pa.Revoked, pa.NotFound, pa.ContextStale, pa.Invalid, pa.IdempotencyConflict, pa.CapacityFull:
		return effectResult{state: store.EffectFailed, outcome: string(code)}
	default:
		if b.now().Sub(since) >= pa.DeliveredWorkWindow {
			return effectResult{state: store.EffectFailed, outcome: string(code)}
		}
		return effectResult{deferErr: err}
	}
}
