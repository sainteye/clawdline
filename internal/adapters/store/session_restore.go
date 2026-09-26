package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Which assistant sessions were open, per boot of the machine
// (docs/session-restore.md).
//
// A reboot or a crash takes every tmux pane and iTerm2 tab with it, and
// nothing else this daemon keeps says which conversations were in them. So
// every complete reading of the machine replaces the current boot's rows with
// the conversations it saw, and after the next boot the previous boot's rows
// are what can be offered back.
//
// The boots table is separate from the rows on purpose. A boot in which the
// person closed everything has no rows, and it still has to be the "previous
// boot" after the next restart — otherwise the boot before it, whose sessions
// were closed by hand long ago, would be offered instead.
const sessionRestoreSchema = `
CREATE TABLE IF NOT EXISTS restore_boots (
  boot_id    TEXT    PRIMARY KEY,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS restore_sessions (
  boot_id         TEXT    NOT NULL,
  conversation_id TEXT    NOT NULL,
  assistant       TEXT    NOT NULL CHECK (assistant IN ('claude', 'codex')),
  cwd             TEXT    NOT NULL,
  place           TEXT    NOT NULL,
  title           TEXT    NOT NULL DEFAULT '',
  backend         TEXT    NOT NULL DEFAULT '',
  first_seen      INTEGER NOT NULL,
  last_seen       INTEGER NOT NULL,
  resolution      TEXT    CHECK (resolution IS NULL OR resolution IN ('restored', 'dismissed')),
  resolved_at     INTEGER,
  PRIMARY KEY (boot_id, conversation_id)
);
`

func openSessionRestore(db *sql.DB) error {
	_, err := db.Exec(sessionRestoreSchema)
	return err
}

// The two answers a person can give about a row. A row with neither is still
// on offer.
const (
	RestoreRestored  = "restored"
	RestoreDismissed = "dismissed"
)

// RestoreRow is one conversation that was open in one boot.
type RestoreRow struct {
	ConversationID string
	Assistant      string
	CWD            string
	Place          string
	Title          string
	Backend        string
	FirstSeen      time.Time
	LastSeen       time.Time
	// Resolution is empty, RestoreRestored or RestoreDismissed.
	Resolution string
	ResolvedAt time.Time
}

// RestoreBoot is one boot this store has rows, or had a reading, for.
type RestoreBoot struct {
	ID        string
	FirstSeen time.Time
	LastSeen  time.Time
}

// RecordBoot makes boot's rows exactly rows, as of now, and keeps only the
// keep most recently seen boots, boot among them.
//
// A row already there keeps its first_seen and its resolution; a row no longer
// in rows is removed. The boot itself is stamped seen now whatever rows holds,
// including none.
func (s *Store) RecordBoot(ctx context.Context, boot string, rows []RestoreRow, now time.Time, keep int) error {
	if boot == "" {
		return errors.New("a boot with no id is not recorded")
	}
	if keep < 1 {
		keep = 1
	}
	at := now.Unix()
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO restore_boots (boot_id, first_seen, last_seen) VALUES (?, ?, ?)
			 ON CONFLICT(boot_id) DO UPDATE SET last_seen = excluded.last_seen`,
			boot, at, at); err != nil {
			return 0, err
		}
		ids := make([]any, 0, len(rows)+1)
		ids = append(ids, boot)
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO restore_sessions
				   (boot_id, conversation_id, assistant, cwd, place, title, backend, first_seen, last_seen)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(boot_id, conversation_id) DO UPDATE SET
				   assistant = excluded.assistant, cwd = excluded.cwd, place = excluded.place,
				   title = excluded.title, backend = excluded.backend, last_seen = excluded.last_seen`,
				boot, r.ConversationID, r.Assistant, r.CWD, r.Place, r.Title, r.Backend, at, at); err != nil {
				return 0, err
			}
			ids = append(ids, r.ConversationID)
		}
		gone := `DELETE FROM restore_sessions WHERE boot_id = ?`
		if len(rows) > 0 {
			gone += ` AND conversation_id NOT IN (` + placeholders(len(rows)) + `)`
		}
		if _, err := tx.ExecContext(ctx, gone, ids...); err != nil {
			return 0, err
		}
		// The boots past the keep most recent ones, and their rows. The
		// current boot is never among them, whatever the clock says.
		old, err := tx.QueryContext(ctx,
			`SELECT boot_id FROM restore_boots WHERE boot_id != ?
			 ORDER BY last_seen DESC, boot_id DESC LIMIT -1 OFFSET ?`, boot, keep-1)
		if err != nil {
			return 0, err
		}
		var stale []any
		for old.Next() {
			var id string
			if err := old.Scan(&id); err != nil {
				old.Close()
				return 0, err
			}
			stale = append(stale, id)
		}
		old.Close()
		if len(stale) > 0 {
			in := placeholders(len(stale))
			if _, err := tx.ExecContext(ctx, `DELETE FROM restore_sessions WHERE boot_id IN (`+in+`)`, stale...); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM restore_boots WHERE boot_id IN (`+in+`)`, stale...); err != nil {
				return 0, err
			}
		}
		return 1, nil
	})
}

// PreviousBoot is the most recently seen boot that is not current.
func (s *Store) PreviousBoot(ctx context.Context, current string) (RestoreBoot, bool, error) {
	if err := reading(); err != nil {
		return RestoreBoot{}, false, err
	}
	var b RestoreBoot
	var first, last int64
	err := s.db.QueryRowContext(ctx,
		`SELECT boot_id, first_seen, last_seen FROM restore_boots WHERE boot_id != ?
		 ORDER BY last_seen DESC, boot_id DESC LIMIT 1`, current).Scan(&b.ID, &first, &last)
	if err == sql.ErrNoRows {
		return RestoreBoot{}, false, nil
	}
	if err != nil {
		return RestoreBoot{}, false, classify(err)
	}
	b.FirstSeen, b.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
	return b, true, nil
}

// RestoreRows is every row of one boot, resolved or not, most recently seen
// first.
func (s *Store) RestoreRows(ctx context.Context, boot string) ([]RestoreRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT conversation_id, assistant, cwd, place, title, backend, first_seen, last_seen,
		        resolution, resolved_at
		 FROM restore_sessions WHERE boot_id = ?
		 ORDER BY last_seen DESC, first_seen DESC, conversation_id`, boot)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []RestoreRow{}
	for rows.Next() {
		var r RestoreRow
		var first, last int64
		var resolution sql.NullString
		var resolved sql.NullInt64
		if err := rows.Scan(&r.ConversationID, &r.Assistant, &r.CWD, &r.Place, &r.Title, &r.Backend,
			&first, &last, &resolution, &resolved); err != nil {
			return nil, classify(err)
		}
		r.FirstSeen, r.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		r.Resolution = resolution.String
		if resolved.Valid {
			r.ResolvedAt = time.Unix(resolved.Int64, 0)
		}
		out = append(out, r)
	}
	return out, classify(rows.Err())
}

// ResolveRestore gives the still-unresolved rows of boot named by ids — every
// one of them when ids is nil — the resolution, and answers how many it
// changed. A row already resolved keeps its first answer.
func (s *Store) ResolveRestore(ctx context.Context, boot string, ids []string, resolution string, now time.Time) (int64, error) {
	if resolution != RestoreRestored && resolution != RestoreDismissed {
		return 0, errors.New("unknown resolution " + resolution)
	}
	if ids != nil && len(ids) == 0 {
		return 0, nil
	}
	var changed int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		q := `UPDATE restore_sessions SET resolution = ?, resolved_at = ?
		      WHERE boot_id = ? AND resolution IS NULL`
		args := []any{resolution, now.Unix(), boot}
		if ids != nil {
			q += ` AND conversation_id IN (` + placeholders(len(ids)) + `)`
			for _, id := range ids {
				args = append(args, id)
			}
		}
		res, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return 0, err
		}
		changed, err = res.RowsAffected()
		return changed, err
	})
	return changed, err
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
