package orchestrator

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/store"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const EffectAssistantWorkCompleted = "bot.work.completed"

func init() {
	effectHandlers[EffectAssistantWorkCompleted] = effectHandler{idempotent: true, run: runAssistantWorkCompleted}
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
	sink, outcome := b.AssistantWorkChanged, "bot_work_result_published"
	if r.AssistantOwner.ContinueOnCompletion {
		sink, outcome = b.AssistantWorkCompleted, "bot_continuation_queued"
	}
	if sink == nil {
		if b.now().Sub(r.FinishedAt) >= pa.CompletionWakeRetryWindow {
			return effectResult{state: store.EffectFailed, outcome: "bot_host_unavailable"}
		}
		return effectResult{deferErr: errors.New("Bot completion host is unavailable")}
	}
	err = sink(ctx, AssistantWorkSnapshotOf(r))
	if err == nil {
		return effectResult{state: store.EffectDone, outcome: outcome}
	}
	code := pa.Code(err)
	switch code {
	case pa.ScopeDenied, pa.Revoked, pa.NotFound, pa.ContextStale, pa.Invalid, pa.IdempotencyConflict:
		return effectResult{state: store.EffectFailed, outcome: string(code)}
	default:
		if b.now().Sub(r.FinishedAt) >= pa.CompletionWakeRetryWindow {
			return effectResult{state: store.EffectFailed, outcome: string(code)}
		}
		return effectResult{deferErr: err}
	}
}
