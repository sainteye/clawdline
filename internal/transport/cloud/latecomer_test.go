package cloud

import (
	"context"
	"testing"
	"time"

	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// A viewer that arrives after this machine's last change.
//
// The three tests here are one measurement in three parts: how long an idle
// Mac says nothing, what that silence costs a viewer that connects into it,
// and what it costs the wire to end it.

// The silence itself, with the clock driven so the number is the real one and
// not a wait.
//
// This is not the bug — an unchanged row that is not re-sent is the whole
// point of the skip — it is the size of the window the bug lives in.
func TestAnIdleMacSaysNothingBetweenHeartbeats(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Now = func() time.Time { return at }
	publisher.firstPass(context.Background())
	if len(out.channels()) != 3 {
		t.Fatalf("the first pass published %v; want the descriptor, the row and the marker", out.channels())
	}

	// Every pass from here to the heartbeat, at the publisher's own interval.
	out.reset()
	silent := time.Duration(0)
	for at.Sub(time.Unix(1_700_000_000, 0)) < Heartbeat {
		at = at.Add(SnapshotInterval)
		publisher.Pass(context.Background())
		if len(out.channels()) > 0 {
			break
		}
		silent = at.Sub(time.Unix(1_700_000_000, 0))
	}
	if silent != Heartbeat-SnapshotInterval {
		t.Errorf("an idle Mac was quiet for %v; the skip and the %v heartbeat make that %v",
			silent, Heartbeat, Heartbeat-SnapshotInterval)
	}
	if len(out.channels()) != 3 {
		t.Errorf("the heartbeat pass published %v; want all three again", out.channels())
	}
}

// The bug: a viewer that connects into that silence is told nothing, however
// long it waits and however much it asks.
//
// Before this was fixed the loop below published nothing at all: the skip
// remembers what this machine last *sent*, which is a fact about the wire and
// not about who was listening to it, and a viewer whose relay connection was
// evicted holds none of it.
func TestAViewerHeardFromForTheFirstTimeIsStatedToAgain(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Now = func() time.Time { return at }
	publisher.firstPass(context.Background())
	out.reset()

	// A viewer connects and asks this machine something — a schedule list, a
	// status digest, anything. Nothing about this machine has changed.
	at = at.Add(30 * time.Second)
	publisher.Seen("viewer-01")
	publisher.Pass(context.Background())

	channels := out.channels()
	if len(channels) != 3 {
		t.Fatalf("a machine that has just heard from a viewer published %v; "+
			"want the descriptor, the row and the marker, or that viewer waits %v",
			channels, Heartbeat)
	}
	// The order is what makes the list readable: a row before its descriptor
	// is a session the page cannot open, and a marker before its rows is a
	// list that names sessions the viewer was never sent.
	if channels[0] != "orch/mac-01" || channels[len(channels)-1] != "s/mac-01/"+InventorySessionID {
		t.Errorf("the re-statement went out as %v; want the descriptor first and the marker last", channels)
	}
}

// What it costs: one re-statement per device per connection, and nothing for
// the viewer's second question or its hundredth.
func TestAViewerAlreadyStatedToCostsNothingMore(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Now = func() time.Time { return at }
	publisher.firstPass(context.Background())

	publisher.Seen("viewer-01")
	at = at.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	out.reset()

	// A hundred asks inside one heartbeat window, so that what is counted
	// below is the asking and not the heartbeat the clock would otherwise
	// cross.
	for i := 0; i < 100; i++ {
		publisher.Seen("viewer-01")
		at = at.Add(time.Second)
		publisher.Pass(context.Background())
	}
	if names := out.channels(); len(names) != 0 {
		t.Errorf("a viewer that kept asking was answered with %d more envelopes: %v", len(names), names)
	}

	// A second device is a second audience, and is stated to once.
	publisher.Seen("viewer-02")
	at = at.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	if names := out.channels(); len(names) != 3 {
		t.Errorf("the second viewer was stated to with %v; want all three", names)
	}
}

// This machine is not its own audience: its own envelopes must not make it
// re-state anything.
func TestThisMachineIsNotItsOwnAudience(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.Now = func() time.Time { return at }
	publisher.firstPass(context.Background())
	out.reset()

	publisher.Seen("mac-01")
	publisher.Seen("")
	at = at.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	if names := out.channels(); len(names) != 0 {
		t.Errorf("published %v for a sender that is this machine or nobody", names)
	}
}

// The relay is where the evidence arrives. A refused request proves a viewer
// is out there just as well as an answered one, so the audience hears about
// it on the way in.
func TestTheRelayTellsThePublisherEverySenderItHearsFrom(t *testing.T) {
	var heard []string
	relay := &Relay{Depth: 1, Log: func(string, ...any) {},
		Audience: func(sender string) { heard = append(heard, sender) }}
	relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Seq: 1}, []byte(`{}`))
	// The second one is past the queue's depth and is refused.
	relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: "viewer-02", Seq: 2}, []byte(`{}`))
	if len(heard) != 2 || heard[0] != "viewer-01" || heard[1] != "viewer-02" {
		t.Fatalf("the audience heard %v; want both senders, including the refused one", heard)
	}
}
