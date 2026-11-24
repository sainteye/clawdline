package projectassistant

import (
	"context"
	"errors"
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
	if t.State != pa.TurnQueued || t.Steering || t.QueueEditID != "" || a.turnID == t.ID || a.steering[t.ID] {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "send_now_turn")
	}
	running := a.turnID
	steerer, ok := a.session.(SteeringSession)
	if running == "" || c.ActiveTurnID != running || !ok {
		// Nothing is running to take it: it goes in its turn.
		return pa.Receipt{}, runtimeFailure(pa.Unsupported, "send_now_idle")
	}
	active, err := r.o.Repository.Turn(ctx, a.key, running)
	if err != nil {
		return pa.Receipt{}, err
	}
	if pa.RestartSourceTurn(active) != "" {
		return pa.Receipt{}, runtimeFailure(pa.ReadOnly, "restart_continuation")
	}
	// A native session is shared across chats; steering is still a message
	// delivery to the running transcript, so both turns must name that chat.
	if (t.Room == nil) != (active.Room == nil) || t.Room != nil && t.Room.Group != active.Room.Group {
		return pa.Receipt{}, runtimeFailure(pa.ScopeDenied, "send_now_scope")
	}
	export, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	var text string
	for _, m := range export.Messages {
		if m.ID == t.MessageID {
			if len(m.Attachments) > 0 || len(m.Files) > 0 {
				return pa.Receipt{}, runtimeFailure(pa.Unsupported, "send_now_pictures")
			}
			text = m.Text
		}
	}
	if text == "" {
		return pa.Receipt{}, runtimeFailure(pa.NotFound, "send_now_message")
	}
	t.Steering = true
	started := r.command(a.key, pa.SendNowOperation, "runtime-steer-start", q.Command)
	started.IdempotencyKey += "-" + started.ContentHash
	if _, err = r.mutate(ctx, bot, Mutation{Command: started, Turn: &t,
		Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID},
		Events:  []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}}); err != nil {
		return pa.Receipt{}, err
	}
	if a.steering == nil {
		a.steering = map[string]bool{}
	}
	a.steering[t.ID] = true
	// Any native steer may have changed the input the active Agent saw.
	a.steeredTurnID = running
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
	// Once the native call could have seen the text, a lost reply leaves
	// the durable intent behind. A later restart must not send it again.
	if steerErr != nil {
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.o.Limits.CleanupTimeout)
		defer cancel()
		latest, err := r.o.Repository.Turn(settleCtx, a.key, t.ID)
		if err == nil && latest.State == pa.TurnQueued && latest.Steering {
			err = r.finishLocked(settleCtx, a, latest, pa.TurnUnknown, "", runtimeFailure(pa.ExecutionEffectUnknown, pa.SteerUnknownField))
		}
		return pa.Receipt{}, errors.Join(steerErr, err)
	}
	for {
		bot, err = r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return pa.Receipt{}, err
		}
		t, err = r.o.Repository.Turn(ctx, a.key, q.TurnID)
		if err != nil {
			return pa.Receipt{}, err
		}
		if t.State != pa.TurnQueued || !t.Steering {
			return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "send_now_turn")
		}
		t.Steering = false
		effect := pa.EffectAccepted
		if outcome == SteerJoined {
			t.State, t.JoinedTurnID, effect = pa.TurnSucceeded, running, pa.EffectDelivered
		}
		receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Turn: &t, Receipt: pa.Receipt{State: effect, ResourceID: t.ID},
			Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return receipt, err
		}
	}
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
