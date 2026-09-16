package task

// WorkState is the one closed, user-facing answer to "where is this line of
// work". It is a projection over several separate axes — the terminal, the
// tasks, the landings, the waits — and a client must never infer it from any
// one of them.
type WorkState string

const (
	WorkReady             WorkState = "ready"
	WorkWorking           WorkState = "working"
	WorkHolding           WorkState = "holding"
	WorkWaitingYou        WorkState = "waiting_you"
	WorkWaitingSession    WorkState = "waiting_session"
	WorkUnknown           WorkState = "unknown"
	WorkMilestoneComplete WorkState = "milestone_complete"
	WorkComplete          WorkState = "work_complete"
)

// WorkInputs are the separate facts the projection reads. They are passed in
// rather than fetched so the precedence below is a table rather than a trace.
type WorkInputs struct {
	// AskedOnScreen is a question stopped on the person.
	AskedOnScreen bool
	// EvidenceMissing is true when the reading this rests on was incomplete.
	EvidenceMissing bool
	// Active is current activity: a turn running now.
	Active bool
	// WaitingOnPeer is an open obligation whose mover is somebody else.
	WaitingOnPeer bool
	// LiveChildren counts this session's dispatched tasks that have not
	// finished.
	LiveChildren int
	// DeliveredReceipt is an authenticated delivery this session recorded.
	DeliveredReceipt bool
	// PromptWithoutAssistant is an ordinary shell at its prompt: the one piece
	// of positive evidence that a terminal is free to take work.
	PromptWithoutAssistant bool
}

// ProjectWorkState applies the precedence.
//
// The order is the contract's, and each rung is here for a reason that cost
// somebody a day:
//
//   - A question stopped on a person outranks everything, because it is the
//     only state that asks them to act.
//   - Missing evidence outranks activity, because what it casts doubt on is
//     whether the rest of the reading is complete.
//   - Current activity outranks an older receipt: a session that delivered an
//     hour ago and is working now is working.
//   - `ready` requires positive evidence. An idle assistant with none is
//     `unknown`, which asks nothing of the reader. Flattening it into `ready`
//     would invite somebody to send work to a session that is mid-turn.
func ProjectWorkState(in WorkInputs) WorkState {
	switch {
	case in.AskedOnScreen:
		return WorkWaitingYou
	case in.WaitingOnPeer:
		return WorkWaitingSession
	case in.EvidenceMissing:
		return WorkUnknown
	case in.Active:
		return WorkWorking
	case in.LiveChildren > 0:
		// An idle root with work out is not idle: it is waiting for its own
		// children, and that is work in flight rather than a free session.
		return WorkWorking
	case in.DeliveredReceipt:
		return WorkMilestoneComplete
	case in.PromptWithoutAssistant:
		return WorkReady
	default:
		return WorkUnknown
	}
}
