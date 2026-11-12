package projectassistantstore

import (
	"encoding/json"
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestCompleteArchiveRoundTripIntoEmptyDatabase(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectUnknown)
	if _, err := s.Save(testContext, memoryWrite(b, 1, "fact", "message", "forget this")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(testContext, memoryWrite(b, 3, "safe", "safe-source", "keep this")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read"), pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "alice", MessageSeq: 1}); err != nil {
		t.Fatal(err)
	}
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(archive)
	if err != nil {
		t.Fatal(err)
	}
	var transferred Archive
	if err = json.Unmarshal(raw, &transferred); err != nil {
		t.Fatal(err)
	}
	_, restored, _ := testDB(t)
	if err = restored.RestoreArchive(testContext, transferred); err != nil {
		t.Fatal(err)
	}
	after, err := restored.ExportArchive(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(archive, after) {
		t.Fatalf("roundtrip mismatch: %v\nbefore=%+v\nafter=%+v", err, archive, after)
	}
	r, err := restored.Receipt(testContext, command(b.Key, pa.SendOperation, "send"))
	if err != nil || r.State != pa.EffectUnknown {
		t.Fatalf("unknown lost %+v %v", r, err)
	}
	if _, err = restored.Commit(testContext, app.Mutation{Command: r.Command, ExpectedVersion: 999}); err != nil {
		t.Fatal(err)
	}
	snap, err := restored.Snapshot(testContext, b.Key)
	if err != nil || snap.Text != "keep this" {
		t.Fatalf("context %+v %v", snap, err)
	}
	if err = restored.Restore(testContext, transferred.Data); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("overwrite allowed %v", err)
	}
}

func TestRestoreRejectsVersionCorruptionAndForeignRelations(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectDelivered)
	archive, err := s.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*Archive)
	}{
		{"future schema", func(a *Archive) { a.Data.SchemaVersion++ }},
		{"future contract", func(a *Archive) { a.Data.ContractVersion = "future" }},
		{"integrity", func(a *Archive) { a.Manifest.SHA256 = "wrong" }},
		{"foreign turn", func(a *Archive) { a.Data.Turns[0].Fence.Key.BotID = "other" }},
		{"foreign binding", func(a *Archive) { a.Data.Bindings[0].Fence.Project.CanonicalID = "other" }},
		{"foreign permission", func(a *Archive) { a.Data.Permissions[0].Fence.Key.BotID = "other" }},
		{"foreign work", func(a *Archive) { a.Data.WorkLinks[0].TurnID = "absent" }},
		{"foreign event", func(a *Archive) { a.Data.Events[1].Message.Fence.Key.BotID = "other" }},
		{"foreign receipt", func(a *Archive) { a.Data.Receipts[0].Command.Key.BotID = "other" }},
		{"duplicate facts", func(a *Archive) { a.Data.Turns = append(a.Data.Turns, a.Data.Turns[0]) }},
		{"turn message owned by another turn", func(a *Archive) {
			turn := a.Data.Turns[0]
			turn.ID = "other-turn"
			a.Data.Turns = append(a.Data.Turns, turn)
		}},
	}
	raw, _ := json.Marshal(archive)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var changed Archive
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			test.edit(&changed)
			_, target, _ := testDB(t)
			if test.name == "integrity" {
				err = target.RestoreArchive(testContext, changed)
			} else {
				err = target.Restore(testContext, changed.Data)
			}
			if err == nil {
				t.Fatal("restore accepted invalid data")
			}
			if _, err = target.Bot(testContext, b.Key); pa.Code(err) != pa.NotFound {
				t.Fatalf("restore leaked Bot %v", err)
			}
		})
	}
}

func TestRestoreLateCollisionRollsBack(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	data, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	db, target, _ := testDB(t)
	if _, err = db.Exec(`INSERT INTO project_assistant_events(seq,machine,bot,event_id,data) VALUES(1,'other','other','collision','{}')`); err != nil {
		t.Fatal(err)
	}
	if err = target.Restore(testContext, data); err == nil {
		t.Fatal("collision accepted")
	}
	if _, err = target.Bot(testContext, b.Key); pa.Code(err) != pa.NotFound {
		t.Fatalf("restore leaked Bot %v", err)
	}
}
