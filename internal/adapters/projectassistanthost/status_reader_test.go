package projectassistanthost

import (
	"context"
	"testing"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	hoststore "github.com/sainteye/clawdline/internal/adapters/store"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
)

// Every Bot read authorizes several times, and each authorization inspects
// the installed schema. That inspection must not queue behind a write that
// holds the store's single writer connection: on 2026-11-23 Bot reads averaged
// 1.5 s against a 28-58 ms store cost because they waited for Bot commits.
func TestStatusDoesNotWaitForTheWriter(t *testing.T) {
	ctx := context.Background()
	st, err := hoststore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	db := st.ProjectAssistantDatabase()
	if err = app.InstallSchemas(ctx, db, featurestore.Installer{}, featurestore.GroupInstaller{}); err != nil {
		t.Fatal(err)
	}
	h := &Host{o: Options{Database: db, Reader: st.ProjectAssistantReader(), MachineID: "source"}}

	// A write in progress owns the only writer connection until it commits.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_schema SET version=version WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	if _, err = h.Status(wait); err != nil {
		t.Fatalf("Status waited for the writer and failed after %v: %v", time.Since(started), err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Status took %v while a write was open", elapsed)
	}
}
