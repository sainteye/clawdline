package cloud

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The sizes this channel really carries, measured on one machine on
// 2026-09-21 against its own routes:
//
//	transcriptAnswerBytes  GET /v1/transcript?session=…&limit=1000, the
//	                       largest a Cloud `transcript` read may ask for
//	boardAnswerBytes       GET /v1/board?project=…&audience=all&limit=200,
//	                       the largest of that machine's 35 project boards
//
// They are constants here because the bug is about size: a test with ten-byte
// rows passes through a cap that thirteen real answers fill.
const (
	transcriptAnswerBytes = 151_932
	boardAnswerBytes      = 362_457
)

// A channel is not shut by the receipts of answers it already delivered.
//
// This is the failure as it was measured: one long session's transcript
// channel held 2,080,989 bytes of answers that had all been acked, the
// fourteenth read was refused `the outbound spool is full`, and it stayed
// refused for the whole ten-minute tombstone window while the spool's global
// rows read 401 of 2,000 and its bytes 3.09 MB of 16 MiB.
func TestAChannelIsNotShutByTheReceiptsOfWhatItAlreadyDelivered(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	const channel = "t/mac-01/%2519"

	// Thirty transcript answers, each one reserved, sealed, written and
	// acked, is 4.5 MB through a channel whose cap is 4 MiB — and every byte
	// of it delivered. Ten minutes have not passed, so every receipt is still
	// held.
	for i := 0; i < 30; i++ {
		seq, err := spool.Reserve(SpoolChannelT, channel, "session", transcriptAnswerBytes)
		if err != nil {
			t.Fatalf("answer %d of a channel that is being drained: %v", i+1, err)
		}
		sealRow(t, spool, seq, now)
		if d := spool.SendNext(); d.Row == nil || d.Row.Seq != seq {
			t.Fatalf("answer %d was not the one to write: %+v", i+1, d)
		}
		if _, err := spool.Settle(seq, channel, SettleDelivered); err != nil {
			t.Fatalf("settling answer %d: %v", i+1, err)
		}
		now = now.Add(2 * time.Second)
	}

	if _, err := spool.Reserve(SpoolChannelT, channel, "session", transcriptAnswerBytes); err != nil {
		t.Fatalf("the next read of a channel with nothing owed on it: %v", err)
	}

	rows, bytes := spool.Readings()
	if rows.Used != 1 || bytes.Used != transcriptAnswerBytes {
		t.Fatalf("the spool is owed %d row(s) and %d bytes; one answer is outstanding", rows.Used, bytes.Used)
	}
	channelBytes, receipts := spool.ChannelReadings()
	if channelBytes.Used != transcriptAnswerBytes {
		t.Fatalf("the fullest channel reads %d bytes, want the one answer that is owed", channelBytes.Used)
	}
	if receipts.Used != 30 || receipts.WindowSeconds != 600 {
		t.Fatalf("receipts: %d held over %ds, want the 30 answered rows over the retention window",
			receipts.Used, receipts.WindowSeconds)
	}
}

// What is owed still fills the channel. Nothing here says a channel may hold
// as much as it likes: the cap is on the answers nobody has taken yet.
func TestAChannelNobodyIsDrainingStillFills(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	const channel = "t/mac-01/%2519"

	admitted := 0
	var refusal error
	for i := 0; i < 64; i++ {
		if _, err := spool.Reserve(SpoolChannelT, channel, "session", transcriptAnswerBytes); err != nil {
			refusal = err
			break
		}
		admitted++
	}
	if !errors.Is(refusal, ErrSpoolCapacity) {
		t.Fatalf("a channel nobody drains was never refused: %d admitted, %v", admitted, refusal)
	}
	// 4 MiB of transcript answers, and the refusal says which channel, how
	// much it holds and what the bound is.
	if admitted != (4<<20)/transcriptAnswerBytes {
		t.Fatalf("%d answers fitted a 4 MiB channel at %d bytes each", admitted, transcriptAnswerBytes)
	}
	for _, want := range []string{channel, "bytes of 4194304"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("the refusal reads %q and does not name %q", refusal, want)
		}
	}
}

// A channel that is full can still say it is full. Everything else about a
// full channel is exactly what will not go through it, so the sentence that
// says so is admitted under a reserve of its own — one at a time.
func TestAFullChannelCanStillSayItIsFull(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	const channel = "t/mac-01/%2519"

	for {
		if _, err := spool.Reserve(SpoolChannelT, channel, "session", boardAnswerBytes); err != nil {
			break
		}
	}
	// The board answers leave a gap smaller than one more of them; small
	// rows take the rest, and then the channel is full by both of its own
	// bounds — the bytes it holds and the rows it holds.
	var full error
	for i := 0; i < 1_000; i++ {
		if _, err := spool.Reserve(SpoolChannelT, channel, "session", 400); err != nil {
			full = err
			break
		}
	}
	if !errors.Is(full, ErrSpoolCapacity) {
		t.Fatalf("the channel is not full: %v", full)
	}

	if _, err := spool.ReserveRefusal(SpoolChannelT, channel, "session", 400); err != nil {
		t.Fatalf("the refusal could not be admitted to a full channel: %v", err)
	}
	// A second refusal on the same channel is a second waiter's answer, not
	// the first one repeated, and it is admitted too.
	if _, err := spool.ReserveRefusal(SpoolChannelT, channel, "session", 400); err != nil {
		t.Fatalf("a second refusal for the same channel: %v", err)
	}
	if _, err := spool.ReserveRefusal(SpoolChannelT, channel, "session", 8<<10); !errors.Is(err, ErrSpoolRefusalSize) {
		t.Fatalf("an answer-sized `refusal` must not pass the reserve")
	}
	// Another channel is not being told anything and may be.
	if _, err := spool.ReserveRefusal(SpoolChannelT, "t/mac-01/%2531", "other", 400); err != nil {
		t.Fatalf("a second channel's refusal: %v", err)
	}
}

// The reserve is bounded and not spent for ever: a channel whose refusals
// have been answered may be told again the next time it fills.
func TestAChannelMayBeToldAgainOnceTheLastRefusalWasAnswered(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.RecipientByteCap = 1 << 10
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	const channel = "t/mac-01/%2519"
	if _, err := spool.Reserve(SpoolChannelT, channel, "session", 1<<10); err != nil {
		t.Fatal(err)
	}
	told, err := spool.ReserveRefusal(SpoolChannelT, channel, "session", 400)
	if err != nil {
		t.Fatalf("the refusal: %v", err)
	}
	// The answer ahead of it is sealed too: a reservation nobody sealed stops
	// the queue, which is invariant 2 and not something the reserve overrides.
	for _, seq := range []uint64{told - 1, told} {
		sealRow(t, spool, seq, now)
	}
	for {
		d := spool.SendNext()
		if d.Row == nil {
			break
		}
		if _, err := spool.Settle(d.Row.Seq, channel, SettleDelivered); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := spool.ReserveRefusal(SpoolChannelT, channel, "session", 400); err != nil {
		t.Fatalf("after the first refusal was answered: %v", err)
	}
}

// The reserve does not outrank the machine. Past the global caps there is no
// memory to put a refusal in, and spending the last of the budget on the
// explanation is not the trade.
func TestTheRefusalReserveStopsAtTheGlobalCaps(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.GlobalRowCap = 2
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := spool.Reserve(SpoolChannelT, "t/mac-01/one", "one", 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := spool.ReserveRefusal(SpoolChannelT, "t/mac-01/two", "two", 400); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("a refusal past the global row cap: %v", err)
	}
}

// The receipt table is bounded, and letting one go early is counted: from
// then on a late ack for that sequence reads as a sequence never sent.
func TestTheReceiptTableIsBoundedAndSaysWhatItLetGo(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	limits := DefaultSpoolLimits()
	limits.ReceiptCap = 4
	spool, err := NewSpool(limits, &MemoryFence{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var first uint64
	for i := 0; i < 6; i++ {
		seq, err := spool.Reserve(SpoolChannelT, "t/mac-01/one", "one", 10)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = seq
		}
		sealRow(t, spool, seq, now)
		if d := spool.SendNext(); d.Row == nil {
			t.Fatalf("nothing to write on pass %d: %+v", i, d)
		}
		if _, err := spool.Settle(seq, "t/mac-01/one", SettleDelivered); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	// The collect runs on the next reservation.
	if _, err := spool.Reserve(SpoolChannelT, "t/mac-01/one", "one", 10); err != nil {
		t.Fatal(err)
	}
	_, receipts := spool.ChannelReadings()
	if receipts.Used != int64(limits.ReceiptCap) || receipts.Counters.Expired != 2 {
		t.Fatalf("receipts: %d held, %d expired; want %d held and the two oldest let go",
			receipts.Used, receipts.Counters.Expired, limits.ReceiptCap)
	}
	if _, ok := spool.Row(first); ok {
		t.Fatal("the oldest receipt is still held; the table went past its cap instead")
	}
	// A receipt that is still held answers late and ignored, which is the
	// protection the retention window is for.
	held := spool.InFlight()
	if len(held) != 0 {
		t.Fatalf("nothing should still be in flight: %v", held)
	}
}

// The per-channel bound and the receipt table are the register's rows, spelled
// once, as the two global caps already were.
func TestTheChannelCapAndTheReceiptTableAreTheRegisters(t *testing.T) {
	limits := DefaultSpoolLimits()
	if int64(limits.RecipientByteCap) != capacity.Default(capacity.CloudSpoolChannelBytes) ||
		int64(limits.ReceiptCap) != capacity.Default(capacity.CloudSpoolReceipts) {
		t.Fatalf("the spool runs %d bytes per channel and %d receipts; the register says %d and %d",
			limits.RecipientByteCap, limits.ReceiptCap,
			capacity.Default(capacity.CloudSpoolChannelBytes), capacity.Default(capacity.CloudSpoolReceipts))
	}
}
