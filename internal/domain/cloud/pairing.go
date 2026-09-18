package cloud

// Pairing: how the account's master secret reaches a browser, and how a
// browser's signing key reaches this machine.
//
// This is the machine half of what `Resources/web/app/js/net/cloud-pairing.js`
// does in the browser and what `Sources/CloudHandover.swift` and
// `Sources/CloudPairing.swift` do in the Swift app. The three must agree byte
// for byte, so everything here is written out rather than reached for: the
// canonical JSON is this package's, the base64 spellings are checked by
// re-encoding, and the key derivation is HKDF written as its two HMACs because
// that is the shape the other two implementations have.
//
// The shape of one pairing, machine side:
//
//	1. This machine asks the control plane for an invitation id and shows a
//	   one-time 32-byte secret in a QR or a link. The cloud stores only
//	   SHA-256 of that secret.
//	2. The browser, already signed in to the same account, opens the link,
//	   asks the control plane for a `pairing_id` and a `claim_nonce`, builds a
//	   **pairing offer** carrying its Ed25519 public key and a fresh X25519
//	   public key, and hands it back through the invitation, encrypted under
//	   the QR secret.
//	3. This machine decrypts the offer, agrees an X25519 secret with the
//	   browser's ephemeral key, derives one phase key bound to this pairing,
//	   and seals a **pairing handover** — account id, machine id, this
//	   machine's signing public key and fingerprint, the key id, and the
//	   account master secret — into a seven-member **wrapper**.
//	4. The control plane carries those opaque bytes to the browser, which
//	   claims them exactly once.
//
// The cloud sees a hash, two opaque blobs and two device ids. It never sees the
// QR secret, the offer, the master secret, or any private key.
//
// **The fingerprint is not authority.** It is the short string two people
// compare out loud. What makes a viewer trustworthy here is that this machine
// pinned its public key at step 3 — see `internal/adapters/cloud`'s pin store.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Pairing constants. Every one of them is a value the browser also holds; a
// change here is a wire break.
const (
	// PairingOfferLifetimeMS is how long an offer may claim to live. An offer
	// is carried across a room, not kept: a long-lived one is a claim nonce
	// sitting on a screen (`CloudHandover.offerLifetimeMilliseconds`).
	PairingOfferLifetimeMS = 600_000
	// PairingPhaseGrant is the phase this handover travels in. `grant` and
	// `confirm` bind to the **machine's** ephemeral key, which is what the
	// wrapper carries; `offer` and `activate` bind to the viewer's.
	PairingPhaseGrant = "grant"
	// PairingNonceBytes is the pairing and claim nonce length.
	PairingNonceBytes = 32
	// PairingSecretBytes is the invitation secret length.
	PairingSecretBytes = 32
	// PairingEphemeralBytes is an X25519 public or private key.
	PairingEphemeralBytes = 32
	// MaxPairingBodyBytes is the control plane's phase-body bound
	// (`PAIRING_PHASE_MAX_BYTES`). A wrapper past it can never fit in a legal
	// body, so it is refused here rather than by a 413 later.
	MaxPairingBodyBytes = 65_536
	// maxPairingIDBytes bounds every id that travels in these documents.
	maxPairingIDBytes = 128

	pairingSaltDomain    = "clawdline-pair-salt-v1"
	pairingInfoDomain    = "clawdline-pair-v1"
	pairingInvitationAAD = "clawdline-pairing-invitation-v1\x00"
	pairingOfferType     = "pairing_offer"
	pairingHandoverType  = "pairing_handover"
	pairingInvitationTyp = "pairing_invitation"
	// PairingFragmentName is the URL fragment the hosted console reads an
	// invitation out of: `https://app.clawdline.com/#pair=<fragment>`.
	PairingFragmentName = "pair"
)

// Pairing errors. They are separate values rather than one because each of
// them sends a person somewhere different: an expired offer means "do it
// again", a fingerprint mismatch means "that was not the browser you think".
var (
	ErrPairingFields      = errors.New("the pairing document does not have its exact members")
	ErrPairingVersion     = errors.New("the pairing document is not version 1")
	ErrPairingType        = errors.New("the pairing document is not the type it should be")
	ErrPairingID          = errors.New("a pairing id must be 1..128 bytes of printable ASCII")
	ErrPairingBase64URL   = errors.New("not canonical unpadded base64url")
	ErrPairingNonceReuse  = errors.New("the claim nonce and the pairing nonce are the same bytes")
	ErrPairingFingerprint = errors.New("the fingerprint does not match the key it travels with")
	ErrPairingExpired     = errors.New("that pairing offer has expired")
	ErrPairingLifetime    = errors.New("that pairing offer claims an unusable lifetime")
	ErrPairingAccount     = errors.New("the pairing document names another account")
	ErrPairingSender      = errors.New("the pairing wrapper was sealed by a different device")
	ErrPairingKeyBinding  = errors.New("the wrapper's ephemeral key is not the one its phase binds to")
	ErrPairingAgreement   = errors.New("the X25519 agreement produced an unusable secret")
	ErrPairingSealed      = errors.New("the sealed pairing bytes did not authenticate")
	ErrPairingBodyTooBig  = errors.New("the pairing body is past the wire maximum")
	ErrInvitationExpired  = errors.New("that pairing invitation has expired")
)

// MARK: offer

// PairingOffer is what the browser hands this machine: who it is, the two
// public keys it wants answered, and the routing handle the control plane gave
// it. Every binary member is canonical padded base64 as a string, because that
// is what is inside the canonical JSON both sides hash.
type PairingOffer struct {
	PairingID          string
	ClaimNonce         string
	PairingNonce       string
	AccountID          string
	ViewerDeviceID     string
	ViewerSigningKey   string
	ViewerEphemeralKey string
	ViewerFingerprint  string
	ExpiresAt          int64
}

var pairingOfferMembers = []string{
	"account_id", "claim_nonce", "expires_at", "pairing_id", "pairing_nonce",
	"type", "v", "viewer_device_id", "viewer_ephemeral_key",
	"viewer_fingerprint", "viewer_signing_key",
}

// Value is the offer as the canonical document both sides sign over.
func (o PairingOffer) Value() Value {
	return Object(map[string]Value{
		"v":                    Int(1),
		"type":                 Str(pairingOfferType),
		"pairing_id":           Str(o.PairingID),
		"claim_nonce":          Str(o.ClaimNonce),
		"pairing_nonce":        Str(o.PairingNonce),
		"account_id":           Str(o.AccountID),
		"viewer_device_id":     Str(o.ViewerDeviceID),
		"viewer_signing_key":   Str(o.ViewerSigningKey),
		"viewer_ephemeral_key": Str(o.ViewerEphemeralKey),
		"viewer_fingerprint":   Str(o.ViewerFingerprint),
		"expires_at":           Int(o.ExpiresAt),
	})
}

// CanonicalJSON is the offer's bytes.
func (o PairingOffer) CanonicalJSON() []byte { return CanonicalBytes(o.Value()) }

// Fragment is the offer as the browser shows it: canonical JSON in unpadded
// base64url. It is what a person copies from the browser to this machine when
// there is no camera in the room.
func (o PairingOffer) Fragment() string {
	return EncodeCanonicalBase64URL(o.CanonicalJSON())
}

// DecodePairingOfferFragment reads one offer, and refuses it unless every rule
// holds: exact members, canonical bytes, the fingerprint matching the key it
// travels with, and a live expiry inside the one lifetime an offer may claim.
//
// `nowMS` is passed rather than read so that the same fragment is judged the
// same way by a test, by the daemon and by the fixture that proves the two
// implementations agree.
func DecodePairingOfferFragment(fragment string, nowMS int64) (PairingOffer, error) {
	raw, err := DecodeCanonicalBase64URL(fragment)
	if err != nil {
		return PairingOffer{}, err
	}
	return DecodePairingOffer(raw, nowMS)
}

// DecodePairingOffer reads one offer from its canonical bytes.
func DecodePairingOffer(raw []byte, nowMS int64) (PairingOffer, error) {
	if len(raw) > MaxPairingBodyBytes {
		return PairingOffer{}, ErrPairingBodyTooBig
	}
	value, err := ParseStrict(raw)
	if err != nil {
		return PairingOffer{}, err
	}
	if err := exactMembers(value, pairingOfferMembers); err != nil {
		return PairingOffer{}, err
	}
	if err := requireVersionAndType(value, pairingOfferType); err != nil {
		return PairingOffer{}, err
	}
	expires, ok := memberInt(value, "expires_at")
	if !ok {
		return PairingOffer{}, ErrPairingFields
	}
	offer := PairingOffer{
		PairingID:          memberString(value, "pairing_id"),
		ClaimNonce:         memberString(value, "claim_nonce"),
		PairingNonce:       memberString(value, "pairing_nonce"),
		AccountID:          memberString(value, "account_id"),
		ViewerDeviceID:     memberString(value, "viewer_device_id"),
		ViewerSigningKey:   memberString(value, "viewer_signing_key"),
		ViewerEphemeralKey: memberString(value, "viewer_ephemeral_key"),
		ViewerFingerprint:  memberString(value, "viewer_fingerprint"),
		ExpiresAt:          expires,
	}
	if err := offer.Validate(nowMS); err != nil {
		return PairingOffer{}, err
	}
	return offer, nil
}

// Validate refuses an offer this machine will not answer.
func (o PairingOffer) Validate(nowMS int64) error {
	for _, id := range []string{o.PairingID, o.AccountID, o.ViewerDeviceID} {
		if err := validPairingID(id); err != nil {
			return err
		}
	}
	claim, err := decodeFixedBase64(o.ClaimNonce, PairingNonceBytes)
	if err != nil {
		return err
	}
	pairing, err := decodeFixedBase64(o.PairingNonce, PairingNonceBytes)
	if err != nil {
		return err
	}
	// Not a theoretical guard: the salt commits to both, and two equal nonces
	// would make it say less than it looks like.
	if string(claim) == string(pairing) {
		return ErrPairingNonceReuse
	}
	signing, err := decodeFixedBase64(o.ViewerSigningKey, 32)
	if err != nil {
		return err
	}
	if _, err := decodeFixedBase64(o.ViewerEphemeralKey, PairingEphemeralBytes); err != nil {
		return err
	}
	if o.ViewerFingerprint != Fingerprint(signing) {
		return ErrPairingFingerprint
	}
	if o.ExpiresAt < 0 || nowMS < 0 {
		return fmt.Errorf("%w: a pairing clock before the epoch", ErrUnsafeInteger)
	}
	if o.ExpiresAt < nowMS {
		return ErrPairingExpired
	}
	if o.ExpiresAt-nowMS > PairingOfferLifetimeMS {
		return ErrPairingLifetime
	}
	return nil
}

// MARK: handover

// PairingHandover is what this machine seals for the browser: the account's
// master secret and the key this machine will sign with, so that the browser
// can open what this machine publishes and pin who published it.
type PairingHandover struct {
	AccountID          string
	MachineID          string
	MachineSigningKey  string
	MachineFingerprint string
	KeyID              string
	MasterSecret       string
}

var pairingHandoverMembers = []string{
	"account_id", "key_id", "machine_fingerprint", "machine_id",
	"machine_signing_key", "master_secret", "type", "v",
}

// Value is the handover as the canonical document that gets sealed.
func (h PairingHandover) Value() Value {
	return Object(map[string]Value{
		"v":                   Int(1),
		"type":                Str(pairingHandoverType),
		"account_id":          Str(h.AccountID),
		"machine_id":          Str(h.MachineID),
		"machine_signing_key": Str(h.MachineSigningKey),
		"machine_fingerprint": Str(h.MachineFingerprint),
		"key_id":              Str(h.KeyID),
		"master_secret":       Str(h.MasterSecret),
	})
}

// CanonicalJSON is the handover's bytes.
func (h PairingHandover) CanonicalJSON() []byte { return CanonicalBytes(h.Value()) }

// Validate refuses a handover that would not open cleanly on the other side.
func (h PairingHandover) Validate() error {
	for _, id := range []string{h.AccountID, h.MachineID, h.KeyID} {
		if err := validPairingID(id); err != nil {
			return err
		}
	}
	signing, err := decodeFixedBase64(h.MachineSigningKey, 32)
	if err != nil {
		return err
	}
	if _, err := decodeFixedBase64(h.MasterSecret, ContentKeyBytes); err != nil {
		return err
	}
	if h.MachineFingerprint != Fingerprint(signing) {
		return ErrPairingFingerprint
	}
	return nil
}

// MARK: wrapper

// PairingWrapper is the seven-member envelope the control plane carries. Five
// of its members are the AEAD's additional data; `nonce` and `ct` are
// deliberately outside it.
type PairingWrapper struct {
	Phase          string
	PairingID      string
	SenderDeviceID string
	EphemeralKey   string
	Nonce          string
	Ct             string
}

var pairingWrapperMembers = []string{
	"ct", "ephemeral_key", "nonce", "pairing_id", "phase", "sender_device_id", "v",
}

// Value is the wrapper as one canonical object.
func (w PairingWrapper) Value() Value {
	return Object(map[string]Value{
		"v":                Int(1),
		"phase":            Str(w.Phase),
		"pairing_id":       Str(w.PairingID),
		"sender_device_id": Str(w.SenderDeviceID),
		"ephemeral_key":    Str(w.EphemeralKey),
		"nonce":            Str(w.Nonce),
		"ct":               Str(w.Ct),
	})
}

// CanonicalJSON is what goes into `POST /v1/pairing/complete`, base64'd.
func (w PairingWrapper) CanonicalJSON() []byte { return CanonicalBytes(w.Value()) }

// AAD is the five authenticated members, canonically. It is rebuilt from the
// wrapper rather than remembered, so that a wrapper whose members were changed
// in flight authenticates against the changed bytes and fails.
func (w PairingWrapper) AAD() ([]byte, error) {
	if err := validPairingID(w.PairingID); err != nil {
		return nil, err
	}
	if err := validPairingID(w.SenderDeviceID); err != nil {
		return nil, err
	}
	if _, err := decodeFixedBase64(w.EphemeralKey, PairingEphemeralBytes); err != nil {
		return nil, err
	}
	return CanonicalBytes(Object(map[string]Value{
		"v":                Int(1),
		"phase":            Str(w.Phase),
		"pairing_id":       Str(w.PairingID),
		"sender_device_id": Str(w.SenderDeviceID),
		"ephemeral_key":    Str(w.EphemeralKey),
	})), nil
}

// DecodePairingWrapper reads a wrapper back. It exists for the round-trip test
// and for a viewer implemented here; the production viewer is a browser.
func DecodePairingWrapper(raw []byte) (PairingWrapper, error) {
	if len(raw) > MaxPairingBodyBytes {
		return PairingWrapper{}, ErrPairingBodyTooBig
	}
	value, err := ParseStrict(raw)
	if err != nil {
		return PairingWrapper{}, err
	}
	if err := exactMembers(value, pairingWrapperMembers); err != nil {
		return PairingWrapper{}, err
	}
	if version, ok := memberInt(value, "v"); !ok || version != 1 {
		return PairingWrapper{}, ErrPairingVersion
	}
	wrapper := PairingWrapper{
		Phase:          memberString(value, "phase"),
		PairingID:      memberString(value, "pairing_id"),
		SenderDeviceID: memberString(value, "sender_device_id"),
		EphemeralKey:   memberString(value, "ephemeral_key"),
		Nonce:          memberString(value, "nonce"),
		Ct:             memberString(value, "ct"),
	}
	if _, err := wrapper.AAD(); err != nil {
		return PairingWrapper{}, err
	}
	if _, err := decodeFixedBase64(wrapper.Nonce, NonceBytes); err != nil {
		return PairingWrapper{}, err
	}
	ct, err := DecodeCanonicalBase64(wrapper.Ct)
	if err != nil {
		return PairingWrapper{}, err
	}
	if len(ct) < 16 {
		return PairingWrapper{}, fmt.Errorf("%w: %d bytes", ErrCiphertext, len(ct))
	}
	return wrapper, nil
}

// MARK: invitation

// PairingInvitation is the one-time secret this machine shows. The secret is
// never logged, never sent to the control plane, and never leaves this process
// except inside the fragment a person carries.
type PairingInvitation struct {
	InvitationID string
	Secret       []byte
	ExpiresAt    int64
}

var pairingInvitationMembers = []string{"expires_at", "invitation_id", "secret", "type", "v"}

// NewPairingInvitationSecret draws a fresh invitation secret. Pass nil for
// crypto/rand.
func NewPairingInvitationSecret(source io.Reader) ([]byte, error) {
	if source == nil {
		source = rand.Reader
	}
	secret := make([]byte, PairingSecretBytes)
	if _, err := io.ReadFull(source, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// SecretHash is what the control plane is told: SHA-256 of the secret, and
// nothing else.
func (i PairingInvitation) SecretHash() []byte {
	sum := sha256.Sum256(i.Secret)
	return sum[:]
}

// Validate refuses an invitation this machine would not show.
func (i PairingInvitation) Validate() error {
	if err := validPairingID(i.InvitationID); err != nil {
		return err
	}
	if len(i.Secret) != PairingSecretBytes {
		return fmt.Errorf("%w: an invitation secret must be %d bytes", ErrKeyLength, PairingSecretBytes)
	}
	if i.ExpiresAt < 0 {
		return fmt.Errorf("%w: an invitation expiry before the epoch", ErrUnsafeInteger)
	}
	return nil
}

// Fragment is the invitation as the browser reads it out of `#pair=`.
func (i PairingInvitation) Fragment() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	return EncodeCanonicalBase64URL(CanonicalBytes(Object(map[string]Value{
		"v":             Int(1),
		"type":          Str(pairingInvitationTyp),
		"invitation_id": Str(i.InvitationID),
		"secret":        Base64(i.Secret),
		"expires_at":    Int(i.ExpiresAt),
	}))), nil
}

// URL is the fragment on an app origin, which is what a QR carries and what a
// person can paste into a browser that has no camera.
//
// The origin is a parameter because a local api and a local console are how
// this is tested; production is `https://app.clawdline.com/`.
func (i PairingInvitation) URL(appOrigin string) (string, error) {
	fragment, err := i.Fragment()
	if err != nil {
		return "", err
	}
	origin := strings.TrimRight(appOrigin, "/")
	if origin == "" {
		return "", errors.New("a pairing link needs an app origin")
	}
	return origin + "/#" + PairingFragmentName + "=" + fragment, nil
}

// DecodePairingInvitation reads an invitation fragment back. The machine does
// not need this — it made the invitation — but the fixture that proves this
// implementation and the browser's agree does.
func DecodePairingInvitation(fragment string, nowMS int64) (PairingInvitation, error) {
	raw, err := DecodeCanonicalBase64URL(fragment)
	if err != nil {
		return PairingInvitation{}, err
	}
	value, err := ParseStrict(raw)
	if err != nil {
		return PairingInvitation{}, err
	}
	if err := exactMembers(value, pairingInvitationMembers); err != nil {
		return PairingInvitation{}, err
	}
	if err := requireVersionAndType(value, pairingInvitationTyp); err != nil {
		return PairingInvitation{}, err
	}
	expires, ok := memberInt(value, "expires_at")
	if !ok {
		return PairingInvitation{}, ErrPairingFields
	}
	secret, err := decodeFixedBase64(memberString(value, "secret"), PairingSecretBytes)
	if err != nil {
		return PairingInvitation{}, err
	}
	invitation := PairingInvitation{
		InvitationID: memberString(value, "invitation_id"),
		Secret:       secret,
		ExpiresAt:    expires,
	}
	if err := invitation.Validate(); err != nil {
		return PairingInvitation{}, err
	}
	if invitation.ExpiresAt < nowMS {
		return PairingInvitation{}, ErrInvitationExpired
	}
	return invitation, nil
}

// OpenEncryptedOffer decrypts what the browser left in the invitation slot.
//
// `blob` is the base64 the control plane carried: a 12-byte nonce followed by
// the AES-GCM ciphertext and tag, authenticated by the invitation's own domain
// string and its id. The plaintext is the offer *fragment*, not the offer — the
// browser encrypts what it would otherwise have shown on screen.
func (i PairingInvitation) OpenEncryptedOffer(blob string, nowMS int64) (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	if nowMS < 0 || i.ExpiresAt < nowMS {
		return "", ErrInvitationExpired
	}
	combined, err := DecodeCanonicalBase64(blob)
	if err != nil {
		return "", err
	}
	if len(combined) < NonceBytes+16 {
		return "", fmt.Errorf("%w: %d bytes", ErrCiphertext, len(combined))
	}
	block, err := aes.NewCipher(i.Secret)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceBytes)
	if err != nil {
		return "", err
	}
	clear, err := aead.Open(nil, combined[:NonceBytes], combined[NonceBytes:],
		[]byte(pairingInvitationAAD+i.InvitationID))
	if err != nil {
		return "", ErrPairingSealed
	}
	if len(clear) == 0 {
		return "", ErrPairingSealed
	}
	return string(clear), nil
}

// SealEncryptedOffer is the browser's half of the invitation, in Go. Production
// viewers are browsers; this exists so the suite can prove the two agree
// without one.
func (i PairingInvitation) SealEncryptedOffer(offerFragment string, nonce []byte) (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	if len(nonce) != NonceBytes {
		return "", ErrNonceLength
	}
	block, err := aes.NewCipher(i.Secret)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceBytes)
	if err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nonce, []byte(offerFragment),
		[]byte(pairingInvitationAAD+i.InvitationID))
	return base64.StdEncoding.EncodeToString(append(append([]byte(nil), nonce...), sealed...)), nil
}

// MARK: agreement and derivation

// X25519PublicKey is the public half of a 32-byte X25519 private key.
func X25519PublicKey(privateRaw []byte) ([]byte, error) {
	if len(privateRaw) != PairingEphemeralBytes {
		return nil, fmt.Errorf("%w: %d", ErrKeyLength, len(privateRaw))
	}
	key, err := ecdh.X25519().NewPrivateKey(privateRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPairingAgreement, err)
	}
	return key.PublicKey().Bytes(), nil
}

// NewX25519PrivateKey draws a fresh ephemeral private key. Pass nil for
// crypto/rand.
func NewX25519PrivateKey(source io.Reader) ([]byte, error) {
	if source == nil {
		source = rand.Reader
	}
	raw := make([]byte, PairingEphemeralBytes)
	if _, err := io.ReadFull(source, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// PairingSharedSecret is X25519, refusing the results that must never reach a
// key derivation.
//
// A low-order peer key produces an all-zero secret, and an all-zero secret is
// the same secret for every peer: derive from it and both sides agree on a key
// an attacker also holds. Go's `crypto/ecdh` refuses those inputs itself; the
// length and all-zero checks after it are the negative vectors'
// `reject_before_hkdf` written down where a future rewrite can see them
// (`contracts/cloud/v1/crypto-negative-vectors.json`).
func PairingSharedSecret(privateRaw, peerPublicRaw []byte) ([]byte, error) {
	if len(privateRaw) != PairingEphemeralBytes || len(peerPublicRaw) != PairingEphemeralBytes {
		return nil, fmt.Errorf("%w: %d and %d", ErrKeyLength, len(privateRaw), len(peerPublicRaw))
	}
	private, err := ecdh.X25519().NewPrivateKey(privateRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPairingAgreement, err)
	}
	peer, err := ecdh.X25519().NewPublicKey(peerPublicRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPairingAgreement, err)
	}
	shared, err := private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPairingAgreement, err)
	}
	if len(shared) != 32 || allZero(shared) {
		return nil, ErrPairingAgreement
	}
	return shared, nil
}

// DerivePairingPhaseKey is HKDF-SHA256 with one expand block, written as its
// two HMACs because that is the shape the Swift and JavaScript sides have.
//
// The salt commits to the pairing nonce, the pairing id and the claim nonce, so
// a phase key belongs to one handover and to no other; the info commits to the
// phase, so a key derived for `grant` cannot open an `activate`.
func DerivePairingPhaseKey(shared, pairingNonce []byte, pairingID string, claimNonce []byte, phase string) ([]byte, error) {
	if len(shared) != 32 || allZero(shared) {
		return nil, ErrPairingAgreement
	}
	if len(pairingNonce) != PairingNonceBytes || len(claimNonce) != PairingNonceBytes {
		return nil, fmt.Errorf("%w: a pairing nonce must be %d bytes", ErrKeyLength, PairingNonceBytes)
	}
	if err := validPairingID(pairingID); err != nil {
		return nil, err
	}
	if phase == "" {
		phase = PairingPhaseGrant
	}
	var salted []byte
	salted = appendL16(salted, []byte(pairingSaltDomain))
	salted = appendL16(salted, pairingNonce)
	salted = appendL16(salted, []byte(pairingID))
	salted = appendL16(salted, claimNonce)
	salt := sha256.Sum256(salted)

	extract := hmac.New(sha256.New, salt[:])
	extract.Write(shared)
	prk := extract.Sum(nil)

	var info []byte
	info = appendL16(info, []byte(pairingInfoDomain))
	info = appendL16(info, []byte(phase))
	info = append(info, 0x01)

	expand := hmac.New(sha256.New, prk)
	expand.Write(info)
	return expand.Sum(nil), nil
}

// appendL16 is the length-prefixed concatenation both other implementations
// use: two big-endian length bytes and then the value. Without it, two fields
// of different lengths could be spelled the same way in one preimage.
func appendL16(out, value []byte) []byte {
	if len(value) > 0xffff {
		// Unreachable for every field this file feeds it: ids are bounded at
		// 128 bytes and nonces at 32. Truncating silently would be the bug.
		panic("clawdline: an L16 field is longer than 65535 bytes")
	}
	return append(append(out, byte(len(value)>>8), byte(len(value))), value...)
}

// MARK: seal and open

// SealPairingHandover is the machine half: agree a secret with the browser's
// ephemeral key, derive one phase key bound to this pairing, and seal the
// account material into the one slot the control plane will carry.
//
// `machineEphemeralPrivate` and `nonce` are parameters rather than drawn here
// so the cross-runtime fixture can reproduce these exact bytes. Production
// callers pass fresh random values; **a reused nonce under the same phase key
// destroys the AEAD**, which is why nothing in this package caches either.
func SealPairingHandover(handover PairingHandover, offer PairingOffer, machineDeviceID string, machineEphemeralPrivate, nonce []byte, nowMS int64) (PairingWrapper, error) {
	if err := offer.Validate(nowMS); err != nil {
		return PairingWrapper{}, err
	}
	if err := handover.Validate(); err != nil {
		return PairingWrapper{}, err
	}
	if handover.AccountID != offer.AccountID {
		return PairingWrapper{}, ErrPairingAccount
	}
	if err := validPairingID(machineDeviceID); err != nil {
		return PairingWrapper{}, err
	}
	if len(nonce) != NonceBytes {
		return PairingWrapper{}, ErrNonceLength
	}
	phaseKey, err := pairingPhaseKeyFor(offer, machineEphemeralPrivate, offer.ViewerEphemeralKey)
	if err != nil {
		return PairingWrapper{}, err
	}
	ephemeralPublic, err := X25519PublicKey(machineEphemeralPrivate)
	if err != nil {
		return PairingWrapper{}, err
	}
	wrapper := PairingWrapper{
		Phase:          PairingPhaseGrant,
		PairingID:      offer.PairingID,
		SenderDeviceID: machineDeviceID,
		EphemeralKey:   base64.StdEncoding.EncodeToString(ephemeralPublic),
		Nonce:          base64.StdEncoding.EncodeToString(nonce),
	}
	aad, err := wrapper.AAD()
	if err != nil {
		return PairingWrapper{}, err
	}
	block, err := aes.NewCipher(phaseKey)
	if err != nil {
		return PairingWrapper{}, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceBytes)
	if err != nil {
		return PairingWrapper{}, err
	}
	wrapper.Ct = base64.StdEncoding.EncodeToString(
		aead.Seal(nil, nonce, handover.CanonicalJSON(), aad))
	if len(wrapper.CanonicalJSON()) > MaxPairingBodyBytes {
		return PairingWrapper{}, ErrPairingBodyTooBig
	}
	return wrapper, nil
}

// OpenPairingHandover is the viewer half, in Go. Production viewers are
// browsers running the JavaScript mirror; this exists so the suite can prove
// the two agree without one, and so a round trip is one test rather than two
// implementations nobody compares.
func OpenPairingHandover(wrapper PairingWrapper, offer PairingOffer, viewerEphemeralPrivate []byte, senderDeviceID string, nowMS int64) (PairingHandover, error) {
	if err := offer.Validate(nowMS); err != nil {
		return PairingHandover{}, err
	}
	if wrapper.Phase != PairingPhaseGrant {
		return PairingHandover{}, ErrPairingVersion
	}
	if wrapper.PairingID != offer.PairingID {
		return PairingHandover{}, ErrPairingSender
	}
	// The control plane says which device wrote the slot. Comparing it to the
	// wrapper closes the one thing the AEAD cannot say by itself.
	if senderDeviceID != "" && senderDeviceID != wrapper.SenderDeviceID {
		return PairingHandover{}, ErrPairingSender
	}
	phaseKey, err := pairingPhaseKeyFor(offer, viewerEphemeralPrivate, wrapper.EphemeralKey)
	if err != nil {
		return PairingHandover{}, err
	}
	aad, err := wrapper.AAD()
	if err != nil {
		return PairingHandover{}, err
	}
	nonce, err := decodeFixedBase64(wrapper.Nonce, NonceBytes)
	if err != nil {
		return PairingHandover{}, err
	}
	ct, err := DecodeCanonicalBase64(wrapper.Ct)
	if err != nil {
		return PairingHandover{}, err
	}
	block, err := aes.NewCipher(phaseKey)
	if err != nil {
		return PairingHandover{}, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceBytes)
	if err != nil {
		return PairingHandover{}, err
	}
	clear, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return PairingHandover{}, ErrPairingSealed
	}
	value, err := ParseStrict(clear)
	if err != nil {
		return PairingHandover{}, err
	}
	if err := exactMembers(value, pairingHandoverMembers); err != nil {
		return PairingHandover{}, err
	}
	if err := requireVersionAndType(value, pairingHandoverType); err != nil {
		return PairingHandover{}, err
	}
	handover := PairingHandover{
		AccountID:          memberString(value, "account_id"),
		MachineID:          memberString(value, "machine_id"),
		MachineSigningKey:  memberString(value, "machine_signing_key"),
		MachineFingerprint: memberString(value, "machine_fingerprint"),
		KeyID:              memberString(value, "key_id"),
		MasterSecret:       memberString(value, "master_secret"),
	}
	if err := handover.Validate(); err != nil {
		return PairingHandover{}, err
	}
	if handover.AccountID != offer.AccountID {
		return PairingHandover{}, ErrPairingAccount
	}
	return handover, nil
}

// pairingPhaseKeyFor agrees with the named peer key and derives the grant
// phase key. The peer key is passed separately from the offer because the two
// halves look at different keys: the machine agrees with the viewer's
// ephemeral key from the offer, the viewer with the machine's from the wrapper.
func pairingPhaseKeyFor(offer PairingOffer, privateRaw []byte, peerPublicBase64 string) ([]byte, error) {
	peer, err := decodeFixedBase64(peerPublicBase64, PairingEphemeralBytes)
	if err != nil {
		return nil, err
	}
	shared, err := PairingSharedSecret(privateRaw, peer)
	if err != nil {
		return nil, err
	}
	pairingNonce, err := decodeFixedBase64(offer.PairingNonce, PairingNonceBytes)
	if err != nil {
		return nil, err
	}
	claimNonce, err := decodeFixedBase64(offer.ClaimNonce, PairingNonceBytes)
	if err != nil {
		return nil, err
	}
	return DerivePairingPhaseKey(shared, pairingNonce, offer.PairingID, claimNonce, PairingPhaseGrant)
}

// MARK: base64url

// EncodeCanonicalBase64URL is the fragment alphabet: base64url without padding.
func EncodeCanonicalBase64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCanonicalBase64URL accepts exactly one spelling, for the same reason
// DecodeCanonicalBase64 does: a permissive decoder accepts several spellings of
// the same bytes and only one of them is what the other side hashed.
func DecodeCanonicalBase64URL(text string) ([]byte, error) {
	if text == "" || len(text)%4 == 1 {
		return nil, fmt.Errorf("%w: length %d", ErrPairingBase64URL, len(text))
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_':
		default:
			return nil, fmt.Errorf("%w: byte 0x%02x", ErrPairingBase64URL, c)
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPairingBase64URL, err)
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != text {
		return nil, fmt.Errorf("%w: not the canonical spelling", ErrPairingBase64URL)
	}
	return decoded, nil
}

// MARK: small shared checks

// validPairingID is 1..128 bytes of printable ASCII. These ids do not stay at
// this layer — they are exactly the values a caller reaches for when it needs a
// signing domain, where a NUL would collide with the signing input's own
// separator — so the class is refused here rather than one layer down with less
// context.
func validPairingID(text string) error {
	if len(text) < 1 || len(text) > maxPairingIDBytes {
		return fmt.Errorf("%w: %d bytes", ErrPairingID, len(text))
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] > 0x7e {
			return fmt.Errorf("%w: byte 0x%02x", ErrPairingID, text[i])
		}
	}
	return nil
}

func decodeFixedBase64(text string, want int) ([]byte, error) {
	raw, err := DecodeCanonicalBase64(text)
	if err != nil {
		return nil, err
	}
	if len(raw) != want {
		return nil, fmt.Errorf("%w: %d bytes, wanted %d", ErrKeyLength, len(raw), want)
	}
	return raw, nil
}

func exactMembers(value Value, want []string) error {
	keys, ok := value.Keys()
	if !ok || len(keys) != len(want) {
		return ErrPairingFields
	}
	for i, key := range keys {
		// Keys() answers in canonical (UTF-16 code unit) order and `want` is
		// written in that order, so this is one pass rather than a set.
		if key != want[i] {
			return ErrPairingFields
		}
	}
	return nil
}

func requireVersionAndType(value Value, want string) error {
	if version, ok := memberInt(value, "v"); !ok || version != 1 {
		return ErrPairingVersion
	}
	if kind := memberString(value, "type"); kind != want {
		return ErrPairingType
	}
	return nil
}

func memberString(value Value, key string) string {
	member, ok := value.Member(key)
	if !ok {
		return ""
	}
	text, _ := member.Str()
	return text
}

func memberInt(value Value, key string) (int64, bool) {
	member, ok := value.Member(key)
	if !ok {
		return 0, false
	}
	return member.Int()
}

func allZero(data []byte) bool {
	var acc byte
	for _, b := range data {
		acc |= b
	}
	return acc == 0
}
