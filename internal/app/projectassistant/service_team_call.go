package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// managerCall is one create_bot or update_bot call as it is parsed, planned
// and offered for approval.
type managerCall struct {
	fence  pa.Fence
	turn   string
	action string
	m      ManagerContext
	fields map[string]json.RawMessage

	id                                              string
	name, instructions, color, shape, folder, botID *string
	hidden                                          *bool
	key                                             pa.BotKey
	operation                                       pa.Operation
	operationID                                     string
	cmd                                             pa.Command
	now                                             time.Time
	next                                            pa.Bot
	conversation                                    *pa.Conversation
	expected, targetSettingsVersion                 int64
	targetName, directory                           string
	creationProject                                 pa.ProjectScope
	detail                                          string
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
		return t.listTeam(ctx, fence, turn, m, fields)
	}
	if action != "create_bot" && action != "update_bot" {
		return nil, serviceFailure(pa.Unsupported, "manager_tool")
	}
	call := &managerCall{fence: fence, turn: turn, action: action, m: m, fields: fields}
	if err = call.parse(); err != nil {
		return nil, err
	}
	if err = call.command(); err != nil {
		return nil, err
	}
	// A previously committed call returns its receipt's Bot; no new card or
	// target is created after an uncertain transport response.
	if prior, err := t.o.Repository.Receipt(ctx, call.cmd); err != nil {
		return nil, err
	} else if prior != nil {
		if _, err = pa.DecideReplay(call.cmd, prior); err != nil {
			return nil, err
		}
		if call.operation == pa.EditOperation {
			return t.managedTarget(ctx, m, call.key)
		}
		return t.o.Repository.Bot(ctx, call.key)
	}
	call.now = t.o.Clock().UTC()
	if err = t.planManagerChange(ctx, call); err != nil {
		return nil, err
	}
	if err = t.describeManagerEffect(ctx, call); err != nil {
		return nil, err
	}
	return t.awaitManagerApproval(ctx, call)
}

// listTeam answers list_team with the Bots this manager may set up.
func (t *TeamCoordinator) listTeam(ctx context.Context, fence pa.Fence, turn string, m ManagerContext, fields map[string]json.RawMessage) (any, error) {
	var query string
	if raw, ok := fields["query"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &query) != nil {
			return nil, serviceFailure(pa.Invalid, "manager_arguments")
		}
		delete(fields, "query")
	}
	if len(fields) != 0 {
		return nil, serviceFailure(pa.Invalid, "manager_arguments")
	}
	bots, err := t.machineBots(ctx, m.Project.MachineID, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	folders := make(map[pa.ProjectScope]string)
	out := []TeamMember{}
	for _, b := range bots {
		if !m.CanManageBotSetup(b) {
			continue
		}
		if _, lookedUp := folders[b.Project]; m.Caller.Lead && !lookedUp {
			folders[b.Project] = t.projectFolder(ctx, b.Project)
		}
		out = append(out, TeamMember{ID: b.Key.BotID, Name: b.Name, Project: b.Project, Folder: folders[b.Project], Instructions: b.Instructions.Text, Agent: b.Agent, Appearance: b.Appearance, Hidden: b.Hidden, ManageTeam: b.CanManageTeam(), CreatedBy: b.CreatedBy})
	}
	current, err := t.o.Check(ctx, fence, turn)
	if err != nil {
		return nil, err
	}
	if err := ConfirmManagerPlan(m.Grant, current); err != nil {
		return nil, err
	}
	return map[string]any{"bots": out}, nil
}

// parse reads the idempotency key and the editable fields, refusing any
// field a manager may not set.
func (call *managerCall) parse() error {
	fields := call.fields
	if json.Unmarshal(fields["idempotency_key"], &call.id) != nil || strings.TrimSpace(call.id) == "" {
		return serviceFailure(pa.Invalid, "idempotency_key")
	}
	delete(fields, "idempotency_key")
	if _, ok := fields["folder"]; ok && call.action != "create_bot" {
		return serviceFailure(pa.ScopeDenied, "manager_field_folder")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	if err := CheckManagerFields(keys); err != nil {
		return err
	}
	for _, f := range []struct {
		key  string
		into **string
	}{{"name", &call.name}, {"instructions", &call.instructions}, {"avatarColor", &call.color}, {"avatarShape", &call.shape}, {"folder", &call.folder}, {"botId", &call.botID}} {
		value, err := managerText(fields, f.key)
		if err != nil {
			return err
		}
		*f.into = value
	}
	if raw, ok := fields["hidden"]; ok {
		var v bool
		if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
			return serviceFailure(pa.Invalid, "manager_hidden")
		}
		call.hidden = &v
	}
	return nil
}

// managerText is a string field of a manager call, or nil when it is absent.
func managerText(fields map[string]json.RawMessage, key string) (*string, error) {
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

// command derives the durable operation key from the manager's turn and
// idempotency key, and the command the change is committed under.
func (call *managerCall) command() error {
	encoded, _ := json.Marshal(struct {
		Fence            pa.Fence
		Turn, ID, Action string
	}{call.fence, call.turn, call.id, call.action})
	hash := sha256.Sum256(encoded)
	call.operationID = "team-" + hex.EncodeToString(hash[:])
	call.key = pa.BotKey{MachineID: call.fence.Key.MachineID, BotID: call.operationID}
	call.operation = pa.CreateOperation
	if call.action == "update_bot" {
		call.operation = pa.EditOperation
		if call.botID == nil || strings.TrimSpace(*call.botID) == "" {
			return serviceFailure(pa.Invalid, "manager_bot")
		}
		call.key.BotID = *call.botID
	} else if call.botID != nil {
		return serviceFailure(pa.Invalid, "manager_bot")
	}
	cmd, err := canonicalCommand(pa.Command{Key: call.key, Operation: call.operation, IdempotencyKey: call.operationID}, call.operation, "manager:"+call.fence.Key.BotID, call.fields)
	call.cmd = cmd
	return err
}

// planManagerChange resolves the destination folder and computes the Bot
// the call would create, or the target as the edit would leave it.
func (t *TeamCoordinator) planManagerChange(ctx context.Context, call *managerCall) error {
	if call.folder != nil {
		resolved, err := t.resolveFolder(ctx, call.m.Project.MachineID, *call.folder)
		if err != nil {
			return err
		}
		call.directory, call.creationProject = resolved.Directory, resolved.Scope
	}
	if call.action == "create_bot" {
		return t.planManagedCreate(ctx, call)
	}
	return t.planManagedEdit(ctx, call)
}

func (t *TeamCoordinator) planManagedCreate(ctx context.Context, call *managerCall) error {
	if call.name == nil {
		return serviceFailure(pa.Invalid, "name")
	}
	look := pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}
	if call.color != nil {
		look.Color = *call.color
	}
	if call.shape != nil {
		look.Shape = pa.Shape(*call.shape)
	}
	instruction := ""
	if call.instructions != nil {
		instruction = *call.instructions
	}
	bots, err := t.machineBots(ctx, call.m.Project.MachineID, "")
	if err != nil {
		return err
	}
	created := 0
	for _, b := range bots {
		if b.CreatedBy != nil {
			created++
		}
	}
	plan, err := PlanManagedCreate(call.m, ManagedCreate{Project: call.creationProject, Name: *call.name, Instructions: instruction, Appearance: look}, created, DefaultManagerLimits())
	if err != nil {
		return err
	}
	now := call.now
	next := pa.Bot{Key: call.key, Project: plan.Project, ConversationID: t.o.ID(), Name: plan.Name, Appearance: plan.Appearance, Agent: plan.Agent, PermissionPolicy: plan.PermissionPolicy, Mode: plan.Mode, ManageTeam: plan.ManageTeam, CreatedBy: &plan.CreatedBy, ReplyInput: pa.ReplyInputSteer, Instructions: pa.InstructionSnapshot{Revision: 1, Text: plan.Instructions}, Version: 1, CreatedAt: now, UpdatedAt: now}
	call.targetName = next.Name
	if call.hidden != nil {
		next.Hidden = *call.hidden
	}
	c := pa.Conversation{Key: call.key, ID: next.ConversationID, MemoryRevision: 1, Version: 1}
	call.next, call.conversation = next, &c
	_, err = pa.ApplyEdit(next, pa.EditBot{ExpectedVersion: next.Version, Name: next.Name, Appearance: next.Appearance, Instructions: next.Instructions.Text})
	return err
}

func (t *TeamCoordinator) planManagedEdit(ctx context.Context, call *managerCall) error {
	target, err := t.managedTarget(ctx, call.m, call.key)
	if err != nil {
		return err
	}
	var look *pa.Appearance
	if call.color != nil || call.shape != nil {
		value := target.Appearance
		if call.color != nil {
			value.Color = *call.color
		}
		if call.shape != nil {
			value.Shape = pa.Shape(*call.shape)
		}
		look = &value
	}
	edit := ManagedEdit{Target: call.key, Name: call.name, Instructions: call.instructions, Appearance: look, Hidden: call.hidden}
	if err = CheckManagedEdit(call.m, target, edit); err != nil {
		return err
	}
	settings := pa.EditBot{ExpectedVersion: target.Version, Name: target.Name, Instructions: target.Instructions.Text, Appearance: target.Appearance}
	if call.name != nil {
		settings.Name = *call.name
	}
	if call.instructions != nil {
		settings.Instructions = *call.instructions
	}
	if look != nil {
		settings.Appearance = *look
	}
	next, err := pa.ApplyEdit(target, settings)
	call.next = next
	if err != nil {
		return err
	}
	if call.hidden != nil {
		call.next.Hidden = *call.hidden
	}
	call.next.UpdatedAt = call.now
	call.expected, call.targetSettingsVersion, call.targetName = target.Version, max(1, target.SettingsVersion), target.Name
	return nil
}

// describeManagerEffect is the approval card's detail: the entire resulting
// effect, including all inherited authority.
func (t *TeamCoordinator) describeManagerEffect(ctx context.Context, call *managerCall) error {
	next := call.next
	effect := map[string]any{"operation": call.operation, "project": next.Project, "botId": next.Key.BotID}
	if call.operation == pa.CreateOperation {
		settings := map[string]any{"name": next.Name, "instructions": next.Instructions.Text, "appearance": next.Appearance, "hidden": next.Hidden, "agent": next.Agent, "permission_policy": next.PermissionPolicy, "mode": next.Mode, "manageTeam": next.ManageTeam, "createdBy": next.CreatedBy}
		if call.directory != "" {
			// Older consoles render settings as field rows. Keep the destination visible on those clients.
			settings["folder"] = call.directory
		}
		effect["settings"] = settings
	} else {
		effect["targetName"] = call.targetName
		if next.Project != call.m.Project {
			destination := t.projectFolder(ctx, next.Project)
			if destination == "" {
				destination = next.Project.ProjectID
			}
			// The destination describes the target; it is not an editable field.
			call.fields["folder"], _ = json.Marshal(destination)
		}
		effect["changes"] = call.fields
	}
	detail, err := json.MarshalIndent(effect, "", "  ")
	if err != nil {
		return err
	}
	// UTF-16 units match CodyNC and JavaScript length, including astral text.
	if len(utf16.Encode([]rune(string(detail)))) > capacity.MaximumBotManagerCardUnits {
		return serviceFailure(pa.CapacityFull, "manager_card_detail")
	}
	call.detail = string(detail)
	return nil
}

func (t *TeamCoordinator) managerMutation(call *managerCall) Mutation {
	next, key, now := call.next, call.key, call.now
	mutation := Mutation{Command: call.cmd, ExpectedVersion: call.expected, Bot: &next, Conversation: call.conversation, Receipt: pa.Receipt{ID: t.o.ID(), Command: call.cmd, State: pa.EffectAccepted, ResourceID: key.BotID}}
	kind := pa.BotUpdated
	if call.operation == pa.CreateOperation {
		kind = pa.BotCreated
	}
	summary := pa.BotSummary{Bot: next, Version: next.Version, ActivityAt: now}
	mutation.Events = []pa.Event{{Version: pa.ContractVersion, ID: t.o.ID(), Fence: pa.Fence{Key: key, Project: next.Project, ConversationID: next.ConversationID}, Kind: kind, At: now, Bot: &summary}}
	return mutation
}

// awaitManagerApproval offers the planned change for approval and waits for
// its outcome, the plan's expiry or the caller's cancellation.
func (t *TeamCoordinator) awaitManagerApproval(ctx context.Context, call *managerCall) (any, error) {
	mutation := t.managerMutation(call)
	// The durable operation key stays stable, while each approval attempt has
	// a new capability ID. An old expired/revoked card cannot approve a retry
	// made under a new grant, even with the same native idempotency key.
	planID := t.o.ID()
	if planID == "" {
		return nil, serviceFailure(pa.Invalid, "manager_plan_id")
	}
	if t.o.Review != nil {
		planID = sourcePermissionPrefix + planID
	}
	limit := time.Duration(capacity.MaximumBotManagerApprovalSeconds) * time.Second
	planContext, cancelPlan := context.WithTimeout(ctx, limit)
	defer cancelPlan()
	fence := call.fence
	pending := &teamPending{plan: TeamPlan{ID: planID, Manager: fence.Key, ManagerName: call.m.Caller.Name, TargetName: call.targetName, Target: call.key, Operation: call.operation, Detail: call.detail, ExpiresAt: call.now.Add(limit)}, execution: ManagerExecution{Fence: fence, TurnID: call.turn, Under: call.m.Grant, TargetSettingsVersion: call.targetSettingsVersion}, mutation: mutation, context: planContext, done: make(chan teamOutcome, 1)}
	if call.folder != nil {
		pending.folder, pending.directory = *call.folder, call.directory
		pending.execution.CreationProject = call.creationProject
	}
	if err := t.registerPending(planID, pending, call); err != nil {
		return nil, err
	}
	defer func() { t.mu.Lock(); delete(t.pending, planID); t.mu.Unlock() }()
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
	if t.o.Review != nil {
		err := t.o.Review(planContext, pending.plan, pending.execution, func(answerCtx context.Context, allow bool, source pa.SourceStatus) error {
			_, err := t.Decide(answerCtx, fence.Key, planID, allow, source, func(context.Context) error { return nil })
			return err
		})
		select {
		case outcome := <-pending.done:
			return outcome.bot, outcome.err
		default:
		}
		if err != nil && planContext.Err() == nil {
			return nil, err
		}
	}
	select {
	case outcome := <-pending.done:
		return outcome.bot, outcome.err
	case <-planContext.Done():
		if errors.Is(planContext.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, abandon(pa.Expired)
		}
		return nil, abandon(pa.Revoked)
	}
}

// registerPending holds the plan until it is decided, refusing a second
// plan for the same operation or manager and a full pending table.
func (t *TeamCoordinator) registerPending(planID string, pending *teamPending, call *managerCall) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if planID == "" || t.pending[planID] != nil {
		return serviceFailure(pa.Invalid, "manager_plan_id")
	}
	for _, old := range t.pending {
		if old.mutation.Command.IdempotencyKey == call.operationID {
			if old.mutation.Command.ContentHash != call.cmd.ContentHash {
				return serviceFailure(pa.IdempotencyConflict, "manager_plan")
			}
			return serviceFailure(pa.OutcomeUnknown, "manager_plan_pending")
		}
		if old.plan.Manager == call.fence.Key {
			return serviceFailure(pa.CapacityFull, "manager_pending")
		}
	}
	if len(t.pending) >= capacity.MaximumBotManagerPending {
		return serviceFailure(pa.CapacityFull, "manager_pending")
	}
	t.pending[planID] = pending
	return nil
}
