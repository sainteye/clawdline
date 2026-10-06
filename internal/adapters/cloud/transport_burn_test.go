package cloud

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type settlement struct {
	channel string
	seq     uint64
	kind    SettleKind
}

// publishBurnTestRow queues one sealed terminal frame whose envelope names its
// channel and sequence, the two things the relay answers by.
func publishBurnTestRow(t *testing.T, spool *Spool, channel string, at time.Time) uint64 {
	t.Helper()
	seq, err := spool.ReserveLatestValue(SpoolChannelTerm, channel, channel, 100)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := json.Marshal(map[string]any{"ch": channel, "seq": seq, "ct": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Seal(seq, sealed, at); err != nil {
		t.Fatal(err)
	}
	return seq
}

func waitForSettlement(t *testing.T, settled <-chan settlement, want settlement) {
	t.Helper()
	select {
	case got := <-settled:
		if got != want {
			t.Fatalf("settled %+v, want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("nothing settled; want %+v", want)
	}
}

// The relay took a frame and never answered it, for longer than the spool's
// attempt window, while the socket stayed up (it still answered pings, so
// nothing reconnected and no terminal connection was closed). The spool
// burned the row, and until 2026-10-06 nobody heard: the terminal connection
// waiting on that frame kept every later screen back. The burned row now
// reaches OnSettled once, and the next frame after the relay answers again
// settles as usual.
func TestARowTheRelayNeverAnswersSettlesAsBurnedAndTheNextOneIsDelivered(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	spool, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	settled := make(chan settlement, 16)
	line := newTestLineConfigured(t, tokenServer(t), func(o *Options) {
		o.Spool = spool
		o.OnSettled = func(channel string, seq uint64, kind SettleKind) {
			settled <- settlement{channel, seq, kind}
		}
	})
	line.relay.Withhold(true)
	line.run(t)

	channel := "term/mac_test/viewer/AAAAAAAAAAAAAAAAAAAAAA"
	first := publishBurnTestRow(t, spool, channel, clock())
	deadline := time.Now().Add(3 * time.Second)
	for len(line.relay.Published()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the frame never reached the relay")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	now = now.Add(DefaultSpoolLimits().AttemptWindow + time.Second)
	mu.Unlock()
	waitForSettlement(t, settled, settlement{channel, first, SettleBurned})

	line.relay.Withhold(false)
	second := publishBurnTestRow(t, spool, channel, clock())
	waitForSettlement(t, settled, settlement{channel, second, SettleDelivered})
	select {
	case extra := <-settled:
		t.Fatalf("settled again: %+v", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

// A burned row is handed out once, whichever rule burned it.
func TestTakeBurnedHandsEachBurnedRowOutOnce(t *testing.T) {
	t.Parallel()
	now := time.Unix(1789000000, 0)
	spool := testSpool(t, &now)
	channel := "term/mac/viewer/AAAAAAAAAAAAAAAAAAAAAA"
	sent := publishBurnTestRow(t, spool, channel, now)
	if row := spool.SendNext().Row; row == nil || row.Seq != sent {
		t.Fatal("not sent")
	}
	stale := publishBurnTestRow(t, spool, channel, now)
	now = now.Add(DefaultSpoolLimits().SealedFrameFreshness + time.Second)
	spool.BurnExpired()
	spool.SendNext() // burns the stale ready row
	rows := spool.TakeBurned()
	if len(rows) != 2 || rows[0].Seq != sent || rows[1].Seq != stale || rows[0].Recipient != channel {
		t.Fatalf("burned rows: %+v", rows)
	}
	if again := spool.TakeBurned(); len(again) != 0 {
		t.Fatalf("handed out again: %+v", again)
	}
}
