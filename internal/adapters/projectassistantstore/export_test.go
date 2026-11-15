package projectassistantstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestLegacyV1ArchiveRestoresAllFactsAndExportsV2(t *testing.T) {
	_, source, _, b := legacyStoreFixture(t)
	legacy, err := source.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Data.SchemaVersion, legacy.Manifest.SchemaVersion = 1, 1
	legacy.Manifest.SHA256, err = digest(legacy.Data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var transferred Archive
	if err = json.Unmarshal(raw, &transferred); err != nil {
		t.Fatal(err)
	}
	for _, archive := range []bool{false, true} {
		t.Run(fmt.Sprint("archive=", archive), func(t *testing.T) {
			_, target, _ := testDB(t)
			if archive {
				err = target.RestoreArchive(testContext, transferred)
			} else {
				err = target.Restore(testContext, transferred.Data)
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := target.ExportArchive(testContext, b.Key)
			if err != nil || out.Data.SchemaVersion != 2 || out.Manifest.SchemaVersion != 2 {
				t.Fatalf("restore did not export schema 2: %+v %v", out.Manifest, err)
			}
			want := transferred.Data
			want.SchemaVersion = 2
			if !reflect.DeepEqual(want, out.Data) {
				t.Fatal("legacy facts changed during restore")
			}
			if sum, err := digest(out.Data); err != nil || sum != out.Manifest.SHA256 {
				t.Fatalf("schema 2 manifest hash: %v", err)
			}
			r, err := target.Receipt(testContext, command(b.Key, pa.SendOperation, "send"))
			if err != nil || r.ID != "sent" || r.State != pa.EffectUnknown {
				t.Fatalf("unknown receipt changed: %+v %v", r, err)
			}
			replayed, err := target.Commit(testContext, app.Mutation{Command: r.Command, ExpectedVersion: 999})
			if err != nil || replayed != *r {
				t.Fatalf("unknown receipt replay changed: %+v %v", replayed, err)
			}
			requireCode(t, target.RestoreArchive(testContext, transferred), pa.IdempotencyConflict)
		})
	}
}

func TestArchiveVersionAgreementAndIntegrityRefuseAtomically(t *testing.T) {
	_, source, _ := testDB(t)
	b := seed(t, source, "versions")
	valid, err := source.ExportArchive(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name           string
		manifest, data int
		corrupt        bool
		code           pa.ErrorCode
	}{
		{"legacy manifest with current data", 1, 2, false, pa.Unsupported},
		{"current manifest with legacy data", 2, 1, false, pa.Unsupported},
		{"zero", 0, 0, false, pa.Unsupported},
		{"future", 3, 3, false, pa.Unsupported},
		{"future manifest", 3, 2, false, pa.Unsupported},
		{"future data", 2, 3, false, pa.Unsupported},
		{"legacy integrity", 1, 1, true, pa.Invalid},
		{"current integrity", 2, 2, true, pa.Invalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := valid
			in.Manifest.SchemaVersion, in.Data.SchemaVersion = test.manifest, test.data
			in.Manifest.SHA256, err = digest(in.Data)
			if err != nil {
				t.Fatal(err)
			}
			if test.corrupt {
				in.Data.Bot.Name = "damaged transfer"
			}
			db, target, _ := testDB(t)
			before := databaseFacts(t, db)
			requireCode(t, target.RestoreArchive(testContext, in), test.code)
			if !reflect.DeepEqual(before, databaseFacts(t, db)) {
				t.Fatal("rejected archive changed database")
			}
		})
	}
}

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
		{"future schema", func(a *Archive) { a.Data.SchemaVersion = pa.SchemaVersion + 1 }},
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
	for _, version := range []int{1, pa.SchemaVersion} {
		archive.Data.SchemaVersion, archive.Manifest.SchemaVersion = version, version
		raw, _ := json.Marshal(archive)
		for _, test := range tests {
			t.Run(fmt.Sprintf("v%d/%s", version, test.name), func(t *testing.T) {
				var changed Archive
				if err := json.Unmarshal(raw, &changed); err != nil {
					t.Fatal(err)
				}
				test.edit(&changed)
				db, target, _ := testDB(t)
				before := databaseFacts(t, db)
				if test.name != "integrity" {
					changed.Manifest.SHA256, err = digest(changed.Data)
					if err != nil {
						t.Fatal(err)
					}
				}
				err = target.RestoreArchive(testContext, changed)
				if err == nil {
					t.Fatal("restore accepted invalid data")
				}
				if _, err = target.Bot(testContext, b.Key); pa.Code(err) != pa.NotFound {
					t.Fatalf("restore leaked Bot %v", err)
				}
				if !reflect.DeepEqual(before, databaseFacts(t, db)) {
					t.Fatal("rejected restore changed existing facts")
				}
			})
		}
	}
}

func TestRestoreLateCollisionRollsBack(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	data, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, pa.SchemaVersion} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			data.SchemaVersion = version
			db, target, _ := testDB(t)
			if _, err = db.Exec(`INSERT INTO project_assistant_events(seq,machine,bot,event_id,data) VALUES(1,'other','other','collision','{}')`); err != nil {
				t.Fatal(err)
			}
			before := databaseFacts(t, db)
			if err = target.Restore(testContext, data); err == nil {
				t.Fatal("collision accepted")
			}
			if !reflect.DeepEqual(before, databaseFacts(t, db)) {
				t.Fatal("late restore collision changed existing facts")
			}
		})
	}
}
