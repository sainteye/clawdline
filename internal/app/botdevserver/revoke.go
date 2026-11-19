package botdevserver

import (
	"context"
	"errors"

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
// to a model. Authority loss stops the Bot's known processes; there is no auto
// restart on a later successful check and no inference that an unknown PID died.
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
		if err := check(ctx, e.saved.Spec); err != nil {
			if ctx.Err() != nil {
				return errors.Join(result, ctx.Err())
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
	}
	return result
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
