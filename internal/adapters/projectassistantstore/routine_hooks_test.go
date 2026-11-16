package projectassistantstore

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanttest"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRoutineHookArchiveCreatesNewLocalCapabilityAndDeliveryIsUnique(t *testing.T) {
	db, err := projectassistanttest.Bootstrap(testContext, Installer{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db)
	b := seed(t, s, "hooks")
	d, err := pa.SetRoutineSchedule(routineFixture(b), pa.Routine{}, []pa.RoutineTrigger{{Type: "webhook"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "save-hook"), ExpectedVersion: 1, Routine: &d, Receipt: pa.Receipt{ID: "saved-hook", State: pa.EffectDelivered, ResourceID: d.ID}}); err != nil {
		t.Fatal(err)
	}
	_, secret, err := s.RoutineHook(testContext, d.ID)
	if err != nil || len(secret) != 2*pa.MaxRoutineWebhookSecretBytes {
		t.Fatal("missing local capability", err)
	}
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(archive)
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("export contains hook credential")
	}
	targetDB, err := projectassistanttest.Bootstrap(testContext, Installer{})
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	target := New(targetDB)
	if err = target.RestoreArchive(testContext, archive); err != nil {
		t.Fatal(err)
	}
	_, restoredSecret, err := target.RoutineHook(testContext, d.ID)
	if err != nil || restoredSecret == secret || len(restoredSecret) != len(secret) {
		t.Fatal("archive transported capability", err)
	}
	finished := time.Now()
	run := pa.RoutineRun{ID: "delivery-first", RoutineID: d.ID, Source: "webhook", DeliveryID: "same-delivery", Event: pa.RoutineEvent{"source": "github"}, MemoryRevision: 1, Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, State: pa.TurnSucceeded, TimeoutSeconds: d.TimeoutSeconds, CreatedAt: finished.Add(-time.Minute), FinishedAt: &finished, Published: true}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "first-delivery"), ExpectedVersion: 2, RoutineRun: &run, Receipt: pa.Receipt{ID: "first", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	run.ID = "delivery-second"
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "duplicate-delivery"), ExpectedVersion: 3, RoutineRun: &run, Receipt: pa.Receipt{ID: "second", State: pa.EffectDelivered}}); err == nil {
		t.Fatal("SQLite admitted a duplicate delivery for one routine")
	}
	if err = s.DeleteBot(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM project_assistant_routine_hooks`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Bot deletion retained hook secret", count, err)
	}
}
