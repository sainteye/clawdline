package projectassistant

import (
	"context"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SteerNotSent is a Steer refusal that provably happened before anything was
// written to the native agent: it could not steer, or a check before the
// native call refused. The queued message keeps its turn; only an error after
// the native call may leave its outcome unknown.
type SteerNotSent struct{ Err error }

func (e *SteerNotSent) Error() string { return e.Err.Error() }
func (e *SteerNotSent) Unwrap() error { return e.Err }

// SteerRefused marks err as a Steer refusal before the native write.
func SteerRefused(err error) error {
	if err == nil {
		return nil
	}
	return &SteerNotSent{Err: err}
}

// SteeringField is the field of a Send now the agent cannot take: the page
// stops offering Send now for it.
const SteeringField = "steering"

// checkQueueScope is the fence of an action on a turn already queued. Every
// turn start mints a new generation, so an exact generation would refuse a
// phone that read the queue one turn earlier; the queued turn's own id and
// state are the guard, and the fence only has to name this chat.
func checkQueueScope(got, current pa.Fence) error {
	if got.Key != current.Key || got.Project != current.Project || got.ConversationID != current.ConversationID {
		return runtimeFailure(pa.ScopeDenied, "fence")
	}
	return nil
}

// SendNow is Codync's steer (host/src/agent/bot/steer.rs): the queued
// message goes to the agent to join the turn it is running. While the agent
// answers, the worker does not start that message. If the agent takes it, the
// queued turn ends succeeded, joined to the running turn, with no reply of its
// own; if that turn had just ended, or the agent cannot take it, the message
// stays queued and runs in its turn.
func (r *Engine) SendNow(ctx context.Context, q SendNowRequest) (pa.Receipt, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.Receipt{}, notAdmitted(err)
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
	if err = checkQueueScope(q.Fence, currentFence(bot, c)); err != nil {
		return pa.Receipt{}, err
	}
	t, err := r.o.Repository.Turn(ctx, a.key, q.TurnID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if a.turnID == t.ID || t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
		// The worker started it before the press arrived: it is running.
		return r.acceptSendNowLocked(ctx, a, q.Command, t.ID)
	}
	if t.State != pa.TurnQueued || t.Steering || t.QueueEditID != "" || a.steering[t.ID] {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "send_now_turn")
	}
	running := a.turnID
	if running == "" || c.ActiveTurnID != running {
		// The running turn just ended: the message runs in its turn, next.
		return r.acceptSendNowLocked(ctx, a, q.Command, t.ID)
	}
	steerer, ok := a.session.(SteeringSession)
	if !ok {
		return pa.Receipt{}, runtimeFailure(pa.Unsupported, SteeringField)
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
	// A refused steer leaves no receipt under the person's command, so the
	// same press can come again; each attempt's own records are its own.
	attempt := r.o.ID()
	started.IdempotencyKey += "-" + started.ContentHash + "-" + attempt
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
	steered := a.steeredTurnID
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
	var notSent *SteerNotSent
	if errors.As(steerErr, &notSent) {
		// Nothing reached the agent: the message keeps its place in the queue.
		a.steeredTurnID = steered
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.o.Limits.CleanupTimeout)
		defer cancel()
		refusal := notSent.Err
		if pa.Code(refusal) == pa.Unsupported {
			refusal = runtimeFailure(pa.Unsupported, SteeringField)
		}
		if err := r.unsteerLocked(settleCtx, a, q.Command, attempt, t.ID); err != nil {
			return pa.Receipt{}, errors.Join(refusal, err)
		}
		return pa.Receipt{}, refusal
	}
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

// acceptSendNowLocked answers a Send now whose message needs no steer, as
// the agent's promptRequired is answered: it stays queued and runs in its
// turn, or it is already running. The caller holds a.mu.
func (r *Engine) acceptSendNowLocked(ctx context.Context, a *runtimeActor, command pa.Command, turnID string) (pa.Receipt, error) {
	for {
		bot, err := r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return pa.Receipt{}, err
		}
		receipt, err := r.mutate(ctx, bot, Mutation{Command: command, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: turnID}})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return receipt, err
		}
	}
}

// unsteerLocked clears the durable steer intent of a message the agent never
// saw, so the worker starts it in its turn. No receipt is kept under the
// person's command: the press can be sent again. The caller holds a.mu.
func (r *Engine) unsteerLocked(ctx context.Context, a *runtimeActor, command pa.Command, attempt, turnID string) error {
	refused := r.command(a.key, pa.SendNowOperation, "runtime-steer-refused", command)
	refused.IdempotencyKey += "-" + refused.ContentHash + "-" + attempt
	for {
		bot, err := r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return err
		}
		t, err := r.o.Repository.Turn(ctx, a.key, turnID)
		if err != nil {
			return err
		}
		if t.State != pa.TurnQueued || !t.Steering {
			return nil
		}
		t.Steering = false
		_, err = r.mutate(ctx, bot, Mutation{Command: refused, Turn: &t, Receipt: pa.Receipt{State: pa.EffectFailed, ResourceID: t.ID},
			Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return err
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
