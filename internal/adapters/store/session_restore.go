package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Which assistant sessions were open, per boot of the machine
// (docs/session-restore.md).
//
// A reboot or a crash takes every tmux pane and iTerm2 tab with it, and
// nothing else this daemon keeps says which conversations were in them. So
// every complete reading of the machine brings the current boot's rows up to
// date with the conversations it saw, and after the next boot the previous
// boot's rows are what can be offered back.
//
// **A conversation a reading no longer shows is not deleted.** A shutdown
// quits iTerm2 and kills the tmux server while this daemon may still be
// scanning, and both then answer complete and empty; were a row deleted by
// that reading, the last seconds before a restart would erase the very record
// the restart needs. So the row stays and gets gone_at, the time of the first
// complete reading that did not see it, and the boot's last_seen is kept
// moving by a heartbeat (TouchBoot). Which of the gone rows are worth offering
// — those that went in the final minutes — is the reader's rule
// (app.SessionRestore.Restorable), not the store's.
//
// The boots table is separate from the rows on purpose. A boot in which the
// person closed everything has no rows worth offering, and it still has to be
// the "previous boot" after the next restart — otherwise the boot before it,
// whose sessions were closed by hand long ago, would be offered instead.
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
  gone_at         INTEGER,
  closed_at       INTEGER,
  PRIMARY KEY (boot_id, conversation_id)
);
`

// The columns added after the tables first shipped. A store from that commit
// has the tables without them, and CREATE TABLE IF NOT EXISTS leaves it so.
var sessionRestoreColumns = []struct{ column, ddl string }{
	{"gone_at", "ALTER TABLE restore_sessions ADD COLUMN gone_at INTEGER"},
	{"closed_at", "ALTER TABLE restore_sessions ADD COLUMN closed_at INTEGER"},
}

func openSessionRestore(db *sql.DB) error {
	if _, err := db.Exec(sessionRestoreSchema); err != nil {
		return err
	}
	for _, step := range sessionRestoreColumns {
		has, err := hasColumn(db, "restore_sessions", step.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.Exec(step.ddl); err != nil {
			return fmt.Errorf("restore_sessions.%s: %w", step.column, err)
		}
	}
	return nil
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
	// GoneAt is when the first complete reading that no longer showed the
	// conversation was taken; zero while it is open.
	GoneAt time.Time
	// ClosedAt is when the person closed it through this daemon; zero when
	// they did not, or when it was open again afterwards.
	ClosedAt time.Time
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
// BootReading is one complete reading of the machine, as RecordBoot keeps it.
type BootReading struct {
	Boot string
	// Rows is every conversation the reading showed.
	Rows []RestoreRow
	// At is when the reading is recorded. ScannedAt is when the scan behind
	// it began; zero is At.
	At, ScannedAt time.Time
	// KeepBoots is how many of the most recently seen boots are kept, Boot
	// among them; KeepRows how many rows one boot keeps.
	KeepBoots, KeepRows int
}

// RecordBoot brings the boot's rows up to date with one complete reading, and
// answers how many rows went past KeepRows.
//
// A row in the reading is written with its latest details, keeps its
// first_seen and its resolution, and is open (gone_at cleared). A row not in
// it keeps everything and gets gone_at = At, unless it already had one. Past
// KeepRows the rows that went longest ago are dropped first. The boot is
// stamped seen At whatever the reading holds, including nothing.
//
// A row closed through this daemon stays closed while the reading's scan began
// no later than the close: that scan saw the terminal before it was taken.
func (s *Store) RecordBoot(ctx context.Context, rd BootReading) (int64, error) {
	if rd.Boot == "" {
		return 0, errors.New("a boot with no id is not recorded")
	}
	keepBoots, keepRows := max(rd.KeepBoots, 1), max(rd.KeepRows, 1)
	at := rd.At.Unix()
	scanned := at
	if !rd.ScannedAt.IsZero() {
		scanned = rd.ScannedAt.Unix()
	}
	boot := rd.Boot
	var evicted int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		evicted = 0
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO restore_boots (boot_id, first_seen, last_seen) VALUES (?, ?, ?)
			 ON CONFLICT(boot_id) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen)`,
			boot, at, at); err != nil {
			return 0, err
		}
		ids := make([]any, 0, len(rd.Rows)+2)
		ids = append(ids, at, boot)
		for _, r := range rd.Rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO restore_sessions
				   (boot_id, conversation_id, assistant, cwd, place, title, backend, first_seen, last_seen)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(boot_id, conversation_id) DO UPDATE SET
				   assistant = excluded.assistant, cwd = excluded.cwd, place = excluded.place,
				   title = excluded.title, backend = excluded.backend, last_seen = excluded.last_seen,
				   gone_at   = CASE WHEN closed_at >= ? THEN gone_at ELSE NULL END,
				   closed_at = CASE WHEN closed_at >= ? THEN closed_at ELSE NULL END`,
				boot, r.ConversationID, r.Assistant, r.CWD, r.Place, r.Title, r.Backend, at, at,
				scanned, scanned); err != nil {
				return 0, err
			}
			ids = append(ids, r.ConversationID)
		}
		gone := `UPDATE restore_sessions SET gone_at = ? WHERE boot_id = ? AND gone_at IS NULL`
		if len(rd.Rows) > 0 {
			gone += ` AND conversation_id NOT IN (` + placeholders(len(rd.Rows)) + `)`
		}
		if _, err := tx.ExecContext(ctx, gone, ids...); err != nil {
			return 0, err
		}
		// Past the row limit: open rows first, then the most recently gone.
		res, err := tx.ExecContext(ctx,
			`DELETE FROM restore_sessions WHERE boot_id = ? AND conversation_id IN (
			   SELECT conversation_id FROM restore_sessions WHERE boot_id = ?
			   ORDER BY gone_at IS NULL DESC, gone_at DESC, last_seen DESC, conversation_id
			   LIMIT -1 OFFSET ?)`, boot, boot, keepRows)
		if err != nil {
			return 0, err
		}
		if evicted, err = res.RowsAffected(); err != nil {
			return 0, err
		}
		// The boots past the keep most recent ones, and their rows. The
		// current boot is never among them, whatever the clock says.
		old, err := tx.QueryContext(ctx,
			`SELECT boot_id FROM restore_boots WHERE boot_id != ?
			 ORDER BY last_seen DESC, boot_id DESC LIMIT -1 OFFSET ?`, boot, keepBoots-1)
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
	return evicted, err
}

// TouchBoot is the heartbeat: it moves the boot's last_seen to now, never
// back, and changes nothing else. A boot not recorded yet is left unrecorded.
func (s *Store) TouchBoot(ctx context.Context, boot string, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`UPDATE restore_boots SET last_seen = MAX(last_seen, ?) WHERE boot_id = ?`, now.Unix(), boot)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// CloseRestore marks one conversation of boot as closed by the person through
// this daemon, now, and gone now unless it already was.
func (s *Store) CloseRestore(ctx context.Context, boot, conversation string, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`UPDATE restore_sessions SET closed_at = ?, gone_at = COALESCE(gone_at, ?)
			 WHERE boot_id = ? AND conversation_id = ?`, now.Unix(), now.Unix(), boot, conversation)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
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
		        resolution, resolved_at, gone_at, closed_at
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
		var resolved, gone, closed sql.NullInt64
		if err := rows.Scan(&r.ConversationID, &r.Assistant, &r.CWD, &r.Place, &r.Title, &r.Backend,
			&first, &last, &resolution, &resolved, &gone, &closed); err != nil {
			return nil, classify(err)
		}
		r.FirstSeen, r.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		r.Resolution = resolution.String
		if resolved.Valid {
			r.ResolvedAt = time.Unix(resolved.Int64, 0)
		}
		if gone.Valid {
			r.GoneAt = time.Unix(gone.Int64, 0)
		}
		if closed.Valid {
			r.ClosedAt = time.Unix(closed.Int64, 0)
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
