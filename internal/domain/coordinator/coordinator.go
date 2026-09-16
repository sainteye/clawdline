// Package coordinator is the machine-wide role: one explicitly registered
// session that explains other people's receipts and authors none of them.
package coordinator

import (
	"fmt"
	"time"
)

// Record is the one durable binding. It is a separate identity ledger rather
// than a flag on a session, because the role outlives any particular process
// and must never be inferred from a label or from whoever was busiest.
type Record struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	SessionID      string    `json:"sessionId"`
	ConversationID string    `json:"conversationId"`
	Assistant      string    `json:"assistant"`
	PID            int       `json:"pid"`
	RegisteredAt   time.Time `json:"registeredAt"`
	ReboundAt      time.Time `json:"reboundAt,omitempty"`
	// Generation increments on every rebind, so a stale caller holding an old
	// view cannot move a role that has already moved.
	Generation int64 `json:"generation"`
}

// Liveness is what a current inventory says about the bound process.
//
// `Unknown` is a first-class answer and is never treated as absence. "Not in
// this reading" is also true of a terminal that lost a permission for a moment,
// and converting that into proof of death is how a live coordinator gets
// replaced underneath itself.
type Liveness string

const (
	Online  Liveness = "online"
	Offline Liveness = "offline"
	Unknown Liveness = "unknown"
)

type Refusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

// Register creates the binding.
//
// Registration is never a takeover. An existing record keeps its place even
// while liveness is degraded, because the alternative — letting a new claimant
// in whenever the old one cannot be seen — makes the role migrate on a bad
// reading rather than on a decision.
func Register(existing *Record, want Record, live Liveness) (Record, error) {
	if want.SessionID == "" || want.ConversationID == "" {
		return Record{}, Refusal{"bad_request",
			"registering the role needs both the session and its conversation id"}
	}
	if existing != nil {
		if existing.ConversationID == want.ConversationID {
			// The same session registering twice is idempotent, not a conflict.
			return *existing, nil
		}
		return Record{}, Refusal{"coordinator_exists",
			fmt.Sprintf("%s already holds the role; registration is never a takeover", existing.Label)}
	}
	_ = live
	want.RegisteredAt = time.Now()
	want.Generation = 1
	return want, nil
}

// Rebind moves the role to one exact live process after a complete inventory
// proves the old one absent.
//
// This is reconnect, not takeover: it is how a role survives a terminal
// restarting, and it refuses everything that is not that. The compare-and-swap
// is closed over three fields — the record's id, the generation the caller
// read, and exactly one live session — so a caller working from a stale view
// cannot move a role that has already moved.
func Rebind(existing *Record, expectID string, expectGeneration int64,
	liveCandidates []string, oldLive Liveness) (Record, error) {

	if existing == nil {
		return Record{}, Refusal{"coordinator_not_registered", "there is no role to move"}
	}
	if existing.ID != expectID || existing.Generation != expectGeneration {
		return Record{}, Refusal{"coordinator_identity_stale",
			"the role moved since you read it"}
	}
	switch oldLive {
	case Online:
		return Record{}, Refusal{"coordinator_online",
			"the bound session is alive; a rebind is reconnect, not replacement"}
	case Unknown:
		// The app refuses to convert absence of evidence into proof of death.
		return Record{}, Refusal{"coordinator_liveness_unknown",
			"the bound session could not be read, which is not the same as gone"}
	}
	switch len(liveCandidates) {
	case 0:
		return Record{}, Refusal{"coordinator_receiver_missing",
			"no live session to move the role to"}
	case 1:
	default:
		return Record{}, Refusal{"coordinator_receiver_ambiguous",
			fmt.Sprintf("%d live candidates; the role moves to one exact process", len(liveCandidates))}
	}

	next := *existing
	next.SessionID = liveCandidates[0]
	next.Generation = existing.Generation + 1
	next.ReboundAt = time.Now()
	return next, nil
}
