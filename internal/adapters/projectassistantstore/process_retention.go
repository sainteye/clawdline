package projectassistantstore

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// retainedTraceSQL lists pa.RetainedTraceCategories as an SQL value list.
var retainedTraceSQL = "'" + strings.Join(pa.RetainedTraceCategories, "','") + "'"

// dropTurnProcess deletes what only a running turn needs, in the commit that
// stops it being the Bot's active turn: its process traces (streamed chunks,
// thoughts, tool steps) and the receipts of the events it published. Nothing
// can use either afterwards: incoming events for a turn that is not active
// are refused before their receipt is consulted, and the chat shows a past
// turn's messages, not its process. The machine's newest event is never
// deleted, so the event frontier a reader holds cannot move backwards.
func dropTurnProcess(ctx context.Context, tx *sql.Tx, key pa.BotKey, turn string) error {
	if turn == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_events WHERE machine=? AND bot=?
 AND json_extract(CAST(data AS TEXT),'$.kind')='trace_updated'
 AND json_extract(CAST(data AS TEXT),'$.trace.turn_id')=?
 AND coalesce(json_extract(CAST(data AS TEXT),'$.trace.category'),'') NOT IN (`+retainedTraceSQL+`)
 AND seq<(SELECT max(seq) FROM project_assistant_events WHERE machine=?)`,
		key.MachineID, key.BotID, turn, key.MachineID); err != nil {
		return err
	}
	prefix := pa.RuntimeEventCommandPrefix + turn + "-"
	_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_receipts WHERE machine=? AND bot=? AND operation=?
 AND substr(command_key,1,?)=?`, key.MachineID, key.BotID, string(pa.SendOperation), utf8.RuneCountInString(prefix), prefix)
	return err
}

// replaceRosterChange removes a Bot's earlier roster journal rows before its
// new one is appended. Every reader takes either the newest row per Bot or the
// rows after a frontier, and reads the Bot's current summary for each, so the
// newest row carries everything an older one could: a frontier before the
// removed rows still sees the Bot through its newest row.
func replaceRosterChange(ctx context.Context, tx *sql.Tx, key pa.BotKey) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM project_assistant_roster_changes WHERE machine=? AND bot=?`, key.MachineID, key.BotID)
	return err
}
