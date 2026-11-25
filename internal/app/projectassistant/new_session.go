package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SessionResetter starts a fresh provider session for a Bot's conversation.
// It is exported so a runtime wrapped in another package, such as the Host's
// facade, can forward it; an unexported method can only be satisfied here.
type SessionResetter interface {
	NewSession(context.Context, pa.Command, int64) (pa.Receipt, error)
}

var _ SessionResetter = (*Engine)(nil)

func (r *Engine) NewSession(ctx context.Context, command pa.Command, expected int64) (pa.Receipt, error) {
	a, err := r.acquire(command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()

	if prior, err := r.replay(ctx, command, pa.EditOperation); err != nil || prior != nil {
		if err != nil {
			return pa.Receipt{}, err
		}
		return *prior, nil
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if bot.Version != expected {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "bot")
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	chat, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	chat.SessionResetGeneration = chat.Generation
	chat.SessionResetAfter = chat.LastMessageSeq
	chat.ContextUsage = pa.ContextSample{}
	receipt, err := r.mutate(ctx, bot, Mutation{
		Command: command, Conversation: &chat, StartFreshSession: true, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: chat.ID},
	})
	if err != nil {
		return pa.Receipt{}, err
	}
	// The runtime owns this turn until its cancellation and terminal state have
	// settled. Queued turns, routines and background work have their own lives.
	if chat.ActiveTurnID != "" && chat.ActiveTurnID == a.turnID {
		a.stopped.Store(true)
		if a.cancel != nil {
			a.cancel()
		}
	}
	return receipt, nil
}
