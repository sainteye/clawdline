package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
)

func plaintext(t *testing.T, object map[string]any) []byte {
	t.Helper()
	out, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("that body is not JSON: %v", err)
	}
	return out
}

// answering is a bridge with a router that says yes to everything, which is
// enough for the questions this file asks: they are about addresses and
// delivery, not about what an operation means.
func answering(t *testing.T) cloudops.Bridge {
	t.Helper()
	return cloudops.Bridge{MachineID: "mac-01", AllowCommands: func() bool { return true },
		Router: routerFunc(func(context.Context, cloudops.LocalRequest) (cloudops.LocalResponse, error) {
			return cloudops.LocalResponse{Status: 200, Body: []byte(`{"ok":true}`)}, nil
		})}
}

type routerFunc func(context.Context, cloudops.LocalRequest) (cloudops.LocalResponse, error)

func (f routerFunc) Do(ctx context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	return f(ctx, req)
}

// TestAnAnswerRidesTheSessionsOwnTranscriptChannel. `t/<machine>/<session>` is
// where the viewer is already subscribed; a new prefix would need a relay this
// repository does not contain.
func TestAnAnswerRidesTheSessionsOwnTranscriptChannel(t *testing.T) {
	fake := NewFake(4)
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(string, ...any) {}}
	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl",
		Sender: "viewer-device-01", Sequence: 411,
		Plaintext: plaintext(t, map[string]any{"type": "git", "session": "%19"})})
	published := fake.Published()
	if len(published) != 1 {
		t.Fatalf("published %d answers, wanted one", len(published))
	}
	out := published[0]
	if out.Channel != "t/mac-01/%2519" {
		t.Fatalf("published on %q, wanted t/mac-01/%%2519", out.Channel)
	}
	if out.Class != "stream" {
		t.Fatalf("published as class %q, wanted stream", out.Class)
	}
	if out.Reply.Name != "git" || out.Reply.Sender != "viewer-device-01" || out.Reply.Sequence != 411 {
		t.Fatalf("the answer lost who it is for: %+v", out.Reply)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Payload, &payload); err != nil {
		t.Fatalf("that payload is not JSON: %v", err)
	}
	if payload["read"] != "git" || payload["status"] != float64(200) {
		t.Fatalf("the payload is %s", out.Payload)
	}
}

// A Cloud mutation is an externally initiated machine change. Its typed
// outcome must be readable later without decrypting or logging its body.
func TestACloudCommandThatRanLeavesOneAuditLine(t *testing.T) {
	fake := NewFake(4)
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl",
		Sender: "viewer-device-01", Sequence: 414,
		Plaintext: plaintext(t, map[string]any{"type": "end", "session": "%19", "request": "req-end",
			"accept_loss": false, "expected_closeability_version": ""})})
	if len(lines) != 1 {
		t.Fatalf("a successful Cloud command left %d lines: %v", len(lines), lines)
	}
	for _, wanted := range []string{"operation=end", "session=%19", "status=200", "code=ok",
		"sender=viewer-device-01", "seq=414"} {
		if !strings.Contains(lines[0], wanted) {
			t.Errorf("log %q does not contain %q", lines[0], wanted)
		}
	}
	if strings.Contains(lines[0], "req-end") || strings.Contains(lines[0], "accept_loss") {
		t.Fatalf("the log contains command-body data: %q", lines[0])
	}
}

// TestAnAnswerWithNowhereToGoIsRecordedRatherThanSent.
func TestAnAnswerWithNowhereToGoIsRecordedRatherThanSent(t *testing.T) {
	fake := NewFake(4)
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	// Addressed to another Mac: the viewer listens on the channel it named,
	// and an answer on ours would be read by nobody.
	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-02", Class: "ctl",
		Sender: "viewer-device-01", Sequence: 412,
		Plaintext: plaintext(t, map[string]any{"type": "git", "session": "%19"})})
	if len(fake.Published()) != 0 {
		t.Fatalf("an answer for another machine was published: %+v", fake.Published())
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "answered nobody") {
		t.Fatalf("that drop was not recorded: %v", lines)
	}
	// And it names the session. Without that, two menu answers refused on
	// 2026-09-20 recorded a code and a sender and nothing that said which
	// session had been left unable to answer. A body too malformed to name one
	// reads as a word rather than as a gap, which would look like a session
	// called "".
	if !strings.Contains(lines[0], "session=unknown") {
		t.Fatalf("a recorded drop does not name its session: %q", lines[0])
	}
}

// TestAPublicationThatCannotLeaveIsSaidOutLoud: the answer's own channel is
// the only way back to the asker, so a failure there is recorded rather than
// retried into a second effect.
func TestAPublicationThatCannotLeaveIsSaidOutLoud(t *testing.T) {
	fake := NewFake(4)
	fake.Fail = errors.New("the socket is closed")
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(format string, args ...any) { lines = append(lines, format) }}
	answer := service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl",
		Sender: "viewer-device-01", Sequence: 413,
		Plaintext: plaintext(t, map[string]any{"type": "git", "session": "%19"})})
	if !answer.Published() {
		t.Fatalf("the bridge decided nothing: %+v", answer)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "not delivered") {
		t.Fatalf("an undelivered answer was not recorded: %v", lines)
	}
}

// TestRunDrainsUntilTheTransportIsFinished.
func TestRunDrainsUntilTheTransportIsFinished(t *testing.T) {
	fake := NewFake(4)
	for _, session := range []string{"%19", "%18"} {
		fake.Deliver(Inbound{Channel: "ctl/mac-01", Class: "ctl", Sender: "viewer-device-01",
			Sequence: 1, Plaintext: plaintext(t, map[string]any{"type": "git", "session": session})})
	}
	fake.Close()
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(string, ...any) {}}
	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run ended with %v", err)
	}
	if len(fake.Published()) != 2 {
		t.Fatalf("published %d answers, wanted two", len(fake.Published()))
	}
}

// TestTheRouterDispatchesInThisProcess. Not a socket, and everything behind
// the handler is kept: the path as spelled, the query, the body, the headers
// the gate reads.
func TestTheRouterDispatchesInThisProcess(t *testing.T) {
	var seen *http.Request
	var body []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PNG"))
	})
	router := Router{Handler: handler, Authorize: func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer a-token")
	}}
	res, err := router.Do(context.Background(), cloudops.LocalRequest{
		Method: "POST", Path: "/v1/sessions/%2519/send",
		Query:  map[string]string{"limit": "200"},
		Header: map[string]string{"Idempotency-Key": "req-send"},
		Body:   []byte(`{"text":"hello"}`)})
	if err != nil {
		t.Fatalf("the router failed: %v", err)
	}
	if res.Status != 200 || string(res.Body) != "PNG" || res.ContentType != "image/png" {
		t.Fatalf("the answer is %+v", res)
	}
	if seen.URL.EscapedPath() != "/v1/sessions/%2519/send" {
		t.Fatalf("the path arrived as %q", seen.URL.EscapedPath())
	}
	if seen.URL.Query().Get("limit") != "200" {
		t.Fatalf("the query arrived as %q", seen.URL.RawQuery)
	}
	if string(body) != `{"text":"hello"}` {
		t.Fatalf("the body arrived as %q", body)
	}
	if seen.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("a change without a JSON content type: %q", seen.Header.Get("Content-Type"))
	}
	if seen.Header.Get("Idempotency-Key") != "req-send" {
		t.Fatalf("the idempotency key did not arrive: %q", seen.Header.Get("Idempotency-Key"))
	}
	if seen.Header.Get("Authorization") != "Bearer a-token" {
		t.Fatalf("the credential did not arrive: %q", seen.Header.Get("Authorization"))
	}
	if seen.Host != "127.0.0.1" {
		t.Fatalf("the Host is %q, which the rebinding check would refuse", seen.Host)
	}
}

// TestARouterWithNoCredentialIsRefusedByTheGate is the default being the safe
// one: wiring that forgets to say who this viewer is gets the gate's own no,
// not an answer.
func TestARouterWithNoCredentialIsRefusedByTheGate(t *testing.T) {
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"This needs a paired device."}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	res, err := Router{Handler: gate}.Do(context.Background(),
		cloudops.LocalRequest{Method: "GET", Path: "/v1/places"})
	if err != nil {
		t.Fatalf("the router failed: %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("status %d, wanted 401", res.Status)
	}
}

// TestARouterWithNoHandlerSaysSo rather than answering an empty success.
func TestARouterWithNoHandlerSaysSo(t *testing.T) {
	if _, err := (Router{}).Do(context.Background(),
		cloudops.LocalRequest{Method: "GET", Path: "/v1/places"}); err == nil {
		t.Fatal("a router with nothing behind it answered")
	}
}

// TestAHandlerThatWroteNothingIsNotARefusal: Go's own server answers 200 with
// an empty body there, and inventing a refusal would report a failure the
// route did not make.
func TestAHandlerThatWroteNothingIsNotARefusal(t *testing.T) {
	res, err := Router{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}.
		Do(context.Background(), cloudops.LocalRequest{Method: "GET", Path: "/v1/places"})
	if err != nil || res.Status != 200 || len(res.Body) != 0 {
		t.Fatalf("answered %+v, %v", res, err)
	}
}

// A Cloud read left no line at all on this machine, so a phone that showed
// "loading" for minutes could not be matched to anything the Mac did. A read
// that was refused or slow is one line; a fast 2xx read stays silent, because
// reads are the frequent half of the traffic.
func TestACloudReadIsRecordedOnlyWhenRefusedOrSlow(t *testing.T) {
	read := func(status int, took time.Duration) []string {
		t.Helper()
		var lines []string
		clock := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
		bridge := cloudops.Bridge{MachineID: "mac-01", AllowCommands: func() bool { return true },
			Router: routerFunc(func(context.Context, cloudops.LocalRequest) (cloudops.LocalResponse, error) {
				clock = clock.Add(took)
				if status != 200 {
					return cloudops.LocalResponse{Status: status,
						Body: []byte(`{"error":"session_not_found","detail":"no such terminal"}`)}, nil
				}
				return cloudops.LocalResponse{Status: 200, Body: []byte(`{"direct_todos":[]}`)}, nil
			})}
		service := Service{MachineID: "mac-01", Bridge: bridge, Transport: NewFake(4),
			Now: func() time.Time { return clock },
			Log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
		service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl",
			Sender: "viewer-device-01", Sequence: 520,
			Plaintext: plaintext(t, map[string]any{"type": "work.v2.session-todos",
				"session": "__clawdline_machine__", "request": "req-todos", "terminal": "%19"})})
		return lines
	}

	if lines := read(200, 40*time.Millisecond); len(lines) != 0 {
		t.Fatalf("a fast successful read left %d lines: %v", len(lines), lines)
	}
	if lines := read(200, slowReadAnswer); len(lines) != 0 {
		t.Fatalf("a read at exactly the threshold left %d lines: %v", len(lines), lines)
	}
	slow := read(200, 6200*time.Millisecond)
	if len(slow) != 1 {
		t.Fatalf("a slow read left %d lines: %v", len(slow), slow)
	}
	for _, wanted := range []string{"cloud: read answered:", "operation=work.v2.session-todos", "status=200",
		"code=ok", "ms=6200", "sender=viewer-device-01", "seq=520"} {
		if !strings.Contains(slow[0], wanted) {
			t.Errorf("log %q does not contain %q", slow[0], wanted)
		}
	}
	refused := read(404, 10*time.Millisecond)
	if len(refused) != 1 {
		t.Fatalf("a refused read left %d lines: %v", len(refused), refused)
	}
	for _, wanted := range []string{"operation=work.v2.session-todos", "status=404", "code=session_not_found", "ms=10"} {
		if !strings.Contains(refused[0], wanted) {
			t.Errorf("log %q does not contain %q", refused[0], wanted)
		}
	}
	for _, line := range append(slow, refused...) {
		if strings.Contains(line, "req-todos") || strings.Contains(line, "%19") || strings.Contains(line, "no such terminal") {
			t.Fatalf("the log contains request or answer body data: %q", line)
		}
	}
}

// A read whose answer did not fit its channel is the other way a phone waits
// the whole read timeout; the line that says so names which read it was.
func TestAReadThatDidNotFitNamesItsOperation(t *testing.T) {
	fake := NewFake(4)
	fake.Fail = errors.New("the outbound spool is full")
	fake.FailIsChannelFull = true
	var lines []string
	service := Service{MachineID: "mac-01", Bridge: answering(t), Transport: fake,
		Log: func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }}
	service.Answer(context.Background(), Inbound{Channel: "ctl/mac-01", Class: "ctl",
		Sender: "viewer-device-01", Sequence: 521,
		Plaintext: plaintext(t, map[string]any{"type": "work.v2.session-todos",
			"session": "__clawdline_machine__", "request": "req-todos", "terminal": "%19"})})
	if len(lines) != 1 || !strings.Contains(lines[0], "did not fit its channel") ||
		!strings.Contains(lines[0], "operation=work.v2.session-todos") {
		t.Fatalf("the channel-full line does not name the read: %v", lines)
	}
}
