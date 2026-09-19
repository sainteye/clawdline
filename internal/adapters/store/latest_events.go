package store

import (
	"context"
	"database/sql"
	"time"
)

// The newest event of one kind about each of a few subjects: how the session
// list reads a session's own "this turn is delivered" receipt
// (`session.delivered`, keyed by terminal) from this daemon's store rather than
// from the Swift app's (cutover B1).
//
// The index is what keeps it one seek per subject. The events table only
// grows, and the list is built every time a page asks; without the index each
// build would scan every event ever recorded.
const latestEventsSchema = `
CREATE INDEX IF NOT EXISTS events_kind_subject ON events(kind, subject, seq);
`

func openLatestEvents(db *sql.DB) error {
	_, err := db.Exec(latestEventsSchema)
	return err
}

// LatestEvents answers, for each subject that has one, the newest event of
// kind about it. A subject with none is absent from the map, which is a known
// answer: the table was read.
func (s *Store) LatestEvents(ctx context.Context, kind string, subjects []string) (map[string]Event, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := make(map[string]Event, len(subjects))
	for _, subject := range subjects {
		if _, done := out[subject]; done || subject == "" {
			continue
		}
		var e Event
		var at int64
		var payload string
		err := s.db.QueryRowContext(ctx,
			`SELECT seq, at, payload FROM events WHERE kind = ? AND subject = ? ORDER BY seq DESC LIMIT 1`,
			kind, subject).Scan(&e.Seq, &at, &payload)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, classify(err)
		}
		e.At, e.Kind, e.Subject, e.Payload = time.Unix(at, 0), kind, subject, []byte(payload)
		out[subject] = e
	}
	return out, nil
}
