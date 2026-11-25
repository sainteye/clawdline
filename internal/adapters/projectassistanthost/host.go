// Package projectassistanthost composes the removable Bot feature with the
// existing database, canonical Project registry and work broker.
package projectassistanthost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	network "github.com/sainteye/clawdline/internal/app/botnetwork"
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
	// Shared and Agents feed dispatch rule resolution. Empty rules keep
	// Assistant for every dispatch.
	Shared pa.SharedSettings
	Agents []pa.DispatchAgentOption
}

type Options struct {
	// A composing server can finish publishing its admission state before
	// recovery or routine work checks that authority.
	DeferBackgroundStart bool

	// Both lead setup and quota checks use source-owned local observations.
	DefaultLeadAgent func(time.Time) pa.Agent
	LeadTimeZone     string
	LeadQuotaNear    func(pa.Bot, time.Time) bool
	// ServerLaunch is an optional owned-process fixture; production uses the source launcher.
	ServerLaunch func(context.Context, servers.Spec, pa.ProjectResolution) (servers.Process, error)
	// ServerAdopt is the matching optional fixture for adopting a running
	// process after a restart; production uses the source launcher's.
	ServerAdopt func(context.Context, servers.Spec, servers.Mark) (servers.Process, error)
	// ServerBootID is a trusted composition seam; production reads the OS boot identity.
	ServerBootID        func(context.Context) (string, error)
	RoutineWebhookURL   func(ctx context.Context, id, key string, wait bool) string
	NativeHistory       app.NativeMaintenanceHistory
	Assistants          *network.AssistantService
	Network             *network.Service
	NetworkTransport    func(context.Context, string) (network.Transport, error)
	NetworkAccount      func(context.Context) (string, error)
	NetworkCapabilities func(context.Context, string, string) pa.NetworkCapabilities
	Extensions          app.ExtensionInventory
	LoginProfiles       func(context.Context) (app.LoginProfiles, error)
	ProjectMemory       func(context.Context, pa.ProjectResolution, string, app.ProjectMemoryPage) (pa.MemoryLayer, string, error)
	Database            *sql.DB
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
	// SharedMemory is the machine's shared Bot memory section for one Bot.
	SharedMemory func(context.Context, pa.Bot) string
	// ManageBots calls the same source Bot operations as the console, with an
	// execution-scoped Service supplied by the Host and no daemon credential.
	ManageBots func(context.Context, *app.Service, pa.BotKey, app.LeadBotRequest) (json.RawMessage, error)
	Provider   func() (app.Provider, error)
	// NativeWork is composition-only. Unregister must remove its process-local
	// entry synchronously without IO or background work; Host owns its lifetime.
	NativeWork func(context.Context, app.OpenRequest, NativeWork) (func(), error)
	// Attachments reads the pictures sent with messages for their prompts.
	Attachments app.AttachmentReader
	Inputs      app.InputReader
	Outputs     featurestore.Outputs
	// Browser is optional; without it the browser tools answer disabled.
	Browser *BrowserAccess
}

type Host struct {
	serverRegistry *servers.Registry
	serverBoot     string
	// serverReconcileLogged is the last Reconcile failure written to the log,
	// so a failure that repeats every tick is logged once.
	serverReconcileLogged string
	profiles              app.LoginProfiles
	configurationSlots    chan struct{}
	routineStop           context.CancelFunc
	routineDone           chan struct{}
	o                     Options
	mu                    sync.Mutex
	transition            sync.Mutex
	enabled, closed       bool
	cleanupPending        bool
	permissionRecovery    bool
	store                 *featurestore.Store
	service               *app.Service
	engine                *app.Engine
	groups                *app.GroupRooms
	rooms                 *app.RoomDriver
	team                  *app.TeamCoordinator
	authority             *orchestrator.AssistantAuthority
	bridge                *app.BrokerWorkBridge
	grants                dispatchGrants
}

// directUserTurn is a turn the person started by talking to this Bot, not a
// message from another Bot, a room, the Network or a routine.
func directUserTurn(t pa.Turn) bool {
	return t.ID != "" && t.Origin == pa.UserOrigin && t.Room == nil && t.Network == nil && t.RoutineRunID == ""
}

func failure(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

// New migrates an already installed feature on the primary database before
// exposing its read service. A fresh default-off source still creates no
// feature tables, runtime, provider, actor authority or broker subscription.
func New(ctx context.Context, o Options) (*Host, error) {
	if o.Database == nil || o.MachineID == "" || o.Projects == nil || o.Broker == nil || o.Clock == nil || o.ID == nil || o.Authority == nil || o.Source == nil || o.Limits.MaxBots < 1 || o.Limits.PromptTimeout < 0 || o.Limits.CleanupTimeout <= 0 {
		return nil, failure(pa.Invalid, "host_options")
	}
	h := &Host{o: o, configurationSlots: make(chan struct{}, pa.MaxConfigurationProbes)}
	exists, _, err := h.installedSchema(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		// Disabled installations retain readable history and may skip Enable.
		// Run repeatable additive migrations here, never in the read adapter.
		if err = app.InstallSchemas(ctx, o.Database, featurestore.Installer{}, featurestore.GroupInstaller{}); err != nil {
			return nil, err
		}
		if err = serverstore.InstallCatalog(ctx, o.Database); err != nil {
			return nil, err
		}
		if err = h.makeService(); err != nil {
			return nil, err
		}
		h.permissionRecovery = true
	}
	return h, nil
}

// statusReader answers the per-request feature inspection. Every Bot read
// (roster, view, groups, attachment, handoff card, dev servers) runs it two to
// four times; on the single writer connection those SELECTs queued behind any
// write transaction in the daemon, so a 1 ms attachment read took 12 s while a
// Bot turn or the broker held the writer (measured 2026-11-23). The query-only
// pool reads the same WAL file and sees every committed schema change.
func (h *Host) statusReader() *sql.DB {
	if h.o.Reader != nil {
		return h.o.Reader
	}
	return h.o.Database
}

func (h *Host) installedSchema(ctx context.Context) (bool, int, error) {
	return installedSchemaOn(ctx, h.o.Database)
}

func installedSchemaOn(ctx context.Context, db *sql.DB) (bool, int, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_schema'`).Scan(&exists); err != nil {
		return false, 0, err
	}
	if exists == 0 {
		return false, 0, nil
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil {
		return true, 0, failure(pa.Unsupported, "schema_version")
	}
	return true, version, nil
}

func (h *Host) inspect(ctx context.Context) (bool, bool, error) {
	db := h.statusReader()
	exists, version, err := installedSchemaOn(ctx, db)
	if err != nil || !exists {
		return exists, false, err
	}
	if version != pa.SchemaVersion {
		return true, false, failure(pa.Unsupported, "schema_version")
	}
	var has bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assistant_bots WHERE machine=?) OR EXISTS(SELECT 1 FROM project_assistant_groups WHERE machine=?)`, h.o.MachineID, h.o.MachineID).Scan(&has)
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
	h.store = featurestore.New(h.o.Database).WithReader(h.o.Reader)
	if h.o.Broker.Store != nil {
		h.store.CompletionIntent = h.o.Broker.BotCompletionIntent
		h.store.DecisionIntent = h.o.Broker.BotDecisionIntent
		if h.o.Broker.BotPermission == nil {
			store := h.store
			h.o.Broker.BotPermission = store.Permission
		}
	}
	views := h.o.ReadViews
	if views == nil && h.o.Reader != nil {
		var err error
		views, err = featurestore.NewReadViews(h.o.Reader)
		if err != nil {
			return err
		}
	}

	groups, err := app.NewGroupRooms(featurestore.NewGroupStore(h.o.Database).WithReader(h.o.Reader), app.DefaultGroupLimits(), h.o.Clock)
	if err != nil {
		return err
	}
	rooms, err := app.NewRoomDriver(app.RoomDriverOptions{Rooms: groups, Bots: h.store, Limits: app.DefaultRoomLimits(), ID: h.o.ID, Authorize: func(ctx context.Context, key pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		return h.background(ctx, key, true)
	}})
	if err != nil {
		return err
	}
	rooms.Attach(runtimeFacade{h})
	team, err := app.NewTeamCoordinator(app.TeamCoordinatorOptions{Repository: h.store, Projects: h.o.Projects, Clock: h.o.Clock, ID: h.o.ID, Check: func(ctx context.Context, fence pa.Fence, turn string) (app.ManagerContext, error) {
		h.mu.Lock()
		r := h.engine
		h.mu.Unlock()
		if r == nil {
			return app.ManagerContext{}, failure(pa.Revoked, "manager_execution")
		}
		return r.ManagerContext(ctx, fence, turn)
	}, Review: func(ctx context.Context, plan app.TeamPlan, execution app.ManagerExecution, decide func(context.Context, bool, pa.SourceStatus) error) error {
		r, err := h.runtime()
		if err != nil {
			return err
		}
		return r.ReviewTeamPlan(ctx, plan, execution, decide)
	}})
	if err != nil {
		return err
	}
	h.groups, h.rooms, h.team = groups, rooms, team
	if h.o.LoginProfiles != nil {
		h.profiles, err = h.o.LoginProfiles(context.Background())
		if err != nil {
			return err
		}
	}
	s, err := app.NewService(app.ServiceOptions{NetworkAccount: h.networkAccount, NetworkSettings: h.store, NetworkCapabilities: h.o.NetworkCapabilities, Extensions: h.o.Extensions, LoginProfiles: h.profiles, ProjectMemory: h.o.ProjectMemory, Configuration: h.configuration, Groups: groups, Rooms: rooms, Repository: h.store, ReadViews: views, ReadViewByteLimit: h.o.ReadViewByteLimit, Memory: h.store, Events: h.store, Runtime: runtimeFacade{h}, Projects: h.o.Projects, Rules: h.o.Rules, SourceRules: hostSourceRules{h}, DevServers: hostDevServers{h}, Deleter: hostBotDeleter{h}, Authorize: h.human, Clock: h.o.Clock, ID: h.o.ID})
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
	out := orchestrator.AssistantCurrent{Fence: f, Source: source, Feature: state, Project: resolved, BotName: b.Name, BotAppearance: b.Appearance, BotAgent: string(b.Agent)}
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
	var active pa.Turn
	for _, turn := range x.Turns {
		if turn.ID == c.ActiveTurnID && turn.Fence == f && (turn.State == pa.TurnRunning || turn.State == pa.TurnWaitingPermission) {
			out.ActiveTurnID = turn.ID
			active = turn
			break
		}
	}
	if out.ActiveTurnID != "" && pa.IsPeerRequest(active.Origin) {
		out.WorkOwner = peerWorkOwner(ctx, st, b)
	}
	if h.o.DispatchPolicy != nil {
		p, e := h.o.DispatchPolicy(ctx, key)
		if e == nil {
			// Isolated work needs a repository worktree. Another Project kind
			// keeps chat and never asks the authority for a dispatch actor.
			allowed := p.Allowed && p.TimeoutMinutes > 0
			out.DispatchAllowed = allowed && resolved.Scope.Kind == pa.RepositoryProject
			_, resolvesFolders := h.o.Projects.(app.TeamProjectResolver)
			canResolve := allowed && resolvesFolders
			out.ProjectDispatchAllowed = canResolve && b.Lead && pa.DirectUserTurn(active)
			// Another Bot asks the person first. Saved rules and once grants
			// apply only to a turn the person started by talking to this Bot.
			if out.ProjectDispatchAskable = canResolve && !b.Lead && directUserTurn(active); out.ProjectDispatchAskable {
				rules, err := h.dispatchRules(ctx, key)
				if err != nil {
					return orchestrator.AssistantCurrent{}, err
				}
				for project := range rules {
					out.ApprovedProjects = append(out.ApprovedProjects, project)
				}
				out.ApprovedProjects = append(out.ApprovedProjects, h.grants.approved(key, active.ID)...)
			}
			out.AllowedClaims = append([]string(nil), p.AllowedClaims...)
			out.AnyProjectFile = p.AnyProjectFile
			out.Assistant = p.Assistant
			out.PermissionMode = p.PermissionMode
			out.TimeoutMinutes = p.TimeoutMinutes
			out.SharedSettings = p.Shared
			out.DispatchAgents = p.Agents
		}
	}
	return out, nil
}

// Called at composition time, never while the host broker is being rebound by
// another owner. Once installed, disable/enable reuses these exact objects.
func (h *Host) connect() error {
	if h.bridge != nil {
		if h.o.Broker.AssistantAuthority != h.authority || h.o.Broker.AssistantWorkChanged == nil || h.o.Broker.AssistantWorkCompleted == nil {
			return failure(pa.ScopeDenied, "broker_owner")
		}
		return nil
	}
	if h.o.Broker.AssistantAuthority != nil || h.o.Broker.AssistantWorkChanged != nil || h.o.Broker.AssistantWorkCompleted != nil {
		return failure(pa.ScopeDenied, "broker_owner")
	}
	a, err := orchestrator.NewAssistantAuthority(orchestrator.AssistantAuthorityOptions{Current: h.current, ResolveProject: h.o.Projects.Resolve, Clock: h.o.Clock})
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
	h.o.Broker.AssistantWorkCompleted = h.completeWork
	h.o.Broker.BotRequestNotice = h.botRequestNotice
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
	recoverPermissions := h.permissionRecovery
	tickDone := h.routineDone
	h.mu.Unlock()
	if tickDone != nil {
		select {
		case <-tickDone:
			h.mu.Lock()
			h.routineDone = nil
			h.routineStop = nil
			h.mu.Unlock()
		default:
			return failure(pa.ExecutionEffectUnknown, "routine_tick_cleanup")
		}
	}
	if r != nil {
		u := r.Usage()
		if !u.Known || u.ActiveActors != 0 || u.Processes != 0 {
			return failure(pa.ExecutionEffectUnknown, "owned_cleanup")
		}
	}
	// Establish the read barrier before enabling any automatic restart work.
	if err := h.nativeHistoryReady(ctx); err != nil {
		return err
	}
	if pending || recoverPermissions {
		cleanup, cancel := context.WithTimeout(ctx, h.o.Limits.CleanupTimeout)
		var err error
		if pending {
			err = h.quiesce(cleanup)
		} else {
			err = h.recoverPermissionWaits(cleanup)
		}
		cancel()
		if err != nil {
			return errors.Join(failure(pa.ExecutionEffectUnknown, "durable_cleanup"), err)
		}
		h.mu.Lock()
		h.cleanupPending = false
		h.permissionRecovery = false
		h.mu.Unlock()
	}
	if err := app.InstallSchemas(ctx, h.o.Database, featurestore.Installer{}, featurestore.GroupInstaller{}); err != nil {
		return err
	}
	if err := serverstore.InstallCatalog(ctx, h.o.Database); err != nil {
		return err
	}
	if err := serverstore.InstallRestarts(ctx, h.o.Database); err != nil {
		return err
	}
	if err := h.makeServers(ctx); err != nil {
		return err
	}
	h.mu.Lock()
	if err := h.makeService(); err != nil {
		h.mu.Unlock()
		return err
	}
	if err := h.connect(); err != nil {
		h.mu.Unlock()
		return err
	}
	// Only a disabled, proven clean Engine can be replaced.
	if !h.enabled {
		h.engine = nil
	}
	h.enabled = true
	registry, boot := h.serverRegistry, h.serverBoot
	h.mu.Unlock()
	// Restoration needs current background authority, which is unavailable
	// until enabled. The transition lock still excludes Disable and Close.
	if err := registry.ResumeClean(ctx, boot); err != nil {
		h.mu.Lock()
		h.enabled = false
		h.mu.Unlock()
		return err
	}
	if err := h.ensureLead(ctx); err != nil {
		log.Printf("bots: lead setup did not complete: %s", pa.Code(err))
	}
	if !h.o.DeferBackgroundStart {
		h.StartBackgroundWork()
	}
	return nil
}

func (h *Host) StartBackgroundWork() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled || h.routineStop != nil {
		return
	}
	tickCtx, stop := context.WithCancel(context.Background())
	h.routineStop = stop
	h.routineDone = make(chan struct{})
	go h.routineTick(tickCtx, h.routineDone)
	go func() {
		h.resumeInterrupted(tickCtx)
		h.reconcileSettledWork(tickCtx)
	}()
}

// resumeInterrupted recovers eligible turns and wakes senders with durable
// Bot replies once the host is enabled.
func (h *Host) resumeInterrupted(parent context.Context) {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, h.o.Limits.CleanupTimeout*4)
	defer cancel()
	var keys, replySenders []pa.BotKey
	query := pa.ListBots{MachineID: h.o.MachineID, PageSize: pa.MaxPageRows}
	for {
		page, err := st.List(ctx, query)
		if err != nil {
			return
		}
		for _, bot := range page.Items {
			hasReply, err := st.HasPendingExchangeReply(ctx, bot.Bot.Key)
			if err == nil && hasReply {
				replySenders = append(replySenders, bot.Bot.Key)
			}
			pending, err := st.PendingTurns(ctx, bot.Bot.Key)
			if err != nil {
				continue
			}
			for _, turn := range pending {
				if turn.State == pa.TurnRunning || turn.State == pa.TurnWaitingPermission || turn.Steering ||
					turn.State == pa.TurnUnknown || pa.IsLeadCallTurn(turn) ||
					pa.QueuedChatRestartWork(turn) && turn.Fence.BindingID != "" {
					keys = append(keys, bot.Bot.Key)
					break
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	if len(keys) == 0 && len(replySenders) == 0 {
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
		if err := r.Recover(ctx, key); err != nil {
			log.Printf("bots: could not recover chat work: %v", err)
		}
	}
	for _, key := range replySenders {
		r.ResumeExchangeReplies(key)
	}
}

// Repair already settled broker records missed by an older projection. This
// refreshes WorkLinks without scheduling a result review or performing a landing.
func (h *Host) reconcileSettledWork(parent context.Context) {
	h.mu.Lock()
	st, bridge := h.store, h.bridge
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, h.o.Limits.CleanupTimeout)
	defer cancel()
	after := ""
	for {
		ids, err := st.SettledWorkRepairs(ctx, h.o.MachineID, after)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("bots: cannot read settled work for reconciliation: %v", err)
			}
			return
		}
		for _, id := range ids {
			record, _, err := h.o.Broker.Record(ctx, id)
			if err == nil {
				err = bridge.Sync(ctx, orchestrator.AssistantWorkSnapshotOf(record))
			}
			if err != nil && ctx.Err() == nil {
				log.Printf("bots: cannot reconcile settled work %s: %s", id, pa.Code(err))
			}
			after = id
		}
		if len(ids) < pa.MaxPageRows {
			return
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
	serverRegistry := h.serverRegistry
	serverBoot := h.serverBoot
	rooms := h.rooms
	stop, done := h.routineStop, h.routineDone
	h.cleanupPending = h.store != nil
	h.mu.Unlock()
	var result error
	if extensions, ok := h.o.Extensions.(interface{ Suspend(context.Context) error }); ok {
		result = errors.Join(result, extensions.Suspend(ctx))
	}
	if h.profiles != nil {
		if closeHost {
			result = errors.Join(result, h.profiles.Close(ctx))
		} else {
			result = errors.Join(result, h.profiles.Suspend(ctx))
		}
	}
	if rooms != nil {
		rooms.CancelRooms()
	}
	if stop != nil {
		stop()
		select {
		case <-done:
			h.mu.Lock()
			h.routineStop = nil
			h.routineDone = nil
			h.mu.Unlock()
		case <-ctx.Done():
			result = failure(pa.ExecutionEffectUnknown, "routine_tick_cleanup")
		}
	}
	if serverRegistry != nil {
		if closeHost {
			result = errors.Join(result, serverRegistry.CloseForRestart(ctx, serverBoot))
		} else {
			result = errors.Join(result, serverRegistry.Close(ctx))
		}
	}
	if r != nil {
		if closeHost {
			result = errors.Join(result, r.CloseForRestart(ctx))
		} else {
			result = errors.Join(result, r.Close(ctx))
		}
	}
	if rooms != nil {
		rooms.Idle()
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
	h.permissionRecovery = false
	h.mu.Unlock()
	return nil
}

// One page at a time under the caller's SINGLE total cleanup deadline. There
// is no provider, actor allocation, retained cursor registry or background loop.
func (h *Host) quiesce(ctx context.Context) error { return h.quiesceFor(ctx, "execution") }

func (h *Host) quiesceFor(ctx context.Context, field string) error {
	return h.visitBotHistory(ctx, func(st *featurestore.Store, bot pa.BotSummary) error {
		return app.QuiesceHistoryFor(ctx, st, h.o.Clock, h.o.ID, bot.Bot.Key, field)
	})
}

func (h *Host) recoverPermissionWaits(ctx context.Context) error {
	return h.visitBotHistory(ctx, func(st *featurestore.Store, bot pa.BotSummary) error {
		if bot.PendingPermissions == 0 && bot.Status != "needsInput" {
			return nil
		}
		return app.RecoverPermissionWaits(ctx, st, h.o.Clock, h.o.ID, bot.Bot.Key)
	})
}

func (h *Host) visitBotHistory(ctx context.Context, visit func(*featurestore.Store, pa.BotSummary) error) error {
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
			if err = visit(st, bot); err != nil {
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
	r, err := app.NewRuntime(app.RuntimeOptions{NetworkCheck: h.checkNetworkTurn, NativeHistory: h.o.NativeHistory, Repository: h.store, Memory: h.store, WorkBridge: h.bridge, ProjectResolver: h.o.Projects, LeadQuotaNear: h.o.LeadQuotaNear, Provider: &ownedProvider{h: h, p: p}, Clock: h.o.Clock, ID: h.o.ID, Limits: h.o.Limits, Rooms: h.rooms, Attachments: h.o.Attachments, Inputs: h.o.Inputs, SharedMemory: h.o.SharedMemory, Authorize: func(ctx context.Context, key pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
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
func (f runtimeFacade) EditQueue(ctx context.Context, q app.QueueEditRequest, check func(context.Context) error) (pa.Receipt, error) {
	r, err := f.h.runtime()
	if err != nil {
		return pa.Receipt{}, err
	}
	return r.EditQueue(ctx, q, check)
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

// The enabled Host owns one tick and stops it before actor teardown. It never
// constructs a native provider when there are no due or pending definitions.
func (h *Host) routineTick(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Duration(pa.MaxRoutineTickMilliseconds) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.tickBackgroundTasks(ctx)
			h.mu.Lock()
			serverRegistry := h.serverRegistry
			h.mu.Unlock()
			if serverRegistry != nil {
				check, done := context.WithTimeout(ctx, h.o.Limits.CleanupTimeout)
				h.logReconcile(serverRegistry.Reconcile(check, h.serverCheck))
				done()
			}
			query := pa.ListBots{MachineID: h.o.MachineID, PageSize: pa.MaxPageRows}
			for {
				page, err := h.store.List(ctx, query)
				if err != nil {
					break
				}
				for _, b := range page.Items {
					defs, runs, e := h.store.Routines(ctx, b.Bot.Key)
					if e != nil {
						continue
					}
					needed := false
					for _, d := range defs {
						if d.Enabled && !d.Deleted && !d.NextDue.IsZero() && !h.o.Clock().Before(d.NextDue) {
							needed = true
						}
					}
					for _, run := range runs {
						if run.State == pa.TurnQueued {
							needed = true
						}
					}
					if needed {
						r, e := h.runtime()
						if e == nil {
							_ = r.TickRoutines(ctx, b.Bot.Key)
						}
					}
				}
				if page.NextCursor == "" {
					break
				}
				query.Cursor = page.NextCursor
			}
		}
	}
}

// AgentRests reads the machine-owned default identities without starting a provider.
func (h *Host) AgentRests(ctx context.Context) ([]pa.AgentRest, error) {
	rests := []pa.AgentRest{}
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		rest, err := h.store.AgentRest(ctx, h.o.MachineID, agent)
		if err != nil {
			return nil, err
		}
		if rest != nil {
			rests = append(rests, *rest)
		}
	}
	return rests, nil
}

func (f runtimeFacade) TeamCoordinator() *app.TeamCoordinator {
	f.h.mu.Lock()
	defer f.h.mu.Unlock()
	return f.h.team
}
func (f runtimeFacade) BusyElsewhere(ctx context.Context, member, group pa.BotKey) (bool, error) {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return false, nil
	}
	return r.BusyElsewhere(ctx, member, group)
}
func (f runtimeFacade) CancelRoomTurns(ctx context.Context, group, member pa.BotKey, active bool) error {
	f.h.mu.Lock()
	r := f.h.engine
	f.h.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.CancelRoomTurns(ctx, group, member, active)
}

// logReconcile writes a Reconcile failure once per distinct failure, and once
// when it clears. The codes name no command, folder or Project.
func (h *Host) logReconcile(err error) {
	text := ""
	if err != nil {
		text = string(pa.Code(err))
		if text == "" {
			text = "unclassified"
		}
	}
	h.mu.Lock()
	changed := text != h.serverReconcileLogged
	h.serverReconcileLogged = text
	h.mu.Unlock()
	if !changed {
		return
	}
	if text == "" {
		log.Printf("bots: dev server authority checks succeed again")
		return
	}
	log.Printf("bots: dev server authority check failed: %s", text)
}
