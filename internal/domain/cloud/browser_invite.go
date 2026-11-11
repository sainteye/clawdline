package cloud

// Browser invitations: how an owner admits a browser that has no Clawdline
// account (a cloud computer's browser, say) to one machine.
//
// The machine draws a six-character secret; Cloud assigns a five-character
// invitation id. The person reads `IIIII-SSSSSS` on a device already paired
// with the machine and types it into the new browser. Only the id ever
// reaches Cloud. The secret is CPace's password (cpace.go), so Cloud, which
// relays both CPace messages, learns nothing it could use and gets one online
// guess if it substitutes a message — after which the invitation is spent.
//
// From CPace's ISK this file derives three things, each under its own label:
// the key the browser seals its confirmation with, the key the machine seals
// the handover with, and the six digits both screens show. The person
// approves on the inviting device only when the digits match, which is what
// defeats someone who saw the code and typed it first.
//
// docs/browser-invite.md is the normative description.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// InviteAlphabet is Crockford's base32: no I, L, O or U.
const InviteAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Invitation code part lengths, in alphabet characters.
const (
	InviteIDChars     = 5
	InviteSecretChars = 6
)

// Invitation expiry choices. They name how long an admitted browser stays
// admitted, not how long the code lives (that is Cloud's ten minutes).
const (
	InviteExpiry8h    = "8h"
	InviteExpiry7d    = "7d"
	InviteExpiryNever = "never"
)

// InviteExpirySeconds is each choice's lifetime; never is 0.
func InviteExpirySeconds(choice string) (int64, bool) {
	switch choice {
	case InviteExpiry8h:
		return 8 * 3600, true
	case InviteExpiry7d:
		return 7 * 24 * 3600, true
	case InviteExpiryNever:
		return 0, true
	}
	return 0, false
}

// Browser invitation refusals.
var (
	ErrInviteCode     = errors.New("browser invite: that is not an invitation code")
	ErrInviteSealed   = errors.New("browser invite: the sealed message does not open with this invitation's key")
	ErrInvitePayload  = errors.New("browser invite: the sealed message is malformed")
	ErrInviteMismatch = errors.New("browser invite: the sealed message names a different invitation, browser or machine")
)

// NewInviteSecret draws the secret half.
func NewInviteSecret(random io.Reader) (string, error) {
	return inviteChars(random, InviteSecretChars)
}

// NewInviteID draws an id; Cloud owns ids in production, tests use this.
func NewInviteID(random io.Reader) (string, error) {
	return inviteChars(random, InviteIDChars)
}

func inviteChars(random io.Reader, n int) (string, error) {
	raw := make([]byte, n)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = InviteAlphabet[b&31]
	}
	return string(out), nil
}

// FormatInviteCode is what the person reads: `IIIII-SSSSSS`.
func FormatInviteCode(id, secret string) string { return id + "-" + secret }

// ParseInviteCode reads what a person typed. Case, spaces and dashes are
// ignored, and the letters Crockford reads as digits (O, I, L) are read so.
func ParseInviteCode(typed string) (id, secret string, err error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(typed) {
		switch r {
		case ' ', '-', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(InviteAlphabet, r) {
			return "", "", ErrInviteCode
		}
		b.WriteRune(r)
	}
	s := b.String()
	if len(s) != InviteIDChars+InviteSecretChars {
		return "", "", ErrInviteCode
	}
	return s[:InviteIDChars], s[InviteIDChars:], nil
}

// ValidInviteID is the id's shape, for routes that receive one.
func ValidInviteID(id string) bool {
	if len(id) != InviteIDChars {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune(InviteAlphabet, r) {
			return false
		}
	}
	return true
}

// CPace inputs for an invitation. The browser is the initiator.
var (
	InviteBrowserAD = []byte("clawdline browser invite v1 browser")
	InviteMachineAD = []byte("clawdline browser invite v1 machine")
)

// InviteCPaceInputs returns PRS, CI and sid. The machine id is not among
// them: the browser must send its message before Cloud tells it which machine
// it is joining, and Cloud is not trusted to say so. The id is bound instead
// inside both sealed messages, which each end checks.
func InviteCPaceInputs(id, secret string) (prs, ci, sid []byte) {
	return []byte(secret), CPaceLVCat([]byte("clawdline browser invite v1")),
		[]byte("clawdline-invite:" + id)
}

// InviteKeys are what one ISK yields.
type InviteKeys struct {
	Confirm  []byte // AES-256-GCM key for the browser's confirmation
	Handover []byte // AES-256-GCM key for the machine's handover
	Code     string // six digits both screens show
}

// DeriveInviteKeys expands the ISK with HKDF-SHA256, salted by the id.
func DeriveInviteKeys(isk []byte, id string) (InviteKeys, error) {
	salt := []byte("clawdline-invite:" + id)
	confirm, err := hkdf.Key(sha256.New, isk, salt, "clawdline browser invite v1 confirm key", 32)
	if err != nil {
		return InviteKeys{}, err
	}
	handover, err := hkdf.Key(sha256.New, isk, salt, "clawdline browser invite v1 handover key", 32)
	if err != nil {
		return InviteKeys{}, err
	}
	digits, err := hkdf.Key(sha256.New, isk, salt, "clawdline browser invite v1 comparison code", 4)
	if err != nil {
		return InviteKeys{}, err
	}
	return InviteKeys{Confirm: confirm, Handover: handover,
		Code: fmt.Sprintf("%06d", binary.BigEndian.Uint32(digits)%1_000_000)}, nil
}

// InviteConfirm is what the browser seals after CPace: the signing key it
// will use and the name it asks to be known by. Cloud reports the same key in
// clear; the machine admits only when the two agree.
type InviteConfirm struct {
	V         int    `json:"v"`
	Type      string `json:"type"`
	InviteID  string `json:"invite_id"`
	MachineID string `json:"machine_id"`
	DeviceID  string `json:"device_id"`
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
}

const (
	inviteConfirmType  = "clawdline_browser_invite_confirm"
	inviteHandoverType = "clawdline_browser_invite_handover"
)

// InviteHandover is the pairing the machine seals for the admitted browser:
// the same fields an ordinary handover carries, plus the admission's end.
type InviteHandover struct {
	V                  int    `json:"v"`
	Type               string `json:"type"`
	InviteID           string `json:"invite_id"`
	AccountID          string `json:"account_id"`
	MachineID          string `json:"machine_id"`
	MachineSigningKey  string `json:"machine_signing_key"`
	MachineFingerprint string `json:"machine_fingerprint"`
	KeyID              string `json:"key_id"`
	MasterSecret       string `json:"master_secret"`
	DeviceID           string `json:"device_id"`
	Scope              string `json:"scope"`
	// ExpiresAt is Unix seconds; 0 is never.
	ExpiresAt int64 `json:"expires_at"`
}

// SealInviteConfirm seals the browser's confirmation. Only tests and the
// vector generator seal one in Go; browsers seal theirs in cpace.ts's twin.
func SealInviteConfirm(keys InviteKeys, c InviteConfirm, nonce []byte) (string, error) {
	c.V, c.Type = 1, inviteConfirmType
	return inviteSeal(keys.Confirm, c.InviteID, c, nonce)
}

// OpenInviteConfirm opens and checks the browser's confirmation against what
// this machine knows: the invitation, itself, and the device Cloud reported.
func OpenInviteConfirm(keys InviteKeys, sealed, inviteID, machineID, deviceID, publicKey string) (InviteConfirm, error) {
	var c InviteConfirm
	if err := inviteOpen(keys.Confirm, inviteID, sealed, &c); err != nil {
		return InviteConfirm{}, err
	}
	if c.V != 1 || c.Type != inviteConfirmType {
		return InviteConfirm{}, ErrInvitePayload
	}
	if c.InviteID != inviteID || c.MachineID != machineID || c.DeviceID != deviceID || c.PublicKey != publicKey {
		return InviteConfirm{}, ErrInviteMismatch
	}
	raw, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return InviteConfirm{}, ErrInvitePayload
	}
	return c, nil
}

// SealInviteHandover seals the pairing for the admitted browser.
func SealInviteHandover(keys InviteKeys, h InviteHandover, nonce []byte) (string, error) {
	h.V, h.Type = 1, inviteHandoverType
	return inviteSeal(keys.Handover, h.InviteID, h, nonce)
}

// OpenInviteHandover is the browser's half, kept here for the round-trip test.
func OpenInviteHandover(keys InviteKeys, sealed, inviteID string) (InviteHandover, error) {
	var h InviteHandover
	if err := inviteOpen(keys.Handover, inviteID, sealed, &h); err != nil {
		return InviteHandover{}, err
	}
	if h.V != 1 || h.Type != inviteHandoverType || h.InviteID != inviteID {
		return InviteHandover{}, ErrInvitePayload
	}
	return h, nil
}

// inviteSeal is AES-256-GCM, nonce prepended, the invite id as associated
// data, standard base64.
func inviteSeal(key []byte, inviteID string, v any, nonce []byte) (string, error) {
	if len(nonce) != NonceBytes {
		return "", ErrNonceLength
	}
	plain, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	aead, err := inviteAEAD(key)
	if err != nil {
		return "", err
	}
	out := append(append([]byte(nil), nonce...), aead.Seal(nil, nonce, plain, []byte(inviteID))...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func inviteOpen(key []byte, inviteID, sealed string, v any) error {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < NonceBytes+16 {
		return ErrInvitePayload
	}
	aead, err := inviteAEAD(key)
	if err != nil {
		return err
	}
	plain, err := aead.Open(nil, raw[:NonceBytes], raw[NonceBytes:], []byte(inviteID))
	if err != nil {
		return ErrInviteSealed
	}
	dec := json.NewDecoder(strings.NewReader(string(plain)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return ErrInvitePayload
	}
	return nil
}

func inviteAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
