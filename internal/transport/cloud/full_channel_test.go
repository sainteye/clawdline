package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

// answerPayload is the shape every answer on this transport has.
type answerPayload struct {
	Read   string `json:"read"`
	Status int    `json:"status"`
	Error  struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Layer   string         `json:"layer"`
		Seq     uint64         `json:"seq"`
		Detail  map[string]any `json:"detail"`
	} `json:"error"`
}

// A read whose answer does not fit its channel is answered, not logged.
//
// This was the whole of the failure: 34 log lines on one machine over one
// evening, and on the phone a card that said "loading" until the browser's own
// sixty-second read timeout gave up. The answer cannot go — that is what full
// means — but the sentence saying so can, and it is the only thing that makes
// the person stop waiting.
func TestAReadWhoseAnswerDoesNotFitIsAnsweredAndNotJustLogged(t *testing.T) {
	fake := NewFake(1)
	fake.Fail = adaptercloud.ErrSpoolCapacity
	fake.FailIsChannelFull = true
	fake.ChannelBytes = 4 << 20
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(f string, a ...any) { lines = append(lines, sprintf(f, a...)) }}

	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 91,
		Plaintext: plaintext(t, map[string]any{"type": "transcript", "session": "%19", "limit": 1000})})

	published := fake.Published()
	if len(published) != 1 {
		t.Fatalf("published %d things; the answer did not fit and the refusal had to", len(published))
	}
	out := published[0]
	if !out.Refusal {
		t.Fatal("the refusal was not marked as one, so it would be admitted through the same full cap as the answer")
	}
	if out.Channel != "t/mac-01/%2519" || out.Reply.Code != "cloud_read_busy" || out.Reply.Status != 429 {
		t.Fatalf("answered %q %d %s", out.Channel, out.Reply.Status, out.Reply.Code)
	}
	var payload answerPayload
	if err := json.Unmarshal(out.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Read != "transcript" || payload.Status != 429 || payload.Error.Code != "cloud_read_busy" ||
		payload.Error.Layer != "mac_transport" || payload.Error.Seq != 91 {
		t.Fatalf("payload %s", out.Payload)
	}
	// What a person can do about it is a number and a wait, not an adjective.
	if payload.Error.Detail["lane"] != "egress" || payload.Error.Detail["limit"] != float64(4<<20) ||
		payload.Error.Detail["retry_after"] != float64(5) {
		t.Fatalf("detail %v", payload.Error.Detail)
	}
	// One typed refusal is small enough for the reserve that admitted it.
	if len(out.Payload) > 4<<10 {
		t.Fatalf("the refusal is %d bytes, past the reserve it is admitted under", len(out.Payload))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "its sender was told cloud_read_busy") {
		t.Fatalf("the log says %q", lines)
	}
}

// A command's answer is not a busy signal. The effect already happened, so
// the sender is told its reply was lost and the console does not retry it —
// which is the difference between a refused read and a command run twice.
func TestACommandWhoseReplyDoesNotFitSaysTheReplyWasLost(t *testing.T) {
	fake := NewFake(1)
	fake.Fail = adaptercloud.ErrSpoolFairness
	fake.FailIsChannelFull = true
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake, Log: func(string, ...any) {}}

	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 92,
		Plaintext: plaintext(t, map[string]any{"type": "send", "session": "%19", "request": "r1", "text": "hi"})})

	published := fake.Published()
	if len(published) != 1 {
		t.Fatalf("published %d things", len(published))
	}
	var payload answerPayload
	if err := json.Unmarshal(published[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != 503 || payload.Error.Code != "command_answer_undeliverable" ||
		payload.Error.Layer != "mac_reply" {
		t.Fatalf("payload %s", published[0].Payload)
	}
	if len(payload.Error.Detail) != 0 {
		t.Fatalf("a lost reply carries no detail the reader would keep: %v", payload.Error.Detail)
	}
}

// A line that is down is not a full channel, and must not be dressed as one:
// "try again shortly" about a machine that is offline is a sentence that sends
// somebody back to a card that will not change.
func TestOnlyAFullChannelIsAnsweredAsOne(t *testing.T) {
	fake := NewFake(1)
	fake.Fail = ErrRelayNotReady
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(f string, a ...any) { lines = append(lines, sprintf(f, a...)) }}

	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 93,
		Plaintext: plaintext(t, map[string]any{"type": "transcript", "session": "%19", "limit": 50})})

	if len(fake.Published()) != 0 {
		t.Fatalf("a line that is down published %+v", fake.Published())
	}
	if !strings.Contains(strings.Join(lines, "\n"), "was not delivered") {
		t.Fatalf("the log says %q", lines)
	}
}

// When the reserve itself cannot take the refusal, that is the one case where
// the waiter is told nothing — and it says so in those words, rather than
// looking like the answer that simply did not fit.
func TestARefusalThatDoesNotFitEitherSaysSo(t *testing.T) {
	fake := NewFake(1)
	fake.Fail = adaptercloud.ErrSpoolCapacity
	fake.FailIsChannelFull = true
	fake.FailRefusals = true
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(f string, a ...any) { lines = append(lines, sprintf(f, a...)) }}

	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-01", Sequence: 94,
		Plaintext: plaintext(t, map[string]any{"type": "transcript", "session": "%19", "limit": 50})})

	if len(fake.Published()) != 0 {
		t.Fatalf("published %+v", fake.Published())
	}
	if !strings.Contains(strings.Join(lines, "\n"), "the refusal did not either") {
		t.Fatalf("the log says %q", lines)
	}
}

// sprintf is fmt.Sprintf, named here so a test's Log is one expression.
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
