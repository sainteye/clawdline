package projectassistantstore

import (
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func managedStoreFixture(t *testing.T) (*Store, pa.Bot, app.ManagerExecution) {
	t.Helper()
	_, s, _ := testDB(t)
	b := seed(t, s, "manager")
	f := populate(t, s, b, pa.EffectExecuting)
	b, err := s.Bot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	next := b
	next.ManageTeam = true
	next.Version++
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "grant"), ExpectedVersion: b.Version, Bot: &next, Receipt: pa.Receipt{ID: "grant", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	return s, next, app.ManagerExecution{Fence: f, TurnID: "turn", Under: app.GrantFor(next)}
}
func managedStoreCreate(manager pa.Bot, id string) app.Mutation {
	b := manager
	b.Key.BotID = id
	b.ConversationID = "chat/" + id
	b.CreatedBy = &manager.Key
	b.Version = 1
	b.Name = id
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	return app.Mutation{Command: command(b.Key, pa.CreateOperation, "create-managed-"+id), Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "created-managed-" + id, State: pa.EffectDelivered, ResourceID: id}}
}
func managedStoreNoEffect(t *testing.T, s *Store, m app.Mutation) {
	t.Helper()
	if _, err := s.Bot(testContext, m.Command.Key); pa.Code(err) != pa.NotFound {
		t.Fatal("refused transaction left a Bot", err)
	}
	if receipt, err := s.Receipt(testContext, m.Command); err != nil || receipt != nil {
		t.Fatal("refused transaction left a receipt", receipt, err)
	}
}
func TestManagedStoreTransactionRechecksRevokedGrantAndBinding(t *testing.T) {
	for _, reason := range []string{"grant-off", "grant-replaced", "binding-revoked", "turn-ended"} {
		t.Run(reason, func(t *testing.T) {
			s, manager, execution := managedStoreFixture(t)
			m := managedStoreCreate(manager, "helper")
			change := app.Mutation{Command: command(manager.Key, pa.EditOperation, reason), ExpectedVersion: manager.Version, Receipt: pa.Receipt{ID: reason, State: pa.EffectDelivered}}
			switch reason {
			case "grant-off":
				next := manager
				next.ManageTeam = false
				change.Bot = &next
			case "binding-revoked":
				b, err := s.Binding(testContext, manager.Key, execution.Fence.BindingID)
				if err != nil {
					t.Fatal(err)
				}
				b.Revoked = true
				change.Binding = &b
			case "turn-ended":
				turn, err := s.Turn(testContext, manager.Key, execution.TurnID)
				if err != nil {
					t.Fatal(err)
				}
				turn.State = pa.TurnSucceeded
				change.Turn = &turn
				c, err := s.Conversation(testContext, manager.Key, manager.ConversationID)
				if err != nil {
					t.Fatal(err)
				}
				c.ActiveTurnID = ""
				change.Conversation = &c
			}
			if _, err := s.Commit(testContext, change); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CommitManaged(testContext, execution, m, 20); err == nil {
				t.Fatal("stale manager mutation committed after revocation")
			}
			managedStoreNoEffect(t, s, m)
		})
	}
}
func TestManagedStoreTransactionCapsCreationAndRollsBackRefusals(t *testing.T) {
	s, manager, execution := managedStoreFixture(t)
	first := managedStoreCreate(manager, "first")
	if _, err := s.CommitManaged(testContext, execution, first, 1); err != nil {
		t.Fatal(err)
	}
	second := managedStoreCreate(manager, "second")
	if _, err := s.CommitManaged(testContext, execution, second, 1); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	managedStoreNoEffect(t, s, second)
	for _, reason := range []string{"created-by", "policy", "project", "lead"} {
		t.Run(reason, func(t *testing.T) {
			m := managedStoreCreate(manager, reason)
			switch reason {
			case "created-by":
				key := pa.BotKey{MachineID: manager.Key.MachineID, BotID: "model-forged"}
				m.Bot.CreatedBy = &key
			case "policy":
				m.Bot.PermissionPolicy = pa.Approve
			case "project":
				m.Bot.Project.ProjectID = "other"
			case "lead":
				m.Bot.Lead = true
			}
			if _, err := s.CommitManaged(testContext, execution, m, 20); err == nil {
				t.Fatal("authority field committed")
			}
			managedStoreNoEffect(t, s, m)
		})
	}
}
func TestManagedStoreTransactionPermitsSelfEditsWithoutWideningAuthority(t *testing.T) {
	s, manager, execution := managedStoreFixture(t)
	next, err := pa.ApplyEdit(manager, pa.EditBot{ExpectedVersion: manager.Version, Name: "Renamed manager", Appearance: manager.Appearance, Instructions: "Review carefully"})
	if err != nil {
		t.Fatal(err)
	}
	m := app.Mutation{Command: command(manager.Key, pa.EditOperation, "self"), ExpectedVersion: manager.Version, Bot: &next, Receipt: pa.Receipt{ID: "self", State: pa.EffectDelivered}}
	if _, err = s.CommitManaged(testContext, execution, m, 20); err != nil {
		t.Fatal(err)
	}
	got, err := s.Bot(testContext, manager.Key)
	if err != nil || got.Name != "Renamed manager" || !got.ManageTeam || got.CreatedBy != nil {
		t.Fatal(got, err)
	}
}
