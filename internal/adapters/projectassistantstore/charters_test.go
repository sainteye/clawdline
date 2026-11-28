package projectassistantstore

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var charterAt = time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)

func saveCharter(t *testing.T, s *Store, expected int64, text, key string) pa.CharterVersion {
	t.Helper()
	v, err := s.SaveCharter(testContext, CharterWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: expected, Text: text, Author: pa.CharterByPerson, IdempotencyKey: key, At: charterAt})
	if err != nil {
		t.Fatalf("save %q at %d: %v", text, expected, err)
	}
	return v
}

func TestCharterVersionsConflictReplayAndRestore(t *testing.T) {
	_, store, _ := testDB(t)
	none, err := store.Charter(testContext, "m", "p")
	if err != nil || none.Version != 0 {
		t.Fatalf("a Project without a charter is version 0: %+v %v", none, err)
	}
	first := saveCharter(t, store, 0, "Lead plans; makers build.", "k1")
	if first.Version != 1 || first.Author != pa.CharterByPerson {
		t.Fatalf("first version: %+v", first)
	}
	// Another device still at version 0 is refused, with the current version.
	current, err := store.SaveCharter(testContext, CharterWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 0, Text: "other", Author: pa.CharterByPerson, IdempotencyKey: "k2", At: charterAt})
	requireCode(t, err, pa.StaleVersion)
	if current.Version != 1 {
		t.Fatalf("a stale save answers the current version: %+v", current)
	}
	// The lost reply of the first save finds its own version.
	again := saveCharter(t, store, 0, "Lead plans; makers build.", "k1")
	if again.Version != 1 {
		t.Fatalf("a replay answers its own version: %+v", again)
	}
	// The same key with another request is a conflict, not a second save.
	_, err = store.SaveCharter(testContext, CharterWrite{MachineID: "m", ProjectID: "p", ExpectedVersion: 1, Text: "changed", Author: pa.CharterByPerson, IdempotencyKey: "k1", At: charterAt})
	requireCode(t, err, pa.IdempotencyConflict)
	saveCharter(t, store, 1, "Ask the person before deploying.", "k3")
	restored, err := store.RestoreCharter(testContext, "m", "p", 2, 1, "k4", charterAt)
	if err != nil || restored.Version != 3 || restored.Author != pa.CharterByRestore || restored.Text != "Lead plans; makers build." {
		t.Fatalf("restore copies an old version as a new one: %+v %v", restored, err)
	}
	replayed, err := store.RestoreCharter(testContext, "m", "p", 2, 1, "k4", charterAt)
	if err != nil || replayed.Version != 3 {
		t.Fatalf("a repeated restore answers its own version: %+v %v", replayed, err)
	}
	_, err = store.RestoreCharter(testContext, "m", "p", 3, 99, "k5", charterAt)
	requireCode(t, err, pa.NotFound)
	history, err := store.CharterHistory(testContext, "m", "p")
	if err != nil || len(history) != 3 || history[0].Version != 3 || history[0].Text != "" || history[2].Version != 1 {
		t.Fatalf("history is newest first without text: %+v %v", history, err)
	}
	old, err := store.CharterAt(testContext, "m", "p", 2)
	if err != nil || old.Text != "Ask the person before deploying." {
		t.Fatalf("an old version is readable: %+v %v", old, err)
	}
	list, err := store.Charters(testContext, "m")
	if err != nil || len(list) != 1 || list[0].ProjectID != "p" || list[0].Version != 3 || list[0].UpdatedBy != pa.CharterByRestore {
		t.Fatalf("listing: %+v %v", list, err)
	}
	if other, _ := store.Charter(testContext, "other-machine", "p"); other.Version != 0 {
		t.Fatalf("another machine's Project has its own charter: %+v", other)
	}
}

func TestCharterCapacityRefusesAndTrimsOldestVersions(t *testing.T) {
	_, store, _ := testDB(t)
	_, err := store.SaveCharter(testContext, CharterWrite{MachineID: "m", ProjectID: "p", Text: strings.Repeat("x", pa.MaxCharterTextBytes+1), Author: pa.CharterByPerson, IdempotencyKey: "big", At: charterAt})
	requireCode(t, err, pa.CapacityFull)
	for i := range pa.MaxCharterVersions + 5 {
		saveCharter(t, store, int64(i), fmt.Sprintf("v%d", i+1), fmt.Sprintf("k%d", i))
	}
	history, err := store.CharterHistory(testContext, "m", "p")
	if err != nil || len(history) != pa.MaxCharterVersions || history[0].Version != pa.MaxCharterVersions+5 || history[len(history)-1].Version != 6 {
		t.Fatalf("only the newest %d versions stay: %d %+v %v", pa.MaxCharterVersions, len(history), history[len(history)-1], err)
	}
	_, err = store.CharterAt(testContext, "m", "p", 5)
	requireCode(t, err, pa.NotFound)
	for i := range pa.MaxCharterProposals {
		if _, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: pa.MaxCharterVersions + 5, Text: fmt.Sprint(i), BotID: "lead", TurnID: "t", CreatedAt: charterAt}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: pa.MaxCharterVersions + 5, Text: "one more", BotID: "lead", TurnID: "t", CreatedAt: charterAt})
	requireCode(t, err, pa.CapacityFull)
}

func TestCharterProposalApplyDismissAndStaleBase(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "Old rule.", "k1")
	proposal, err := store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 1, Text: "New rule.", Summary: "Replace the old rule", BotID: "lead", TurnID: "t1", CreatedAt: charterAt})
	if err != nil || proposal.ID == "" {
		t.Fatalf("propose: %+v %v", proposal, err)
	}
	same, err := store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 1, Text: "New rule.", Summary: "Replace the old rule", BotID: "lead", TurnID: "t1", CreatedAt: charterAt})
	if err != nil || same.ID != proposal.ID {
		t.Fatalf("a repeated proposal is the same proposal: %+v %v", same, err)
	}
	_, err = store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 0, Text: "x", BotID: "lead", TurnID: "t2", CreatedAt: charterAt})
	requireCode(t, err, pa.StaleVersion)
	dismissed, err := store.ProposeCharter(testContext, "m", pa.CharterProposal{ProjectID: "p", BaseVersion: 1, Text: "Dismiss me.", BotID: "lead", TurnID: "t3", CreatedAt: charterAt})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := store.Charters(testContext, "m"); len(list) != 1 || list[0].Pending != 2 {
		t.Fatalf("the listing counts pending proposals: %+v", list)
	}
	applied, err := store.ApplyCharterProposal(testContext, "m", "p", proposal.ID, 1, "a1", "", charterAt)
	if err != nil || applied.Version != 2 || applied.Text != "New rule." || applied.Author != pa.CharterByLeadProposal || applied.Reason != "Replace the old rule" {
		t.Fatalf("apply saves the proposal as the next version: %+v %v", applied, err)
	}
	replay, err := store.ApplyCharterProposal(testContext, "m", "p", proposal.ID, 1, "a1", "", charterAt)
	if err != nil || replay.Version != 2 {
		t.Fatalf("a repeated apply answers its own version: %+v %v", replay, err)
	}
	// The other proposal was written against version 1; the charter moved.
	_, err = store.ApplyCharterProposal(testContext, "m", "p", dismissed.ID, 2, "a2", "", charterAt)
	requireFailure(t, err, pa.StaleVersion, "charter_proposal_base")
	if err = store.DismissCharterProposal(testContext, "m", "p", dismissed.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.DismissCharterProposal(testContext, "m", "p", dismissed.ID); err != nil {
		t.Fatalf("dismissing twice is the same dismissal: %v", err)
	}
	pending, err := store.CharterProposals(testContext, "m", "p")
	if err != nil || len(pending) != 0 {
		t.Fatalf("no proposals remain: %+v %v", pending, err)
	}
	_, err = store.ApplyCharterProposal(testContext, "m", "p", dismissed.ID, 2, "a3", "", charterAt)
	requireCode(t, err, pa.NotFound)
	if err = store.RecordCharterRead(testContext, "m", "p", "bot-1", 2, "turn-9", charterAt); err != nil {
		t.Fatal(err)
	}
	reads, err := store.CharterReads(testContext, "m", "p")
	if err != nil || reads["bot-1"].Version != 2 || reads["bot-1"].TurnID != "turn-9" {
		t.Fatalf("reads: %+v %v", reads, err)
	}
}

func requireFailure(t *testing.T, err error, code pa.ErrorCode, field string) {
	t.Helper()
	requireCode(t, err, code)
	if f, ok := err.(*pa.Failure); !ok || f.Field != field {
		t.Fatalf("want field %s, got %v", field, err)
	}
}
