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
	if err = checkCloudHookSchema(ctx, tx); err != nil {
		return err
	}
	// Inspect before any feature DDL or backfill. A future (or invalid) source
	// must remain untouched even when it is missing one of our known tables.
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_schema'`).Scan(&exists); err != nil {
		return err
	}
	version := pa.SchemaVersion
	if exists != 0 {
		if err = tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil {
			return err
		}
		if version != 1 && version != 2 && version != 3 && version != 4 && version != 5 && version != 6 && version != 7 && version != 8 && version != 9 && version != 10 && version != 11 && version != 12 && version != 13 && version != 14 && version != 15 && version != pa.SchemaVersion {
			return failure(pa.Unsupported, "schema_version")
		}
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_assistant_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`); err != nil {
		return err
	}
	if exists == 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_schema(id,version) VALUES(1,?)`, pa.SchemaVersion); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		networkSettingsSchema,
		towerFloorsSchema,
		towerFloorVersionsSchema,
		reactionsSchema,
		handoffCardSchema,
		handoffReceiptSchema,
		backgroundTaskSchema,
		`CREATE UNIQUE INDEX IF NOT EXISTS project_assistant_background_worker ON project_assistant_background_tasks(machine,worker) WHERE worker<>''`,
		`CREATE INDEX IF NOT EXISTS project_assistant_background_waiting ON project_assistant_background_tasks(machine,bot,state,order_id)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_connector_holds(source TEXT NOT NULL,bot TEXT NOT NULL,connector TEXT NOT NULL,scope TEXT NOT NULL,revision INTEGER NOT NULL,id TEXT NOT NULL,PRIMARY KEY(source,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_connector_credentials(source TEXT NOT NULL,bot TEXT NOT NULL,connector TEXT NOT NULL,id TEXT NOT NULL,scope TEXT NOT NULL,state TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(source,bot,connector))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_login_kinds (source TEXT NOT NULL,id TEXT NOT NULL,kind TEXT NOT NULL,PRIMARY KEY(source,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_login_profiles (source TEXT NOT NULL,id TEXT NOT NULL,agent TEXT NOT NULL,name TEXT NOT NULL,state TEXT NOT NULL,revision INTEGER NOT NULL,removed INTEGER NOT NULL,PRIMARY KEY(source,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_login_holds (source TEXT NOT NULL,profile TEXT NOT NULL,id TEXT NOT NULL,PRIMARY KEY(source,profile,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_cloud_hook_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL CHECK(version=1))`,
		`INSERT OR IGNORE INTO project_assistant_cloud_hook_schema(id,version) VALUES(1,1)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_cloud_hooks (account TEXT NOT NULL, machine TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0), fingerprint TEXT NOT NULL, PRIMARY KEY(account,machine))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_cloud_hook_pending (account TEXT NOT NULL, machine TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(account,machine))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_exchanges (machine TEXT NOT NULL, id TEXT NOT NULL, source TEXT NOT NULL, target TEXT NOT NULL, outstanding INTEGER NOT NULL, revision INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,id))`,
		`CREATE INDEX IF NOT EXISTS project_assistant_exchange_pending ON project_assistant_exchanges(machine,outstanding,source)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_exchange_reply_pending ON project_assistant_exchanges(machine,source) WHERE json_extract(CAST(data AS TEXT),'$.delivery') IN ('waiting','queued')`,
		`CREATE TABLE IF NOT EXISTS project_assistant_lead_setup (machine TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_agent_rests (machine TEXT NOT NULL, agent TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,agent))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_bots (machine TEXT NOT NULL, bot TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0), data BLOB NOT NULL, PRIMARY KEY(machine,bot))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_events (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, event_id TEXT NOT NULL, data BLOB NOT NULL, UNIQUE(machine,event_id))`,
		`CREATE INDEX IF NOT EXISTS project_assistant_event_bot ON project_assistant_events(machine,bot,seq)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_event_machine ON project_assistant_events(machine,seq)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_roster_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL, conversation TEXT NOT NULL, generation INTEGER NOT NULL, binding TEXT NOT NULL, removed INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_roster_machine ON project_assistant_roster_changes(machine,seq)`,
		`CREATE INDEX IF NOT EXISTS project_assistant_roster_bot ON project_assistant_roster_changes(machine,bot,seq)`,
		`INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed) SELECT b.machine,b.bot,json_extract(CAST(b.data AS TEXT),'$.Bot.project'),json_extract(CAST(b.data AS TEXT),'$.Conversation.id'),json_extract(CAST(b.data AS TEXT),'$.Conversation.generation'),json_extract(CAST(b.data AS TEXT),'$.Conversation.binding_id'),0 FROM project_assistant_bots b WHERE json_extract(CAST(b.data AS TEXT),'$.BackgroundOwner') IS NULL AND NOT EXISTS(SELECT 1 FROM project_assistant_roster_changes c WHERE c.machine=b.machine AND c.bot=b.bot)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_routines (machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, deleted INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_routine_hooks (machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, secret TEXT NOT NULL, PRIMARY KEY(machine,bot,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_routine_runs (machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, routine TEXT NOT NULL, state TEXT NOT NULL, published INTEGER NOT NULL, finished TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,id))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS project_assistant_routine_pending ON project_assistant_routine_runs(machine,bot,routine) WHERE state='queued'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS project_assistant_routine_delivery ON project_assistant_routine_runs(machine,bot,routine,json_extract(CAST(data AS TEXT),'$.delivery_id')) WHERE json_extract(CAST(data AS TEXT),'$.delivery_id') IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS project_assistant_receipts (machine TEXT NOT NULL, bot TEXT NOT NULL, operation TEXT NOT NULL, command_key TEXT NOT NULL, receipt_id TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,bot,operation,command_key), UNIQUE(machine,bot,receipt_id))`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	// V15 keeps independent workspace and exchange identities out of older writers.
	// V14 keeps private background workers out of older writers and rosters.
	// V13 protects terminal quota attempt facts from older writers.
	// V12 protects named topic-note metadata and index-only context from old writers.
	// V8 makes login kind durable; old source-owned profiles are accounts.
	// Backfill only during migration. Missing kind authority in schema8 must
	// refuse, never silently turn an encrypted-key identity into an account.
	if version < 8 {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_assistant_login_kinds(source,id,kind) SELECT source,id,'account' FROM project_assistant_login_profiles`); err != nil {
			return err
		}
	}
	// V7 protects managed login and extension selections and adds source-owned login holds.
	// V6 protects Agent configuration and notification choices from older writers.
	// V5 preserves human artifact title overlays; older readers refuse before rewriting them.
	// V4 protects hook capabilities, follow-up provenance and retry state that older readers do
	// not understand. No historical JSON, event or receipt is decoded/rebuilt.
	// Keep the metadata update last so failure rolls back the additive DDL too.
	if version < pa.SchemaVersion {
		if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_schema SET version=? WHERE id=1 AND version=?`, pa.SchemaVersion, version); err != nil {
			return err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&version); err != nil {
		return err
	}
	if version != pa.SchemaVersion {
		return failure(pa.Unsupported, "schema_version")
	}
	return tx.Commit()
}
