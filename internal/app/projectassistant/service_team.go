package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
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
	Fence  pa.Fence
	TurnID string
	Under  TeamManagerGrant
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
	Check      func(context.Context, pa.Fence, string) (ManagerContext, error)
	Clock      func() time.Time
	ID         func() string
}

type TeamPlan struct {
	ID          string
	Manager     pa.BotKey
	ManagerName string
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

// TeamMember is setup data from the same full Project, hidden Bots included.
// It intentionally carries no memory, messages, native identity or path.
type TeamMember struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Instructions string        `json:"instructions"`
	Agent        pa.Agent      `json:"agent"`
	Appearance   pa.Appearance `json:"appearance"`
	Hidden       bool          `json:"hidden"`
	ManageTeam   bool          `json:"manageTeam"`
	CreatedBy    *pa.BotKey    `json:"createdBy,omitempty"`
}

func (t *TeamCoordinator) members(ctx context.Context, scope pa.ProjectScope) ([]pa.Bot, error) {
	all, err := t.machineBots(ctx, scope.MachineID)
	if err != nil {
		return nil, err
	}
	out := []pa.Bot{}
	for _, b := range all {
		if b.Project == scope {
			out = append(out, b)
		}
	}
	return out, nil
}
func (t *TeamCoordinator) machineBots(ctx context.Context, machine string) ([]pa.Bot, error) {
	all := []pa.Bot{}
	cursor := ""
	for {
		page, err := t.o.Repository.List(ctx, pa.ListBots{MachineID: machine, Cursor: cursor, PageSize: pa.MaxPageRows})
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

// Manager tool names and argument fields follow CodyNC. The local host
// additionally requires an idempotency key and refuses authority fields.
func (t *TeamCoordinator) Call(ctx context.Context, fence pa.Fence, turn, action string, arguments json.RawMessage) (any, error) {
	m, err := t.o.Check(ctx, fence, turn)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(arguments, &fields) != nil || fields == nil {
		return nil, serviceFailure(pa.Invalid, "manager_arguments")
	}
	if action == "list_team" {
		if len(fields) != 0 {
			return nil, serviceFailure(pa.Invalid, "manager_arguments")
		}
		bots, err := t.members(ctx, m.Project)
		if err != nil {
			return nil, err
		}
		out := []TeamMember{}
		for _, b := range bots {
			out = append(out, TeamMember{ID: b.Key.BotID, Name: b.Name, Instructions: b.Instructions.Text, Agent: b.Agent, Appearance: b.Appearance, Hidden: b.Hidden, ManageTeam: b.CanManageTeam(), CreatedBy: b.CreatedBy})
		}
		if _, err = t.o.Check(ctx, fence, turn); err != nil {
			return nil, err
		}
		return map[string]any{"bots": out}, nil
	}
	if action != "create_bot" && action != "update_bot" {
		return nil, serviceFailure(pa.Unsupported, "manager_tool")
	}
	var id string
	if json.Unmarshal(fields["idempotency_key"], &id) != nil || strings.TrimSpace(id) == "" {
		return nil, serviceFailure(pa.Invalid, "idempotency_key")
	}
	delete(fields, "idempotency_key")
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	if err = CheckManagerFields(keys); err != nil {
		return nil, err
	}
	text := func(key string) (*string, error) {
		raw, exists := fields[key]
		if !exists {
			return nil, nil
		}
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
			return nil, serviceFailure(pa.Invalid, "manager_"+key)
		}
		return &value, nil
	}
	name, err := text("name")
	if err != nil {
		return nil, err
	}
	instructions, err := text("instructions")
	if err != nil {
		return nil, err
	}
	color, err := text("avatarColor")
	if err != nil {
		return nil, err
	}
	shape, err := text("avatarShape")
	if err != nil {
		return nil, err
	}
	botID, err := text("botId")
	if err != nil {
		return nil, err
	}
	var hidden *bool
	if raw, ok := fields["hidden"]; ok {
		var v bool
		if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
			return nil, serviceFailure(pa.Invalid, "manager_hidden")
		}
		hidden = &v
	}
	encoded, _ := json.Marshal(struct {
		Fence            pa.Fence
		Turn, ID, Action string
	}{fence, turn, id, action})
	hash := sha256.Sum256(encoded)
	operationID := "team-" + hex.EncodeToString(hash[:])
	key := pa.BotKey{MachineID: fence.Key.MachineID, BotID: operationID}
	operation := pa.CreateOperation
	if action == "update_bot" {
		operation = pa.EditOperation
		if botID == nil || strings.TrimSpace(*botID) == "" {
			return nil, serviceFailure(pa.Invalid, "manager_bot")
		}
		key.BotID = *botID
	} else if botID != nil {
		return nil, serviceFailure(pa.Invalid, "manager_bot")
	}
	cmd, err := canonicalCommand(pa.Command{Key: key, Operation: operation, IdempotencyKey: operationID}, operation, "manager:"+fence.Key.BotID, fields)
	if err != nil {
		return nil, err
	}
	// A previously committed call returns its receipt's Bot; no new card or
	// target is created after an uncertain transport response.
	if prior, err := t.o.Repository.Receipt(ctx, cmd); err != nil {
		return nil, err
	} else if prior != nil {
		if _, err = pa.DecideReplay(cmd, prior); err != nil {
			return nil, err
		}
		return t.o.Repository.Bot(ctx, key)
	}
	now := t.o.Clock().UTC()
	var next pa.Bot
	var conversation *pa.Conversation
	var expected int64
	if action == "create_bot" {
		if name == nil {
			return nil, serviceFailure(pa.Invalid, "name")
		}
		look := pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}
		if color != nil {
			look.Color = *color
		}
		if shape != nil {
			look.Shape = pa.Shape(*shape)
		}
		instruction := ""
		if instructions != nil {
			instruction = *instructions
		}
		bots, err := t.machineBots(ctx, m.Project.MachineID)
		if err != nil {
			return nil, err
		}
		created := 0
		for _, b := range bots {
			if b.CreatedBy != nil {
				created++
			}
		}
		plan, err := PlanManagedCreate(m, ManagedCreate{Name: *name, Instructions: instruction, Appearance: look}, created, DefaultManagerLimits())
		if err != nil {
			return nil, err
		}
		next = pa.Bot{Key: key, Project: plan.Project, ConversationID: t.o.ID(), Name: plan.Name, Appearance: plan.Appearance, Agent: plan.Agent, PermissionPolicy: plan.PermissionPolicy, Mode: plan.Mode, ManageTeam: plan.ManageTeam, CreatedBy: &plan.CreatedBy, Instructions: pa.InstructionSnapshot{Revision: 1, Text: plan.Instructions}, Version: 1, CreatedAt: now, UpdatedAt: now}
		if hidden != nil {
			next.Hidden = *hidden
		}
		c := pa.Conversation{Key: key, ID: next.ConversationID, MemoryRevision: 1, Version: 1}
		conversation = &c
		if _, err = pa.ApplyEdit(next, pa.EditBot{ExpectedVersion: next.Version, Name: next.Name, Appearance: next.Appearance, Instructions: next.Instructions.Text}); err != nil {
			return nil, err
		}
	} else {
		target, err := t.o.Repository.Bot(ctx, key)
		if err != nil {
			return nil, err
		}
		var look *pa.Appearance
		if color != nil || shape != nil {
			value := target.Appearance
			if color != nil {
				value.Color = *color
			}
			if shape != nil {
				value.Shape = pa.Shape(*shape)
			}
			look = &value
		}
		edit := ManagedEdit{Target: key, Name: name, Instructions: instructions, Appearance: look, Hidden: hidden}
		if err = CheckManagedEdit(m, target, edit); err != nil {
			return nil, err
		}
		settings := pa.EditBot{ExpectedVersion: target.Version, Name: target.Name, Instructions: target.Instructions.Text, Appearance: target.Appearance}
		if name != nil {
			settings.Name = *name
		}
		if instructions != nil {
			settings.Instructions = *instructions
		}
		if look != nil {
			settings.Appearance = *look
		}
		next, err = pa.ApplyEdit(target, settings)
		if err != nil {
			return nil, err
		}
		if hidden != nil {
			next.Hidden = *hidden
		}
		next.UpdatedAt = now
		expected = target.Version
	}
	// Display the entire resulting effect, including all inherited authority.
	// UTF-16 units match CodyNC and JavaScript length, including astral text.
	effect := map[string]any{"operation": operation, "project": next.Project, "botId": next.Key.BotID}
	if operation == pa.CreateOperation {
		effect["settings"] = map[string]any{"name": next.Name, "instructions": next.Instructions.Text, "appearance": next.Appearance, "hidden": next.Hidden, "agent": next.Agent, "permission_policy": next.PermissionPolicy, "mode": next.Mode, "manageTeam": next.ManageTeam, "createdBy": next.CreatedBy}
	} else {
		effect["changes"] = fields
	}
	detail, err := json.MarshalIndent(effect, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(utf16.Encode([]rune(string(detail)))) > capacity.MaximumBotManagerCardUnits {
		return nil, serviceFailure(pa.CapacityFull, "manager_card_detail")
	}
	mutation := Mutation{Command: cmd, ExpectedVersion: expected, Bot: &next, Conversation: conversation, Receipt: pa.Receipt{ID: t.o.ID(), Command: cmd, State: pa.EffectAccepted, ResourceID: key.BotID}}
	kind := pa.BotUpdated
	if operation == pa.CreateOperation {
		kind = pa.BotCreated
	}
	summary := pa.BotSummary{Bot: next, Version: next.Version, ActivityAt: now}
	mutation.Events = []pa.Event{{Version: pa.ContractVersion, ID: t.o.ID(), Fence: pa.Fence{Key: key, Project: next.Project, ConversationID: next.ConversationID}, Kind: kind, At: now, Bot: &summary}}
	// The durable operation key stays stable, while each approval attempt has
	// a new capability ID. An old expired/revoked card cannot approve a retry
	// made under a new grant, even with the same native idempotency key.
	planID := t.o.ID()
	planContext, cancelPlan := context.WithCancel(ctx)
	defer cancelPlan()
	pending := &teamPending{plan: TeamPlan{ID: planID, Manager: fence.Key, ManagerName: m.Caller.Name, Target: key, Operation: operation, Detail: string(detail), ExpiresAt: now.Add(time.Duration(capacity.MaximumBotManagerApprovalSeconds) * time.Second)}, execution: ManagerExecution{Fence: fence, TurnID: turn, Under: m.Grant}, mutation: mutation, context: planContext, done: make(chan teamOutcome, 1)}
	t.mu.Lock()
	if planID == "" || t.pending[planID] != nil {
		t.mu.Unlock()
		return nil, serviceFailure(pa.Invalid, "manager_plan_id")
	}
	for _, old := range t.pending {
		if old.mutation.Command.IdempotencyKey == operationID {
			t.mu.Unlock()
			if old.mutation.Command.ContentHash != cmd.ContentHash {
				return nil, serviceFailure(pa.IdempotencyConflict, "manager_plan")
			}
			return nil, serviceFailure(pa.OutcomeUnknown, "manager_plan_pending")
		}
		if old.plan.Manager == fence.Key {
			t.mu.Unlock()
			return nil, serviceFailure(pa.CapacityFull, "manager_pending")
		}
	}
	if len(t.pending) >= capacity.MaximumBotManagerPending {
		t.mu.Unlock()
		return nil, serviceFailure(pa.CapacityFull, "manager_pending")
	}
	t.pending[planID] = pending
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.pending, planID); t.mu.Unlock() }()
	timer := time.NewTimer(time.Duration(capacity.MaximumBotManagerApprovalSeconds) * time.Second)
	defer timer.Stop()
	abandon := func(code pa.ErrorCode) error {
		t.mu.Lock()
		cancelPlan()
		applying := pending.applying
		t.mu.Unlock()
		if applying {
			return serviceFailure(pa.OutcomeUnknown, "manager_mutation")
		}
		return serviceFailure(code, "manager_approval")
	}
	select {
	case outcome := <-pending.done:
		return outcome.bot, outcome.err
	case <-ctx.Done():
		return nil, abandon(pa.Revoked)
	case <-timer.C:
		return nil, abandon(pa.Expired)
	}
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
	return t.Decide(ctx, cmd.Key, id, allow, a.Source, func(ctx context.Context) error { _, err := s.authorizeEffect(ctx, cmd.Key, true, a); return err })
}
