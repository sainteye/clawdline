package projectassistant

import (
	"context"
	"errors"
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
	r.notifyLocked()
	r.mu.Unlock()
}

// The execution owner alone closes its session. A failed close keeps both the
// handle and process slot, including after its prompt caller has timed out.
func (r *Engine) closeSession(a *runtimeActor, session ProviderSession) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	defer cancel()
	err := session.Close(ctx)
	if err != nil {
		r.retain(a, err)
		return errors.Join(runtimeFailure(pa.ExecutionEffectUnknown, "owned_cleanup"), err)
	}
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	return nil
}

func (r *Engine) stopPendingLocked(ctx context.Context, a *runtimeActor) error {
	a.stopped = true
	if a.cancel != nil {
		a.cancel()
	}
	return QuiesceHistory(ctx, r.o.Repository, r.o.Clock, r.o.ID, a.key)
}

// QuiesceHistory stops durable chat work without constructing a Runtime or
// provider. It shares the runtime terminal-receipt namespace and CAS contract.
// A failed/partial scan remains inspectable and is safe to repeat explicitly.
func QuiesceHistory(ctx context.Context, repo Repository, clock func() time.Time, id func() string, key pa.BotKey) error {
	if repo == nil || clock == nil || id == nil {
		return runtimeFailure(pa.Invalid, "history_cleanup")
	}
	pending, err := repo.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
	for _, t := range pending {
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
		if t.State != pa.TurnQueued && t.State != pa.TurnAccepted && t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
			continue
		}
		state := pa.TurnCancelled
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
			state = pa.TurnUnknown
		}
		t.State = state
		t.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "execution"}
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
			if state == pa.TurnUnknown {
				offer.State = pa.PermissionUnknown
			} else if !clock().Before(offer.ExpiresAt) {
				offer.State = pa.PermissionExpired
			} else {
				offer.State = pa.PermissionCancelled
			}
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
		_, err = repo.Commit(ctx, Mutation{Command: command, ExpectedVersion: expected, Bot: &bot, Conversation: &conversation, Turn: &t, Permissions: permissions, Receipt: pa.Receipt{ID: id(), Command: command, State: effect, ResourceID: t.ID}, Events: events})
		if err != nil {
			return err
		}
	}
	return nil
}

// Close seals admission before cancellation. It is bounded by the existing
// cleanup policy and never detaches an outstanding Open/Close owner.
func (r *Engine) Close(ctx context.Context) error { return r.shutdown(ctx, nil) }

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
	a.mu.Unlock()
	// A provider reported an unknown Open without a cleanup handle. Keep the
	// owner/slot; inventing a successful close would discard that uncertainty.
	r.mu.Lock()
	slot := a.slot
	r.mu.Unlock()
	if session == nil && slot {
		return
	}
	if session != nil {
		if err := r.closeSession(a, session); err != nil {
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	a.mu.Lock()
	err := r.stopPendingLocked(ctx, a)
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
