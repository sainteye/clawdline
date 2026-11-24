package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NativeContextFacts are the durable parts of one Bot read together. Native
// context checks this snapshot again after sampling the still-owned session.
type NativeContextFacts struct {
	Bot          pa.Bot
	Conversation pa.Conversation
	Turn         *pa.Turn
	Binding      *pa.ExecutionBinding
	Memory       pa.MemoryRevision
}

type NativeContextRead func(context.Context, pa.BotKey, string) (NativeContextFacts, error)

// NativeContext borrows only an existing execution owner. It never opens a
// provider, acquires a process slot, creates an actor, or retains a last reading.
// A known reading describes this live turn, not eligibility for compaction.
func (r *Engine) NativeContext(ctx context.Context, fence pa.Fence, readCheck func() error, readFacts NativeContextRead) (NativeContextState, error) {
	unknown := NativeContextState{Fence: fence}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	r.mu.Lock()
	a := r.actors[fence.Key]
	if r.closed || a == nil || a.retained {
		r.mu.Unlock()
		return unknown, nil
	}
	a.users++
	r.mu.Unlock()
	defer r.release(a)
	// A read cannot wait behind a native write or delay another Bot's actor.
	if !a.mu.TryLock() {
		return unknown, nil
	}
	defer a.mu.Unlock()
	if err := readCheck(); err != nil {
		return unknown, err
	}
	before, live, err := r.nativeContextBinding(ctx, a, fence, readFacts)
	if err != nil || !live {
		return unknown, err
	}
	reader, ok := a.session.(NativeContextReader)
	if !ok {
		return unknown, nil
	}
	state := reader.NativeContext()
	// A provider without this optional report remains unknown. It may also
	// seal its lifetime while this read is in progress.
	if state.NativeSessionID == "" {
		return unknown, nil
	}
	if state.Fence != fence || state.NativeSessionID != before.NativeSessionID || a.session.SessionID() != before.NativeSessionID {
		return unknown, runtimeFailure(pa.StaleGeneration, "native_context_identity")
	}
	if err := readCheck(); err != nil {
		return unknown, err
	}
	after, live, err := r.nativeContextBinding(ctx, a, fence, readFacts)
	if err != nil || !live {
		return unknown, err
	}
	if after != before {
		return unknown, runtimeFailure(pa.StaleGeneration, "native_context_binding")
	}
	state.TurnID = a.turnID
	state.CompactAdvertised = state.CommandsKnown && state.CompactAdvertised
	if !state.UsageKnown || state.UsedTokens < 0 || state.WindowTokens <= 0 {
		state.UsageKnown = false
		state.UsedTokens, state.WindowTokens = 0, 0
	}
	return state, nil
}

// The caller holds actor.mu. Recheck persisted authority on both sides of the
// optional native read; an old process handle is not current binding authority.
func (r *Engine) nativeContextBinding(ctx context.Context, a *runtimeActor, fence pa.Fence, readFacts NativeContextRead) (pa.ExecutionBinding, bool, error) {
	var empty pa.ExecutionBinding
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	r.mu.Lock()
	unavailable := r.closed || a.retained || r.disabled[pa.ProjectSelection{MachineID: fence.Project.MachineID, ProjectID: fence.Project.ProjectID}]
	r.mu.Unlock()
	if unavailable || a.opening || a.closing || a.stopped.Load() || a.lifetime.Err() != nil || a.session == nil || a.turnID == "" {
		return empty, false, nil
	}
	facts, err := readFacts(ctx, fence.Key, a.turnID)
	if err != nil {
		return empty, false, err
	}
	bot, conversation := facts.Bot, facts.Conversation
	if bot.ConversationID != conversation.ID {
		return empty, false, runtimeFailure(pa.NotFound, "conversation")
	}
	if err = pa.CheckFence(fence, currentFence(bot, conversation)); err != nil {
		return empty, false, err
	}
	if conversation.ActiveTurnID != a.turnID {
		return empty, false, nil
	}
	if facts.Turn == nil {
		return empty, false, runtimeFailure(pa.NotFound, "turn")
	}
	turn := facts.Turn
	if turn.Room != nil || turn.Fence != fence || (turn.State != pa.TurnRunning && turn.State != pa.TurnWaitingPermission) {
		return empty, false, nil
	}
	if facts.Binding == nil {
		return empty, false, runtimeFailure(pa.NotFound, "binding")
	}
	binding := *facts.Binding
	if binding.Fence != fence || binding.Revoked {
		return empty, false, runtimeFailure(pa.Revoked, "native_context_binding")
	}
	if binding.NativeSessionID == "" {
		return empty, false, nil
	}
	resolved, err := r.o.ProjectResolver.Resolve(ctx, pa.ProjectSelection{MachineID: fence.Key.MachineID, ProjectID: bot.Project.ProjectID})
	if err != nil {
		return empty, false, err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: fence.Key.MachineID, ProjectID: bot.Project.ProjectID}, resolved); err != nil {
		return empty, false, err
	}
	if resolved.Scope != bot.Project {
		return empty, false, runtimeFailure(pa.ScopeDenied, "native_context_project")
	}
	memory := facts.Memory
	if memory.Key != fence.Key || memory.Project != bot.Project || memory.Revision != binding.MemoryRevision || memory.ForgottenThrough != binding.ForgottenThrough || bot.Instructions.Revision != binding.InstructionRevision {
		return empty, false, runtimeFailure(pa.ContextStale, "native_context_revision")
	}
	return binding, true, nil
}

// The terminal projection is captured only after a successful prompt and is
// published only after the native session's cleanup and the turn commit.
func completedContextSample(session ProviderSession, turn pa.Turn) pa.ContextSample {
	if turn.Room != nil {
		return pa.ContextSample{}
	}
	reader, ok := session.(NativeContextReader)
	if !ok {
		return pa.ContextSample{}
	}
	state := reader.NativeContext()
	if !state.UsageKnown || state.Fence != turn.Fence || state.NativeSessionID == "" ||
		state.NativeSessionID != session.SessionID() || state.UsedTokens < 0 || state.WindowTokens <= 0 {
		return pa.ContextSample{}
	}
	return pa.ContextSample{TurnID: turn.ID, BindingID: turn.Fence.BindingID, Generation: turn.Fence.Generation,
		MemoryRevision: turn.MemoryRevision, UsedTokens: state.UsedTokens, WindowTokens: state.WindowTokens}
}
