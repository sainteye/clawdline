package cloud

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testSpool(t *testing.T, now *time.Time) *Spool {
	t.Helper()
	spool, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, func() time.Time { return *now })
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	return spool
}

func sealRow(t *testing.T, spool *Spool, seq uint64, at time.Time) {
	t.Helper()
	if err := spool.Seal(seq, []byte(`{"ct":"x"}`), at); err != nil {
		t.Fatalf("seal %d: %v", seq, err)
	}
}

// Writes go out in global sequence order, not per channel, and a reservation
// nobody has sealed stops everything below it. Skipping it would leave a hole
// the receiver's replay window can never fill.
func TestAnUnsealedReservationBlocksTheQueue(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)

	first, _ := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req-1", 10)
	second, _ := spool.Reserve(SpoolChannelT, "t/mac/session", "session", 10)
	sealRow(t, spool, second, now)

	disposition := spool.SendNext()
	if !disposition.BlockedOnSeal || disposition.HeadSeq != first {
		t.Fatalf("disposition: %+v, want blocked on seq %d", disposition, first)
	}
	sealRow(t, spool, first, now)
	disposition = spool.SendNext()
	if disposition.Row == nil || disposition.Row.Seq != first {
		t.Fatalf("disposition: %+v, want seq %d", disposition, first)
	}
}

// A newer snapshot on the same channel and recipient replaces an older
// never-sent one. Keeping both would only delay the newer one behind a value
// nobody wants.
func TestASnapshotCoalescesWithTheOlderOne(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)

	older, _ := spool.ReserveLatestValue(SpoolChannelS, "s/mac/session-1", "session-1", 10)
	sealRow(t, spool, older, now)
	newer, err := spool.ReserveLatestValue(SpoolChannelS, "s/mac/session-1", "session-1", 10)
	if err != nil {
		t.Fatalf("second reservation: %v", err)
	}
	sealRow(t, spool, newer, now)

	if _, ok := spool.Row(older); ok {
		t.Fatal("the older snapshot survived coalescing")
	}
	// The sequence is **not** reused: the replacement takes a new, higher one.
	if newer <= older {
		t.Fatalf("the replacement took sequence %d, the original had %d", newer, older)
	}
	disposition := spool.SendNext()
	if disposition.Row == nil || disposition.Row.Seq != newer {
		t.Fatalf("disposition: %+v", disposition)
	}
}

// A command channel never coalesces: dropping an older command loses an
// instruction, which a snapshot cannot.
func TestACommandChannelNeverCoalesces(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	if _, err := spool.ReserveLatestValue(SpoolChannelCtlr, "ctlr/mac/viewer", "req-1", 10); err == nil {
		t.Fatal("ctlr was allowed to coalesce")
	}
	first, _ := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req-1", 10)
	second, _ := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req-2", 10)
	sealRow(t, spool, first, now)
	sealRow(t, spool, second, now)
	if spool.SendNext().Row.Seq != first {
		t.Fatal("the first command was not first")
	}
	if spool.SendNext().Row.Seq != second {
		t.Fatal("the second command was dropped")
	}
}

// A row is `sent` before the write, and a receipt settles it exactly once.
// Every later receipt for the same sequence is counted and ignored, not an
// error and not a resurrection.
func TestASecondReceiptIsLateNotUnknown(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	seq, _ := spool.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	sealRow(t, spool, seq, now)

	disposition := spool.SendNext()
	if disposition.Row == nil || disposition.Row.State != SpoolSent {
		t.Fatalf("the row was not marked sent before the write: %+v", disposition.Row)
	}
	result, err := spool.Settle(seq, "orch/mac", SettleDelivered)
	if err != nil || result != SettleResultAcked {
		t.Fatalf("first settle: %v %v", result, err)
	}
	result, err = spool.Settle(seq, "orch/mac", SettleDelivered)
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}
	if result != SettleResultLateIgnored {
		t.Fatalf("second settle: %v, want late_ignored", result)
	}
}

// A receipt naming a different channel than the row is a correlation failure,
// not a late ack. Accepting it would let one channel's receipts settle
// another's rows.
func TestAReceiptForAnotherChannelIsRefused(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	seq, _ := spool.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	sealRow(t, spool, seq, now)
	spool.SendNext()
	if _, err := spool.Settle(seq, "orch/another-mac", SettleDelivered); !errors.Is(err, ErrSpoolMismatch) {
		t.Fatalf("settle across channels: %v", err)
	}
}

// A `sent` row that nobody answers is burned as *uncertain* once the attempt
// window passes — not as failed. Nobody knows whether those bytes arrived.
func TestAnUnansweredRowBurnsAsUncertain(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	seq, _ := spool.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	sealRow(t, spool, seq, now)
	spool.SendNext()

	now = now.Add(31 * time.Second)
	burned := spool.BurnExpired()
	if len(burned) != 1 || burned[0] != seq {
		t.Fatalf("burned: %v", burned)
	}
	row, ok := spool.Row(seq)
	if !ok || row.State != SpoolBurned || row.Outcome != AttemptTransportUncertain {
		t.Fatalf("row: %+v", row)
	}
}

// Nothing is evicted to make room. A live row is somebody's unsent
// instruction, and dropping it to admit a newer one is a silent loss.
func TestCapacityRefusesRatherThanEvicts(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.GlobalRowCap = 3
	limits.RecipientRowCap = 3
	// High enough that the fairness reserve is not what refuses here; this
	// test is about the hard cap, and the reserve has a test of its own.
	limits.FairnessRecipientRowCap = 100
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req", 10); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req", 10); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("the fourth row: %v, want a capacity refusal", err)
	}
	if spool.Rows() != 3 {
		t.Fatalf("rows: %d — something was evicted", spool.Rows())
	}
}

// Once the spool is 90% full the rest is a reserve, and one loud recipient
// may not take all of it — so a recipient inside its own hard cap is still
// refused, and a quieter one still gets in.
func TestTheReserveIsForOtherRecipients(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.GlobalRowCap = 10
	limits.RecipientRowCap = 100
	limits.FairnessRecipientRowCap = 5
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	// Nine rows: (9*10+9)/10 = 9, so the reserve is open at the tenth.
	for i := 0; i < 9; i++ {
		if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/loud", "req", 10); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/loud", "req", 10); !errors.Is(err, ErrSpoolFairness) {
		t.Fatalf("the loud recipient past the reserve: %v, want a fairness refusal", err)
	}
	if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/quiet", "req", 10); err != nil {
		t.Fatalf("the quiet recipient was refused the reserve: %v", err)
	}
}

// A frame sealed longer ago than the freshness bound is burned rather than
// sent: the relay would measure its `ts` against its own clock and refuse it.
func TestAStaleFrameIsBurnedBeforeItIsSent(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	seq, _ := spool.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	sealRow(t, spool, seq, now)

	now = now.Add(241 * time.Second)
	disposition := spool.SendNext()
	if disposition.Row != nil {
		t.Fatalf("a stale frame was sent: %+v", disposition.Row)
	}
	row, ok := spool.Row(seq)
	if !ok || row.State != SpoolBurned || row.BurnReason != BurnStaleReadyAtSend {
		t.Fatalf("row: %+v", row)
	}
}

// The in-flight window bounds how much may be waiting for a receipt at once.
func TestTheInFlightWindowBoundsWhatIsWaiting(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.OutboundWindowRowCap = 2
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	for i := 0; i < 3; i++ {
		seq, _ := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req", 10)
		sealRow(t, spool, seq, now)
	}
	if spool.SendNext().Row == nil || spool.SendNext().Row == nil {
		t.Fatal("the first two rows did not go")
	}
	disposition := spool.SendNext()
	if disposition.Row != nil || !disposition.BlockedOnAck {
		t.Fatalf("the third row went past the window: %+v", disposition)
	}
	if _, err := spool.Settle(0, "ctlr/mac/viewer", SettleDelivered); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if spool.SendNext().Row == nil {
		t.Fatal("the window did not reopen after a receipt")
	}
}

// The fence is why a restart does not start again at zero. A viewer's replay
// window has already claimed the sequences the previous run used.
func TestTheFenceSurvivesARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fence := NewFileFence(dir, "mac_test")
	first, err := NewSpool(DefaultSpoolLimits(), fence, time.Now)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := first.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10); err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}

	// A new process, a new spool, the same fence file.
	second, err := NewSpool(DefaultSpoolLimits(), NewFileFence(dir, "mac_test"), time.Now)
	if err != nil {
		t.Fatalf("second spool: %v", err)
	}
	seq, err := second.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	if err != nil {
		t.Fatalf("reserve after restart: %v", err)
	}
	if seq < 3 {
		t.Fatalf("the restart handed out sequence %d, which the previous run already used", seq)
	}
	if _, err := filepath.Abs(filepath.Join(dir, SequenceFenceFile)); err != nil {
		t.Fatalf("fence path: %v", err)
	}
}

// Another sender's ceiling in the same file is not disturbed. That file may
// hold a previous identity's high-water mark, and overwriting it would
// un-fence that sender if it ever came back.
func TestTheFenceKeepsOtherSendersCeilings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one := NewFileFence(dir, "mac_one")
	two := NewFileFence(dir, "mac_two")
	if err := one.Reserve(500); err != nil {
		t.Fatalf("one: %v", err)
	}
	if err := two.Reserve(10); err != nil {
		t.Fatalf("two: %v", err)
	}
	ceiling, err := NewFileFence(dir, "mac_one").Ceiling()
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if ceiling < 500 {
		t.Fatalf("mac_one's ceiling is %d after mac_two wrote", ceiling)
	}
}
