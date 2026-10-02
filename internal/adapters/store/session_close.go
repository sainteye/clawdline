package store

import "context"

// SessionResponsibility is an unfinished Board item or to-do owned by one
// conversation. The close projection needs only identities, not private text.
type SessionResponsibility struct {
	Session string
	Code    string
	ID      string
}

// unstartedBoardItem is the one definition of a Board item nobody has started
// (docs/work-system.md, "Closing a Session that owns Board items"): it is still
// in `assigned`, never moved on to implementing or later, and none of its steps
// is marked done. Only such an item may be released by a close; every other
// unfinished item keeps blocking it.
const unstartedBoardItem = `phase='assigned' AND NOT EXISTS
	(SELECT 1 FROM work_v2_steps st WHERE st.work_id=work_v2_items.id AND st.done=1)`

// deployingAwaitingPerson is the one definition of a Board item whose owning
// Session has already asked the person to deploy it (work.ItemV2.AwaitsPersonToDeploy):
// in deploying, waiting_user, and linked to a decision that is still open. It
// does not block a close; every close releases it and keeps the decision.
const deployingAwaitingPerson = `phase='deploying' AND condition='waiting_user' AND decision_id<>''
	AND EXISTS (SELECT 1 FROM decisions d WHERE d.id=work_v2_items.decision_id AND d.state='open')`

// DeployingAwaitingPersonItems is every Board item the conversation owns that
// waits on the person to deploy it, in id order, with the version its release
// is made against. A failed query is an error, never an empty list.
func (s *Store) DeployingAwaitingPersonItems(ctx context.Context, conversation string) ([]UnstartedBoardItem, error) {
	return s.ownedBoardItems(ctx, conversation, deployingAwaitingPerson)
}

// UnstartedBoardItem is one item a close may release: its id and the version
// the unassign is made against.
type UnstartedBoardItem struct {
	ID      string
	Version int64
}

// UnstartedBoardItems is every unstarted Board item the conversation owns, in
// id order. A failed query is an error, never an empty list.
func (s *Store) UnstartedBoardItems(ctx context.Context, conversation string) ([]UnstartedBoardItem, error) {
	return s.ownedBoardItems(ctx, conversation, unstartedBoardItem)
}

func (s *Store) ownedBoardItems(ctx context.Context, conversation, where string) ([]UnstartedBoardItem, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	if conversation == "" {
		return nil, nil
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT id,version FROM work_v2_items
		WHERE owner_session=? AND `+where+` ORDER BY id`, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UnstartedBoardItem{}
	for rows.Next() {
		var item UnstartedBoardItem
		if err := rows.Scan(&item.ID, &item.Version); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
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
		{"board_item_unstarted", `SELECT owner_session,id FROM work_v2_items
			WHERE owner_session<>'' AND ` + unstartedBoardItem},
		{"board_item_open", `SELECT owner_session,id FROM work_v2_items
			WHERE owner_session<>'' AND phase NOT IN ('done','cancelled') AND NOT (` + unstartedBoardItem + `)
			AND NOT (` + deployingAwaitingPerson + `)`},
		{"session_todo_open", `SELECT session_id,id FROM session_direct_todos
			WHERE completed_at IS NULL`},
		{"dispatch_todo_open", `SELECT owner_session,id FROM todos
			WHERE state IN ('open','handed_off')`},
	}
	out := []SessionResponsibility{}
	for _, q := range queries {
		rows, err := s.rd.QueryContext(ctx, q.sql)
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
