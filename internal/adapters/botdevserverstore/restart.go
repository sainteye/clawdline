package botdevserverstore

import (
	"context"
	"database/sql"
	"encoding/hex"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ servers.Restarts = (*Catalog)(nil)

// This optional source-local table does not alter the portable Bot format or
// the existing catalog. An older writer may ignore it; the complete current
// attempt/revision comparison makes such a writer's old checkpoint inert.
const restartTable = `CREATE TABLE bot_devserver_restarts (
 id TEXT NOT NULL PRIMARY KEY, boot TEXT NOT NULL,
 attempt_key TEXT NOT NULL, revision INTEGER NOT NULL)`

func restartReady(ctx context.Context, tx *sql.Tx) error {
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM sqlite_master WHERE name='bot_devserver_restarts'`).Scan(&kind); err != nil {
		if err == sql.ErrNoRows {
			return refusal(pa.Disabled, "server_restarts_absent")
		}
		return err
	}
	if kind != "table" {
		return refusal(pa.Unsupported, "server_restarts_shape")
	}
	return columnsMatch(ctx, tx, `PRAGMA table_info(bot_devserver_restarts)`, []column{{"id", "TEXT", 1, 1}, {"boot", "TEXT", 1, 0}, {"attempt_key", "TEXT", 1, 0}, {"revision", "INTEGER", 1, 0}})
}

// InstallRestarts is explicit Host composition. Merely opening a catalog or
// inspecting definitions never installs a table or consumes a restart intent.
func InstallRestarts(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return refusal(pa.Invalid, "server_restart_database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	if err := restartReady(ctx, tx); pa.Code(err) == pa.Disabled {
		if _, err := tx.ExecContext(ctx, restartTable); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Catalog) SaveRestart(ctx context.Context, row servers.Restart) error {
	if err := row.Validate(); err != nil {
		return err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return err
	}
	if err := restartReady(ctx, tx); err != nil {
		return err
	}
	got, _, err := definition(ctx, tx, row.Saved.ID)
	if err != nil {
		return err
	}
	if got != row.Saved {
		return refusal(pa.StaleVersion, "server_restart")
	}
	var count, exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*),count(CASE WHEN id=? THEN 1 END) FROM bot_devserver_restarts`, got.ID).Scan(&count, &exists); err != nil {
		return err
	}
	// Checkpoints share the definition budget; they cannot become an
	// independently growing retry or history queue.
	if count > capacity.MaximumBotServerDefinitions || (exists == 0 && count == capacity.MaximumBotServerDefinitions) {
		return refusal(pa.CapacityFull, "server_definitions")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bot_devserver_restarts(id,boot,attempt_key,revision) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET boot=excluded.boot,attempt_key=excluded.attempt_key,revision=excluded.revision`, got.ID, row.Boot, key(got.Attempt.Identity), got.Attempt.Version)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Catalog) TakeRestarts(ctx context.Context) ([]servers.Restart, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := catalogReady(ctx, tx); err != nil {
		return nil, err
	}
	if err := restartReady(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,boot,attempt_key,revision FROM bot_devserver_restarts ORDER BY id LIMIT ?`, capacity.MaximumBotServerDefinitions+1)
	if err != nil {
		return nil, err
	}
	type checkpoint struct {
		id, boot, attempt string
		revision          int64
	}
	var checkpoints []checkpoint
	for rows.Next() {
		var row checkpoint
		if err := rows.Scan(&row.id, &row.boot, &row.attempt, &row.revision); err != nil {
			rows.Close()
			return nil, err
		}
		checkpoints = append(checkpoints, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(checkpoints) > capacity.MaximumBotServerDefinitions {
		return nil, refusal(pa.CapacityFull, "server_definitions")
	}
	var result []servers.Restart
	for _, row := range checkpoints {
		boot, err := hex.DecodeString(row.boot)
		if err != nil || len(boot) != 32 || row.revision <= 0 {
			return nil, refusal(pa.Invalid, "server_restart")
		}
		saved, current, err := definition(ctx, tx, row.id)
		if pa.Code(err) == pa.NotFound {
			continue // An explicitly removed definition cannot restart.
		}
		if err != nil {
			return nil, err
		}
		if current != row.attempt || saved.Attempt.Version != row.revision {
			continue // An intervening old/new writer superseded the checkpoint.
		}
		r := servers.Restart{Saved: saved, Boot: row.boot}
		if err := r.Validate(); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_devserver_restarts`); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err // Never return work after a possibly committed Take.
	}
	return result, nil
}
