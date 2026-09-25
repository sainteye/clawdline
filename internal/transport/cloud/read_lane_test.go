package cloud

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// A read must not wait behind a command that has not come back.
//
// On a machine reached through Cloud, one request that did not return held
// every request after it: the Board's Project picker and the new-session
// dialog both wait on `places`, and both stayed on "loading" while the Session
// list — pushed, not asked for — kept moving. These tests hold a command or a
// read open on purpose and ask what the requests behind it get.

// gatedRouter answers every read at once unless its path is held, and holds
// every command until its text is released. It records the order commands
// reached it in.
type gatedRouter struct {
	mu       sync.Mutex
	commands []string
	gates    map[string]chan struct{}
	holdRead chan struct{}
	reading  int
}

func newGatedRouter() *gatedRouter {
	return &gatedRouter{gates: map[string]chan struct{}{}}
}

func (g *gatedRouter) gate(text string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.gates[text] == nil {
		g.gates[text] = make(chan struct{})
	}
	return g.gates[text]
}

func (g *gatedRouter) Do(ctx context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	if req.Method == "GET" {
		g.mu.Lock()
		hold := g.holdRead
		g.reading++
		g.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
			}
		}
		return cloudops.LocalResponse{Status: 200, Body: []byte(`{"places":[]}`)}, nil
	}
	var body struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(req.Body, &body)
	g.mu.Lock()
	g.commands = append(g.commands, body.Text)
	g.mu.Unlock()
	select {
	case <-g.gate(body.Text):
	case <-ctx.Done():
	}
	return cloudops.LocalResponse{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func (g *gatedRouter) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.commands...)
}

func (g *gatedRouter) readsInside() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reading
}

// laneLink is a link with a real request queue and a fake line out, which is
// everything runService touches.
func laneLink(t *testing.T, router cloudops.LocalRouter) (*Link, *Fake) {
	t.Helper()
	fake := NewFake(1)
	bridge := cloudops.Bridge{MachineID: "mac-01", AllowCommands: func() bool { return true }, Router: router}
	return &Link{
		relay:   &Relay{Depth: 64},
		service: Service{MachineID: "mac-01", Bridge: bridge, Transport: fake, Log: func(string, ...any) {}},
	}, fake
}

func deliver(t *testing.T, l *Link, seq uint64, object map[string]any) {
	t.Helper()
	l.relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Seq: seq},
		plaintext(t, object))
}

func placesRead(request string) map[string]any {
	return map[string]any{"type": "places", "session": cloudops.MachineReplySession, "request": request}
}

func sendCommand(text string) map[string]any {
	return map[string]any{"type": "send", "session": "%19", "request": "a-" + text, "text": text, "images": []any{}}
}

func publishedNamed(fake *Fake, name string) (Outbound, bool) {
	for _, out := range fake.Published() {
		if out.Reply.Name == name {
			return out, true
		}
	}
	return Outbound{}, false
}

// serve runs runService until the test ends, and returns a wait for it.
func serve(t *testing.T, l *Link) (stop func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = l.runService(ctx)
	}()
	stop = func() {
		cancel()
		<-finished
	}
	t.Cleanup(stop)
	return stop, finished
}

func TestAReadIsAnsweredWhileACommandBeforeItIsStuck(t *testing.T) {
	router := newGatedRouter()
	l, fake := laneLink(t, router)
	serve(t, l)

	deliver(t, l, 1, sendCommand("stuck"))
	waitFor(t, "the command to reach its route", func() bool { return len(router.seen()) == 1 })
	deliver(t, l, 2, placesRead("p1"))

	waitFor(t, "the places read to be answered behind a stuck command", func() bool {
		_, ok := publishedNamed(fake, "read:p1")
		return ok
	})
	if _, ok := publishedNamed(fake, "action:a-stuck"); ok {
		t.Fatal("the command was answered; the test meant to hold it")
	}
	close(router.gate("stuck"))
	waitFor(t, "the command's own answer", func() bool {
		_, ok := publishedNamed(fake, "action:a-stuck")
		return ok
	})
}

func TestCommandsAreStillAnsweredInTheOrderTheyArrived(t *testing.T) {
	router := newGatedRouter()
	l, fake := laneLink(t, router)
	serve(t, l)

	deliver(t, l, 1, sendCommand("first"))
	deliver(t, l, 2, sendCommand("second"))
	deliver(t, l, 3, placesRead("p1"))
	waitFor(t, "the read behind them", func() bool {
		_, ok := publishedNamed(fake, "read:p1")
		return ok
	})
	// The second command typed into a session before the first came back
	// would be text arriving out of the order it was sent in.
	time.Sleep(50 * time.Millisecond)
	if got := router.seen(); strings.Join(got, ",") != "first" {
		t.Fatalf("while the first command was out, the route saw %v", got)
	}
	close(router.gate("first"))
	waitFor(t, "the second command to start", func() bool { return len(router.seen()) == 2 })
	close(router.gate("second"))
	waitFor(t, "both answers", func() bool {
		_, ok := publishedNamed(fake, "action:a-second")
		return ok
	})
	if got := router.seen(); strings.Join(got, ",") != "first,second" {
		t.Fatalf("the route saw %v", got)
	}
}

func TestAReadPastTheBoundIsRefusedByNameAndCommandsStillGo(t *testing.T) {
	router := newGatedRouter()
	hold := make(chan struct{})
	router.holdRead = hold
	l, fake := laneLink(t, router)
	serve(t, l)
	defer close(hold)

	for i := 0; i < CloudReadConcurrencyLimit; i++ {
		deliver(t, l, uint64(10+i), placesRead(sprintf("held-%d", i)))
	}
	waitFor(t, "every slot to be taken", func() bool { return router.readsInside() == CloudReadConcurrencyLimit })

	deliver(t, l, 100, placesRead("over"))
	waitFor(t, "the read past the bound to be told", func() bool {
		_, ok := publishedNamed(fake, "read:over")
		return ok
	})
	out, _ := publishedNamed(fake, "read:over")
	if out.Reply.Status != 429 || out.Reply.Code != "cloud_ingress_busy" {
		t.Fatalf("the read past the bound was answered %d %s", out.Reply.Status, out.Reply.Code)
	}
	var payload answerPayload
	if err := json.Unmarshal(out.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Detail["limit"] != float64(CloudReadConcurrencyLimit) || payload.Error.Detail["retry_after"] != float64(1) {
		t.Fatalf("detail %v", payload.Error.Detail)
	}
	if router.readsInside() != CloudReadConcurrencyLimit {
		t.Fatalf("%d reads reached the route; the bound is %d", router.readsInside(), CloudReadConcurrencyLimit)
	}

	deliver(t, l, 101, sendCommand("go"))
	close(router.gate("go"))
	waitFor(t, "a command behind a full read lane", func() bool {
		_, ok := publishedNamed(fake, "action:a-go")
		return ok
	})
	l.mu.Lock()
	refused := l.refused
	l.mu.Unlock()
	if refused < 1 {
		t.Fatal("the refused read was not counted")
	}
}

// A queue that closes with a read still out does not end the service before
// that read has answered: Run waits for it, as it waits for a command.
func TestTheServiceWaitsForAReadStillOut(t *testing.T) {
	router := newGatedRouter()
	hold := make(chan struct{})
	router.holdRead = hold
	l, fake := laneLink(t, router)
	_, done := serve(t, l)

	deliver(t, l, 1, placesRead("late"))
	waitFor(t, "the read to reach its route", func() bool { return router.readsInside() == 1 })
	l.relay.Close()
	select {
	case <-done:
		t.Fatal("the service returned with a read still out")
	case <-time.After(50 * time.Millisecond):
	}
	close(hold)
	<-done
	if _, ok := publishedNamed(fake, "read:late"); !ok {
		t.Fatal("the read was never answered")
	}
}

// The command lane is as deep as the queue in front of it, and a command past
// that depth is told so at once: the goroutine sorting requests must not wait
// on it, or the next read would be behind it again.
func TestACommandPastTheLaneIsRefusedByNameAndReadsStillGo(t *testing.T) {
	router := newGatedRouter()
	l, fake := laneLink(t, router)
	l.relay.Depth = 2
	serve(t, l)

	deliver(t, l, 1, sendCommand("stuck"))
	waitFor(t, "the command to reach its route", func() bool { return len(router.seen()) == 1 })
	// The queue in front is as shallow as the lane, so each request is let
	// through it before the next: this test is about the lane, not the queue.
	for i, body := range []map[string]any{sendCommand("waits-1"), sendCommand("waits-2"),
		sendCommand("over"), placesRead("p1")} {
		deliver(t, l, uint64(2+i), body)
		waitFor(t, "the queue to be sorted", func() bool {
			waiting, _, _ := l.relay.Queue()
			return waiting == 0
		})
	}

	waitFor(t, "the command past the lane to be told", func() bool {
		_, ok := publishedNamed(fake, "action:a-over")
		return ok
	})
	out, _ := publishedNamed(fake, "action:a-over")
	if out.Reply.Status != 429 || out.Reply.Code != "cloud_ingress_busy" {
		t.Fatalf("the command past the lane was answered %d %s", out.Reply.Status, out.Reply.Code)
	}
	waitFor(t, "the read behind it", func() bool {
		_, ok := publishedNamed(fake, "read:p1")
		return ok
	})
	for _, text := range []string{"stuck", "waits-1", "waits-2"} {
		close(router.gate(text))
	}
	waitFor(t, "the commands that waited", func() bool {
		_, ok := publishedNamed(fake, "action:a-waits-2")
		return ok
	})
	if got := router.seen(); strings.Join(got, ",") != "stuck,waits-1,waits-2" {
		t.Fatalf("the route saw %v; the refused command must never reach it", got)
	}
}
