// Package botdevserver owns individual source-local command attempts. It does
// not expose a client route, approve commands or install a persistence format.
package botdevserver

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type State string

const (
	Accepted State = "accepted"
	Running  State = "running"
	Stopping State = "stopping"
	Stopped  State = "stopped"
	Failed   State = "failed"
	Unknown  State = "unknown"
)

// Identity is immutable for one attempt. A restart is a new explicit attempt,
// admitted by the enclosing server registry only after the old owner is clean.
// Fingerprint binds the exact command/directory/approval supplied by that owner.
type Identity struct {
	Key         pa.BotKey
	Project     pa.ProjectScope
	ServerID    string
	AttemptID   string
	Fingerprint string
}

func (i Identity) Validate() error {
	digest, err := hex.DecodeString(i.Fingerprint)
	if err != nil || len(digest) != 32 || i.Key.MachineID == "" || i.Key.BotID == "" || i.Project.MachineID != i.Key.MachineID || i.Project.ProjectID == "" || i.Project.CanonicalID == "" || (i.Project.Kind != pa.RepositoryProject && i.Project.Kind != pa.PlaceProject) || i.ServerID == "" || i.AttemptID == "" {
		return failure(pa.Invalid, "server_identity")
	}
	return nil
}

type Record struct {
	Identity Identity
	Version  int64
	State    State
	Failure  string
}

func (r Record) Validate() error {
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	if r.Version <= 0 {
		return failure(pa.Invalid, "server_revision")
	}
	switch r.State {
	case Accepted, Running, Stopping, Stopped, Failed, Unknown:
		return nil
	default:
		return failure(pa.Invalid, "server_state")
	}
}

// Journal is a source-local CAS ledger for this exact attempt. Save is atomic;
// an error may mean the commit succeeded but its acknowledgement was lost.
// Load returns Version zero only when this identity has never been admitted.
type Journal interface {
	Load(context.Context, Identity) (Record, error)
	Save(context.Context, int64, Record) error
}

type Process interface {
	Output() io.Reader
	WaitExit(context.Context) error
	Close(context.Context) error
}

type Options struct {
	Identity Identity
	Lifetime context.Context // Host-owned, never the submitting HTTP/tool context.
	Journal  Journal
	// Check revalidates the source, Bot, full canonical Project and the exact
	// command approval. Launch uses a trusted source environment, not an Agent's.
	Check func(context.Context) error
	// Launch must return every acquired handle, including alongside an error.
	// Without a handle, only ExecutionEffectUnknown may describe a possibly
	// started command. Other errors must prove execution never began.
	Launch         func(context.Context) (Process, error)
	CleanupTimeout time.Duration // Trusted composition's existing cleanup budget.
}

type Owner struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	record Record
	p      Process
	worker bool
	sealed bool
	done   chan struct{}
	once   sync.Once
}

func failure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

// Open only reads the ledger. An interrupted accepted/running/stopping attempt
// becomes an unknown observation, never a launch or a success inferred by PID.
func Open(ctx context.Context, o Options) (*Owner, error) {
	i := o.Identity
	if i.Validate() != nil || o.Lifetime == nil || o.Journal == nil || o.Check == nil || o.Launch == nil || o.CleanupTimeout <= 0 {
		return nil, failure(pa.Invalid, "server_owner")
	}
	r, err := o.Journal.Load(ctx, i)
	if err != nil {
		return nil, err
	}
	if r.Version < 0 || (r.Version > 0 && r.Identity != i) {
		return nil, failure(pa.ScopeDenied, "server_attempt")
	}
	if r.Version > 0 {
		switch r.State {
		case Accepted, Running, Stopping:
			r.State, r.Failure = Unknown, "owner_lost"
		case Stopped, Failed, Unknown:
		default:
			return nil, failure(pa.Invalid, "server_state")
		}
	} else {
		if r.State != "" || r.Failure != "" || r.Identity != (Identity{}) {
			return nil, failure(pa.Invalid, "server_attempt")
		}
		r.Identity = i
	}
	lifetime, cancel := context.WithCancel(o.Lifetime)
	return &Owner{o: o, ctx: lifetime, cancel: cancel, record: r, done: make(chan struct{})}, nil
}

func (o *Owner) Snapshot() Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.record
}

// save holds mu. Retain a possible committed intent on every journal error;
// neither a caller retry nor reopening this attempt can launch it again.
func (o *Owner) save(ctx context.Context, state State, reason string) error {
	next := o.record
	next.Version++
	if next.Version <= o.record.Version {
		return failure(pa.CapacityFull, "server_revision")
	}
	next.State, next.Failure = state, reason
	if err := o.o.Journal.Save(ctx, o.record.Version, next); err != nil {
		o.record.State, o.record.Failure = Unknown, "journal_unknown"
		return failure(pa.ExecutionEffectUnknown, "server_journal")
	}
	o.record = next
	return nil
}

// Start returns durable acceptance, not proof of a running process. The
// single launch belongs to the Host lifetime after acceptance; disconnection
// of this request cannot cancel it or lose its cleanup owner.
func (o *Owner) Start(ctx context.Context) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if err := o.o.Check(ctx); err != nil {
		return Record{}, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.record.State == Unknown {
		return o.record, failure(pa.ExecutionEffectUnknown, "server_attempt")
	}
	if o.record.Version > 0 {
		return o.record, nil
	}
	if o.sealed || o.ctx.Err() != nil {
		return o.record, failure(pa.Disabled, "server_owner")
	}
	if err := o.save(ctx, Accepted, ""); err != nil {
		return o.record, err
	}
	o.worker = true
	go o.run()
	return o.record, nil
}

func (o *Owner) finish(state State, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.record.State != Unknown {
		ctx, cancel := context.WithTimeout(context.Background(), o.o.CleanupTimeout)
		defer cancel()
		_ = o.save(ctx, state, reason)
	}
}

func (o *Owner) run() {
	defer o.once.Do(func() { close(o.done) })
	if err := o.o.Check(o.ctx); err != nil {
		o.finish(Stopped, "admission_closed")
		return
	}
	if o.ctx.Err() != nil {
		o.finish(Stopped, "")
		return
	}
	p, err := o.o.Launch(o.ctx)
	if p == nil {
		if err == nil || pa.Code(err) == pa.ExecutionEffectUnknown {
			o.finish(Unknown, "launch_unknown")
		} else if o.ctx.Err() != nil {
			o.finish(Stopped, "")
		} else {
			o.finish(Failed, "launch_failed")
		}
		return
	}
	o.mu.Lock()
	o.p = p
	stopping := o.sealed || o.ctx.Err() != nil || o.record.State != Accepted
	o.mu.Unlock()
	if err == nil && !stopping {
		err = o.o.Check(o.ctx)
	}
	if err == nil && !stopping {
		o.mu.Lock()
		if o.sealed || o.ctx.Err() != nil || o.record.State != Accepted {
			stopping = true
		} else {
			err = o.save(o.ctx, Running, "")
		}
		o.mu.Unlock()
	}
	var exit error
	if err == nil && !stopping {
		exit = p.WaitExit(o.ctx)
	}
	cleanup, cancel := context.WithTimeout(context.Background(), o.o.CleanupTimeout)
	defer cancel()
	if cleanupErr := p.Close(cleanup); cleanupErr != nil {
		o.finish(Unknown, "cleanup_unknown")
		return // Retain p: an absent or timed-out observation is not clean.
	}
	o.mu.Lock()
	o.p = nil
	requested := o.sealed || o.ctx.Err() != nil
	o.mu.Unlock()
	if err != nil && pa.Code(err) == pa.ExecutionEffectUnknown {
		o.finish(Unknown, "launch_unknown")
	} else if err != nil && !requested {
		o.finish(Failed, "admission_closed")
	} else if exit != nil && !requested {
		o.finish(Failed, "process_exit")
	} else {
		o.finish(Stopped, "")
	}
}

// Close is a trusted owner shutdown, also used after source revocation. Human
// stop operations must authorize separately. It records stop before cancelling
// the lifetime and waits for the existing launch/cleanup, never starting another.
func (o *Owner) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, o.o.CleanupTimeout)
	defer cancel()
	o.mu.Lock()
	o.sealed = true
	if o.record.State == Stopped || o.record.State == Failed {
		o.cancel()
		o.mu.Unlock()
		return nil
	}
	if !o.worker {
		if o.record.State == Unknown {
			o.cancel()
			o.mu.Unlock()
			return failure(pa.ExecutionEffectUnknown, "server_owner")
		}
		err := o.save(ctx, Stopped, "")
		o.cancel()
		o.once.Do(func() { close(o.done) })
		o.mu.Unlock()
		return err
	}
	var err error
	if o.record.State != Unknown && o.record.State != Stopping {
		err = o.save(ctx, Stopping, "")
	}
	o.cancel()
	o.mu.Unlock()
	select {
	case <-o.done:
		if o.Snapshot().State == Unknown {
			return failure(pa.ExecutionEffectUnknown, "server_owner")
		}
		return err
	case <-ctx.Done():
		return errors.Join(failure(pa.ExecutionEffectUnknown, "server_cleanup"), ctx.Err())
	}
}
