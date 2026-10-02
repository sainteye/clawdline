package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

const (
	HumanInterventionsOpenLimit   = 8
	HumanInterventionsTotalLimit  = 2000
	HumanInterventionsRecentLimit = 5
)

var (
	ErrHumanInterventionsFull    = errors.New("human interventions full")
	ErrHumanInterventionNotFound = errors.New("human intervention not found")
)

const humanInterventionSchema = `
CREATE TABLE IF NOT EXISTS human_interventions (
 id TEXT PRIMARY KEY,
 source_conversation TEXT NOT NULL,
 source_label TEXT NOT NULL,
 target_conversation TEXT NOT NULL,
 target_session TEXT NOT NULL,
 kind TEXT NOT NULL,
 title TEXT NOT NULL,
 summary TEXT NOT NULL,
 action TEXT NOT NULL,
 reason TEXT NOT NULL,
 detail TEXT NOT NULL,
 options TEXT NOT NULL,
 document_url TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 read_at INTEGER,
 resolved_at INTEGER,
 resolution TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS human_interventions_target ON human_interventions(target_conversation, resolved_at, created_at DESC);
CREATE TABLE IF NOT EXISTS human_interventions_retention (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 pruned_resolved INTEGER NOT NULL DEFAULT 0,
 last_pruned_at INTEGER
);
INSERT OR IGNORE INTO human_interventions_retention(singleton) VALUES (1);
`

func openHumanInterventions(db *sql.DB) error {
	_, err := db.Exec(humanInterventionSchema)
	return err
}

const humanInterventionColumns = `id,source_conversation,source_label,target_conversation,target_session,kind,title,summary,action,reason,detail,options,document_url,created_at,read_at,resolved_at,resolution,version`

func scanHumanIntervention(row interface{ Scan(...any) error }) (work.HumanIntervention, error) {
	var out work.HumanIntervention
	var options string
	var created int64
	var read, resolved sql.NullInt64
	err := row.Scan(&out.ID, &out.SourceConversation, &out.SourceLabel, &out.TargetConversation, &out.TargetSession,
		&out.Kind, &out.Title, &out.Summary, &out.Action, &out.Reason, &out.Detail, &options, &out.DocumentURL,
		&created, &read, &resolved, &out.Resolution, &out.Version)
	if err != nil {
		return out, err
	}
	out.CreatedAt = time.Unix(created, 0).UTC()
	if read.Valid {
		at := time.Unix(read.Int64, 0).UTC()
		out.ReadAt = &at
	}
	if resolved.Valid {
		at := time.Unix(resolved.Int64, 0).UTC()
		out.ResolvedAt = &at
	}
	if err := json.Unmarshal([]byte(options), &out.Options); err != nil {
		return out, err
	}
	return out, nil
}

func (t *WorkV2Tx) AddHumanIntervention(v work.HumanIntervention, at time.Time) error {
	var open, total int
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM human_interventions WHERE target_conversation=? AND resolved_at IS NULL`, v.TargetConversation).Scan(&open); err != nil {
		return err
	}
	if open >= HumanInterventionsOpenLimit {
		return ErrHumanInterventionsFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM human_interventions`).Scan(&total); err != nil {
		return err
	}
	if total >= HumanInterventionsTotalLimit {
		res, err := t.tx.ExecContext(t.ctx, `DELETE FROM human_interventions WHERE id=(SELECT id FROM human_interventions WHERE resolved_at IS NOT NULL ORDER BY resolved_at,id LIMIT 1)`)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrHumanInterventionsFull
		}
		if _, err = t.tx.ExecContext(t.ctx, `UPDATE human_interventions_retention SET pruned_resolved=pruned_resolved+1,last_pruned_at=? WHERE singleton=1`, at.Unix()); err != nil {
			return err
		}
	}
	options, err := json.Marshal(v.Options)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO human_interventions (`+humanInterventionColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.SourceConversation, v.SourceLabel, v.TargetConversation, v.TargetSession, v.Kind, v.Title, v.Summary, v.Action, v.Reason,
		v.Detail, string(options), v.DocumentURL, v.CreatedAt.Unix(), nil, nil, "", 1)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) HumanIntervention(id string) (work.HumanIntervention, error) {
	v, err := scanHumanIntervention(t.tx.QueryRowContext(t.ctx, `SELECT `+humanInterventionColumns+` FROM human_interventions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrHumanInterventionNotFound
	}
	return v, err
}

func (t *WorkV2Tx) PutHumanIntervention(prev, next work.HumanIntervention) error {
	if prev.ResolvedAt != nil && next.ResolvedAt == nil {
		var open int
		if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM human_interventions WHERE target_conversation=? AND resolved_at IS NULL`, prev.TargetConversation).Scan(&open); err != nil {
			return err
		}
		if open >= HumanInterventionsOpenLimit {
			return ErrHumanInterventionsFull
		}
	}
	var read, resolved any
	if next.ReadAt != nil {
		read = next.ReadAt.Unix()
	}
	if next.ResolvedAt != nil {
		resolved = next.ResolvedAt.Unix()
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE human_interventions SET read_at=?,resolved_at=?,resolution=?,version=version+1 WHERE id=? AND version=?`, read, resolved, next.Resolution, prev.ID, prev.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (s *Store) HumanInterventions(ctx context.Context, conversation string) ([]work.HumanIntervention, int64, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT `+humanInterventionColumns+` FROM human_interventions WHERE target_conversation=? AND resolved_at IS NULL ORDER BY created_at DESC,id LIMIT ?`, conversation, HumanInterventionsOpenLimit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]work.HumanIntervention, 0)
	for rows.Next() {
		v, e := scanHumanIntervention(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	rows, err = s.rd.QueryContext(ctx, `SELECT `+humanInterventionColumns+` FROM human_interventions WHERE target_conversation=? AND resolved_at IS NOT NULL ORDER BY resolved_at DESC,id LIMIT ?`, conversation, HumanInterventionsRecentLimit)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		v, e := scanHumanIntervention(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	var pruned int64
	err = s.rd.QueryRowContext(ctx, `SELECT pruned_resolved FROM human_interventions_retention WHERE singleton=1`).Scan(&pruned)
	return out, pruned, err
}

func (s *Store) HumanInterventionCapacityCounts(ctx context.Context) (int64, int64, error) {
	var open, total int64
	if err := s.rd.QueryRowContext(ctx, `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM human_interventions WHERE resolved_at IS NULL GROUP BY target_conversation)`).Scan(&open); err != nil {
		return 0, 0, err
	}
	if err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_interventions`).Scan(&total); err != nil {
		return 0, 0, err
	}
	return open, total, nil
}

// OpenHumanInterventionCounts reads the list's attention summary in one query.
// A missing key means zero only when this query succeeded; callers must not
// turn a failed read into an empty map.
func (s *Store) OpenHumanInterventionCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT target_conversation,COUNT(*) FROM human_interventions WHERE resolved_at IS NULL GROUP BY target_conversation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var conversation string
		var count int64
		if err := rows.Scan(&conversation, &count); err != nil {
			return nil, err
		}
		counts[conversation] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}
