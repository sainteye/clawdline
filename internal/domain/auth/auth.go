// Package auth decides who may ask this daemon anything, and what they may ask
// for. It is a port of the Swift app's Sources/RemoteAuth.swift, rule for rule;
// where it differs, the difference is written beside it.
//
// **The thing to understand before reading any of this: once a tunnel is
// running, every request arrives from 127.0.0.1.** cloudflared connects to the
// local port like any other program on the machine, so a phone in another
// country and a curl in another terminal have the same socket address.
// Loopback is therefore not an authorisation signal, and there is no exception
// for it anywhere in this package.
//
// Two kinds of secret, hashed two different ways, and the difference is not an
// inconsistency:
//
//   - A device token is 256 random bits. There is nothing to guess, so there is
//     nothing to slow down: it is stored as a plain SHA-256 and compared in
//     constant time.
//   - A password is chosen by a person. Guessing it is the attack, so it goes
//     through PBKDF2-HMAC-SHA256 at 600,000 iterations.
//
// Nothing here touches a file. What must outlive the process goes through
// Store, which is the seam an operating-system keychain would plug into.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Capability is what a token is allowed to do. An explicit list rather than a
// level, because reading and sending are not points on one scale: reading leaks
// a repository name, and sending is remote code execution.
type Capability string

const (
	Read  Capability = "read"
	Send  Capability = "send"
	Admin Capability = "admin"
)

// ParseCapability accepts the three spellings the Swift store writes.
func ParseCapability(s string) (Capability, bool) {
	switch Capability(s) {
	case Read, Send, Admin:
		return Capability(s), true
	}
	return "", false
}

// Caps is a set of capabilities, kept sorted so that two equal sets are equal
// slices and the stored file does not churn.
type Caps []Capability

// NewCaps builds a set from any list, dropping duplicates.
func NewCaps(list ...Capability) Caps {
	seen := map[Capability]bool{}
	out := Caps{}
	for _, c := range list {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Has reports whether the set carries c.
func (c Caps) Has(want Capability) bool {
	for _, have := range c {
		if have == want {
			return true
		}
	}
	return false
}

// Joined is the audit spelling: sorted, joined with "+".
func (c Caps) Joined() string {
	parts := make([]string, len(c))
	for i, v := range c {
		parts[i] = string(v)
	}
	return strings.Join(parts, "+")
}

// Device is one key that may reach this daemon.
type Device struct {
	ID   string
	Name string
	// Hash is the SHA-256 of the token, hex. Never the token itself.
	Hash     string
	Caps     Caps
	Created  time.Time
	LastSeen time.Time // zero when never seen
	// Approved on the Mac. The Swift store keeps the flag; this port only ever
	// stores approved devices, and reads the flag so a file written by either
	// means the same thing.
	Approved bool
	// Local is the one this machine made for itself, so that a script or the
	// shell running as this user can talk to the daemon. It counts for
	// authentication and not for the tunnel interlock: a token this app wrote
	// for its own use is not somebody deciding to be reachable.
	Local bool
}

// Password is a stretched password and what it takes to check one.
type Password struct {
	Hash       []byte
	Salt       []byte
	Iterations int
}

// State is everything about devices that must outlive the process.
type State struct {
	Devices  []Device
	Password *Password
}

// Store keeps what an Authority must not lose.
//
// This is the seam for an operating-system keychain. The device list holds only
// hashes and can stay a file anywhere; the two plaintext secrets — the local
// token here, the machine token in the adapter — are what a keychain would hold.
// The local token is read back by scripts, so a keychain version still has to
// put it somewhere a script running as the user can find.
type Store interface {
	Load() (State, error)
	Save(State) error
	// Audit appends one line. A failed append is the store's to report; the
	// action it records has already happened.
	Audit(event string, fields map[string]string)
	ReadLocalToken() (string, error)
	WriteLocalToken(token string) error
}

// Verdict is what a request turned out to be allowed to do.
type Verdict struct {
	Allowed bool
	Device  string
	Caps    Caps
	Local   bool
}

// Refusals an Authority gives, each one a code a caller may branch on.
var (
	ErrRateLimited = errors.New("rate_limited")
	// ErrPairingLocked is pairing closed by wrong codes; see PairingGuesses.
	ErrPairingLocked = errors.New("pairing_locked")
	ErrNotFound      = errors.New("not_found")
	ErrLocalDevice   = errors.New("local_device")
	ErrBadCaps       = errors.New("bad_caps")
	// ErrInvalidState is a stored state that reads and does not make sense.
	ErrInvalidState = errors.New("invalid_state")
)

// PairResult is how a confirmation went.
type PairResult struct {
	Kind  PairKind
	Token string // only when Kind is Paired
	Left  int    // only when Kind is WrongCode
	// Device is the new device's id, only when Kind is Paired.
	Device string
}

type PairKind int

const (
	Expired PairKind = iota
	WrongCode
	Paired
)

// pairing is a device waiting to be let in. The code is shown on the Mac and
// typed into the device, never the other way round: anybody can reach the
// pairing route, and only somebody who can see this machine's screen can
// finish it.
type pairing struct {
	ID      string
	Name    string
	Code    string
	token   string
	Expires time.Time
}

// Hash is the stored form of a token.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ConstantTimeEquals compares without letting the time taken say how much of
// it matched. Both sides are fixed-length hex digests or fixed-length codes, so
// the length check leaks nothing, and the loop does not stop early.
func ConstantTimeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var difference byte
	for i := 0; i < len(a); i++ {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}

// VerifySecret checks a presented secret against the expected one by comparing
// their digests, so neither length nor content shows in the time taken. An
// empty expected secret verifies nothing: an unreadable credential fails
// closed.
func VerifySecret(expected, presented string) bool {
	if expected == "" || presented == "" {
		return false
	}
	return ConstantTimeEquals(Hash(expected), Hash(presented))
}

// NewToken is 256 bits from the system generator, URL-safe so it can live in a
// QR code and a fragment.
func NewToken(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("no randomness for a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newID is a lowercase UUID, version 4 — the spelling the Swift store uses.
func newID(r io.Reader) (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("no randomness for an id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// newCode is six decimal digits, uniformly drawn.
func newCode(r io.Reader) (string, error) {
	// Rejection sampling over 20 bits keeps every code equally likely.
	b := make([]byte, 3)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return "", fmt.Errorf("no randomness for a code: %w", err)
		}
		n := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) & 0xfffff
		if n < 1_000_000 {
			return fmt.Sprintf("%06d", n), nil
		}
	}
}

var defaultRandom io.Reader = rand.Reader
