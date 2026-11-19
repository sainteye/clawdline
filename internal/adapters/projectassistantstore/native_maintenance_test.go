package projectassistantstore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
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

func maintenanceReaderIntent() app.NativeMaintenanceIntent {
	return app.NativeMaintenanceIntent{
		ID: "sandbox-intent", PrincipalID: "person", ReaderID: "reader", AuthorizationRevision: "grant",
		CLIVersion: "1", AdapterVersion: "1", Evidence: "synthetic",
		Binding: pa.ExecutionBinding{NativeSessionID: "held-native", Agent: pa.Codex, MemoryRevision: 1,
			Fence: pa.Fence{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, ConversationID: "conversation", BindingID: "initial", Generation: 1,
				Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}}},
	}
}

func establishMaintenanceHistory(t *testing.T, state string) (string, *sql.DB, *NativeMaintenanceJournal) {
	t.Helper()
	dir := filepath.Join(state, nativeMaintenanceDir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, nativeMaintenanceMarker), []byte(nativeMaintenanceFormat), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, nativeMaintenanceDB)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	journal, err := NewNativeMaintenanceJournal(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return path, db, journal
}

func TestNativeMaintenanceHistoryReaderBlocksPendingWALAndRecoveredHistory(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	path, db, journal := establishMaintenanceHistory(t, state)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	main := read(path)
	intent := maintenanceReaderIntent()
	before := NewNativeMaintenanceHistoryReader(state)
	if err := before.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if _, inserted, err := journal.Reserve(ctx, intent); !inserted || err != nil {
		t.Fatal("could not stage a synthetic intent", inserted, err)
	}
	if !bytes.Equal(read(path), main) {
		t.Fatal("the pending intent should only be available through WAL")
	}
	resume := intent.Binding
	resume.Fence.Generation++
	resume.Fence.BindingID = "replacement-binding"
	resume.MemoryRevision++
	for _, reader := range []*NativeMaintenanceHistoryReader{before, NewNativeMaintenanceHistoryReader(state)} {
		if err := reader.CheckNativeHistory(ctx, resume); pa.Code(err) != pa.ExecutionEffectUnknown {
			t.Fatal("pending WAL intent did not fence the same history", err)
		}
	}
	if n, err := journal.RecoverInterrupted(ctx); n != 1 || err != nil {
		t.Fatal("failed to simulate restart recovery", n, err)
	}
	if _, err := journal.Resolve(ctx, app.NativeMaintenanceRecord{Intent: intent, State: app.NativeCompactionCompleted, NativeID: "late"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := NewNativeMaintenanceHistoryReader(state)
	if err := replacement.CheckNativeHistory(ctx, resume); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("late terminal cleared the recovered barrier", err)
	}
	for _, id := range []string{"", "another-native"} {
		resume.NativeSessionID = id
		if err := replacement.CheckNativeHistory(ctx, resume); pa.Code(err) != pa.ExecutionEffectUnknown {
			t.Fatal("changing the native ID reopened the same partition", id, err)
		}
	}
	forgotten, instructions, routine := resume, resume, resume
	forgotten.ForgottenThrough++
	instructions.InstructionRevision++
	routine.SessionSlot = "independent-routine"
	for _, clean := range []pa.ExecutionBinding{forgotten, instructions, routine} {
		if err := replacement.CheckNativeHistory(ctx, clean); err != nil {
			t.Fatal("separate history partition was blocked", clean, err)
		}
	}
}

func TestNativeMaintenanceHistoryReaderPreservesUncertainTerminalsAcrossEpochs(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	_, db, journal := establishMaintenanceHistory(t, state)
	var receipts []app.NativeMaintenanceRecord
	for i, result := range []app.NativeCompactionState{app.NativeCompactionFailed, app.NativeCompactionCancelled, app.NativeCompactionCompleted, app.NativeCompactionUnknown} {
		intent := maintenanceReaderIntent()
		intent.ID = string(result)
		intent.Binding.Fence.BindingID = string(result)
		intent.Binding.Fence.Generation += int64(i)
		intent.Binding.InstructionRevision += int64(i)
		if _, inserted, err := journal.Reserve(ctx, intent); !inserted || err != nil {
			t.Fatal("could not reserve a new epoch", result, inserted, err)
		}
		terminal := app.NativeMaintenanceRecord{Intent: intent, State: result}
		if result != app.NativeCompactionUnknown {
			terminal.NativeID = "matched-terminal"
		}
		if got, err := journal.Resolve(ctx, terminal); err != nil || got != terminal {
			t.Fatal("terminal evidence changed", got, err)
		}
		receipts = append(receipts, terminal)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reader := NewNativeMaintenanceHistoryReader(state)
	for _, receipt := range receipts {
		binding := receipt.Intent.Binding
		binding.NativeSessionID = ""
		want := pa.ExecutionEffectUnknown
		if receipt.State == app.NativeCompactionCompleted {
			want = ""
		}
		if err := reader.CheckNativeHistory(ctx, binding); pa.Code(err) != want {
			t.Fatalf("unexpected history gate for %s: %v", receipt.State, err)
		}
	}
}

func TestNativeMaintenanceHistoryReaderSurvivesMainStoreRollback(t *testing.T) {
	ctx := context.Background()
	state := t.TempDir()
	main := filepath.Join(state, "clawdline.sqlite3")
	db, err := sql.Open("sqlite", main)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := app.InstallSchemas(ctx, db, Installer{}, GroupInstaller{}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE synthetic_release_probe (phase TEXT); INSERT INTO synthetic_release_probe VALUES ('previous')"); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), "previous.sqlite3")
	if _, err = db.Exec("VACUUM INTO ?", old); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE synthetic_release_probe SET phase='candidate'"); err != nil {
		t.Fatal(err)
	}
	_, writer, journal := establishMaintenanceHistory(t, state)
	intent := maintenanceReaderIntent()
	intent.Binding.Fence.Generation = 3
	if _, inserted, err := journal.Reserve(ctx, intent); !inserted || err != nil {
		t.Fatal("could not stage a synthetic intent", inserted, err)
	}
	if _, err := journal.Resolve(ctx, app.NativeMaintenanceRecord{Intent: intent, State: app.NativeCompactionUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(db.Close(), writer.Close()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", main)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var phase string
	if err := db.QueryRow("SELECT phase FROM synthetic_release_probe").Scan(&phase); err != nil || phase != "previous" {
		t.Fatal("the previous main snapshot was not restored", phase, err)
	}
	after := NewNativeMaintenanceHistoryReader(state)
	if err := after.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	beforeMaintenance := intent.Binding
	beforeMaintenance.NativeSessionID = ""
	beforeMaintenance.Fence.Generation--
	beforeMaintenance.Fence.BindingID = "previous-binding"
	if err := after.CheckNativeHistory(ctx, beforeMaintenance); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("main store rollback removed the independent barrier", err)
	}
}

func TestNativeMaintenanceHistoryReaderFailsClosedForEstablishedState(t *testing.T) {
	ctx := context.Background()
	t.Run("never initialized creates nothing", func(t *testing.T) {
		state := t.TempDir()
		r := NewNativeMaintenanceHistoryReader(state)
		if err := r.Ready(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.CheckNativeHistory(ctx, maintenanceReaderIntent().Binding); err != nil {
			t.Fatal(err)
		}
		if entries, err := os.ReadDir(state); len(entries) != 0 || err != nil {
			t.Fatal("reader created writer state", entries, err)
		}
	})
	for _, fault := range []string{"marker lost", "directory lost", "database lost", "database corrupt", "wrong SQLite schema", "database locked", "invalid intent row", "both lost during this Host"} {
		t.Run(fault, func(t *testing.T) {
			state := t.TempDir()
			path, db, journal := establishMaintenanceHistory(t, state)
			r := NewNativeMaintenanceHistoryReader(state)
			if err := r.Ready(ctx); err != nil {
				t.Fatal(err)
			}
			intent := maintenanceReaderIntent()
			if _, inserted, err := journal.Reserve(ctx, intent); !inserted || err != nil {
				t.Fatal("could not stage a synthetic intent", inserted, err)
			}
			switch fault {
			case "marker lost":
				if err := os.Remove(filepath.Join(state, nativeMaintenanceMarker)); err != nil {
					t.Fatal(err)
				}
			case "directory lost":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Dir(path), filepath.Join(state, "moved-for-test")); err != nil {
					t.Fatal(err)
				}
			case "database lost", "database corrupt", "wrong SQLite schema":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if fault == "database corrupt" {
					if err := os.WriteFile(path, []byte("broken journal"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if fault == "wrong SQLite schema" {
					different, err := sql.Open("sqlite", path)
					if err != nil {
						t.Fatal(err)
					}
					defer different.Close()
					if _, err := different.Exec("CREATE TABLE unrelated (id TEXT)"); err != nil {
						t.Fatal(err)
					}
				}
			case "database locked":
				conn, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
					t.Fatal(err)
				}
				defer conn.ExecContext(ctx, "ROLLBACK")
			case "invalid intent row":
				if _, err := db.Exec("UPDATE native_maintenance SET intent='{}'"); err != nil {
					t.Fatal(err)
				}
			case "both lost during this Host":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Dir(path), filepath.Join(state, "moved-for-test")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(state, nativeMaintenanceMarker)); err != nil {
					t.Fatal(err)
				}
			}
			if fault != "both lost during this Host" {
				r = NewNativeMaintenanceHistoryReader(state)
			}
			if err := r.CheckNativeHistory(ctx, intent.Binding); pa.Code(err) != pa.ExecutionEffectUnknown {
				t.Fatal("failed journal opened native history", err)
			}
			if fault != "invalid intent row" {
				if err := r.Ready(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
					t.Fatal("invalid established journal reported readiness", err)
				}
			}
		})
	}
}
