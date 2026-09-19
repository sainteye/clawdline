package cloud

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The ten-field JSON object the relay accepts, and nothing else.
//
//	{"v":1,"ch":"s/<machine>/<session>","seq":412,"ts":1787740000000,
//	 "class":"stream","key_id":"ms-1","nonce":"<base64 12 bytes>",
//	 "ct":"<base64 ciphertext||tag>","sender":"<device id>",
//	 "sig":"<base64 64 bytes>"}
//
// Nonce, ct and sig keep their canonical padded standard-base64 *spelling*
// throughout, because the signature is over that text and not over the bytes
// behind it. Re-encoding one of them — base64url, an unpadded spelling, a
// round trip through a permissive decoder — changes what was signed while
// leaving the value equal, which is the single most expensive mistake available
// in this file.
//
// Ported from ~/code/clawdline/Sources/CloudEnvelope.swift and checked against
// the cloud service's copy of the same contract. See docs/cloud-wire.md §2.

// The wire's fixed sizes and bounds.
const (
	// EnvelopeVersion is the only accepted v.
	EnvelopeVersion = 1
	// NonceBytes is AES-256-GCM's 96-bit nonce (D16). PROTOCOL.md §1's sample
	// says 24; every implementation and D16 itself say 12, and the contract
	// package records that as GAP-NONCE.
	NonceBytes = 12
	// TagBytes is the AES-GCM tag appended to the ciphertext inside ct.
	TagBytes = 16
	// SignatureBytes is an Ed25519 signature.
	SignatureBytes = 64
	// MaxChannelBytes bounds the whole channel string.
	MaxChannelBytes = 300
	// MaxSegmentBytes bounds one channel segment and the sender.
	MaxSegmentBytes = 128
	// MaxKeyIDBytes bounds key_id.
	MaxKeyIDBytes = 64
	// MaxCiphertextBytes is the largest ciphertext whose base64 frame still fits
	// Cloudflare's 32 MiB WebSocket message: floor((32 MiB - 4 KiB) * 3/4).
	// An account's own cap is separate and lower; do not read this as an
	// entitlement.
	MaxCiphertextBytes = 25162752
	// AccountEnvelopeBytes is what every tier is given today for one
	// ciphertext, per PROTOCOL.md §12 "Byte caps". It is a policy number, not
	// a wire constant, and the entitlements answer overrides it.
	AccountEnvelopeBytes = 16 << 20
	// ReplyKeyIDBytes is the exact length of a ctlr response key id,
	// "rk-" plus 22 base64url characters.
	ReplyKeyIDBytes = 25
)

// Class is the envelope's coarse routing and billing label. It is deliberately
// the only thing about a message the cloud can read besides its channel.
type Class string

// The four classes. dispatch marks a work-dispatching command — the metadata
// boundary already declares 派工次數 cloud-visible — and every other command
// stays an indistinct ctl.
const (
	ClassStream   Class = "stream"
	ClassCtl      Class = "ctl"
	ClassDispatch Class = "dispatch"
	ClassHo       Class = "ho"
)

// What the wire shape refuses. Each is a sentinel; the wrapped text names the
// field.
var (
	ErrEnvelopeVersion    = errors.New("unsupported envelope version")
	ErrUnsafeInteger      = errors.New("outside the relay's safe-integer range")
	ErrInvalidChannel     = errors.New("invalid channel")
	ErrClassChannel       = errors.New("the class does not match its channel")
	ErrInvalidToken       = errors.New("invalid token")
	ErrInvalidBase64      = errors.New("not canonical padded base64")
	ErrNonceLength        = errors.New("the AES-GCM nonce is not 12 bytes")
	ErrCiphertext         = errors.New("the ciphertext is unusable")
	ErrSignatureLength    = errors.New("the signature is not 64 bytes")
	ErrUnknownSender      = errors.New("no pinned public key for the sender")
	ErrBadSignature       = errors.New("the signature does not verify")
	ErrEnvelopeFields     = errors.New("the envelope must carry exactly its ten fields")
	ErrReservedChannel    = errors.New("a reserved channel prefix")
	ErrReplyKeyID         = errors.New("a ctlr response needs an rk- key id")
	ErrCiphertextTooBig   = errors.New("the ciphertext is past the wire maximum")
	ErrDecryptFailed      = errors.New("the ciphertext did not authenticate")
	ErrKeyLength          = errors.New("a content key must be 32 bytes")
	ErrSeedLength         = errors.New("a device key seed must be 32 bytes")
	ErrRecoveryCode       = errors.New("not a Clawdline recovery code")
	ErrRecoveryChecksum   = errors.New("the recovery code's checksum does not match")
	ErrPublicKeyLength    = errors.New("an Ed25519 public key must be 32 bytes")
	ErrChannelUnsupported = errors.New("this build does not produce that channel")
)

// Envelope is one message on the wire.
type Envelope struct {
	V      int
	Ch     string
	Seq    uint64
	Ts     uint64
	Class  Class
	KeyID  string
	Nonce  string
	Ct     string
	Sender string
	Sig    string
}

// envelopeFields is the exact member set, in the canonical (sorted) order the
// relay, the Swift app and the contract fixtures all write.
var envelopeFields = []string{"ch", "class", "ct", "key_id", "nonce", "sender", "seq", "sig", "ts", "v"}

// SigningString is v|ch|seq|ts|class|key_id|nonce|ct — that order, seven ASCII
// pipes, no prefix, suffix, BOM, NUL, newline or extra space. Integers are
// plain base 10. sender and sig are NOT covered: sender must be resolved to a
// locally pinned key before verification, because a caller-chosen key would
// erase that binding.
func (e Envelope) SigningString() string {
	return strings.Join([]string{
		strconv.Itoa(e.V),
		e.Ch,
		strconv.FormatUint(e.Seq, 10),
		strconv.FormatUint(e.Ts, 10),
		string(e.Class),
		e.KeyID,
		e.Nonce,
		e.Ct,
	}, "|")
}

// SigningBytes is SigningString as UTF-8.
func (e Envelope) SigningBytes() []byte { return []byte(e.SigningString()) }

// Value is the envelope as a canonical-JSON value.
func (e Envelope) Value() Value {
	return Object(map[string]Value{
		"v":      Int(int64(e.V)),
		"ch":     Str(e.Ch),
		"seq":    Int(int64(e.Seq)),
		"ts":     Int(int64(e.Ts)),
		"class":  Str(string(e.Class)),
		"key_id": Str(e.KeyID),
		"nonce":  Str(e.Nonce),
		"ct":     Str(e.Ct),
		"sender": Str(e.Sender),
		"sig":    Str(e.Sig),
	})
}

// CanonicalJSON is the envelope's RFC 8785 bytes: members in UTF-16 code-unit
// order, which for these ten ASCII names is
// ch, class, ct, key_id, nonce, sender, seq, sig, ts, v.
//
// The relay does not require this spelling — it parses whatever JSON arrives —
// but the contract fixtures are pinned to it, and a deterministic spelling is
// what makes a digest of an envelope mean anything.
func (e Envelope) CanonicalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	value := e.Value()
	if err := ValidateNumberDomain(value); err != nil {
		return nil, err
	}
	return CanonicalBytes(value), nil
}

// DecodeEnvelope reads one envelope from wire JSON. The member set must be
// exactly the ten fields — no unknown member, no missing one — and the result
// must pass the whole signed wire shape.
//
// The bytes need not be canonical: a browser's JSON.stringify writes members in
// insertion order.
func DecodeEnvelope(data []byte) (Envelope, error) {
	value, err := Parse(data)
	if err != nil {
		return Envelope{}, err
	}
	keys, ok := value.Keys()
	if !ok {
		return Envelope{}, fmt.Errorf("%w: not a JSON object", ErrEnvelopeFields)
	}
	if len(keys) != len(envelopeFields) {
		return Envelope{}, fmt.Errorf("%w: found %d", ErrEnvelopeFields, len(keys))
	}
	for _, name := range envelopeFields {
		if _, present := value.Member(name); !present {
			return Envelope{}, fmt.Errorf("%w: %s is missing", ErrEnvelopeFields, name)
		}
	}
	text := func(name string) (string, error) {
		member, _ := value.Member(name)
		s, ok := member.Str()
		if !ok {
			return "", fmt.Errorf("%w: %s is not a string", ErrEnvelopeFields, name)
		}
		return s, nil
	}
	number := func(name string) (int64, error) {
		member, _ := value.Member(name)
		i, ok := member.Int()
		if !ok {
			return 0, fmt.Errorf("%w: %s is not an integer", ErrEnvelopeFields, name)
		}
		if i < 0 {
			return 0, fmt.Errorf("%w: %s is negative", ErrUnsafeInteger, name)
		}
		return i, nil
	}

	var e Envelope
	version, err := number("v")
	if err != nil {
		return Envelope{}, err
	}
	e.V = int(version)
	if e.Ch, err = text("ch"); err != nil {
		return Envelope{}, err
	}
	seq, err := number("seq")
	if err != nil {
		return Envelope{}, err
	}
	e.Seq = uint64(seq)
	ts, err := number("ts")
	if err != nil {
		return Envelope{}, err
	}
	e.Ts = uint64(ts)
	class, err := text("class")
	if err != nil {
		return Envelope{}, err
	}
	e.Class = Class(class)
	if e.KeyID, err = text("key_id"); err != nil {
		return Envelope{}, err
	}
	if e.Nonce, err = text("nonce"); err != nil {
		return Envelope{}, err
	}
	if e.Ct, err = text("ct"); err != nil {
		return Envelope{}, err
	}
	if e.Sender, err = text("sender"); err != nil {
		return Envelope{}, err
	}
	if e.Sig, err = text("sig"); err != nil {
		return Envelope{}, err
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Validate is the whole signed wire shape, signature length included.
func (e Envelope) Validate() error {
	if err := e.validateUnsigned(); err != nil {
		return err
	}
	signature, err := DecodeCanonicalBase64(e.Sig)
	if err != nil {
		return fmt.Errorf("%w: sig", err)
	}
	if len(signature) != SignatureBytes {
		return fmt.Errorf("%w: %d", ErrSignatureLength, len(signature))
	}
	return nil
}

// validateUnsigned is everything the signature covers, which is everything but
// sender and sig. Seal checks this before it signs.
func (e Envelope) validateUnsigned() error {
	if e.V != EnvelopeVersion {
		return fmt.Errorf("%w: %d", ErrEnvelopeVersion, e.V)
	}
	if e.Seq > uint64(MaxSafeInteger) {
		return fmt.Errorf("%w: seq", ErrUnsafeInteger)
	}
	if e.Ts > uint64(MaxSafeInteger) {
		return fmt.Errorf("%w: ts", ErrUnsafeInteger)
	}
	kind, err := channelKindOf(e.Ch)
	if err != nil {
		return err
	}
	if !kind.allows(e.Class) {
		return fmt.Errorf("%w: %s on %s", ErrClassChannel, e.Class, kind.prefix)
	}
	if !validToken(e.KeyID, MaxKeyIDBytes) {
		return fmt.Errorf("%w: key_id", ErrInvalidToken)
	}
	if kind.prefix == "ctlr" && !IsReplyKeyID(e.KeyID) {
		return fmt.Errorf("%w: %q", ErrReplyKeyID, e.KeyID)
	}
	if !validToken(e.Sender, MaxSegmentBytes) {
		return fmt.Errorf("%w: sender", ErrInvalidToken)
	}
	nonce, err := DecodeCanonicalBase64(e.Nonce)
	if err != nil {
		return fmt.Errorf("%w: nonce", err)
	}
	if len(nonce) != NonceBytes {
		return fmt.Errorf("%w: %d", ErrNonceLength, len(nonce))
	}
	ciphertext, err := DecodeCanonicalBase64(e.Ct)
	if err != nil {
		return fmt.Errorf("%w: ct", err)
	}
	if len(ciphertext) == 0 {
		return fmt.Errorf("%w: empty", ErrCiphertext)
	}
	if len(ciphertext) > MaxCiphertextBytes {
		return fmt.Errorf("%w: %d bytes", ErrCiphertextTooBig, len(ciphertext))
	}
	return nil
}

// SealParams is everything Seal needs besides the plaintext.
type SealParams struct {
	Ch     string
	Seq    uint64
	Ts     uint64
	Class  Class
	KeyID  string
	Sender string
	// Key seals the payload: the account master secret for every ordinary
	// envelope, the request-scoped reply key for a ctlr response.
	Key ContentKey
	// Signer is this device's Ed25519 authentication key. The two key roles
	// are separate on purpose and must not be conflated.
	Signer DeviceKey
	// Nonce fixes the 12 AEAD bytes. Production callers leave it nil and get
	// fresh random bytes; a fixed nonce exists so the published protocol
	// vectors are reproducible, and reusing one under the same key destroys
	// AEAD security outright.
	Nonce []byte
	// Rand supplies the nonce when Nonce is nil. nil means crypto/rand.
	Rand io.Reader
}

// Seal encrypts, then signs. The order matters: the signature covers the
// base64 spelling of the ciphertext this call produced.
func Seal(plaintext []byte, params SealParams) (Envelope, error) {
	nonce := params.Nonce
	if nonce == nil {
		source := params.Rand
		if source == nil {
			source = rand.Reader
		}
		nonce = make([]byte, NonceBytes)
		if _, err := io.ReadFull(source, nonce); err != nil {
			return Envelope{}, err
		}
	}
	if len(nonce) != NonceBytes {
		return Envelope{}, fmt.Errorf("%w: %d", ErrNonceLength, len(nonce))
	}
	aead, err := params.Key.aead()
	if err != nil {
		return Envelope{}, err
	}
	// Go appends the tag to the ciphertext, which is exactly the ct layout.
	sealed := aead.Seal(nil, nonce, plaintext, nil)

	e := Envelope{
		V:      EnvelopeVersion,
		Ch:     params.Ch,
		Seq:    params.Seq,
		Ts:     params.Ts,
		Class:  params.Class,
		KeyID:  params.KeyID,
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		Ct:     base64.StdEncoding.EncodeToString(sealed),
		Sender: params.Sender,
	}
	if err := e.validateUnsigned(); err != nil {
		return Envelope{}, err
	}
	e.Sig = base64.StdEncoding.EncodeToString(params.Signer.Sign(e.SigningBytes()))
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// PublicKeyFor resolves an envelope's sender to the public key this machine
// pinned for it at pairing time. Answering nil is "not a device I know", which
// is a refusal and never a reason to try another key.
type PublicKeyFor func(sender string) (ed25519.PublicKey, bool)

// Verify checks the signature against the pinned key for this envelope's
// sender. A malformed envelope, an unknown sender and a bad signature are one
// answer here — false — because none of them may be acted on; Open separates
// them for the refusal log.
func (e Envelope) Verify(pinned PublicKeyFor) bool {
	if err := e.Validate(); err != nil {
		return false
	}
	key, ok := pinned(e.Sender)
	if !ok || len(key) != ed25519.PublicKeySize {
		return false
	}
	signature, err := DecodeCanonicalBase64(e.Sig)
	if err != nil {
		return false
	}
	return ed25519.Verify(key, e.SigningBytes(), signature)
}

// Open verifies the sender's signature and then opens the payload. Both, in
// that order: a ciphertext is never opened for a device this machine has not
// pinned, so a stolen account cannot produce a command any Mac will run.
func (e Envelope) Open(key ContentKey, pinned PublicKeyFor) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if _, ok := pinned(e.Sender); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSender, e.Sender)
	}
	if !e.Verify(pinned) {
		return nil, ErrBadSignature
	}
	nonce, err := DecodeCanonicalBase64(e.Nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: nonce", err)
	}
	sealed, err := DecodeCanonicalBase64(e.Ct)
	if err != nil {
		return nil, fmt.Errorf("%w: ct", err)
	}
	if len(sealed) < TagBytes {
		return nil, fmt.Errorf("%w: %d bytes cannot hold a tag", ErrCiphertext, len(sealed))
	}
	aead, err := key.aead()
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// MARK: channels

// channelKind is one allowed prefix together with its arity and classes.
type channelKind struct {
	prefix   string
	segments int
	classes  []Class
}

func (k channelKind) allows(c Class) bool {
	for _, allowed := range k.classes {
		if allowed == c {
			return true
		}
	}
	return false
}

// channelKinds is the whole vocabulary, as the cloud service's contract lists
// it.
//
// ctlr is the response rail the contract pins and the relay already routes; the
// Swift app has no branch for it (contract gap GAP-CTLR), so this reader
// accepts it while ProduceChannel refuses to *emit* one until the additive
// readers are deployed.
var channelKinds = []channelKind{
	{prefix: "s", segments: 2, classes: []Class{ClassStream}},
	{prefix: "t", segments: 2, classes: []Class{ClassStream}},
	{prefix: "orch", segments: 1, classes: []Class{ClassStream}},
	{prefix: "ctl", segments: 1, classes: []Class{ClassCtl, ClassDispatch}},
	{prefix: "ctlr", segments: 2, classes: []Class{ClassCtl}},
	{prefix: "ho", segments: 2, classes: []Class{ClassHo}},
}

// ReservedChannelPrefixes are the prefixes nothing may squat on. wh/ is held
// for the deferred webhook-out feature (PROTOCOL.md §10).
var ReservedChannelPrefixes = []string{"wh"}

func channelKindOf(channel string) (channelKind, error) {
	if channel == "" || len(channel) > MaxChannelBytes {
		return channelKind{}, fmt.Errorf("%w: %d bytes", ErrInvalidChannel, len(channel))
	}
	parts := strings.Split(channel, "/")
	prefix := parts[0]
	for _, reserved := range ReservedChannelPrefixes {
		if prefix == reserved {
			return channelKind{}, fmt.Errorf("%w: %s", ErrReservedChannel, prefix)
		}
	}
	for _, segment := range parts[1:] {
		if !validToken(segment, MaxSegmentBytes) {
			return channelKind{}, fmt.Errorf("%w: a bad segment", ErrInvalidChannel)
		}
	}
	for _, kind := range channelKinds {
		if kind.prefix == prefix && len(parts)-1 == kind.segments {
			return kind, nil
		}
	}
	return channelKind{}, fmt.Errorf("%w: %s", ErrInvalidChannel, channel)
}

// ChannelClasses is the class set a channel allows, and whether the channel is
// one this protocol knows at all.
func ChannelClasses(channel string) ([]Class, bool) {
	kind, err := channelKindOf(channel)
	if err != nil {
		return nil, false
	}
	return append([]Class(nil), kind.classes...), true
}

// ProducibleChannel says whether this build may *publish* on a channel, as
// opposed to read one. It is narrower than validation on purpose: the contract
// requires additive readers everywhere before a producer is enabled, and ctlr
// has no Swift reader today (GAP-CTLR). Reading a ctlr envelope is fine; making
// one is a decision somebody records first.
func ProducibleChannel(channel string) error {
	kind, err := channelKindOf(channel)
	if err != nil {
		return err
	}
	if kind.prefix == "ctlr" {
		return fmt.Errorf("%w: ctlr, until its readers are deployed", ErrChannelUnsupported)
	}
	return nil
}

// validToken is the protocol's token alphabet: 1..maximum bytes of ASCII
// 0x21..0x7e, with "/" and "|" excluded. Those two are the separators of the
// channel and of the signing string; without excluding them two different
// envelopes could produce identical signed bytes.
func validToken(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x21 || c > 0x7e || c == '/' || c == '|' {
			return false
		}
	}
	return true
}

// IsReplyKeyID reports whether a key id is a ctlr response key: "rk-" and 22
// base64url characters. Matching this proves nothing about freshness,
// correlation or possession — those are the host's gates.
func IsReplyKeyID(keyID string) bool {
	if len(keyID) != ReplyKeyIDBytes || !strings.HasPrefix(keyID, "rk-") {
		return false
	}
	for i := 3; i < len(keyID); i++ {
		c := keyID[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// MARK: base64

// DecodeCanonicalBase64 accepts exactly one spelling: standard alphabet,
// padded, full quanta, zero unused bits. It decodes and then re-encodes and
// compares, because a permissive decoder accepts several spellings of the same
// bytes and only one of them is what was signed. Go's own decoder also skips
// newlines, which the alphabet check here refuses first.
func DecodeCanonicalBase64(text string) ([]byte, error) {
	if len(text)%4 != 0 {
		return nil, fmt.Errorf("%w: length %d", ErrInvalidBase64, len(text))
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '+', c == '/', c == '=':
		default:
			return nil, fmt.Errorf("%w: byte 0x%02x", ErrInvalidBase64, c)
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBase64, err)
	}
	if base64.StdEncoding.EncodeToString(decoded) != text {
		return nil, fmt.Errorf("%w: not the canonical spelling", ErrInvalidBase64)
	}
	return decoded, nil
}

// MARK: AEAD

func (k ContentKey) aead() (cipher.AEAD, error) {
	if len(k.raw) != ContentKeyBytes {
		return nil, fmt.Errorf("%w: %d", ErrKeyLength, len(k.raw))
	}
	block, err := aes.NewCipher(k.raw)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, NonceBytes)
}
