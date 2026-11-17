package projectassistant

import (
	"context"
	"errors"
	"slices"
	"strconv"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type AgentRestRepository interface {
	AgentRest(context.Context, string, pa.Agent) (*pa.AgentRest, error)
}

func (r *Engine) checkAttemptBoundary(ctx context.Context, a *runtimeActor, bot pa.Bot, c pa.Conversation, t pa.Turn, checkMemory bool) error {
	a.mu.Lock()
	stopped := a.stopped
	a.mu.Unlock()
	if stopped || ctx.Err() != nil {
		return runtimeFailure(pa.ExecutionEffectUnknown, "stopped_before_attempt")
	}
	if err := r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	_, current, err := r.currentForStart(ctx, a.key, bot, c)
	if err != nil {
		return err
	}
	if current.BindingID != t.Fence.BindingID || current.Generation != t.Fence.Generation {
		return runtimeFailure(pa.StaleGeneration, "attempt_fence")
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, current.BindingID)
	if err != nil {
		return err
	}
	if binding.Revoked {
		return runtimeFailure(pa.Revoked, "attempt_binding")
	}
	resolved, err := r.o.ProjectResolver.Resolve(ctx, pa.ProjectSelection{MachineID: bot.Key.MachineID, ProjectID: bot.Project.ProjectID})
	if err != nil {
		return err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: bot.Key.MachineID, ProjectID: bot.Project.ProjectID}, resolved); err != nil {
		return err
	}
	if resolved.Scope != bot.Project {
		return runtimeFailure(pa.ScopeDenied, "attempt_project")
	}
	if !checkMemory {
		return nil
	}
	// WithCurrent owns the memory lock at Send; only Open reads it here.
	memory, err := r.o.Memory.Current(ctx, a.key)
	if err != nil {
		return err
	}
	if memory.Revision != t.MemoryRevision {
		return runtimeFailure(pa.ContextStale, "attempt_memory")
	}
	return nil
}

func (r *Engine) selectAgent(ctx context.Context, bot pa.Bot, t pa.Turn) (pa.Agent, error) {
	// This slice selects on visible human turns only. Routines retain the
	// primary route and its separate main-session qualification; a shared rest
	// must not silently substitute a different routine provider.
	if t.RoutineRunID != "" || len(t.Attempts) == 0 && len(bot.FallbackAgents.Agents()) == 0 {
		return bot.Agent, nil
	}
	rests, ok := r.o.Repository.(AgentRestRepository)
	if !ok && len(bot.FallbackAgents.Agents()) > 0 {
		return "", runtimeFailure(pa.Unsupported, "agent_rests")
	}
	for _, agent := range pa.AgentChain(bot) {
		tried := false
		for _, f := range t.Attempts {
			if f.Agent == agent {
				tried = true
				break
			}
		}
		if tried {
			continue
		}
		if ok {
			profile := ""
			if agent == bot.Agent {
				profile = bot.LoginProfile
			}
			rest, err := loginRest(ctx, rests, bot.Key.MachineID, agent, profile)
			if err != nil {
				return "", err
			}
			if rest != nil && r.o.Clock().Before(rest.Until) {
				continue
			}
		}
		// Stand-in mode mapping is not qualified yet. Auto preserves native policy;
		// a fixed mode must never become a more permissive alternate implicitly.
		if agent != bot.Agent && bot.Mode != "" && bot.Mode != pa.AutoMode {
			if len(t.Attempts) == 0 {
				return bot.Agent, nil
			}
			return "", runtimeFailure(pa.Unsupported, "fallback_mode")
		}
		return agent, nil
	}
	// Codync starts new input on primary when every login is resting. Rest
	// selects an alternate; it is never a machine-wide admission prohibition.
	// An existing turn still cannot retry an agent it already attempted.
	if len(t.Attempts) == 0 {
		return bot.Agent, nil
	}
	return "", runtimeFailure(pa.Unsupported, "agents_resting_or_attempted")
}

func (r *Engine) appendAttempt(ctx context.Context, a *runtimeActor, t *pa.Turn, stage pa.AttemptStage, binding *pa.ExecutionBinding, rest *pa.AgentRest) error {
	if len(t.Attempts) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Native events may have appended facts while Open/Send/Wait ran. Reload
	// rather than overwriting that evidence with the worker's older snapshot.
	latest, err := r.o.Repository.Turn(ctx, a.key, t.ID)
	if err != nil {
		return err
	}
	if latest.Fence != t.Fence || len(latest.Attempts) == 0 {
		return runtimeFailure(pa.StaleGeneration, "attempt_fence")
	}
	*t = latest
	f := t.Attempts[len(t.Attempts)-1]
	f.Stage = stage
	switch stage {
	case pa.AttemptOpened:
		f.Opened = true
	case pa.AttemptSending:
		f.SendStarted = true
		f.Effect = "unknown"
	case pa.AttemptSent:
		f.SendReturned = true
	case pa.AttemptAnswered:
		f.AnswerObserved = true
	case pa.AttemptClosed:
		f.CleanupConfirmed = true
	}
	t.Attempts = append(slices.Clone(t.Attempts), f)
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return err
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-attempt-"+t.ID+"-"+strconv.Itoa(len(t.Attempts)), f)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Turn: t, Binding: binding, AgentRest: rest, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: t}}})
	return err
}

// Only this explicit provider guarantee can enter the no-send transition. It
// is checked before Send is called, never in a Send/Wait/permission error path.
func (r *Engine) refuseBeforeSend(ctx context.Context, a *runtimeActor, t *pa.Turn, binding pa.ExecutionBinding, err error) (bool, error) {
	if len(t.Attempts) == 0 {
		return false, nil
	}
	var refusal *PreSendRefusal
	if !errors.As(err, &refusal) || refusal.Failure == nil || !knownFallbackReason(refusal.Failure.Reason) || t.RoutineRunID != "" {
		return false, nil
	}
	if pa.Code(err) == pa.ExecutionEffectUnknown || pa.Code(err) == pa.OutcomeUnknown {
		return false, nil
	}
	x, e := r.o.Repository.Export(ctx, a.key)
	if e != nil {
		return false, e
	}
	// Contradictory durable effects override the explicit contract. Absence alone
	// is never the proof: PreSendRefusal is mandatory above.
	for _, p := range x.Permissions {
		if p.TurnID == t.ID {
			return false, nil
		}
	}
	for _, w := range x.WorkLinks {
		if w.TurnID == t.ID {
			return false, nil
		}
	}
	for _, m := range x.Messages {
		if m.TurnID == t.ID && m.ID != t.MessageID {
			return false, nil
		}
	}
	for _, event := range x.Events {
		if event.Fence == t.Fence && event.Trace != nil {
			return false, nil
		}
	}
	f := t.Attempts[len(t.Attempts)-1]
	if f.Stage != pa.AttemptOpening || f.SendStarted || f.NativeActivity || f.ToolActivity || f.SendReturned || f.AnswerObserved {
		return false, nil
	}
	f.Reason = string(refusal.Failure.Reason)
	f.Effect = "none"
	// Persist the refusal and revoke the old execution before Close. A crash
	// here remains unknown; recovery never advances the ledger automatically.
	f.Stage = pa.AttemptRefused
	t.Attempts = append(slices.Clone(t.Attempts), f)
	binding.Revoked = true
	latest, e := r.o.Repository.Bot(ctx, a.key)
	if e != nil {
		return false, e
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-refused-"+t.ID+"-"+strconv.Itoa(f.Ordinal), f)
	_, e = r.mutate(ctx, latest, Mutation{Command: cmd, Turn: t, Binding: &binding, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: t}}})
	return e == nil, e
}

func (r *Engine) attemptRest(ctx context.Context, bot pa.Bot, t pa.Turn) (*pa.AgentRest, error) {
	store, ok := r.o.Repository.(AgentRestRepository)
	if !ok {
		return nil, runtimeFailure(pa.Unsupported, "agent_rests")
	}
	f := t.Attempts[len(t.Attempts)-1]
	before, err := loginRest(ctx, store, bot.Key.MachineID, f.Agent, f.LoginProfile)
	if err != nil {
		return nil, err
	}
	rest := pa.RestAfter(bot.Key.MachineID, f.Agent, f.Reason, r.o.Clock(), before)
	if rest != nil {
		rest.LoginProfile = f.LoginProfile
	}
	return rest, nil
}

// holdAttempt records cleanup knowledge without granting replay authority. It
// never promotes a diagnostic provider category to no-effect evidence.
func (r *Engine) holdAttempt(a *runtimeActor, turn pa.Turn, cause error, cleanup bool) error {
	if len(turn.Attempts) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	defer cancel()
	latest, err := r.o.Repository.Turn(ctx, a.key, turn.ID)
	if err != nil {
		return err
	}
	if latest.Fence != turn.Fence || len(latest.Attempts) == 0 {
		return nil
	}
	f := latest.Attempts[len(latest.Attempts)-1]
	if f.Stage == pa.AttemptClosed || f.Stage == pa.AttemptHeld {
		return nil
	}
	f.Stage = pa.AttemptHeld
	f.Effect = "unknown"
	f.CleanupConfirmed = cleanup
	var failure *ProviderFailure
	if errors.As(cause, &failure) {
		f.Reason = string(failure.Reason)
	}
	latest.Attempts = append(slices.Clone(latest.Attempts), f)
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return err
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-held-"+turn.ID+"-"+f.Fence.BindingID, f)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Turn: &latest, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: turn.ID}, Events: []pa.Event{{Fence: latest.Fence, Kind: pa.TurnUpdated, Turn: &latest}}})
	return err
}

func knownFallbackReason(reason ProviderFailureReason) bool {
	switch reason {
	case ProviderUsageExhausted, ProviderOutOfCredits, ProviderRateLimited, ProviderOverloaded, ProviderSignedOut, ProviderModelMissing:
		return true
	}
	return false
}

func attemptSwitchNotice(x Export, t pa.Turn, primary pa.Agent) string {
	if len(t.Attempts) == 0 {
		return ""
	}
	first := t.Attempts[0]
	effective := t.Attempts[len(t.Attempts)-1].Agent
	before := primary
	var generation int64
	for _, b := range x.Bindings {
		if b.SessionSlot == "" && b.Fence.Generation < first.Fence.Generation && b.Fence.Generation > generation {
			before = b.Agent
			generation = b.Fence.Generation
		}
	}
	if before == effective {
		return ""
	}
	return "Agent switched before sending / 送出前已切換 Agent：" + string(before) + " → " + string(effective)
}

func loginRest(ctx context.Context, store AgentRestRepository, machine string, agent pa.Agent, profile string) (*pa.AgentRest, error) {
	if profile == "" {
		return store.AgentRest(ctx, machine, agent)
	}
	if p, ok := store.(interface {
		ProfileRest(context.Context, string, pa.Agent, string) (*pa.AgentRest, error)
	}); ok {
		return p.ProfileRest(ctx, machine, agent, profile)
	}
	return nil, runtimeFailure(pa.Unsupported, "profile_rests")
}
