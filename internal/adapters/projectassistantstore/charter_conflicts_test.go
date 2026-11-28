package projectassistantstore

import (
	"fmt"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func conflictReport(bot string, version int64, sentence string) pa.CharterConflict {
	return pa.CharterConflict{ProjectID: "p", BotID: bot, TurnID: "turn-" + bot, Version: version, Source: pa.ConflictInMemory,
		BotSentence: sentence, CharterSentence: "The lead reviews every change.", Section: "Review", CreatedAt: charterAt}
}

func openConflicts(t *testing.T, s *Store) map[string]pa.CharterConflictState {
	t.Helper()
	list, err := s.CharterConflicts(testContext, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]pa.CharterConflictState{}
	for _, c := range list {
		out[c.BotSentence] = c.State
	}
	return out
}

// The same Bot reporting the same sentence against the same version is one
// report, whatever became of it; a report on another version is stale.
func TestCharterConflictsAreDeduplicatedByBotVersionAndSentence(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "## Review\n\nThe lead reviews every change.", "k1")
	first, err := store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, "I merge without review."))
	if err != nil || first.State != pa.ConflictOpen || !strings.HasPrefix(first.ID, "cc-") {
		t.Fatalf("first report: %+v %v", first, err)
	}
	again := conflictReport("atlas", 1, "I merge without review.")
	again.TurnID = "later-turn"
	second, err := store.ReportCharterConflict(testContext, "m", again)
	if err != nil || second.ID != first.ID || second.TurnID != first.TurnID {
		t.Fatalf("a repeated report answers the stored one: %+v %v", second, err)
	}
	if _, err = store.ReportCharterConflict(testContext, "m", conflictReport("lens", 1, "I merge without review.")); err != nil {
		t.Fatal(err)
	}
	list, err := store.CharterConflicts(testContext, "m", "p")
	if err != nil || len(list) != 2 {
		t.Fatalf("two Bots with one sentence are two reports: %+v %v", list, err)
	}
	// A dismissed report is not reopened by the same sentence.
	if _, err = store.BeginConflictResolution(testContext, "m", "p", first.ID, pa.DismissReport, "d1", "dismiss"); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishConflictResolution(testContext, "m", "p", first.ID, pa.ConflictDismissed, charterAt); err != nil {
		t.Fatal(err)
	}
	if back, err := store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, "I merge without review.")); err != nil || back.State != pa.ConflictDismissed {
		t.Fatalf("a dismissed report stays dismissed: %+v %v", back, err)
	}
	_, err = store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 2, "Another sentence."))
	requireCode(t, err, pa.StaleVersion)
}

// When a Bot is asked to check a newer version, its unresolved reports on
// older versions are superseded; another Bot's reports and handled ones stay.
func TestACheckOfANewerVersionSupersedesTheBotsOlderOpenReports(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "v1", "k1")
	for _, r := range []pa.CharterConflict{conflictReport("atlas", 1, "Atlas open."), conflictReport("atlas", 1, "Atlas handled."), conflictReport("lens", 1, "Lens open.")} {
		if _, err := store.ReportCharterConflict(testContext, "m", r); err != nil {
			t.Fatal(err)
		}
	}
	handled := CharterConflictID("atlas", 1, "Atlas handled.")
	if _, err := store.BeginConflictResolution(testContext, "m", "p", handled, pa.ChangeCharter, "c1", "charter"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishConflictResolution(testContext, "m", "p", handled, pa.ConflictHandedToCharter, charterAt); err != nil {
		t.Fatal(err)
	}
	saveCharter(t, store, 1, "v2", "k2")
	// Checking the version the reports were made against changes nothing.
	if err := store.RecordCharterCheck(testContext, "m", "p", "atlas", 1, "t1", charterAt); err != nil {
		t.Fatal(err)
	}
	if got := openConflicts(t, store); got["Atlas open."] != pa.ConflictOpen {
		t.Fatalf("a check of the same version keeps the report: %+v", got)
	}
	if err := store.RecordCharterCheck(testContext, "m", "p", "atlas", 2, "t2", charterAt); err != nil {
		t.Fatal(err)
	}
	got := openConflicts(t, store)
	if got["Atlas open."] != pa.ConflictSuperseded || got["Atlas handled."] != pa.ConflictHandedToCharter || got["Lens open."] != pa.ConflictOpen {
		t.Fatalf("after Atlas checked version 2: %+v", got)
	}
	checks, err := store.CharterChecks(testContext, "m", "p")
	if err != nil || checks["atlas"] != 2 {
		t.Fatalf("checks: %+v %v", checks, err)
	}
	// A superseded report cannot be resolved any more.
	_, err = store.BeginConflictResolution(testContext, "m", "p", CharterConflictID("atlas", 1, "Atlas open."), pa.FollowCharter, "f1", "bot")
	requireCode(t, err, pa.StaleVersion)
}

// Open reports past the bound are refused until the person handles one, and
// an over-long quote is refused.
func TestCharterConflictCapacityRefusals(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "v1", "k1")
	for i := 0; i < pa.MaxCharterConflicts; i++ {
		if _, err := store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, fmt.Sprintf("Sentence %d.", i))); err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, "One too many."))
	requireCode(t, err, pa.CapacityFull)
	if _, err = store.BeginConflictResolution(testContext, "m", "p", CharterConflictID("atlas", 1, "Sentence 0."), pa.DismissReport, "d1", "dismiss"); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishConflictResolution(testContext, "m", "p", CharterConflictID("atlas", 1, "Sentence 0."), pa.ConflictDismissed, charterAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, "One too many.")); err != nil {
		t.Fatalf("a handled report makes room: %v", err)
	}
	long := conflictReport("atlas", 1, strings.Repeat("長", pa.MaxCharterConflictSentenceBytes/3+1))
	_, err = store.ReportCharterConflict(testContext, "m", long)
	requireCode(t, err, pa.CapacityFull)
	counts, err := store.OpenCharterConflicts(testContext, "m")
	if err != nil || counts["p"] != pa.MaxCharterConflicts {
		t.Fatalf("open counts: %+v %v", counts, err)
	}
}

// A repeated choice with the same key answers the plan it chose; the same
// key with another action is an idempotency conflict.
func TestConflictResolutionRemembersItsPlan(t *testing.T) {
	_, store, _ := testDB(t)
	saveCharter(t, store, 0, "v1", "k1")
	c, err := store.ReportCharterConflict(testContext, "m", conflictReport("atlas", 1, "Mine."))
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.BeginConflictResolution(testContext, "m", "p", c.ID, pa.FollowCharter, "f1", "memory:fact")
	if err != nil || first.Repeated || first.Plan != "memory:fact" {
		t.Fatalf("first: %+v %v", first, err)
	}
	again, err := store.BeginConflictResolution(testContext, "m", "p", c.ID, pa.FollowCharter, "f1", "bot")
	if err != nil || !again.Repeated || again.Plan != "memory:fact" {
		t.Fatalf("a repeated key keeps its plan: %+v %v", again, err)
	}
	_, err = store.BeginConflictResolution(testContext, "m", "p", c.ID, pa.DismissReport, "f1", "dismiss")
	requireCode(t, err, pa.IdempotencyConflict)
}
