// Package botdevserverstore persists source-local execution receipts in the
// source's existing SQLite writer. It never opens/closes a pool, stores a
// command or installs this ledger into the portable Bot aggregate.
package botdevserverstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Ledger struct{ db *sql.DB }

var _ servers.Journal = (*Ledger)(nil)

const marker = `CREATE TABLE bot_devserver_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`
const attempts = `CREATE TABLE bot_devserver_attempts (
 attempt_key TEXT NOT NULL PRIMARY KEY,
 machine TEXT NOT NULL, bot TEXT NOT NULL, project TEXT NOT NULL,
 canonical TEXT NOT NULL, kind TEXT NOT NULL,
 server TEXT NOT NULL, attempt TEXT NOT NULL, fingerprint TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),
 state TEXT NOT NULL CHECK(state IN ('accepted','running','stopping','stopped','failed','unknown')),
 failure TEXT NOT NULL)`

func refusal(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

func schema(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('bot_devserver_schema','bot_devserver_attempts')`).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	if count != 2 {
		return false, refusal(pa.Invalid, "server_ledger_partial")
	}
	var rows int
	var id, low, high sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT count(*), min(id), min(version), max(version) FROM bot_devserver_schema`).Scan(&rows, &id, &low, &high); err != nil {
		return false, err
	}
	if rows != 1 || !id.Valid || id.Int64 != 1 || !low.Valid || low != high {
		return false, refusal(pa.Invalid, "server_ledger_marker")
	}
	if low.Int64 != 1 {
		return false, refusal(pa.Unsupported, "server_ledger_schema")
	}
	columns, err := tx.QueryContext(ctx, `PRAGMA table_info(bot_devserver_attempts)`)
	if err != nil {
		return false, err
	}
	defer columns.Close()
	want := []string{"attempt_key", "machine", "bot", "project", "canonical", "kind", "server", "attempt", "fingerprint", "revision", "state", "failure"}
	index := 0
	for columns.Next() {
		var cid, required, primary int
		var name, kind string
		var defaultValue any
		if err = columns.Scan(&cid, &name, &kind, &required, &defaultValue, &primary); err != nil {
			return false, err
		}
		expectedKind, expectedPrimary := "TEXT", 0
		if name == "revision" {
			expectedKind = "INTEGER"
		}
		if index == 0 {
			expectedPrimary = 1
		}
		if index >= len(want) || cid != index || name != want[index] || kind != expectedKind || required != 1 || primary != expectedPrimary {
			return false, refusal(pa.Unsupported, "server_ledger_shape")
		}
		index++
	}
	if err = columns.Err(); err != nil {
		return false, err
	}
	if index != len(want) {
		return false, refusal(pa.Unsupported, "server_ledger_shape")
	}
	return true, nil
}

// Install is explicit trusted composition, separate from read-only New and
// the Bot schema installer. Partial/unknown ledgers are never silently repaired
// or stamped down. It leaves PRAGMA user_version and unrelated tables alone.
func Install(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return refusal(pa.Invalid, "server_ledger_database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	present, err := schema(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	for _, statement := range []string{marker, attempts, `INSERT INTO bot_devserver_schema(id,version) VALUES(1,1)`} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func New(ctx context.Context, db *sql.DB) (*Ledger, error) {
	if db == nil {
		return nil, refusal(pa.Invalid, "server_ledger_database")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if present, err := schema(ctx, tx); err != nil {
		return nil, err
	} else if !present {
		return nil, refusal(pa.Disabled, "server_ledger_absent")
	}
	return &Ledger{db: db}, nil
}

// The stable key excludes mutable payload/approval/Project data. Reusing an
// attempt ID with a changed fingerprint cannot select an empty row and launch
// again; the complete stored identity must still match before any update.
func key(i servers.Identity) string {
	b, _ := json.Marshal(struct {
		Key               pa.BotKey
		ServerID, Attempt string
	}{i.Key, i.ServerID, i.AttemptID})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func load(ctx context.Context, tx *sql.Tx, i servers.Identity) (servers.Record, error) {
	r, err := recordAt(ctx, tx, key(i))
	if err != nil || r.Version == 0 {
		return r, err
	}
	if r.Identity != i {
		return servers.Record{}, refusal(pa.ScopeDenied, "server_attempt")
	}
	return r, nil
}

func recordAt(ctx context.Context, tx *sql.Tx, attemptKey string) (servers.Record, error) {
	var r servers.Record
	err := tx.QueryRowContext(ctx, `SELECT machine,bot,project,canonical,kind,server,attempt,fingerprint,revision,state,failure FROM bot_devserver_attempts WHERE attempt_key=?`, attemptKey).Scan(
		&r.Identity.Key.MachineID, &r.Identity.Key.BotID, &r.Identity.Project.ProjectID, &r.Identity.Project.CanonicalID, &r.Identity.Project.Kind,
		&r.Identity.ServerID, &r.Identity.AttemptID, &r.Identity.Fingerprint, &r.Version, &r.State, &r.Failure)
	if errors.Is(err, sql.ErrNoRows) {
		return servers.Record{}, nil
	}
	if err != nil {
		return r, err
	}
	r.Identity.Project.MachineID = r.Identity.Key.MachineID
	if err = r.Validate(); err != nil {
		return servers.Record{}, err
	}
	if key(r.Identity) != attemptKey {
		return servers.Record{}, refusal(pa.Invalid, "server_attempt_key")
	}
	return r, nil
}

func (s *Ledger) Load(ctx context.Context, i servers.Identity) (servers.Record, error) {
	if err := i.Validate(); err != nil {
		return servers.Record{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return servers.Record{}, err
	}
	defer tx.Rollback()
	if present, err := schema(ctx, tx); err != nil {
		return servers.Record{}, err
	} else if !present {
		return servers.Record{}, refusal(pa.Disabled, "server_ledger_absent")
	}
	return load(ctx, tx, i)
}

func transition(from, to servers.State) bool {
	switch from {
	case "":
		return to == servers.Accepted || to == servers.Stopped
	case servers.Accepted:
		return to == servers.Running || to == servers.Stopping || to == servers.Stopped || to == servers.Failed || to == servers.Unknown
	case servers.Running:
		return to == servers.Stopping || to == servers.Stopped || to == servers.Failed || to == servers.Unknown
	case servers.Stopping:
		return to == servers.Stopped || to == servers.Unknown
	default:
		return false // Terminal and unknown facts cannot become another launch.
	}
}

// released is a person's explicit release of an attempt whose process could
// not be proved stopped; it is the only way out of a persisted unknown.
func released(from servers.State, r servers.Record) bool {
	switch from {
	case servers.Accepted, servers.Running, servers.Stopping, servers.Unknown:
		return r.State == servers.Failed && r.Failure == servers.ReleasedUnconfirmed
	}
	return false
}

// Save is a CAS, deliberately not an idempotent insert: two different owners
// cannot both win the same accepted intent even when their payloads match.
func (s *Ledger) Save(ctx context.Context, expected int64, r servers.Record) error {
	if err := validateSave(expected, r); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if present, err := schema(ctx, tx); err != nil {
		return err
	} else if !present {
		return refusal(pa.Disabled, "server_ledger_absent")
	}
	if err := saveAttempt(ctx, tx, expected, r); err != nil {
		return err
	}
	return tx.Commit()
}

func validateSave(expected int64, r servers.Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if expected < 0 || r.Version <= expected || r.Version-expected != 1 {
		return refusal(pa.StaleVersion, "server_revision")
	}
	return nil
}

// The caller has checked both schema and receipt and owns the transaction.
// Catalog admission uses this same CAS without an intermediate commit.
func saveAttempt(ctx context.Context, tx *sql.Tx, expected int64, r servers.Record) error {
	prior, err := load(ctx, tx, r.Identity)
	if err != nil {
		return err
	}
	if prior.Version != expected {
		return refusal(pa.StaleVersion, "server_revision")
	}
	if !transition(prior.State, r.State) && !released(prior.State, r) {
		return refusal(pa.Invalid, "server_transition")
	}
	var result sql.Result
	if expected == 0 {
		i := r.Identity
		result, err = tx.ExecContext(ctx, `INSERT INTO bot_devserver_attempts(attempt_key,machine,bot,project,canonical,kind,server,attempt,fingerprint,revision,state,failure) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(attempt_key) DO NOTHING`,
			key(i), i.Key.MachineID, i.Key.BotID, i.Project.ProjectID, i.Project.CanonicalID, i.Project.Kind, i.ServerID, i.AttemptID, i.Fingerprint, r.Version, r.State, r.Failure)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE bot_devserver_attempts SET revision=?,state=?,failure=? WHERE attempt_key=? AND revision=?`, r.Version, r.State, r.Failure, key(r.Identity), expected)
	}
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return refusal(pa.StaleVersion, "server_revision")
	}
	return nil
}
