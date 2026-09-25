package cloud

import (
	"errors"
	"testing"
	"time"
)

// The small-answer reserve: a large answer that would leave less than
// smallAnswerReserveLimit of its channel free is refused as capacity, so a
// small one behind it still fits. It holds only where it was asked for.
func TestALargeAnswerLeavesRoomForSmallOnes(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	const machine = "t/mac-01/__clawdline_machine__"
	channel := DefaultSpoolLimits().RecipientByteCap

	// On an idle channel even a picture the size of the channel may go:
	// otherwise it could never be delivered at all.
	if _, err := spool.ReserveAnswer(SpoolChannelT, machine, "img", channel-1000, true); err != nil {
		t.Fatalf("a large answer on an idle channel: %v", err)
	}
	fresh, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	spool = fresh

	big := 1_200_000
	for i := 0; i < 2; i++ {
		if _, err := spool.ReserveAnswer(SpoolChannelT, machine, "img", big, true); err != nil {
			t.Fatalf("image %d: %v", i+1, err)
		}
	}
	_, err = spool.ReserveAnswer(SpoolChannelT, machine, "img", big, true)
	if !errors.Is(err, ErrSpoolHeadroom) || !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("a third image that would eat the reserve: %v", err)
	}
	// A small answer may use the reserve, and so may one exactly at the
	// threshold.
	if _, err := spool.ReserveAnswer(SpoolChannelT, machine, "todos", 40_000, true); err != nil {
		t.Fatalf("a small answer: %v", err)
	}
	if _, err := spool.ReserveAnswer(SpoolChannelT, machine, "board", largeAnswerByteLimit, true); err != nil {
		t.Fatalf("an answer at the threshold: %v", err)
	}
	// A channel that did not ask for the reserve keeps its whole cap: a
	// transcript channel holds two of its largest answers (limits N22).
	const session = "t/mac-01/%2519"
	for i := 0; i < 2; i++ {
		if _, err := spool.ReserveAnswer(SpoolChannelT, session, "document", 2<<20-100, false); err != nil {
			t.Fatalf("document %d on a transcript channel: %v", i+1, err)
		}
	}
}

// A flood of refused reads is bounded: at most RecipientRefusalCap refusals
// live on one channel, each at most the refusal reserve, and the one past the
// cap is refused and counted where diagnostics reads it.
func TestTheRefusalCapBoundsMemory(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	const machine = "t/mac-01/__clawdline_machine__"
	if _, err := spool.Reserve(SpoolChannelT, machine, "img", limits.RecipientByteCap); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < limits.RecipientRefusalCap; i++ {
		if _, err := spool.ReserveRefusal(SpoolChannelT, machine, "refusal", limits.RefusalByteLimit()); err != nil {
			t.Fatalf("refusal %d of %d: %v", i+1, limits.RecipientRefusalCap, err)
		}
	}
	if _, err := spool.ReserveRefusal(SpoolChannelT, machine, "refusal", 400); !errors.Is(err, ErrSpoolRefusalPending) {
		t.Fatalf("a refusal past the cap: %v", err)
	}
	reading := spool.RefusalReading()
	if reading.Used != int64(limits.RecipientRefusalCap) || reading.Counters.Refused != 1 {
		t.Fatalf("the refusal row reads %+v", reading)
	}
	_, bytes := spool.Readings()
	if held, most := bytes.Used, int64(limits.RecipientByteCap+limits.RecipientRefusalCap*limits.RefusalByteLimit()); held > most {
		t.Fatalf("one channel holds %d bytes, past %d", held, most)
	}
	if limits.RecipientRefusalCap*limits.RefusalByteLimit() > 256<<10 {
		t.Fatalf("the refusal reserve alone may hold %d bytes a channel", limits.RecipientRefusalCap*limits.RefusalByteLimit())
	}
}
