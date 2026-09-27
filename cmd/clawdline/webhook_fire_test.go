package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The token every test's URL carries; no output may contain it.
const webhookTestToken = "swhk_fixture_not_a_real_token_0000"

const webhookTestPath = "/v1/schedule-webhook-trigger/" + webhookTestToken

// fakeWebhookClock is time that moves only when the command sleeps.
type fakeWebhookClock struct {
	mu    sync.Mutex
	t     time.Time
	slept []time.Duration
}

func (c *fakeWebhookClock) clock() webhookClock {
	return webhookClock{
		now: func() time.Time {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.t
		},
		sleep: func(d time.Duration) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.t = c.t.Add(d)
			c.slept = append(c.slept, d)
		},
		pollFirst:  3 * time.Second,
		pollMax:    10 * time.Second,
		retryFirst: time.Second,
		retryMax:   30 * time.Second,
		client:     webhookClient(),
	}
}

// webhookSeen is one request the stand-in Cloud received.
type webhookSeen struct {
	Method, Path, Key, ContentType, Body string
}

// webhookStandIn is a Cloud that answers the trigger with trigger(n) for the
// nth POST and each status read with the next of statuses (the last one
// repeating).
type webhookStandIn struct {
	mu       sync.Mutex
	seen     []webhookSeen
	posts    int
	reads    int
	trigger  func(n int) (int, string)
	statuses [][2]any
	srv      *httptest.Server
}

func newWebhookStandIn(t *testing.T, trigger func(n int) (int, string), statuses ...[2]any) *webhookStandIn {
	s := &webhookStandIn{trigger: trigger, statuses: statuses}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.seen = append(s.seen, webhookSeen{r.Method, r.URL.Path, r.Header.Get("Idempotency-Key"), r.Header.Get("Content-Type"), string(body)})
		var code int
		var answer string
		switch {
		case r.Method == http.MethodPost && r.URL.Path == webhookTestPath:
			s.posts++
			code, answer = s.trigger(s.posts)
		case r.Method == http.MethodGet && r.URL.Path == webhookTestPath+"/deliveries/d-1":
			i := min(s.reads, len(s.statuses)-1)
			s.reads++
			code, answer = s.statuses[i][0].(int), s.statuses[i][1].(string)
		default:
			code, answer = 404, `{"error":{"code":"route_not_found"}}`
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *webhookStandIn) url() string { return s.srv.URL + webhookTestPath }

func (s *webhookStandIn) requests() []webhookSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]webhookSeen(nil), s.seen...)
}

func webhookAccept(int) (int, string) {
	return 202, `{"schema":"clawdline.schedule_webhook.accepted.v1","schedule_id":"s-1","delivery_id":"d-1","deliver_by":"2026-09-27T10:01:00Z"}`
}

func webhookState(state, outcome, refusal string) [2]any {
	q := func(s string) string {
		if s == "" {
			return "null"
		}
		return `"` + s + `"`
	}
	return [2]any{200, `{"schema":"clawdline.schedule_webhook.delivery_status.v1","delivery_id":"d-1","state":"` + state +
		`","outcome":` + q(outcome) + `,"refusal_code":` + q(refusal) +
		`,"deliver_by":"2026-09-27T10:01:00Z","updated_at":"2026-09-27T10:00:05Z"}`}
}

// fireWebhook runs the command with the URL in the environment and checks
// that nothing it printed carries the URL or its token.
func fireWebhook(t *testing.T, rawURL string, args ...string) (int, string, string, *fakeWebhookClock) {
	t.Helper()
	fc := &fakeWebhookClock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	var out, errs bytes.Buffer
	getenv := func(name string) string {
		if name == webhookURLEnv {
			return rawURL
		}
		return ""
	}
	code := runWebhookFire(&out, &errs, args, getenv, fc.clock())
	assertNoWebhookSecret(t, rawURL, out.String()+errs.String())
	return code, out.String(), errs.String(), fc
}

func assertNoWebhookSecret(t *testing.T, rawURL, printed string) {
	t.Helper()
	for _, secret := range []string{rawURL, webhookTestToken, "schedule-webhook-trigger"} {
		if secret != "" && strings.Contains(printed, secret) {
			t.Fatalf("output carries %q:\n%s", secret, printed)
		}
	}
}

// Accepted, leased, then a success: exit 0, each change on stderr, one line
// on stdout, and a POST shaped exactly as the contract says.
func TestWebhookFireSucceeds(t *testing.T) {
	s := newWebhookStandIn(t, webhookAccept,
		webhookState("queued", "", ""), webhookState("leased", "", ""),
		webhookState("dispatch_accepted", "", ""), webhookState("terminal", "success", ""))
	code, out, errs, fc := fireWebhook(t, s.url())
	if code != 0 || out != "success delivery=d-1 state=terminal outcome=success\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	for _, want := range []string{"delivery d-1: accepted", "delivery d-1: leased", "delivery d-1: dispatch_accepted", "delivery d-1: terminal"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, errs)
		}
	}
	if strings.Contains(errs, "d-1: queued") {
		t.Fatalf("an unchanged state was printed again:\n%s", errs)
	}
	seen := s.requests()
	post := seen[0]
	if post.Method != http.MethodPost || post.Body != `{"deliver_within_seconds":60}` || post.ContentType != "application/json" ||
		!regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(post.Key) {
		t.Fatalf("trigger: %+v", post)
	}
	if len(seen) != 5 || seen[1].Method != http.MethodGet || seen[1].Path != webhookTestPath+"/deliveries/d-1" {
		t.Fatalf("requests: %+v", seen)
	}
	// 3s, growing by half, to the 10s ceiling.
	if want := []time.Duration{3 * time.Second, 4500 * time.Millisecond, 6750 * time.Millisecond, 10 * time.Second}; !equalDurations(fc.slept, want) {
		t.Fatalf("slept %v, want %v", fc.slept, want)
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Every ending that is not a success, and the exit status each one is.
func TestWebhookFireEndings(t *testing.T) {
	cases := []struct {
		name  string
		state [2]any
		code  int
		out   string
	}{
		{"timed out", webhookState("terminal", "timed_out", ""), 1, "failed delivery=d-1 state=terminal outcome=timed_out\n"},
		{"spawn failed", webhookState("terminal", "spawn_failed", ""), 1, "failed delivery=d-1 state=terminal outcome=spawn_failed\n"},
		{"expired", webhookState("expired", "", ""), 2, "not_delivered delivery=d-1 state=expired\n"},
		{"canceled", webhookState("canceled", "", ""), 2, "not_delivered delivery=d-1 state=canceled\n"},
		{"refused", webhookState("dispatch_refused", "", "capacity_full"), 3, "refused delivery=d-1 state=dispatch_refused refusal_code=capacity_full\n"},
		{"webhook gone while waiting", [2]any{404, `{"error":{"code":"webhook_unavailable","message":"gone"}}`}, 3, "refused delivery=d-1 code=webhook_unavailable status=404\n"},
		{"delivery unknown", [2]any{404, `{"error":"not_found"}`}, 2, "not_delivered delivery=d-1 state=unknown code=not_found\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newWebhookStandIn(t, webhookAccept, webhookState("leased", "", ""), c.state)
			code, out, errs, _ := fireWebhook(t, s.url())
			if code != c.code || out != c.out {
				t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
			}
		})
	}
}

// The trigger refused: the webhook is gone, the caller is over its limit, the
// request was bad. None of them is sent again.
func TestWebhookFireTriggerRefused(t *testing.T) {
	cases := []struct {
		status int
		body   string
		out    string
	}{
		{404, `{"error":{"code":"webhook_unavailable","message":"This webhook is not available."}}`, "refused delivery=- code=webhook_unavailable status=404\n"},
		{429, `{"error":{"code":"rate_limited"}}`, "refused delivery=- code=rate_limited status=429\n"},
		{400, `{"code":"bad_request"}`, "refused delivery=- code=bad_request status=400\n"},
	}
	for _, c := range cases {
		s := newWebhookStandIn(t, func(int) (int, string) { return c.status, c.body })
		code, out, errs, _ := fireWebhook(t, s.url())
		if code != 3 || out != c.out {
			t.Fatalf("%d: exit %d, stdout %q, stderr:\n%s", c.status, code, out, errs)
		}
		if n := len(s.requests()); n != 1 {
			t.Fatalf("%d: sent %d times", c.status, n)
		}
	}
}

// A 503 and then a 202 is one delivery: the second send carries the first's
// Idempotency-Key.
func TestWebhookFireRetriesWithTheSameKey(t *testing.T) {
	s := newWebhookStandIn(t, func(n int) (int, string) {
		if n == 1 {
			return 503, `{"error":{"code":"unavailable"}}`
		}
		return webhookAccept(n)
	})
	code, out, errs, fc := fireWebhook(t, s.url(), "--no-wait")
	if code != 0 || out != "d-1\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	seen := s.requests()
	if len(seen) != 2 || seen[0].Key == "" || seen[0].Key != seen[1].Key || seen[0].Body != seen[1].Body {
		t.Fatalf("requests: %+v", seen)
	}
	if !equalDurations(fc.slept, []time.Duration{time.Second}) {
		t.Fatalf("slept %v", fc.slept)
	}
}

// 5xx until the deliver-within deadline: never reached the machine, exit 2,
// after 1s, 2s and 4s of backoff (the next 8s would pass the 10s deadline),
// every send with the one key.
func TestWebhookFireGivesUpOnAnUnreachableTrigger(t *testing.T) {
	s := newWebhookStandIn(t, func(int) (int, string) { return 502, "bad gateway" })
	code, out, errs, fc := fireWebhook(t, s.url(), "--deliver-within", "10s")
	if code != 2 || out != "not_delivered delivery=- reason=unreachable\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	seen := s.requests()
	if len(seen) != 4 {
		t.Fatalf("sent %d times", len(seen))
	}
	for _, r := range seen {
		if r.Key != seen[0].Key || r.Body != `{"deliver_within_seconds":10}` {
			t.Fatalf("requests: %+v", seen)
		}
	}
	if !equalDurations(fc.slept, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Fatalf("slept %v", fc.slept)
	}
}

// Nothing listening: exit 2, and neither the URL, its token nor the address
// it dialled is printed — a net/http error quotes all three.
func TestWebhookFireUnreachableSaysNothingOfTheURL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	rawURL := "http://" + addr + webhookTestPath
	code, out, errs, _ := fireWebhook(t, rawURL, "--deliver-within", "10s")
	if code != 2 || out != "not_delivered delivery=- reason=unreachable\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	if strings.Contains(out+errs, addr) {
		t.Fatalf("output carries the address:\n%s", errs)
	}
	if !strings.Contains(errs, "attempt 1 failed (") {
		t.Fatalf("stderr does not say why:\n%s", errs)
	}
}

// --timeout reached with the delivery still on its way: exit 4 with the last
// state it was seen in. 429s and 5xx on the way are read through.
func TestWebhookFireGivesUpWaiting(t *testing.T) {
	s := newWebhookStandIn(t, webhookAccept,
		webhookState("leased", "", ""), [2]any{429, `{"error":{"code":"rate_limited"}}`},
		[2]any{500, "oops"}, webhookState("dispatch_deferred", "", ""))
	code, out, errs, fc := fireWebhook(t, s.url(), "--timeout", "40s")
	if code != 4 || out != "gave_up delivery=d-1 state=dispatch_deferred\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	var total time.Duration
	for _, d := range fc.slept {
		total += d
	}
	if total != 40*time.Second {
		t.Fatalf("waited %v in all: %v", total, fc.slept)
	}
	// After the 429 the poll went straight to its ceiling.
	if fc.slept[2] != 10*time.Second {
		t.Fatalf("slept %v", fc.slept)
	}
}

// --deliver-within is sent as whole seconds, and refused outside 10s–24h.
func TestWebhookFireDeliverWithin(t *testing.T) {
	s := newWebhookStandIn(t, webhookAccept)
	code, out, errs, _ := fireWebhook(t, s.url(), "--deliver-within", "2m30s", "--no-wait")
	if code != 0 || out != "d-1\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	if body := s.requests()[0].Body; body != `{"deliver_within_seconds":150}` {
		t.Fatalf("body %s", body)
	}
	for _, bad := range []string{"9s", "25h", "soon", "-10s"} {
		if code, _, _, _ := fireWebhook(t, s.url(), "--deliver-within", bad); code != 64 {
			t.Fatalf("--deliver-within %s: exit %d", bad, code)
		}
	}
	if n := len(s.requests()); n != 1 {
		t.Fatalf("a refused flag sent anyway: %d requests", n)
	}
}

// The URL as an argument is refused without being repeated; so is a URL that
// is not https to somewhere else, and no URL at all.
func TestWebhookFireRefusesTheURLOutsideAFileOrTheEnvironment(t *testing.T) {
	s := newWebhookStandIn(t, webhookAccept)
	code, out, errs, _ := fireWebhook(t, "", s.url())
	if code != 64 || out != "" || !strings.Contains(errs, "takes no arguments") {
		t.Fatalf("positional: exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	assertNoWebhookSecret(t, s.url(), errs)
	code, _, errs, _ = fireWebhook(t, "", "--url", s.url())
	if code != 64 {
		t.Fatalf("unknown flag: exit %d", code)
	}
	assertNoWebhookSecret(t, s.url(), errs)
	if code, _, errs, _ = fireWebhook(t, ""); code != 64 || !strings.Contains(errs, webhookURLEnv) {
		t.Fatalf("no URL: exit %d, %s", code, errs)
	}
	plain := "http://example.com" + webhookTestPath
	if code, _, errs, _ = fireWebhook(t, plain); code != 64 || !strings.Contains(errs, "must be https") {
		t.Fatalf("plain http: exit %d, %s", code, errs)
	}
	if n := len(s.requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

// --url-file is read and trimmed, and wins over the environment.
func TestWebhookFireReadsTheURLFile(t *testing.T) {
	s := newWebhookStandIn(t, webhookAccept)
	path := filepath.Join(t.TempDir(), "webhook-url")
	if err := os.WriteFile(path, []byte("  "+s.url()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs, _ := fireWebhook(t, "https://example.invalid/elsewhere", "--url-file", path, "--no-wait")
	if code != 0 || out != "d-1\n" {
		t.Fatalf("exit %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	assertNoWebhookSecret(t, s.url(), out+errs)
}

// A refusal's code is read from either envelope, or a bare code.
func TestWebhookErrorCodeReadsEveryEnvelope(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"code":"webhook_unavailable","message":"m"}}`: "webhook_unavailable",
		`{"error":"not_found","detail":"d"}`:                     "not_found",
		`{"code":"rate_limited"}`:                                "rate_limited",
		`{"error":{"code":"has space"}}`:                         "-",
		`<html>`:                                                 "-",
		``:                                                       "-",
	} {
		if got := webhookErrorCode([]byte(body)); got != want {
			t.Fatalf("%s: %q, want %q", body, got, want)
		}
	}
}
