package projectassistantwebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// testSecret is synthetic and, like upstream keys, 64 characters long.
var testSecret = strings.Repeat("k7", 32)

// upstreamBounds are the values pinned in Codync hooks.rs (64 KiB body,
// 200-byte delivery id) plus test-only choices for the two bounds upstream
// leaves implicit. The product registers its own when it wires the module.
var upstreamBounds = Limits{BodyBytes: 64 * 1024, DeliveryIDBytes: 200, GitHubEventNameBytes: 200, SecretMinBytes: 32}

func headers(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(pairs); i += 2 {
		h.Add(pairs[i], pairs[i+1])
	}
	return h
}

func sha256Sig(key string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func sha1Sig(key string, body []byte) string {
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write(body)
	return "sha1=" + hex.EncodeToString(mac.Sum(nil))
}

func reason(t *testing.T, err error, code Code) string {
	t.Helper()
	f := FailureOf(err)
	if f == nil || f.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
	return f.Reason
}

func TestAuthenticateBearerAndSignatureOverExactBytes(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	accepted := map[string]http.Header{
		"bearer":                    headers("Authorization", "Bearer "+testSecret),
		"signature":                 headers("X-Hub-Signature-256", sha256Sig(testSecret, body)),
		"uppercase hex":             headers("X-Hub-Signature-256", "sha256="+strings.ToUpper(strings.TrimPrefix(sha256Sig(testSecret, body), "sha256="))),
		"wrong bearer, good sig":    headers("Authorization", "Bearer nope", "X-Hub-Signature-256", sha256Sig(testSecret, body)),
		"other auth scheme and sig": headers("Authorization", "Basic abc", "X-Hub-Signature-256", sha256Sig(testSecret, body)),
	}
	lower := http.Header{}
	lower.Set("x-hub-signature-256", sha256Sig(testSecret, body))
	accepted["lowercase header names"] = lower
	for name, h := range accepted {
		if err := Authenticate(testSecret, h, body, upstreamBounds); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	refused := []struct {
		name   string
		secret string
		h      http.Header
		body   []byte
		why    string
	}{
		{"no proof", testSecret, headers(), body, "proof_missing"},
		{"wrong bearer", testSecret, headers("Authorization", "Bearer nope"), body, "bearer_mismatch"},
		{"bearer prefix only", testSecret, headers("Authorization", "Bearer "+testSecret[:63]), body, "bearer_mismatch"},
		{"bearer with suffix", testSecret, headers("Authorization", "Bearer "+testSecret+" "), body, "bearer_mismatch"},
		{"lowercase scheme", testSecret, headers("Authorization", "bearer "+testSecret), body, "proof_missing"},
		{"changed bytes", testSecret, headers("X-Hub-Signature-256", sha256Sig(testSecret, body)), []byte(`{"action":"opened" }`), "signature_mismatch"},
		{"other key", testSecret, headers("X-Hub-Signature-256", sha256Sig(strings.Repeat("z", 64), body)), body, "signature_mismatch"},
		{"sha1 algorithm", testSecret, headers("X-Hub-Signature-256", sha1Sig(testSecret, body)), body, "signature_algorithm"},
		{"sha1 header only", testSecret, headers("X-Hub-Signature", sha1Sig(testSecret, body)), body, "proof_missing"},
		{"bare hex", testSecret, headers("X-Hub-Signature-256", strings.TrimPrefix(sha256Sig(testSecret, body), "sha256=")), body, "signature_algorithm"},
		{"short hex", testSecret, headers("X-Hub-Signature-256", "sha256=zz"), body, "signature_malformed"},
		{"truncated digest", testSecret, headers("X-Hub-Signature-256", sha256Sig(testSecret, body)[:70]), body, "signature_malformed"},
		{"non-hex digest", testSecret, headers("X-Hub-Signature-256", "sha256="+strings.Repeat("g", 64)), body, "signature_malformed"},
		{"absent secret", "", headers("Authorization", "Bearer "), body, "secret_unavailable"},
		{"absent secret, signature", "", headers("X-Hub-Signature-256", sha256Sig("", body)), body, "secret_unavailable"},
		{"short secret", "short", headers("Authorization", "Bearer short"), body, "secret_unavailable"},
		{"secret with space", strings.Repeat("a", 31) + " b", headers("Authorization", "Bearer "+strings.Repeat("a", 31)+" b"), body, "secret_unavailable"},
		{"two bearers", testSecret, headers("Authorization", "Bearer nope", "Authorization", "Bearer "+testSecret), body, "proof_ambiguous"},
		{"two signatures", testSecret, headers("X-Hub-Signature-256", "sha256=00", "X-Hub-Signature-256", sha256Sig(testSecret, body)), body, "proof_ambiguous"},
	}
	for _, c := range refused {
		err := Authenticate(c.secret, c.h, c.body, upstreamBounds)
		if got := reason(t, err, Unauthorized); got != c.why {
			t.Errorf("%s: reason %q, want %q", c.name, got, c.why)
		}
		if strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), string(c.body)) {
			t.Errorf("%s: failure discloses the secret or the body: %v", c.name, err)
		}
	}
}

func TestDeliveryIDs(t *testing.T) {
	ok := map[string]struct {
		h    http.Header
		want string
	}{
		"none":            {headers(), ""},
		"github":          {headers("X-GitHub-Delivery", "g-1"), "g-1"},
		"first wins":      {headers("X-Delivery-Id", "a", "Idempotency-Key", "b"), "a"},
		"github over key": {headers("X-GitHub-Delivery", "g", "Idempotency-Key", "b"), "g"},
		"at the bound":    {headers("Idempotency-Key", strings.Repeat("x", 200)), strings.Repeat("x", 200)},
		"punctuation":     {headers("X-Delivery-Id", "delivery-7.attempt_2~!"), "delivery-7.attempt_2~!"},
	}
	for name, c := range ok {
		got, err := DeliveryID(c.h, upstreamBounds)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
	bad := map[string]http.Header{
		"space":      headers("X-Delivery-Id", "has space"),
		"over bound": headers("X-Delivery-Id", strings.Repeat("x", 201)),
		"empty":      headers("X-Delivery-Id", ""),
		"non-ASCII":  headers("X-GitHub-Delivery", "délivery"),
		"control":    headers("Idempotency-Key", "a\tb"),
		"repeated":   headers("X-Delivery-Id", "a", "X-Delivery-Id", "b"),
		// The first present header decides, even when a later one is valid.
		"bad first": headers("X-Delivery-Id", "bad id", "X-GitHub-Delivery", "good"),
	}
	for name, h := range bad {
		_, err := DeliveryID(h, upstreamBounds)
		if got := reason(t, err, Invalid); got != "delivery_id" {
			t.Errorf("%s: reason %q", name, got)
		}
	}
}

func TestLimitsAreRequiredAndEnforcedWithoutTruncation(t *testing.T) {
	for _, l := range []Limits{{}, {BodyBytes: 1, DeliveryIDBytes: 1, GitHubEventNameBytes: 1}, {BodyBytes: -1, DeliveryIDBytes: 1, GitHubEventNameBytes: 1, SecretMinBytes: 1}} {
		if _, err := Receive(testSecret, headers(), nil, l); FailureOf(err) == nil || FailureOf(err).Code != LimitsInvalid {
			t.Errorf("Receive accepted bounds %+v: %v", l, err)
		}
		if _, err := ReadBody(strings.NewReader(""), l); FailureOf(err) == nil || FailureOf(err).Code != LimitsInvalid {
			t.Errorf("ReadBody accepted bounds %+v: %v", l, err)
		}
	}
	small := upstreamBounds
	small.BodyBytes = 16
	atBound := []byte(`{"k":"12345678"}`) // exactly 16 bytes
	if len(atBound) != 16 {
		t.Fatalf("fixture is %d bytes", len(atBound))
	}
	over := append(bytes.Clone(atBound), ' ')

	got, err := ReadBody(bytes.NewReader(atBound), small)
	if err != nil || !bytes.Equal(got, atBound) {
		t.Fatalf("body at the bound: %q, %v", got, err)
	}
	counting := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))}
	if _, err := ReadBody(counting, small); reason(t, err, TooLarge) != "body_bytes" {
		t.Fatal(err)
	}
	if counting.n > small.BodyBytes+1 {
		t.Fatalf("ReadBody read %d bytes for a %d-byte bound", counting.n, small.BodyBytes)
	}
	failing := errors.New("connection reset")
	if _, err := ReadBody(io.MultiReader(strings.NewReader("{"), errReader{failing}), small); !errors.Is(err, failing) {
		t.Fatalf("read error not returned: %v", err)
	}

	h := headers("Authorization", "Bearer "+testSecret)
	if in, err := Receive(testSecret, h, atBound, small); err != nil || in.Event["k"] != "12345678" {
		t.Fatalf("Receive at the bound: %+v, %v", in, err)
	}
	// Oversize is refused before authentication spends work on it, and Decode
	// refuses it too when called directly.
	if _, err := Receive(testSecret, headers("Authorization", "Bearer wrong"), over, small); reason(t, err, TooLarge) != "body_bytes" {
		t.Fatal(err)
	}
	if _, err := Decode(headers(), over, small); reason(t, err, TooLarge) != "body_bytes" {
		t.Fatal(err)
	}
	if _, err := Receive(testSecret, headers("X-GitHub-Event", "ping", "Authorization", "Bearer "+testSecret), over, small); reason(t, err, TooLarge) != "body_bytes" {
		t.Fatal("an oversize ping must be refused, as upstream")
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestDecodeRefusesWhatCannotBeAnObjectEvent(t *testing.T) {
	ok := map[string]int{
		`{"a":1}`:   1,
		"":          0,
		" \t\r\n\f": 0,
		`{"source":"slack","event":"message","text":"hi"}`: 3,
	}
	for body, fields := range ok {
		in, err := Decode(headers(), []byte(body), upstreamBounds)
		if err != nil || in.Ignored || in.Event == nil || len(in.Event) != fields {
			t.Errorf("%q: %+v, %v", body, in, err)
		}
	}
	if in, _ := Decode(headers(), []byte(`{"source":"slack","text":"hi"}`), upstreamBounds); in.Event["text"] != "hi" || in.Event["source"] != "slack" {
		t.Fatalf("object not kept as sent: %v", in.Event)
	}
	in, _ := Decode(headers(), []byte(`{"n":12345678901234567890}`), upstreamBounds)
	if in.Event["n"].(interface{ String() string }).String() != "12345678901234567890" {
		t.Fatalf("a large number lost precision: %v", in.Event["n"])
	}
	bad := map[string]string{
		"[1]":              "body_not_object",
		`"text"`:           "body_not_object",
		"null":             "body_not_object",
		"7":                "body_not_object",
		"disk full":        "body_json",
		"payload=%7B%7D":   "body_json",
		`{"a":1`:           "body_json",
		`{"a":1}{"b":2}`:   "body_json",
		`{"a":1} x`:        "body_json",
		"{\"a\":\"\xff\"}": "body_utf8",
	}
	for body, why := range bad {
		_, err := Decode(headers(), []byte(body), upstreamBounds)
		if got := reason(t, err, Invalid); got != why {
			t.Errorf("%q: reason %q, want %q", body, got, why)
		}
	}
	for name, h := range map[string]http.Header{
		"empty name": headers("X-GitHub-Event", ""),
		"space":      headers("X-GitHub-Event", "pull request"),
		"over bound": headers("X-GitHub-Event", strings.Repeat("e", 201)),
		"two names":  headers("X-GitHub-Event", "push", "X-GitHub-Event", "ping"),
		"non-ASCII":  headers("X-GitHub-Event", "pushé"),
	} {
		if _, err := Decode(h, []byte(`{}`), upstreamBounds); reason(t, err, Invalid) != "github_event_name" {
			t.Errorf("%s accepted", name)
		}
	}
}

// TestReceiveFromBytesToMatchedTrigger runs the whole module path on genuine
// bytes: authentication, delivery id, normalization and the domain match.
func TestReceiveFromBytesToMatchedTrigger(t *testing.T) {
	body := []byte(`{"action":"opened","pull_request":{"title":"Fix login","html_url":"https://example.invalid/o/r/pull/1"},"repository":{"full_name":"o/r"},"sender":{"login":"kai"}}`)
	h := headers("X-GitHub-Event", "pull_request", "X-GitHub-Delivery", "d-1", "X-Hub-Signature-256", sha256Sig(testSecret, body))
	in, err := Receive(testSecret, h, body, upstreamBounds)
	if err != nil {
		t.Fatal(err)
	}
	if in.DeliveryID != "d-1" || in.Ignored {
		t.Fatalf("got %+v", in)
	}
	spec := pa.RoutineEventSpec{Source: "github", Event: "pull_request.opened", Filters: map[string]string{"repo": "o/r", "sender": "kai", "messageContains": "login"}}
	if err := spec.Validate(pa.RoutineEventLimits{EventNameBytes: 200, Filters: 20, FilterKeyBytes: 200, FilterValueBytes: 4000}); err != nil {
		t.Fatal(err)
	}
	if !spec.Matches(in.Event) {
		t.Fatalf("spec did not match %v", in.Event)
	}
	for key, value := range map[string]string{"repo": "o/other", "sender": "someone", "messageContains": "logout", "url": "x"} {
		negative := pa.RoutineEventSpec{Source: spec.Source, Event: spec.Event, Filters: map[string]string{"repo": "o/r", key: value}}
		if negative.Matches(in.Event) {
			t.Errorf("filter %s=%s matched", key, value)
		}
	}
	if (pa.RoutineEventSpec{Source: "github", Event: "pull_request.closed"}).Matches(in.Event) ||
		(pa.RoutineEventSpec{Source: "slack", Event: "*"}).Matches(in.Event) {
		t.Fatal("another event or source matched")
	}

	// The same bytes with a byte changed after signing never reach decoding.
	tampered := bytes.Replace(body, []byte("o/r"), []byte("o/x"), 1)
	if _, err := Receive(testSecret, h, tampered, upstreamBounds); reason(t, err, Unauthorized) != "signature_mismatch" {
		t.Fatal(err)
	}
	// Authentication is checked before data validity: an unauthenticated
	// malformed body is unauthorized, an authenticated one is invalid.
	junk := []byte("not json")
	if _, err := Receive(testSecret, headers("Authorization", "Bearer x"), junk, upstreamBounds); reason(t, err, Unauthorized) != "bearer_mismatch" {
		t.Fatal(err)
	}
	if _, err := Receive(testSecret, headers("Authorization", "Bearer "+testSecret), junk, upstreamBounds); reason(t, err, Invalid) != "body_json" {
		t.Fatal(err)
	}
	if _, err := Receive(testSecret, headers("Authorization", "Bearer "+testSecret, "X-Delivery-Id", "bad id"), []byte(`{}`), upstreamBounds); reason(t, err, Invalid) != "delivery_id" {
		t.Fatal(err)
	}
	if _, err := Receive(testSecret, headers("Authorization", "Bearer nope", "X-Delivery-Id", "bad id"), []byte(`{}`), upstreamBounds); reason(t, err, Unauthorized) != "bearer_mismatch" {
		t.Fatal("an unauthenticated sender learned about its delivery id")
	}

	// A generic webhook's object is the event as sent; its source is data.
	generic := []byte(`{"source":"github","event":"push","text":"deploy"}`)
	g, err := Receive(testSecret, headers("Authorization", "Bearer "+testSecret), generic, upstreamBounds)
	if err != nil || g.DeliveryID != "" || !(pa.RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"messageContains": "dep"}}).Matches(g.Event) {
		t.Fatalf("generic delivery: %+v, %v", g, err)
	}
}

func TestAuthenticatedPingIsIgnored(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome.","hook_id":1}`)
	in, err := Receive(testSecret, headers("X-GitHub-Event", "ping", "X-Hub-Signature-256", sha256Sig(testSecret, body), "X-GitHub-Delivery", "p-1"), body, upstreamBounds)
	if err != nil || !in.Ignored || in.Event != nil || in.DeliveryID != "p-1" {
		t.Fatalf("got %+v, %v", in, err)
	}
	// Upstream returns before parsing a ping's body.
	if in, err := Decode(headers("X-GitHub-Event", "ping"), []byte("not json"), upstreamBounds); err != nil || !in.Ignored {
		t.Fatalf("ping with a non-JSON body: %+v, %v", in, err)
	}
	if _, err := Receive(testSecret, headers("X-GitHub-Event", "ping"), body, upstreamBounds); reason(t, err, Unauthorized) != "proof_missing" {
		t.Fatal("an unauthenticated ping was not refused")
	}
}
