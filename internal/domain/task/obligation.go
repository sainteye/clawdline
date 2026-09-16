// Package task is the coordination domain: what is owed, by whom, and since
// when. It is pure — nothing here reads a clock, a file or a socket, so every
// rule in it can be stated as a table.
package task

import "time"

// Kind is what sort of obligation this is.
//
// The Swift app models each of these separately, with its own states, its own
// way of stalling and its own way of being queried. They are one concept: a
// thing that is owed, by someone, and that nobody has cleared. Unifying them is
// what makes "what is stuck, and who has to move" a query instead of a round of
// messages to every session on the machine.
type Kind string

const (
	KindWait       Kind = "wait"       // one session waiting on another's paths
	KindLanding    Kind = "landing"    // a delivery that has not reached its branch
	KindHandoff    Kind = "handoff"    // a work line passed to a named receiver
	KindSuccession Kind = "succession" // the machine role moving between sessions
	KindAssignment Kind = "assignment" // a proposal the old owner still owns
	KindDeadLetter Kind = "deadletter" // a completion nobody acknowledged
	KindReview     Kind = "review"     // a delivery awaiting an independent reader
)

// MoverKind names who can clear an obligation.
//
// In the Swift app this exists in exactly one place — the closeability
// projection — and it is the only typed answer anywhere to "who has to move".
// Every obligation has that answer; only one of them writes it down. Here all
// of them do.
type MoverKind string

const (
	MoverThisSession  MoverKind = "this_session"
	MoverOtherSession MoverKind = "other_session"
	MoverPerson       MoverKind = "person"
	MoverTask         MoverKind = "task"
	MoverBroker       MoverKind = "broker"
)

type Mover struct {
	Kind MoverKind `json:"kind"`
	// ID names the session, task or person when the kind needs one. A mover
	// that cannot be named is a mover nobody can be asked to be.
	ID string `json:"id,omitempty"`
}

// Evidence grades what is known about an obligation, in the same vocabulary the
// session readings use.
type Evidence string

const (
	EvidenceObserved Evidence = "observed" // proved by a durable record
	EvidenceDerived  Evidence = "derived"  // projected from other people's records
	EvidenceUnknown  Evidence = "unknown"  // nothing supports or refutes it
)

// Obligation is one outstanding thing, of any kind.
type Obligation struct {
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Subject string `json:"subject"` // whose it is: a session, a task, a repository
	Mover   Mover  `json:"mover"`

	OpenedAt time.Time `json:"opened_at"`
	Evidence Evidence  `json:"evidence"`

	// Note is one line in the owner's own words, when they gave one.
	Note string `json:"note,omitempty"`
}

// Age is how long this has been owed as of `now`.
//
// It is a first-class value rather than something a reader computes, because
// the whole failure this design exists to fix is an obligation that nobody was
// told to look at. On 2026-09-06 this machine ended the day with 26 landings
// nobody had recorded, while the route that lists them had named every one of
// them correctly all day. Nothing made anybody look.
func (o Obligation) Age(now time.Time) time.Duration { return now.Sub(o.OpenedAt) }

// Escalation is how loud an obligation has become. It never becomes a verdict.
type Escalation string

const (
	EscalationQuiet   Escalation = "quiet"
	EscalationAging   Escalation = "aging"
	EscalationOverdue Escalation = "overdue"
)

// Thresholds are the two ages at which an obligation gets louder. They are
// values rather than constants so a test can reach the boundary in
// microseconds instead of waiting out a real deadline — the Swift suite spends
// 71 seconds of a 183-second run doing exactly that waiting.
type Thresholds struct {
	Aging   time.Duration
	Overdue time.Duration
}

var DefaultThresholds = Thresholds{Aging: 30 * time.Minute, Overdue: 4 * time.Hour}

// Escalate reports how loud this obligation should be, and never says it is
// dead.
//
// The principle it protects is the one the Swift app is built on and is right
// about: absence of evidence is never proof of death. "Not in this reading" is
// also true of a terminal that lost its accessibility permission for a moment.
// So nothing here times out, nothing is cancelled, and no state changes.
//
// What changes is visibility. The cost of that principle, in the current
// design, is that every obligation outliving its clock becomes a silent
// `unknown` handed to a person who was never told to look. An ageing, sortable,
// queryable fact is the same honesty with the silence removed.
func (o Obligation) Escalate(now time.Time, t Thresholds) Escalation {
	age := o.Age(now)
	switch {
	case age >= t.Overdue:
		return EscalationOverdue
	case age >= t.Aging:
		return EscalationAging
	default:
		return EscalationQuiet
	}
}

// Stuck reports the obligations a mover has to act on, loudest first.
//
// This is the query that replaces asking. On 2026-09-06 seventeen sessions were
// each sent a message asking whether they were stuck; seventeen answered that
// they were not, and the field that could have answered it was already on every
// row. The cost of a broadcast is not the sender's tokens, it is one turn from
// every recipient, charged to the same quota.
func Stuck(all []Obligation, now time.Time, t Thresholds) []Obligation {
	out := make([]Obligation, 0, len(all))
	for _, o := range all {
		if o.Escalate(now, t) != EscalationQuiet {
			out = append(out, o)
		}
	}
	return out
}
