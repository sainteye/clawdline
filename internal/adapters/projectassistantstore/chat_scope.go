package projectassistantstore

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ConversationForChat retains native identity and execution controls, while
// reporting the own-chat transcript's original (possibly sparse) sequence.
func (s *Store) ConversationForChat(ctx context.Context, key pa.BotKey, id string) (pa.Conversation, error) {
	a, err := load(ctx, s.db, key)
	if err != nil {
		return pa.Conversation{}, err
	}
	if a.Conversation.ID != id {
		return pa.Conversation{}, failure(pa.NotFound, "conversation")
	}
	c := a.Conversation
	c.LastMessageSeq = 0
	for _, m := range a.Messages {
		if ownChatTurn(a, m.TurnID) {
			c.LastMessageSeq = m.Seq
		}
	}
	return c, nil
}

// Native execution remains Bot-wide. Only the human transcript is projected:
// retain the original aggregate, IDs, sequences, binding and archive untouched.
func ownChatTurn(a aggregate, id string) bool { return a.Turns[id].Room == nil }

// These expressions are adapter-owned SQL, never input from a request/cursor.
// The same predicate is applied before LIMIT, window budgets and counts.
func ownChatTurnSQL(id string) string {
	return `NOT EXISTS(SELECT 1 FROM room_turns WHERE room_turns.id=` + id + `)`
}

const roomTurnsCTE = `, room_turns AS MATERIALIZED (
 SELECT t.key id FROM a,json_each(a.j,'$.Turns') t WHERE t.type='object' AND json_type(t.value,'$.room')='object'
) `

// Unqueue is the intentional cancellation of a turn that never started and
// has no execution failure. Keep its original facts and receipt for auditing.
func withdrawnTurn(t pa.Turn) bool {
	return t.State == pa.TurnCancelled && t.Failure == nil && t.StartedAt == nil
}

func ownChatMessageSQL(id string) string {
	return ownChatTurnSQL(id) + ` AND NOT EXISTS(SELECT 1 FROM withdrawn_turns WHERE withdrawn_turns.id=` + id + `)`
}

const withdrawnTurnsCTE = `, withdrawn_turns AS MATERIALIZED (
 SELECT t.key id FROM a,json_each(a.j,'$.Turns') t WHERE t.type='object'
 AND json_extract(t.value,'$.state')='cancelled' AND json_extract(t.value,'$.failure') IS NULL
 AND json_extract(t.value,'$.started_at') IS NULL
) `

func ownChatEventSQL(event string) string {
	// Permissions remain visible: a member's single execution can require a
	// decision or cancellation even while the person is in its own chat.
	return ownChatTurnSQL(`coalesce(json_extract(` + event + `,'$.message.turn_id'),json_extract(` + event + `,'$.turn.id'),json_extract(` + event + `,'$.trace.turn_id'),json_extract(` + event + `,'$.work.turn_id'),'')`)
}

// unreadKindsSQL is pa.CountsAsUnread for the roster projection.
var unreadKindsSQL = "('" + string(pa.AssistantMessage) + "','" + string(pa.WorkMessage) + "')"
