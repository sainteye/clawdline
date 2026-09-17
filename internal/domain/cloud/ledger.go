package cloud

// The command ledger: the second layer of duplicate suppression, after the
// replay window in replay.go.
//
// The two are not the same job and neither replaces the other. The replay
// window answers "have I seen these *bytes* before" and is keyed by
// (sender, seq); it runs on every envelope, at authentication time. The ledger
// answers "have I already *done* this command", is keyed by
// (viewer sender, request_id), and survives the answer being lost on the way
// back — a viewer that never saw the response sends the same request_id again
// and must be handed the same outcome, not a second execution.
//
// Ported from `Sources/CloudCommandLedger.swift:330-470`. Three of its
// constants are decisions rather than tuning, spelled out there as
// `checksDeadlineBeforeLookup`, `permitsEvictionAtCapacity` and
// `retriesRecoveredInProgress` (:305-307) precisely so they can be read and
// mutation-tested; they are carried over with the same answers.
//
// What this wave does **not** carry over: the durable store behind the rows
// (`CloudDurableStores.swift`) and the metrics snapshot. This ledger is
// in-memory, which means a restart forgets every outcome and a repeated
// request_id executes again. That is a real gap, written down in
// docs/cloud-wire.md §15, not an oversight.

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// LedgerState is where a command has got to.
type LedgerState string

const (
	// LedgerReserved is admitted, effect not started. A reserved row is
	// dropped when the process restarts: nothing happened, so nothing is owed.
	LedgerReserved LedgerState = "reserved"
	// LedgerInProgress is past the point of no return. A row found in this
	// state after a restart is **not** retried — this process can no longer
	// prove whether the effect happened, and repeating it is the one answer
	// that can be wrong twice.
	LedgerInProgress LedgerState = "in_progress"
	// LedgerCompleted carries the outcome every later duplicate is handed.
	LedgerCompleted LedgerState = "completed"
)

// Ledger bounds, `CloudCommandLedger.swift:295-302`.
const (
	// LedgerRetention is how long a completed outcome is kept and answered
	// with. 24 hours.
	LedgerRetention = 24 * time.Hour
	// LedgerGlobalHardLimit is the row ceiling for the whole ledger.
	LedgerGlobalHardLimit = 10000
	// LedgerFairnessReserveStart is where the last 1,000 rows become a reserve
	// that one loud viewer may not take all of.
	LedgerFairnessReserveStart = 9000
	// LedgerNormalActorLimit is one viewer's ordinary ceiling.
	LedgerNormalActorLimit = 1000
	// LedgerFairnessActorLimit is one viewer's ceiling once the reserve has
	// been reached.
	LedgerFairnessActorLimit = 100
	// LedgerGCBatchLimit bounds how much expiry work one admission pays for,
	// so a large ledger cannot turn one command into an unbounded pause.
	LedgerGCBatchLimit = 100
)

// Ledger errors. Each is typed because each is a different thing for the
// viewer to do: conflict means "you reused an id with different bytes",
// outcome unknown means "do not send that again", capacity means "later".
var (
	ErrLedgerConflict     = errors.New("that request id was used for different bytes")
	ErrLedgerOutcome      = errors.New("the outcome of that request is no longer knowable")
	ErrLedgerDeadline     = errors.New("the command's deadline has passed")
	ErrLedgerCapacity     = errors.New("the command ledger is full")
	ErrLedgerCorrupt      = errors.New("the ledger row is not readable")
	ErrLedgerTransition   = errors.New("not a legal ledger transition")
	ErrLedgerNotFound     = errors.New("no such ledger row")
	ErrLedgerEpochUnknown = errors.New("the clock is not trusted enough to admit a command")
)

// LedgerKey identifies one command. The viewer's sender id is part of it: two
// viewers may pick the same request_id and they are not the same command.
type LedgerKey struct {
	ViewerSender string
	RequestID    string
}

func (k LedgerKey) String() string { return k.ViewerSender + "|" + k.RequestID }

// TransportIdempotencyKey is the transport's own spelling for an inbound
// envelope, `CloudTransport.swift:618-620`. It is a *different* key from
// LedgerKey: it identifies bytes on the wire, and the ledger identifies a
// command. Both exist because a viewer may legitimately re-send the same
// command under a new sequence, and may never re-send the same sequence.
func TransportIdempotencyKey(sender string, sequence uint64) string {
	return fmt.Sprintf("cloud:%s:%d", sender, sequence)
}

// LedgerOutcome is the answer a completed command is remembered by. Status is
// the HTTP-shaped number the viewer is given; Payload is the sealed response
// body, or nil when there was none.
type LedgerOutcome struct {
	Code    string
	Status  int
	Payload []byte
}

// Ledger outcome codes, `CloudCommandLedger.swift:29-45`.
const (
	OutcomeSucceeded   = "succeeded"
	OutcomeFailed      = "failed"
	OutcomeInternal    = "internal"
	OutcomeBusy        = "busy"
	OutcomeRateLimited = "rate_limited"
	OutcomeUnavailable = "unavailable"
)

// DefaultOutcomeStatus is the status a code carries when the caller does not
// name one.
func DefaultOutcomeStatus(code string) int {
	switch code {
	case OutcomeSucceeded:
		return 200
	case OutcomeFailed:
		return 400
	case OutcomeInternal:
		return 500
	case OutcomeBusy, OutcomeRateLimited:
		return 429
	case OutcomeUnavailable:
		return 503
	}
	return 500
}

// LedgerRequest is one admission attempt.
type LedgerRequest struct {
	Key LedgerKey
	// RequestSHA256 pins the bytes this id was used for. A second request
	// with the same id and different bytes is a conflict, not a duplicate.
	RequestSHA256 [32]byte
	// ReplyKeyID and RecipientDevice are what the answer will be sealed for.
	// The reply *key* itself is deliberately never stored: it is authenticated
	// plaintext that lives no longer than the command
	// (`CloudCommandLedger.swift:125`).
	ReplyKeyID      string
	RecipientDevice string
	// DeadlineAt is the command's own deadline, from its payload.
	DeadlineAt time.Time
}

type ledgerRow struct {
	key             LedgerKey
	requestSHA256   [32]byte
	replyKeyID      string
	recipientDevice string
	deadlineAt      time.Time
	state           LedgerState
	outcome         *LedgerOutcome
	createdAt       time.Time
	expiresAt       time.Time
}

// Admission is what Reserve answered.
type Admission struct {
	// Reserved is true when this caller now owns the effect.
	Reserved bool
	// Cached carries the remembered outcome when this is a duplicate of a
	// command that already finished.
	Cached *LedgerOutcome
}

// Ledger is the in-memory command ledger.
//
// Its lock covers admission and completion together. Two envelopes carrying
// the same request_id that arrive at once must not both be admitted, and the
// window in which that could happen is exactly the gap between deciding and
// recording.
type Ledger struct {
	mu   sync.Mutex
	wake *sync.Cond
	rows map[LedgerKey]*ledgerRow
	now  func() time.Time
}

// NewLedger returns an empty ledger. now may be nil, meaning time.Now.
func NewLedger(now func() time.Time) *Ledger {
	if now == nil {
		now = time.Now
	}
	l := &Ledger{rows: map[LedgerKey]*ledgerRow{}, now: now}
	l.wake = sync.NewCond(&l.mu)
	return l
}

// Reserve admits a command, or answers with the outcome a duplicate is owed.
//
// The deadline is checked **before** the row is looked up, so an expired
// command is refused whether or not it has been seen before
// (`checksDeadlineBeforeLookup`, :305). Doing it the other way round would let
// an expired duplicate collect a cached answer and would make expiry depend on
// history.
//
// A caller that finds the command already in flight in this process blocks
// until the owner finishes, and is then handed the same outcome. That is the
// whole point: the second copy of a "start a session" command must not start a
// second session.
func (l *Ledger) Reserve(request LedgerRequest, clockReady bool) (Admission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		now := l.now()
		if !now.Before(request.DeadlineAt) {
			return Admission{}, ErrLedgerDeadline
		}
		if !clockReady {
			// An uncertain clock cannot tell a fresh command from a replayed
			// one whose deadline this machine merely believes is in the
			// future. docs/cloud-wire.md §6.2.
			return Admission{}, ErrLedgerEpochUnknown
		}

		existing, found := l.rows[request.Key]
		if found {
			if existing.requestSHA256 != request.RequestSHA256 {
				return Admission{}, ErrLedgerConflict
			}
			switch existing.state {
			case LedgerReserved, LedgerInProgress:
				// Someone in this process owns it. Wait for their answer
				// rather than doing it twice.
				l.wake.Wait()
				continue
			case LedgerCompleted:
				if existing.outcome == nil {
					return Admission{}, ErrLedgerCorrupt
				}
				cached := *existing.outcome
				return Admission{Cached: &cached}, nil
			default:
				return Admission{}, ErrLedgerCorrupt
			}
		}

		// Expiry, the capacity counts and the insert are one step. Collecting
		// separately would leave a window in which the counts below are read
		// from a ledger that a concurrent admission has already changed.
		l.collectExpired(now, LedgerGCBatchLimit)

		actorRows := 0
		for _, row := range l.rows {
			if row.key.ViewerSender == request.Key.ViewerSender {
				actorRows++
			}
		}
		if actorRows >= LedgerNormalActorLimit {
			return Admission{}, fmt.Errorf("%w: this viewer holds %d rows", ErrLedgerCapacity, actorRows)
		}
		if len(l.rows) >= LedgerGlobalHardLimit {
			// Nothing is evicted to make room (`permitsEvictionAtCapacity`,
			// :306). An evicted row is an outcome this machine promised to
			// remember and then forgot, and the viewer finds out by having its
			// command executed a second time.
			return Admission{}, fmt.Errorf("%w: %d rows", ErrLedgerCapacity, len(l.rows))
		}
		if len(l.rows) >= LedgerFairnessReserveStart && actorRows >= LedgerFairnessActorLimit {
			return Admission{}, fmt.Errorf("%w: the reserve is for other viewers", ErrLedgerCapacity)
		}

		l.rows[request.Key] = &ledgerRow{
			key:             request.Key,
			requestSHA256:   request.RequestSHA256,
			replyKeyID:      request.ReplyKeyID,
			recipientDevice: request.RecipientDevice,
			deadlineAt:      request.DeadlineAt,
			state:           LedgerReserved,
			createdAt:       now,
			expiresAt:       now.Add(LedgerRetention),
		}
		return Admission{Reserved: true}, nil
	}
}

// Begin moves a reserved row past the point of no return. After this the
// command's outcome must be recorded, because a restart can no longer decide
// whether the effect happened.
func (l *Ledger) Begin(key LedgerKey) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[key]
	if !ok {
		return ErrLedgerNotFound
	}
	if row.state != LedgerReserved {
		return fmt.Errorf("%w: %s is %s", ErrLedgerTransition, key, row.state)
	}
	row.state = LedgerInProgress
	return nil
}

// Complete records the outcome and wakes anybody waiting on it.
func (l *Ledger) Complete(key LedgerKey, outcome LedgerOutcome) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[key]
	if !ok {
		return ErrLedgerNotFound
	}
	if row.state == LedgerCompleted {
		return fmt.Errorf("%w: %s is already completed", ErrLedgerTransition, key)
	}
	if outcome.Status == 0 {
		outcome.Status = DefaultOutcomeStatus(outcome.Code)
	}
	row.state = LedgerCompleted
	row.outcome = &outcome
	l.wake.Broadcast()
	return nil
}

// Release drops a reserved row that never started. It is the answer for a
// command refused *before* any effect — capacity, a closed queue — where
// leaving the row would make the viewer's honest retry a conflict.
func (l *Ledger) Release(key LedgerKey) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[key]
	if !ok {
		return ErrLedgerNotFound
	}
	if row.state != LedgerReserved {
		return fmt.Errorf("%w: %s is %s", ErrLedgerTransition, key, row.state)
	}
	delete(l.rows, key)
	l.wake.Broadcast()
	return nil
}

// Recover is what a restart does: reserved rows are dropped because nothing
// happened under them, and in-progress rows are kept so that a repeat of that
// request is refused with ErrLedgerOutcome rather than executed again
// (`retriesRecoveredInProgress = false`, :307). It answers how many of each.
func (l *Ledger) Recover() (dropped, kept int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, row := range l.rows {
		switch row.state {
		case LedgerReserved:
			delete(l.rows, key)
			dropped++
		case LedgerInProgress:
			kept++
		}
	}
	return dropped, kept
}

// Rows is how many rows are held.
func (l *Ledger) Rows() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.rows)
}

// State answers a row's state, and whether there is one.
func (l *Ledger) State(key LedgerKey) (LedgerState, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[key]
	if !ok {
		return "", false
	}
	return row.state, true
}

// collectExpired removes at most limit rows that are past their retention.
// The caller holds the lock.
func (l *Ledger) collectExpired(now time.Time, limit int) int {
	removed := 0
	for key, row := range l.rows {
		if removed >= limit {
			break
		}
		if row.state == LedgerInProgress {
			// An unfinished effect is never collected; its row is the only
			// record that it may have happened.
			continue
		}
		if !now.Before(row.expiresAt) {
			delete(l.rows, key)
			removed++
		}
	}
	return removed
}
