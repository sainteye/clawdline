package projectassistantstore

import (
	"reflect"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func routineFixture(b pa.Bot) pa.Routine {
	return pa.Routine{ID: "routine", Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, Name: "Fixture", Instruction: "Return a result", IntervalSeconds: 60, TimeoutSeconds: 3600, Enabled: true, NextDue: time.Unix(1000, 0), Version: 1}
}
func TestRoutineDefinitionCommitRollbackReplayAndOwner(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "one")
	d := routineFixture(b)
	mutation := app.Mutation{Command: command(b.Key, pa.EditOperation, "save-routine"), ExpectedVersion: 1, Routine: &d, Receipt: pa.Receipt{ID: "routine-saved", State: pa.EffectDelivered, ResourceID: d.ID}}
	if _, e := db.Exec(`CREATE TRIGGER reject_routine_receipt BEFORE INSERT ON project_assistant_receipts BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Commit(testContext, mutation); e == nil {
		t.Fatal("failed SQL commit accepted")
	}
	defs, runs, e := s.Routines(testContext, b.Key)
	if e != nil || len(defs) != 0 || len(runs) != 0 {
		t.Fatal("rollback leaked routine", defs, runs, e)
	}
	if _, e = db.Exec(`DROP TRIGGER reject_routine_receipt`); e != nil {
		t.Fatal(e)
	}
	first, e := s.Commit(testContext, mutation)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Commit(testContext, mutation)
	if e != nil || first != again {
		t.Fatal("same command replay changed", first, again, e)
	}
	defs, _, e = s.Routines(testContext, b.Key)
	if e != nil || len(defs) != 1 {
		t.Fatal(defs, e)
	}
	foreign := d
	foreign.Project.CanonicalID = "foreign"
	foreign.Version++
	_, e = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "foreign"), ExpectedVersion: 2, Routine: &foreign, Receipt: pa.Receipt{ID: "foreign", State: pa.EffectDelivered}})
	requireCode(t, e, pa.ScopeDenied)
	archive, e := s.ExportArchive(testContext, b.Key)
	if e != nil {
		t.Fatal(e)
	}
	_, target, _ := testDB(t)
	if e = target.RestoreArchive(testContext, archive); e != nil {
		t.Fatal(e)
	}
	restored, e := target.ExportArchive(testContext, b.Key)
	if e != nil || !reflect.DeepEqual(archive, restored) {
		t.Fatal("routine archive changed", e)
	}
	if e = s.DeleteBot(testContext, b.Key); e != nil {
		t.Fatal(e)
	}
	defs, runs, e = s.Routines(testContext, b.Key)
	if e != nil || len(defs) != 0 || len(runs) != 0 {
		t.Fatal("delete leaked owned rows", defs, runs, e)
	}
}
func TestRoutinePendingWakeCoalescesAndPausedClaimRefuses(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	d := routineFixture(b)
	if _, e := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "save"), ExpectedVersion: 1, Routine: &d, Receipt: pa.Receipt{ID: "save", State: pa.EffectDelivered}}); e != nil {
		t.Fatal(e)
	}
	run := pa.RoutineRun{Source: "schedule", ID: "wake", RoutineID: d.ID, Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, State: pa.TurnQueued, CreatedAt: time.Unix(1000, 0), TimeoutSeconds: 3600}
	bad := run
	bad.State = "unrecognized"
	_, badErr := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "invalid-run"), ExpectedVersion: 2, RoutineRun: &bad, Receipt: pa.Receipt{ID: "invalid-run", State: pa.EffectAccepted}})
	requireCode(t, badErr, pa.Invalid)
	m := app.Mutation{Command: command(b.Key, pa.EditOperation, "wake"), ExpectedVersion: 2, RoutineRun: &run, Receipt: pa.Receipt{ID: "wake", State: pa.EffectAccepted}}
	if _, e := s.Commit(testContext, m); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Commit(testContext, m); e != nil {
		t.Fatal(e)
	}
	forged := run
	forged.CreatedAt = forged.CreatedAt.Add(time.Second)
	_, forgedErr := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "forged-run"), ExpectedVersion: 3, RoutineRun: &forged, Receipt: pa.Receipt{ID: "forged-run", State: pa.EffectAccepted}})
	requireCode(t, forgedErr, pa.StaleVersion)
	duplicate := run
	duplicate.ID = "duplicate"
	m.RoutineRun = &duplicate
	m.Command = command(b.Key, pa.EditOperation, "duplicate")
	m.ExpectedVersion = 3
	m.Receipt.ID = "duplicate"
	if _, e := s.Commit(testContext, m); e == nil {
		t.Fatal("second pending wake admitted")
	}
	d.Version++
	d.Enabled = false
	if _, e := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "pause"), ExpectedVersion: 3, Routine: &d, Receipt: pa.Receipt{ID: "pause", State: pa.EffectDelivered}}); e != nil {
		t.Fatal(e)
	}
	m.ExpectedVersion = 4
	_, e := s.Commit(testContext, m)
	requireCode(t, e, pa.Revoked)
}
