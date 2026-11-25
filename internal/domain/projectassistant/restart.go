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
