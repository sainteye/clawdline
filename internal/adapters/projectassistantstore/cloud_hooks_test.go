package projectassistantstore

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestCloudHooksRevisionSurvivesReopenAndLostAcknowledgment(t *testing.T) {
	db, s, path := testDB(t)
	id := bh.Identity{AccountID: "account", MachineID: "machine"}
	if set, _, err := s.PrepareCloudHooks(testContext, id, nil); err != nil || set.Revision != 0 {
		t.Fatal("new empty source enrolled", set.Revision, err)
	}
	hooks := []bh.Hook{{ID: "z", Key: strings.Repeat("b", 64), Enabled: true}, {ID: "a", Key: strings.Repeat("a", 64), Enabled: true}}
	set, digest, err := s.PrepareCloudHooks(testContext, id, hooks)
	if err != nil || set.Revision != 1 || set.Hooks[0].ID != "a" {
		t.Fatal(set.Revision, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = New(db)
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	hooks[0], hooks[1] = hooks[1], hooks[0]
	retry, digest2, err := s.PrepareCloudHooks(testContext, id, hooks)
	if err != nil || retry.Revision != 1 || digest2 != digest {
		t.Fatal("lost response minted another revision", retry.Revision, err)
	}
	hooks[0].Key = strings.Repeat("c", 64)
	rotated, _, err := s.PrepareCloudHooks(testContext, id, hooks)
	if err != nil || rotated.Revision != 2 {
		t.Fatal("rotation reused revision", err)
	}
	hooks[0].Enabled = false
	paused, _, err := s.PrepareCloudHooks(testContext, id, hooks)
	if err != nil || paused.Revision != 3 {
		t.Fatal("pause reused revision", err)
	}
	withdrawn, _, err := s.PrepareCloudHooks(testContext, id, nil)
	if err != nil || withdrawn.Revision != 4 || len(withdrawn.Hooks) != 0 {
		t.Fatal("withdrawal omitted", err)
	}
	if _, err = db.Exec(`UPDATE project_assistant_cloud_hooks SET revision=?`, bh.MaxRevision); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PrepareCloudHooks(testContext, id, hooks); bh.Code(err) != "webhook_revision_conflict" {
		t.Fatal("revision overflow did not refuse", err)
	}
}

func TestCloudHookPendingIsDurableAndNeverInExport(t *testing.T) {
	db, s, path := testDB(t)
	b := seed(t, s, "private-hook")
	d, err := pa.SetRoutineSchedule(routineFixture(b), pa.Routine{}, []pa.RoutineTrigger{{Type: "webhook"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "save"), ExpectedVersion: 1, Routine: &d, Receipt: pa.Receipt{ID: "saved", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	candidates, err := s.CloudHookCandidates(testContext, b.Key.MachineID)
	if err != nil || len(candidates) != 1 {
		t.Fatal("candidate read", err)
	}
	id := bh.Identity{AccountID: "account", MachineID: b.Key.MachineID}
	if _, _, err = s.PrepareCloudHooks(testContext, id, []bh.Hook{candidates[0].Hook}); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginCloudHook(testContext, id, "queue-original"); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginCloudHook(testContext, id, "queue-new"); err == nil {
		t.Fatal("second in-flight delivery bypassed barrier")
	}
	ack := bh.Ack{Protocol: bh.Protocol, LeaseToken: "private-lease-token", Status: 409, Code: "webhook_outcome_unknown"}
	if err = s.FinishCloudHook(testContext, id, "queue-original", ack); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = New(db)
	pending, err := s.CloudHookPending(testContext, id)
	if err != nil || pending.ID != "queue-original" || pending.Ack == nil || *pending.Ack != ack {
		t.Fatal("ack did not survive restart", err)
	}
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(archive)
	for _, secret := range []string{candidates[0].Hook.Key, ack.LeaseToken, "queue-original"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("bridge private data entered archive")
		}
	}
	if err = s.ClearCloudHook(testContext, id, "queue-original"); err != nil {
		t.Fatal(err)
	}
	if pending, err = s.CloudHookPending(testContext, id); err != nil || pending != nil {
		t.Fatal("pending not cleared", err)
	}
}

func TestCloudHookSchemaRefusesFutureVersionTransactionally(t *testing.T) {
	db, s, _ := testDB(t)
	if _, err := db.Exec(`PRAGMA ignore_check_constraints=1; UPDATE project_assistant_cloud_hook_schema SET version=2; DROP TABLE project_assistant_cloud_hooks`); err != nil {
		t.Fatal(err)
	}
	if err := (Installer{}).Install(testContext, db); bh.Code(err) != "webhook_schema_unsupported" {
		t.Fatal("future bridge schema accepted", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='project_assistant_cloud_hooks'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("future schema modified", count, err)
	}
	if ok, err := s.CloudHooksInstalled(testContext); ok || err == nil {
		t.Fatal("future bridge usable")
	}
}

func TestCloudHookCandidatesNeverTruncateTheFullSet(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "capacity")
	d, err := pa.SetRoutineSchedule(routineFixture(b), pa.Routine{}, []pa.RoutineTrigger{{Type: "webhook"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i <= bh.MaxHooks; i++ {
		d.ID = fmt.Sprintf("hook-%04d", i)
		raw, _ := json.Marshal(d)
		if _, err = tx.Exec(`INSERT INTO project_assistant_routines(machine,bot,id,deleted,data) VALUES(?,?,?,0,?)`, b.Key.MachineID, b.Key.BotID, d.ID, raw); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO project_assistant_routine_hooks(machine,bot,id,secret) VALUES(?,?,?,?)`, b.Key.MachineID, b.Key.BotID, d.ID, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CloudHookCandidates(testContext, b.Key.MachineID); bh.Code(err) != "webhook_hooks_capacity" {
		t.Fatal("oversize set was silently truncated", err)
	}
}
