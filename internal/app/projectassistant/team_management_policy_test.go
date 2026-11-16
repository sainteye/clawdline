package projectassistant

import (
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func managerFixture() (ManagerContext, pa.Bot) {
	scope := pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "repo", Kind: pa.RepositoryProject}
	caller := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "lead"}, Project: scope, Name: "Lead", Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Mode: pa.AutoMode}
	target := pa.Bot{Key: pa.BotKey{MachineID: "m", BotID: "mate"}, Project: scope, Name: "Mate", Agent: pa.ClaudeCode}
	return ManagerContext{Caller: caller, Grant: TeamManagerGrant{Bot: caller.Key, Enabled: true, Revision: 1}, Project: scope}, target
}

var look = pa.Appearance{Shape: pa.Hex, Color: "#336699"}

func TestTeamManagementRequiresThePersonsOptIn(t *testing.T) {
	m, target := managerFixture()
	limits := ManagerLimits{MaxCreated: 20}
	off := m
	off.Grant.Enabled = false
	if _, err := PlanManagedCreate(off, ManagedCreate{Name: "New", Appearance: look}, 0, limits); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("manager off: %v", err)
	}
	stolen := m
	stolen.Grant.Bot = target.Key // another Bot's grant does not authorize this caller
	if err := CheckManagedEdit(stolen, target, ManagedEdit{Target: target.Key, Hidden: new(bool)}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("borrowed grant: %v", err)
	}
	for _, field := range []string{"manageTeam", "createdBy", "permission", "mode", "folder", "delete"} {
		if err := CheckManagerFields([]string{"botId", field}); pa.Code(err) != pa.ScopeDenied {
			t.Fatalf("%s accepted: %v", field, err)
		}
	}
	if err := CheckManagerFields([]string{"surprise"}); pa.Code(err) != pa.Invalid {
		t.Fatal("unknown field accepted")
	}
	if err := CheckManagerFields([]string{"botId", "name", "hidden"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagedBotsInheritAndNeverExceedTheManager(t *testing.T) {
	m, target := managerFixture()
	limits := ManagerLimits{MaxCreated: 20}
	plan, err := PlanManagedCreate(m, ManagedCreate{Name: "Helper", Appearance: look}, 3, limits)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Project != m.Project || plan.PermissionPolicy != pa.Ask || plan.Mode != pa.AutoMode || !plan.ManageTeam || plan.CreatedBy != m.Caller.Key || plan.Agent != pa.ClaudeCode {
		t.Fatalf("plan %+v", plan)
	}
	if _, err = PlanManagedCreate(m, ManagedCreate{Name: "Freer", Appearance: look, PermissionPolicy: pa.Approve}, 0, limits); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("freer policy: %v", err)
	}
	if _, err = PlanManagedCreate(m, ManagedCreate{Name: "Other mode", Appearance: look, Mode: "bypassPermissions"}, 0, limits); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("other mode: %v", err)
	}
	if _, err = PlanManagedCreate(m, ManagedCreate{Name: "x", Appearance: look}, 20, limits); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("created limit: %v", err)
	}
	if _, err = PlanManagedCreate(m, ManagedCreate{Name: "x", Appearance: look}, 0, ManagerLimits{}); pa.Code(err) != pa.Invalid {
		t.Fatal("missing limits accepted")
	}
	// Another agent: allowed only from Auto mode under Ask.
	if p, err := PlanManagedCreate(m, ManagedCreate{Name: "Codex", Appearance: look, Agent: pa.Codex}, 0, limits); err != nil || p.Agent != pa.Codex {
		t.Fatalf("auto+ask other agent: %v", err)
	}
	planMode := m
	planMode.Caller.Mode = "plan"
	if _, err = PlanManagedCreate(planMode, ManagedCreate{Name: "Codex", Appearance: look, Agent: pa.Codex}, 0, limits); pa.Code(err) != pa.Unsupported {
		t.Fatalf("native mode carried to another agent: %v", err)
	}
	autoRead := m
	autoRead.Caller.PermissionPolicy = pa.AutoRead
	if _, err = PlanManagedCreate(autoRead, ManagedCreate{Name: "Codex", Appearance: look, Agent: pa.Codex}, 0, limits); pa.Code(err) != pa.Unsupported {
		t.Fatalf("auto_read carried to another agent: %v", err)
	}
	// Same full Project only: the same repository under another Project ID is foreign.
	foreign := target
	foreign.Project.ProjectID = "p2"
	if err = CheckManagedEdit(m, foreign, ManagedEdit{Target: foreign.Key, Hidden: new(bool)}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("foreign target: %v", err)
	}
	moved := m
	moved.Caller.Project.ProjectID = "p2"
	if _, err = PlanManagedCreate(moved, ManagedCreate{Name: "x", Appearance: look}, 0, limits); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("caller outside the current Project: %v", err)
	}
	name := "Renamed"
	if err = CheckManagedEdit(m, m.Caller, ManagedEdit{Target: m.Caller.Key, Name: &name}); err != nil {
		t.Fatalf("self edit: %v", err)
	}
	if err = CheckManagedEdit(m, target, ManagedEdit{Target: target.Key}); pa.Code(err) != pa.Invalid {
		t.Fatal("empty edit")
	}
}

func TestRevokingManagementWhileACardWaitsChangesNothing(t *testing.T) {
	m, _ := managerFixture()
	plan, err := PlanManagedCreate(m, ManagedCreate{Name: "Helper", Appearance: look}, 0, ManagerLimits{MaxCreated: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err = ConfirmManagerPlan(plan.Under, m); err != nil {
		t.Fatal(err)
	}
	revoked := m
	revoked.Grant.Enabled, revoked.Grant.Revision = false, 2
	if err = ConfirmManagerPlan(plan.Under, revoked); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("revoked: %v", err)
	}
	reenabled := m
	reenabled.Grant.Revision = 3
	if err = ConfirmManagerPlan(plan.Under, reenabled); pa.Code(err) != pa.Revoked {
		t.Fatalf("off and on again while waiting: %v", err)
	}
}
