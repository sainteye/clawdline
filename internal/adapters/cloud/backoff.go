package cloud

// Reconnect backoff and the classification of what went wrong.
//
// The numbers are `Sources/CloudTransport.swift:1711-1718` and the ladder is
// :2107-2113. Production on the Swift side passes none of them, so these are
// the values that are actually deployed against the relay today.

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/rand"
	"net"
	"time"
)

// Transport timings, `CloudTransport.swift:1711-1718`.
const (
	// InitialBackoff is the first reconnect wait.
	InitialBackoff = 250 * time.Millisecond
	// MaximumBackoff is the ceiling.
	MaximumBackoff = 30 * time.Second
	// BackoffResetAfter is how long a connection must have been *up* before
	// its death resets the ladder. Not "how long since traffic": a socket that
	// is accepted and immediately refused, over and over, must not look
	// healthy because the refusals are quick.
	BackoffResetAfter = 30 * time.Second
	// OpeningTimeout bounds the token fetch and the socket open together.
	OpeningTimeout = 15 * time.Second
	// AuthenticationTimeout bounds the wait for the challenge, and separately
	// the wait for ready.
	AuthenticationTimeout = 15 * time.Second
	// ReceiveTimeout is the liveness bound: no frame of any kind for this long
	// and the socket is closed and redialled.
	ReceiveTimeout = 90 * time.Second
	// KeepaliveInterval is how often a ping goes out. It is half the receive
	// timeout at most, because a keepalive that cannot answer the liveness
	// bound twice before it fires is not a keepalive.
	KeepaliveInterval = 30 * time.Second
	// RefreshAhead is how long before a device token expires that a new one is
	// fetched.
	RefreshAhead = 60 * time.Second
)

// Backoff is the reconnect ladder. It is not safe for concurrent use; one
// connection loop owns one.
type Backoff struct {
	Initial time.Duration
	Maximum time.Duration
	// Jitter returns a number in [0,1). nil means math/rand.
	Jitter func() float64

	current time.Duration
}

// NewBackoff returns a ladder at its first rung.
func NewBackoff() *Backoff {
	return &Backoff{Initial: InitialBackoff, Maximum: MaximumBackoff, current: InitialBackoff}
}

// Next answers how long to wait, and then doubles.
//
// The order matters and is the Swift order: sleep first, double afterwards
// (`CloudTransport.swift:2112-2113`). So the first retry after any failure is
// always about 250 ms, however long the previous outage was — the ladder is
// about the *current* outage, not about history.
//
// The jitter is ±25% (`0.75 + unit*0.5`), symmetric. This is deliberately not
// "full jitter": the point here is to stop a fleet of machines redialling in
// lockstep after one relay restart, not to spread one client's retries over
// the whole window, and a symmetric band keeps the expected delay equal to the
// nominal one.
func (b *Backoff) Next() time.Duration {
	if b.current <= 0 {
		b.current = b.Initial
	}
	jitter := 0.75 + b.unit()*0.5
	delay := time.Duration(float64(min(b.current, b.Maximum)) * jitter)
	b.current = min(b.Maximum, b.current*2)
	return delay
}

// Reset puts the ladder back to its first rung.
func (b *Backoff) Reset() { b.current = b.Initial }

// ResetIfStable resets the ladder when the connection that just died had been
// up for at least BackoffResetAfter.
func (b *Backoff) ResetIfStable(connectedFor time.Duration) {
	if connectedFor >= BackoffResetAfter {
		b.Reset()
	}
}

func (b *Backoff) unit() float64 {
	if b.Jitter != nil {
		return b.Jitter()
	}
	return rand.Float64()
}

// terminalRelayCodes are the refusals that mean *stop*. Redialling with the
// same credential after one of these is a loop whose only outcome is a rate
// limit, and the user never finds out why nothing works.
// `CloudTransport.swift:2238-2240`.
var terminalRelayCodes = map[string]bool{
	CodeUnauthorized:  true,
	CodeForbidden:     true,
	"revoked":         true,
	"device_revoked":  true,
	"account_revoked": true,
}

// tokenExpiryCodes are the in-band refusals that mean *get a new token and try
// again*. `CloudTransport.swift:2218-2224`.
var tokenExpiryCodes = map[string]bool{
	CodeTokenExpired:    true,
	CodeUnauthorized:    true,
	CodeTokenSuperseded: true,
}

// IsTerminalAuthorization reports whether this failure ends the line until
// somebody signs in again.
//
// The subtlety worth keeping: `unauthorized` is in **both** tables, and which
// one wins depends on where it arrived. A 401 at the HTTP upgrade, or from the
// token endpoint, is the credential being wrong — terminal. The same word in a
// frame on an already-authenticated socket is the token having aged out while
// the socket was open — retryable, with a new token. The Swift loop gets this
// right by asking about token expiry *first* (`CloudTransport.swift:2055-2056`),
// and so does connectionLoop here.
func IsTerminalAuthorization(err error) bool {
	var upgrade *UpgradeError
	if errors.As(err, &upgrade) {
		return upgrade.Status == 401 || upgrade.Status == 403
	}
	var relay *RelayError
	if errors.As(err, &relay) {
		return terminalRelayCodes[relay.Code]
	}
	return errors.Is(err, ErrUnauthorized)
}

// IsTokenExpiry reports whether an in-band refusal is asking for a fresh
// token rather than for the line to stop.
func IsTokenExpiry(err error) bool {
	var relay *RelayError
	if errors.As(err, &relay) {
		return tokenExpiryCodes[relay.Code]
	}
	return false
}

// FailureCode turns an error into the short snake_case word that goes into the
// status file and the log line. `CloudTransport.swift:2181-2202`.
//
// It is a closed vocabulary on purpose: an operator reading a status file
// should be able to grep for one of these, and a free-text error message means
// every machine spells the same failure differently.
//
// The words a far end chose — the relay's code, the control plane's — are
// copied only when they are a plain snake_case token of at most
// maxFailureCodeBytes, because they end up in the status route and in every
// log line; anything else is `…_unrecognized`, which failure.go names as
// unknown rather than guessing at.
func FailureCode(err error) string {
	if err == nil {
		return ""
	}
	var api *APIError
	if errors.As(err, &api) {
		if api.Code == "" {
			return "api_http_" + itoa(api.Status)
		}
		return "api_" + farEndCode(api.Code)
	}
	var relay *RelayError
	if errors.As(err, &relay) {
		return "relay_" + farEndCode(relay.Code)
	}
	var upgrade *UpgradeError
	if errors.As(err, &upgrade) {
		return "upgrade_refused_" + itoa(upgrade.Status)
	}
	var closed *CloseError
	if errors.As(err, &closed) {
		return "closed_" + itoa(closed.Code)
	}
	switch {
	case errors.Is(err, ErrIncompatible):
		return "incompatible"
	case errors.Is(err, ErrNoIdentity):
		return "no_identity"
	case errors.Is(err, ErrOtherEnvironment):
		return "identity_other_environment"
	case errors.Is(err, ErrLoginDenied):
		return "login_denied"
	case errors.Is(err, ErrLoginExpired):
		return "login_expired"
	case errors.Is(err, ErrLoginTimeout):
		return "login_timeout"
	case errors.Is(err, ErrInvalidToken):
		return "invalid_token"
	case errors.Is(err, ErrDisabled):
		return "switched_off"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrChallengeTimeout):
		return "challenge_timeout"
	case errors.Is(err, ErrReadyTimeout):
		return "ready_timeout"
	case errors.Is(err, ErrReceiveTimeout):
		return "receive_timeout"
	case errors.Is(err, ErrConnectionTimeout):
		return "connection_timeout"
	case errors.Is(err, ErrTokenRotated):
		return "token_rotation"
	case errors.Is(err, ErrIdentityBinding):
		return "identity_binding"
	case errors.Is(err, ErrUnexpectedFrame):
		return "unexpected_frame"
	case errors.Is(err, ErrNotConnected):
		return "not_connected"
	case errors.Is(err, ErrConnClosed):
		return "connection_failed"
	}
	// The network's own failures, only in the phase before a line exists.
	// A socket that dies mid-session is `connection_failed`, as it always
	// was: "unreachable" would be a lie about a relay that answered for an
	// hour and then closed.
	if untrustedCertificate(err) {
		return "tls_untrusted"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "unreachable"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "unreachable"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "connection_timeout"
	}
	return "connection_failed"
}

// maxFailureCodeBytes bounds a word copied from the far end into a failure
// code. The relay's and the control plane's own codes are all under 32.
const maxFailureCodeBytes = 64

// farEndCode copies a code the far end sent when it is a plain snake_case
// token, and answers `unrecognized` otherwise.
func farEndCode(code string) string {
	if code == "" || len(code) > maxFailureCodeBytes {
		return "unrecognized"
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "unrecognized"
		}
	}
	return code
}

// untrustedCertificate reports a TLS peer whose certificate did not verify:
// a proxy in the middle, a wrong host, or a clock far enough off that every
// certificate looks expired.
func untrustedCertificate(err error) bool {
	var verification *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &authority) ||
		errors.As(err, &hostname) || errors.As(err, &invalid)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
