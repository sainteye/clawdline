package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// History fixes metadata, room epoch, lines and actual member activity in one
// read transaction. A member's complete conversation is never loaded into Go.
func (s *GroupStore) History(ctx context.Context, key pa.BotKey, before int64, limit int) (app.GroupHistoryPage, error) {
	var out app.GroupHistoryPage
	if limit < 1 {
		return out, failure(pa.Invalid, "room_lines")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if out.Group, err = groupRead(ctx, tx, key); err != nil {
		return out, err
	}
	if out.Room, err = roomRead(ctx, tx, key); err != nil {
		return out, err
	}
	if out.Lines, err = linesBefore(ctx, tx, key, before, limit); err != nil {
		return out, err
	}
	if out.Activity, err = roomActivity(ctx, tx, out.Group, out.Room); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func roomActivity(ctx context.Context, tx *sql.Tx, group pa.Group, room pa.RoomState) ([]pa.RoomActivity, error) {
	out := []pa.RoomActivity{}
	if group.Deleted {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT
 json_extract(CAST(b.data AS TEXT),'$.Bot.key'),
 json_extract(CAST(b.data AS TEXT),'$.Bot.project'),
 json_extract(CAST(b.data AS TEXT),'$.Conversation'),t.key,
 json_object('id',json_extract(t.value,'$.id'),'state',json_extract(t.value,'$.state'),
 'fence',json_extract(t.value,'$.fence'),'room',json_extract(t.value,'$.room'))
 FROM project_assistant_group_members m
 JOIN project_assistant_bots b ON b.machine=m.machine AND b.bot=m.bot
 JOIN json_each(CAST(b.data AS TEXT),'$.Turns') t
 WHERE m.machine=? AND m.grp=? AND t.type='object'
 AND json_extract(t.value,'$.state') IN ('accepted','queued','running','waiting_permission')
 AND json_extract(t.value,'$.room.group.machine_id')=?
 AND json_extract(t.value,'$.room.group.bot_id')=?
 ORDER BY b.bot,t.key`, group.Key.MachineID, group.Key.BotID, group.Key.MachineID, group.Key.BotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var keyRaw, projectRaw, conversationRaw, turnRaw []byte
		var id string
		if err = rows.Scan(&keyRaw, &projectRaw, &conversationRaw, &id, &turnRaw); err != nil {
			return nil, err
		}
		var key pa.BotKey
		var project pa.ProjectScope
		var conversation pa.Conversation
		var turn pa.Turn
		for _, value := range []struct {
			raw []byte
			out any
		}{{keyRaw, &key}, {projectRaw, &project}, {conversationRaw, &conversation}, {turnRaw, &turn}} {
			if json.Unmarshal(value.raw, value.out) != nil {
				return nil, failure(pa.Invalid, "room_activity")
			}
		}
		if turn.ID != id || turn.Room == nil {
			return nil, failure(pa.Invalid, "room_activity")
		}
		// Stale tickets or another conversation/fence cannot claim a member
		// is working in this room. Queued tickets may precede a native rebind.
		if turn.Room.Member != key || project != group.Project || turn.Fence.Key != key || turn.Fence.Project != project || turn.Fence.ConversationID != conversation.ID || turn.Fence.Generation > conversation.Generation || pa.CheckRoomTicket(group, room, *turn.Room) != nil {
			continue
		}
		if turn.State == pa.TurnRunning || turn.State == pa.TurnWaitingPermission {
			if turn.ID != conversation.ActiveTurnID || turn.Fence.BindingID != conversation.BindingID || turn.Fence.Generation != conversation.Generation {
				continue
			}
		}
		out = append(out, pa.RoomActivity{Member: key, TurnID: turn.ID, State: turn.State})
	}
	return out, rows.Err()
}
