package cloud

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// A page that comes back to a Session list after a long time.
//
// The relay forgets every row when the account's object is evicted, and this
// machine re-sends an unchanged row only on its heartbeat. `Seen` re-states
// everything for a device this socket has never heard from — but a phone that
// was heard from hours ago is not new to a socket that stayed up, so it got
// whichever rows had changed and an incomplete list until each heartbeat came
// round. The page already asks `sessions.snapshot` on every connection that
// did not take over a live socket; these tests are this machine answering it.

// The control: what a returning device got before this word existed. Nothing
// changed and the device was heard from before, so nothing is re-sent.
func TestADeviceHeardFromBeforeIsNotStatedToAgainBySeenAlone(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Now = func() time.Time { return at }
	publisher.firstPass(context.Background())
	publisher.Seen("viewer-01")
	at = at.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	out.reset()

	// Hours later, on the same socket, the page is reloaded after an eviction.
	at = at.Add(time.Minute)
	publisher.Seen("viewer-01")
	publisher.Pass(context.Background())
	if names := out.channels(); len(names) != 0 {
		t.Fatalf("expected the old silence; the machine published %v", names)
	}
}

// The fix: the same returning page asks, and every row goes out before the
// answer that names them.
func TestASessionsSnapshotStatesEveryRowBeforeItAnswers(t *testing.T) {
	out := &collector{}
	router := &fixedRouter{body: scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "%19", tty: "ttys001", backend: "tmux"},
		scanRow{id: "GUID-A", tty: "ttys002", backend: "iterm"})}
	publisher := newPublisher(router, out)
	publisher.Every = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = publisher.Run(ctx); close(done) }()
	waitFor(t, "the first pass", func() bool { return len(out.channels()) >= 4 })
	// Idle passes publish nothing; wait for one to be sure the first pass is
	// over before the reset.
	time.Sleep(3 * publisher.Every)
	out.reset()

	stated, refusal := publisher.Snapshot(ctx)
	if refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	if strings.Join(stated.IDs, ",") != "%19,GUID-A" || !stated.Complete {
		t.Fatalf("answered %+v", stated)
	}
	channels := out.channels()
	want := []string{"orch/mac-01", "s/mac-01/%2519", "s/mac-01/GUID-A", "s/mac-01/" + InventorySessionID}
	if strings.Join(channels, " ") != strings.Join(want, " ") {
		t.Fatalf("before the answer the machine published %v; want %v", channels, want)
	}

	cancel()
	<-done
}

// However many pages ask, one machine states its rows once per window, and a
// burst waiting for that pass is bounded. The one past the bound is told to
// come back, at once, rather than queued.
func TestSessionsSnapshotWaitersAreBoundedAndTheLineGoingDownAnswersThem(t *testing.T) {
	publisher := newPublisher(&fixedRouter{body: completeScan}, &collector{})

	if _, refusal := publisher.Snapshot(context.Background()); refusal == nil || refusal.Code != "cloud_starting" {
		t.Fatalf("a publisher that is not running answered %+v", refusal)
	}

	publisher.open()
	waiting := make(chan *cloudops.Refusal, SessionSnapshotWaitersLimit)
	for i := 0; i < SessionSnapshotWaitersLimit; i++ {
		go func() {
			_, refusal := publisher.Snapshot(context.Background())
			waiting <- refusal
		}()
	}
	waitFor(t, "every waiter", func() bool {
		publisher.mu.Lock()
		defer publisher.mu.Unlock()
		return len(publisher.asks) == SessionSnapshotWaitersLimit
	})

	_, refusal := publisher.Snapshot(context.Background())
	if refusal == nil || refusal.Code != "cloud_read_busy" || refusal.Status != 429 {
		t.Fatalf("the waiter past the bound was answered %+v", refusal)
	}
	if refusal.Detail["retry_after"] != 5 || refusal.Detail["limit"] != SessionSnapshotWaitersLimit {
		t.Fatalf("the busy answer's detail is %v", refusal.Detail)
	}

	// The line goes down before the pass: every waiter is told, none hangs.
	publisher.close()
	for i := 0; i < SessionSnapshotWaitersLimit; i++ {
		select {
		case refusal := <-waiting:
			if refusal == nil || refusal.Code != "cloud_reconnecting" {
				t.Fatalf("a waiter was answered %+v when the line went down", refusal)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d waiters were answered", i, SessionSnapshotWaitersLimit)
		}
	}
}

// Two pages asking inside one window share a pass: the second waits for the
// window rather than buying a second full re-statement at once.
func TestTwoAsksInsideOneWindowShareTheWire(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Every = 150 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = publisher.Run(ctx) }()
	waitFor(t, "the first pass", func() bool { return len(out.channels()) >= 3 })
	time.Sleep(publisher.Every + 20*time.Millisecond)

	out.reset()
	if _, refusal := publisher.Snapshot(ctx); refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	first := len(out.channels())
	started := time.Now()
	if _, refusal := publisher.Snapshot(ctx); refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	if waited := time.Since(started); waited < publisher.Every/2 {
		t.Fatalf("the second ask was answered after %v, inside the %v window", waited, publisher.Every)
	}
	if first != 3 || len(out.channels()) != 6 {
		t.Fatalf("published %d then %d envelopes; want 3 per re-statement", first, len(out.channels()))
	}
}

// An id carried while its own source is unreadable is named by the marker, so
// a re-statement has to send its row too: a page that holds nothing would
// otherwise wait on a Session it was never given, for as long as iTerm2 stays
// unreadable.
func TestARestatementSendsTheHeldRowOfACarriedID(t *testing.T) {
	out := &collector{}
	router := &fixedRouter{body: scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"},
		scanRow{id: "GUID-B", tty: "ttys002", backend: "iterm"})}
	publisher := newPublisher(router, out)
	publisher.firstPass(context.Background())
	router.set(scanBody(t, false, map[string]bool{"ps": true, "tmux": true, "iterm": false},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"}))
	publisher.Pass(context.Background())
	out.reset()

	publisher.restatePass(context.Background())
	if !holds(inventoryNames(t, out), "GUID-B") {
		t.Fatalf("the carried id left the inventory: %v", inventoryNames(t, out))
	}
	row := out.payload(t, "s/mac-01/GUID-B")
	session, _ := row["session"].(map[string]any)
	if session["id"] != "GUID-B" || session["tty"] != "ttys002" {
		t.Fatalf("the carried row went out as %v", row)
	}
	channels := out.channels()
	if channels[len(channels)-1] != "s/mac-01/"+InventorySessionID {
		t.Fatalf("the marker went out before a row it names: %v", channels)
	}

	// Once iTerm2 answers again and the Session is gone, its held row goes
	// with it and is never sent again.
	router.set(scanBody(t, true, map[string]bool{"ps": true, "tmux": true, "iterm": true},
		scanRow{id: "GUID-A", tty: "ttys001", backend: "iterm"}))
	publisher.Pass(context.Background())
	if _, held := publisher.held["GUID-B"]; held {
		t.Fatal("a Session its source proved gone is still held for re-sending")
	}
}

// The page asks only a machine whose inventory marker lists the word
// (`_machineHasFeature` in net/cloud-client.js). Before this, the marker said
// `board.items` alone and no page ever asked.
func TestTheInventoryMarkerTellsThePageItMayAskForTheRows(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.firstPass(context.Background())
	marker := out.payload(t, "s/mac-01/"+InventorySessionID)
	words, _ := marker["features"].([]any)
	listed := false
	for _, word := range words {
		listed = listed || word == "sessions.snapshot"
	}
	if !listed {
		t.Fatalf("the marker's features are %v; a page will not ask for the rows", words)
	}
	descriptor := out.payload(t, "orch/mac-01")
	commands, _ := descriptor["machine"].(map[string]any)["commands"].([]any)
	listed = false
	for _, word := range commands {
		listed = listed || word == "sessions.snapshot"
	}
	if !listed {
		t.Fatal("the descriptor's commands leave the word out, and the page refuses to send a word not in them")
	}
}

// A row the relay refused is not answered as sent: the page would be named a
// Session that never arrives. The re-statement answers a retryable refusal,
// and the next pass tries the row again rather than waiting for its heartbeat.
func TestARowTheRelayRefusedIsNotAnsweredAsStated(t *testing.T) {
	out := &collector{}
	refuse := true
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Publish = func(ctx context.Context, o Outbound) error {
		if refuse && o.Channel == "s/mac-01/%2519" {
			return errors.New("spool full")
		}
		return out.publish(ctx, o)
	}
	publisher.firstPass(context.Background())
	out.reset()

	publisher.mu.Lock()
	answer := make(chan snapshotResult, 1)
	publisher.asks = []chan snapshotResult{answer}
	publisher.mu.Unlock()
	publisher.restatePass(context.Background())
	result := <-answer
	if result.refusal == nil || result.refusal.Code != "reading_busy" {
		t.Fatalf("a pass whose row was refused answered %+v", result)
	}

	refuse = false
	out.reset()
	publisher.Pass(context.Background())
	if !holds(out.channels(), "s/mac-01/%2519") {
		t.Fatalf("the refused row was not tried again on the next pass: %v", out.channels())
	}
}
