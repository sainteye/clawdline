package cloud

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// These are failure-injection tests for the machine-read reply channel,
// `t/<machine>/__clawdline_machine__`, run against a real Relay and a real
// Spool at production limits. Nothing drains the spool, so every answer it
// admitted is still owed when the next read arrives: that is the state the
// phone was in on 2026-09-25, when a few full-size reference images in flight
// held the channel at 3.2–4.0 MB of its 4 MiB and every read behind them was
// either refused once or not answered at all.

// recordingRelay is a real Relay that also keeps what it accepted, so a test
// can read which waiter was told what without opening the ciphertext.
type recordingRelay struct {
	*Relay
	mu       sync.Mutex
	accepted []Outbound
}

func (r *recordingRelay) Publish(ctx context.Context, out Outbound) error {
	if err := r.Relay.Publish(ctx, out); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accepted = append(r.accepted, out)
	return nil
}

func (r *recordingRelay) answers() map[string]Outbound {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]Outbound{}
	for _, a := range r.accepted {
		out[a.Reply.Name] = a
	}
	return out
}

func newRecordingRelay(t *testing.T, limits adaptercloud.SpoolLimits) (*recordingRelay, *adaptercloud.Spool) {
	t.Helper()
	spool, err := adaptercloud.NewSpool(limits, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// The transport is never run: Publish reserves, seals and hands the row
	// to the spool, and nothing writes it, so nothing is ever answered.
	relay := &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "mac-01",
		Signer: signer, Secret: secret}
	return &recordingRelay{Relay: relay}, spool
}

// imageBridge answers every reference image with a PNG-typed body of
// imageBytes, and a Session's to-dos with a JSON body of todoBytes.
func imageBridge(imageBytes, todoBytes int) cloudops.Bridge {
	picture := make([]byte, imageBytes)
	todos := []byte(`{"ok":true,"todos":[],"pad":"` + strings.Repeat("x", todoBytes) + `"}`)
	return cloudops.Bridge{MachineID: "mac-01", AllowCommands: func() bool { return true },
		Router: routerFunc(func(_ context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
			if strings.HasPrefix(req.Path, "/v1/work/v2/images/") {
				return cloudops.LocalResponse{Status: 200, ContentType: "image/png", Body: picture}, nil
			}
			return cloudops.LocalResponse{Status: 200, ContentType: "application/json", Body: todos}, nil
		})}
}

func imageRead(t *testing.T, request string, seq uint64) Inbound {
	return Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: seq,
		Plaintext: plaintext(t, map[string]any{"type": "work.v2.image", "session": cloudops.MachineReplySession,
			"request": request, "id": "img-" + request})}
}

// Five full-size reference images are asked for, then a Session's to-dos. The
// to-dos are answered — not refused, and not left to a sixty-second silence —
// and every image that could not go was told so under its own read id.
//
// The sizes are chosen so the rule that existed before cannot pass: 1,040,000
// bytes of PNG is 1,386,668 as base64, three of them leave 34 KB of the 4 MiB
// channel, and a 40 KB to-do list does not fit in that. Only a reserve kept
// for small answers lets it through.
func TestFiveImagesInFlightDoNotStarveTheTodoRead(t *testing.T) {
	relay, spool := newRecordingRelay(t, adaptercloud.DefaultSpoolLimits())
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: imageBridge(1_040_000, 40_000), Transport: relay,
		Log: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		service.Answer(ctx, imageRead(t, fmt.Sprintf("img%d", i), uint64(i)))
	}
	// The admitted answers go on the wire and are not answered.
	for d := spool.SendNext(); d.Row != nil; d = spool.SendNext() {
	}
	service.Answer(ctx, Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 6,
		Plaintext: plaintext(t, map[string]any{"type": "work.v2.session-todos", "session": cloudops.MachineReplySession,
			"request": "todos", "terminal": "term-1"})})

	answers := relay.answers()
	todos, ok := answers["read:todos"]
	if !ok {
		t.Fatalf("the to-do read got no answer; the log says %q", lines)
	}
	if todos.Refusal || todos.Reply.Status != 200 {
		t.Fatalf("the to-do read was answered %d %s, not with its to-dos", todos.Reply.Status, todos.Reply.Code)
	}
	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("read:img%d", i)
		a, ok := answers[name]
		if !ok {
			t.Fatalf("%s got no answer at all; the log says %q", name, lines)
		}
		if a.Channel != "t/mac-01/"+cloudops.MachineReplySession {
			t.Fatalf("%s answered on %s", name, a.Channel)
		}
		if a.Refusal && (a.Reply.Status != 429 || a.Reply.Code != "cloud_read_busy") {
			t.Fatalf("%s was refused %d %s, not busy", name, a.Reply.Status, a.Reply.Code)
		}
	}
	// The first two go whole and the rest are told to come back: the reserve
	// is what refused the third, not the channel's cap.
	if answers["read:img1"].Refusal || answers["read:img2"].Refusal || !answers["read:img3"].Refusal {
		t.Fatalf("which images went whole: %v %v %v", !answers["read:img1"].Refusal,
			!answers["read:img2"].Refusal, !answers["read:img3"].Refusal)
	}
	if strings.Contains(strings.Join(lines, "\n"), "the refusal did not either") {
		t.Fatalf("a waiter was left with nothing: %q", lines)
	}
}

// While the channel is full, every refused read is told, under its own id.
// The rule this replaces admitted one live refusal per channel, so the first
// was told and every later one waited out its browser's timeout.
func TestEveryReadRefusedOnAFullChannelGetsItsOwnRefusal(t *testing.T) {
	relay, _ := newRecordingRelay(t, adaptercloud.DefaultSpoolLimits())
	var lines []string
	// 3,000,000 bytes is 4,000,000 as base64: on its own it may go, and it
	// leaves no room for another picture.
	service := Service{MachineID: "mac-01", Bridge: imageBridge(3_000_000, 10), Transport: relay,
		Log: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	ctx := context.Background()
	service.Answer(ctx, imageRead(t, "first", 1))
	if a, ok := relay.answers()["read:first"]; !ok || a.Refusal {
		t.Fatalf("a picture the size of the channel could not go on an idle one: %q", lines)
	}

	const n = 12
	for i := 0; i < n; i++ {
		service.Answer(ctx, imageRead(t, fmt.Sprintf("r%d", i), uint64(10+i)))
	}
	answers := relay.answers()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("read:r%d", i)
		a, ok := answers[name]
		if !ok {
			t.Fatalf("%s got no answer; the log says %q", name, lines)
		}
		if !a.Refusal || a.Reply.Code != "cloud_read_busy" || a.Reply.Sequence != uint64(10+i) {
			t.Fatalf("%s was answered %+v", name, a.Reply)
		}
	}
	if len(answers) != n+1 {
		t.Fatalf("%d answers for %d reads", len(answers), n+1)
	}
}

// The refusals are bounded. Past `cloud.spool_refusals` a refusal is dropped
// — and that drop is the one silence left, so it is logged with the read's
// operation and its sender.
func TestTheRefusalCapBoundsWhatAFloodHolds(t *testing.T) {
	limits := adaptercloud.DefaultSpoolLimits()
	limits.RecipientRefusalCap = 3
	relay, spool := newRecordingRelay(t, limits)
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: imageBridge(3_000_000, 10), Transport: relay,
		Log: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}
	ctx := context.Background()
	service.Answer(ctx, imageRead(t, "first", 1))
	for i := 0; i < 5; i++ {
		service.Answer(ctx, imageRead(t, fmt.Sprintf("r%d", i), uint64(10+i)))
	}
	refused := 0
	for _, a := range relay.answers() {
		if a.Refusal {
			refused++
		}
	}
	if refused != 3 {
		t.Fatalf("%d refusals were admitted past a cap of 3", refused)
	}
	reading := spool.RefusalReading()
	if reading.Used != 3 || reading.Counters.Refused != 2 {
		t.Fatalf("the refusal row reads %+v", reading)
	}
	dropped := 0
	for _, line := range lines {
		if strings.Contains(line, "the refusal did not either") {
			dropped++
			if !strings.Contains(line, "operation=work.v2.image") || !strings.Contains(line, "sender=viewer-01") {
				t.Fatalf("a dropped refusal is logged without whose it was: %q", line)
			}
		}
	}
	if dropped != 2 {
		t.Fatalf("%d drops logged, wanted 2: %q", dropped, lines)
	}
}
