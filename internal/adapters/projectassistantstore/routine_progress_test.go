package projectassistantstore

import (
	"database/sql"
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRoutineProgressForgetIsAtomicAndCannotResurrect(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "progress")
	d := routineFixture(b)
	d.Progress = "Handled up to #40"
	d.ProgressRevision = 1
	save := app.Mutation{Command: command(b.Key, pa.EditOperation, "save-progress"), ExpectedVersion: 1, Routine: &d, Receipt: pa.Receipt{ID: "progress-saved", State: pa.EffectDelivered, ResourceID: d.ID}}
	if _, err := s.Commit(testContext, save); err != nil {
		t.Fatal(err)
	}
	defs, _, err := s.Routines(testContext, b.Key)
	if err != nil || len(defs) != 1 || defs[0].Progress != d.Progress {
		t.Fatal(defs, err)
	}
	forget := app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget-progress"), Project: b.Project, ExpectedRevision: 1, SourceIDs: []string{"old-source"}}
	if _, err = db.Exec(`CREATE TRIGGER fail_progress_forget BEFORE INSERT ON project_assistant_receipts WHEN NEW.operation='forget' BEGIN SELECT RAISE(ABORT,'fixture forget failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Forget(testContext, forget); err == nil {
		t.Fatal("forget injection did not fail")
	}
	defs, _, err = s.Routines(testContext, b.Key)
	if err != nil || defs[0].Progress != d.Progress || defs[0].Version != d.Version {
		t.Fatal("failed forget cleared progress", defs, err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_progress_forget`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Forget(testContext, forget); err != nil {
		t.Fatal(err)
	}
	defs, _, err = s.Routines(testContext, b.Key)
	if err != nil || defs[0].Progress != "" || defs[0].Version != 2 || defs[0].ProgressRevision != 2 {
		t.Fatal("forget retained note", defs, err)
	}
	if _, err = s.Commit(testContext, save); err != nil {
		t.Fatal("original receipt must remain replayable", err)
	}
	b, err = s.Bot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	stale := defs[0]
	stale.Version++
	stale.Progress = d.Progress
	stale.ProgressRevision = 1
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "stale-progress"), ExpectedVersion: b.Version, Routine: &stale, Receipt: pa.Receipt{ID: "stale", State: pa.EffectDelivered}})
	requireCode(t, err, pa.ContextStale)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted := New(db2)
	after, _, err := restarted.Routines(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(defs, after) {
		t.Fatal("restart changed progress/forget state", after, err)
	}
	archive, err := restarted.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, target, _ := testDB(t)
	if err = target.RestoreArchive(testContext, archive); err != nil {
		t.Fatal(err)
	}
	restored, _, err := target.Routines(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(after, restored) {
		t.Fatal("archive changed progress/forget state", restored, err)
	}
}
