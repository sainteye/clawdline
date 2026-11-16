// Package projectassistantwebhook turns one routine webhook delivery — its
// original body bytes and HTTP headers — into an authenticated, normalized
// routine event, as the pinned Codync hooks.rs does.
//
// It makes no network or provider call, stores no credential, and neither
// queues, deduplicates nor executes anything. Nothing routes to it yet: the
// caller wires authentication, admission, durable delivery dedupe and the run
// itself, and registers the bounds it passes in Limits.
package projectassistantwebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Code says which family a refusal belongs to, so authentication failures stay
// apart from malformed or oversize data.
type Code string

const (
	// Unauthorized covers a missing or unusable secret, a missing proof and a
	// wrong proof alike, so a sender cannot probe which one it was.
	Unauthorized Code = "webhook_unauthorized"
	// Invalid is a delivery that authenticated (or was never authenticated
	// because the caller only decoded it) but cannot become an event.
	Invalid Code = "webhook_invalid"
	// TooLarge is a body over the caller's byte bound; it is never truncated.
	TooLarge Code = "webhook_too_large"
	// LimitsInvalid is a caller bug: a zero or negative bound.
	LimitsInvalid Code = "webhook_limits_invalid"
)

// Failure is structured refusal data for the caller to map onto its own
// transport error. Reason is for diagnostics; it never carries the secret, the
// proof or the payload, and the caller should not show it to the sender.
type Failure struct {
	Code   Code   `json:"code"`
	Reason string `json:"reason"`
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Reason }

func refuse(code Code, reason string) error { return &Failure{Code: code, Reason: reason} }

// FailureOf returns the Failure inside err, or nil.
func FailureOf(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return nil
}

// Limits are the caller's registered bounds; this package has no defaults.
type Limits struct {
	// BodyBytes is the largest accepted body. ReadBody reads at most one byte
	// more, so an oversize body is refused without being held in full.
	BodyBytes int
	// DeliveryIDBytes bounds the sender's delivery id.
	DeliveryIDBytes int
	// GitHubEventNameBytes bounds the X-GitHub-Event value.
	GitHubEventNameBytes int
	// SecretMinBytes is the shortest routine secret this module will accept
	// as a key; a shorter or empty one never authenticates anything.
	SecretMinBytes int
}

func (l Limits) Validate() error {
	if l.BodyBytes < 1 || l.DeliveryIDBytes < 1 || l.GitHubEventNameBytes < 1 || l.SecretMinBytes < 1 {
		return refuse(LimitsInvalid, "limits")
	}
	return nil
}

// ReadBody reads the body up to the bound and refuses anything longer, having
// read at most BodyBytes+1 bytes. Errors from r are returned unchanged.
func ReadBody(r io.Reader, l Limits) ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(r, int64(l.BodyBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > l.BodyBytes {
		return nil, refuse(TooLarge, "body_bytes")
	}
	return body, nil
}

// Input is one authenticated, decoded delivery.
type Input struct {
	// DeliveryID is the sender's id for retries of this delivery, or "" when
	// it sent none. It identifies the delivery, not the body and not a run.
	DeliveryID string
	// Ignored is an authenticated GitHub ping: it only tests the connection
	// and must not start anything.
	Ignored bool
	// Event is the normalized untrusted event; nil when Ignored.
	Event pa.RoutineEvent
}

// Receive checks a delivery in upstream order: the byte bound, the secret
// against the original bytes, the delivery id, then decoding. It returns what
// a routine's triggers would match; it does not decide whether anything runs.
func Receive(secret string, h http.Header, body []byte, l Limits) (Input, error) {
	if err := l.Validate(); err != nil {
		return Input{}, err
	}
	if len(body) > l.BodyBytes {
		return Input{}, refuse(TooLarge, "body_bytes")
	}
	if err := Authenticate(secret, h, body, l); err != nil {
		return Input{}, err
	}
	id, err := DeliveryID(h, l)
	if err != nil {
		return Input{}, err
	}
	in, err := Decode(h, body, l)
	if err != nil {
		return Input{}, err
	}
	in.DeliveryID = id
	return in, nil
}

const (
	bearerPrefix    = "Bearer "
	signatureHeader = "X-Hub-Signature-256"
	signaturePrefix = "sha256="
)

// Authenticate accepts `Authorization: Bearer <secret>` or
// `X-Hub-Signature-256: sha256=<hex HMAC-SHA256(secret, body)>` over the exact
// original bytes. As upstream, a wrong bearer still lets a valid signature
// through. A repeated proof header is ambiguous and refused.
func Authenticate(secret string, h http.Header, body []byte, l Limits) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if !usableSecret(secret, l) {
		return refuse(Unauthorized, "secret_unavailable")
	}
	auth, sig := h.Values("Authorization"), h.Values(signatureHeader)
	if len(auth) > 1 || len(sig) > 1 {
		return refuse(Unauthorized, "proof_ambiguous")
	}
	reason := "proof_missing"
	if len(auth) == 1 {
		if given, ok := strings.CutPrefix(auth[0], bearerPrefix); ok {
			if equalSecret(given, secret) {
				return nil
			}
			reason = "bearer_mismatch"
		}
	}
	if len(sig) == 0 {
		return refuse(Unauthorized, reason)
	}
	digest, ok := strings.CutPrefix(sig[0], signaturePrefix)
	if !ok {
		return refuse(Unauthorized, "signature_algorithm")
	}
	given, err := hex.DecodeString(digest)
	if err != nil || len(given) != sha256.Size {
		return refuse(Unauthorized, "signature_malformed")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), given) {
		return refuse(Unauthorized, "signature_mismatch")
	}
	return nil
}

// usableSecret refuses an empty, short or non-printable key: such a key could
// not be sent as a bearer token and never authenticates anything.
func usableSecret(secret string, l Limits) bool {
	return len(secret) >= l.SecretMinBytes && asciiGraphic(secret)
}

// equalSecret compares digests so neither the content nor the length of the
// secret shapes the comparison time.
func equalSecret(given, secret string) bool {
	a, b := sha256.Sum256([]byte(given)), sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func asciiGraphic(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

var deliveryHeaders = []string{"X-Delivery-Id", "X-Github-Delivery", "Idempotency-Key"}

// DeliveryID returns the first of X-Delivery-Id, X-GitHub-Delivery and
// Idempotency-Key that is present, as upstream. A present id must be one
// value of printable ASCII within the bound; "" means none was sent.
func DeliveryID(h http.Header, l Limits) (string, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}
	for _, name := range deliveryHeaders {
		values := h.Values(name)
		if len(values) == 0 {
			continue
		}
		id := values[0]
		if len(values) != 1 || id == "" || len(id) > l.DeliveryIDBytes || !asciiGraphic(id) {
			return "", refuse(Invalid, "delivery_id")
		}
		return id, nil
	}
	return "", nil
}

// Decode turns a body into the event a routine's triggers match. It does not
// authenticate: call it only on bytes Authenticate accepted, or use Receive.
//
// An empty or whitespace-only body is an empty object, as upstream. Any other
// body must be one valid UTF-8 JSON object. Unlike upstream, a non-object or
// non-JSON body is refused rather than wrapped as {"payload"} or {"text"}.
// With X-GitHub-Event the object is normalized (github.go), and a ping is
// Ignored before its body is parsed, as upstream.
func Decode(h http.Header, body []byte, l Limits) (Input, error) {
	if err := l.Validate(); err != nil {
		return Input{}, err
	}
	if len(body) > l.BodyBytes {
		return Input{}, refuse(TooLarge, "body_bytes")
	}
	names := h.Values("X-Github-Event")
	if len(names) > 1 {
		return Input{}, refuse(Invalid, "github_event_name")
	}
	if len(names) == 1 {
		name := names[0]
		if name == "" || len(name) > l.GitHubEventNameBytes || !asciiGraphic(name) {
			return Input{}, refuse(Invalid, "github_event_name")
		}
		if name == "ping" {
			return Input{Ignored: true}, nil
		}
		object, err := decodeObject(body)
		if err != nil {
			return Input{}, err
		}
		return Input{Event: github(name, object)}, nil
	}
	object, err := decodeObject(body)
	if err != nil {
		return Input{}, err
	}
	return Input{Event: pa.RoutineEvent(object)}, nil
}

func decodeObject(body []byte) (map[string]any, error) {
	if len(bytes.Trim(body, " \t\n\f\r")) == 0 {
		return map[string]any{}, nil
	}
	if !utf8.Valid(body) {
		return nil, refuse(Invalid, "body_utf8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, refuse(Invalid, "body_json")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, refuse(Invalid, "body_json")
	}
	object, ok := v.(map[string]any)
	if !ok {
		return nil, refuse(Invalid, "body_not_object")
	}
	return object, nil
}
