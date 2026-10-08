package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GoCache is one exact build-cache directory created by Clawdline. A row is
// retained after removal so a restarted sweep can explain its last outcome.
type GoCache struct {
	Path        string
	OwnerKind   string
	OwnerID     string
	Purpose     string
	Rebuildable bool
	CreatedAt   time.Time
	LastOutcome string
	LastReason  string
	LastAt      time.Time
}

// RegisterGoCache is immutable and idempotent. Validation of broker-owned
// scratch happens at the app boundary before this write.
func (s *Store) RegisterGoCache(ctx context.Context, c GoCache) error {
	if c.Path == "" || c.OwnerKind == "" || c.OwnerID == "" || c.Purpose == "" || c.CreatedAt.IsZero() {
		return errors.New("incomplete Go cache registration")
	}
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var old GoCache
		var rebuild int
		err := tx.QueryRowContext(ctx, `SELECT owner_kind, owner_id, purpose, rebuildable
			FROM broker_go_caches WHERE path = ?`, c.Path).
			Scan(&old.OwnerKind, &old.OwnerID, &old.Purpose, &rebuild)
		if err == nil {
			if old.OwnerKind != c.OwnerKind || old.OwnerID != c.OwnerID || old.Purpose != c.Purpose ||
				(rebuild != 0) != c.Rebuildable {
				return 0, fmt.Errorf("Go cache path is already registered to a different owner or purpose")
			}
			return 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO broker_go_caches
			(path, owner_kind, owner_id, purpose, rebuildable, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Path, c.OwnerKind, c.OwnerID, c.Purpose, c.Rebuildable, c.CreatedAt.Unix())
		return 1, err
	})
}

// GoCachesByOwner reads the durable cache registrations for one owner.
func (s *Store) GoCachesByOwner(ctx context.Context, kind, id string) ([]GoCache, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT path, owner_kind, owner_id, purpose, rebuildable,
		created_at, last_outcome, last_reason, last_at FROM broker_go_caches
		WHERE owner_kind = ? AND owner_id = ? ORDER BY path`, kind, id)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []GoCache{}
	for rows.Next() {
		var c GoCache
		var rebuild int
		var created, last int64
		if err := rows.Scan(&c.Path, &c.OwnerKind, &c.OwnerID, &c.Purpose, &rebuild,
			&created, &c.LastOutcome, &c.LastReason, &last); err != nil {
			return nil, classify(err)
		}
		c.Rebuildable = rebuild != 0
		c.CreatedAt = time.Unix(created, 0)
		if last != 0 {
			c.LastAt = time.Unix(last, 0)
		}
		out = append(out, c)
	}
	return out, classify(rows.Err())
}

// MarkGoCacheCleanup records the last actual sweep decision, including an
// intent that can be recovered after a crash. A dry run never calls it.
func (s *Store) MarkGoCacheCleanup(ctx context.Context, path, kind, id, outcome, reason string, at time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx, `UPDATE broker_go_caches SET last_outcome = ?, last_reason = ?, last_at = ?
			WHERE path = ? AND owner_kind = ? AND owner_id = ?`, outcome, reason, at.Unix(), path, kind, id)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n != 1 {
			return 0, errors.New("Go cache registration is missing")
		}
		return n, nil
	})
}
