package projectassistant

import "strings"

const RestartContinuationPrefix = "runtime-resume-"
const RestartResumedField = "restart_resumed"
const SteerUnknownField = "steer_unknown"

// RestartNotRepeatedField marks a turn a restart interrupted that was settled
// without a continuation so the queue behind it could run. Its effect stays
// unknown; it was never repeated.
const RestartNotRepeatedField = "restart_not_repeated"

// NoticeRestartNotRepeated is the key of the chat notice for such a turn;
// Cause is the interrupted turn's ID.
const NoticeRestartNotRepeated = "restart_not_repeated"

func RestartSourceTurn(turn Turn) string {
	if turn.Origin != HostNoticeOrigin || turn.RoutineRunID != "" || turn.Room != nil || turn.Network != nil {
		return ""
	}
	id, ok := strings.CutPrefix(turn.IdempotencyKey, RestartContinuationPrefix)
	if !ok {
		return ""
	}
	return id
}

// QueuedChatRestartWork is already admitted work whose original execution
// has not begun. Other origins have their own recovery and authority rules.
func QueuedChatRestartWork(turn Turn) bool {
	if turn.State != TurnQueued || turn.StartedAt != nil || turn.Steering {
		return false
	}
	return QueuedWorkCompletion(turn) || RestartSourceTurn(turn) != "" ||
		(turn.Origin == UserOrigin || turn.Origin == "") && turn.Room == nil && turn.Network == nil && turn.DecisionExchangeID == "" && turn.RoutineRunID == ""
}

func (t Turn) AcknowledgeUnknown() Turn {
	t.State = TurnFailed
	t.Failure = &Failure{Code: ExecutionEffectUnknown, Field: "acknowledged"}
	return t
}

// HeldAutomaticWork is queued automatic work a person's message that
// released a restart hold may run ahead of: a host notice such as a work
// review, or another Bot's request or reply. It never names the person's own
// messages, a restart continuation, room or network work, or a held edit.
func HeldAutomaticWork(t Turn) bool {
	if t.State != TurnQueued && t.State != TurnAccepted || t.StartedAt != nil || t.Steering || t.QueueEditID != "" ||
		t.Room != nil || t.Network != nil || RestartSourceTurn(t) != "" {
		return false
	}
	return t.Origin == HostNoticeOrigin || t.Origin == BotRequestOrigin || t.Origin == BotReplyOrigin
}

// OrderQueue moves each queued turn that released a restart hold ahead of
// the automatic work directly before it, keeping every other relative order.
// turns must already be in admission order.
func OrderQueue(turns []Turn) []Turn {
	for i := range turns {
		t := turns[i]
		if !t.ReleasedRestart || t.State != TurnQueued && t.State != TurnAccepted || t.StartedAt != nil {
			continue
		}
		j := i
		for j > 0 && HeldAutomaticWork(turns[j-1]) {
			j--
		}
		if j < i {
			copy(turns[j+1:i+1], turns[j:i])
			turns[j] = t
		}
	}
	return turns
}
