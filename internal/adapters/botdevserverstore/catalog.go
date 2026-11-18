package botdevserverstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Catalog borrows the same source writer as Ledger. Definitions are local
// configuration, not portable Bot memory or an authority grant. NewCatalog
// reads only; InstallCatalog is an explicit composition operation.
type Catalog struct{ db *sql.DB }

var _ servers.Catalog = (*Catalog)(nil)

const catalogMarker = `CREATE TABLE bot_devserver_catalog_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`
const definitions = `CREATE TABLE bot_devserver_definitions (
 id TEXT NOT NULL PRIMARY KEY, fingerprint TEXT NOT NULL,
 spec BLOB NOT NULL, current_attempt TEXT NOT NULL,
 removed INTEGER NOT NULL CHECK(removed IN (0,1)))`

type column struct {
	name, kind        string
	required, primary int
}

func columnsMatch(ctx context.Context, tx *sql.Tx, query string, want []column) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var cid int
		var got column
		var defaultValue any
		if err := rows.Scan(&cid, &got.name, &got.kind, &got.required, &defaultValue, &got.primary); err != nil {
			return err
		}
		if index >= len(want) || cid != index || got != want[index] {
			return refusal(pa.Unsupported, "server_catalog_shape")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != len(want) {
		return refusal(pa.Unsupported, "server_catalog_shape")
	}
	return nil
}

func catalogSchema(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('bot_devserver_catalog_schema','bot_devserver_definitions')`).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	if count != 2 {
		return false, refusal(pa.Invalid, "server_catalog_partial")
	}
	if err := columnsMatch(ctx, tx, `PRAGMA table_info(bot_devserver_catalog_schema)`, []column{{"id", "INTEGER", 0, 1}, {"version", "INTEGER", 1, 0}}); err != nil {
		return false, err
	}
	var rows int
	var id, low, high sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),min(id),min(version),max(version) FROM bot_devserver_catalog_schema`).Scan(&rows, &id, &low, &high); err != nil {
		return false, err
	}
	if rows != 1 || !id.Valid || id.Int64 != 1 || !low.Valid || low != high {
		return false, refusal(pa.Invalid, "server_catalog_marker")
	}
	if low.Int64 != 1 {
		return false, refusal(pa.Unsupported, "server_catalog_schema")
	}
	if err := columnsMatch(ctx, tx, `PRAGMA table_info(bot_devserver_definitions)`, []column{
		{"id", "TEXT", 1, 1}, {"fingerprint", "TEXT", 1, 0}, {"spec", "BLOB", 1, 0}, {"current_attempt", "TEXT", 1, 0}, {"removed", "INTEGER", 1, 0},
	}); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_definitions WHERE removed NOT IN (0,1)`).Scan(&count); err != nil {
		return false, err
	}
	if count != 0 {
		return false, refusal(pa.Invalid, "server_catalog_removed")
	}
	return true, nil
}

func InstallCatalog(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return refusal(pa.Invalid, "server_catalog_database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ledger, err := schema(ctx, tx)
	if err != nil {
		return err
	}
	catalog, err := catalogSchema(ctx, tx)
	if err != nil {
		return err
	}
	if catalog && !ledger {
		return refusal(pa.Invalid, "server_catalog_without_ledger")
	}
	if ledger && !catalog {
		var outstanding int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_attempts WHERE state IN ('accepted','running','stopping','unknown')`).Scan(&outstanding); err != nil {
			return err
		}
		if outstanding != 0 {
			return refusal(pa.ExecutionEffectUnknown, "server_ledger_unowned")
		}
	}
	if !ledger {
		for _, statement := range []string{marker, attempts, `INSERT INTO bot_devserver_schema(id,version) VALUES(1,1)`} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if !catalog {
		for _, statement := range []string{catalogMarker, definitions, `INSERT INTO bot_devserver_catalog_schema(id,version) VALUES(1,1)`} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func catalogReady(ctx context.Context, tx *sql.Tx) error {
	if present, err := schema(ctx, tx); err != nil {
		return err
	} else if !present {
		return refusal(pa.Disabled, "server_ledger_absent")
	}
	if present, err := catalogSchema(ctx, tx); err != nil {
		return err
	} else if !present {
		return refusal(pa.Disabled, "server_catalog_absent")
	}
	var invalid int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_definitions d LEFT JOIN bot_devserver_attempts a ON d.current_attempt=a.attempt_key WHERE d.removed=0 AND d.current_attempt<>'' AND a.attempt_key IS NULL`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return refusal(pa.Invalid, "server_current_attempt")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_attempts a LEFT JOIN bot_devserver_definitions d ON d.current_attempt=a.attempt_key AND d.removed=0 WHERE a.state IN ('accepted','running','stopping','unknown') AND d.id IS NULL`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return refusal(pa.ExecutionEffectUnknown, "server_ledger_unowned")
	}
	return nil
}

func NewCatalog(ctx context.Context, db *sql.DB) (*Catalog, error) {
	if db == nil {
		return nil, refusal(pa.Invalid, "server_catalog_database")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return nil, err
	}
	return &Catalog{db: db}, nil
}

func definition(ctx context.Context, tx *sql.Tx, id string) (servers.SavedServer, string, error) {
	var saved servers.SavedServer
	var fingerprint, current string
	var raw []byte
	var removed int
	// Check size in SQLite before materializing possibly corrupt command data.
	err := tx.QueryRowContext(ctx, `SELECT id,fingerprint,CASE WHEN length(spec)<=? THEN spec ELSE NULL END,current_attempt,removed FROM bot_devserver_definitions WHERE id=?`, capacity.MaximumBotServerSpecBytes, id).Scan(&saved.ID, &fingerprint, &raw, &current, &removed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && removed == 1) {
		return saved, "", refusal(pa.NotFound, "server")
	}
	if err != nil {
		return saved, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved.Spec); err != nil {
		return saved, "", refusal(pa.Invalid, "server_definition")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return saved, "", refusal(pa.Invalid, "server_definition")
	}
	actual, err := saved.Spec.Fingerprint()
	if err != nil || fingerprint != actual {
		return saved, "", refusal(pa.Invalid, "server_definition")
	}
	if current != "" {
		saved.Attempt, err = recordAt(ctx, tx, current)
		if err != nil {
			return saved, "", err
		}
		i := saved.Attempt.Identity
		if saved.Attempt.Version == 0 || i.Key != saved.Spec.Key || i.Project != saved.Spec.Project || i.ServerID != saved.ID || i.Fingerprint != fingerprint {
			return saved, "", refusal(pa.Invalid, "server_current_attempt")
		}
	}
	return saved, current, nil
}

func (c *Catalog) List(ctx context.Context, limit int) ([]servers.SavedServer, error) {
	if limit <= 0 || limit > capacity.MaximumBotServerDefinitions {
		return nil, refusal(pa.Invalid, "server_page")
	}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM bot_devserver_definitions WHERE removed=0 ORDER BY id LIMIT ?`, limit+1)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		return nil, refusal(pa.CapacityFull, "server_definitions")
	}
	result := make([]servers.SavedServer, 0, len(ids))
	for _, id := range ids {
		saved, _, err := definition(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, saved)
	}
	return result, nil
}

func (c *Catalog) Create(ctx context.Context, saved servers.SavedServer) error {
	fingerprint, err := saved.Spec.Fingerprint()
	if err != nil {
		return err
	}
	if saved.ID == "" || saved.Attempt != (servers.Record{}) {
		return refusal(pa.Invalid, "server_definition")
	}
	raw, _ := json.Marshal(saved.Spec)
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_definitions WHERE id=? OR (removed=0 AND fingerprint=?)`, saved.ID, fingerprint).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return refusal(pa.IdempotencyConflict, "server_definition")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_attempts WHERE server=?`, saved.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return refusal(pa.IdempotencyConflict, "server_definition")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_definitions WHERE removed=0`).Scan(&count); err != nil {
		return err
	}
	if count >= capacity.MaximumBotServerDefinitions {
		return refusal(pa.CapacityFull, "server_definitions")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bot_devserver_definitions(id,fingerprint,spec,current_attempt,removed) VALUES(?,?,?,'',0)`, saved.ID, fingerprint, raw); err != nil {
		return err
	}
	return tx.Commit()
}

func terminal(state servers.State) bool { return state == servers.Stopped || state == servers.Failed }

func (c *Catalog) Remove(ctx context.Context, expected servers.SavedServer) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	got, _, err := definition(ctx, tx, expected.ID)
	if err != nil {
		return err
	}
	if got != expected {
		return refusal(pa.StaleVersion, "server_definition")
	}
	if got.Attempt.Version > 0 && !terminal(got.Attempt.State) {
		return refusal(pa.ExecutionEffectUnknown, "server_owner")
	}
	// Retain an ID-only tombstone so delayed controls cannot address a reused
	// definition. Remove executable command text; retain past attempt receipts.
	if _, err := tx.ExecContext(ctx, `UPDATE bot_devserver_definitions SET spec=x'',fingerprint='',current_attempt='',removed=1 WHERE id=? AND removed=0`, expected.ID); err != nil {
		return err
	}
	return tx.Commit()
}

type catalogJournal struct {
	c        *Catalog
	baseline servers.SavedServer
	identity servers.Identity
}

func (c *Catalog) Journal(ctx context.Context, saved servers.SavedServer, i servers.Identity) (servers.Journal, error) {
	fingerprint, err := saved.Spec.Fingerprint()
	if err != nil {
		return nil, err
	}
	if i.Validate() != nil || i.ServerID != saved.ID || i.Key != saved.Spec.Key || i.Project != saved.Spec.Project || i.Fingerprint != fingerprint {
		return nil, refusal(pa.ScopeDenied, "server_definition")
	}
	j := &catalogJournal{c: c, baseline: saved, identity: i}
	if _, err := j.Load(ctx, i); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *catalogJournal) Load(ctx context.Context, i servers.Identity) (servers.Record, error) {
	if i != j.identity {
		return servers.Record{}, refusal(pa.ScopeDenied, "server_attempt")
	}
	tx, err := j.c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return servers.Record{}, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return servers.Record{}, err
	}
	saved, current, err := definition(ctx, tx, j.baseline.ID)
	if err != nil {
		return servers.Record{}, err
	}
	if saved.Spec != j.baseline.Spec {
		return servers.Record{}, refusal(pa.ScopeDenied, "server_definition")
	}
	r, err := load(ctx, tx, i)
	if err != nil {
		return r, err
	}
	if r.Version > 0 {
		if current != key(i) {
			return servers.Record{}, refusal(pa.StaleGeneration, "server_attempt")
		}
	} else if saved != j.baseline {
		return servers.Record{}, refusal(pa.StaleVersion, "server_definition")
	}
	return r, nil
}

func (j *catalogJournal) Save(ctx context.Context, expected int64, r servers.Record) error {
	if r.Identity != j.identity {
		return refusal(pa.ScopeDenied, "server_attempt")
	}
	if err := validateSave(expected, r); err != nil {
		return err
	}
	tx, err := j.c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	saved, current, err := definition(ctx, tx, j.baseline.ID)
	if err != nil {
		return err
	}
	if saved.Spec != j.baseline.Spec {
		return refusal(pa.ScopeDenied, "server_definition")
	}
	if expected == 0 {
		if saved != j.baseline {
			return refusal(pa.StaleVersion, "server_definition")
		}
		if saved.Attempt.Version > 0 && !terminal(saved.Attempt.State) {
			return refusal(pa.ExecutionEffectUnknown, "server_owner")
		}
		if r.State == servers.Accepted {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_devserver_definitions d JOIN bot_devserver_attempts a ON d.current_attempt=a.attempt_key WHERE d.removed=0 AND a.state IN ('accepted','running','stopping','unknown')`).Scan(&count); err != nil {
				return err
			}
			if count >= capacity.MaximumBotServerProcesses {
				return refusal(pa.CapacityFull, "server_processes")
			}
		}
	} else if current != key(r.Identity) {
		return refusal(pa.StaleGeneration, "server_attempt")
	}
	if err := saveAttempt(ctx, tx, expected, r); err != nil {
		return err
	}
	if expected == 0 {
		result, err := tx.ExecContext(ctx, `UPDATE bot_devserver_definitions SET current_attempt=? WHERE id=? AND current_attempt=? AND removed=0`, key(r.Identity), saved.ID, current)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return refusal(pa.StaleVersion, "server_definition")
		}
	}
	return tx.Commit()
}
