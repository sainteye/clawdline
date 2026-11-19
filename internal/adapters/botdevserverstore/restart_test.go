package botdevserverstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func restartFixture(t *testing.T) (*sql.DB, *Catalog, servers.Restart) {
	t.Helper()
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	if err := InstallRestarts(ctx, db); err != nil {
		t.Fatal(err)
	}
	saved := savedDefinition("restart")
	if err := c.Create(ctx, saved); err != nil {
		t.Fatal(err)
	}
	j, record := newIntent(t, c, saved, "old")
	if err := j.Save(ctx, 0, record); err != nil {
		t.Fatal(err)
	}
	record.Version++
	record.State = servers.Stopped
	if err := j.Save(ctx, 1, record); err != nil {
		t.Fatal(err)
	}
	saved.Attempt = record
	return db, c, servers.Restart{Saved: saved, Boot: strings.Repeat("a", 64)}
}

func TestRestartCheckpointIsAnExactTerminalReceiptConsumedOnce(t *testing.T) {
	_, c, row := restartFixture(t)
	ctx := context.Background()
	if err := c.SaveRestart(ctx, row); err != nil {
		t.Fatal(err)
	}
	// Reopen the actual file, so the original connection cannot supply an
	// uncommitted or cached composition receipt.
	var sequence int
	var name, path string
	if err := c.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil || name != "main" || path == "" {
		t.Fatal("missing source database path", err)
	}
	if err := c.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	other, err := NewCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := other.TakeRestarts(ctx)
	if err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatal(rows, err)
	}
	c = other
	if rows, err := c.TakeRestarts(ctx); err != nil || len(rows) != 0 {
		t.Fatal("consumed intent was replayed", rows, err)
	}
	stale := row
	stale.Saved.Attempt.Version++
	if err := c.SaveRestart(ctx, stale); pa.Code(err) != pa.StaleVersion {
		t.Fatal("stale terminal receipt saved", err)
	}
	row.Saved.Attempt.State = servers.Unknown
	if err := c.SaveRestart(ctx, row); pa.Code(err) != pa.Invalid {
		t.Fatal("unknown cleanup was saved as restartable", err)
	}
}

func TestRestartCheckpointDoesNotSurviveAnInterveningOldCatalogWriter(t *testing.T) {
	_, c, row := restartFixture(t)
	ctx := context.Background()
	if err := c.SaveRestart(ctx, row); err != nil {
		t.Fatal(err)
	}
	// The existing catalog API neither reads nor requires the optional table.
	// A new explicit attempt supersedes the saved terminal revision.
	j, record := newIntent(t, c, row.Saved, "new")
	if err := j.Save(ctx, 0, record); err != nil {
		t.Fatal(err)
	}
	rows, err := c.TakeRestarts(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal("old writer's newer attempt was replayed", rows, err)
	}
}

func TestRestartCheckpointTakeFailureReturnsNoWorkAndRollsBack(t *testing.T) {
	db, c, row := restartFixture(t)
	ctx := context.Background()
	if err := c.SaveRestart(ctx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER refuse_restart_take BEFORE DELETE ON bot_devserver_restarts BEGIN SELECT RAISE(ABORT,'synthetic take failure'); END`); err != nil {
		t.Fatal(err)
	}
	if rows, err := c.TakeRestarts(ctx); err == nil || len(rows) != 0 {
		t.Fatal("failed take exposed executable work", rows, err)
	}
	if _, err := db.Exec(`DROP TRIGGER refuse_restart_take`); err != nil {
		t.Fatal(err)
	}
	if rows, err := c.TakeRestarts(ctx); err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatal("failed take did not roll back", rows, err)
	}
}

func TestRestartReadsDoNotInstallAndUnknownShapeIsNotRepaired(t *testing.T) {
	db, _ := testDatabase(t)
	c := readyCatalog(t, db)
	ctx := context.Background()
	if rows, err := c.TakeRestarts(ctx); pa.Code(err) != pa.Disabled || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='bot_devserver_restarts'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("read installed restart storage", count, err)
	}
	if _, err := db.Exec(`CREATE TABLE bot_devserver_restarts(id TEXT NOT NULL PRIMARY KEY, future BLOB)`); err != nil {
		t.Fatal(err)
	}
	if err := InstallRestarts(ctx, db); pa.Code(err) != pa.Unsupported {
		t.Fatal("unknown table was silently repaired", err)
	}
}
