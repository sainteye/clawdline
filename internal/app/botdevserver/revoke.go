package botdevserver

import (
	"context"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// RevokeBot is trusted Host teardown, independent of human/native access that
// may already have been revoked. It only closes this source's known handles;
// a lost owner remains unknown and its definition cannot be removed.
func (r *Registry) RevokeBot(ctx context.Context, key pa.BotKey, remove bool) error {
	if key.MachineID != r.o.MachineID || key.BotID == "" {
		return failure(pa.ScopeDenied, "server_scope")
	}
	ctx, cancel := context.WithTimeout(ctx, r.o.CleanupTimeout)
	defer cancel()
	r.ops.Lock()
	defer r.ops.Unlock()
	r.mu.Lock()
	var entries []*registered
	for _, e := range r.rows {
		if e.saved.Spec.Key == key {
			entries = append(entries, e)
		}
	}
	r.mu.Unlock()
	var result error
	for _, e := range entries {
		err := r.revokeEntry(ctx, e, remove)
		result = errors.Join(result, err)
	}
	return result
}

// Reconcile checks every source-owned definition without exposing its content
// to a model. Only an authoritative denial (scope lost, Bot removed, Bots
// disabled, rule or context revoked) stops the Bot's known processes; there is
// no auto restart after one. Any other check error is a failed read: the
// server keeps running, is marked unconfirmed, and the next tick checks again.
func (r *Registry) Reconcile(ctx context.Context, check func(context.Context, Spec) error) error {
	if check == nil {
		return failure(pa.Invalid, "server_reconcile")
	}

	r.mu.Lock()
	var entries []*registered
	for _, e := range r.rows {
		entries = append(entries, e)
	}
	r.mu.Unlock()
	var result error
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		err := check(ctx, e.saved.Spec)
		if err != nil && ctx.Err() != nil {
			return errors.Join(result, ctx.Err())
		}
		if err == nil || !AuthoritativeDenial(err) {
			r.mu.Lock()
			if err == nil {
				e.unconfirmedSince = time.Time{}
			} else if e.unconfirmedSince.IsZero() {
				e.unconfirmedSince = r.o.Clock()
			}
			r.mu.Unlock()
			result = errors.Join(result, err)
			continue
		}
		r.ops.Lock()
		r.mu.Lock()
		current := r.rows[e.saved.ID] == e
		r.mu.Unlock()
		if current {
			result = errors.Join(result, r.revokeEntry(ctx, e, false))
		}
		r.ops.Unlock()
	}
	return result
}

// AuthoritativeDenial is a refusal that a later successful read cannot undo.
// Everything else, including a raw file or database error, is only unknown.
func AuthoritativeDenial(err error) bool {
	switch pa.Code(err) {
	case pa.ScopeDenied, pa.NotFound, pa.Disabled, pa.Revoked, pa.Unpaired, pa.ContextStale:
		return true
	}
	return false
}

// Unconfirmed reports that Reconcile has failed to confirm this definition's
// authority for at least capacity.BotServerUnconfirmedNotice. The server is
// left running; the person decides whether to stop it.
func (r *Registry) Unconfirmed(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.rows[id]
	if e == nil || e.unconfirmedSince.IsZero() {
		return false
	}
	return r.o.Clock().Sub(e.unconfirmedSince) >= capacity.BotServerUnconfirmedNotice
}

// ReleasedUnconfirmed is the terminal failure a person records when they
// release an attempt whose process Clawdline can no longer prove stopped.
const ReleasedUnconfirmed = "released_unconfirmed"

// ReleaseExpected is a person's explicit release of an attempt whose owner is
// lost or whose cleanup could not be confirmed. It signals nothing: the
// process, if it still runs, is no longer stopped or counted by Clawdline.
// The attempt becomes failed, its slot is freed and the definition can then
// be removed. An attempt with a live, confirmed owner must be stopped instead.
func (r *Registry) ReleaseExpected(ctx context.Context, expected SavedServer) error {
	ctx, cancel := r.operation(ctx)
	defer cancel()
	r.ops.Lock()
	defer r.ops.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	e := r.rows[expected.ID]
	r.mu.Unlock()
	if e == nil {
		return failure(pa.NotFound, "server")
	}
	saved := r.view(e)
	if saved != expected {
		return failure(pa.StaleVersion, "server_definition")
	}
	if err := r.o.CheckControl(ctx, saved.Spec); err != nil {
		return err
	}
	if saved.Attempt.State != Unknown {
		return failure(pa.Invalid, "server_release")
	}
	journal, err := r.o.Catalog.Journal(ctx, saved, saved.Attempt.Identity)
	if err != nil {
		return err
	}
	current, err := journal.Load(ctx, saved.Attempt.Identity)
	if err != nil {
		return err
	}
	next := current
	if activeState(current.State) {
		next.Version++
		next.State, next.Failure = Failed, ReleasedUnconfirmed
		if err := journal.Save(ctx, current.Version, next); err != nil {
			return err
		}
	}
	r.mu.Lock()
	owner := e.owner
	e.owner, e.saved.Attempt, e.unconfirmedSince = nil, next, time.Time{}
	if e.reserved {
		e.reserved = false
		r.active--
	}
	r.mu.Unlock()
	if owner != nil {
		owner.cancel()
	}
	return nil
}

// Caller holds ops; a stale definition cannot stop another definition's owner.
func (r *Registry) revokeEntry(ctx context.Context, e *registered, remove bool) error {
	r.mu.Lock()
	owner := e.owner
	r.mu.Unlock()
	var err error
	if owner != nil {
		err = owner.Close(ctx)
		if state := owner.Snapshot().State; state == Stopped || state == Failed {
			r.release(e, owner)
		}
	} else if activeState(r.view(e).Attempt.State) {
		err = failure(pa.ExecutionEffectUnknown, "server_owner")
	}
	if err == nil && remove {
		err = r.removeDefinition(ctx, e)
	}
	return err
}

func (r *Registry) removeDefinition(ctx context.Context, e *registered) error {
	saved := r.view(e)
	if activeState(saved.Attempt.State) {
		return failure(pa.ExecutionEffectUnknown, "server_owner")
	}
	if err := r.o.Catalog.Remove(ctx, saved); err != nil {
		return err
	}
	fingerprint, _ := saved.Spec.Fingerprint()
	r.mu.Lock()
	owner := e.owner
	r.mu.Unlock()
	if owner != nil {
		owner.cancel()
	}
	r.release(e, owner)
	r.mu.Lock()
	delete(r.rows, saved.ID)
	delete(r.bySpec, fingerprint)
	r.mu.Unlock()
	return nil
}
