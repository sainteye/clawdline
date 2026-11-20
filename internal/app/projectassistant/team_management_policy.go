package projectassistant

import (
	"slices"
	"strings"
	"unicode/utf8"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// TeamManagerGrant is the person's "Manage the team" switch for one Bot, as
// the host stores it. It is off by default; only the person's own settings
// write it, and the Bots a manager creates inherit it from the host, not from
// anything a model asked for. Revision moves on every change, so a decision
// made under one grant cannot be applied under another.
type TeamManagerGrant struct {
	Bot      pa.BotKey `json:"bot"`
	Enabled  bool      `json:"enabled"`
	Revision int64     `json:"revision"`
	// CreatedBy is set by the host when a manager created this Bot.
	CreatedBy *pa.BotKey `json:"created_by,omitempty"`
}

// GrantFor is the Bot's effective grant under its
// aggregate version, which moves on every settings change, so a decision made
// under one version cannot be applied under another.
func GrantFor(b pa.Bot) TeamManagerGrant {
	return TeamManagerGrant{Bot: b.Key, Enabled: b.CanManageTeam(), Revision: b.Version, CreatedBy: b.CreatedBy}
}

// ManagerContext is what the host derives for a manage tool call: the calling
// Bot from the turn's fence, its current grant, and its current Project. None
// of it comes from tool arguments.
type ManagerContext struct {
	Caller  pa.Bot
	Grant   TeamManagerGrant
	Project pa.ProjectScope
}

func (m ManagerContext) check() error {
	if m.Grant.Bot != m.Caller.Key || !m.Grant.Enabled || m.Grant.Revision < 1 {
		return serviceFailure(pa.ScopeDenied, "manage_team")
	}
	if err := pa.ValidateScope(m.Project); err != nil {
		return err
	}
	if m.Caller.Project != m.Project {
		return serviceFailure(pa.ScopeDenied, "manage_team_project")
	}
	return nil
}

// Manager tool arguments a model may set. Anything else, and in particular
// the authority fields below, is refused before a card is shown.
var managerFields = []string{"name", "instructions", "avatarColor", "avatarShape", "hidden"}

// Fields that would let a model widen its own or a teammate's authority, or
// that this increment cannot carry safely.
var managerRefused = []string{"manageTeam", "createdBy", "permission", "approvals", "mode", "agent", "backend", "folder", "cwd", "project", "model", "fallbacks", "routines", "connectors", "skills", "computer", "delete"}

// CheckManagerFields refuses unknown and authority-bearing argument names.
func CheckManagerFields(keys []string) error {
	for _, k := range keys {
		if slices.Contains(managerRefused, k) {
			return serviceFailure(pa.ScopeDenied, "manager_field_"+k)
		}
		if !slices.Contains(managerFields, k) && k != "botId" {
			return serviceFailure(pa.Invalid, "manager_field")
		}
	}
	return nil
}

// ManagedCreate is what a manager asked to create. Agent, PermissionPolicy and
// Mode are optional: empty inherits the manager's own.
type ManagedCreate struct {
	Name             string
	Instructions     string
	Appearance       pa.Appearance
	Agent            pa.Agent
	PermissionPolicy pa.PermissionPolicy
	Mode             string
}

// ManagedBotPlan is the Bot the host would create after approval. It is
// data: the host creates it through its own manager operation, never through
// the person's Service.Create, and records CreatedBy itself.
type ManagedBotPlan struct {
	Project          pa.ProjectScope
	Name             string
	Instructions     string
	Appearance       pa.Appearance
	Agent            pa.Agent
	PermissionPolicy pa.PermissionPolicy
	Mode             string
	ManageTeam       bool
	CreatedBy        pa.BotKey
	// Under is the grant the plan was made under; ConfirmManagerPlan
	// refuses it once that grant changed.
	Under TeamManagerGrant
}

// ManagerLimits are the caller's registered bounds. MaxCreated is how many
// live Bots managers may have created (Codync uses 20).
type ManagerLimits struct {
	MaxCreated int
}

// PlanManagedCreate mirrors Codync's create_bot: the new Bot works in the
// manager's Project, approves the way the manager does and works in the
// manager's mode. Only the explicit team grant is inherited. A policy or mode other
// than the manager's own is refused rather than ordered, since no ordering of
// policies across agents is proved. Another agent is refused unless the
// manager asks first and works in Auto mode: a native mode is its agent's,
// and Ask is the only approval policy whose meaning is the host's own.
func PlanManagedCreate(m ManagerContext, req ManagedCreate, createdLive int, l ManagerLimits) (ManagedBotPlan, error) {
	if err := m.check(); err != nil {
		return ManagedBotPlan{}, err
	}
	if l.MaxCreated < 1 {
		return ManagedBotPlan{}, serviceFailure(pa.Invalid, "manager_limits")
	}
	if createdLive < 0 || createdLive >= l.MaxCreated {
		return ManagedBotPlan{}, serviceFailure(pa.CapacityFull, "manager_created")
	}
	src := m.Caller
	if req.PermissionPolicy != "" && req.PermissionPolicy != src.PermissionPolicy {
		return ManagedBotPlan{}, serviceFailure(pa.ScopeDenied, "manager_permission_ceiling")
	}
	if req.Mode != "" && req.Mode != src.Mode {
		return ManagedBotPlan{}, serviceFailure(pa.ScopeDenied, "manager_mode_ceiling")
	}
	agent := req.Agent
	if agent == "" {
		agent = src.Agent
	}
	if !agent.Valid() {
		return ManagedBotPlan{}, serviceFailure(pa.Invalid, "agent")
	}
	if agent != src.Agent {
		if src.Mode != "" && src.Mode != pa.AutoMode {
			return ManagedBotPlan{}, serviceFailure(pa.Unsupported, "manager_mode_compatibility")
		}
		if src.PermissionPolicy != pa.Ask {
			return ManagedBotPlan{}, serviceFailure(pa.Unsupported, "manager_policy_compatibility")
		}
	}
	if strings.TrimSpace(req.Name) == "" || !utf8.ValidString(req.Name) || utf8.RuneCountInString(req.Name) > pa.MaxNameRunes {
		return ManagedBotPlan{}, serviceFailure(pa.Invalid, "name")
	}
	if err := pa.ValidateAppearance(req.Appearance); err != nil {
		return ManagedBotPlan{}, err
	}
	return ManagedBotPlan{Project: m.Project, Name: req.Name, Instructions: req.Instructions, Appearance: req.Appearance, Agent: agent, PermissionPolicy: src.PermissionPolicy, Mode: src.Mode, ManageTeam: src.ManageTeam, CreatedBy: src.Key, Under: m.Grant}, nil
}

// ManagedEdit is what a manager may change on a Bot, itself included.
type ManagedEdit struct {
	Target       pa.BotKey
	Name         *string
	Instructions *string
	Appearance   *pa.Appearance
	Hidden       *bool
}

// CheckManagedEdit allows editing a nondeleted Bot of the manager's own full
// Project. Deleting Bots, and changing who may manage, stay the person's.
func CheckManagedEdit(m ManagerContext, target pa.Bot, e ManagedEdit) error {
	if err := m.check(); err != nil {
		return err
	}
	if target.Key != e.Target || target.Key.BotID == "" {
		return serviceFailure(pa.NotFound, "bot")
	}
	if target.Project != m.Project {
		return serviceFailure(pa.ScopeDenied, "manager_target_project")
	}
	if e.Name == nil && e.Instructions == nil && e.Appearance == nil && e.Hidden == nil {
		return serviceFailure(pa.Invalid, "manager_nothing_to_change")
	}
	if e.Appearance != nil {
		return pa.ValidateAppearance(*e.Appearance)
	}
	return nil
}

// ConfirmManagerPlan is called after the person's approval and before the
// mutation: the grant the decision was made under must still be the current
// one, still enabled, and the caller still in the same Project. Revoking the
// switch while a card waits makes the late Allow change nothing.
func ConfirmManagerPlan(under TeamManagerGrant, now ManagerContext) error {
	if err := now.check(); err != nil {
		return err
	}
	if now.Grant.Bot != under.Bot || now.Grant.Revision != under.Revision || now.Grant.Enabled != under.Enabled {
		return serviceFailure(pa.Revoked, "manage_team")
	}
	return nil
}
