package botdevserver

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type SavedServer struct {
	ID      string
	Spec    Spec
	Attempt Record
}

// Catalog is source-local. Create stores only a definition; Journal binds its
// immutable identity and current attempt in the same CAS as each receipt.
// List must refuse an oversized result rather than silently truncate it.
type Catalog interface {
	List(context.Context, int) ([]SavedServer, error)
	Create(context.Context, SavedServer) error
	// Remove must compare the complete current receipt and refuse active or
	// unknown attempts in the same transaction that deletes the definition.
	Remove(context.Context, SavedServer) error
	Journal(context.Context, SavedServer, Identity) (Journal, error)
}

type RegistryOptions struct {
	MachineID string
	Lifetime  context.Context
	Catalog   Catalog
	ID        func() string
	// CheckControl validates current source/Bot/Project operate authority.
	CheckControl func(context.Context, Spec) error
	// AuthorizeStart obtains exact command approval and returns a Host-owned
	// launch check. That check must not expire merely because the Agent turn
	// ends. It still rechecks source revocation, Project and forgotten context.
	AuthorizeStart func(context.Context, SavedServer) (func(context.Context) error, error)
	// AuthorizeRestart cannot ask a person or retain an earlier AllowOnce.
	AuthorizeRestart func(context.Context, SavedServer) (func(context.Context) error, error)
	Restarts         Restarts
	Launch           func(context.Context, Spec) (Process, error)
	CleanupTimeout   time.Duration
}

type registered struct {
	saved    SavedServer
	owner    *Owner
	log      *LogTail
	reserved bool
}

type Registry struct {
	o      RegistryOptions
	ctx    context.Context
	cancel context.CancelFunc
	ops    sync.Mutex // Definition admission is serial; no buffered work queue.
	mu     sync.Mutex
	closed bool
	rows   map[string]*registered
	bySpec map[string]string
	active int
}

// OpenRegistry is inspection only and launches nothing. There must be exactly
// one Registry per source writer/Host, as with the existing Bot Store.
func OpenRegistry(ctx context.Context, o RegistryOptions) (*Registry, error) {
	if o.MachineID == "" || o.Lifetime == nil || o.Catalog == nil || o.ID == nil || o.CheckControl == nil || o.AuthorizeStart == nil || o.Launch == nil || o.CleanupTimeout <= 0 {
		return nil, failure(pa.Invalid, "server_registry")
	}
	rows, err := o.Catalog.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil {
		return nil, err
	}
	if len(rows) > capacity.MaximumBotServerDefinitions {
		return nil, failure(pa.CapacityFull, "server_definitions")
	}
	lifetime, cancel := context.WithCancel(o.Lifetime)
	r := &Registry{o: o, ctx: lifetime, cancel: cancel, rows: map[string]*registered{}, bySpec: map[string]string{}}
	for _, saved := range rows {
		fingerprint, err := saved.Spec.Fingerprint()
		if err != nil || saved.ID == "" || saved.Spec.Key.MachineID != o.MachineID || r.rows[saved.ID] != nil || r.bySpec[fingerprint] != "" {
			cancel()
			return nil, failure(pa.Invalid, "server_catalog")
		}
		e := &registered{saved: saved, log: &LogTail{}}
		if saved.Attempt.Version > 0 {
			e.log.incomplete() // Past output was not retained across Host restart.
			i := saved.Attempt.Identity
			if saved.Attempt.Validate() != nil || i.ServerID != saved.ID || i.Key != saved.Spec.Key || i.Project != saved.Spec.Project || i.Fingerprint != fingerprint {
				cancel()
				return nil, failure(pa.ScopeDenied, "server_catalog")
			}
			if activeState(saved.Attempt.State) {
				e.saved.Attempt.State, e.saved.Attempt.Failure = Unknown, "owner_lost"
				e.reserved = true
				r.active++
			}
		} else if saved.Attempt != (Record{}) {
			cancel()
			return nil, failure(pa.Invalid, "server_catalog")
		}
		r.rows[saved.ID], r.bySpec[fingerprint] = e, saved.ID
	}
	return r, nil
}

func activeState(s State) bool { return s == Accepted || s == Running || s == Stopping || s == Unknown }

func (r *Registry) operation(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if r.ctx.Err() != nil {
		cancel()
	}
	stop := context.AfterFunc(r.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (r *Registry) view(e *registered) SavedServer {
	r.mu.Lock()
	saved, owner := e.saved, e.owner
	r.mu.Unlock()
	if owner != nil {
		if receipt := owner.Snapshot(); receipt.Version > 0 || receipt.State != "" {
			saved.Attempt = receipt
		}
	}
	return saved
}

// Start is internal mutation plumbing. Native callers must wrap it in Caller.Do
// and paired controls must retain their source-owned expected-version fence.
// An already active command is a no-op, never a second independent process.
func (r *Registry) Start(ctx context.Context, spec Spec, attemptID string) (SavedServer, error) {
	return r.start(ctx, spec, attemptID, nil)
}
func (r *Registry) StartExpected(ctx context.Context, saved SavedServer, attemptID string) (SavedServer, error) {
	return r.start(ctx, saved.Spec, attemptID, &saved)
}
func (r *Registry) start(ctx context.Context, spec Spec, attemptID string, expected *SavedServer) (SavedServer, error) {
	return r.startMode(ctx, spec, attemptID, expected, false)
}
func (r *Registry) startMode(ctx context.Context, spec Spec, attemptID string, expected *SavedServer, restarting bool) (SavedServer, error) {
	fingerprint, err := spec.Fingerprint()
	if err != nil {
		return SavedServer{}, err
	}
	if spec.Key.MachineID != r.o.MachineID || attemptID == "" {
		return SavedServer{}, failure(pa.ScopeDenied, "server_scope")
	}
	ctx, cancel := r.operation(ctx)
	defer cancel()
	if err = r.o.CheckControl(ctx, spec); err != nil {
		return SavedServer{}, err
	}
	// Inspect before asking for approval. Keeping an already owned process does
	// not execute a command and must not create another approval card.
	r.ops.Lock()
	e, prior, done, err := r.admitExpected(ctx, fingerprint, attemptID, expected)
	if !done && err == nil && e == nil {
		e, err = r.define(ctx, spec, fingerprint)
	}
	r.ops.Unlock()
	if done || err != nil {
		return prior, err
	}
	// Human approval can wait, so it must not hold either registry mutex.
	reviewedID := e.saved.ID
	authorize := r.o.AuthorizeStart
	if restarting {
		authorize = r.o.AuthorizeRestart
	}
	if authorize == nil {
		return SavedServer{}, failure(pa.ScopeDenied, "server_restart_approval")
	}
	check, err := authorize(ctx, r.view(e))
	if err != nil {
		return SavedServer{}, err
	}
	if check == nil {
		return SavedServer{}, failure(pa.ScopeDenied, "server_approval")
	}
	r.ops.Lock()
	defer r.ops.Unlock()
	if err = ctx.Err(); err != nil {
		return SavedServer{}, err
	}
	if err = r.o.CheckControl(ctx, spec); err != nil {
		return SavedServer{}, err
	}
	e, prior, done, err = r.admitExpected(ctx, fingerprint, attemptID, expected)
	if done || err != nil {
		return prior, err
	}
	if e == nil || e.saved.ID != reviewedID {
		return SavedServer{}, failure(pa.StaleVersion, "server_definition")
	}
	if err = check(ctx); err != nil {
		return SavedServer{}, err
	}

	i := Identity{Key: spec.Key, Project: spec.Project, ServerID: e.saved.ID, AttemptID: attemptID, Fingerprint: fingerprint}
	journal, err := r.o.Catalog.Journal(ctx, r.view(e), i)
	if err != nil {
		return r.view(e), err
	}
	log := &LogTail{}
	owner, err := Open(ctx, Options{Identity: i, Lifetime: r.ctx, Journal: journal, Check: check, CleanupTimeout: r.o.CleanupTimeout,
		Launch: func(ctx context.Context) (Process, error) {
			p, err := r.o.Launch(ctx, spec)
			if p != nil {
				if output := p.Output(); output != nil {
					go func() {
						if _, err := io.Copy(log, output); err != nil {
							log.incomplete()
						}
					}()
				} else {
					log.incomplete()
				}
			}
			return p, err
		}})
	if err != nil {
		return r.view(e), err
	}
	r.mu.Lock()
	e.owner, e.log = owner, log
	e.reserved = true
	r.active++
	r.mu.Unlock()
	_, err = owner.Start(ctx)
	state := owner.Snapshot().State
	if state == Stopped || state == Failed || state == "" {
		r.release(e, owner)
	} else if state != Unknown {
		go func() {
			<-owner.done
			if owner.Snapshot().State != Unknown {
				r.release(e, owner)
			}
		}()
	}
	return r.view(e), err
}

// Definitions persist before review, so an exact standing rule can reference a
// real, bounded, immutable ID. A declined command has no attempt or process.
func (r *Registry) define(ctx context.Context, spec Spec, fingerprint string) (*registered, error) {
	saved := SavedServer{ID: r.o.ID(), Spec: spec}
	if saved.ID == "" {
		return nil, failure(pa.Invalid, "server_id")
	}
	r.mu.Lock()
	collision := r.rows[saved.ID] != nil
	r.mu.Unlock()
	if collision {
		return nil, failure(pa.IdempotencyConflict, "server_id")
	}
	if err := r.o.Catalog.Create(ctx, saved); err != nil {
		return nil, err
	}
	e := &registered{saved: saved, log: &LogTail{}}
	r.mu.Lock()
	r.rows[saved.ID], r.bySpec[fingerprint] = e, saved.ID
	r.mu.Unlock()
	return e, nil
}

// admit holds ops. A completed owner may have published its terminal receipt
// before its watcher gets CPU time; release that exact generation here too.
func (r *Registry) admit(ctx context.Context, fingerprint, attemptID string) (*registered, SavedServer, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, SavedServer{}, false, err
	}
	r.mu.Lock()
	closed := r.closed
	e := r.rows[r.bySpec[fingerprint]]
	r.mu.Unlock()
	if closed || r.ctx.Err() != nil {
		return nil, SavedServer{}, false, failure(pa.Disabled, "server_registry")
	}
	if e != nil {
		prior := r.view(e)
		if prior.Attempt.State == Unknown {
			return e, prior, true, failure(pa.ExecutionEffectUnknown, "server_attempt")
		}
		if prior.Attempt.Identity.AttemptID == attemptID || activeState(prior.Attempt.State) {
			return e, prior, true, nil
		}
		r.mu.Lock()
		owner := e.owner
		r.mu.Unlock()
		r.release(e, owner)
	}
	r.mu.Lock()
	full := r.active >= capacity.MaximumBotServerProcesses
	definitionsFull := e == nil && len(r.rows) >= capacity.MaximumBotServerDefinitions
	r.mu.Unlock()
	if full {
		return e, SavedServer{}, false, failure(pa.CapacityFull, "server_processes")
	}
	if definitionsFull {
		return e, SavedServer{}, false, failure(pa.CapacityFull, "server_definitions")
	}
	return e, SavedServer{}, false, nil
}

func (r *Registry) release(e *registered, owner *Owner) {
	r.mu.Lock()
	if e.owner == owner && e.reserved {
		e.reserved = false
		r.active--
	}
	r.mu.Unlock()
}

// The source-owned attempt fence prevents a delayed stop from stopping a later
// explicit restart of the same server. Callers cannot omit this fence.
func (r *Registry) Stop(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string) error {
	return r.stop(ctx, key, project, id, attemptID, nil)
}
func (r *Registry) StopExpected(ctx context.Context, saved SavedServer) error {
	return r.stop(ctx, saved.Spec.Key, saved.Spec.Project, saved.ID, saved.Attempt.Identity.AttemptID, &saved)
}
func (r *Registry) stop(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string, expected *SavedServer) error {
	ctx, cancel := r.operation(ctx)
	defer cancel()
	r.ops.Lock()
	defer r.ops.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	e := r.rows[id]
	r.mu.Unlock()
	if e == nil {
		return failure(pa.NotFound, "server")
	}
	saved := r.view(e)
	if expected != nil && saved != *expected {
		return failure(pa.StaleVersion, "server_definition")
	}
	if saved.Spec.Key != key || saved.Spec.Project != project {
		return failure(pa.ScopeDenied, "server_scope")
	}
	if attemptID == "" || saved.Attempt.Identity.AttemptID != attemptID {
		return failure(pa.StaleGeneration, "server_attempt")
	}
	if err := r.o.CheckControl(ctx, saved.Spec); err != nil {
		return err
	}
	r.mu.Lock()
	owner := e.owner
	r.mu.Unlock()
	if owner == nil {
		if activeState(saved.Attempt.State) {
			return failure(pa.ExecutionEffectUnknown, "server_owner")
		}
		return nil
	}
	err := owner.Close(ctx)
	if state := owner.Snapshot().State; state == Stopped || state == Failed {
		r.release(e, owner)
	}
	return err
}

// Inspect returns only this Bot's full canonical Project. It performs the
// source's current content/Forget check even for an otherwise idle definition.
func (r *Registry) Inspect(ctx context.Context, key pa.BotKey, project pa.ProjectScope) ([]SavedServer, error) {
	ctx, cancel := r.operation(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	entries := make([]*registered, 0, len(r.rows))
	for _, e := range r.rows {
		if e.saved.Spec.Key == key && e.saved.Spec.Project == project {
			entries = append(entries, e)
		}
	}
	r.mu.Unlock()
	rows := make([]SavedServer, 0, len(entries))
	for _, e := range entries {
		saved := r.view(e)
		if err := r.o.CheckControl(ctx, saved.Spec); err != nil {
			return nil, err
		}
		rows = append(rows, saved)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

func (r *Registry) ReadLog(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string) (Log, error) {
	return r.readLog(ctx, key, project, id, attemptID, true)
}

// ReadHumanLog still binds full resource identity but uses the caller's human
// read authorization instead of a live native/source write capability.
func (r *Registry) ReadHumanLog(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string) (Log, error) {
	return r.readLog(ctx, key, project, id, attemptID, false)
}
func (r *Registry) readLog(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string, authorize bool) (Log, error) {
	if authorize {
		var cancel func()
		ctx, cancel = r.operation(ctx)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return Log{}, err
	}
	r.mu.Lock()
	e := r.rows[id]
	if e == nil {
		r.mu.Unlock()
		return Log{}, failure(pa.NotFound, "server")
	}
	spec, owner, saved, log := e.saved.Spec, e.owner, e.saved.Attempt, e.log
	r.mu.Unlock()
	if spec.Key != key || spec.Project != project {
		return Log{}, failure(pa.ScopeDenied, "server_scope")
	}
	if authorize {
		if err := r.o.CheckControl(ctx, spec); err != nil {
			return Log{}, err
		}
	}
	if owner != nil {
		saved = owner.Snapshot()
	}
	if attemptID == "" || saved.Identity.AttemptID != attemptID {
		return Log{}, failure(pa.StaleGeneration, "server_attempt")
	}
	if owner == nil && activeState(saved.State) {
		return Log{}, failure(pa.ExecutionEffectUnknown, "server_log")
	}
	return log.Snapshot(), nil
}

func (r *Registry) Remove(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string) error {
	return r.remove(ctx, key, project, id, attemptID, nil)
}
func (r *Registry) RemoveExpected(ctx context.Context, saved SavedServer) error {
	return r.remove(ctx, saved.Spec.Key, saved.Spec.Project, saved.ID, saved.Attempt.Identity.AttemptID, &saved)
}
func (r *Registry) remove(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id, attemptID string, expected *SavedServer) error {
	ctx, cancel := r.operation(ctx)
	defer cancel()
	r.ops.Lock()
	defer r.ops.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	e := r.rows[id]
	r.mu.Unlock()
	if e == nil {
		return failure(pa.NotFound, "server")
	}
	saved := r.view(e)
	if expected != nil && saved != *expected {
		return failure(pa.StaleVersion, "server_definition")
	}
	if saved.Spec.Key != key || saved.Spec.Project != project {
		return failure(pa.ScopeDenied, "server_scope")
	}
	if err := r.o.CheckControl(ctx, saved.Spec); err != nil {
		return err
	}
	if saved.Attempt.Identity.AttemptID != attemptID {
		return failure(pa.StaleGeneration, "server_attempt")
	}
	if activeState(saved.Attempt.State) {
		return failure(pa.ExecutionEffectUnknown, "server_owner")
	}
	return r.removeDefinition(ctx, e)
}

type Usage struct {
	Definitions, Processes int
	Known                  bool
}

func (r *Registry) Usage() Usage {
	r.mu.Lock()
	rows := make([]*registered, 0, len(r.rows))
	for _, e := range r.rows {
		rows = append(rows, e)
	}
	u := Usage{Definitions: len(rows), Processes: r.active, Known: true}
	r.mu.Unlock()
	for _, e := range rows {
		if r.view(e).Attempt.State == Unknown {
			u.Known = false
		}
	}
	return u
}

func (r *Registry) Close(ctx context.Context) error {
	return r.close(ctx, "")
}
func (r *Registry) close(ctx context.Context, boot string) error {
	ctx, cancel := context.WithTimeout(ctx, r.o.CleanupTimeout)
	defer cancel()
	r.ops.Lock()
	defer r.ops.Unlock()
	r.mu.Lock()
	if r.closed {
		boot = "" // A repeated close cannot create or renew restart intent.
	}
	r.closed = true
	entries := make([]*registered, 0, len(r.rows))
	for _, e := range r.rows {
		entries = append(entries, e)
	}
	r.mu.Unlock()
	wanted := make(map[*registered]bool)
	if boot != "" && r.o.Restarts != nil {
		for _, e := range entries {
			state := r.view(e).Attempt.State
			wanted[e] = state == Accepted || state == Running
		}
	}
	r.cancel()
	var result error
	for _, e := range entries {
		r.mu.Lock()
		owner := e.owner
		r.mu.Unlock()
		if owner != nil {
			err := owner.Close(ctx)
			result = errors.Join(result, err)
			if state := owner.Snapshot().State; state == Stopped || state == Failed {
				r.release(e, owner)
			}
			if err == nil && wanted[e] {
				row := Restart{Saved: r.view(e), Boot: boot}
				if row.Saved.Attempt.State == Stopped && row.Saved.Attempt.Failure == "" {
					result = errors.Join(result, r.o.Restarts.SaveRestart(ctx, row))
				}
			}
		} else if activeState(r.view(e).Attempt.State) {
			result = errors.Join(result, failure(pa.ExecutionEffectUnknown, "server_owner"))
		}
	}
	return result
}
