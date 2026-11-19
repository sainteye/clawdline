package botdevserver

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Restart is a clean-shutdown fact, not an approval or a process handle. The
// terminal receipt binds it to one exact definition and completed attempt.
type Restart struct {
	Saved SavedServer
	Boot  string // SHA-256 of the operating system's boot identity.
}

func (r Restart) Validate() error {
	b, err := hex.DecodeString(r.Boot)
	if err != nil || len(b) != 32 || r.Saved.ID == "" || r.Saved.Spec.Validate() != nil || r.Saved.Attempt.Validate() != nil || r.Saved.Attempt.State != Stopped || r.Saved.Attempt.Failure != "" {
		return failure(pa.Invalid, "server_restart")
	}
	i := r.Saved.Attempt.Identity
	fingerprint, _ := r.Saved.Spec.Fingerprint()
	if i.ServerID != r.Saved.ID || i.Key != r.Saved.Spec.Key || i.Project != r.Saved.Spec.Project || i.Fingerprint != fingerprint {
		return failure(pa.ScopeDenied, "server_restart")
	}
	return nil
}

// Restarts retains at most one checkpoint per bounded definition. Take atomically
// consumes the batch, including stale checkpoints, before any new launch. An
// uncertain Take must return no work. A lost acknowledgement cannot replay it.
type Restarts interface {
	SaveRestart(context.Context, Restart) error
	TakeRestarts(context.Context) ([]Restart, error)
}

// CloseForRestart preserves only attempts whose own tree was proven stopped.
// Disable, explicit stop, natural exit and revocation use Close without intent.
func (r *Registry) CloseForRestart(ctx context.Context, boot string) error {
	return r.close(ctx, boot)
}

// ResumeClean is explicit Host composition after current authority is available;
// OpenRegistry still performs no mutation or launch. Boot mismatch consumes the
// intent without restarting. Every launch needs a new exact standing-rule check;
// a one-time choice and a historical approval are never carried over.
func (r *Registry) ResumeClean(ctx context.Context, boot string) error {
	if r.o.Restarts == nil {
		return nil
	}
	rows, err := r.o.Restarts.TakeRestarts(ctx)
	if err != nil {
		return errors.Join(failure(pa.ExecutionEffectUnknown, "server_restart_take"), err)
	}
	if len(rows) > capacity.MaximumBotServerDefinitions {
		return failure(pa.CapacityFull, "server_definitions")
	}
	for _, row := range rows {
		if err := row.Validate(); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if boot == "" || row.Boot != boot || r.o.AuthorizeRestart == nil {
			continue
		}
		// start rechecks current source and the complete expected receipt. Its
		// private mode selects the standing-rule-only authorizer, never review.
		_, _ = r.startMode(ctx, row.Saved.Spec, r.o.ID(), &row.Saved, true)
	}
	return nil
}
