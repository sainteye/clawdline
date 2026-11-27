package projectassistant

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SteerNotSent proves the agent did not consume the input. A refusal can
// happen before the write or come from an adapter that rejected the request.
type SteerNotSent struct{ Err error }

func (e *SteerNotSent) Error() string { return e.Err.Error() }
func (e *SteerNotSent) Unwrap() error { return e.Err }

// SteerRefused marks err as proof the agent did not consume the input.
func SteerRefused(err error) error {
	if err == nil {
		return nil
	}
	return &SteerNotSent{Err: err}
}

// SteeringField identifies a Send now refusal specific to the agent.
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
	defer a.mu.Unlock()
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
	return r.steerQueuedLocked(ctx, a, bot, c, q.Command, t, nil, nil)
}

// trySteerFollowUpLocked runs only after admitting a person's message. Its
// original receipt remains replayable; the turn records the steering result.
// A Bot steers unless the person chose to queue. Clawdline's own notices,
// other Bots' requests, rooms, compaction, handoffs, decisions and restart
// continuations always queue.
func (r *Engine) trySteerFollowUpLocked(ctx context.Context, a *runtimeActor, bot pa.Bot, c pa.Conversation, t pa.Turn, m pa.Message, s Submission) {
	if bot.ReplyInput.Effective() != pa.ReplyInputSteer || c.ActiveTurnID == "" || a.session == nil ||
		!personMessage(s) || s.Message.Handoff != nil || s.Decision != nil || s.RestartSource != "" {
		return
	}
	// Admission is durable. Losing the caller no longer decides whether the
	// native write is confirmed, but live authority still gates that write.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(capacity.MaximumBotAutoSteerMilliseconds)*time.Millisecond)
	defer cancel()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		logAutoSteerFailure(t.ID, err)
		return
	}
	if bot.ReplyInput.Effective() != pa.ReplyInputSteer {
		return
	}
	command := r.command(a.key, pa.SendNowOperation, "runtime-auto-steer-"+t.ID, s.Command)
	// Admission already succeeded. A pre-write refusal leaves the full message
	// queued with the reason. An ambiguous write is recorded on the turn and
	// never replayed.
	if _, err := r.steerQueuedLocked(ctx, a, bot, c, command, t, &m, s.AuthorityCheck); err != nil {
		logAutoSteerFailure(t.ID, err)
	}
}

func logAutoSteerFailure(turnID string, err error) {
	// Native and repository errors may contain message content.
	log.Printf("project assistant: automatic steer failed; turn=%s code=%s error_type=%T", turnID, pa.Code(err), err)
}

func (r *Engine) steerQueuedLocked(ctx context.Context, a *runtimeActor, bot pa.Bot, c pa.Conversation, command pa.Command, t pa.Turn, m *pa.Message, check func(context.Context) error) (pa.Receipt, error) {
	automatic := m != nil
	// staysQueued leaves the message for its own turn. An automatic
	// follow-up already has its send receipt and records why; a press is
	// answered as the agent's promptRequired is.
	staysQueued := func(reason pa.SteerFallback) (pa.Receipt, error) {
		if automatic {
			return pa.Receipt{}, r.recordSteerFallbackLocked(ctx, a, command, t.ID, reason)
		}
		return r.acceptSendNowLocked(ctx, a, command, t.ID)
	}
	// refused is a press's typed refusal; the automatic follow-up has no one
	// to answer, so it records the reason and stays queued.
	refused := func(reason pa.SteerFallback, err error) (pa.Receipt, error) {
		if automatic {
			return pa.Receipt{}, r.recordSteerFallbackLocked(ctx, a, command, t.ID, reason)
		}
		return pa.Receipt{}, err
	}
	running := a.turnID
	switch {
	case a.pendingPermissions != 0:
		return staysQueued(pa.SteerFallbackPermissionPending)
	case running == "" || c.ActiveTurnID != running || a.closing || a.stopped.Load():
		return staysQueued(pa.SteerFallbackTurnEnded)
	case len(a.steering) != 0:
		return staysQueued(pa.SteerFallbackBusy)
	}
	active, err := r.o.Repository.Turn(ctx, a.key, running)
	if err != nil {
		return pa.Receipt{}, err
	}
	if pa.RestartSourceTurn(active) != "" {
		return refused(pa.SteerFallbackScope, runtimeFailure(pa.ReadOnly, "restart_continuation"))
	}
	switch {
	case active.State == pa.TurnWaitingPermission:
		return staysQueued(pa.SteerFallbackPermissionPending)
	case active.State != pa.TurnRunning:
		return staysQueued(pa.SteerFallbackTurnEnded)
	case active.RoutineRunID != "" || active.Origin == pa.CompactOrigin:
		return staysQueued(pa.SteerFallbackScope)
	}
	// The agent belongs to the running chat. Do not inject input across the
	// boundaries the regular prompt and transcript paths enforce.
	if (t.Room == nil) != (active.Room == nil) || t.Room != nil && t.Room.Group != active.Room.Group ||
		active.Network != nil && t.Network == nil {
		return refused(pa.SteerFallbackScope, runtimeFailure(pa.ScopeDenied, "send_now_scope"))
	}
	steerer, ok := a.session.(SteeringSession)
	if !ok || !steerer.SupportsSteering(false) {
		return refused(pa.SteerFallbackAgentUnsupported, runtimeFailure(pa.Unsupported, SteeringField))
	}
	input, err := r.steeringInput(ctx, t, active, m)
	if err != nil {
		return pa.Receipt{}, err
	}
	if len(input.Images) > 0 && !steerer.SupportsSteering(true) {
		return staysQueued(pa.SteerFallbackImagesUnsupported)
	}
	if check != nil {
		if err := check(ctx); err != nil {
			return pa.Receipt{}, err
		}
	}
	t.Steering = true
	started := r.command(a.key, pa.SendNowOperation, "runtime-steer-start", command)
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
	outcome, steerErr := steerWhileUnlocked(ctx, a, steerer, input)
	if steerErr == nil && outcome != SteerNext && outcome != SteerJoined {
		steerErr = runtimeFailure(pa.OutcomeUnknown, "steering_answer")
	}
	delete(a.steering, t.ID)
	defer r.kickLocked(a)
	// The client may disconnect after the native reply. Finish settling the
	// durable intent on the existing cleanup budget.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.o.Limits.CleanupTimeout)
	defer cancel()
	// Once the native call could have seen the input, a lost reply leaves
	// the durable intent behind. A later restart must not send it again.
	var notSent *SteerNotSent
	if errors.As(steerErr, &notSent) {
		// Nothing reached the agent: the message keeps its place in the queue.
		a.steeredTurnID = steered
		refusal, reason := notSent.Err, pa.SteerFallbackNotSent
		if pa.Code(refusal) == pa.Unsupported {
			refusal, reason = runtimeFailure(pa.Unsupported, SteeringField), pa.SteerFallbackAgentUnsupported
			if len(input.Images) > 0 {
				reason = pa.SteerFallbackImagesUnsupported
			}
		}
		if !automatic {
			reason = ""
		}
		if err := r.unsteerLocked(settleCtx, a, command, attempt, t.ID, reason); err != nil {
			return pa.Receipt{}, errors.Join(refusal, err)
		}
		if automatic {
			return pa.Receipt{}, nil
		}
		return pa.Receipt{}, refusal
	}
	if steerErr != nil {
		latest, err := r.o.Repository.Turn(settleCtx, a.key, t.ID)
		if err == nil && latest.State == pa.TurnQueued && latest.Steering {
			err = r.finishLocked(settleCtx, a, latest, pa.TurnUnknown, "", runtimeFailure(pa.ExecutionEffectUnknown, pa.SteerUnknownField))
		}
		return pa.Receipt{}, errors.Join(steerErr, err)
	}
	var reason pa.SteerFallback
	if outcome == SteerNext {
		a.steeredTurnID = steered
		if automatic {
			reason = pa.SteerFallbackTurnEnded
		}
	}
	return r.settleSteeringLocked(settleCtx, a, command, t.ID, running, outcome, reason)
}

// recordSteerFallbackLocked marks a message that never reached the agent
// with why it waits for its own turn. It writes nothing once the message
// left the queue. The caller holds a.mu.
func (r *Engine) recordSteerFallbackLocked(ctx context.Context, a *runtimeActor, command pa.Command, turnID string, reason pa.SteerFallback) error {
	fallback := r.command(a.key, pa.SendNowOperation, "runtime-steer-fallback", command)
	fallback.IdempotencyKey += "-" + fallback.ContentHash
	for {
		bot, err := r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return err
		}
		t, err := r.o.Repository.Turn(ctx, a.key, turnID)
		if err != nil {
			return err
		}
		if t.State != pa.TurnQueued || t.Steering || t.SteerFallback == reason {
			return nil
		}
		t.SteerFallback = reason
		_, err = r.mutate(ctx, bot, Mutation{Command: fallback, Turn: &t, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: t.ID},
			Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return err
		}
	}
}

func (r *Engine) settleSteeringLocked(ctx context.Context, a *runtimeActor, command pa.Command, turnID, running string, outcome SteerOutcome, reason pa.SteerFallback) (pa.Receipt, error) {
	for {
		bot, err := r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return pa.Receipt{}, err
		}
		t, err := r.o.Repository.Turn(ctx, a.key, turnID)
		if err != nil {
			return pa.Receipt{}, err
		}
		if t.State != pa.TurnQueued || !t.Steering {
			return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "send_now_turn")
		}
		t.Steering = false
		effect := pa.EffectAccepted
		if reason != "" {
			t.SteerFallback = reason
		}
		if outcome == SteerJoined {
			t.State, t.JoinedTurnID, effect = pa.TurnSucceeded, running, pa.EffectDelivered
			t.SteerFallback = ""
		}
		receipt, err := r.mutate(ctx, bot, Mutation{Command: command, Turn: &t, Receipt: pa.Receipt{State: effect, ResourceID: t.ID},
			Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return receipt, err
		}
	}
}

// Turn and tool events also take a.mu. Wait for the receipt without
// that lock, while the durable steering fact keeps the worker off this input.
func steerWhileUnlocked(ctx context.Context, a *runtimeActor, session SteeringSession, input PromptRequest) (SteerOutcome, error) {
	a.mu.Unlock()
	defer a.mu.Lock()
	answerCtx, cancel := context.WithTimeout(ctx, time.Duration(capacity.MaximumBotSteerAnswerMilliseconds)*time.Millisecond)
	defer cancel()
	return session.Steer(answerCtx, input)
}

func (r *Engine) steeringInput(ctx context.Context, queued, active pa.Turn, message *pa.Message) (PromptRequest, error) {
	var export Export
	if message == nil || message.QuoteMessageID != "" {
		var err error
		export, err = r.o.Repository.Export(ctx, queued.Fence.Key)
		if err != nil {
			return PromptRequest{}, err
		}
	}
	if message == nil {
		for _, m := range export.Messages {
			if m.ID == queued.MessageID {
				message = &m
				break
			}
		}
	}
	if message == nil {
		return PromptRequest{}, runtimeFailure(pa.NotFound, "send_now_message")
	}
	images, err := r.images(queued.Fence.Key, message.Attachments)
	if err != nil {
		return PromptRequest{}, err
	}
	files, err := r.promptFiles(ctx, message)
	if err != nil {
		return PromptRequest{}, err
	}
	input := PromptRequest{Turn: active, Text: message.Text, Images: images, Files: files}
	if message.QuoteMessageID != "" {
		memory, err := r.o.Memory.Current(ctx, queued.Fence.Key)
		if err != nil {
			return PromptRequest{}, err
		}
		input.Quote, err = resolvePromptQuote(export, message.QuoteMessageID, active.Fence, memory)
		if err != nil {
			return PromptRequest{}, err
		}
	}
	return input, nil
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
func (r *Engine) unsteerLocked(ctx context.Context, a *runtimeActor, command pa.Command, attempt, turnID string, reason pa.SteerFallback) error {
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
		if reason != "" {
			t.SteerFallback = reason
		}
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
