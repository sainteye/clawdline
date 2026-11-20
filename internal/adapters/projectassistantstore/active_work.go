package projectassistantstore

import (
	"context"
	"database/sql"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A work link counts as active until it fails, completes, lands or becomes
// unknown; a background task until it reaches a terminal turn state. Keep the
// SQL fragments below in step with these.
const (
	activeWorkLinkStatesSQL   = `('accepted','waiting_for_person','executing')`
	activeBackgroundStatesSQL = `('waiting','active')`
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

// activeBackground reads every Bot's unfinished background tasks on one
// machine, or on all machines for an empty machine, in one query per page.
func activeBackground(ctx context.Context, r interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, machine string) (map[pa.BotKey]int64, error) {
	rows, err := r.QueryContext(ctx, `SELECT machine,bot,count(*) FROM project_assistant_background_tasks
		WHERE (?='' OR machine=?) AND state IN `+activeBackgroundStatesSQL+` GROUP BY machine,bot`, machine, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[pa.BotKey]int64{}
	for rows.Next() {
		var key pa.BotKey
		var n int64
		if err = rows.Scan(&key.MachineID, &key.BotID, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

// noteBackgroundOwner records a roster change for the Bot that owns a
// background task whose state changed. The task lives in its own table and the
// worker is a hidden aggregate, so neither write would otherwise tell a live
// roster that the owner's active work changed.
func noteBackgroundOwner(ctx context.Context, tx *sql.Tx, owner pa.BotKey) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed)
		SELECT machine,bot,json_extract(CAST(data AS TEXT),'$.Bot.project'),json_extract(CAST(data AS TEXT),'$.Conversation.id'),
			json_extract(CAST(data AS TEXT),'$.Conversation.generation'),json_extract(CAST(data AS TEXT),'$.Conversation.binding_id'),0
		FROM project_assistant_bots WHERE machine=? AND bot=? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL`,
		owner.MachineID, owner.BotID)
	return err
}
