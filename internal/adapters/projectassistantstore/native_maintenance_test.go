package projectassistantstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestNativeMaintenanceJournalAtomicallyFencesRecoveryAndReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.sqlite") + "?_pragma=busy_timeout(5000)"
	open := func() (*sql.DB, *NativeMaintenanceJournal) {
		t.Helper()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		journal, err := NewNativeMaintenanceJournal(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return db, journal
	}
	dbA, a := open()
	dbB, b := open()
	intent := app.NativeMaintenanceIntent{
		ID: "qualification-1", PrincipalID: "person", ReaderID: "reader", AuthorizationRevision: "grant",
		CLIVersion: "1", AdapterVersion: "1", Evidence: "synthetic",
		Binding: pa.ExecutionBinding{NativeSessionID: "original-native", Agent: pa.Codex, MemoryRevision: 1,
			Fence: pa.Fence{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, ConversationID: "conversation", BindingID: "first", Generation: 1,
				Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}}},
	}
	start := make(chan struct{})
	results := make(chan bool, 2)
	var pending sync.WaitGroup
	for _, journal := range []*NativeMaintenanceJournal{a, b} {
		pending.Add(1)
		go func() {
			defer pending.Done()
			<-start
			record, added, err := journal.Reserve(ctx, intent)
			if err != nil || record.Intent != intent || record.State != "" {
				t.Errorf("contending reservation: %+v %v", record, err)
			}
			results <- added
		}()
	}
	close(start)
	pending.Wait()
	if first, second := <-results, <-results; first == second {
		t.Fatal("exactly one independent connection must win the reservation", first, second)
	}

	conflict := intent
	conflict.AuthorizationRevision = "another-grant"
	if _, _, err := b.Reserve(ctx, conflict); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal("same-key payload substitution was admitted", err)
	}
	other := intent
	other.ID, other.Binding.Fence.BindingID = "new-id", "new-binding"
	if _, _, err := b.Reserve(ctx, other); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("competing key escaped the pending barrier", err)
	}

	for _, db := range []*sql.DB{dbA, dbB} {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	_, restarted := open()
	if n, err := restarted.RecoverInterrupted(ctx); err != nil || n != 1 {
		t.Fatal("durable pending intent did not survive reopen", n, err)
	}
	if n, err := restarted.RecoverInterrupted(ctx); err != nil || n != 0 {
		t.Fatal("recover was not idempotent", n, err)
	}
	late, err := restarted.Resolve(ctx, app.NativeMaintenanceRecord{Intent: intent, NativeID: "late", State: app.NativeCompactionCompleted})
	if err != nil || late.State != app.NativeCompactionUnknown || late.NativeID != "" {
		t.Fatal("late terminal replaced durable unknown", late, err)
	}
	if got, added, e := restarted.Reserve(ctx, intent); e != nil || added || got != late {
		t.Fatal("same-key unknown was not preserved", got, added, e)
	}
	if _, _, e := restarted.Reserve(ctx, other); pa.Code(e) != pa.ExecutionEffectUnknown {
		t.Fatal("different key bypassed recovery", e)
	}
}

func TestNativeMaintenanceJournalLeavesInstalledSchemaUntouched(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = app.InstallSchemas(ctx, db, Installer{}, GroupInstaller{}); err != nil {
		t.Fatal(err)
	}
	if _, err = NewNativeMaintenanceJournal(ctx, db); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("maintenance wrote to the feature database", err)
	}
	var exists bool
	err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='native_maintenance')`).Scan(&exists)
	if err != nil || exists {
		t.Fatal("fixture journal leaked into installed schema", exists, err)
	}
}
