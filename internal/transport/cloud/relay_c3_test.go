package cloud

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// limits N20: a full request queue refuses the new request and keeps every
// one it already took. It used to drop the oldest, and nobody who sent that
// one was told.
func TestAFullQueueRefusesTheNewRequestAndKeepsTheOnesItTook(t *testing.T) {
	relay := &Relay{Depth: 2, Log: func(string, ...any) {}}
	for seq := uint64(1); seq <= 3; seq++ {
		relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Seq: seq}, []byte(`{}`))
	}
	queued := []uint64{(<-relay.Requests()).Sequence, (<-relay.Requests()).Sequence}
	if queued[0] != 1 || queued[1] != 2 {
		t.Fatalf("the queue holds %v; the two it took first must stay, the third must be refused", queued)
	}
	select {
	case refused := <-relay.Refused():
		if refused.Sequence != 3 {
			t.Fatalf("refused seq %d, wanted the newest, 3", refused.Sequence)
		}
	default:
		t.Fatal("nothing was handed over to be told it was refused")
	}
	waiting, depth, counters := relay.Queue()
	if waiting != 0 || depth != 2 || counters.Refused != 1 || counters.Dropped != 0 {
		t.Fatalf("queue reads waiting=%d depth=%d %+v; want 0, 2, refused 1, dropped 0", waiting, depth, counters)
	}
}

// When even the refusal lane is full, the sender cannot be told: that request
// is the one that reaches nobody, and it is counted as such.
func TestARefusalThatCannotBeToldIsCountedAsDropped(t *testing.T) {
	var lines []string
	relay := &Relay{Depth: 1, Log: func(f string, _ ...any) { lines = append(lines, f) }}
	for seq := uint64(1); seq <= uint64(2+refusalLane); seq++ {
		relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Seq: seq}, []byte(`{}`))
	}
	_, _, counters := relay.Queue()
	if counters.Refused != int64(1+refusalLane) || counters.Dropped != 1 {
		t.Fatalf("%+v: want %d refused and the one past the lane dropped", counters, 1+refusalLane)
	}
	if refused, unanswered := relay.Refusals(); refused != 1+refusalLane || unanswered != 1 {
		t.Fatalf("status says refused %d unanswered %d", refused, unanswered)
	}
}

// refusingFake is the relay's two channels over the fake's publication, so
// Service can be run against it without a socket.
type refusingFake struct {
	*Fake
	refusals chan Inbound
	mu       sync.Mutex
	lost     []string
}

func (f *refusingFake) Refused() <-chan Inbound { return f.refusals }
func (f *refusingFake) QueueDepth() int         { return 64 }
func (f *refusingFake) Unanswered(_ Inbound, why string) {
	f.mu.Lock()
	f.lost = append(f.lost, why)
	f.mu.Unlock()
}

// The refused request's sender is answered 429 cloud_ingress_busy on the
// channel it is waiting on, with the detail the hosted console reads — while
// the bridge is still busy with the request that filled the queue.
func TestTheSenderOfARefusedRequestIsToldItsBusy(t *testing.T) {
	fake := &refusingFake{Fake: NewFake(1), refusals: make(chan Inbound, 1)}
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake, Log: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	fake.refusals <- Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 77,
		Plaintext: plaintext(t, map[string]any{"type": "transcript", "session": "%19", "limit": 50})}
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.Published()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	published := fake.Published()
	if len(published) != 1 {
		t.Fatalf("published %d answers to a refused request, wanted one", len(published))
	}
	out := published[0]
	if out.Channel != "t/mac-01/%2519" || out.Reply.Code != "cloud_ingress_busy" || out.Reply.Status != 429 {
		t.Fatalf("answered %q %d %s", out.Channel, out.Reply.Status, out.Reply.Code)
	}
	var payload struct {
		Read   string `json:"read"`
		Status int    `json:"status"`
		Error  struct {
			Code   string         `json:"code"`
			Layer  string         `json:"layer"`
			Seq    uint64         `json:"seq"`
			Detail map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Read != "transcript" || payload.Status != 429 || payload.Error.Code != "cloud_ingress_busy" ||
		payload.Error.Layer != "mac_transport" || payload.Error.Seq != 77 ||
		payload.Error.Detail["retry_after"] != float64(1) || payload.Error.Detail["lane"] != "ingress" ||
		payload.Error.Detail["limit"] != float64(64) {
		t.Fatalf("payload %s", out.Payload)
	}
}

// A refused command from a device that may not write is told that, not that
// it may retry; a body that names no waiter cannot be answered and says why.
func TestARefusalIsTheRightSentenceOrSaysWhyNobodyHeardIt(t *testing.T) {
	fake := NewFake(1)
	bridge := answering(t)
	bridge.AllowCommands = func() bool { return false }
	service := Service{MachineID: "mac-01", Bridge: bridge, Transport: fake, Log: func(string, ...any) {}}
	why := service.Busy(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "v", Sequence: 1,
		Plaintext: plaintext(t, map[string]any{"type": "send", "session": "%1", "request": "r1", "text": "hi"})}, 64)
	if why != "" || len(fake.Published()) != 1 || fake.Published()[0].Reply.Code != "cloud_commands_disabled" {
		t.Fatalf("why %q, published %+v", why, fake.Published())
	}
	why = service.Busy(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "v", Sequence: 2,
		Plaintext: []byte(`not json`)}, 64)
	if why == "" || len(fake.Published()) != 1 {
		t.Fatalf("a body naming no waiter was answered: why %q, published %d", why, len(fake.Published()))
	}
}
