package store

import "context"

// SessionResponsibility is an unfinished Board item or to-do owned by one
// conversation. The close projection needs only identities, not private text.
type SessionResponsibility struct {
	Session string
	Code    string
	ID      string
}

// OpenSessionResponsibilities reads every unfinished assignment once for a
// session inventory. A failed query is not an empty list: callers fail closed.
func (s *Store) OpenSessionResponsibilities(ctx context.Context) ([]SessionResponsibility, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	queries := []struct {
		code, sql string
	}{
		{"board_item_open", `SELECT owner_session,id FROM work_v2_items
			WHERE owner_session<>'' AND phase NOT IN ('done','cancelled')`},
		{"session_todo_open", `SELECT session_id,id FROM session_direct_todos
			WHERE completed_at IS NULL`},
		{"dispatch_todo_open", `SELECT owner_session,id FROM todos
			WHERE state IN ('open','handed_off')`},
	}
	out := []SessionResponsibility{}
	for _, q := range queries {
		rows, err := s.db.QueryContext(ctx, q.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SessionResponsibility
			if err := rows.Scan(&r.Session, &r.ID); err != nil {
				rows.Close()
				return nil, err
			}
			r.Code = q.code
			out = append(out, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
