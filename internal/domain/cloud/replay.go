package cloud

// Inbound replay protection: docs/cloud-wire.md §6.3, ported from
// `Sources/CloudEnvelope.swift:360-488` without changing a rule.

import "sync"

const (
	// ReplayWindowSize is how far below the highest accepted sequence this
	// remembers. `CloudEnvelope.swift:374`.
	ReplayWindowSize uint64 = 1024
	// MaxTrackedSenders bounds the memory this can be made to hold. A sender
	// past it is refused rather than tracked by evicting somebody else's
	// window: an evicted window can no longer decide anything, so eviction
	// turns a bounded memory cost into an unbounded correctness one
	// (`CloudEnvelope.swift:375-378`).
	MaxTrackedSenders = 4096

	replayWords = int(ReplayWindowSize / 64)
)

// ReplayDecision is what a claim answered.
type ReplayDecision uint8

const (
	// ReplayAccepted means this (sender, seq) had not been claimed before and
	// now has been.
	ReplayAccepted ReplayDecision = iota
	// ReplayRefused means the pair was claimed already, or is too old to
	// decide. Those two are deliberately one answer: a sequence whose bit has
	// fallen off the bottom of the window is *undecidable*, and an
	// undecidable envelope is refused exactly as a repeat is.
	ReplayRefused
	// ReplaySenderCapacity means this process is already tracking
	// MaxTrackedSenders senders and will not drop one to make room.
	ReplaySenderCapacity
)

func (d ReplayDecision) String() string {
	switch d {
	case ReplayAccepted:
		return "accepted"
	case ReplayRefused:
		return "replay"
	case ReplaySenderCapacity:
		return "replay_window_full"
	}
	return "unknown"
}

// SequenceTracker is the per-sender sliding window. It is not safe for
// concurrent use; ReplayWindow wraps it for the callers that share one.
//
// The one rule is that the same (sender, seq) is claimed at most once, and
// **a claim is final**. The caller claims after authentication and never gives
// a claim back, not even when capacity then refuses the command, so an
// envelope that was refused and sent again is a replay
// (`CloudEnvelope.swift:372-373`, `CloudTransport.swift:2449-2450`).
//
// A strictly-increasing counter stood here once and refused the lower-numbered
// tab whenever one device had two of them open, because each tab reserves its
// own block of sequences (`CloudEnvelope.swift:369-372`). The window is what
// makes two live producers on one device legal.
type SequenceTracker struct {
	windows       map[string]*replayWindow
	maximumSender int
}

type replayWindow struct {
	highest uint64
	// seen bit i records that highest-1-i was claimed.
	seen [replayWords]uint64
}

// NewSequenceTracker returns a tracker holding at most maximum senders.
// A maximum of zero or less means MaxTrackedSenders.
func NewSequenceTracker(maximum int) *SequenceTracker {
	if maximum <= 0 {
		maximum = MaxTrackedSenders
	}
	return &SequenceTracker{windows: map[string]*replayWindow{}, maximumSender: maximum}
}

// Claim decides and records in one step.
func (t *SequenceTracker) Claim(sender string, sequence uint64) ReplayDecision {
	window, ok := t.windows[sender]
	if !ok {
		if len(t.windows) >= t.maximumSender {
			return ReplaySenderCapacity
		}
		t.windows[sender] = &replayWindow{highest: sequence}
		return ReplayAccepted
	}
	if sequence > window.highest {
		window.advance(sequence - window.highest)
		window.highest = sequence
		return ReplayAccepted
	}
	offset := window.highest - sequence
	// offset 0 is the highest itself, which was claimed when it became the
	// highest. Past ReplayWindowSize the answer has fallen off the bottom.
	if offset < 1 || offset > ReplayWindowSize {
		return ReplayRefused
	}
	bit := offset - 1
	word, mask := int(bit/64), uint64(1)<<(bit%64)
	if window.seen[word]&mask != 0 {
		return ReplayRefused
	}
	window.seen[word] |= mask
	return ReplayAccepted
}

// Highest is the highest sequence claimed from sender, and whether that sender
// is tracked at all.
func (t *SequenceTracker) Highest(sender string) (uint64, bool) {
	window, ok := t.windows[sender]
	if !ok {
		return 0, false
	}
	return window.highest, true
}

// TrackedSenders is how many windows are held.
func (t *SequenceTracker) TrackedSenders() int { return len(t.windows) }

// advance moves every recorded position distance further from the new highest
// and records the old highest, which now sits at offset distance. Positions
// past the window fall off. `CloudEnvelope.swift:437-458`.
func (w *replayWindow) advance(distance uint64) {
	if distance >= ReplayWindowSize {
		w.seen = [replayWords]uint64{}
	} else {
		wordShift := int(distance / 64)
		bitShift := distance % 64
		for index := replayWords - 1; index >= 0; index-- {
			source := index - wordShift
			var value uint64
			if source >= 0 {
				value = w.seen[source] << bitShift
				if bitShift > 0 && source-1 >= 0 {
					value |= w.seen[source-1] >> (64 - bitShift)
				}
			}
			w.seen[index] = value
		}
	}
	oldHighestBit := distance - 1
	if oldHighestBit >= ReplayWindowSize {
		return
	}
	w.seen[oldHighestBit/64] |= uint64(1) << (oldHighestBit % 64)
}

// ReplayWindow is the process-lifetime owner of inbound replay state.
//
// It is shared across every transport this process builds, because a transport
// rebuilt after a sign-out, a retry or an identity change would otherwise start
// from an empty window and accept an envelope the previous one had already
// accepted (`CloudEnvelope.swift:460-465`). Tests build their own so that
// unrelated fixtures cannot see each other's sequences.
type ReplayWindow struct {
	mu      sync.Mutex
	tracker *SequenceTracker
}

// NewReplayWindow returns a window tracking at most maximum senders.
func NewReplayWindow(maximum int) *ReplayWindow {
	return &ReplayWindow{tracker: NewSequenceTracker(maximum)}
}

// Claim decides and records inside one critical section, so two transports
// alive during a replacement cannot both accept the same envelope.
func (w *ReplayWindow) Claim(sender string, sequence uint64) ReplayDecision {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tracker.Claim(sender, sequence)
}

// Highest is the highest sequence claimed from sender.
func (w *ReplayWindow) Highest(sender string) (uint64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tracker.Highest(sender)
}

// TrackedSenders is how many windows are held.
func (w *ReplayWindow) TrackedSenders() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tracker.TrackedSenders()
}
