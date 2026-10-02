package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// A root landing is the broker's record of a landing no child task carries:
// the owning Session proved one commit on a target branch and on its
// remote-tracking copy (`item phase deploying --commit --target --remote`),
// and this row is that proof. Before it, the proof lived only as a copy inside
// the item's `item.phase_changed` payload, a second truth beside the broker's
// task landings (docs/design-decisions.md D01). The item's event now carries
// only this row's id.
//
// One row per (item, repository, commit, target): the id is derived from those
// four, so the same landing recorded twice — a retry under a new
// Idempotency-Key after a version reread, or Finish after `item phase` — is the
// same row, and the first proof stands.
//
// Expand only: an older daemon never reads this table, and the events it
// wrote before it keep their `landing` copy, which the item read still shows.
const rootLandingSchema = `
CREATE TABLE IF NOT EXISTS broker_root_landings (
  id            TEXT    PRIMARY KEY,
  work_id       TEXT    NOT NULL,
  session_id    TEXT    NOT NULL,
  repository    TEXT    NOT NULL,
  target        TEXT    NOT NULL,
  commit_sha    TEXT    NOT NULL,
  target_commit TEXT    NOT NULL,
  remote        TEXT    NOT NULL,
  remote_commit TEXT    NOT NULL,
  recorded_at   INTEGER NOT NULL,
  UNIQUE (work_id, repository, commit_sha, target)
);
CREATE INDEX IF NOT EXISTS broker_root_landings_work ON broker_root_landings(work_id, recorded_at, id);
`

// WorkV2RootLandingLimit is how many root landings one item keeps, and how
// many legacy landing copies its read returns. An item re-enters deploying
// once a cycle, so this is many reopenings; at the limit a new landing is
// refused and nothing is evicted.
const WorkV2RootLandingLimit = 64

// ErrRootLandingsFull is an item that already holds WorkV2RootLandingLimit
// root landings.
var ErrRootLandingsFull = errors.New("root landings full")

// RootLanding is one broker_root_landings row.
type RootLanding struct {
	ID           string
	WorkID       string
	SessionID    string
	Repository   string
	Target       string
	Commit       string
	TargetCommit string
	Remote       string
	RemoteCommit string
	RecordedAt   time.Time
}

// RootLandingID is the id a landing of commit on target in repository, for
// item workID, is stored under: the same four always answer the same id.
func RootLandingID(workID, repository, commit, target string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{workID, repository, commit, target}, "\x00")))
	h := hex.EncodeToString(sum[:16])
	return "rl-" + h
}

func openRootLandings(db *sql.DB) error {
	_, err := db.Exec(rootLandingSchema)
	return err
}

// RecordRootLanding writes l in the item's transaction and answers the row as
// stored: the existing one when this landing was recorded before, whatever
// later target or remote tips l carries.
func (t *WorkV2Tx) RecordRootLanding(l RootLanding) (RootLanding, error) {
	l.ID = RootLandingID(l.WorkID, l.Repository, l.Commit, l.Target)
	existing, err := scanRootLanding(t.tx.QueryRowContext(t.ctx,
		`SELECT `+rootLandingColumns+` FROM broker_root_landings WHERE id = ?`, l.ID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RootLanding{}, err
	}
	var n int
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM broker_root_landings WHERE work_id = ?`,
		l.WorkID).Scan(&n); err != nil {
		return RootLanding{}, err
	}
	if n >= WorkV2RootLandingLimit {
		return RootLanding{}, ErrRootLandingsFull
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO broker_root_landings
	  (id, work_id, session_id, repository, target, commit_sha, target_commit, remote, remote_commit, recorded_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?)`, l.ID, l.WorkID, l.SessionID, l.Repository, l.Target, l.Commit,
		l.TargetCommit, l.Remote, l.RemoteCommit, l.RecordedAt.Unix()); err != nil {
		return RootLanding{}, err
	}
	t.wrote++
	l.RecordedAt = time.Unix(l.RecordedAt.Unix(), 0)
	return l, nil
}

const rootLandingColumns = `id, work_id, session_id, repository, target, commit_sha, target_commit, remote,
  remote_commit, recorded_at`

func scanRootLanding(sc scanner) (RootLanding, error) {
	var l RootLanding
	var at int64
	if err := sc.Scan(&l.ID, &l.WorkID, &l.SessionID, &l.Repository, &l.Target, &l.Commit, &l.TargetCommit,
		&l.Remote, &l.RemoteCommit, &at); err != nil {
		return RootLanding{}, err
	}
	l.RecordedAt = time.Unix(at, 0)
	return l, nil
}

func queryRootLandings(ctx context.Context, q querier, workID string) ([]RootLanding, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+rootLandingColumns+` FROM broker_root_landings
	  WHERE work_id = ? ORDER BY recorded_at, id LIMIT ?`, workID, WorkV2RootLandingLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RootLanding{}
	for rows.Next() {
		l, err := scanRootLanding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// RootLandings is every root landing recorded for one item, oldest first.
func (s *Store) RootLandings(ctx context.Context, workID string) ([]RootLanding, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return queryRootLandings(ctx, s.rd, workID)
}

// RootLandings is RootLandings inside the item's transaction.
func (t *WorkV2Tx) RootLandings(workID string) ([]RootLanding, error) {
	return queryRootLandings(t.ctx, t.tx, workID)
}

// LegacyLanding is a landing copy an `item.phase_changed` event carried
// before root landings were rows of their own. It is read, never rewritten.
type LegacyLanding struct {
	Seq     int64
	At      time.Time
	Payload string
}

func queryLegacyLandings(ctx context.Context, q querier, workID string) ([]LegacyLanding, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq, at, payload FROM work_v2_events
	  WHERE work_id = ? AND kind = 'item.phase_changed' AND json_valid(payload)
	    AND json_type(payload, '$.landing') = 'object' AND json_type(payload, '$.landing_id') IS NULL
	  ORDER BY seq LIMIT ?`, workID, WorkV2RootLandingLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegacyLanding{}
	for rows.Next() {
		var l LegacyLanding
		var at int64
		if err := rows.Scan(&l.Seq, &at, &l.Payload); err != nil {
			return nil, err
		}
		l.At = time.Unix(at, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// LegacyLandings is every landing copy an older daemon wrote into the item's
// phase events, oldest first.
func (s *Store) LegacyLandings(ctx context.Context, workID string) ([]LegacyLanding, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return queryLegacyLandings(ctx, s.rd, workID)
}

// WorkV2Tasks is WorkV2Tx.Tasks outside a write: the broker tasks bound to
// one item, for a read.
func (s *Store) WorkV2Tasks(ctx context.Context, workID string) ([]BrokerRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return queryWorkV2Tasks(ctx, s.rd, workID)
}
