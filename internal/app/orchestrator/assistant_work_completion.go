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
	EffectAssistantWorkLanding   = "bot.work.landing"
	// EffectAssistantWorkWaiting is written when a Bot-owned task first
	// starts waiting for a person. The host's review turn shares the
	// completion receipt, so one task never gets a second review turn.
	EffectAssistantWorkWaiting = "bot.work.waiting"
)

// runMutationEffects is assigned in init: the effect table's handlers call
// mutate, so mutate cannot name the runner in a package initializer.
var runMutationEffects func(*Broker, context.Context, []int64)

func init() {
	runMutationEffects = func(b *Broker, ctx context.Context, ids []int64) { b.runRecorded(ctx, ids) }
	effectHandlers[EffectAssistantWorkCompleted] = effectHandler{idempotent: true, run: runAssistantWorkCompleted}
	effectHandlers[EffectAssistantWorkWaiting] = effectHandler{idempotent: true, run: runAssistantWorkWaiting}
	effectHandlers[EffectAssistantWorkLanding] = effectHandler{idempotent: true, run: runAssistantWorkLanding}
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

func runAssistantWorkLanding(ctx context.Context, b *Broker, e store.Effect) effectResult {
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil {
		if isNotFound(err) {
			return effectResult{state: store.EffectFailed, outcome: "worker_missing"}
		}
		return effectResult{deferErr: err}
	}
	if r.AssistantOwner == nil || r.Landing == nil || !pa.SettledLanding(string(r.Landing.State)) {
		return effectResult{state: store.EffectFailed, outcome: "worker_landing_not_settled"}
	}
	return deliverAssistantWork(ctx, b, r, b.AssistantWorkChanged, r.Landing.At, "bot_work_landing_delivered")
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
		if b.now().Sub(since) >= pa.CompletionWakeRetryWindow {
			return effectResult{state: store.EffectFailed, outcome: "bot_host_unavailable"}
		}
		return effectResult{deferErr: errors.New("Bot completion host is unavailable")}
	}
	// The window is read once, so the host and the outbox agree on whether
	// this attempt is the last one.
	snapshot := AssistantWorkSnapshotOf(r)
	snapshot.LastAttempt = b.now().Sub(since) >= pa.CompletionWakeRetryWindow
	err := sink(HostContext(ctx), snapshot)
	if err == nil {
		return effectResult{state: store.EffectDone, outcome: outcome}
	}
	code := pa.Code(err)
	if AssistantWorkRefused(code) || snapshot.LastAttempt {
		return effectResult{state: store.EffectFailed, outcome: string(code)}
	}
	return effectResult{deferErr: err}
}

// AssistantWorkRefused is whether a host refusal of a Bot work delivery is
// final: the outbox stops retrying it, so the host tells the person in the
// chat instead of failing silently. A full Bot (pa.CapacityFull, such as
// active_bots or owned_cleanup) is transient and retries until the window
// ends.
func AssistantWorkRefused(code pa.ErrorCode) bool {
	switch code {
	case pa.ScopeDenied, pa.Revoked, pa.NotFound, pa.ContextStale, pa.Invalid, pa.IdempotencyConflict:
		return true
	}
	return false
}

// AssistantWorkGivenUp is whether the host should tell the person that this
// delivery ends without a review turn: a final refusal, or a full Bot on the
// last attempt of the retry window.
func AssistantWorkGivenUp(s AssistantWorkSnapshot, code pa.ErrorCode) bool {
	return AssistantWorkRefused(code) || (code == pa.CapacityFull && s.LastAttempt)
}

// HostContext keeps the cancellation of the request that recorded the
// effect and drops its values. A child's result arrives on a task-secret
// route whose gate verdict admits no person; carried into the host, that
// verdict was revalidated as a person's and refused as pa_revoked, so every
// review turn queued while a child reported failed. The review turn is the
// host's notice, never the authority of whoever settled the task.
func HostContext(ctx context.Context) context.Context { return hostContext{ctx} }

type hostContext struct{ context.Context }

func (hostContext) Value(any) any { return nil }
