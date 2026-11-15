// Package projectassistantstore implements feature-owned SQLite persistence.
// The caller owns the database, authorization and existing Project authority.
package projectassistantstore

import (
	"context"
	"database/sql"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Installer struct{}

var _ app.SchemaInstaller = Installer{}

func (Installer) Version() int { return pa.SchemaVersion }

// Install is transactional, repeatable and leaves the host user_version alone.
func (Installer) Install(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS project_assistant_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`,
		`INSERT OR IGNORE INTO project_assistant_schema(id,version) VALUES(1,1)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_bots (machine TEXT NOT NULL, bot TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0), data BLOB NOT NULL, PRIMARY KEY(machine,bot))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_events (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, event_id TEXT NOT NULL, data BLOB NOT NULL, UNIQUE(machine,event_id))`,
		`CREATE INDEX IF NOT EXISTS project_assistant_event_bot ON project_assistant_events(machine,bot,seq)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_roster_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, conversation TEXT NOT NULL, generation INTEGER NOT NULL, binding TEXT NOT NULL, removed INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_roster_machine ON project_assistant_roster_changes(machine,seq)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_roster_bot ON project_assistant_roster_changes(machine,bot,seq)`,
		`INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed) SELECT b.machine,b.bot,json_extract(CAST(b.data AS TEXT),'$.Bot.project'),json_extract(CAST(b.data AS TEXT),'$.Conversation.id'),json_extract(CAST(b.data AS TEXT),'$.Conversation.generation'),json_extract(CAST(b.data AS TEXT),'$.Conversation.binding_id'),0 FROM project_assistant_bots b WHERE NOT EXISTS(SELECT 1 FROM project_assistant_roster_changes c WHERE c.machine=b.machine AND c.bot=b.bot)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_routines (machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, deleted INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_routine_runs (machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, routine TEXT NOT NULL, state TEXT NOT NULL, published INTEGER NOT NULL, finished TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,id))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS project_assistant_routine_pending ON project_assistant_routine_runs(machine,bot,routine) WHERE state='queued'`,
		`CREATE TABLE IF NOT EXISTS project_assistant_receipts (machine TEXT NOT NULL, bot TEXT NOT NULL, operation TEXT NOT NULL, command_key TEXT NOT NULL, receipt_id TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,operation,command_key), UNIQUE(machine,bot,receipt_id))`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil {
		return err
	}
	if version != pa.SchemaVersion {
		return failure(pa.Unsupported, "schema_version")
	}
	return tx.Commit()
}
