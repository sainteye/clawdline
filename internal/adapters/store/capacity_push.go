package store

import (
	"context"
	"database/sql"
)

// The capacity register's pushes (design-decisions C4) are outbox rows like
// every other effect: the `capacity.notify` event that says a row crossed a
// line and the push it owes are one commit, and the push runs after it.
//
// What this file adds is the one question the register asks the outbox that
// nothing else did — what did an earlier process last say about each row —
// and an index so that asking it reads the rows of one kind and not the whole
// outbox. The outbox is never pruned, so a scan would grow with every effect
// this daemon has ever run; the index keeps the answer at one row per subject.

const capacityPushSchema = `
CREATE INDEX IF NOT EXISTS outbox_kind ON outbox(kind, subject, id);
`

func openCapacityPush(db *sql.DB) error {
	_, err := db.Exec(capacityPushSchema)
	return err
}

// LatestEffects is the newest effect of one kind for each subject, in subject
// order: for the capacity register, the last push each row owed, whatever
// became of it.
func (s *Store) LatestEffects(ctx context.Context, kind string) ([]Effect, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+effectColumns+` FROM outbox WHERE id IN
		   (SELECT MAX(id) FROM outbox WHERE kind = ? GROUP BY subject)
		 ORDER BY subject ASC`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Effect{}
	for rows.Next() {
		e, err := scanEffect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
