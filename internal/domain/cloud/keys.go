package cloud

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// The two key roles, kept apart on purpose (blueprint §02, PROTOCOL.md §1).
//
//   - A ContentKey seals payloads with AES-256-GCM. The account master secret
//     is one, named on the wire by a key_id like "ms-1"; a ctlr response's
//     request-scoped reply key is another, named "rk-<22>". One account secret
//     is shared by every device the user paired; the cloud never holds it.
//   - A DeviceKey signs envelopes with Ed25519. Every device has its own, and a
//     Mac pins the public half of each paired device at pairing time and trusts
//     that pin rather than anything the cloud says.
//
// Conflating them is the mistake this comment exists to prevent: signing with
// the master secret would make every device able to impersonate every other,
// and sealing under a device key would make an account unreadable to itself.
//
// Ported from ~/code/clawdline/Sources/CloudKeys.swift; see docs/cloud-wire.md §5.

// Key sizes.
const (
	// ContentKeyBytes is an AES-256 key.
	ContentKeyBytes = 32
	// DeviceKeySeedBytes is an Ed25519 seed, which is what CryptoKit calls a
	// private key's rawRepresentation and what this app stores.
	DeviceKeySeedBytes = 32
)

// recoveryPrefix and recoveryDomain belong to the human-typed recovery code.
const (
	recoveryPrefix = "CLAWD1"
	recoveryDomain = "clawdline-recovery-v1"
	recoveryGroup  = 5
	// fingerprintBytes is the 80-bit prefix of SHA-256 people compare aloud.
	fingerprintBytes = 10
	fingerprintGroup = 4
)

// ContentKey is a 32-byte AES-256-GCM key.
type ContentKey struct{ raw []byte }

// NewContentKey draws a fresh key. Pass nil for crypto/rand.
func NewContentKey(source io.Reader) (ContentKey, error) {
	if source == nil {
		source = rand.Reader
	}
	raw := make([]byte, ContentKeyBytes)
	if _, err := io.ReadFull(source, raw); err != nil {
		return ContentKey{}, err
	}
	return ContentKey{raw: raw}, nil
}

// ContentKeyFromBytes adopts exactly 32 bytes.
func ContentKeyFromBytes(raw []byte) (ContentKey, error) {
	if len(raw) != ContentKeyBytes {
		return ContentKey{}, fmt.Errorf("%w: %d", ErrKeyLength, len(raw))
	}
	return ContentKey{raw: append([]byte(nil), raw...)}, nil
}

// Bytes is a copy of the key material.
func (k ContentKey) Bytes() []byte { return append([]byte(nil), k.raw...) }

// Valid reports whether this key holds material at all. The zero ContentKey is
// not usable and every operation on it refuses.
func (k ContentKey) Valid() bool { return len(k.raw) == ContentKeyBytes }

// RecoveryCode is the account master secret written down: "CLAWD1-" and the
// secret plus a four-byte checksum in RFC 4648 base32, grouped in fives.
//
// The checksum is SHA-256("clawdline-recovery-v1" || secret)[:4]; it catches a
// mistyped code before the user is told their content will not open, which is
// the only failure mode that looks like data loss.
func (k ContentKey) RecoveryCode() string {
	body := append(append([]byte(nil), k.raw...), recoveryChecksum(k.raw)...)
	return recoveryPrefix + "-" + groupBase32(base32Encode(body), recoveryGroup)
}

// ContentKeyFromRecoveryCode reads a code back. Case, hyphens and spaces are
// forgiven because a person types this; the checksum is not.
func ContentKeyFromRecoveryCode(code string) (ContentKey, error) {
	compact := strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, strings.ToUpper(code))
	if !strings.HasPrefix(compact, recoveryPrefix) {
		return ContentKey{}, ErrRecoveryCode
	}
	decoded, ok := base32Decode(strings.TrimPrefix(compact, recoveryPrefix))
	if !ok || len(decoded) != ContentKeyBytes+4 {
		return ContentKey{}, ErrRecoveryCode
	}
	secret := decoded[:ContentKeyBytes]
	if !hmac.Equal(decoded[ContentKeyBytes:], recoveryChecksum(secret)) {
		return ContentKey{}, ErrRecoveryChecksum
	}
	return ContentKeyFromBytes(secret)
}

func recoveryChecksum(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte(recoveryDomain), secret...))
	return sum[:4]
}

// DeviceKey is this device's Ed25519 authentication key.
type DeviceKey struct {
	seed []byte
	priv ed25519.PrivateKey
}

// NewDeviceKey draws a fresh key. Pass nil for crypto/rand.
func NewDeviceKey(source io.Reader) (DeviceKey, error) {
	if source == nil {
		source = rand.Reader
	}
	seed := make([]byte, DeviceKeySeedBytes)
	if _, err := io.ReadFull(source, seed); err != nil {
		return DeviceKey{}, err
	}
	return DeviceKeyFromSeed(seed)
}

// DeviceKeyFromSeed adopts a stored 32-byte seed. CryptoKit's
// Curve25519.Signing.PrivateKey.rawRepresentation is exactly this seed, so a
// key written by the Swift app and one written here are the same 32 bytes.
func DeviceKeyFromSeed(seed []byte) (DeviceKey, error) {
	if len(seed) != DeviceKeySeedBytes {
		return DeviceKey{}, fmt.Errorf("%w: %d", ErrSeedLength, len(seed))
	}
	copied := append([]byte(nil), seed...)
	return DeviceKey{seed: copied, priv: ed25519.NewKeyFromSeed(copied)}, nil
}

// Seed is a copy of the stored private material.
func (k DeviceKey) Seed() []byte { return append([]byte(nil), k.seed...) }

// Valid reports whether this key holds material. The zero DeviceKey does not.
func (k DeviceKey) Valid() bool { return len(k.seed) == DeviceKeySeedBytes }

// PublicKey is the 32-byte public half.
func (k DeviceKey) PublicKey() ed25519.PublicKey {
	if !k.Valid() {
		return nil
	}
	return k.priv.Public().(ed25519.PublicKey)
}

// Sign produces the 64-byte signature.
func (k DeviceKey) Sign(message []byte) []byte {
	if !k.Valid() {
		return nil
	}
	return ed25519.Sign(k.priv, message)
}

// Fingerprint is this key's pairing fingerprint.
func (k DeviceKey) Fingerprint() string { return Fingerprint(k.PublicKey()) }

// Fingerprint is the 80-bit SHA-256 prefix of a public key, in uppercase
// unpadded RFC 4648 base32, grouped in fours: DTGF-MRHB-3IYP-RIER.
//
// This is the short value two people compare while pairing. It is not a key id
// and carries no authority by itself.
func Fingerprint(public ed25519.PublicKey) string {
	if len(public) != ed25519.PublicKeySize {
		return ""
	}
	digest := sha256.Sum256(public)
	return groupBase32(base32Encode(digest[:fingerprintBytes]), fingerprintGroup)
}

// PublicKeyFromBytes checks a stored or received public key's length. Nothing
// else about it can be checked: every 32-byte string is a syntactically valid
// Ed25519 point, which is why a pin, not a format, is what makes a sender
// trustworthy.
func PublicKeyFromBytes(raw []byte) (ed25519.PublicKey, error) {
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: %d", ErrPublicKeyLength, len(raw))
	}
	return ed25519.PublicKey(append([]byte(nil), raw...)), nil
}

// MARK: base32

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// base32Encode is RFC 4648 without padding: the trailing partial group is
// zero-filled on the right, which is the one spelling the decoder accepts back.
func base32Encode(data []byte) string {
	var out strings.Builder
	var buffer uint32
	bits := 0
	for _, b := range data {
		buffer = buffer<<8 | uint32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out.WriteByte(base32Alphabet[(buffer>>uint(bits))&31])
		}
		buffer &= (1 << uint(bits)) - 1
	}
	if bits > 0 {
		out.WriteByte(base32Alphabet[(buffer<<uint(5-bits))&31])
	}
	return out.String()
}

// base32Decode refuses non-zero unused bits: accepting both spellings would
// make a checksummed recovery code needlessly non-canonical.
func base32Decode(text string) ([]byte, bool) {
	var out []byte
	var buffer uint32
	bits := 0
	for i := 0; i < len(text); i++ {
		value := strings.IndexByte(base32Alphabet, text[i])
		if value < 0 {
			return nil, false
		}
		buffer = buffer<<5 | uint32(value)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte((buffer>>uint(bits))&0xff))
		}
		buffer &= (1 << uint(bits)) - 1
	}
	if bits > 0 && buffer != 0 {
		return nil, false
	}
	return out, true
}

func groupBase32(text string, size int) string {
	var out strings.Builder
	for i := 0; i < len(text); i += size {
		if i > 0 {
			out.WriteByte('-')
		}
		end := min(i+size, len(text))
		out.WriteString(text[i:end])
	}
	return out.String()
}

// KeyStore is where this machine's two long-lived cloud secrets live.
//
// It is the seam for an operating-system keystore. Today
// internal/adapters/cloudkeys keeps them as owner-only files under this app's
// own directory, which is the one store macOS, Windows and Linux all have;
// later that adapter grows a Keychain, a Credential Manager and a Secret
// Service implementation behind this same interface, with the file store
// staying as the fallback for a Linux without a desktop session.
//
// Loading answers (zero value, false, nil) for "not there yet", so a caller can
// tell an absent secret from an unreadable one. An unreadable secret is an
// error and must never be read as an absent one: that difference is what stops
// a failed read from silently minting a new identity and orphaning the user's
// paired devices.
type KeyStore interface {
	DeviceKey() (DeviceKey, bool, error)
	SaveDeviceKey(DeviceKey) error
	MasterSecret() (ContentKey, bool, error)
	SaveMasterSecret(ContentKey) error
	// Forget removes both secrets. Signing out of the account must not leave
	// key material behind.
	Forget() error
}

// LoadOrCreateDeviceKey returns this machine's device key, minting one the
// first time. A read failure is returned as it is; only a confirmed absence
// mints.
func LoadOrCreateDeviceKey(store KeyStore, source io.Reader) (DeviceKey, error) {
	key, found, err := store.DeviceKey()
	if err != nil {
		return DeviceKey{}, err
	}
	if found {
		return key, nil
	}
	key, err = NewDeviceKey(source)
	if err != nil {
		return DeviceKey{}, err
	}
	if err := store.SaveDeviceKey(key); err != nil {
		return DeviceKey{}, err
	}
	return key, nil
}

// LoadOrCreateMasterSecret returns the account master secret, minting one the
// first time.
//
// Minting is right only for an account this machine is the first device of.
// A machine joining an existing account receives the secret through pairing and
// must call SaveMasterSecret with what it received; minting there would leave
// it unable to read anything the account already holds.
func LoadOrCreateMasterSecret(store KeyStore, source io.Reader) (ContentKey, error) {
	secret, found, err := store.MasterSecret()
	if err != nil {
		return ContentKey{}, err
	}
	if found {
		return secret, nil
	}
	secret, err = NewContentKey(source)
	if err != nil {
		return ContentKey{}, err
	}
	if err := store.SaveMasterSecret(secret); err != nil {
		return ContentKey{}, err
	}
	return secret, nil
}
