package cloud

import (
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"
)

func ledgerRequest(sender, id, body string, deadline time.Time) LedgerRequest {
	return LedgerRequest{
		Key:             LedgerKey{ViewerSender: sender, RequestID: id},
		RequestSHA256:   sha256.Sum256([]byte(body)),
		ReplyKeyID:      "rk-zF3jN8rQ4Wm2pV6sT0uYxA",
		RecipientDevice: sender,
		DeadlineAt:      deadline,
	}
}

// A repeat of a finished command is handed the outcome, not a second run.
// This is the whole reason the ledger exists: the viewer that never saw the
// answer sends the same request_id again.
func TestACompletedCommandIsReplayedNotRerun(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	request := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))

	admission, err := ledger.Reserve(request, true)
	if err != nil || !admission.Reserved {
		t.Fatalf("first reserve: %+v %v", admission, err)
	}
	if err := ledger.Begin(request.Key); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.Complete(request.Key, LedgerOutcome{Code: OutcomeSucceeded, Payload: []byte(`{"accepted":true}`)}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	admission, err = ledger.Reserve(request, true)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if admission.Reserved {
		t.Fatal("the duplicate was admitted; it must be answered from the record")
	}
	if admission.Cached == nil || admission.Cached.Code != OutcomeSucceeded || admission.Cached.Status != 200 {
		t.Fatalf("cached outcome: %+v", admission.Cached)
	}
}

// The same id with different bytes is a conflict, not a duplicate. Answering
// it from the record would hand a viewer the answer to a question it did not
// ask.
func TestTheSameIDWithDifferentBytesIsAConflict(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	first := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))
	if _, err := ledger.Reserve(first, true); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := ledgerRequest("viewer-1", "req-1", "goodbye", now.Add(time.Minute))
	if _, err := ledger.Reserve(second, true); !errors.Is(err, ErrLedgerConflict) {
		t.Fatalf("conflict: %v", err)
	}
}

// The deadline is checked before the row is looked up, so an expired command
// is refused whether or not it has been seen before.
func TestTheDeadlineIsCheckedBeforeTheRecord(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	clock := now
	ledger := NewLedger(func() time.Time { return clock })
	request := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))
	if _, err := ledger.Reserve(request, true); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := ledger.Begin(request.Key); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.Complete(request.Key, LedgerOutcome{Code: OutcomeSucceeded}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	clock = now.Add(2 * time.Minute)
	// A *completed* row is present, so a lookup-first ledger would answer the
	// cached outcome. Expiry does not depend on history.
	if _, err := ledger.Reserve(request, true); !errors.Is(err, ErrLedgerDeadline) {
		t.Fatalf("expired duplicate: %v, want a deadline refusal", err)
	}
}

// An uncertain clock cannot tell a fresh command from a replayed one whose
// deadline this machine merely believes is in the future.
func TestAnUncertainClockAdmitsNothing(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	request := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))
	if _, err := ledger.Reserve(request, false); !errors.Is(err, ErrLedgerEpochUnknown) {
		t.Fatalf("uncertain clock: %v", err)
	}
}

// A restart drops reserved rows — nothing happened under them — and keeps
// in-progress ones, whose effect this process can no longer prove either way.
func TestRecoveryDropsReservationsAndKeepsEffects(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	reserved := ledgerRequest("viewer-1", "reserved", "a", now.Add(time.Minute))
	started := ledgerRequest("viewer-1", "started", "b", now.Add(time.Minute))
	if _, err := ledger.Reserve(reserved, true); err != nil {
		t.Fatalf("reserve a: %v", err)
	}
	if _, err := ledger.Reserve(started, true); err != nil {
		t.Fatalf("reserve b: %v", err)
	}
	if err := ledger.Begin(started.Key); err != nil {
		t.Fatalf("begin b: %v", err)
	}
	dropped, kept := ledger.Recover()
	if dropped != 1 || kept != 1 {
		t.Fatalf("recovery dropped %d kept %d, want 1 and 1", dropped, kept)
	}
	if _, ok := ledger.State(reserved.Key); ok {
		t.Fatal("the reservation survived recovery")
	}
	if state, ok := ledger.State(started.Key); !ok || state != LedgerInProgress {
		t.Fatalf("the started row is %s %v", state, ok)
	}
}

// Release is for a refusal *before* any effect. Leaving the row would make
// the viewer's honest retry a conflict.
func TestReleaseFreesAnUnstartedReservation(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	request := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))
	if _, err := ledger.Reserve(request, true); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := ledger.Release(request.Key); err != nil {
		t.Fatalf("release: %v", err)
	}
	admission, err := ledger.Reserve(request, true)
	if err != nil || !admission.Reserved {
		t.Fatalf("re-reserve after release: %+v %v", admission, err)
	}
	// A started row may not be released; its effect may have happened.
	if err := ledger.Begin(request.Key); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.Release(request.Key); !errors.Is(err, ErrLedgerTransition) {
		t.Fatalf("release of a started row: %v", err)
	}
}

// One viewer may not fill the ledger, and nothing is evicted to make room for
// it either.
func TestOneViewerCannotFillTheLedger(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	for i := 0; i < LedgerNormalActorLimit; i++ {
		request := ledgerRequest("loud", string(rune('a'+i%26))+itoaTest(i), "x", now.Add(time.Minute))
		if _, err := ledger.Reserve(request, true); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	overflow := ledgerRequest("loud", "one-too-many", "x", now.Add(time.Minute))
	if _, err := ledger.Reserve(overflow, true); !errors.Is(err, ErrLedgerCapacity) {
		t.Fatalf("overflow: %v, want a capacity refusal", err)
	}
	if ledger.Rows() != LedgerNormalActorLimit {
		t.Fatalf("rows: %d — something was evicted", ledger.Rows())
	}
	// Another viewer is unaffected: the limit is per actor.
	quiet := ledgerRequest("quiet", "req-1", "x", now.Add(time.Minute))
	if _, err := ledger.Reserve(quiet, true); err != nil {
		t.Fatalf("another viewer: %v", err)
	}
}

// A second copy of a command in flight waits for the first one's answer
// rather than starting a second session.
func TestADuplicateInFlightWaitsForTheOneOutcome(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	ledger := NewLedger(func() time.Time { return now })
	request := ledgerRequest("viewer-1", "req-1", "hello", now.Add(time.Minute))
	if _, err := ledger.Reserve(request, true); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := ledger.Begin(request.Key); err != nil {
		t.Fatalf("begin: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var admission Admission
	var admissionErr error
	go func() {
		defer wg.Done()
		admission, admissionErr = ledger.Reserve(request, true)
	}()

	// Give the duplicate time to park, then finish the first one.
	time.Sleep(50 * time.Millisecond)
	if err := ledger.Complete(request.Key, LedgerOutcome{Code: OutcomeSucceeded}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	wg.Wait()
	if admissionErr != nil {
		t.Fatalf("the waiting duplicate: %v", admissionErr)
	}
	if admission.Reserved || admission.Cached == nil {
		t.Fatalf("the waiting duplicate was admitted: %+v", admission)
	}
}

// The transport's key and the ledger's key are different things and the
// spelling of the first one is on the wire.
func TestTheTransportIdempotencyKeyIsTheSwiftSpelling(t *testing.T) {
	t.Parallel()
	if got := TransportIdempotencyKey("viewer-device-01", 411); got != "cloud:viewer-device-01:411" {
		t.Fatalf("idempotency key: %q", got)
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
