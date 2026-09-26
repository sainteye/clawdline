package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Verifications are the things a person has said they will check later, kept
// until the person accepts or rejects them and deletes them
// (docs/verifications.md). Being told "remember to check this on the third"
// is how a check is forgotten; a row here is how it is not.
//
// Three tables: the record, its criteria in the order they were written, and
// the notes appended to it over time. Criteria and notes go with the record
// (ON DELETE CASCADE), because a deleted check has nothing left to say.
//
// `verification_seeds` remembers which records this daemon planted by itself
// (app.SeedVerifications). It outlives the record on purpose: a seed the
// person deleted stays deleted, and the next start does not plant it again.
const verificationSchema = `
CREATE TABLE IF NOT EXISTS verifications (
  id            TEXT    PRIMARY KEY,
  title         TEXT    NOT NULL,
  why           TEXT    NOT NULL DEFAULT '',
  started_at    INTEGER NOT NULL,
  due_at        INTEGER NOT NULL,
  source_kind   TEXT    NOT NULL DEFAULT '',
  source_since  TEXT    NOT NULL DEFAULT '',
  schedule_id   TEXT    NOT NULL DEFAULT '',
  status        TEXT    NOT NULL DEFAULT 'open',
  close_reason  TEXT    NOT NULL DEFAULT '',
  closed_at     INTEGER NOT NULL DEFAULT 0,
  seed          TEXT    NOT NULL DEFAULT '',
  create_key    TEXT    NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS verifications_by_due ON verifications(status, due_at);
CREATE UNIQUE INDEX IF NOT EXISTS verifications_by_create_key ON verifications(create_key) WHERE create_key != '';
CREATE TABLE IF NOT EXISTS verification_criteria (
  verification_id TEXT    NOT NULL REFERENCES verifications(id) ON DELETE CASCADE,
  position        INTEGER NOT NULL,
  text            TEXT    NOT NULL,
  state           TEXT    NOT NULL DEFAULT 'unset',
  updated_at      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (verification_id, position)
);
CREATE TABLE IF NOT EXISTS verification_notes (
  seq             INTEGER PRIMARY KEY AUTOINCREMENT,
  verification_id TEXT    NOT NULL REFERENCES verifications(id) ON DELETE CASCADE,
  at              INTEGER NOT NULL,
  author_kind     TEXT    NOT NULL,
  author          TEXT    NOT NULL DEFAULT '',
  text            TEXT    NOT NULL,
  note_key        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS verification_notes_by_record ON verification_notes(verification_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS verification_notes_by_key ON verification_notes(verification_id, note_key) WHERE note_key != '';
CREATE TABLE IF NOT EXISTS verification_seeds (
  seed      TEXT    PRIMARY KEY,
  seeded_at INTEGER NOT NULL
);
`

func openVerifications(db *sql.DB) error {
	_, err := db.Exec(verificationSchema)
	return err
}

// Verification is one record as the store keeps it. The rules about what a
// record may say live in internal/app (verify.go); this is the row.
type Verification struct {
	ID, Title, Why          string
	StartedAt, DueAt        int64
	SourceKind, SourceSince string
	ScheduleID              string
	Status                  string
	CloseReason             string
	ClosedAt                int64
	Seed                    string
	CreatedAt, UpdatedAt    int64
	Criteria                []VerificationCriterion
	Notes                   []VerificationNote
}

// VerificationCriterion is one sentence the check is judged by, and whether
// it has been: `unset`, `passed` or `failed`.
type VerificationCriterion struct {
	Index     int
	Text      string
	State     string
	UpdatedAt int64
}

// VerificationNote is one thing somebody wrote on the record: the person, or
// a session (a scheduled task writing its readout, an assistant through the
// CLI).
type VerificationNote struct {
	ID         int64
	At         int64
	AuthorKind string
	Author     string
	Text       string
}

// The refusals a verification write can meet. Each is decided inside the
// write's own transaction, so two writers cannot both find room for one more.
var (
	ErrVerificationNotFound  = errors.New("verification: no such record")
	ErrVerificationFull      = errors.New("verification: the store holds as many records as it may")
	ErrVerificationNotesFull = errors.New("verification: the record holds as many notes as it may")
	ErrVerificationClosed    = errors.New("verification: the record is closed")
	ErrVerificationOpen      = errors.New("verification: the record is still open")
	ErrVerificationCriterion = errors.New("verification: the record has no such criterion")
	ErrVerificationSchedule  = errors.New("verification: the record is not linked to that schedule")
)

const verificationColumns = `id, title, why, started_at, due_at, source_kind, source_since, schedule_id,
  status, close_reason, closed_at, seed, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanVerification(r rowScanner) (Verification, error) {
	var v Verification
	err := r.Scan(&v.ID, &v.Title, &v.Why, &v.StartedAt, &v.DueAt, &v.SourceKind, &v.SourceSince,
		&v.ScheduleID, &v.Status, &v.CloseReason, &v.ClosedAt, &v.Seed, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

// rowQuerier is what both a handle and a transaction can read through.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Verifications is every record, soonest due first among the open ones and
// then the closed ones, each with its criteria and notes.
// ScheduleIDs lists the stored schedules' ids and nothing else. Unlike
// ScheduleFiles it stamps nothing, so asking which schedules exist leaves
// the scheduler's first sight of a row to the scheduler.
func (s *Store) ScheduleIDs(ctx context.Context) ([]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM schedule_files ORDER BY id`)
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

func (s *Store) Verifications(ctx context.Context) ([]Verification, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+verificationColumns+` FROM verifications
		ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END, due_at, created_at, id`)
	if err != nil {
		return nil, classify(err)
	}
	out := []Verification{}
	for rows.Next() {
		v, err := scanVerification(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		if err := fillVerification(ctx, s.db, &out[i]); err != nil {
			return nil, classify(err)
		}
	}
	return out, nil
}

// Verification is one record, and whether it exists.
func (s *Store) Verification(ctx context.Context, id string) (Verification, bool, error) {
	if err := reading(); err != nil {
		return Verification{}, false, err
	}
	v, found, err := oneVerification(ctx, s.db, id)
	return v, found, classify(err)
}

func oneVerification(ctx context.Context, q rowQuerier, id string) (Verification, bool, error) {
	v, err := scanVerification(q.QueryRowContext(ctx,
		`SELECT `+verificationColumns+` FROM verifications WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Verification{}, false, nil
	}
	if err != nil {
		return Verification{}, false, err
	}
	if err := fillVerification(ctx, q, &v); err != nil {
		return Verification{}, false, err
	}
	return v, true, nil
}

func fillVerification(ctx context.Context, q rowQuerier, v *Verification) error {
	v.Criteria, v.Notes = []VerificationCriterion{}, []VerificationNote{}
	rows, err := q.QueryContext(ctx, `SELECT position, text, state, updated_at FROM verification_criteria
		WHERE verification_id = ? ORDER BY position`, v.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c VerificationCriterion
		if err := rows.Scan(&c.Index, &c.Text, &c.State, &c.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		v.Criteria = append(v.Criteria, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT seq, at, author_kind, author, text FROM verification_notes
		WHERE verification_id = ? ORDER BY seq`, v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n VerificationNote
		if err := rows.Scan(&n.ID, &n.At, &n.AuthorKind, &n.Author, &n.Text); err != nil {
			return err
		}
		v.Notes = append(v.Notes, n)
	}
	return rows.Err()
}

// CreateVerification writes one new record with its criteria. A key the
// store has already seen answers the record that key made, unchanged, and
// created false: a phone that sent twice has made one record. The limit is
// how many records the store may hold, counted in here.
func (s *Store) CreateVerification(ctx context.Context, v Verification, key string, limit int) (Verification, bool, error) {
	var out Verification
	created := false
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if key != "" {
			var id string
			err := tx.QueryRowContext(ctx, `SELECT id FROM verifications WHERE create_key = ?`, key).Scan(&id)
			if err == nil {
				got, _, err := oneVerification(ctx, tx, id)
				out = got
				return 0, err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
		}
		var total int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM verifications`).Scan(&total); err != nil {
			return 0, err
		}
		if total >= limit {
			return 0, ErrVerificationFull
		}
		if err := insertVerification(ctx, tx, v, key); err != nil {
			return 0, err
		}
		got, _, err := oneVerification(ctx, tx, v.ID)
		out, created = got, true
		return 1, err
	})
	return out, created, err
}

func insertVerification(ctx context.Context, tx *sql.Tx, v Verification, key string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO verifications (id, title, why, started_at, due_at,
		source_kind, source_since, schedule_id, status, seed, create_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, ?, ?)`,
		v.ID, v.Title, v.Why, v.StartedAt, v.DueAt, v.SourceKind, v.SourceSince, v.ScheduleID,
		v.Seed, key, v.CreatedAt, v.CreatedAt); err != nil {
		return err
	}
	for i, c := range v.Criteria {
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_criteria
			(verification_id, position, text, state, updated_at) VALUES (?, ?, ?, 'unset', 0)`,
			v.ID, i, c.Text); err != nil {
			return err
		}
	}
	return nil
}

// AppendVerificationNote adds one note. A key already used on this record
// answers the note it made. schedule, when not empty, is the schedule the
// writer runs for, and the record must be linked to it: that is the whole of
// what a scheduled task's secret may write (app.AppendScheduledNote).
func (s *Store) AppendVerificationNote(ctx context.Context, id string, n VerificationNote, key, schedule string, limit int) (VerificationNote, error) {
	var out VerificationNote
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var linked string
		err := tx.QueryRowContext(ctx, `SELECT schedule_id FROM verifications WHERE id = ?`, id).Scan(&linked)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrVerificationNotFound
		}
		if err != nil {
			return 0, err
		}
		if schedule != "" && linked != schedule {
			return 0, ErrVerificationSchedule
		}
		if key != "" {
			err := tx.QueryRowContext(ctx, `SELECT seq, at, author_kind, author, text FROM verification_notes
				WHERE verification_id = ? AND note_key = ?`, id, key).
				Scan(&out.ID, &out.At, &out.AuthorKind, &out.Author, &out.Text)
			if err == nil {
				return 0, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM verification_notes WHERE verification_id = ?`,
			id).Scan(&count); err != nil {
			return 0, err
		}
		if count >= limit {
			return 0, ErrVerificationNotesFull
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO verification_notes
			(verification_id, at, author_kind, author, text, note_key) VALUES (?, ?, ?, ?, ?, ?)`,
			id, n.At, n.AuthorKind, n.Author, n.Text, key)
		if err != nil {
			return 0, err
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE verifications SET updated_at = ? WHERE id = ?`, n.At, id); err != nil {
			return 0, err
		}
		out = n
		out.ID = seq
		return 1, nil
	})
	return out, err
}

// SetVerificationCriterion marks one criterion. A closed record's criteria
// are what it was closed on and do not change.
func (s *Store) SetVerificationCriterion(ctx context.Context, id string, index int, state string, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		status, err := verificationStatus(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if status != "open" {
			return 0, ErrVerificationClosed
		}
		res, err := tx.ExecContext(ctx, `UPDATE verification_criteria SET state = ?, updated_at = ?
			WHERE verification_id = ? AND position = ?`, state, now.Unix(), id, index)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			if err == nil {
				err = ErrVerificationCriterion
			}
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE verifications SET updated_at = ? WHERE id = ?`, now.Unix(), id)
		return 1, err
	})
}

// CloseVerification settles a record as accepted or rejected, with the
// reason. Closing it again the same way is answered as done; closing it the
// other way is ErrVerificationClosed, because a verdict is not overwritten.
func (s *Store) CloseVerification(ctx context.Context, id, status, reason string, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		current, err := verificationStatus(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if current == status {
			return 0, nil
		}
		if current != "open" {
			return 0, ErrVerificationClosed
		}
		_, err = tx.ExecContext(ctx, `UPDATE verifications SET status = ?, close_reason = ?, closed_at = ?,
			updated_at = ? WHERE id = ?`, status, reason, now.Unix(), now.Unix(), id)
		return 1, err
	})
}

// DeleteVerification removes one record, its criteria and its notes. An open
// record is refused unless force says the person meant it.
func (s *Store) DeleteVerification(ctx context.Context, id string, force bool) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		status, err := verificationStatus(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if status == "open" && !force {
			return 0, ErrVerificationOpen
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM verifications WHERE id = ?`, id)
		return 1, err
	})
}

func verificationStatus(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM verifications WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVerificationNotFound
	}
	return status, err
}

// SeedVerification plants v once, under v.Seed. It answers true only when it
// wrote the record. A seed already recorded — planted before, whether or not
// the record is still there — writes nothing, which is what keeps a deleted
// seed deleted. A record the person already made for the same data source and
// schedule is taken as the seed: the seed is recorded and nothing is added.
func (s *Store) SeedVerification(ctx context.Context, v Verification, limit int) (bool, error) {
	planted := false
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var at int64
		err := tx.QueryRowContext(ctx, `SELECT seeded_at FROM verification_seeds WHERE seed = ?`, v.Seed).Scan(&at)
		if err == nil {
			return 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_seeds (seed, seeded_at) VALUES (?, ?)`,
			v.Seed, v.CreatedAt); err != nil {
			return 0, err
		}
		var same, total int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM verifications
			WHERE source_kind = ? AND schedule_id = ?`, v.SourceKind, v.ScheduleID).Scan(&same); err != nil {
			return 0, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM verifications`).Scan(&total); err != nil {
			return 0, err
		}
		if same > 0 || total >= limit {
			return 1, nil
		}
		if err := insertVerification(ctx, tx, v, ""); err != nil {
			return 0, err
		}
		planted = true
		return 1, nil
	})
	return planted, err
}
