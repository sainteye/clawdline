package projectassistantstore

import (
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func savePlaybook(t *testing.T, s *Store, expected int64, p pa.CharterPlaybook, key string) pa.CharterVersion {
	t.Helper()
	v, err := s.SaveCharterPlaybook(testContext, CharterPlaybookWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: expected, Playbook: p, IdempotencyKey: key, At: charterAt})
	if err != nil {
		t.Fatalf("save playbook %q at %d: %v", p.Title, expected, err)
	}
	return v
}

// A playbook is part of the charter: each change is a version, a text save
// keeps the playbooks, and the delivery version moves only for the Bots
// whose playbooks changed.
func TestCharterPlaybooksAreVersionsOfTheCharter(t *testing.T) {
	_, store, _ := testDB(t)
	_, err := store.SaveCharterPlaybook(testContext, CharterPlaybookWrite{MachineID: "m", ProjectID: "p", Playbook: pa.CharterPlaybook{Title: "Release"}, IdempotencyKey: "early", At: charterAt})
	requireFailure(t, err, pa.Invalid, "charter")

	saveCharter(t, store, 0, "Lead plans.", "k1")
	v2 := savePlaybook(t, store, 1, pa.CharterPlaybook{Title: "Release", Text: "Tag, then ship.", BotIDs: []string{"maker"}}, "k2")
	if len(v2.Playbooks) != 1 || len(v2.Playbooks[0].ID) != len("pb-")+16 || v2.DeliveryVersion("maker") != 2 || v2.DeliveryVersion("lead") != 1 {
		t.Fatalf("a new playbook is version 2 for its Bot only: %+v", v2)
	}
	id := v2.Playbooks[0].ID
	if again := savePlaybook(t, store, 1, pa.CharterPlaybook{Title: "Release", Text: "Tag, then ship.", BotIDs: []string{"maker"}}, "k2"); again.Version != 2 || again.Playbooks[0].ID != id {
		t.Fatalf("a repeated create answers its own version and id: %+v", again)
	}
	_, err = store.SaveCharterPlaybook(testContext, CharterPlaybookWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 1, Playbook: pa.CharterPlaybook{Title: "Other"}, IdempotencyKey: "stale", At: charterAt})
	requireFailure(t, err, pa.StaleVersion, "charter_version")
	_, err = store.SaveCharterPlaybook(testContext, CharterPlaybookWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 2, Playbook: pa.CharterPlaybook{ID: "pb-missing", Title: "Other"}, IdempotencyKey: "missing", At: charterAt})
	requireFailure(t, err, pa.NotFound, "charter_playbook")

	// A text save that knows nothing of playbooks keeps them, and the text
	// change reaches everybody.
	v3 := saveCharter(t, store, 2, "Lead plans; makers build.", "k3")
	current, err := store.Charter(testContext, "m", "p")
	if err != nil || current.Version != 3 || len(current.Playbooks) != 1 || current.Playbooks[0].ID != id || v3.DeliveryVersion("maker") != 3 || current.DeliveryVersion("lead") != 3 {
		t.Fatalf("a text save keeps the playbooks: %+v %v", current, err)
	}
	// Unassigning re-delivers to the Bot that lost it.
	v4 := savePlaybook(t, store, 3, pa.CharterPlaybook{ID: id, Title: "Release", Text: "Tag, then ship.", BotIDs: []string{"lead"}}, "k4")
	v5 := savePlaybook(t, store, 4, pa.CharterPlaybook{Title: "Review", Text: "Two approvals.", BotIDs: []string{"lead"}}, "k5")
	current, err = store.Charter(testContext, "m", "p")
	if err != nil || v4.DeliveryVersion("maker") != 4 || current.DeliveryVersion("maker") != 4 || current.DeliveryVersion("lead") != 5 || current.DeliveryVersion("idle") != 3 || len(v5.Playbooks) != 2 {
		t.Fatalf("deliveries survive a reread: %+v %v", current, err)
	}
	// Restore brings back that version's text and playbooks.
	restored, err := store.RestoreCharter(testContext, "m", "p", 5, 2, "k6", charterAt)
	if err != nil || restored.Text != "Lead plans." || len(restored.Playbooks) != 1 || !restored.Playbooks[0].Assigned("maker") || restored.DeliveryVersion("lead") != 6 {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	// A lead proposal applied keeps the playbooks too.
	if _, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 6, Text: "Proposed.", BotID: "lead", TurnID: "t", CreatedAt: charterAt}); err != nil {
		t.Fatal(err)
	}
	proposals, _ := store.CharterProposals(testContext, "m", "p")
	applied, err := store.ApplyCharterProposal(testContext, "m", "p", proposals[0].ID, 6, "k7", "", charterAt)
	if err != nil || len(applied.Playbooks) != 1 {
		t.Fatalf("an applied proposal keeps the playbooks: %+v %v", applied, err)
	}
	// Removing one playbook, then the charter.
	without, err := store.RemoveCharterPlaybook(testContext, "m", "p", 7, applied.Playbooks[0].ID, "k8", charterAt)
	if err != nil || len(without.Playbooks) != 0 || without.DeliveryVersion("maker") != 8 || without.DeliveryVersion("lead") != 7 {
		t.Fatalf("playbook removal: %+v %v", without, err)
	}
	_, err = store.RemoveCharterPlaybook(testContext, "m", "p", 8, applied.Playbooks[0].ID, "k9", charterAt)
	requireFailure(t, err, pa.NotFound, "charter_playbook")
	savePlaybook(t, store, 8, pa.CharterPlaybook{Title: "Again", BotIDs: []string{"maker"}}, "k10")
	if _, err = store.RemoveCharter(testContext, "m", "p", 9, "k11", charterAt); err != nil {
		t.Fatal(err)
	}
	gone, err := store.Charter(testContext, "m", "p")
	if err != nil || gone.Version != 0 || gone.DeliveryVersion("maker") != 0 || len(gone.Playbooks) != 0 {
		t.Fatalf("a removed charter has no playbooks: %+v %v", gone, err)
	}
	fresh := saveCharter(t, store, 0, "New charter.", "k12")
	if len(fresh.Playbooks) != 0 || fresh.DeliveryVersion("maker") != 11 {
		t.Fatalf("a charter after a removal starts without the old playbooks: %+v", fresh)
	}
}

// A version row written before playbooks existed reads as no playbooks whose
// text changed at its own version, and the next save builds on it.
func TestCharterRowsFromBeforePlaybooksReadAsNone(t *testing.T) {
	db, store, _ := testDB(t)
	if _, err := db.Exec(`INSERT INTO project_assistant_charter_versions(machine,project,version,author,reason,created_at,text,idem,request) VALUES('m','p',4,'person','',0,'Old charter.','old','old')`); err != nil {
		t.Fatal(err)
	}
	old, err := store.Charter(testContext, "m", "p")
	if err != nil || old.Version != 4 || old.Playbooks != nil || old.TextVersion != 4 || old.DeliveryVersion("maker") != 4 {
		t.Fatalf("an older row: %+v %v", old, err)
	}
	next := savePlaybook(t, store, 4, pa.CharterPlaybook{Title: "First", BotIDs: []string{"maker"}}, "k1")
	if next.DeliveryVersion("lead") != 4 || next.DeliveryVersion("maker") != 5 {
		t.Fatalf("the first playbook on an older row: %+v", next)
	}
	// Installing again, as every start does, leaves the added columns alone.
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
}

func TestCharterDocumentBoundRefusesAPlaybookPastIt(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "Charter.", "k0")
	version := int64(1)
	for i := range 8 {
		v, err := store.SaveCharterPlaybook(testContext, CharterPlaybookWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: version,
			Playbook: pa.CharterPlaybook{Title: "Long", Text: string(make([]byte, pa.MaxCharterPlaybookTextBytes))}, IdempotencyKey: "big" + string(rune('a'+i)), At: charterAt})
		if i == 7 {
			requireFailure(t, err, pa.CapacityFull, "charter_document")
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		version = v.Version
	}
}
