package projectassistant

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// TeamTools is an execution-bound optional MCP capability. Available must be
// checked both when listing tools and for every invocation; advertising a tool
// never grants authority to use it later.
type TeamTools interface {
	TeamAvailable(context.Context) error
	Team(context.Context, string, json.RawMessage) (any, error)
}

// ManagerExecution is host-derived, never decoded from tool arguments.
type ManagerExecution struct {
	Fence                 pa.Fence
	TurnID                string
	Under                 TeamManagerGrant
	TargetSettingsVersion int64
	CreationProject       pa.ProjectScope
}

// ManagedRepository validates the manager's current grant, live execution,
// Project, target policy and creation count in the same transaction as the
// target mutation. A check followed by ordinary Commit would race revocation.
type ManagedRepository interface {
	Repository
	CommitManaged(context.Context, ManagerExecution, Mutation, int) (pa.Receipt, error)
}

// TeamRuntime is the host's private composition seam, not a wire capability.
type TeamRuntime interface{ TeamCoordinator() *TeamCoordinator }

type TeamCoordinatorOptions struct {
	Repository ManagedRepository
	Projects   ProjectResolver
	Check      func(context.Context, pa.Fence, string) (ManagerContext, error)
	Clock      func() time.Time
	ID         func() string
	Review     func(context.Context, TeamPlan, ManagerExecution, func(context.Context, bool, pa.SourceStatus) error) error
}

type TeamPlan struct {
	ID          string
	Manager     pa.BotKey
	ManagerName string
	TargetName  string
	Target      pa.BotKey
	Operation   pa.Operation
	Detail      string
	ExpiresAt   time.Time
}
type teamOutcome struct {
	bot pa.Bot
	err error
}
type teamPending struct {
	plan      TeamPlan
	execution ManagerExecution
	mutation  Mutation
	folder    string
	directory string
	context   context.Context
	done      chan teamOutcome
	deciding  bool
	applying  bool
}

// One bounded, short-lived approval pool belongs to one Host. Nothing is
// resurrected on restart: without the live owned MCP call a card cannot act.
type TeamCoordinator struct {
	o       TeamCoordinatorOptions
	mu      sync.Mutex
	pending map[string]*teamPending
}

func NewTeamCoordinator(o TeamCoordinatorOptions) (*TeamCoordinator, error) {
	if o.Repository == nil || o.Check == nil || o.Clock == nil || o.ID == nil {
		return nil, errors.New("team management requires a managed repository, execution check, clock and IDs")
	}
	return &TeamCoordinator{o: o, pending: map[string]*teamPending{}}, nil
}

// ManagerContext validates the real running turn and its binding. Neither a
// peer Bot's review request nor a stale native session may manage the team.
func (r *Engine) ManagerContext(ctx context.Context, fence pa.Fence, turnID string) (ManagerContext, error) {
	bot, err := r.o.Repository.Bot(ctx, fence.Key)
	if err != nil {
		return ManagerContext{}, err
	}
	if err = r.authorized(ctx, bot.Key, bot.Project); err != nil {
		return ManagerContext{}, err
	}
	c, err := r.o.Repository.Conversation(ctx, bot.Key, bot.ConversationID)
	if err != nil {
		return ManagerContext{}, err
	}
	t, err := r.o.Repository.Turn(ctx, bot.Key, turnID)
	if err != nil {
		return ManagerContext{}, err
	}
	b, err := r.o.Repository.Binding(ctx, bot.Key, fence.BindingID)
	if err != nil {
		return ManagerContext{}, err
	}
	if b.Revoked {
		return ManagerContext{}, serviceFailure(pa.Revoked, "manager_binding")
	}
	if currentFence(bot, c) != fence || t.Fence != fence || c.ActiveTurnID != turnID || t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
		return ManagerContext{}, serviceFailure(pa.StaleGeneration, "manager_turn")
	}
	if pa.IsPeerRequest(t.Origin) {
		return ManagerContext{}, serviceFailure(pa.ScopeDenied, "bot_request_tools")
	}
	if t.Room != nil {
		if err = r.admitRoomTurn(ctx, *t.Room); err != nil {
			return ManagerContext{}, err
		}
	}
	m := ManagerContext{Caller: bot, Grant: GrantFor(bot), Project: bot.Project}
	return m, m.check()
}

func (t *TeamCoordinator) Available(ctx context.Context, fence pa.Fence, turn string) error {
	_, err := t.o.Check(ctx, fence, turn)
	return err
}

// TeamMember describes a Bot's authorized setup, including hidden entries.
// A Lead also sees the registered folder when it can be resolved.
type TeamMember struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Project      pa.ProjectScope `json:"project"`
	Folder       string          `json:"folder,omitempty"`
	Instructions string          `json:"instructions"`
	Agent        pa.Agent        `json:"agent"`
	Appearance   pa.Appearance   `json:"appearance"`
	Hidden       bool            `json:"hidden"`
	ManageTeam   bool            `json:"manageTeam"`
	CreatedBy    *pa.BotKey      `json:"createdBy,omitempty"`
}

// Only resolve a registered folder for display if it still denotes this
// exact Project. Editing saved setup does not need a working directory.
func (t *TeamCoordinator) projectFolder(ctx context.Context, scope pa.ProjectScope) string {
	if t.o.Projects == nil || scope.Kind == pa.PersonalWorkspace {
		return ""
	}
	resolved, err := t.o.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: scope.MachineID, ProjectID: scope.ProjectID})
	if err != nil || resolved.Scope != scope {
		return ""
	}
	return resolved.Directory
}

func (t *TeamCoordinator) managedTarget(ctx context.Context, m ManagerContext, key pa.BotKey) (pa.Bot, error) {
	if repo, ok := t.o.Repository.(backgroundOwnership); ok {
		owner, err := repo.BackgroundOwner(ctx, key)
		if err != nil {
			return pa.Bot{}, err
		}
		if owner.BotID != "" {
			return pa.Bot{}, serviceFailure(pa.NotFound, "bot")
		}
	}
	target, err := t.o.Repository.Bot(ctx, key)
	if err != nil {
		return pa.Bot{}, err
	}
	if !m.CanManageBotSetup(target) {
		return pa.Bot{}, serviceFailure(pa.ScopeDenied, "manager_target_project")
	}
	return target, nil
}

func (t *TeamCoordinator) machineBots(ctx context.Context, machine, query string) ([]pa.Bot, error) {
	all := []pa.Bot{}
	cursor := ""
	for {
		page, err := t.o.Repository.List(ctx, pa.ListBots{MachineID: machine, Query: query, Cursor: cursor, PageSize: pa.MaxPageRows})
		if err != nil {
			return nil, err
		}
		for _, b := range page.Items {
			all = append(all, b.Bot)
		}
		if page.NextCursor == "" {
			return all, nil
		}
		if page.NextCursor == cursor {
			return nil, serviceFailure(pa.CursorExpired, "team")
		}
		cursor = page.NextCursor
	}
}

// Resolve an explicit destination through the source machine's registered
// Projects. A model cannot mint a scope or borrow another Bot's workspace.
func (t *TeamCoordinator) resolveFolder(ctx context.Context, machine, folder string) (pa.ProjectResolution, error) {
	if strings.TrimSpace(folder) == "" {
		return pa.ProjectResolution{}, serviceFailure(pa.Invalid, "manager_folder")
	}
	projects, ok := t.o.Projects.(TeamProjectResolver)
	if !ok {
		return pa.ProjectResolution{}, serviceFailure(pa.Unsupported, "manager_folder")
	}
	resolved, err := projects.ResolveTeamFolder(ctx, machine, folder)
	if err != nil {
		return pa.ProjectResolution{}, err
	}
	if resolved.Scope.Kind == pa.PersonalWorkspace {
		return pa.ProjectResolution{}, serviceFailure(pa.ScopeDenied, "manager_folder")
	}
	return resolved, pa.ValidateTarget(pa.ProjectSelection{MachineID: machine, ProjectID: resolved.Scope.ProjectID}, resolved)
}

func (t *TeamCoordinator) Plans(ctx context.Context, key pa.BotKey) ([]TeamPlan, error) {
	t.mu.Lock()
	candidates := []*teamPending{}
	for _, p := range t.pending {
		if p.plan.Manager != key || p.deciding || p.context.Err() != nil || !t.o.Clock().Before(p.plan.ExpiresAt) {
			continue
		}
		candidates = append(candidates, p)
	}
	t.mu.Unlock()
	out := []TeamPlan{}
	for _, p := range candidates {
		m, err := t.o.Check(ctx, p.execution.Fence, p.execution.TurnID)
		if err != nil || ConfirmManagerPlan(p.execution.Under, m) != nil {
			continue
		}
		if t.o.Review != nil {
			card, err := t.o.Repository.Permission(ctx, key, p.plan.ID)
			if err != nil || card.State != pa.PermissionPending {
				continue
			}
		}
		out = append(out, p.plan)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *TeamCoordinator) Decide(ctx context.Context, key pa.BotKey, id string, allow bool, source pa.SourceStatus, check func(context.Context) error) (string, error) {
	t.mu.Lock()
	p := t.pending[id]
	if p == nil || p.plan.Manager != key {
		t.mu.Unlock()
		return "", serviceFailure(pa.NotFound, "manager_plan")
	}
	if p.deciding {
		t.mu.Unlock()
		return "", serviceFailure(pa.OutcomeUnknown, "manager_plan")
	}
	p.deciding = true
	t.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.context, cancel)
	defer func() { stop(); cancel() }()
	finish := func(out teamOutcome) { p.done <- out }
	if p.context.Err() != nil || !t.o.Clock().Before(p.plan.ExpiresAt) {
		err := serviceFailure(pa.Expired, "manager_approval")
		finish(teamOutcome{err: err})
		return "", err
	}
	m, err := t.o.Check(ctx, p.execution.Fence, p.execution.TurnID)
	if err == nil {
		err = ConfirmManagerPlan(p.execution.Under, m)
	}
	if err == nil {
		err = check(ctx)
	}
	if err != nil {
		finish(teamOutcome{err: err})
		return "", err
	}
	if !allow {
		finish(teamOutcome{err: serviceFailure(pa.ScopeDenied, "manager_declined")})
		return "declined", nil
	}
	if p.folder != "" {
		resolved, err := t.resolveFolder(ctx, key.MachineID, p.folder)
		if err == nil && (resolved.Scope != p.execution.CreationProject || resolved.Directory != p.directory) {
			err = serviceFailure(pa.StaleVersion, "manager_folder")
		}
		if err != nil {
			finish(teamOutcome{err: err})
			return "", err
		}
	}
	t.mu.Lock()
	if p.context.Err() != nil || !t.o.Clock().Before(p.plan.ExpiresAt) {
		t.mu.Unlock()
		err := serviceFailure(pa.Expired, "manager_approval")
		finish(teamOutcome{err: err})
		return "", err
	}
	p.applying = true
	t.mu.Unlock()
	for i := range p.mutation.Events {
		if p.mutation.Events[i].Bot != nil {
			p.mutation.Events[i].Bot.Source = source
		}
	}
	// The repository repeats the grant/fence/count/target checks atomically.
	_, err = t.o.Repository.CommitManaged(ctx, p.execution, p.mutation, DefaultManagerLimits().MaxCreated)
	if err != nil {
		finish(teamOutcome{err: err})
		return "", err
	}
	bot, err := t.o.Repository.Bot(ctx, p.plan.Target)
	finish(teamOutcome{bot: bot, err: err})
	return "applied", err
}

func (s *Service) team() (*TeamCoordinator, error) {
	r, ok := s.opts.Runtime.(TeamRuntime)
	if !ok || r.TeamCoordinator() == nil {
		return nil, serviceFailure(pa.Unsupported, "manage_team")
	}
	return r.TeamCoordinator(), nil
}
func (s *Service) TeamPlans(ctx context.Context, key pa.BotKey) ([]TeamPlan, error) {
	if _, err := s.authorize(ctx, key, false); err != nil {
		return nil, err
	}
	t, err := s.team()
	if err != nil {
		return nil, err
	}
	return t.Plans(ctx, key)
}
func (s *Service) AnswerTeamPlan(ctx context.Context, cmd pa.Command, id string, allow bool) (string, error) {
	a, err := s.authorize(ctx, cmd.Key, true)
	if err != nil {
		return "", err
	}
	if cmd.Operation != pa.AnswerOperation || cmd.IdempotencyKey == "" || id == "" {
		return "", serviceFailure(pa.Invalid, "manager_answer")
	}
	t, err := s.team()
	if err != nil {
		return "", err
	}
	if t.o.Review != nil {
		card, err := t.o.Repository.Permission(ctx, cmd.Key, id)
		if err != nil || !strings.HasPrefix(id, sourcePermissionPrefix) || card.Action == nil || !pa.TeamPermissionKind(card.Action.ToolKind) {
			return "", serviceFailure(pa.NotFound, "manager_plan")
		}
		option := teamReviewDeny
		state := "declined"
		if allow {
			option, state = teamReviewAllow, "applied"
		}
		_, err = s.Answer(ctx, cmd, pa.PermissionAnswer{Fence: card.Fence, TurnID: card.TurnID, OfferID: id, OptionID: option})
		return state, err
	}
	return t.Decide(ctx, cmd.Key, id, allow, a.Source, func(ctx context.Context) error { _, err := s.authorizeEffect(ctx, cmd.Key, true, a); return err })
}
