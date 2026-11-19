package projectassistant

import (
	"context"
	"errors"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NativeMaintenanceIntent is a source-owned, isolated qualification request.
// A binding, an authority epoch and adapter evidence must all match again
// immediately before the native write. No prompt or tool payload is accepted.
type NativeMaintenanceIntent struct {
	ID                    string
	Binding               pa.ExecutionBinding
	PrincipalID           string
	ReaderID              string
	AuthorizationRevision string
	CLIVersion            string
	AdapterVersion        string
	Evidence              string
}

func (i NativeMaintenanceIntent) Validate() error {
	if err := pa.CheckFence(i.Binding.Fence, i.Binding.Fence); err != nil {
		return err
	}
	if strings.TrimSpace(i.ID) == "" ||
		i.Binding.NativeSessionID == "" || i.Binding.Revoked || i.Binding.SessionSlot != "" ||
		i.PrincipalID == "" || i.ReaderID == "" || i.AuthorizationRevision == "" ||
		i.CLIVersion == "" || i.AdapterVersion == "" || i.Evidence == "" {
		return runtimeFailure(pa.Invalid, "native_maintenance_intent")
	}
	return nil
}

type NativeMaintenanceRecord struct {
	Intent NativeMaintenanceIntent
	// An empty State is the durable intent before any possible compaction.
	State    NativeCompactionState
	NativeID string
}

// NativeMaintenanceJournal is separate from the Bot archive and its writer.
// Reserve must atomically enforce replay identity and the unknown barrier.
// Resolve must compare against a still-pending intent. A recovered unknown
// record cannot be overwritten by a late terminal or used to send again.
type NativeMaintenanceJournal interface {
	Lookup(context.Context, NativeMaintenanceIntent) (NativeMaintenanceRecord, bool, error)
	Reserve(context.Context, NativeMaintenanceIntent) (NativeMaintenanceRecord, bool, error)
	Resolve(context.Context, NativeMaintenanceRecord) (NativeMaintenanceRecord, error)
}

// QualifyNativeMaintenance is an explicit internal operation with an isolated,
// tool-free Provider and journal supplied by qualification composition. The
// regular Bot provider, scheduler and HTTP service never call this entry point.
func (r *Engine) QualifyNativeMaintenance(ctx context.Context, intent NativeMaintenanceIntent, journal NativeMaintenanceJournal, provider Provider, check func(context.Context) error) (record NativeMaintenanceRecord, err error) {
	if journal == nil || provider == nil || check == nil {
		return record, runtimeFailure(pa.Disabled, "native_maintenance_qualification")
	}
	if err = intent.Validate(); err != nil {
		return record, err
	}
	fence := intent.Binding.Fence
	if err = check(ctx); err != nil {
		return record, err
	}
	if err = r.authorized(ctx, fence.Key, fence.Project); err != nil {
		return record, err
	}
	if old, found, e := journal.Lookup(ctx, intent); e != nil || found {
		if e == nil {
			e = maintenanceReplayError(old)
		}
		return old, e
	}
	a, err := r.reserveNativeMaintenance(fence)
	if err != nil {
		return record, err
	}
	defer r.release(a)
	defer func() {
		r.mu.Lock()
		if !a.retained {
			a.slot = false
			<-r.slots
		}
		r.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, r.o.Limits.PromptTimeout)
	stop := context.AfterFunc(a.lifetime, cancel)
	defer stop()
	defer cancel()

	bot, target, err := r.maintenanceTarget(ctx, intent, check)
	if err != nil {
		return record, err
	}
	caps, err := provider.Capabilities(ctx, bot)
	if err != nil {
		return record, err
	}
	if err = pa.CheckExecutionCapabilities(caps, fence); err != nil {
		return record, err
	}
	if caps.Agent != intent.Binding.Agent || caps.CLIVersion != intent.CLIVersion ||
		caps.AdapterVersion != intent.AdapterVersion || caps.Evidence != intent.Evidence ||
		!caps.Resume || caps.SendMessage || caps.NativeMemory != pa.NativeMemoryDisabled || !caps.NativeMemoryVerified {
		return record, runtimeFailure(pa.Unsupported, "native_maintenance_capabilities")
	}

	// Reserve before a native Open so a second owner never opens the same
	// conversation while this operation is live or still has an unknown effect.
	// A crash after reservation is conservatively unknown, even before Begin.
	var reserved bool
	err = r.o.Memory.WithCurrent(ctx, fence.Key, intent.Binding.MemoryRevision, func(pa.ContextSnapshot) error {
		currentBot, current, e := r.maintenanceTarget(ctx, intent, check)
		if e != nil {
			return e
		}
		if current != target || !sameExecutionConfig(bot, currentBot) {
			return runtimeFailure(pa.ScopeDenied, "native_maintenance_project")
		}
		record, reserved, e = journal.Reserve(ctx, intent)
		return e
	})
	if err != nil {
		return record, err
	}
	if !reserved {
		return record, maintenanceReplayError(record)
	}

	result, runErr := r.runNativeMaintenance(ctx, a, intent, bot, target, provider, check)
	if runErr == nil {
		final := NativeMaintenanceRecord{Intent: intent, State: result.State, NativeID: result.ID}
		err = r.o.Memory.WithCurrent(ctx, fence.Key, intent.Binding.MemoryRevision, func(pa.ContextSnapshot) error {
			currentBot, current, e := r.maintenanceTarget(ctx, intent, check)
			if e != nil {
				return e
			}
			if current != target || !sameExecutionConfig(bot, currentBot) {
				return runtimeFailure(pa.ScopeDenied, "native_maintenance_project")
			}
			record, e = journal.Resolve(ctx, final)
			return e
		})
		if err == nil {
			if record.State == NativeCompactionUnknown {
				return record, runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_recovered")
			}
			return record, nil
		}
		runErr = err
	}

	// The source or request may already be gone. Recording uncertainty can
	// only seal the existing intent, never reopen it or authorize another send.
	sealCtx, sealCancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	defer sealCancel()
	record, err = journal.Resolve(sealCtx, NativeMaintenanceRecord{Intent: intent, State: NativeCompactionUnknown})
	return record, errors.Join(runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance"), runErr, err)
}

func maintenanceReplayError(record NativeMaintenanceRecord) error {
	switch record.State {
	case "":
		return runtimeFailure(pa.CapacityFull, "native_maintenance_intent_pending")
	case NativeCompactionUnknown:
		return runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance")
	default:
		return nil
	}
}

func (r *Engine) reserveNativeMaintenance(fence pa.Fence) (*runtimeActor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.disabled[pa.ProjectSelection{MachineID: fence.Project.MachineID, ProjectID: fence.Project.ProjectID}] {
		return nil, runtimeFailure(pa.Disabled, "project")
	}
	if a := r.actors[fence.Key]; a != nil {
		if a.retained {
			return nil, runtimeFailure(pa.ExecutionEffectUnknown, "owned_cleanup")
		}
		return nil, runtimeFailure(pa.CapacityFull, "native_maintenance_busy")
	}
	if len(r.actors) >= r.o.Limits.MaxBots {
		return nil, runtimeFailure(pa.CapacityFull, "active_bots")
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return nil, runtimeFailure(pa.CapacityFull, "native_maintenance_process")
	}
	lifetime, stop := context.WithCancel(context.Background())
	a := &runtimeActor{key: fence.Key, project: fence.Project, users: 1, lifetime: lifetime, stop: stop, slot: true, maintenance: true}
	r.actors[a.key] = a
	return a, nil
}

func (r *Engine) maintenanceTarget(ctx context.Context, intent NativeMaintenanceIntent, check func(context.Context) error) (pa.Bot, pa.ProjectResolution, error) {
	fence := intent.Binding.Fence
	var target pa.ProjectResolution
	if err := ctx.Err(); err != nil {
		return pa.Bot{}, target, err
	}
	if err := check(ctx); err != nil {
		return pa.Bot{}, target, err
	}
	if err := r.authorized(ctx, fence.Key, fence.Project); err != nil {
		return pa.Bot{}, target, err
	}
	bot, err := r.o.Repository.Bot(ctx, fence.Key)
	if err != nil {
		return bot, target, err
	}
	c, err := r.o.Repository.Conversation(ctx, fence.Key, bot.ConversationID)
	if err != nil {
		return bot, target, err
	}
	if err = pa.CheckFence(fence, currentFence(bot, c)); err != nil {
		return bot, target, err
	}
	pending, err := r.o.Repository.PendingTurns(ctx, fence.Key)
	if err != nil {
		return bot, target, err
	}
	if c.ActiveTurnID != "" || len(pending) != 0 {
		return bot, target, runtimeFailure(pa.CapacityFull, "native_maintenance_busy")
	}
	binding, err := r.o.Repository.Binding(ctx, fence.Key, fence.BindingID)
	if err != nil {
		return bot, target, err
	}
	if binding != intent.Binding || binding.Agent != bot.Agent || binding.LoginProfile != bot.LoginProfile ||
		binding.InstructionRevision != bot.Instructions.Revision || binding.PrimarySupersededAt != 0 {
		return bot, target, runtimeFailure(pa.StaleGeneration, "native_maintenance_binding")
	}
	memory, err := r.o.Memory.Current(ctx, fence.Key)
	if err != nil {
		return bot, target, err
	}
	if err = pa.CheckOpaqueResume(binding, memory); err != nil {
		return bot, target, err
	}
	if binding.MemoryRevision != memory.Revision {
		return bot, target, runtimeFailure(pa.ContextStale, "native_maintenance_memory")
	}
	selection := pa.ProjectSelection{MachineID: fence.Project.MachineID, ProjectID: fence.Project.ProjectID}
	target, err = r.o.ProjectResolver.Resolve(ctx, selection)
	if err == nil {
		err = pa.ValidateTarget(selection, target)
	}
	if err == nil && (target.Scope != fence.Project || target.Directory == "") {
		err = runtimeFailure(pa.ScopeDenied, "native_maintenance_project")
	}
	return bot, target, err
}

func (r *Engine) runNativeMaintenance(ctx context.Context, a *runtimeActor, intent NativeMaintenanceIntent, bot pa.Bot, target pa.ProjectResolution, provider Provider, check func(context.Context) error) (result NativeCompactionResult, err error) {
	binding := intent.Binding
	a.mu.Lock()
	a.opening = true
	a.mu.Unlock()
	session, err := provider.Open(ctx, OpenRequest{Bot: bot, PrimaryAgent: binding.Agent, Binding: binding, Directory: target.Directory, Resume: true},
		func(context.Context, ProtocolEvent) error {
			return runtimeFailure(pa.ScopeDenied, "native_maintenance_event")
		})
	a.mu.Lock()
	a.opening, a.session = false, session
	a.mu.Unlock()
	if session == nil {
		if err == nil {
			err = runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_open")
		}
		var notStarted *OpenNotStarted
		if !errors.As(err, &notStarted) {
			r.retain(a, err)
		}
		return result, err
	}
	defer func() { err = errors.Join(err, r.closeSession(a, session)) }()
	if err != nil {
		return result, err
	}

	reader, readOK := session.(NativeContextReader)
	compactor, compactOK := session.(NativeCompactor)
	if !readOK || !compactOK {
		return result, runtimeFailure(pa.Unsupported, "native_maintenance_port")
	}
	state := reader.NativeContext()
	if session.SessionID() != binding.NativeSessionID || state.NativeSessionID != binding.NativeSessionID || state.Fence != binding.Fence {
		return result, runtimeFailure(pa.StaleGeneration, "native_maintenance_session")
	}
	if !state.CommandsKnown || !state.CompactAdvertised {
		return result, runtimeFailure(pa.Unsupported, "native_maintenance_command")
	}
	var pending PendingNativeCompaction
	err = r.o.Memory.WithCurrent(ctx, a.key, binding.MemoryRevision, func(pa.ContextSnapshot) error {
		currentBot, current, e := r.maintenanceTarget(ctx, intent, check)
		if e != nil {
			return e
		}
		if current != target || !sameExecutionConfig(bot, currentBot) {
			return runtimeFailure(pa.StaleVersion, "native_maintenance_configuration")
		}
		pending, e = compactor.BeginCompaction(ctx, binding.Fence)
		return e
	})
	if err != nil {
		return result, err
	}
	if pending == nil {
		return result, runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_send")
	}
	result, err = pending.Wait(ctx)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if result.Fence != binding.Fence || result.NativeSessionID != binding.NativeSessionID || result.ID == "" {
		return result, runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_terminal_identity")
	}
	switch result.State {
	case NativeCompactionCompleted, NativeCompactionFailed, NativeCompactionCancelled:
		return result, nil
	default:
		return result, runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_terminal")
	}
}
