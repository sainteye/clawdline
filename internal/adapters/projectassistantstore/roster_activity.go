package projectassistantstore

import (
	"strings"
	"time"

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

// An open question the Bot asked takes precedence over its turn state: it
// belongs to no turn, and until the person answers it is what they must do.
func projectRosterActivity(a aggregate, out *pa.BotSummary) {
	projectTurnActivity(a, out)
	projectOpenDecision(a, out)
}

func projectOpenDecision(a aggregate, out *pa.BotSummary) {
	if out.Status == "needsInput" {
		return
	}
	// The newest question is the one the person's next message answers.
	now := time.Now()
	var selected string
	for id, p := range a.Permissions {
		if p.Kind == pa.DecisionOfferKind && openPermission(p, now) && (selected == "" || p.AnchorMessageSeq > a.Permissions[selected].AnchorMessageSeq || p.AnchorMessageSeq == a.Permissions[selected].AnchorMessageSeq && id < selected) {
			selected = id
		}
	}
	if selected == "" {
		return
	}
	out.Status, out.Activity, out.Failure = "needsInput", "", nil
	if d := a.Permissions[selected].Decision; d != nil {
		out.PendingDecisionKind = pa.RosterDecisionQuestion
		if d.Memory != "" {
			out.PendingDecisionKind = pa.RosterDecisionMemorySave
		}
		if q := strings.TrimSpace(strings.SplitN(d.Question, "\n", 2)[0]); q != "" {
			out.Activity = "Asks: " + q
		}
	}
}

func projectTurnActivity(a aggregate, out *pa.BotSummary) {
	turn, active := a.Turns[a.Conversation.ActiveTurnID]
	var recent pa.Turn
	if !active || turn.Room != nil {
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
			if !ok || candidate.Room != nil || candidate.JoinedTurnID != "" || candidate.MessageID != m.ID {
				continue
			}
			recent = candidate
			break
		}
	}
	projectTurnActivityResolved(a, out, turn, active, recent)
}

func projectTurnActivityResolved(a aggregate, out *pa.BotSummary, activeTurn pa.Turn, active bool, recentTurn pa.Turn) {
	if a.Completion != nil && a.Completion.ConversationID == a.Conversation.ID {
		out.CompletedTurnID = a.Completion.TurnID
	}
	out.Status = "idle"
	turn := activeTurn
	if active && turn.Room != nil {
		// Execution/cancellation and approval authority stay Bot-wide, but room
		// progress and failures do not become own-chat activity.
		if turn.State == pa.TurnWaitingPermission {
			out.Status = "needsInput"
			return
		}
		active, turn = false, pa.Turn{}
	}
	if !active {
		turn = recentTurn
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
		if activity := traceActivity(*e.Trace); activity != "" {
			out.Activity = activity
		}
	}
}

func traceActivity(trace pa.Trace) string {
	switch trace.Category {
	case "tool_call", "tool_call_update", "tool":
		return strings.TrimSpace(strings.SplitN(trace.Text, "\n", 2)[0])
	case "agent_message_chunk":
		return "writing"
	case "agent_thought_chunk":
		return "thinking"
	}
	return ""
}
