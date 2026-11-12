package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
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
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=2`); err != nil {
		t.Fatal(err)
	}
	requireCode(t, (Installer{}).Install(testContext, db), pa.Unsupported)
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
