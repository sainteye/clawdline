package projectassistantstore

import (
	"encoding/json"
	"fmt"
	"os"
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
			if err != nil || out.Data.SchemaVersion != pa.SchemaVersion || out.Manifest.SchemaVersion != pa.SchemaVersion {
				t.Fatalf("restore did not export schema 2: %+v %v", out.Manifest, err)
			}
			want := transferred.Data
			want.SchemaVersion = pa.SchemaVersion
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
		{"future", pa.SchemaVersion + 1, pa.SchemaVersion + 1, false, pa.Unsupported},
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

// Produced by the actual schema-1 types, installer and exporter at 7f4f1db83,
// before fallback fields existed. Recreating v1 under current types misses
// accidental additions to the canonical bytes covered by the old hash.
func TestActualSchemaOneArchiveRetainsItsOriginalCanonicalHash(t *testing.T) {
	raw, err := os.ReadFile("testdata/schema1-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy Archive
	if err = json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Manifest.SchemaVersion != 1 || legacy.Manifest.SHA256 != "0e5767e04de4a4f35c4fbac30e097d09363d0b03e7380e1c61bb701f5f7d7896" {
		t.Fatal("baseline archive changed")
	}
	_, target, _ := testDB(t)
	if err = target.RestoreArchive(testContext, legacy); err != nil {
		t.Fatalf("actual v1 archive rejected: %v", err)
	}
	current, err := target.Export(testContext, legacy.Data.Bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	want := legacy.Data
	want.SchemaVersion = pa.SchemaVersion
	if !reflect.DeepEqual(want, current) {
		t.Fatal("restoring actual v1 archive changed its facts")
	}
	receipt, err := target.Receipt(testContext, command(want.Bot.Key, pa.SendOperation, "send"))
	if err != nil || receipt.State != pa.EffectUnknown {
		t.Fatalf("legacy unknown receipt changed: %+v %v", receipt, err)
	}
}

// Produced by fd9a9629669c6df00839a9a048f079138b0a5836's actual v2
// types/installer/exporter, before copying schema3 code into the staging tree.
const actualSchemaTwoArchive = `{
  "data": {
    "ContractVersion": "project-assistants.v1",
    "SchemaVersion": 2,
    "Bot": {
      "key": {
        "machine_id": "machine",
        "bot_id": "baseline-v2"
      },
      "project": {
        "machine_id": "machine",
        "project_id": "project",
        "canonical_id": "canonical",
        "kind": "repository"
      },
      "conversation_id": "chat/baseline-v2",
      "name": "Bot baseline-v2",
      "appearance": {
        "shape": "blob",
        "color": "#e39567"
      },
      "agent": "claude",
      "permission_policy": "ask",
      "instructions": {
        "revision": 1,
        "text": "Be helpful",
        "role_snapshot_ref": ""
      },
      "version": 1,
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    },
    "Conversation": {
      "key": {
        "machine_id": "machine",
        "bot_id": "baseline-v2"
      },
      "id": "chat/baseline-v2",
      "generation": 0,
      "binding_id": "",
      "last_message_seq": 0,
      "memory_revision": 1,
      "active_turn_id": "",
      "version": 1
    },
    "Messages": null,
    "Turns": [],
    "Bindings": [],
    "Permissions": [],
    "WorkLinks": [],
    "Memory": {
      "key": {
        "machine_id": "machine",
        "bot_id": "baseline-v2"
      },
      "project": {
        "machine_id": "machine",
        "project_id": "project",
        "canonical_id": "canonical",
        "kind": "repository"
      },
      "revision": 1,
      "forgotten_through": 0,
      "forgotten_source_ids": null
    },
    "MemoryEntries": [],
    "Context": {
      "memory": {
        "key": {
          "machine_id": "machine",
          "bot_id": "baseline-v2"
        },
        "project": {
          "machine_id": "machine",
          "project_id": "project",
          "canonical_id": "canonical",
          "kind": "repository"
        },
        "revision": 1,
        "forgotten_through": 0,
        "forgotten_source_ids": null
      },
      "text": "",
      "sources": [],
      "provenance_complete": true
    },
    "Events": [
      {
        "version": "project-assistants.v1",
        "id": "created/baseline-v2",
        "seq": 1,
        "fence": {
          "key": {
            "machine_id": "machine",
            "bot_id": "baseline-v2"
          },
          "project": {
            "machine_id": "machine",
            "project_id": "project",
            "canonical_id": "canonical",
            "kind": "repository"
          },
          "conversation_id": "chat/baseline-v2",
          "binding_id": "",
          "generation": 0
        },
        "kind": "bot_created",
        "at": "0001-01-01T00:00:00Z",
        "bot": {
          "bot": {
            "key": {
              "machine_id": "machine",
              "bot_id": "baseline-v2"
            },
            "project": {
              "machine_id": "machine",
              "project_id": "project",
              "canonical_id": "canonical",
              "kind": "repository"
            },
            "conversation_id": "chat/baseline-v2",
            "name": "Bot baseline-v2",
            "appearance": {
              "shape": "blob",
              "color": "#e39567"
            },
            "agent": "claude",
            "permission_policy": "ask",
            "instructions": {
              "revision": 1,
              "text": "Be helpful",
              "role_snapshot_ref": ""
            },
            "version": 1,
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z"
          },
          "latest_message": "",
          "activity": "",
          "activity_at": "0001-01-01T00:00:00Z",
          "unread_count": 0,
          "read_cursor": {
            "key": {
              "machine_id": "",
              "bot_id": ""
            },
            "conversation_id": "",
            "reader_id": "",
            "message_seq": 0
          },
          "pending_permissions": 0,
          "version": 0,
          "source": {
            "machine_id": "",
            "state": "",
            "readable": false,
            "writable": false,
            "fresh": false,
            "observed_at": "0001-01-01T00:00:00Z",
            "access": "",
            "capabilities": null
          }
        }
      }
    ],
    "Receipts": [
      {
        "command": {
          "key": {
            "machine_id": "machine",
            "bot_id": "baseline-v2"
          },
          "operation": "create",
          "idempotency_key": "create/baseline-v2",
          "content_hash": "939ca6e4d1b78be1211ae995172ff209c613a3fccbdbdd9f05e0a9fbd1c85c89"
        },
        "id": "created/baseline-v2",
        "state": "delivered",
        "resource_id": "baseline-v2",
        "event_seq": 1
      }
    ],
    "ReadCursors": []
  },
  "manifest": {
    "contract_version": "project-assistants.v1",
    "schema_version": 2,
    "sha256": "3b20cd14d176ef2295aee7c81a99a9a4811d18ce189ddaca4de8099743df1067"
  }
}`

func TestActualSchemaTwoArchiveRetainsItsOriginalCanonicalHash(t *testing.T) {
	var in Archive
	if err := json.Unmarshal([]byte(actualSchemaTwoArchive), &in); err != nil {
		t.Fatal(err)
	}
	if in.Manifest.SHA256 != "3b20cd14d176ef2295aee7c81a99a9a4811d18ce189ddaca4de8099743df1067" {
		t.Fatal("old producer receipt changed")
	}
	sum, err := digest(in.Data)
	if err != nil || sum != in.Manifest.SHA256 {
		t.Fatal("v2 empty fields changed canonical JSON", sum, err)
	}
	_, s, _ := testDB(t)
	if err = s.RestoreArchive(testContext, in); err != nil {
		t.Fatal(err)
	}
	out, err := s.ExportArchive(testContext, in.Data.Bot.Key)
	if err != nil || out.Data.SchemaVersion != pa.SchemaVersion {
		t.Fatal(out.Manifest, err)
	}
}
