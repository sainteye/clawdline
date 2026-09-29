package terminal

// How one numbered input is decided against a lease's state (plan v3 D4).
//
// A lease is an epoch; inside it the controller numbers its inputs 1, 2, 3 …
// and the lease remembers the highest number typed. The caller decides and
// types inside one critical section per terminal, so two copies of the same
// input arriving together are decided one after the other: the second finds
// the first already applied and is a duplicate, and nothing is typed twice.

// InputDecision is what to do with one numbered input.
type InputDecision string

const (
	// InputApply: type it, then advance applied to its seq.
	InputApply InputDecision = "apply"
	// InputDuplicate: it was typed already; answer success and type nothing.
	InputDuplicate InputDecision = "duplicate"
	// InputGap: an earlier input has not arrived; refuse with input_gap and
	// the applied number, so the sender resends from there.
	InputGap InputDecision = "gap"
	// InputSuperseded: it belongs to another epoch; refuse with
	// lease_superseded. Its bytes are never typed under this lease.
	InputSuperseded InputDecision = "superseded"
	// InputInvalid: seq 0, which no sender numbers an input with.
	InputInvalid InputDecision = "invalid"
)

// DecideInput is the decision for an input numbered (epoch, seq) against a
// lease at `epoch` that has applied inputs up to `applied`.
//
// An epoch that is not the lease's is superseded whether it is older or
// newer: a newer one was never handed out by this lease, and typing it would
// be typing for somebody the lease does not know.
func DecideInput(applied, epoch, inEpoch, seq uint64) InputDecision {
	switch {
	case inEpoch != epoch:
		return InputSuperseded
	case seq == 0:
		return InputInvalid
	case seq <= applied:
		return InputDuplicate
	case seq == applied+1:
		return InputApply
	}
	return InputGap
}
