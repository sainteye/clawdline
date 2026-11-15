package projectassistantstore

import (
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Keep one validated trace beside the aggregate, rather than querying the
// event history once per roster row. The transaction that publishes the event
// also owns this projection; failed/duplicate events cannot advance it.
func rememberRosterTrace(a *aggregate, event pa.Event) {
	if event.Kind == pa.TraceUpdated && event.Trace != nil && !event.Trace.Complete && event.Trace.TurnID == a.Conversation.ActiveTurnID {
		switch event.Trace.Category {
		case "tool_call", "agent_message_chunk", "agent_thought_chunk":
		default:
			return
		}
		trace := *event.Trace
		a.RosterTrace = &pa.Event{Fence: event.Fence, Trace: &trace}
	}
}

func projectRosterActivity(a aggregate, out *pa.BotSummary) {
	out.Status = "idle"
	turn, active := a.Turns[a.Conversation.ActiveTurnID]
	if !active {
		// Admission order comes from messages, never map iteration or a
		// lexicographic turn ID. Restart continuations and routines enter with
		// notices. Only a turn's own admission anchor selects it; later log
		// messages from an older turn cannot change the selected state.
		for i := len(a.Messages) - 1; i >= 0; i-- {
			m := a.Messages[i]
			if m.Kind != pa.UserMessage && m.Kind != pa.NoticeMessage {
				continue
			}
			candidate, ok := a.Turns[m.TurnID]
			if !ok || candidate.JoinedTurnID != "" || candidate.MessageID != m.ID {
				continue
			}
			turn = candidate
			break
		}
	}
	switch turn.State {
	case pa.TurnFailed, pa.TurnUnknown:
		out.Status = "error"
		out.Failure = turn.Failure
		if turn.State == pa.TurnUnknown {
			out.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown}
		}
		return
	case pa.TurnRunning, pa.TurnAccepted, pa.TurnQueued, pa.TurnWaitingPermission:
		if !active {
			return
		}
		out.Status, out.Activity = "working", string(turn.State)
	default:
		return
	}
	var selected string
	for id, p := range a.Permissions {
		if p.State == pa.PermissionPending && p.TurnID == turn.ID && p.Fence == turn.Fence && (selected == "" || id < selected) {
			selected = id
		}
	}
	if turn.State == pa.TurnWaitingPermission || selected != "" {
		out.Status, out.Activity = "needsInput", ""
		if p := a.Permissions[selected]; p.Action != nil {
			if title := strings.TrimSpace(p.Action.Title); title != "" {
				out.Activity = "Needs approval: " + title
			}
		}
		return
	}
	if e := a.RosterTrace; e != nil && e.Trace != nil && e.Fence == turn.Fence && e.Trace.TurnID == turn.ID && !e.Trace.Complete {
		switch e.Trace.Category {
		case "tool_call":
			if text := strings.TrimSpace(e.Trace.Text); text != "" {
				out.Activity = text
			}
		case "agent_message_chunk":
			out.Activity = "writing"
		case "agent_thought_chunk":
			out.Activity = "thinking"
		}
	}
}
