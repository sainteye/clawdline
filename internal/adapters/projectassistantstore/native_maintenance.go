package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"

	_ "modernc.org/sqlite"

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

// This state lives beside the live Bot databases. The updater can restore its
// main store without discarding the maintenance history barrier. Directory
// and marker are independent so losing either is a refusal, not a fresh start.
const (
	nativeMaintenanceDir    = "project-assistant-native-maintenance"
	nativeMaintenanceMarker = "project-assistant-native-maintenance.initialized"
	nativeMaintenanceDB     = "journal.sqlite3"
	nativeMaintenanceFormat = "native-maintenance-v1\n"
)

// NativeMaintenanceHistoryReader only opens established state read-only. A
// source installed before maintenance has no marker, directory or writer.
type NativeMaintenanceHistoryReader struct {
	state string
	seen  atomic.Bool
}

func NewNativeMaintenanceHistoryReader(state string) *NativeMaintenanceHistoryReader {
	return &NativeMaintenanceHistoryReader{state: state}
}

func (r *NativeMaintenanceHistoryReader) Ready(ctx context.Context) error {
	db, err := r.open()
	if err != nil || db == nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		return maintenanceReadFailure(err)
	}
	// A readable but empty/wrong SQLite file is not an initialized journal.
	var present bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT intent FROM native_maintenance
		WHERE machine=? AND bot=? AND state IN ('', 'unknown'))`, "", "").Scan(&present)
	if err != nil {
		return maintenanceReadFailure(err)
	}
	return nil
}

func (r *NativeMaintenanceHistoryReader) CheckNativeHistory(ctx context.Context, binding pa.ExecutionBinding) error {
	db, err := r.open()
	if err != nil || db == nil {
		return err
	}
	defer db.Close()

	fence := binding.Fence
	var data []byte
	err = db.QueryRowContext(ctx, `SELECT intent FROM native_maintenance
		WHERE machine=? AND bot=? AND state IN ('', 'unknown')`, fence.Key.MachineID, fence.Key.BotID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return maintenanceReadFailure(err)
	}
	var blocked app.NativeMaintenanceIntent
	if err = json.Unmarshal(data, &blocked); err != nil {
		return maintenanceReadFailure(err)
	}
	if err = blocked.Validate(); err != nil || blocked.Binding.Fence.Key != fence.Key {
		return maintenanceReadFailure(err)
	}
	// The ACP directory partitions history by conversation, account selection
	// and privacy/instruction epoch. A new execution generation alone still
	// points at the same history.
	prior := blocked.Binding
	if prior.Fence.Project == fence.Project && prior.Fence.ConversationID == fence.ConversationID &&
		prior.Agent == binding.Agent && prior.LoginProfile == binding.LoginProfile && prior.SessionSlot == binding.SessionSlot &&
		prior.InstructionRevision == binding.InstructionRevision && prior.ForgottenThrough == binding.ForgottenThrough &&
		prior.NativeSessionID == binding.NativeSessionID {
		return failure(pa.ExecutionEffectUnknown, "native_maintenance_history")
	}
	return nil
}

func (r *NativeMaintenanceHistoryReader) open() (*sql.DB, error) {
	if !filepath.IsAbs(r.state) {
		return nil, maintenanceReadFailure(nil)
	}
	state, err := os.Stat(r.state)
	if err != nil || !state.IsDir() {
		return nil, maintenanceReadFailure(err)
	}
	dir := filepath.Join(r.state, nativeMaintenanceDir)
	marker := filepath.Join(r.state, nativeMaintenanceMarker)
	dirInfo, dirErr := os.Lstat(dir)
	markerInfo, markerErr := os.Lstat(marker)
	if errors.Is(dirErr, os.ErrNotExist) && errors.Is(markerErr, os.ErrNotExist) && !r.seen.Load() {
		return nil, nil
	}
	if dirErr == nil || markerErr == nil {
		r.seen.Store(true)
	}
	if err = errors.Join(dirErr, markerErr); err != nil {
		return nil, maintenanceReadFailure(err)
	}
	if !dirInfo.IsDir() || !markerInfo.Mode().IsRegular() || markerInfo.Size() != int64(len(nativeMaintenanceFormat)) {
		return nil, maintenanceReadFailure(nil)
	}
	format, err := os.ReadFile(marker)
	if err != nil || string(format) != nativeMaintenanceFormat {
		return nil, maintenanceReadFailure(err)
	}
	path := filepath.Join(dir, nativeMaintenanceDB)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if suffix != "" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return nil, maintenanceReadFailure(err)
		}
	}
	// SQLite opens the committed WAL view when present. immutable=1 could
	// skip a pending intent written since the last main-file checkpoint.
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, maintenanceReadFailure(err)
	}
	return db, nil
}

func maintenanceReadFailure(cause error) error {
	return errors.Join(failure(pa.ExecutionEffectUnknown, "native_maintenance_journal"), cause)
}
