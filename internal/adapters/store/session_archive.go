package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The Sessions a person archived (docs/session-archive.md).
//
// Archiving closes a Session's process to give its memory back and keeps
// this row so the same conversation can be resumed later. A conversation has
// one row: archiving it again replaces the row, because the newer title and
// directory are the ones worth resuming. Rows are removed only when a restore
// opened the conversation again, or past the bound, oldest first.
const sessionArchiveSchema = `
CREATE TABLE IF NOT EXISTS session_archive (
  conversation_id TEXT    PRIMARY KEY,
  assistant       TEXT    NOT NULL CHECK (assistant IN ('claude', 'codex')),
  cwd             TEXT    NOT NULL,
  place           TEXT    NOT NULL,
  title           TEXT    NOT NULL DEFAULT '',
  persona         TEXT    NOT NULL DEFAULT '',
  backend         TEXT    NOT NULL DEFAULT '',
  archived_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_archive_at ON session_archive (archived_at DESC, conversation_id);
`

func openSessionArchive(db *sql.DB) error {
	_, err := db.Exec(sessionArchiveSchema)
	return err
}

// ArchiveRow is one archived conversation.
type ArchiveRow struct {
	ConversationID string
	Assistant      string
	CWD            string
	Place          string
	// Title is the label the Session row showed when it was archived.
	Title string
	// Persona is the built-in persona the Session was launched with; empty
	// for none.
	Persona    string
	Backend    string
	ArchivedAt time.Time
}

// ArchiveSession writes row, replacing any row the conversation had, keeps
// only the keep most recently archived rows, and answers how many it dropped.
func (s *Store) ArchiveSession(ctx context.Context, row ArchiveRow, keep int) (int64, error) {
	if row.ConversationID == "" {
		return 0, errors.New("a conversation with no id is not archived")
	}
	keep = max(keep, 1)
	var dropped int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		dropped = 0
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO session_archive (conversation_id, assistant, cwd, place, title, persona, backend, archived_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(conversation_id) DO UPDATE SET
			   assistant = excluded.assistant, cwd = excluded.cwd, place = excluded.place,
			   title = excluded.title, persona = excluded.persona, backend = excluded.backend,
			   archived_at = excluded.archived_at`,
			row.ConversationID, row.Assistant, row.CWD, row.Place, row.Title, row.Persona, row.Backend,
			row.ArchivedAt.Unix()); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx,
			`DELETE FROM session_archive WHERE conversation_id IN (
			   SELECT conversation_id FROM session_archive
			   ORDER BY archived_at DESC, conversation_id LIMIT -1 OFFSET ?)`, keep)
		if err != nil {
			return 0, err
		}
		if dropped, err = res.RowsAffected(); err != nil {
			return 0, err
		}
		return 1, nil
	})
	return dropped, err
}

// ArchivedSessions is every archived conversation, most recently archived
// first.
func (s *Store) ArchivedSessions(ctx context.Context) ([]ArchiveRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx,
		`SELECT conversation_id, assistant, cwd, place, title, persona, backend, archived_at
		 FROM session_archive ORDER BY archived_at DESC, conversation_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []ArchiveRow{}
	for rows.Next() {
		var r ArchiveRow
		var at int64
		if err := rows.Scan(&r.ConversationID, &r.Assistant, &r.CWD, &r.Place, &r.Title, &r.Persona,
			&r.Backend, &at); err != nil {
			return nil, classify(err)
		}
		r.ArchivedAt = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, classify(rows.Err())
}

// RemoveArchived forgets one archived conversation. A conversation with no
// row is not an error.
func (s *Store) RemoveArchived(ctx context.Context, conversation string) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM session_archive WHERE conversation_id = ?`, conversation)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}
