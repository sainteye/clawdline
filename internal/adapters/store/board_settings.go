package store

import (
	"context"
	"database/sql"
	"errors"
)

// The board's two settings — whether it is on, and which assistant may read
// it to write narratives — and their revision (design-decisions D37).
//
// They used to be a JSON document of their own, `project-board.json`, with
// the command receipts inside it, evicted oldest first by count. They are one
// row here now, and a command's receipt is the store's one receipt table
// (D03): claimed before the command, completed in the transaction that
// changes the row, and replayed with the revision that command produced —
// never the revision the board has reached since.
//
// The old document is read once, when there is no row yet, and never
// written again: it is this daemon's own file, and what it said is kept.

const boardSettingsSchema = `
CREATE TABLE IF NOT EXISTS board_settings (
  id                 INTEGER PRIMARY KEY CHECK (id = 1),
  revision           INTEGER NOT NULL CHECK (revision >= 0),
  enabled            INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  narrative_consent  TEXT    CHECK (narrative_consent IS NULL OR narrative_consent IN ('codex', 'claude')),
  presentation_epoch INTEGER NOT NULL,
  updated_at         REAL    NOT NULL,
  imported_from      TEXT
);
`

// BoardSettings is the row.
type BoardSettings struct {
	Revision          int64
	Enabled           bool
	NarrativeConsent  *string
	PresentationEpoch int
	UpdatedAt         float64
}

// ErrNoBoardSettings is "nothing has been written or imported yet", which is
// not the same as a read that failed.
var ErrNoBoardSettings = errors.New("no board settings stored")

func scanBoardSettings(sc scanner) (BoardSettings, error) {
	var b BoardSettings
	var enabled int
	var consent sql.NullString
	err := sc.Scan(&b.Revision, &enabled, &consent, &b.PresentationEpoch, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return BoardSettings{}, ErrNoBoardSettings
	}
	if err != nil {
		return BoardSettings{}, err
	}
	b.Enabled = enabled == 1
	if consent.Valid {
		v := consent.String
		b.NarrativeConsent = &v
	}
	return b, nil
}

const boardSettingsColumns = `revision, enabled, narrative_consent, presentation_epoch, updated_at`

// BoardSettings reads the row.
func (s *Store) BoardSettings(ctx context.Context) (BoardSettings, error) {
	if err := reading(); err != nil {
		return BoardSettings{}, err
	}
	return scanBoardSettings(s.db.QueryRowContext(ctx,
		`SELECT `+boardSettingsColumns+` FROM board_settings WHERE id = 1`))
}

// SeedBoardSettings writes the row from an older document, only if there is
// no row yet; true when it wrote.
func (s *Store) SeedBoardSettings(ctx context.Context, b BoardSettings, from string) (bool, error) {
	wrote := false
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO board_settings (id, revision, enabled, narrative_consent, presentation_epoch,
			   updated_at, imported_from) VALUES (1, ?, ?, ?, ?, ?, ?)`,
			b.Revision, boolInt(b.Enabled), b.NarrativeConsent, b.PresentationEpoch, b.UpdatedAt, from)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		wrote = n == 1
		return n, err
	})
	return wrote, err
}

// ApplyBoardSettings changes the row in one transaction: it reads the row as
// the transaction sees it (a fresh board when there is none), asks decide
// for the next one, writes it, and files answer under k in the same
// transaction. decide refusing writes nothing.
func (s *Store) ApplyBoardSettings(ctx context.Context, k ReceiptKey,
	decide func(cur BoardSettings) (BoardSettings, error), answer func(BoardSettings) ReceiptAnswer) (BoardSettings, error) {
	var next BoardSettings
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		cur, err := scanBoardSettings(tx.QueryRowContext(ctx,
			`SELECT `+boardSettingsColumns+` FROM board_settings WHERE id = 1`))
		if errors.Is(err, ErrNoBoardSettings) {
			// A fresh board is enabled, as the Swift app's StoredState.empty is.
			cur, err = BoardSettings{Enabled: true}, nil
		}
		if err != nil {
			return 0, err
		}
		next, err = decide(cur)
		if err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO board_settings (id, revision, enabled, narrative_consent, presentation_epoch, updated_at)
			 VALUES (1, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET revision = excluded.revision, enabled = excluded.enabled,
			   narrative_consent = excluded.narrative_consent, presentation_epoch = excluded.presentation_epoch,
			   updated_at = excluded.updated_at
			 WHERE board_settings.revision = ?`,
			next.Revision, boolInt(next.Enabled), next.NarrativeConsent, next.PresentationEpoch, next.UpdatedAt,
			cur.Revision)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return 0, err
		} else if n != 1 {
			return 0, ErrConflict
		}
		if err := s.completeReceipt(ctx, tx, k, answer(next)); err != nil {
			return 0, err
		}
		return 2, nil
	})
	return next, err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
