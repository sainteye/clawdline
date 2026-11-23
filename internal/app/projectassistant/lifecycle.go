package projectassistant

import (
	"context"
	"errors"
	"log"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (r *Engine) notifyLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *Engine) setProject(a *runtimeActor, project pa.ProjectScope) {
	r.mu.Lock()
	a.project = project
	if r.closed || r.disabled[pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID}] {
		a.stop()
	}
	r.mu.Unlock()
}

func (r *Engine) retain(a *runtimeActor, err error) {
	r.mu.Lock()
	a.retained, a.cleanupErr = true, err
	start := !a.retrying && !r.closed && r.o.Limits.CleanupRetries > 0 && r.o.Limits.CleanupRetryDelay > 0
	if start {
		a.retrying = true
	}
	retrying := a.retrying
	r.notifyLocked()
	r.mu.Unlock()
	// The cause was previously kept only in memory, so a Bot refusing every
	// message left no record of which cleanup step failed (2026-11-23).
	if retrying {
		log.Printf("project assistant: Agent process cleanup unconfirmed; bot=%s keeps its owner and retries cleanup: %v", a.key.BotID, err)
	} else {
		log.Printf("project assistant: Agent process cleanup unconfirmed; bot=%s keeps its owner until cleanup is confirmed: %v", a.key.BotID, err)
	}
	if start {
		go r.retryRetained(a)
	}
}

// retryRetained re-attempts an unconfirmed cleanup a bounded number of times
// with a doubling delay, so a Bot whose Agent process stopped a moment after
// the first check no longer refuses every message until Clawdline restarts
// (Beacon and Lens, 2026-11-23). Each attempt is the same verified
// retryCleanup that shutdown uses: the owner is released only when Close
// returns without error. It never resends the earlier turn, whose unknown
// outcome is already recorded.
func (r *Engine) retryRetained(a *runtimeActor) {
	delay := r.o.Limits.CleanupRetryDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for attempt := 1; attempt <= r.o.Limits.CleanupRetries; attempt++ {
		<-timer.C
		r.mu.Lock()
		if r.closed || !a.retained {
			a.retrying = false
			r.mu.Unlock()
			return
		}
		// Another owner (the finishing worker or shutdown) still holds the
		// actor; this attempt is skipped rather than run concurrently.
		if a.users == 0 {
			a.users++
			r.mu.Unlock()
			r.retryCleanup(a)
			r.mu.Lock()
			if !a.retained {
				a.retrying = false
				r.mu.Unlock()
				log.Printf("project assistant: Agent process cleanup confirmed on retry %d; bot=%s accepts messages again", attempt, a.key.BotID)
				return
			}
		}
		r.mu.Unlock()
		delay *= 2
		timer.Reset(delay)
	}
	r.mu.Lock()
	a.retrying = false
	r.mu.Unlock()
	log.Printf("project assistant: Agent process cleanup still unconfirmed after %d retries; bot=%s keeps its owner", r.o.Limits.CleanupRetries, a.key.BotID)
}

type ownedCleanupFailure struct{ error }

func (f *ownedCleanupFailure) Unwrap() error { return f.error }

// The execution owner alone closes its session. A failed close keeps both the
// handle and process slot, including after its prompt caller has timed out.
func (r *Engine) closeSession(a *runtimeActor, session ProviderSession) error {
	a.mu.Lock()
	a.closing = true
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	defer cancel()
	err := session.Close(ctx)
	if err != nil {
		r.retain(a, err)
		return &ownedCleanupFailure{errors.Join(runtimeFailure(pa.ExecutionEffectUnknown, "owned_cleanup"), err)}
	}
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	return nil
}

func (r *Engine) stopPendingLocked(ctx context.Context, a *runtimeActor) error {
	a.stopped.Store(true)
	if a.cancel != nil {
		a.cancel()
	}
	if err := QuiesceHistoryFor(ctx, r.o.Repository, r.o.Clock, r.o.ID, a.key, r.unknownField()); err != nil {
		return err
	}
	if r.unknownField() == RestartField {
		return nil
	}
	return r.cancelDueFollowUpsLocked(ctx, a)
}

func (r *Engine) cancelDueFollowUpsLocked(ctx context.Context, a *runtimeActor) error {
	x, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		return err
	}
	for _, d := range x.Routines {
		if !d.FollowUp || d.Deleted || d.NextDue.After(r.o.Clock()) {
			continue
		}
		d.Deleted, d.Enabled = true, false
		d.Version++
		bot, e := r.o.Repository.Bot(ctx, a.key)
		if e != nil {
			return e
		}
		if _, e = r.mutate(ctx, bot, Mutation{Command: r.command(a.key, pa.EditOperation, "follow-up-stop-"+d.ID, d.ID), Routine: &d, Receipt: pa.Receipt{State: pa.EffectFailed, ResourceID: d.ID}}); e != nil {
			return e
		}
	}
	return nil
}

// QuiesceHistory stops durable chat work without constructing a Runtime or
// provider. It shares the runtime terminal-receipt namespace and CAS contract.
// A failed/partial scan remains inspectable and is safe to repeat explicitly.
func QuiesceHistory(ctx context.Context, repo Repository, clock func() time.Time, id func() string, key pa.BotKey) error {
	return QuiesceHistoryFor(ctx, repo, clock, id, key, "execution")
}

// RestartField marks a turn the daemon's own shutdown cut off, which a later
// start may resume (ResumeInterrupted); "execution" marks any other stop.
const RestartField = "restart"

// QuiesceHistoryFor is QuiesceHistory recording why an active turn stopped.
func QuiesceHistoryFor(ctx context.Context, repo Repository, clock func() time.Time, id func() string, key pa.BotKey, field string) error {
	return quiesceHistory(ctx, repo, clock, id, key, field, false)
}

// RecoverPermissionWaits retires prompts whose permission request belonged to
// a previous process. Their original reply channel cannot be restored.
func RecoverPermissionWaits(ctx context.Context, repo Repository, clock func() time.Time, id func() string, key pa.BotKey) error {
	return quiesceHistory(ctx, repo, clock, id, key, "execution", true)
}

func quiesceHistory(ctx context.Context, repo Repository, clock func() time.Time, id func() string, key pa.BotKey, field string, permissionWaitsOnly bool) error {
	if repo == nil || clock == nil || id == nil {
		return runtimeFailure(pa.Invalid, "history_cleanup")
	}
	pending, err := repo.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
pendingTurns:
	for _, t := range pending {
		if permissionWaitsOnly && t.State != pa.TurnWaitingPermission && t.State != pa.TurnRunning {
			continue
		}
		x, e := repo.Export(ctx, key)
		if e != nil {
			return e
		}
		found := false
		for _, current := range x.Turns {
			if current.ID == t.ID {
				t = current
				found = true
				break
			}
		}
		if !found {
			return runtimeFailure(pa.NotFound, "cleanup_turn")
		}
		if t.State == pa.TurnUnknown {
			continue
		}
		if permissionWaitsOnly && !interruptedPermission(t, x.Permissions) {
			continue
		}
		// A planned restart retains only the original queue identity whose
		// execution has not started, or whose no-effect refusal was durably
		// proved and cleaned up. Running/unknown work never takes this path.
		if field == RestartField && (unstartedExchangeReply(t) || pa.QueuedWorkCompletion(t)) {
			continue
		}
		if field == RestartField && t.State == pa.TurnQueued && t.QueueEditID != "" {
			continue
		}
		if field == RestartField && t.State == pa.TurnQueued && t.RoutineRunID != "" {
			for _, run := range x.RoutineRuns {
				if run.ID == t.RoutineRunID && run.TurnID == t.ID && run.State == pa.TurnQueued && (t.StartedAt == nil || run.NotStartedProved) {
					continue pendingTurns
				}
			}
		}
		if t.State != pa.TurnQueued && t.State != pa.TurnAccepted && t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
			continue
		}
		state := pa.TurnCancelled
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
			state = pa.TurnUnknown
		}
		t.State, t.QueueEditID = state, ""
		t.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: field}
		conversation := x.Conversation
		if conversation.ActiveTurnID == t.ID {
			conversation.ActiveTurnID = ""
		}
		permissions := []pa.PermissionOffer{}
		events := []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}
		for _, offer := range x.Permissions {
			if offer.TurnID != t.ID || offer.State != pa.PermissionPending {
				continue
			}
			if offer.Fence != t.Fence {
				return runtimeFailure(pa.ScopeDenied, "cleanup_permission")
			}
			offer.State = terminalPermissionState(offer, state, clock())
			permissions = append(permissions, offer)
			copy := offer
			events = append(events, pa.Event{Fence: t.Fence, Kind: pa.PermissionAnswered, Permission: &copy})
		}
		command := runtimeCommand(key, pa.SendOperation, "runtime-finish-"+t.ID, t)
		bot := x.Bot
		expected := bot.Version
		bot.Version++
		bot.UpdatedAt = clock()
		for i := range events {
			events[i].Version = pa.ContractVersion
			events[i].ID = id()
			events[i].At = clock()
		}
		effect := pa.EffectFailed
		if state == pa.TurnUnknown {
			effect = pa.EffectUnknown
		}
		var run *pa.RoutineRun
		if t.RoutineRunID != "" {
			for _, v := range x.RoutineRuns {
				if v.ID == t.RoutineRunID {
					copy := v
					copy.State = state
					copy.NotStartedProved = false
					at := clock()
					copy.FinishedAt = &at
					copy.Published = state != pa.TurnUnknown
					run = &copy
					break
				}
			}
		}
		m := Mutation{RoutineRun: run, Command: command, ExpectedVersion: expected, Bot: &bot, Conversation: &conversation, Turn: &t, Permissions: permissions, Receipt: pa.Receipt{ID: id(), Command: command, State: effect, ResourceID: t.ID}, Events: events}
		if t.ExchangeID != "" {
			p, ok := repo.(CollaborationRepository)
			if !ok {
				return runtimeFailure(pa.Unsupported, "collaboration_cleanup")
			}
			_, err = p.FinishExchange(ctx, m, "")
		} else {
			_, err = repo.Commit(ctx, m)
		}
		if err != nil {
			return err
		}
	}
	if p, ok := repo.(CollaborationRepository); ok && !permissionWaitsOnly && field != RestartField {
		return p.DropExchangeReplies(ctx, key, clock())
	}
	return nil
}

func interruptedPermission(turn pa.Turn, offers []pa.PermissionOffer) bool {
	if turn.State == pa.TurnWaitingPermission {
		return true
	}
	for _, offer := range offers {
		if offer.TurnID == turn.ID && offer.Fence == turn.Fence && offer.State != pa.PermissionResolved {
			return true
		}
	}
	return false
}

func terminalPermissionState(offer pa.PermissionOffer, turnState pa.TurnState, now time.Time) pa.PermissionState {
	if !offer.ExpiresAt.IsZero() && !now.Before(offer.ExpiresAt) {
		return pa.PermissionExpired
	}
	if turnState == pa.TurnUnknown {
		return pa.PermissionUnknown
	}
	return pa.PermissionCancelled
}

// Close seals admission before cancellation. It is bounded by the existing
// cleanup policy and never detaches an outstanding Open/Close owner.
func (r *Engine) Close(ctx context.Context) error { return r.shutdown(ctx, nil) }

// CloseForRestart is Close for the daemon's own shutdown: a turn it cuts off
// is recorded with RestartField, so the next start may resume it.
func (r *Engine) CloseForRestart(ctx context.Context) error {
	r.mu.Lock()
	r.restarting = true
	r.mu.Unlock()
	return r.shutdown(ctx, nil)
}

func (r *Engine) unknownField() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.restarting {
		return RestartField
	}
	return "execution"
}

func (r *Engine) shutdown(ctx context.Context, project *pa.ProjectSelection) error {
	ctx, cancel := context.WithTimeout(ctx, r.o.Limits.CleanupTimeout)
	defer cancel()
	matches := func(a *runtimeActor) bool {
		return project == nil || a.project.MachineID == project.MachineID && a.project.ProjectID == project.ProjectID
	}
	r.mu.Lock()
	if project == nil {
		r.closed = true
	} else {
		r.disabled[*project] = true
	}
	for _, a := range r.actors {
		if matches(a) {
			a.stop()
		}
	}
	r.notifyLocked()
	r.mu.Unlock()
	attempted := make(map[*runtimeActor]bool)
	for {
		r.mu.Lock()
		remaining, unresolved := false, false
		for _, a := range r.actors {
			if !matches(a) {
				continue
			}
			remaining = true
			if a.users == 0 && a.retained {
				if attempted[a] {
					unresolved = true
					continue
				}
				attempted[a] = true
				a.users++
				// At most one cleanup owner per existing bounded actor. If it
				// ignores cancellation, subsequent Close calls only wait for it.
				go r.retryCleanup(a)
			}
		}
		changed := r.changed
		r.mu.Unlock()
		if !remaining {
			return nil
		}
		if unresolved {
			return runtimeFailure(pa.ExecutionEffectUnknown, "owned_cleanup")
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.Join(runtimeFailure(pa.ExecutionEffectUnknown, "shutdown_pending"), ctx.Err())
		}
	}
}

func (r *Engine) retryCleanup(a *runtimeActor) {
	defer r.release(a)
	a.mu.Lock()
	session := a.session
	completion := a.completion != nil
	a.mu.Unlock()
	// A provider reported an unknown Open without a cleanup handle. Keep the
	// owner/slot; inventing a successful close would discard that uncertainty.
	r.mu.Lock()
	slot := a.slot
	r.mu.Unlock()
	if session == nil && slot && !completion {
		return
	}
	if session != nil {
		if err := r.closeSession(a, session); err != nil {
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	a.mu.Lock()
	var err error
	if a.completion != nil {
		err = r.finishLocked(ctx, a, a.completion.turn, pa.TurnSucceeded, a.completion.text, nil)
		if err == nil {
			a.completion = nil
		}
	} else if !a.maintenance {
		err = r.stopPendingLocked(ctx, a)
	}
	a.mu.Unlock()
	cancel()
	if err != nil {
		r.retain(a, err)
		return
	}
	r.mu.Lock()
	a.retained, a.cleanupErr = false, nil
	if a.slot {
		a.slot = false
		<-r.slots
	}
	r.mu.Unlock()
}
