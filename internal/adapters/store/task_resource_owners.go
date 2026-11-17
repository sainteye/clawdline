package store

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// TaskResourcesByOwnerPage is a bounded read of exact immutable ownership.
// It is used by producer publication and to retain command-liveness metadata
// until all resources that depend on it have been reclaimed.
func (s *Store) TaskResourcesByOwnerPage(ctx context.Context, kind, id, after string) ([]taskresource.Resource, string, error) {
	if err := reading(); err != nil {
		return nil, "", err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT record FROM broker_task_resources
	 WHERE owner_kind=? AND owner_id=? AND id>? AND state!='removed' ORDER BY id LIMIT ?`, kind, id, after, taskresource.PageRowsLimit)
	if err != nil {
		return nil, "", classify(err)
	}
	defer rows.Close()
	out := []taskresource.Resource{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, "", classify(err)
		}
		r, err := decodeTaskResource(raw)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", classify(err)
	}
	next := ""
	if len(out) == taskresource.PageRowsLimit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}
