package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

// Squad tables are additive. A prior daemon can continue to read its existing
// tables; new version rows are never overwritten or removed during migration.
const squadSchema = `
CREATE TABLE IF NOT EXISTS squad_entities (
  kind TEXT NOT NULL, id TEXT NOT NULL, version TEXT NOT NULL,
  digest TEXT NOT NULL, payload TEXT NOT NULL,
  PRIMARY KEY(kind,id,version)
);
CREATE TABLE IF NOT EXISTS squad_active (
  kind TEXT NOT NULL, id TEXT NOT NULL, version TEXT NOT NULL,
  PRIMARY KEY(kind,id)
);
CREATE TABLE IF NOT EXISTS squad_catalog_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL
);
INSERT OR IGNORE INTO squad_catalog_state(singleton,version) VALUES(1,0);
CREATE TABLE IF NOT EXISTS squad_settings (
  scope_id TEXT NOT NULL, definition_id TEXT NOT NULL,
  version INTEGER NOT NULL, payload TEXT NOT NULL,
  PRIMARY KEY(scope_id,definition_id)
);
CREATE TABLE IF NOT EXISTS squad_receipts (
  key TEXT PRIMARY KEY, request_hash TEXT NOT NULL,
  response_version INTEGER NOT NULL
);
`

func openSquad(db *sql.DB) error {
	_, err := db.Exec(squadSchema)
	if err != nil {
		return err
	}
	return openSquadPackages(db)
}

type SquadLimits struct {
	Entities     int64
	SettingsRows int64
	Receipts     int64
}

func DefaultSquadLimits() SquadLimits {
	return SquadLimits{Entities: squad.MaxSquadEntities,
		SettingsRows: squad.MaxSquadSettingsRows, Receipts: squad.MaxSquadReceipts}
}

type SquadVersionConflict struct{ Current int64 }

func (e SquadVersionConflict) Error() string {
	return fmt.Sprintf("version_conflict: current %d", e.Current)
}

var ErrSquadReceiptConflict = errors.New("idempotency_conflict")
var ErrSquadCapacityFull = errors.New("capacity_full")
var ErrSquadVersionContent = errors.New("definition_version_conflict")

type SquadSettingRow struct {
	ScopeID      string
	DefinitionID string
	Version      int64
	Payload      json.RawMessage
}

type SquadCounts struct {
	Entities int64
	Settings int64
	Receipts int64
}

// SquadUsage measures retained rows for the capacity beat. A failed count is
// unknown to its caller, never a success-shaped zero.
func (s *Store) SquadUsage(ctx context.Context) (SquadCounts, error) {
	if err := reading(); err != nil {
		return SquadCounts{}, err
	}
	var out SquadCounts
	for _, item := range []struct {
		query string
		into  *int64
	}{
		{`SELECT COUNT(*) FROM squad_entities`, &out.Entities},
		{`SELECT COUNT(*) FROM squad_settings`, &out.Settings},
		{`SELECT (SELECT COUNT(*) FROM squad_receipts)+(SELECT COUNT(*) FROM squad_package_receipts)`, &out.Receipts},
	} {
		if err := s.rd.QueryRowContext(ctx, item.query).Scan(item.into); err != nil {
			return SquadCounts{}, classify(err)
		}
	}
	return out, nil
}

// SquadSettings reads all overrides of one scope. An unreadable store returns
// an error rather than a success-shaped empty list.
func (s *Store) SquadSettings(ctx context.Context, scopeID string) ([]SquadSettingRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx,
		`SELECT definition_id,version,payload FROM squad_settings WHERE scope_id=? ORDER BY definition_id`, scopeID)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []SquadSettingRow{}
	for rows.Next() {
		var row SquadSettingRow
		var payload string
		if err := rows.Scan(&row.DefinitionID, &row.Version, &payload); err != nil {
			return nil, classify(err)
		}
		row.ScopeID, row.Payload = scopeID, json.RawMessage(payload)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classify(err)
	}
	return out, nil
}

func (s *Store) SquadScopes(ctx context.Context) ([]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx,
		`SELECT DISTINCT scope_id FROM squad_settings WHERE scope_id <> 'global' ORDER BY scope_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, classify(err)
		}
		out = append(out, id)
	}
	return out, classify(rows.Err())
}

func requestHash(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func receipt(ctx context.Context, tx *sql.Tx, key, hash string) (int64, bool, error) {
	var gotHash string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response_version FROM squad_receipts WHERE key=?`, key).
		Scan(&gotHash, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if gotHash != hash {
		return 0, false, ErrSquadReceiptConflict
	}
	return version, true, nil
}

func countRows(ctx context.Context, tx *sql.Tx, table string) (int64, error) {
	var n int64
	// table is only a constant from this file, never request input.
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n)
	return n, err
}

// saveSquadSettingTx is the setting half of a package adoption transaction.
// The adoption writer may call it repeatedly, along with
// saveSquadEntitiesTx, while holding one s.write transaction.
func saveSquadSettingTx(ctx context.Context, tx *sql.Tx, scopeID, definitionID string,
	expected int64, payload json.RawMessage, limits SquadLimits) (int64, error) {
	if scopeID == "" || !json.Valid(payload) {
		return 0, errors.New("invalid_squad_setting")
	}
	var current int64
	err := tx.QueryRowContext(ctx,
		`SELECT version FROM squad_settings WHERE scope_id=? AND definition_id=?`,
		scopeID, definitionID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if current != expected {
		return 0, SquadVersionConflict{Current: current}
	}
	if current == 0 {
		if n, err := countRows(ctx, tx, "squad_settings"); err != nil {
			return 0, err
		} else if n >= limits.SettingsRows {
			return 0, ErrSquadCapacityFull
		}
	}
	next := current + 1
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO squad_settings(scope_id,definition_id,version,payload) VALUES(?,?,?,?)
		 ON CONFLICT(scope_id,definition_id) DO UPDATE SET version=excluded.version,payload=excluded.payload`,
		scopeID, definitionID, next, string(payload)); err != nil {
		return 0, err
	}
	return next, nil
}

// SaveSquadSetting atomically compares the version, replaces a complete
// override object and stores its retry receipt. A crash commits all three or
// none; a duplicate key returns the original version after restart.
func (s *Store) SaveSquadSetting(ctx context.Context, scopeID, definitionID string,
	expected int64, payload json.RawMessage, key string, limits SquadLimits) (int64, error) {
	var saved int64
	hash := requestHash("setting", scopeID, definitionID, fmt.Sprint(expected), string(payload))
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if v, ok, err := receipt(ctx, tx, key, hash); err != nil {
			return 0, err
		} else if ok {
			saved = v
			return 0, nil
		}
		if n, err := packageReceiptCount(ctx, tx); err != nil {
			return 0, err
		} else if n >= limits.Receipts {
			return 0, ErrSquadCapacityFull
		}
		var err error
		saved, err = saveSquadSettingTx(ctx, tx, scopeID, definitionID, expected, payload, limits)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO squad_receipts(key,request_hash,response_version) VALUES(?,?,?)`,
			key, hash, saved); err != nil {
			return 0, err
		}
		return 2, nil
	})
	return saved, err
}

// PatchSquadSetting changes only named fields. The merge happens inside the
// same immediate transaction as the version check, so a concurrent editor
// either wins first or gets version_conflict without losing another field.
func (s *Store) PatchSquadSetting(ctx context.Context, scopeID, definitionID string,
	expected int64, patch squad.Patch, key string, limits SquadLimits) (int64, error) {
	var saved int64
	encodedPatch, _ := json.Marshal(patch)
	hash := requestHash("setting-patch", scopeID, definitionID, fmt.Sprint(expected), string(encodedPatch))
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if v, ok, err := receipt(ctx, tx, key, hash); err != nil {
			return 0, err
		} else if ok {
			saved = v
			return 0, nil
		}
		var current int64
		var raw string
		err := tx.QueryRowContext(ctx,
			`SELECT version,payload FROM squad_settings WHERE scope_id=? AND definition_id=?`,
			scopeID, definitionID).Scan(&current, &raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if current != expected {
			return 0, SquadVersionConflict{Current: current}
		}
		var override squad.Override
		if current != 0 {
			if err := json.Unmarshal([]byte(raw), &override); err != nil {
				return 0, err
			}
		}
		if patch.SetHandbook {
			override.Handbook = patch.Handbook
		}
		if patch.SetAutoAssign {
			override.AutoAssign = patch.AutoAssign
		}
		if patch.SetSkills {
			override.Skills = patch.Skills
		}
		payload, err := json.Marshal(override)
		if err != nil {
			return 0, err
		}
		if n, err := packageReceiptCount(ctx, tx); err != nil {
			return 0, err
		} else if n >= limits.Receipts {
			return 0, ErrSquadCapacityFull
		}
		if current == 0 {
			if n, err := countRows(ctx, tx, "squad_settings"); err != nil {
				return 0, err
			} else if n >= limits.SettingsRows {
				return 0, ErrSquadCapacityFull
			}
		}
		saved = current + 1
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO squad_settings(scope_id,definition_id,version,payload) VALUES(?,?,?,?)
			 ON CONFLICT(scope_id,definition_id) DO UPDATE SET version=excluded.version,payload=excluded.payload`,
			scopeID, definitionID, saved, string(payload)); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO squad_receipts(key,request_hash,response_version) VALUES(?,?,?)`,
			key, hash, saved); err != nil {
			return 0, err
		}
		return 2, nil
	})
	return saved, err
}

type SquadEntityRow struct {
	Kind    string
	ID      string
	Version string
	Digest  string
	Payload json.RawMessage
}

func (s *Store) SquadCatalogRows(ctx context.Context, history bool) (int64, []SquadEntityRow, error) {
	if err := reading(); err != nil {
		return 0, nil, err
	}
	var version int64
	if err := s.rd.QueryRowContext(ctx, `SELECT version FROM squad_catalog_state WHERE singleton=1`).Scan(&version); err != nil {
		return 0, nil, classify(err)
	}
	query := `SELECT e.kind,e.id,e.version,e.digest,e.payload FROM squad_entities e
	 JOIN squad_active a ON a.kind=e.kind AND a.id=e.id AND a.version=e.version
	 ORDER BY e.kind,e.id`
	if history {
		query = `SELECT kind,id,version,digest,payload FROM squad_entities ORDER BY kind,id,version`
	}
	rows, err := s.rd.QueryContext(ctx, query)
	if err != nil {
		return 0, nil, classify(err)
	}
	defer rows.Close()
	out := []SquadEntityRow{}
	for rows.Next() {
		var row SquadEntityRow
		var payload string
		if err := rows.Scan(&row.Kind, &row.ID, &row.Version, &row.Digest, &payload); err != nil {
			return 0, nil, classify(err)
		}
		row.Payload = json.RawMessage(payload)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, classify(err)
	}
	return version, out, nil
}

func (s *Store) SquadEntityVersion(ctx context.Context, kind, id, version string) (SquadEntityRow, bool, error) {
	if err := reading(); err != nil {
		return SquadEntityRow{}, false, err
	}
	var row SquadEntityRow
	var payload string
	err := s.rd.QueryRowContext(ctx,
		`SELECT digest,payload FROM squad_entities WHERE kind=? AND id=? AND version=?`,
		kind, id, version).Scan(&row.Digest, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return SquadEntityRow{}, false, nil
	}
	if err != nil {
		return SquadEntityRow{}, false, classify(err)
	}
	row.Kind, row.ID, row.Version, row.Payload = kind, id, version, json.RawMessage(payload)
	return row, true, nil
}

// saveSquadEntitiesTx is shared with package adoption. The caller owns one
// s.write transaction and can add preview validation, selected settings and
// its adoption receipt before commit. This helper never commits or receipts.
func saveSquadEntitiesTx(ctx context.Context, tx *sql.Tx, rows []SquadEntityRow,
	expected int64, limits SquadLimits) (int64, int64, error) {
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM squad_catalog_state WHERE singleton=1`).Scan(&current); err != nil {
		return 0, 0, err
	}
	if current != expected {
		return 0, 0, SquadVersionConflict{Current: current}
	}
	if len(rows) == 0 {
		return current, 0, nil
	}
	seen := map[string]bool{}
	newRows := int64(0)
	for _, row := range rows {
		identity := row.Kind + "\x00" + row.ID
		if seen[identity] || row.Kind == "" || row.ID == "" || row.Version == "" ||
			row.Digest == "" || !json.Valid(row.Payload) {
			return 0, 0, errors.New("invalid_squad_batch")
		}
		seen[identity] = true
		var oldDigest, oldPayload string
		err := tx.QueryRowContext(ctx,
			`SELECT digest,payload FROM squad_entities WHERE kind=? AND id=? AND version=?`,
			row.Kind, row.ID, row.Version).Scan(&oldDigest, &oldPayload)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
		if err == nil && (oldDigest != row.Digest || oldPayload != string(row.Payload)) {
			return 0, 0, ErrSquadVersionContent
		}
		if errors.Is(err, sql.ErrNoRows) {
			newRows++
		}
	}
	if n, err := countRows(ctx, tx, "squad_entities"); err != nil {
		return 0, 0, err
	} else if n+newRows > limits.Entities {
		return 0, 0, ErrSquadCapacityFull
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO squad_entities(kind,id,version,digest,payload) VALUES(?,?,?,?,?)`,
			row.Kind, row.ID, row.Version, row.Digest, string(row.Payload)); err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO squad_active(kind,id,version) VALUES(?,?,?)
			 ON CONFLICT(kind,id) DO UPDATE SET version=excluded.version`,
			row.Kind, row.ID, row.Version); err != nil {
			return 0, 0, err
		}
	}
	next := current + 1
	if _, err := tx.ExecContext(ctx, `UPDATE squad_catalog_state SET version=? WHERE singleton=1`, next); err != nil {
		return 0, 0, err
	}
	return next, int64(len(rows))*2 + newRows + 1, nil
}

// SaveSquadEntities publishes an entire version set or none of it, with one
// catalog CAS and one durable retry receipt.
func (s *Store) SaveSquadEntities(ctx context.Context, rows []SquadEntityRow,
	expected int64, key string, limits SquadLimits) (int64, error) {
	if len(rows) == 0 {
		return 0, errors.New("invalid_squad_batch")
	}
	var saved int64
	raw, _ := json.Marshal(rows)
	hash := requestHash("entity-batch", fmt.Sprint(expected), string(raw))
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if v, ok, err := receipt(ctx, tx, key, hash); err != nil {
			return 0, err
		} else if ok {
			saved = v
			return 0, nil
		}
		if n, err := packageReceiptCount(ctx, tx); err != nil {
			return 0, err
		} else if n >= limits.Receipts {
			return 0, ErrSquadCapacityFull
		}
		var changed int64
		var err error
		saved, changed, err = saveSquadEntitiesTx(ctx, tx, rows, expected, limits)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO squad_receipts(key,request_hash,response_version) VALUES(?,?,?)`,
			key, hash, saved); err != nil {
			return 0, err
		}
		return changed + 1, nil
	})
	return saved, err
}

func (s *Store) SaveSquadEntity(ctx context.Context, row SquadEntityRow,
	expected int64, key string, limits SquadLimits) (int64, error) {
	return s.SaveSquadEntities(ctx, []SquadEntityRow{row}, expected, key, limits)
}
