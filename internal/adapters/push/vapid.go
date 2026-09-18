package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// VAPIDKey is this daemon's application-server identity: one P-256 signing key
// that every subscription on this machine was created against.
type VAPIDKey struct{ key *ecdsa.PrivateKey }

// NewVAPIDKey mints one.
func NewVAPIDKey() (VAPIDKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return VAPIDKey{}, err
	}
	return VAPIDKey{key: key}, nil
}

// VAPIDKeyFromSeed reads the 32-octet private scalar back.
//
// A scalar is checked against the curve order rather than only its length:
// zero and anything at or past the order are not private keys, and a key made
// from one would sign things no push service verifies.
func VAPIDKeyFromSeed(seed []byte) (VAPIDKey, error) {
	if len(seed) != VAPIDSeedBytes {
		return VAPIDKey{}, fmt.Errorf("a VAPID scalar is %d octets, this is %d", VAPIDSeedBytes, len(seed))
	}
	d := new(big.Int).SetBytes(seed)
	order := elliptic.P256().Params().N
	if d.Sign() == 0 || d.Cmp(order) >= 0 {
		return VAPIDKey{}, fmt.Errorf("that scalar is not on P-256")
	}
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}, D: d}
	key.PublicKey.X, key.PublicKey.Y = elliptic.P256().ScalarBaseMult(seed)
	return VAPIDKey{key: key}, nil
}

// Valid is whether this holds a key at all.
func (v VAPIDKey) Valid() bool { return v.key != nil }

// Seed is the 32-octet private scalar, which is what the store keeps.
func (v VAPIDKey) Seed() []byte {
	if v.key == nil {
		return nil
	}
	return v.key.D.FillBytes(make([]byte, VAPIDSeedBytes))
}

// PublicBytes is the uncompressed public point, 65 octets starting 0x04.
func (v VAPIDKey) PublicBytes() []byte {
	if v.key == nil {
		return nil
	}
	// Via crypto/ecdh rather than the deprecated elliptic.Marshal, which is
	// the same 65 octets and the encoding that validates the point.
	pub, err := v.key.PublicKey.ECDH()
	if err != nil {
		return nil
	}
	return pub.Bytes()
}

// PublicKey is what the page passes to `subscribe({ applicationServerKey })` —
// base64url of the uncompressed public point.
func (v VAPIDKey) PublicKey() string { return EncodeBase64URL(v.PublicBytes()) }

// Authorization is the `Authorization` header value: RFC 8292 VAPID, which is
// an ES256 JWT and a copy of the public key that verifies it.
//
// `expires` is not clamped here, because clamping would need a clock and this
// has none on purpose. RFC 8292 §2 says `exp` MUST NOT be more than 24 hours
// out; TokenLifetime is the value every caller in this package uses and it is
// half of that.
func (v VAPIDKey) Authorization(endpoint string, expires time.Time, subject string) (string, error) {
	if !v.Valid() {
		return "", fmt.Errorf("no VAPID key")
	}
	// Checked rather than trusted: a push service answers a malformed `sub`
	// with a 403 and a three-word body, and this is the one thing in the token
	// a person is likely to have typed.
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		return "", fmt.Errorf("%w, and this is %s", ErrBadSubject, subject)
	}
	// `aud` is the **origin** of the endpoint and not the endpoint — RFC 8292
	// §2 is explicit, and putting the full path there is the classic way to
	// get a token that verifies perfectly and is rejected anyway.
	audience := OriginOf(endpoint)
	if audience == "" {
		return "", fmt.Errorf("%w — %s", ErrNoOrigin, endpoint)
	}

	header, err := segment(map[string]any{"typ": "JWT", "alg": "ES256"})
	if err != nil {
		return "", err
	}
	claims, err := segment(map[string]any{
		"aud": audience,
		"exp": expires.Unix(),
		"sub": subject,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	// r‖s, 64 octets, which is exactly the JWS form for ES256 — the DER
	// encoding that most ECDSA APIs hand back would be rejected.
	r, s, err := ecdsa.Sign(rand.Reader, v.key, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return "vapid t=" + signingInput + "." + EncodeBase64URL(signature) +
		", k=" + EncodeBase64URL(v.PublicBytes()), nil
}

// segment is one base64url JWT segment.
//
// Sorted keys so the same claims always produce the same bytes — Go's encoder
// sorts a map's keys already — and no escaped slashes, because `aud` is a URL
// and `https:\/\/…` is legal JSON that reads like a mistake.
func segment(object map[string]any) (string, error) {
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(object); err != nil {
		return "", err
	}
	return EncodeBase64URL([]byte(strings.TrimRight(out.String(), "\n"))), nil
}
