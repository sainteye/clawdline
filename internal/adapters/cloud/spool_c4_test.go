package cloud

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The spool's two global caps are the register's rows, spelled once.
func TestTheSpoolsCapsAreTheRegisters(t *testing.T) {
	limits := DefaultSpoolLimits()
	if int64(limits.GlobalRowCap) != capacity.Default(capacity.CloudSpool) ||
		int64(limits.GlobalByteCap) != capacity.Default(capacity.CloudSpoolBytes) {
		t.Fatalf("spool caps %d rows, %d bytes; the register says %d, %d", limits.GlobalRowCap, limits.GlobalByteCap,
			capacity.Default(capacity.CloudSpool), capacity.Default(capacity.CloudSpoolBytes))
	}
}

// A refusal at the cap used to reach only the log line of whoever called
// Publish (limits N22). Now it is counted where the register reads it, and the
// reading is what the refusal was measured against.
func TestTheSpoolCountsWhatItRefuses(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.GlobalRowCap, limits.RecipientRowCap, limits.FairnessRecipientRowCap = 3, 3, 100
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	rows, bytes := spool.Readings()
	if !rows.Known || rows.Used != 0 || bytes.Used != 0 || rows.Counters.Refused != 0 {
		t.Fatalf("an empty spool: %+v %+v", rows, bytes)
	}
	for i := 0; i < 3; i++ {
		if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req", 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := spool.Reserve(SpoolChannelCtlr, "ctlr/mac/viewer", "req", 10); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("the fourth: %v", err)
	}
	rows, bytes = spool.Readings()
	if rows.Used != 3 || bytes.Used != 30 || rows.Counters.Refused != 1 || bytes.Counters.Refused != 1 ||
		!rows.Counters.LastActionAt.Equal(now) {
		t.Fatalf("after one refusal: %+v %+v", rows, bytes)
	}
}

// What the spool burns is counted by what it was: a replaced snapshot is
// coalesced (nothing lost), a row that never left is dropped, and a row that
// left and was never answered is neither — its delivery is unknown, and the
// note says so.
func TestTheSpoolCountsWhatItBurns(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)

	older, _ := spool.ReserveLatestValue(SpoolChannelS, "s/mac/one", "one", 10)
	sealRow(t, spool, older, now)
	if _, err := spool.ReserveLatestValue(SpoolChannelS, "s/mac/one", "one", 10); err != nil {
		t.Fatal(err)
	}
	if rows, _ := spool.Readings(); rows.Counters.Coalesced != 1 || rows.Counters.Dropped != 0 {
		t.Fatalf("a replaced snapshot: %+v", rows.Counters)
	}

	// The newer snapshot is never sealed, and a reservation nobody seals is
	// burned once it is stale: it never left.
	now = now.Add(61 * time.Second)
	spool.SendNext()
	if rows, _ := spool.Readings(); rows.Counters.Dropped != 1 {
		t.Fatalf("a stale reservation: %+v", rows.Counters)
	}

	sent, _ := spool.Reserve(SpoolChannelOrch, "orch/mac", "orch", 10)
	sealRow(t, spool, sent, now)
	if d := spool.SendNext(); d.Row == nil || d.Row.Seq != sent {
		t.Fatalf("send: %+v", d)
	}
	now = now.Add(31 * time.Second)
	if burned := spool.BurnExpired(); len(burned) != 1 {
		t.Fatalf("burned %v", burned)
	}
	rows, _ := spool.Readings()
	if rows.Counters.Dropped != 1 || !strings.Contains(rows.Note, "1 written answer") ||
		!strings.Contains(rows.Note, "unknown") {
		t.Fatalf("an unanswered row: %+v", rows)
	}
}
