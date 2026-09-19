package work

import (
	"strings"
	"time"
	"unicode"
)

// The session to-do list: step 2 of board-redesign §9, T2 of
// docs/design-decisions.md §6.
//
// A to-do is what a session must not forget, and it is kept for that session
// alone: made, tracked and closed by the broker's facts, never by a person and
// never by the session remembering to call anything (board-redesign §3.1: 43.6%
// of runs forgot even `deliver`). Nothing here asks anybody, pushes anything
// or reaches the board — the board's reader is a person, this list's reader is
// a session, and one record for two readers is the defect the redesign exists
// to undo (D1, #5).
//
// These are the rules and nothing else. They read the facts they are handed
// and answer what the to-do is now; the broker applies them inside the same
// transaction that records the fact (D02: a task landing and its to-do closing
// are one fact), and again when it starts, so a to-do can neither be missed
// nor made twice. The presence rule is the same one the three-track projection
// reads — [Presence], [Liveness] — so "is that session gone" has one answer
// across the package (DG-7).

// TodoState is where a to-do is. Open and handed-off are still owed; done
// and dropped are over.
type TodoState string

const (
	// TodoStateOpen is owed by the session that owns it.
	TodoStateOpen TodoState = "open"
	// TodoStateHandedOff is owed and its owner is positively gone. Nobody has
	// received it yet: the broker has no parent above a root and no handoff
	// route (W6), so the receiver column stays empty until one exists.
	TodoStateHandedOff TodoState = "handed_off"
	// TodoStateDone is closed by a fact: a landing, or a task that ended owing
	// none.
	TodoStateDone TodoState = "done"
	// TodoStateDropped is let go: its owner stayed gone past the grace and nobody
	// took it. The task and any landing it owes stay where they are, in the
	// broker's own inventory; only the reminder stops.
	TodoStateDropped TodoState = "dropped"
)

// TodoStates is every state, in the order counts are listed in.
var TodoStates = []TodoState{TodoStateOpen, TodoStateHandedOff, TodoStateDone, TodoStateDropped}

// Outstanding says the to-do is still owed.
func (s TodoState) Outstanding() bool { return s == TodoStateOpen || s == TodoStateHandedOff }

// Bucket is where the three-track projection (tracks.go) puts a to-do in this
// state: owed is live, over is done. One vocabulary for the one track, whether
// it was read off an old card or recorded by the broker.
func (s TodoState) Bucket() Bucket {
	if s.Outstanding() {
		return TodoLive
	}
	return TodoDone
}

// TodoOrigin is which fact made a to-do.
//
// Only a dispatch has a producer in this broker. The redesign names three
// more — a result's `remaining`, a delivery's `remaining`, an obligation — and
// none of them is a fact this daemon records yet: result.json has no
// `remaining`, a delivery receipt is one sentence, and the obligations table
// is being retired (D07). A to-do from a fact nobody writes would be one this
// package invented.
type TodoOrigin string

// OriginDispatch is a root's "collect it and land it" for one dispatched task.
const OriginDispatch TodoOrigin = "dispatch"

// Why a to-do is in its state. Each is the fact, or the reading, that put it
// there.
const (
	ReasonDispatched = "dispatched"
	// Closing facts: the broker's landing record (D01), or a task that ended
	// owing no landing at all.
	ReasonLanded        = "landed"
	ReasonNothingToLand = "nothing_to_land"
	ReasonAbandoned     = "abandoned"
	ReasonNothingOwed   = "nothing_owed"
	// ReasonObligationOpened is a to-do closed as owing nothing that a
	// landing obligation opened on afterwards.
	ReasonObligationOpened = "obligation_opened"
	// Presence: the owner positively gone, back again, or gone past the grace.
	ReasonOwnerGone     = "owner_gone"
	ReasonOwnerReturned = "owner_returned"
	ReasonUnowned       = "unowned_past_grace"
)

// UnownedGrace is how long a handed-off to-do nobody took is kept before it is
// dropped: board-redesign §5.2, 24 hours.
const UnownedGrace = 24 * time.Hour

// LongLived is the age past which an owed to-do is worth asking a person about
// (I2, board-redesign §4.2: the p95 of how long one lives is 23.6 hours).
const LongLived = 24 * time.Hour

// Todo is one to-do as the rules see it and the store keeps it.
type Todo struct {
	// ID is the origin and the fact it came from, so the same fact names the
	// same to-do however many times it is applied: `dispatch:<task id>`.
	ID     string     `json:"id"`
	Origin TodoOrigin `json:"origin"`
	// Task is the broker task this to-do follows.
	Task string `json:"task_id"`
	// WorkID is the line of work its task is on (D36, lines.go): the one the
	// dispatch named, or the one the broker bound it to. Empty only for a step
	// of other work whose line nobody named.
	WorkID  string `json:"work_id,omitempty"`
	Title   string `json:"title"`
	Project string `json:"project"`
	// Owner is the conversation id of the session that owes it — the same
	// value as the task's `root.session_id` — and OwnerAssistant which
	// assistant that conversation belongs to. A conversation id, never a
	// terminal id: the conversation outlives the tab it was drawn in.
	Owner          string    `json:"owner_session"`
	OwnerAssistant string    `json:"owner_assistant"`
	State          TodoState `json:"state"`
	Reason         string    `json:"reason"`
	// HandedTo is who received a handed-off to-do; empty while nobody has.
	HandedTo    string    `json:"handed_to,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	HandedOffAt time.Time `json:"handed_off_at,omitempty"`
	ClosedAt    time.Time `json:"closed_at,omitempty"`
}

// TodoID is a to-do's id for the fact that made it.
func TodoID(origin TodoOrigin, task string) string { return string(origin) + ":" + task }

// TaskFacts are the broker facts about one task that the rules read. Every
// field is recorded; none is an observation.
type TaskFacts struct {
	Task    string
	WorkID  string
	Title   string
	Kind    string
	Project string
	// Owner and OwnerAssistant are the task's root. Empty Owner is a task
	// with no root — detached automation — which owes no session anything.
	Owner          string
	OwnerAssistant string
	CreatedAt      time.Time
	// State is the broker's word for the task, and Ended whether it is one
	// nothing moves out of.
	State string
	Ended bool
	// Landing is the landing record's state: "", pending, landed,
	// nothing_to_land or abandoned.
	Landing string
	// Attempt is how far down a respawn chain the task is: 0 for an original.
	Attempt int
	// FinishedAt and LandedAt are when the task ended and when its landing
	// was recorded; zero while neither has happened. The board's clock reads
	// them (board.go); the to-do rules do not.
	FinishedAt time.Time
	LandedAt   time.Time
	// LandingTarget is the branch the landing record names, empty until a
	// root named one (D19). T4's I3 reads it: a landing on a default branch
	// is an effect outside this machine.
	LandingTarget string
}

// owed is what a task's facts say about its to-do: still owed, or closed and
// why. It reads the landing record and the task's end, and nothing else — the
// to-do's closing is a function of the one landing fact (D01), not a second
// copy of it.
func owed(f TaskFacts) (closed bool, reason string) {
	switch f.Landing {
	case "landed":
		return true, ReasonLanded
	case "nothing_to_land":
		return true, ReasonNothingToLand
	case "abandoned":
		return true, ReasonAbandoned
	case "pending":
		return false, ""
	}
	if f.Ended {
		// Ended with no landing obligation: the task declared no paths and had
		// no checkout, so the broker holds nothing it still owes.
		return true, ReasonNothingOwed
	}
	return false, ""
}

// DispatchTodo is the to-do a dispatched task leaves its root, as the facts
// now stand. The second answer is false when it leaves none: a task with no
// root owes no session anything.
func DispatchTodo(f TaskFacts, at time.Time) (Todo, bool) {
	if f.Owner == "" {
		return Todo{}, false
	}
	t := Todo{
		ID: TodoID(OriginDispatch, f.Task), Origin: OriginDispatch, Task: f.Task,
		WorkID: f.WorkID, Title: f.Title, Project: f.Project,
		Owner: f.Owner, OwnerAssistant: f.OwnerAssistant,
		State: TodoStateOpen, Reason: ReasonDispatched,
		CreatedAt: at, UpdatedAt: at,
	}
	if closed, why := owed(f); closed {
		t.State, t.Reason, t.ClosedAt = TodoStateDone, why, at
	}
	return t, true
}

// Transition is one change the rules made to a to-do, for the event that
// records it.
type Transition struct {
	From   TodoState `json:"from"`
	To     TodoState `json:"to"`
	Reason string    `json:"reason"`
}

// Follow applies a task's facts to its to-do and answers the to-do now, with
// the transition when there was one.
//
//   - A closing fact closes it, whoever owes it: open, handed off or dropped
//     all become done, because "it landed" is truer than "nobody took it".
//     A done to-do whose closing fact has since become a different one — a
//     task that ended owing nothing and was then recorded landed — says the
//     new one: the reason is the landing record's, never a copy of an older
//     answer (D01).
//   - A to-do closed as owing nothing reopens when a landing obligation opens
//     on the same task afterwards: the landing record is the fact and the
//     to-do says what it says.
//   - Otherwise nothing changes. A fact that has not arrived moves nothing.
func Follow(t Todo, f TaskFacts, at time.Time) (Todo, *Transition) {
	closed, why := owed(f)
	switch {
	case closed && (t.State != TodoStateDone || t.Reason != why):
		return moved(t, TodoStateDone, why, at)
	case !closed && t.State == TodoStateDone && t.Reason == ReasonNothingOwed:
		next, tr := moved(t, TodoStateOpen, ReasonObligationOpened, at)
		next.ClosedAt = time.Time{}
		return next, tr
	}
	return t, nil
}

// Tend applies a reading of the owner's presence, and the clock, to a to-do.
//
//   - Open, and the owner positively gone: handed off.
//   - Handed off, and the owner live again: open, with its owner.
//   - Handed off for the grace, and the owner still positively gone: dropped.
//
// Unknown moves nothing in any direction (DG-7): a reading that could not see
// every session has not said anybody left, and has not said they came back.
func Tend(t Todo, owner Liveness, at time.Time) (Todo, *Transition) {
	switch t.State {
	case TodoStateOpen:
		if owner == Gone {
			next, tr := moved(t, TodoStateHandedOff, ReasonOwnerGone, at)
			next.HandedOffAt = at
			return next, tr
		}
	case TodoStateHandedOff:
		switch {
		case owner == Live:
			next, tr := moved(t, TodoStateOpen, ReasonOwnerReturned, at)
			next.HandedOffAt = time.Time{}
			return next, tr
		case owner == Gone && !t.HandedOffAt.IsZero() && at.Sub(t.HandedOffAt) >= UnownedGrace:
			return moved(t, TodoStateDropped, ReasonUnowned, at)
		}
	}
	return t, nil
}

// Bound is a to-do on the line its task is on now. A task is bound to its
// line once — at admission, or when the broker binds one stored before it
// did — and the to-do says what the task says, as it says the task's landing:
// never a copy of an older answer. The second answer is true when it moved.
func Bound(t Todo, f TaskFacts, at time.Time) (Todo, bool) {
	if f.WorkID == "" || t.WorkID == f.WorkID {
		return t, false
	}
	t.WorkID, t.UpdatedAt = f.WorkID, at
	return t, true
}

func moved(t Todo, to TodoState, why string, at time.Time) (Todo, *Transition) {
	tr := &Transition{From: t.State, To: to, Reason: why}
	t.State, t.Reason, t.UpdatedAt = to, why, at
	if !to.Outstanding() {
		t.ClosedAt = at
	}
	return t, tr
}

// Signal is a fact that makes an owed to-do worth asking a person about
// (board-redesign §4.2). A signal is not a proposal: whether and how to ask is
// T4's, behind its own refusals. This list is the interface T4 reads.
type Signal string

const (
	// SignalCrossSession is I1: the work sent out a child, and no work item
	// holds it yet.
	SignalCrossSession Signal = "cross_session"
	// SignalLongLived is I2: owed for longer than LongLived.
	SignalLongLived Signal = "long_lived"
	// SignalRepeatedFailure is §6's "the same to-do's attempt failed a second
	// time": a respawned task that did not succeed either.
	SignalRepeatedFailure Signal = "repeated_failure"
)

// auxiliaryKinds are the kinds §4.2 never proposes: steps of some other piece
// of work, and questions.
var auxiliaryKinds = map[string]bool{"review": true, "test": true, "correction": true,
	"question": true, "clarification": true}

// failedStates are a task that ended without delivering.
var failedStates = map[string]bool{"failure": true, "timeout": true, "spawn_failed": true}

// Escalation is every signal an owed to-do carries, in a fixed order. A to-do
// that is over, or whose task is a step of other work, carries none. held
// says a work item on the board or in the Backlog holds the to-do's line:
// every dispatch is on a line (lines.go), so having one is not being followed.
func Escalation(t Todo, f TaskFacts, held bool, at time.Time) []Signal {
	out := []Signal{}
	if !t.State.Outstanding() || auxiliary(f.Kind) {
		return out
	}
	if t.Origin == OriginDispatch && !held {
		out = append(out, SignalCrossSession)
	}
	if !t.CreatedAt.IsZero() && at.Sub(t.CreatedAt) > LongLived {
		out = append(out, SignalLongLived)
	}
	if f.Attempt > 0 && f.Ended && failedStates[f.State] {
		out = append(out, SignalRepeatedFailure)
	}
	return out
}

// auxiliary reads a task kind word by word, as the result validator does, so
// "code-review" and "test_fix" are steps and "custom" is not.
func auxiliary(kind string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(kind), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if auxiliaryKinds[w] {
			return true
		}
	}
	return false
}
