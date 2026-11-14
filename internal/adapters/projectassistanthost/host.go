// Package projectassistanthost composes the removable Bot feature with the
// existing database, canonical Project registry and work broker.
package projectassistanthost

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type DispatchPolicy struct {
	Allowed        bool
	AllowedClaims  []string
	AnyProjectFile bool
	Assistant      string
	PermissionMode string
	TimeoutMinutes int
}

type Options struct {
	Database *sql.DB
	// Reader borrows Store's existing query-only pool. Host never closes it.
	Reader *sql.DB
	// ReadViews is an optional composition override for controlled port fixtures.
	ReadViews app.ReadViews
	// Rules lists and revokes the standing rules Claude Code keeps per Project.
	Rules             app.RuleStore
	ReadViewByteLimit int
	MachineID         string
	Projects          app.ProjectResolver
	Broker            *orchestrator.Broker
	Clock             func() time.Time
	ID                func() string
	Limits            app.RuntimeLimits
	// Authority derives the current human principal from each request context.
	Authority func(context.Context, pa.BotKey, bool) (app.Authority, error)
	// Source reads independent current background policy, not HTTP credentials.
	// A read returns the complete status, including its current write ceiling.
	Source         func(context.Context, pa.BotKey, bool) (pa.SourceStatus, error)
	DispatchPolicy func(context.Context, pa.BotKey) (DispatchPolicy, error)
	Provider       func() (app.Provider, error)
	// NativeWork is composition-only. Unregister must remove its process-local
	// entry synchronously without IO or background work; Host owns its lifetime.
	NativeWork func(context.Context, app.OpenRequest, NativeWork) (func(), error)
	// Attachments reads the pictures sent with messages for their prompts.
	Attachments app.AttachmentReader
}

type Host struct {
	o               Options
	mu              sync.Mutex
	transition      sync.Mutex
	enabled, closed bool
	cleanupPending  bool
	store           *featurestore.Store
	service         *app.Service
	engine          *app.Engine
	authority       *orchestrator.AssistantAuthority
	bridge          *app.BrokerWorkBridge
}

func failure(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

// New only reads schema/data. A fresh default-off source creates no feature
// tables, runtime, provider, actor authority or broker subscription.
func New(ctx context.Context, o Options) (*Host, error) {
	if o.Database == nil || o.MachineID == "" || o.Projects == nil || o.Broker == nil || o.Clock == nil || o.ID == nil || o.Authority == nil || o.Source == nil || o.Limits.MaxBots < 1 || o.Limits.MaxProcesses < 1 || o.Limits.PromptTimeout <= 0 || o.Limits.CleanupTimeout <= 0 {
		return nil, failure(pa.Invalid, "host_options")
	}
	h := &Host{o: o}
	exists, _, err := h.inspect(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		if err = h.makeService(); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *Host) inspect(ctx context.Context) (bool, bool, error) {
	var exists int
	if err := h.o.Database.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_schema'`).Scan(&exists); err != nil {
		return false, false, err
	}
	if exists == 0 {
		return false, false, nil
	}
	var version int
	if err := h.o.Database.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil {
		return true, false, failure(pa.Unsupported, "schema_version")
	}
	if version != pa.SchemaVersion {
		return true, false, failure(pa.Unsupported, "schema_version")
	}
	var has bool
	err := h.o.Database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assistant_bots WHERE machine=?)`, h.o.MachineID).Scan(&has)
	return true, has, err
}

func (h *Host) Status(ctx context.Context) (pa.FeatureState, error) {
	_, has, err := h.inspect(ctx)
	h.mu.Lock()
	enabled := h.enabled && !h.closed
	h.mu.Unlock()
	return pa.FeatureState{Enabled: enabled, HasData: has}, err
}

func (h *Host) makeService() error {
	if h.service != nil {
		return nil
	}
	h.store = featurestore.New(h.o.Database)
	views := h.o.ReadViews
	if views == nil && h.o.Reader != nil {
		var err error
		views, err = featurestore.NewReadViews(h.o.Reader)
		if err != nil {
			return err
		}
	}
	s, err := app.NewService(app.ServiceOptions{Repository: h.store, ReadViews: views, ReadViewByteLimit: h.o.ReadViewByteLimit, Memory: h.store, Events: h.store, Runtime: runtimeFacade{h}, Projects: h.o.Projects, Rules: h.o.Rules, Authorize: h.human, Clock: h.o.Clock, ID: h.o.ID})
	if err == nil {
		h.service = s
	}
	return err
}

func (h *Host) Service() (*app.Service, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.service == nil {
		return nil, failure(pa.Disabled, "feature")
	}
	return h.service, nil
}

func (h *Host) human(ctx context.Context, key pa.BotKey, write bool) (app.Authority, error) {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return app.Authority{}, failure(pa.Disabled, "host_closed")
	}
	if key.MachineID != h.o.MachineID {
		return app.Authority{}, failure(pa.ScopeDenied, "machine")
	}
	a, err := h.o.Authority(ctx, key, write)
	if err != nil {
		return a, err
	}
	state, err := h.Status(ctx)
	if err != nil {
		return a, err
	}
	a.Feature.Enabled = a.Feature.Enabled && state.Enabled
	a.Feature.HasData = state.HasData
	if !a.Feature.Enabled {
		a.Source.Access = a.Feature.Access()
	}
	return a, nil
}

func (h *Host) background(ctx context.Context, key pa.BotKey, write bool) (pa.FeatureState, pa.SourceStatus, error) {
	if key.MachineID != h.o.MachineID {
		return pa.FeatureState{}, pa.SourceStatus{}, failure(pa.ScopeDenied, "machine")
	}
	state, err := h.Status(ctx)
	if err != nil {
		return state, pa.SourceStatus{}, err
	}
	s, err := h.o.Source(ctx, key, write)
	if err != nil {
		return state, s, err
	}
	if s.MachineID != key.MachineID {
		return state, s, failure(pa.ScopeDenied, "source")
	}
	if !state.Enabled {
		s.Access = state.Access()
	}
	if write {
		if err = state.CheckWrite(); err != nil {
			return state, s, err
		}
	}
	return state, s, pa.CheckSource(s, write)
}

func (h *Host) current(ctx context.Context, key pa.BotKey) (orchestrator.AssistantCurrent, error) {
	state, source, err := h.background(ctx, key, false)
	if err != nil {
		return orchestrator.AssistantCurrent{}, err
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return orchestrator.AssistantCurrent{}, failure(pa.Disabled, "feature")
	}
	x, err := st.Export(ctx, key)
	if err != nil {
		return orchestrator.AssistantCurrent{}, err
	}
	b, c := x.Bot, x.Conversation
	resolved, err := h.o.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: b.Project.MachineID, ProjectID: b.Project.ProjectID})
	if err != nil {
		return orchestrator.AssistantCurrent{}, err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: b.Project.MachineID, ProjectID: b.Project.ProjectID}, resolved); err != nil {
		return orchestrator.AssistantCurrent{}, err
	}
	if resolved.Scope != b.Project {
		return orchestrator.AssistantCurrent{}, failure(pa.ScopeDenied, "canonical_project")
	}
	f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	out := orchestrator.AssistantCurrent{Fence: f, Source: source, Feature: state, Project: resolved}
	if c.BindingID != "" {
		found := false
		for _, binding := range x.Bindings {
			if binding.Fence == f {
				found = true
				out.BindingRevoked = binding.Revoked
				break
			}
		}
		if !found {
			return out, failure(pa.StaleGeneration, "binding")
		}
	}
	for _, turn := range x.Turns {
		if turn.ID == c.ActiveTurnID && turn.Fence == f && (turn.State == pa.TurnRunning || turn.State == pa.TurnWaitingPermission) {
			out.ActiveTurnID = turn.ID
			break
		}
	}
	if h.o.DispatchPolicy != nil {
		p, e := h.o.DispatchPolicy(ctx, key)
		if e == nil {
			// Isolated work needs a repository worktree. Another Project kind
			// keeps chat and never asks the authority for a dispatch actor.
			out.DispatchAllowed = p.Allowed && p.TimeoutMinutes > 0 && resolved.Scope.Kind == pa.RepositoryProject
			out.AllowedClaims = append([]string(nil), p.AllowedClaims...)
			out.AnyProjectFile = p.AnyProjectFile
			out.Assistant = p.Assistant
			out.PermissionMode = p.PermissionMode
			out.TimeoutMinutes = p.TimeoutMinutes
		}
	}
	return out, nil
}

// Called at composition time, never while the host broker is being rebound by
// another owner. Once installed, disable/enable reuses these exact objects.
func (h *Host) connect() error {
	if h.bridge != nil {
		if h.o.Broker.AssistantAuthority != h.authority || h.o.Broker.AssistantWorkChanged == nil {
			return failure(pa.ScopeDenied, "broker_owner")
		}
		return nil
	}
	if h.o.Broker.AssistantAuthority != nil || h.o.Broker.AssistantWorkChanged != nil {
		return failure(pa.ScopeDenied, "broker_owner")
	}
	a, err := orchestrator.NewAssistantAuthority(orchestrator.AssistantAuthorityOptions{Current: h.current, Clock: h.o.Clock})
	if err != nil {
		return err
	}
	h.o.Broker.AssistantAuthority = a
	b, err := app.NewWorkBridge(app.WorkBridgeOptions{Repository: h.store, Broker: h.o.Broker, Clock: h.o.Clock})
	if err != nil {
		h.o.Broker.AssistantAuthority = nil
		return err
	}
	h.authority, h.bridge = a, b
	h.o.Broker.AssistantWorkChanged = b.Sync
	return nil
}

func (h *Host) Enable(ctx context.Context) error {
	if !h.transition.TryLock() {
		return failure(pa.ExecutionEffectUnknown, "host_transition")
	}
	defer h.transition.Unlock()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return failure(pa.Disabled, "host_closed")
	}
	if h.enabled {
		h.mu.Unlock()
		return nil
	}
	r := h.engine
	pending := h.cleanupPending
	h.mu.Unlock()
	if r != nil {
		u := r.Usage()
		if !u.Known || u.ActiveActors != 0 || u.Processes != 0 {
			return failure(pa.ExecutionEffectUnknown, "owned_cleanup")
		}
	}
	if pending {
		// A turn still active in the store now was cut off by the previous
		// daemon process (a disable quiesces its own turns first), so it is
		// recorded as one a restart cut off and may be resumed.
		cleanup, cancel := context.WithTimeout(ctx, h.o.Limits.CleanupTimeout)
		err := h.quiesceFor(cleanup, app.RestartField)
		cancel()
		if err != nil {
			return errors.Join(failure(pa.ExecutionEffectUnknown, "durable_cleanup"), err)
		}
		h.mu.Lock()
		h.cleanupPending = false
		h.mu.Unlock()
	}
	if err := (featurestore.Installer{}).Install(ctx, h.o.Database); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.makeService(); err != nil {
		return err
	}
	if err := h.connect(); err != nil {
		return err
	}
	// Only a disabled, proven clean Engine can be replaced.
	if !h.enabled {
		h.engine = nil
	}
	h.enabled = true
	go h.resumeInterrupted()
	return nil
}

// resumeInterrupted lets each Bot carry on with the turn the previous daemon
// process cut off, as Codync's bot actor does on spawn. It runs after Enable
// returns and changes nothing for a Bot without such a turn.
func (h *Host) resumeInterrupted() {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.o.Limits.CleanupTimeout*4)
	defer cancel()
	var keys []pa.BotKey
	query := pa.ListBots{MachineID: h.o.MachineID, PageSize: pa.MaxPageRows}
	for {
		page, err := st.List(ctx, query)
		if err != nil {
			return
		}
		for _, bot := range page.Items {
			pending, err := st.PendingTurns(ctx, bot.Bot.Key)
			if err == nil && len(pending) == 1 && pending[0].State == pa.TurnUnknown && pending[0].Failure != nil && pending[0].Failure.Field == app.RestartField {
				keys = append(keys, bot.Bot.Key)
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	if len(keys) == 0 {
		return
	}
	// The runtime is otherwise built on first use; resuming is a use, so it
	// is built only when some Bot has a turn to resume.
	r, err := h.runtime()
	if err != nil {
		log.Printf("bots: interrupted turns were not resumed: %s", pa.Code(err))
		return
	}
	for _, key := range keys {
		if err := r.ResumeInterrupted(ctx, key); err != nil {
			log.Printf("bots: an interrupted turn was not resumed: %v", err)
		}
	}
}

func (h *Host) Disable(ctx context.Context) error { return h.stop(ctx, false) }
func (h *Host) Close(ctx context.Context) error   { return h.stop(ctx, true) }
func (h *Host) stop(ctx context.Context, closeHost bool) error {
	if !h.transition.TryLock() {
		return failure(pa.ExecutionEffectUnknown, "host_transition")
	}
	defer h.transition.Unlock()
	ctx, cancel := context.WithTimeout(ctx, h.o.Limits.CleanupTimeout)
	defer cancel()
	h.mu.Lock()
	h.enabled = false
	h.closed = h.closed || closeHost
	r := h.engine
	h.cleanupPending = h.store != nil
	h.mu.Unlock()
	var result error
	if r != nil {
		if closeHost {
			result = r.CloseForRestart(ctx)
		} else {
			result = r.Close(ctx)
		}
	}
	field := "execution"
	if closeHost {
		field = app.RestartField
	}
	result = errors.Join(result, h.quiesceFor(ctx, field))
	if result != nil {
		return errors.Join(failure(pa.ExecutionEffectUnknown, "host_cleanup"), result)
	}
	h.mu.Lock()
	if h.engine == r {
		h.engine = nil
	}
	h.cleanupPending = false
	h.mu.Unlock()
	return nil
}

// One page at a time under the caller's SINGLE total cleanup deadline. There
// is no provider, actor allocation, retained cursor registry or background loop.
func (h *Host) quiesce(ctx context.Context) error { return h.quiesceFor(ctx, "execution") }

func (h *Host) quiesceFor(ctx context.Context, field string) error {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return nil
	}
	query := pa.ListBots{MachineID: h.o.MachineID, PageSize: pa.MaxPageRows}
	for {
		page, err := st.List(ctx, query)
		if err != nil {
			return err
		}
		for _, bot := range page.Items {
			if err = app.QuiesceHistoryFor(ctx, st, h.o.Clock, h.o.ID, bot.Bot.Key, field); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		query.Cursor = page.NextCursor
	}
}

func (h *Host) Usage() app.RuntimeUsage {
	h.mu.Lock()
	r := h.engine
	pending := h.cleanupPending
	h.mu.Unlock()
	if r == nil {
		return app.RuntimeUsage{Known: !pending}
	}
	usage := r.Usage()
	if pending {
		usage.Known = false
	}
	return usage
}

func (h *Host) runtime() (*app.Engine, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled || h.closed {
		return nil, failure(pa.Disabled, "feature")
	}
	if h.engine != nil {
		return h.engine, nil
	}
	p := &lazyProvider{factory: h.o.Provider}
	r, err := app.NewRuntime(app.RuntimeOptions{Repository: h.store, Memory: h.store, WorkBridge: h.bridge, ProjectResolver: h.o.Projects, Provider: &ownedProvider{h: h, p: p}, Clock: h.o.Clock, ID: h.o.ID, Limits: h.o.Limits, Attachments: h.o.Attachments, Authorize: func(ctx context.Context, key pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		f, s, e := h.background(ctx, key, true)
		if e != nil {
			return f, s, e
		}
		_, e = h.current(ctx, key)
		return f, s, e
	}})
	if err == nil {
		h.engine = r
	}
	return r, err
}

// Recover is explicit opt-in queue recovery; only known-unsent queue entries
// may run. For history reconciliation without an Engine, use RecoverWork.
func (h *Host) Recover(ctx context.Context, key pa.BotKey) error {
	if _, _, err := h.background(ctx, key, true); err != nil {
		return err
	}
	r, err := h.runtime()
	if err != nil {
		return err
	}
	return r.Recover(ctx, key)
}

func (h *Host) RecoverWork(ctx context.Context, key pa.BotKey, taskID string, expires time.Time) (pa.WorkLink, error) {
	if _, err := h.human(ctx, key, false); err != nil {
		return pa.WorkLink{}, err
	}
	// Service's authority validation remains mandatory on this composition seam.
	s, err := h.Service()
	if err != nil {
		return pa.WorkLink{}, err
	}
	if _, _, _, err = s.Get(ctx, key); err != nil {
		return pa.WorkLink{}, err
	}
	current, err := h.current(ctx, key)
	if err != nil {
		return pa.WorkLink{}, err
	}
	h.mu.Lock()
	err = h.connect()
	a, b := h.authority, h.bridge
	h.mu.Unlock()
	if err != nil {
		return pa.WorkLink{}, err
	}
	actor, err := a.Issue(ctx, current.Fence, []pa.ActorOperation{pa.ReadWork}, expires)
	if err != nil {
		return pa.WorkLink{}, err
	}
	return b.Status(ctx, actor, taskID)
}

type runtimeFacade struct{ h *Host }

func (f runtimeFacade) Submit(ctx context.Context, q app.Submission) (pa.Receipt, error) {
	r, e := f.h.runtime()
	if e != nil {
		return pa.Receipt{}, e
	}
	return r.Submit(ctx, q)
}
func (f runtimeFacade) SendNow(ctx context.Context, q app.SendNowRequest) (pa.Receipt, error) {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return pa.Receipt{}, failure(pa.StaleGeneration, "runtime")
	}
	return r.SendNow(ctx, q)
}
func (f runtimeFacade) Stop(ctx context.Context, q app.StopRequest) (pa.Receipt, error) {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return pa.Receipt{}, failure(pa.StaleGeneration, "runtime")
	}
	return r.Stop(ctx, q)
}
func (f runtimeFacade) Answer(ctx context.Context, q app.AnswerRequest) (pa.Receipt, error) {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return pa.Receipt{}, failure(pa.StaleGeneration, "runtime")
	}
	return r.Answer(ctx, q)
}
func (f runtimeFacade) Disable(ctx context.Context, p pa.ProjectSelection) error {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.Disable(ctx, p)
}
