package projectassistant

import "strings"

const WorkCompletionPrefix = "runtime-work-completion-"

func WorkCompletionTask(turn Turn) string {
	if turn.Origin != HostNoticeOrigin || turn.RoutineRunID != "" || turn.Room != nil || turn.Network != nil {
		return ""
	}
	id, ok := strings.CutPrefix(turn.IdempotencyKey, WorkCompletionPrefix)
	if !ok {
		return ""
	}
	return id
}

func QueuedWorkCompletion(turn Turn) bool {
	return WorkCompletionTask(turn) != "" && turn.State == TurnQueued && turn.StartedAt == nil
}

// ReviewsWork reports the per-Bot default: every dispatched task queues one
// review turn unless the person turned the setting off.
func (b Bot) ReviewsWork() bool { return b.WorkReview != "off" }
