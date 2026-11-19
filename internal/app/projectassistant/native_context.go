package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NativeContext borrows only an existing execution owner. It never opens a
// provider, acquires a process slot, creates an actor, or retains a last reading.
// A known reading describes this live turn, not eligibility for compaction.
func (r *Engine) NativeContext(ctx context.Context, fence pa.Fence, readCheck func() error) (NativeContextState, error) {
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
	if readCheck != nil {
		if err := readCheck(); err != nil {
			return unknown, err
		}
	}
	before, live, err := r.nativeContextBinding(ctx, a, fence)
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
	if readCheck != nil {
		if err := readCheck(); err != nil {
			return unknown, err
		}
	}
	after, live, err := r.nativeContextBinding(ctx, a, fence)
	if err != nil || !live {
		return unknown, err
	}
	if after != before {
		return unknown, runtimeFailure(pa.StaleGeneration, "native_context_binding")
	}
	state.CompactAdvertised = state.CommandsKnown && state.CompactAdvertised
	if !state.UsageKnown || state.UsedTokens < 0 || state.WindowTokens <= 0 {
		state.UsageKnown = false
		state.UsedTokens, state.WindowTokens = 0, 0
	}
	return state, nil
}

// The caller holds actor.mu. Recheck persisted authority on both sides of the
// optional native read; an old process handle is not current binding authority.
func (r *Engine) nativeContextBinding(ctx context.Context, a *runtimeActor, fence pa.Fence) (pa.ExecutionBinding, bool, error) {
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
	bot, err := r.o.Repository.Bot(ctx, fence.Key)
	if err != nil {
		return empty, false, err
	}
	if err = r.authorized(ctx, fence.Key, bot.Project); err != nil {
		return empty, false, err
	}
	c, err := r.o.Repository.Conversation(ctx, fence.Key, bot.ConversationID)
	if err != nil {
		return empty, false, err
	}
	if err = pa.CheckFence(fence, currentFence(bot, c)); err != nil {
		return empty, false, err
	}
	if c.ActiveTurnID != a.turnID {
		return empty, false, nil
	}
	turn, err := r.o.Repository.Turn(ctx, fence.Key, a.turnID)
	if err != nil {
		return empty, false, err
	}
	if turn.Fence != fence || (turn.State != pa.TurnRunning && turn.State != pa.TurnWaitingPermission) {
		return empty, false, nil
	}
	binding, err := r.o.Repository.Binding(ctx, fence.Key, fence.BindingID)
	if err != nil {
		return empty, false, err
	}
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
	memory, err := r.o.Memory.Current(ctx, fence.Key)
	if err != nil {
		return empty, false, err
	}
	if memory.Key != fence.Key || memory.Project != bot.Project || memory.Revision != binding.MemoryRevision || memory.ForgottenThrough != binding.ForgottenThrough || bot.Instructions.Revision != binding.InstructionRevision {
		return empty, false, runtimeFailure(pa.ContextStale, "native_context_revision")
	}
	return binding, true, nil
}
