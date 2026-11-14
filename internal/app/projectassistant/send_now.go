package projectassistant

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SendNow is Codync's steer (host/src/agent/bot/steer.rs): the queued
// message goes to the agent to join the turn it is running. While the agent
// answers, the worker does not start that message. If the agent takes it, the
// queued turn ends succeeded, joined to the running turn, with no reply of its
// own; if that turn had just ended, or the agent cannot take it, the message
// stays queued and runs in its turn.
func (r *Engine) SendNow(ctx context.Context, q SendNowRequest) (pa.Receipt, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	if receipt, e := r.replay(ctx, q.Command, pa.SendNowOperation); e != nil {
		return pa.Receipt{}, e
	} else if receipt != nil {
		return *receipt, nil
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckFence(q.Fence, currentFence(bot, c)); err != nil {
		return pa.Receipt{}, err
	}
	t, err := r.o.Repository.Turn(ctx, a.key, q.TurnID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if t.State != pa.TurnQueued || a.turnID == t.ID || a.steering[t.ID] {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "send_now_turn")
	}
	running := a.turnID
	steerer, ok := a.session.(SteeringSession)
	if running == "" || c.ActiveTurnID != running || !ok {
		// Nothing is running to take it: it goes in its turn.
		return pa.Receipt{}, runtimeFailure(pa.Unsupported, "send_now_idle")
	}
	export, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	var text string
	for _, m := range export.Messages {
		if m.ID == t.MessageID {
			if len(m.Attachments) > 0 {
				return pa.Receipt{}, runtimeFailure(pa.Unsupported, "send_now_pictures")
			}
			text = m.Text
		}
	}
	if text == "" {
		return pa.Receipt{}, runtimeFailure(pa.NotFound, "send_now_message")
	}
	if a.steering == nil {
		a.steering = map[string]bool{}
	}
	a.steering[t.ID] = true
	a.mu.Unlock()
	locked = false
	// The agent's answer travels apart from its turn's events, which need
	// actor.mu, so it is awaited unlocked and bounded as Codync's STEER_ANSWER.
	answerCtx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotSteerAnswerMilliseconds)*time.Millisecond)
	outcome, steerErr := steerer.Steer(answerCtx, text)
	cancel()
	a.mu.Lock()
	locked = true
	delete(a.steering, t.ID)
	defer r.kickLocked(a)
	if steerErr != nil {
		return pa.Receipt{}, steerErr
	}
	if bot, err = r.o.Repository.Bot(ctx, a.key); err != nil {
		return pa.Receipt{}, err
	}
	if outcome != SteerJoined {
		return r.mutate(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: t.ID}})
	}
	if t, err = r.o.Repository.Turn(ctx, a.key, q.TurnID); err != nil {
		return pa.Receipt{}, err
	}
	if t.State != pa.TurnQueued {
		// A Stop meanwhile took it; the agent may still have read it.
		return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "send_now_turn")
	}
	t.State, t.JoinedTurnID = pa.TurnSucceeded, running
	return r.mutate(ctx, bot, Mutation{Command: q.Command, Turn: &t, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
}

// kickLocked starts the worker when it stopped, for instance behind a message
// that was being steered. The caller holds a.mu.
func (r *Engine) kickLocked(a *runtimeActor) {
	if a.working {
		return
	}
	a.working = true
	r.mu.Lock()
	a.users++
	r.mu.Unlock()
	go r.work(a)
}
