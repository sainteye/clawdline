package cloud

import "testing"

// The one rule: the same (sender, seq) is claimed at most once.
func TestASequenceIsClaimedOnce(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	if got := tracker.Claim("viewer-1", 7); got != ReplayAccepted {
		t.Fatalf("first claim of 7: %v", got)
	}
	if got := tracker.Claim("viewer-1", 7); got != ReplayRefused {
		t.Fatalf("second claim of 7: %v, want replay", got)
	}
}

// Two tabs of one device each reserve their own block of sequences, so the
// lower-numbered one arrives second and must still be accepted. A strictly
// increasing counter stood here once and refused it.
func TestALowerSequenceInsideTheWindowIsNotAReplay(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	for _, seq := range []uint64{5000, 4096, 4999, 4097} {
		if got := tracker.Claim("viewer-1", seq); got != ReplayAccepted {
			t.Fatalf("claim of %d: %v, want accepted", seq, got)
		}
	}
	for _, seq := range []uint64{5000, 4096, 4999, 4097} {
		if got := tracker.Claim("viewer-1", seq); got != ReplayRefused {
			t.Fatalf("second claim of %d: %v, want replay", seq, got)
		}
	}
}

// The window is exactly 1024 wide, and the edges are where an off-by-one
// lives: highest-1024 is still decidable, highest-1025 is not.
func TestTheWindowEdgesAreExact(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	const highest = 10000
	if got := tracker.Claim("v", highest); got != ReplayAccepted {
		t.Fatalf("highest: %v", got)
	}
	if got := tracker.Claim("v", highest-ReplayWindowSize); got != ReplayAccepted {
		t.Fatalf("highest-1024: %v, want accepted", got)
	}
	if got := tracker.Claim("v", highest-ReplayWindowSize-1); got != ReplayRefused {
		t.Fatalf("highest-1025: %v, want replay", got)
	}
}

// An answer that has fallen off the bottom of the window is *undecidable*, and
// undecidable is refused. This is the rule that makes the whole thing safe:
// the tracker never guesses.
func TestAnUndecidableSequenceIsRefused(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	if got := tracker.Claim("v", 1); got != ReplayAccepted {
		t.Fatalf("first: %v", got)
	}
	// Jumping far ahead shifts 1 out of the window entirely.
	if got := tracker.Claim("v", 1+ReplayWindowSize+1); got != ReplayAccepted {
		t.Fatalf("far ahead: %v", got)
	}
	if got := tracker.Claim("v", 1); got != ReplayRefused {
		t.Fatalf("the sequence that fell off: %v, want replay", got)
	}
}

// Advancing past the whole window clears it, and the old highest is recorded
// at its new offset when it still fits.
func TestAdvancingRecordsTheOldHighest(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	tracker.Claim("v", 100)
	tracker.Claim("v", 200)
	if got := tracker.Claim("v", 100); got != ReplayRefused {
		t.Fatalf("the old highest after advancing: %v, want replay", got)
	}
	if got := tracker.Claim("v", 150); got != ReplayAccepted {
		t.Fatalf("an unclaimed position inside the window: %v", got)
	}
}

// A sender past the cap is refused, and nobody else's window is evicted to
// make room — an evicted window can no longer decide anything.
func TestSenderCapacityRefusesRatherThanEvicts(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(2)
	if got := tracker.Claim("a", 1); got != ReplayAccepted {
		t.Fatalf("a: %v", got)
	}
	if got := tracker.Claim("b", 1); got != ReplayAccepted {
		t.Fatalf("b: %v", got)
	}
	if got := tracker.Claim("c", 1); got != ReplaySenderCapacity {
		t.Fatalf("c: %v, want sender capacity", got)
	}
	// a is still tracked, which is the whole point.
	if got := tracker.Claim("a", 1); got != ReplayRefused {
		t.Fatalf("a's window after the refusal: %v, want replay", got)
	}
	if tracker.TrackedSenders() != 2 {
		t.Fatalf("tracked senders: %d, want 2", tracker.TrackedSenders())
	}
}

// Two senders do not share a window.
func TestSendersAreIndependent(t *testing.T) {
	t.Parallel()
	tracker := NewSequenceTracker(0)
	if got := tracker.Claim("a", 5); got != ReplayAccepted {
		t.Fatalf("a: %v", got)
	}
	if got := tracker.Claim("b", 5); got != ReplayAccepted {
		t.Fatalf("b: %v, want accepted — b has its own window", got)
	}
}

// The process-wide window is what a rebuilt transport shares, so a
// replacement cannot re-accept what its predecessor already accepted.
func TestTheProcessWindowIsShared(t *testing.T) {
	t.Parallel()
	window := NewReplayWindow(0)
	if got := window.Claim("v", 3); got != ReplayAccepted {
		t.Fatalf("first transport: %v", got)
	}
	if got := window.Claim("v", 3); got != ReplayRefused {
		t.Fatalf("second transport on the same window: %v, want replay", got)
	}
	if highest, ok := window.Highest("v"); !ok || highest != 3 {
		t.Fatalf("highest: %d %v", highest, ok)
	}
}
