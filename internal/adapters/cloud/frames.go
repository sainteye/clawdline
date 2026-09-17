package cloud

// The frames on the relay socket, docs/cloud-wire.md §7.2.
//
// These use `encoding/json`, and that is not a contradiction of the domain
// package's refusal to: the *envelope* is parsed by hand because the contract
// is about its exact bytes — duplicate keys, number spellings and a closed
// field set all decide whether a signature verifies (`GAP-RAW`). A frame
// wrapper carries no signature and is not charged for, so an ordinary decoder
// is the right tool for it. The envelope inside a frame is handed to
// `cloud.DecodeEnvelope` as raw bytes, never through a struct.

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Frame type names, both directions.
const (
	FrameChallenge     = "challenge"
	FrameHello         = "hello"
	FrameReady         = "ready"
	FramePublish       = "publish"
	FrameSubscribe     = "subscribe"
	FrameUnsubscribe   = "unsubscribe"
	FramePing          = "ping"
	FramePong          = "pong"
	FrameEnvelope      = "envelope"
	FrameAck           = "ack"
	FramePublishError  = "publish_error"
	FrameSubscriptions = "subscriptions"
	FrameError         = "error"
)

// ChallengeContext is the domain string the handshake signature is bound to.
// It is in the signed bytes for a reason: a signature over a bare nonce is an
// oracle, because the same 32 random bytes could be presented to this device by
// any relay, for any account, and the answer would still verify.
const ChallengeContext = "clawdline-challenge-v1"

// ChallengeBytes is how long the relay's nonce is, exactly.
const ChallengeBytes = 32

// ErrUnexpectedFrame is the answer for a frame that is well-formed JSON and
// not what the handshake was waiting for.
var ErrUnexpectedFrame = errors.New("an unexpected frame")

// RelayError is an `error` frame. It carries the relay's typed code, which is
// the only thing a refused connection tells us — there is no HTTP status,
// because a browser's WebSocket API would never show one (docs/cloud-wire.md
// §9.1), so the relay accepts the upgrade and then refuses in-band.
type RelayError struct {
	Code    string
	Message string
}

func (e *RelayError) Error() string {
	if e.Message == "" {
		return "relay refused: " + e.Code
	}
	return "relay refused: " + e.Code + ": " + e.Message
}

// Relay error codes, docs/cloud-wire.md §9.1.
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthorized     = "unauthorized"
	CodeTokenSuperseded  = "token_superseded"
	CodeForbidden        = "forbidden"
	CodeHandshakeTimeout = "handshake_timeout"
	CodeOverCapacity     = "over_capacity"
	CodeRateLimited      = "rate_limited"
	CodeTooLarge         = "too_large"
	CodeBadGateway       = "bad_gateway"
	CodeInternal         = "internal"
	CodeClockSkew        = "clock_skew"
	CodeMalformed        = "malformed_envelope"
	CodeUnavailable      = "unavailable"
	// CodeTokenExpired is not in the relay's own table. The Swift transport
	// treats it as an in-band token-expiry signal alongside `unauthorized`
	// (`CloudTransport.swift:2218-2224`) and it is kept here for the same
	// reason: whichever of the two arrives, the answer is a new token.
	CodeTokenExpired = "token_expired"
)

// frameHeader is enough to route a frame without committing to its shape.
type frameHeader struct {
	Type string `json:"type"`
}

// ChallengeFrame is step one of the handshake.
type ChallengeFrame struct {
	Type        string `json:"type"`
	V           int    `json:"v"`
	Context     string `json:"context"`
	Account     string `json:"account"`
	Device      string `json:"device"`
	Challenge   string `json:"challenge"`
	ExpiresInMs int64  `json:"expires_in_ms"`
}

// ReadyFrame is step three: the relay saying the line is open.
type ReadyFrame struct {
	Type           string `json:"type"`
	V              int    `json:"v"`
	Account        string `json:"account"`
	Device         string `json:"device"`
	Role           string `json:"role"`
	ConnectedAt    int64  `json:"connected_at"`
	TokenExpiresAt int64  `json:"token_expires_at"`
}

// AckFrame is the relay's answer to one published envelope.
//
// `delivered` means the fan-out happened, and nothing more. It does not mean a
// machine executed anything or that a person saw it — the contract package
// spells this out as six evidence domains that do not imply one another
// (docs/cloud-wire.md §10.2).
type AckFrame struct {
	Type   string `json:"type"`
	Ch     string `json:"ch"`
	Seq    uint64 `json:"seq"`
	Fanout int    `json:"fanout"`
	Status string `json:"status"`
}

// Ack statuses.
const (
	AckDelivered      = "delivered"
	AckMachineOffline = "machine_offline"
	AckViewerOffline  = "viewer_offline"
)

// PublishErrorFrame is a deterministic refusal of one envelope, correlated by
// channel and sequence. It only exists once `ch` and `seq` have passed their
// grammar; before that the relay cannot honestly say which envelope it is
// talking about and answers a bare `error` instead.
type PublishErrorFrame struct {
	Type  string `json:"type"`
	Ch    string `json:"ch"`
	Seq   uint64 `json:"seq"`
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

// ErrorFrame is a connection-level refusal.
type ErrorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// SubscriptionsFrame is the full list after a subscribe or unsubscribe.
type SubscriptionsFrame struct {
	Type     string   `json:"type"`
	Channels []string `json:"channels"`
}

// EnvelopeFrame is one delivery. The envelope is kept as raw bytes so that the
// domain decoder sees exactly what arrived.
type EnvelopeFrame struct {
	Type     string          `json:"type"`
	Realign  bool            `json:"realign,omitempty"`
	Envelope json.RawMessage `json:"envelope"`
}

// HelloFrame is step two: the signature over the challenge string.
type HelloFrame struct {
	Type string `json:"type"`
	Sig  string `json:"sig"`
}

// PingFrame is sent byte-for-byte as `{"type":"ping"}`. The relay recognises
// exactly that spelling and answers it without waking its Durable Object, so
// the spacing is not cosmetic: a reformatted ping costs a DO wake-up every
// thirty seconds, for every machine.
var PingFrame = []byte(`{"type":"ping"}`)

// ChallengeString is the UTF-8 the client signs. Four fields joined by `|`,
// docs/cloud-wire.md §7.1.
func ChallengeString(account, device, challenge string) string {
	return ChallengeContext + "|" + account + "|" + device + "|" + challenge
}

// PublishFrame builds `{"type":"publish","envelope":{…}}` around an envelope
// that is already exactly the bytes to send.
func PublishFrame(envelope []byte) []byte {
	out := make([]byte, 0, len(envelope)+32)
	out = append(out, `{"type":"publish","envelope":`...)
	out = append(out, envelope...)
	out = append(out, '}')
	return out
}

// SubscribeFrame builds a subscribe or unsubscribe frame.
func SubscribeFrame(kind string, channels []string) ([]byte, error) {
	if len(channels) == 0 || len(channels) > 8 {
		// The relay allows one to eight per connection; sending more is a
		// refusal it would have to answer, so it is refused here.
		return nil, fmt.Errorf("a subscription frame carries one to eight channels, not %d", len(channels))
	}
	return json.Marshal(struct {
		Type     string   `json:"type"`
		Channels []string `json:"channels"`
	}{Type: kind, Channels: channels})
}

// frameType reads just the type out of a frame.
func frameType(data []byte) (string, error) {
	var header frameHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return "", fmt.Errorf("a frame that is not JSON: %w", err)
	}
	if header.Type == "" {
		return "", errors.New("a frame with no type")
	}
	return header.Type, nil
}

// decodeError reads an `error` frame.
func decodeError(data []byte) *RelayError {
	var frame ErrorFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return &RelayError{Code: "malformed", Message: "an error frame that is not JSON"}
	}
	return &RelayError{Code: frame.Code, Message: frame.Message}
}

// decodeChallenge reads and checks a challenge frame. An `error` frame here is
// returned as a *RelayError, because a refused connection arrives exactly
// where a challenge was expected.
func decodeChallenge(data []byte) (ChallengeFrame, error) {
	kind, err := frameType(data)
	if err != nil {
		return ChallengeFrame{}, err
	}
	if kind == FrameError {
		return ChallengeFrame{}, decodeError(data)
	}
	if kind != FrameChallenge {
		return ChallengeFrame{}, fmt.Errorf("%w: %s, waiting for a challenge", ErrUnexpectedFrame, kind)
	}
	var frame ChallengeFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return ChallengeFrame{}, fmt.Errorf("a malformed challenge: %w", err)
	}
	if frame.V != 1 {
		return ChallengeFrame{}, fmt.Errorf("a challenge at version %d", frame.V)
	}
	if frame.Context != ChallengeContext {
		return ChallengeFrame{}, fmt.Errorf("a challenge in context %q", frame.Context)
	}
	if frame.Account == "" || frame.Device == "" {
		return ChallengeFrame{}, errors.New("a challenge naming no account or no device")
	}
	if frame.ExpiresInMs <= 0 {
		return ChallengeFrame{}, errors.New("a challenge that has already expired")
	}
	nonce, err := decodeStdBase64(frame.Challenge)
	if err != nil || len(nonce) != ChallengeBytes {
		return ChallengeFrame{}, fmt.Errorf("a challenge nonce that is not %d bytes", ChallengeBytes)
	}
	return frame, nil
}

// decodeReady reads and checks a ready frame against the challenge it answers.
func decodeReady(data []byte, challenge ChallengeFrame, role string) (ReadyFrame, error) {
	kind, err := frameType(data)
	if err != nil {
		return ReadyFrame{}, err
	}
	if kind == FrameError {
		return ReadyFrame{}, decodeError(data)
	}
	if kind != FrameReady {
		return ReadyFrame{}, fmt.Errorf("%w: %s, waiting for ready", ErrUnexpectedFrame, kind)
	}
	var frame ReadyFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return ReadyFrame{}, fmt.Errorf("a malformed ready: %w", err)
	}
	// The account and device are checked against the challenge, not against
	// what this machine believes, so that a relay cannot hand a socket to a
	// different identity halfway through one handshake.
	if frame.V != 1 || frame.Role != role ||
		frame.Account != challenge.Account || frame.Device != challenge.Device {
		return ReadyFrame{}, fmt.Errorf("%w: a ready frame for %s/%s as %s",
			ErrUnexpectedFrame, frame.Account, frame.Device, frame.Role)
	}
	return frame, nil
}
