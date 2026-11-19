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
	if out.Activity, out.MemberOutcomes, err = roomTurnObservations(ctx, tx, out.Group, out.Room); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func roomTurnObservations(ctx context.Context, tx *sql.Tx, group pa.Group, room pa.RoomState) ([]pa.RoomActivity, []pa.RoomMemberOutcome, error) {
	activity := []pa.RoomActivity{}
	var outcomes []pa.RoomMemberOutcome
	if group.Deleted {
		return activity, outcomes, nil
	}
	// Reuse the active-ticket scan for terminal reads. Only those in the
	// current round can add an outcome; previously queued late turns may
	// still contribute activity under their existing authorization. The
	// source's submission receipt orders repeat asks even before they start.
	rows, err := tx.QueryContext(ctx, `SELECT
 json_extract(CAST(b.data AS TEXT),'$.Bot.key'),
 json_extract(CAST(b.data AS TEXT),'$.Bot.name'),
 json_extract(CAST(b.data AS TEXT),'$.Bot.project'),
 json_extract(CAST(b.data AS TEXT),'$.Conversation'),t.key,
 json_object('id',json_extract(t.value,'$.id'),'state',json_extract(t.value,'$.state'),
 'fence',json_extract(t.value,'$.fence'),'room',json_extract(t.value,'$.room'),
 'origin',json_extract(t.value,'$.origin'),
 'failure',json_extract(t.value,'$.failure'),'attempts',json_extract(t.value,'$.attempts'))
 FROM project_assistant_group_members m
 JOIN project_assistant_bots b ON b.machine=m.machine AND b.bot=m.bot
 JOIN json_each(CAST(b.data AS TEXT),'$.Turns') t
 LEFT JOIN project_assistant_receipts sent ON sent.machine=b.machine AND sent.bot=b.bot
 AND sent.operation='send' AND sent.command_key=json_extract(t.value,'$.idempotency_key')
 AND json_extract(CAST(sent.data AS TEXT),'$.resource_id')=json_extract(t.value,'$.id')
 WHERE m.machine=? AND m.grp=? AND t.type='object'
 AND json_extract(t.value,'$.room.group.machine_id')=?
 AND json_extract(t.value,'$.room.group.bot_id')=?
 AND (json_extract(t.value,'$.room.epoch')=?
      OR json_extract(t.value,'$.state') IN ('accepted','queued','running','waiting_permission'))
 ORDER BY b.bot, json_extract(CAST(sent.data AS TEXT),'$.event_seq') DESC,
 json_extract(t.value,'$.fence.generation') DESC,
 json_extract(t.value,'$.state') IN ('accepted','queued') DESC,
 json_extract(t.value,'$.started_at') DESC, t.key`,
		group.Key.MachineID, group.Key.BotID, group.Key.MachineID, group.Key.BotID, room.Epoch)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	projected := make(map[pa.BotKey]bool, len(group.Members))
	for rows.Next() {
		var keyRaw, projectRaw, conversationRaw, turnRaw []byte
		var name, id string
		if err = rows.Scan(&keyRaw, &name, &projectRaw, &conversationRaw, &id, &turnRaw); err != nil {
			return nil, nil, err
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
				return nil, nil, failure(pa.Invalid, "room_activity")
			}
		}
		if turn.ID != id || turn.Room == nil {
			return nil, nil, failure(pa.Invalid, "room_activity")
		}
		// Stale tickets or another conversation/fence cannot claim a member
		// is working in this room. Queued tickets may precede a native rebind.
		if project != group.Project || turn.Fence.Key != key || turn.Fence.Project != project ||
			conversation.Key != key || turn.Fence.ConversationID != conversation.ID ||
			turn.Fence.Generation > conversation.Generation || pa.ValidateRoomTurn(turn) != nil ||
			pa.CheckRoomTicket(group, room, *turn.Room) != nil {
			continue
		}
		if turn.State == pa.TurnRunning || turn.State == pa.TurnWaitingPermission {
			if turn.ID != conversation.ActiveTurnID || turn.Fence.BindingID != conversation.BindingID || turn.Fence.Generation != conversation.Generation {
				continue
			}
		}
		switch turn.State {
		case pa.TurnAccepted, pa.TurnQueued, pa.TurnRunning, pa.TurnWaitingPermission:
			activity = append(activity, pa.RoomActivity{Member: key, TurnID: turn.ID, State: turn.State})
		case pa.TurnSucceeded, pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown:
		default:
			continue
		}
		if turn.Room.Epoch == room.Epoch && !projected[key] {
			outcomes = append(outcomes, app.ProjectRoomMemberOutcome(room, name, turn))
			projected[key] = true
		}
	}
	return activity, outcomes, rows.Err()
}
