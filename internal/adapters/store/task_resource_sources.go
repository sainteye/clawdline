package store

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// TaskResourceSourcePage reads source-owned legacy boundaries without adopting
// their paths into the new resource register. Paging never truncates totals.
func (s *Store) TaskResourceSourcePage(ctx context.Context, after string) ([]BrokerRow, string, error) {
	if err := reading(); err != nil {
		return nil, "", err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+brokerColumns+` FROM broker_tasks WHERE id>? ORDER BY id LIMIT ?`, after, taskresource.PageRowsLimit)
	if err != nil {
		return nil, "", classify(err)
	}
	defer rows.Close()
	out := []BrokerRow{}
	for rows.Next() {
		r, err := scanBroker(rows)
		if err != nil {
			return nil, "", classify(err)
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
