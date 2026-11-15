package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	host "github.com/sainteye/clawdline/internal/adapters/store"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

var testContext = context.Background()

func testDB(t *testing.T) (*sql.DB, *Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "feature.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	return db, New(db), path
}
func command(key pa.BotKey, op pa.Operation, id string) pa.Command {
	return pa.Command{Key: key, Operation: op, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(string(op)+"/"+id)))}
}
func seed(t *testing.T, s *Store, id string) pa.Bot {
	t.Helper()
	b := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: id}, Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}, ConversationID: "chat/" + id, Name: "Bot " + id, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Be helpful"}, Version: 1, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	cmd := command(b.Key, pa.CreateOperation, "create/"+id)
	summary := pa.BotSummary{Bot: b}
	_, err := s.Commit(testContext, app.Mutation{Command: cmd, Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "created/" + id, State: pa.EffectDelivered, ResourceID: id}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "created/" + id, Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.BotCreated, Bot: &summary}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func populate(t *testing.T, s *Store, b pa.Bot, state pa.EffectState) pa.Fence {
	t.Helper()
	f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "binding", Generation: 1}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, BindingID: f.BindingID, Generation: 1, Version: 2, MemoryRevision: 1, ActiveTurnID: "turn"}
	binding := pa.ExecutionBinding{Fence: f, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
	turn := pa.Turn{ID: "turn", Fence: f, MessageID: "message", State: pa.TurnRunning, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "send"}
	m := pa.Message{ID: "message", Fence: f, Seq: 1, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Hello", At: b.CreatedAt}
	p := pa.PermissionOffer{ID: "permission", Fence: f, TurnID: turn.ID, NativeRequestID: "native", State: pa.PermissionPending, ExpiresAt: b.CreatedAt.Add(time.Hour), Options: []pa.PermissionOption{{ID: "reject", Kind: pa.RejectOnce}}}
	w := pa.WorkLink{ID: "work", Fence: f, TurnID: turn.ID, TaskID: "task", State: pa.WorkAccepted}
	_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "send"), ExpectedVersion: 1, Conversation: &c, Binding: &binding, Turn: &turn, Messages: []pa.Message{m}, Permissions: []pa.PermissionOffer{p}, WorkLinks: []pa.WorkLink{w}, Receipt: pa.Receipt{ID: "sent", State: state, ResourceID: "turn"}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "message", Fence: f, Kind: pa.MessageCreated, Message: &m}}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func requireCode(t *testing.T, err error, code pa.ErrorCode) {
	t.Helper()
	if pa.Code(err) != code {
		t.Fatalf("got %v (%s), want %s", err, pa.Code(err), code)
	}
}

func TestInstallerPreservesHostAndReinstalls(t *testing.T) {
	db, s, path := testDB(t)
	if _, err := db.Exec(`PRAGMA user_version=47; CREATE TABLE host_work(id TEXT PRIMARY KEY,value TEXT); INSERT INTO host_work VALUES('session','running')`); err != nil {
		t.Fatal(err)
	}
	b := seed(t, s, "one")
	for i := 0; i < 2; i++ {
		if err := (Installer{}).Install(testContext, db); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 47 {
		t.Fatalf("host version %d: %v", version, err)
	}
	if _, err := db.Exec(`UPDATE host_work SET value='continued' WHERE id='session'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = (Installer{}).Install(testContext, reopened); err != nil {
		t.Fatal(err)
	}
	got, err := New(reopened).Bot(testContext, b.Key)
	if err != nil || got.Name != b.Name {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	var value string
	if err = reopened.QueryRow(`SELECT value FROM host_work WHERE id='session'`).Scan(&value); err != nil || value != "continued" {
		t.Fatalf("host work %q %v", value, err)
	}
}
func TestInstallerRejectsFutureSchema(t *testing.T) {
	db, _, _ := testDB(t)
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=?`, pa.SchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	requireCode(t, (Installer{}).Install(testContext, db), pa.Unsupported)
}

// databaseFacts compares the actual SQLite values (including opaque blobs),
// schema objects and sequence allocation, rather than a decoded projection.
func databaseFacts(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	rows, err := db.Query(`SELECT name,sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for rows.Next() {
		var name, definition string
		if err = rows.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		out["@schema"] = append(out["@schema"], definition)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	rows, err = db.Query(`SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, name := range names {
		rows, err = db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		out[name] = []string{}
		for rows.Next() {
			values, ptrs := make([]any, len(cols)), make([]any, len(cols))
			for i := range ptrs {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out[name] = append(out[name], fmt.Sprintf("%#v", values))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	out["@host_version"] = []string{fmt.Sprint(version)}
	return out
}

func legacyStoreFixture(t *testing.T) (*sql.DB, *Store, string, pa.Bot) {
	t.Helper()
	db, s, path := testDB(t)
	b := seed(t, s, "legacy")
	populate(t, s, b, pa.EffectUnknown)
	if _, err := s.Save(testContext, memoryWrite(b, 1, "fact", "message", "forgotten fact")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(testContext, memoryWrite(b, 3, "safe", "safe-source", "retained fact")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read"), pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "reader", MessageSeq: 1}); err != nil {
		t.Fatal(err)
	}
	current, err := s.Bot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	d := routineFixture(b)
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "routine"), ExpectedVersion: current.Version, Routine: &d, Receipt: pa.Receipt{ID: "routine", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	run := pa.RoutineRun{Source: "schedule", ID: "wake", RoutineID: d.ID, Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, State: pa.TurnQueued, CreatedAt: time.Unix(1000, 0), TimeoutSeconds: 3600}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "wake"), ExpectedVersion: current.Version + 1, RoutineRun: &run, Receipt: pa.Receipt{ID: "wake", State: pa.EffectAccepted}}); err != nil {
		t.Fatal(err)
	}
	// These are v1-compatible facts. No v2 fallback fields are manufactured.
	if _, err = db.Exec(`UPDATE project_assistant_schema SET version=1; PRAGMA user_version=47; CREATE TABLE host_state(id TEXT PRIMARY KEY,data BLOB); INSERT INTO host_state VALUES('session',X'0001FF')`); err != nil {
		t.Fatal(err)
	}
	return db, s, path, b
}

func TestInstallerMigratesV1FactsWithoutRewritingAfterReopen(t *testing.T) {
	db, s, path, b := legacyStoreFixture(t)
	beforeExport, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	// Even bytes the current reader does not understand must survive migration.
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.opaque_fixture','preserve me')`); err != nil {
		t.Fatal(err)
	}
	before := databaseFacts(t, db)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.SetMaxOpenConns(1)
	defer reopened.Close()
	for i := 0; i < 2; i++ {
		if err = (Installer{}).Install(testContext, reopened); err != nil {
			t.Fatal(err)
		}
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	verified.SetMaxOpenConns(1)
	defer verified.Close()
	var version int
	if err = verified.QueryRow(`SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("migration version=%d, want 2: %v", version, err)
	}
	after := databaseFacts(t, verified)
	delete(before, "project_assistant_schema")
	delete(after, "project_assistant_schema")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("migration rewrote SQLite facts or host state")
	}
	afterExport, err := New(verified).Export(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(beforeExport, afterExport) {
		t.Fatalf("migration changed readable history: %v", err)
	}
}

func TestInstallerMetadataFailureRollsBackDDLAndFacts(t *testing.T) {
	for _, action := range []string{"ABORT,'fixture metadata failure'", "IGNORE"} {
		t.Run(action, func(t *testing.T) {
			db, _, _, _ := legacyStoreFixture(t)
			if _, err := db.Exec(`DROP INDEX project_assistant_roster_machine; CREATE TRIGGER reject_migration BEFORE UPDATE ON project_assistant_schema BEGIN SELECT RAISE(` + action + `); END`); err != nil {
				t.Fatal(err)
			}
			before := databaseFacts(t, db)
			if err := (Installer{}).Install(testContext, db); err == nil {
				t.Fatal("failed metadata update accepted")
			}
			if !reflect.DeepEqual(before, databaseFacts(t, db)) {
				t.Fatal("failed migration leaked DDL, metadata or facts")
			}
		})
	}
}

func TestInstallerUnsupportedSchemasAreUntouched(t *testing.T) {
	for _, version := range []int{0, 3, 99} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, _, _ := testDB(t)
			if _, err := db.Exec(`UPDATE project_assistant_schema SET version=?`, version); err != nil {
				t.Fatal(err)
			}
			// Refusal must not repair or backfill even a missing known index.
			if _, err := db.Exec(`DROP INDEX project_assistant_roster_machine`); err != nil {
				t.Fatal(err)
			}
			before := databaseFacts(t, db)
			requireCode(t, (Installer{}).Install(testContext, db), pa.Unsupported)
			if !reflect.DeepEqual(before, databaseFacts(t, db)) {
				t.Fatal("unsupported schema mutated")
			}
		})
	}
}

func TestExistingHostStoreContinuesAcrossFeatureReinstall(t *testing.T) {
	dir := t.TempDir()
	original, err := host.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = original.CreateScheduleFile(testContext, "original", []byte("original schedule"), now, now); err != nil {
		original.Close()
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, host.DBFile))
	if err != nil {
		original.Close()
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = (Installer{}).Install(testContext, db); err != nil {
		original.Close()
		t.Fatal(err)
	}
	b := seed(t, New(db), "retained")
	if err = original.CreateScheduleFile(testContext, "continued", []byte("continued schedule"), now, now); err != nil {
		original.Close()
		t.Fatal(err)
	}
	if err = original.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := host.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	schedules, err := reopened.ScheduleFiles(testContext)
	if err != nil || len(schedules) != 2 {
		t.Fatalf("original host schedule operations %+v %v", schedules, err)
	}
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	got, err := New(db).Bot(testContext, b.Key)
	if err != nil || got.Key != b.Key {
		t.Fatalf("reinstall lost feature %+v %v", got, err)
	}
}

// A real v1 installation has no fallback rest table. Check the combined
// migration, including rollback of that newly created table on metadata failure.
func TestInstallerAddsFallbackRestTableAtomically(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint("metadata_blocked=", blocked), func(t *testing.T) {
			db, _, _, _ := legacyStoreFixture(t)
			if _, err := db.Exec(`DROP TABLE project_assistant_agent_rests`); err != nil {
				t.Fatal(err)
			}
			if blocked {
				if _, err := db.Exec(`CREATE TRIGGER block_rest_migration BEFORE UPDATE ON project_assistant_schema BEGIN SELECT RAISE(IGNORE); END`); err != nil {
					t.Fatal(err)
				}
			}
			before := databaseFacts(t, db)
			err := (Installer{}).Install(testContext, db)
			if blocked {
				if err == nil {
					t.Fatal("ignored metadata update was accepted")
				}
				if !reflect.DeepEqual(before, databaseFacts(t, db)) {
					t.Fatal("failed migration leaked the rest table or changed v1 facts")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var version, count int
			if err = db.QueryRow(`SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil || version != 2 {
				t.Fatal(version, err)
			}
			if err = db.QueryRow(`SELECT count(*) FROM project_assistant_agent_rests`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
			var definition string
			if err = db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='project_assistant_agent_rests'`).Scan(&definition); err != nil {
				t.Fatal(err)
			}
			after := databaseFacts(t, db)
			delete(before, "project_assistant_schema")
			delete(after, "project_assistant_schema")
			delete(after, "project_assistant_agent_rests")
			var unchanged []string
			for _, statement := range after["@schema"] {
				if statement != definition {
					unchanged = append(unchanged, statement)
				}
			}
			after["@schema"] = unchanged
			if !reflect.DeepEqual(before, after) {
				t.Fatal("migration changed facts beyond the new rest table and version marker")
			}
		})
	}
}
