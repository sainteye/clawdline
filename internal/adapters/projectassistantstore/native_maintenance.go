package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NativeMaintenanceJournal is an isolated SQLite qualification journal. It is
// deliberately absent from InstallSchemas and from the portable Bot archive.
type NativeMaintenanceJournal struct{ db *sql.DB }

var _ app.NativeMaintenanceJournal = (*NativeMaintenanceJournal)(nil)

func NewNativeMaintenanceJournal(ctx context.Context, db *sql.DB) (*NativeMaintenanceJournal, error) {
	if db == nil {
		return nil, failure(pa.Invalid, "native_maintenance_database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var other bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM sqlite_schema
		WHERE sql IS NOT NULL AND name NOT IN ('native_maintenance', 'native_maintenance_unresolved')
	)`).Scan(&other)
	if err != nil {
		return nil, err
	}
	if other {
		return nil, failure(pa.ScopeDenied, "native_maintenance_database")
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS native_maintenance (
		machine TEXT NOT NULL, bot TEXT NOT NULL, id TEXT NOT NULL, binding TEXT NOT NULL,
		intent BLOB NOT NULL, state TEXT NOT NULL DEFAULT '', native_id TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (machine, bot, id), UNIQUE (machine, bot, binding),
		CHECK ((state IN ('', 'unknown') AND native_id = '') OR
		       (state IN ('completed', 'failed', 'cancelled') AND native_id <> ''))
	);
	CREATE UNIQUE INDEX IF NOT EXISTS native_maintenance_unresolved
		ON native_maintenance (machine, bot) WHERE state IN ('', 'unknown');`)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &NativeMaintenanceJournal{db: db}, nil
}

func (j *NativeMaintenanceJournal) Lookup(ctx context.Context, intent app.NativeMaintenanceIntent) (app.NativeMaintenanceRecord, bool, error) {
	return lookupMaintenance(ctx, j.db, intent)
}

func lookupMaintenance(ctx context.Context, q reader, intent app.NativeMaintenanceIntent) (app.NativeMaintenanceRecord, bool, error) {
	var out app.NativeMaintenanceRecord
	var data []byte
	key := intent.Binding.Fence.Key
	err := q.QueryRowContext(ctx, `SELECT intent, state, native_id FROM native_maintenance
		WHERE machine=? AND bot=? AND id=?`, key.MachineID, key.BotID, intent.ID).Scan(&data, &out.State, &out.NativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	if err = json.Unmarshal(data, &out.Intent); err != nil {
		return app.NativeMaintenanceRecord{}, false, err
	}
	if intent != out.Intent {
		return app.NativeMaintenanceRecord{}, false, failure(pa.IdempotencyConflict, "native_maintenance_intent")
	}
	return out, true, nil
}

func (j *NativeMaintenanceJournal) Reserve(ctx context.Context, intent app.NativeMaintenanceIntent) (app.NativeMaintenanceRecord, bool, error) {
	var out app.NativeMaintenanceRecord
	if err := intent.Validate(); err != nil {
		return out, false, err
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return out, false, err
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return out, false, err
	}
	defer tx.Rollback()
	fence := intent.Binding.Fence
	res, err := tx.ExecContext(ctx, `INSERT INTO native_maintenance (machine, bot, id, binding, intent)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, fence.Key.MachineID, fence.Key.BotID, intent.ID, fence.BindingID, data)
	if err != nil {
		return out, false, err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return out, false, err
	}
	if inserted != 0 {
		if err = tx.Commit(); err != nil {
			return out, false, err
		}
		return app.NativeMaintenanceRecord{Intent: intent}, true, nil
	}
	if existing, found, err := lookupMaintenance(ctx, tx, intent); err != nil || found {
		return existing, false, err
	}
	var unresolved bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM native_maintenance
		WHERE machine=? AND bot=? AND state IN ('', 'unknown'))`, fence.Key.MachineID, fence.Key.BotID).Scan(&unresolved)
	if err != nil {
		return out, false, err
	}
	if unresolved {
		return out, false, failure(pa.ExecutionEffectUnknown, "native_maintenance_barrier")
	}
	return out, false, failure(pa.IdempotencyConflict, "native_maintenance_binding")
}

func (j *NativeMaintenanceJournal) Resolve(ctx context.Context, terminal app.NativeMaintenanceRecord) (app.NativeMaintenanceRecord, error) {
	var out app.NativeMaintenanceRecord
	if terminal.State == "" {
		return out, failure(pa.Invalid, "native_maintenance_terminal")
	}
	intent := terminal.Intent
	data, err := json.Marshal(intent)
	if err != nil {
		return out, err
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	key := intent.Binding.Fence.Key
	_, err = tx.ExecContext(ctx, `UPDATE native_maintenance SET state=?, native_id=?
		WHERE machine=? AND bot=? AND id=? AND intent=? AND state=''`, terminal.State,
		terminal.NativeID, key.MachineID, key.BotID, intent.ID, data)
	if err != nil {
		return out, err
	}
	out, found, err := lookupMaintenance(ctx, tx, intent)
	if err != nil {
		return out, err
	}
	if !found {
		return out, failure(pa.NotFound, "native_maintenance_intent")
	}
	if err = tx.Commit(); err != nil {
		return app.NativeMaintenanceRecord{}, err
	}
	return out, nil
}

// RecoverInterrupted is for a replacement owner after the prior isolated host
// is fenced off. It never opens a provider. Resuming from an intent would risk
// running a command the prior writer had already sent.
func (j *NativeMaintenanceJournal) RecoverInterrupted(ctx context.Context) (int64, error) {
	res, err := j.db.ExecContext(ctx, `UPDATE native_maintenance SET state='unknown' WHERE state=''`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
