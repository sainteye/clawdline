package botdevserver

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Listener is a source-owned TCP bind observation, not a probed URL or a
// promise that another device can reach it. No process identity crosses here.
type Listener struct {
	Address string
	Port    uint16
}

// ListeningProcess is optional. Unsupported, unreadable and changing ownership
// remain unknown; they must never become a guessed empty set of listeners.
type ListeningProcess interface {
	TCPListeners(context.Context) ([]Listener, error)
}

func (o *Owner) listeners(ctx context.Context, expected Record) ([]Listener, error) {
	o.mu.Lock()
	if o.record != expected {
		o.mu.Unlock()
		return nil, failure(pa.StaleVersion, "server_observation")
	}
	p := o.p
	state := o.record.State
	o.mu.Unlock()
	if state == Stopped || state == Failed {
		return []Listener{}, nil
	}
	if state != Running || p == nil {
		return nil, failure(pa.ExecutionEffectUnknown, "server_observation")
	}
	observer, ok := p.(ListeningProcess)
	if !ok {
		return nil, failure(pa.Unsupported, "server_observation")
	}
	rows, err := observer.TCPListeners(ctx)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	current := o.record == expected && o.p != nil && !o.sealed
	o.mu.Unlock()
	if !current {
		return nil, failure(pa.StaleVersion, "server_observation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// ObserveListeners is trusted Host read plumbing. It binds the complete
// source-owned snapshot twice without holding a registry lock over OS IO.
// Native and human callers retain their separate disclosure authorization.
func (r *Registry) ObserveListeners(ctx context.Context, expected SavedServer) ([]Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	e := r.rows[expected.ID]
	var owner *Owner
	if e != nil {
		owner = e.owner
	}
	r.mu.Unlock()
	if e == nil {
		return nil, failure(pa.NotFound, "server")
	}
	if r.view(e) != expected {
		return nil, failure(pa.StaleVersion, "server_observation")
	}
	var rows []Listener
	var err error
	if owner != nil {
		rows, err = owner.listeners(ctx, expected.Attempt)
	} else {
		if activeState(expected.Attempt.State) {
			return nil, failure(pa.ExecutionEffectUnknown, "server_observation")
		}
		rows = []Listener{}
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	current := r.rows[expected.ID] == e && e.owner == owner
	r.mu.Unlock()
	if !current || r.view(e) != expected {
		return nil, failure(pa.StaleVersion, "server_observation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}
