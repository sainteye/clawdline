// Package projectassistanttest supplies a fresh, isolated SQLite database for
// module tests. It neither imports the global store nor installs production
// tables; the caller supplies PA-03's versioned SchemaInstaller.
package projectassistanttest

import (
	"context"
	"database/sql"
	"fmt"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

// Bootstrap returns a caller-owned in-memory DB. Close it at test cleanup.
// Each call is independent; the single connection keeps :memory: consistent.
func Bootstrap(ctx context.Context, installer app.SchemaInstaller) (*sql.DB, error) {
	if installer == nil || installer.Version() != pa.SchemaVersion {
		return nil, fmt.Errorf("unsupported project assistant schema installer")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err == nil {
		err = installer.Install(ctx, db)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
