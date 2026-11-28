package projectassistantstore

import (
	"context"
	"database/sql"
	"iter"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A work link counts as active until it fails, completes, lands or becomes
// unknown; a background task until it reaches a terminal turn state. Keep the
// SQL fragments below in step with these.
const (
	activeWorkLinkStatesSQL     = `('accepted','waiting_for_person','executing')`
	activeBackgroundStatesSQL   = `('waiting','active')`
	dispatchedWorkLinkStatesSQL = `('accepted','waiting_for_person','executing','completed','failed')`
)

var (
	backgroundWorkerTurnStateSQL = `json_extract(` + factValueSQL(factTurns, "b", `json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id')`) + `,'$.state')`
	backgroundWorkCountsSQL      = `count(*) FILTER (WHERE t.state='active' AND ` + backgroundWorkerTurnStateSQL + ` IS NOT NULL),
		count(*) FILTER (WHERE t.state='waiting' OR (` + backgroundWorkerTurnStateSQL + ` IS NULL AND t.state='active')),
		count(*) FILTER (WHERE ` + backgroundWorkerTurnStateSQL + `='waiting_permission')`
	backgroundWorkFromSQL = ` FROM project_assistant_background_tasks t
		LEFT JOIN project_assistant_bots b ON t.state='active' AND t.worker=b.bot AND t.machine=b.machine
		WHERE t.state IN ` + activeBackgroundStatesSQL
)

func activeWorkLink(state pa.WorkState) bool {
	return state == pa.WorkAccepted || state == pa.WorkWaitingForPerson || state == pa.WorkExecuting
}

func activeWorkLinks(a aggregate) (n int64) {
	for _, link := range a.WorkLinks {
		if activeWorkLink(link.State) {
			n++
		}
	}
	return n
}

// countDispatchedWork counts the broker Sessions a Bot dispatched. Landed and
// unknown links are not counted; a finished link counts as delivered while
// its landing is owed (pa.WorkLink.Unsettled), with no time limit, and stops
// counting the moment the landing settles. The result is nil when nothing
// counts, so the field is absent.
func countDispatchedWork(links iter.Seq[pa.WorkLink]) *pa.DispatchedWork {
	var out pa.DispatchedWork
	for link := range links {
		switch link.State {
		case pa.WorkAccepted:
			out.Running++
		case pa.WorkExecuting:
			out.Running++
			out.Executing++
		case pa.WorkWaitingForPerson:
			out.NeedsYou++
		default:
			if link.Unsettled() {
				out.Delivered++
			}
		}
	}
	if out == (pa.DispatchedWork{}) {
		return nil
	}
	return &out
}

// activeBackground reads every Bot's unfinished background tasks on one
// machine, or on all machines for an empty machine, in one query per page.
func activeBackground(ctx context.Context, r interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, machine string) (map[pa.BotKey]pa.BackgroundWork, error) {
	rows, err := r.QueryContext(ctx, `SELECT t.machine,t.bot,`+backgroundWorkCountsSQL+backgroundWorkFromSQL+
		` AND (?='' OR t.machine=?) GROUP BY t.machine,t.bot`, machine, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[pa.BotKey]pa.BackgroundWork{}
	for rows.Next() {
		var key pa.BotKey
		var work pa.BackgroundWork
		if err = rows.Scan(&key.MachineID, &key.BotID, &work.Working, &work.Waiting, &work.NeedsInput); err != nil {
			return nil, err
		}
		out[key] = work
	}
	return out, rows.Err()
}

// The SQL working count includes occupied workers waiting for approval.
func addRosterBackground(summary *pa.BotSummary, work pa.BackgroundWork) {
	total := work.Working + work.Waiting
	if total == 0 {
		return
	}
	summary.ActiveWork += total
	work.Working -= work.NeedsInput
	summary.BackgroundWork = &work
}

// noteBackgroundOwner records a roster change for the Bot that owns a
// background task whose state changed. The task lives in its own table and the
// worker is a hidden aggregate, so neither write would otherwise tell a live
// roster that the owner's active work changed.
func noteBackgroundOwner(ctx context.Context, tx *sql.Tx, owner pa.BotKey) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND bot=? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL`, owner.MachineID, owner.BotID).Scan(&exists); err != nil || exists == 0 {
		return err
	}
	if err := replaceRosterChange(ctx, tx, owner); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed)
		SELECT machine,bot,json_extract(CAST(data AS TEXT),'$.Bot.project'),json_extract(CAST(data AS TEXT),'$.Conversation.id'),
			json_extract(CAST(data AS TEXT),'$.Conversation.generation'),json_extract(CAST(data AS TEXT),'$.Conversation.binding_id'),0
		FROM project_assistant_bots WHERE machine=? AND bot=? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL`,
		owner.MachineID, owner.BotID)
	return err
}

// SettledWorkRepairs finds existing projected links whose owner already
// settled the broker obligation. Older failed projections carry no retry row.
func (s *Store) SettledWorkRepairs(ctx context.Context, machine, after string) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, `
SELECT t.id
FROM project_assistant_bots b `+factJoinSQL(factWorkLinks, "w", "b")+`
JOIN broker_tasks t ON t.id=json_extract(w.value,'$.task_id')
WHERE b.machine=? AND t.id>? AND json_valid(t.record)
AND json_extract(t.record,'$.assistant_owner.fence.key.machine_id')=b.machine
AND json_extract(t.record,'$.assistant_owner.fence.key.bot_id')=b.bot
AND json_extract(t.record,'$.landing.state') IN ('landed','incorporated','abandoned','nothing_to_land')
AND json_extract(w.value,'$.state') IN ('completed','failed')
AND coalesce(json_extract(w.value,'$.landing'),'') NOT IN ('landed','incorporated','abandoned','nothing_to_land')
ORDER BY t.id LIMIT ?`, machine, after, pa.MaxPageRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClosedRootWorkRepairs finds active WorkLinks whose Bot-opened Root Assignment
// has already closed or failed, so startup reconciliation settles links that
// missed a live closure effect.
func (s *Store) ClosedRootWorkRepairs(ctx context.Context, machine, after string) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, `
SELECT r.id
FROM project_assistant_bots b `+factJoinSQL(factWorkLinks, "w", "b")+`
JOIN broker_root_assignments r ON r.id=json_extract(w.value,'$.task_id')
WHERE b.machine=? AND r.id>? AND json_valid(r.record)
AND json_extract(r.record,'$.bot_owner.fence.key.machine_id')=b.machine
AND json_extract(r.record,'$.bot_owner.fence.key.bot_id')=b.bot
AND json_extract(w.value,'$.state') IN `+activeWorkLinkStatesSQL+`
AND (
	json_extract(r.record,'$.closure') IS NOT NULL
	OR json_extract(r.record,'$.state')='failed'
)
ORDER BY r.id LIMIT ?`, machine, after, pa.MaxPageRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
