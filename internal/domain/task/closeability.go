package task

// CloseState says whether a session may end.
//
// It is a separate question from whether it can take work, and neither may be
// read off the other.
type CloseState string

const (
	CloseSafe    CloseState = "safe"
	CloseBlocked CloseState = "blocked"
	CloseUnknown CloseState = "unknown"
)

// CloseReason is one thing still owed by or to this session.
type CloseReason struct {
	Kind  Kind
	Mover Mover
	Note  string
}

// Close is the whole answer: a state and, when it is blocked, what by.
type Close struct {
	State   CloseState
	Reasons []CloseReason
}

// Closeability decides whether a session may end.
//
// This lives in the domain rather than in the route that draws it, because two
// callers need exactly the same answer: the fleet list, which shows it, and the
// close action, which refuses on it. A screen that says `safe` while the action
// says `blocked` would be worse than either alone.
//
// An unreadable obligation list fails the whole projection closed to `unknown`
// rather than to `safe`, because what it casts doubt on is the completeness of
// the list itself, not one entry in it. `safe` is a positive claim that nothing
// is owed, and it must never be what a failure looks like.
func Closeability(subject string, owed []Obligation, err error) Close {
	if err != nil {
		return Close{State: CloseUnknown, Reasons: []CloseReason{}}
	}
	reasons := []CloseReason{}
	for _, o := range owed {
		if !ownsClose(o, subject) {
			continue
		}
		reasons = append(reasons, CloseReason{Kind: o.Kind, Mover: o.Mover, Note: o.Note})
	}
	if len(reasons) > 0 {
		return Close{State: CloseBlocked, Reasons: reasons}
	}
	return Close{State: CloseSafe, Reasons: reasons}
}

// ownsClose reports whether this obligation stands in the way of that session
// ending.
//
// An obligation naming a session as its mover belongs to that session: it is
// the one that has to act. Anything else belongs to its subject.
func ownsClose(o Obligation, subject string) bool {
	if o.Mover.Kind == MoverOtherSession || o.Mover.Kind == MoverThisSession {
		return o.Mover.ID == subject
	}
	return o.Subject == subject
}
