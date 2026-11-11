package cloud

// CPace over ristretto255 with SHA-512, as draft-irtf-cfrg-cpace-21 defines
// it (Sections 7, 8.3 and Appendix A). It is the balanced PAKE behind
// browser invitations: the short secret half of an invitation code is the
// password, and Cloud, which relays both messages, never learns it. A relay
// that substitutes either message gets one online guess at that password and
// then the invitation is spent.
//
// Every function here is checked against the draft's own vectors in
// testdata/cpace-ristretto255-v1.json, and the browser implementation is
// checked against the same file.

import (
	"crypto/sha512"
	"errors"
	"io"

	"github.com/gtank/ristretto255"
)

// CPaceDSI is the ristretto255 suite's domain separation identifier.
const CPaceDSI = "CPaceRistretto255"

// cpaceHashBlockBytes is SHA-512's input block size, H.s_in_bytes.
const cpaceHashBlockBytes = 128

// CPaceElementBytes is the length of an encoded ristretto255 element.
const CPaceElementBytes = 32

// CPace refusals.
var (
	ErrCPaceInvalidPeer = errors.New("cpace: the peer's element is not a valid non-identity ristretto255 encoding")
	ErrCPaceScalar      = errors.New("cpace: the scalar is not a canonical ristretto255 scalar")
	ErrCPaceRandom      = errors.New("cpace: could not read randomness for the scalar")
)

// CPacePrependLen is prepend_len: the LEB128 length of data, then data.
func CPacePrependLen(data []byte) []byte {
	n := len(data)
	out := make([]byte, 0, n+3)
	for {
		if n < 128 {
			out = append(out, byte(n))
		} else {
			out = append(out, byte(n&0x7f)|0x80)
		}
		n >>= 7
		if n == 0 {
			break
		}
	}
	return append(out, data...)
}

// CPaceLVCat is lv_cat: each argument with its length prepended.
func CPaceLVCat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, CPacePrependLen(part)...)
	}
	return out
}

// CPaceGeneratorString is generator_string(DSI, PRS, CI, sid, 128).
func CPaceGeneratorString(prs, ci, sid []byte) []byte {
	dsi := []byte(CPaceDSI)
	zpad := cpaceHashBlockBytes - 1 - len(CPacePrependLen(prs)) - len(CPacePrependLen(dsi))
	if zpad < 0 {
		zpad = 0
	}
	return CPaceLVCat(dsi, prs, make([]byte, zpad), ci, sid)
}

// cpaceGenerator is calculate_generator: SHA-512 of the generator string,
// mapped with RFC 9496 element derivation.
func cpaceGenerator(prs, ci, sid []byte) *ristretto255.Element {
	sum := sha512.Sum512(CPaceGeneratorString(prs, ci, sid))
	g, err := ristretto255.NewElement().SetUniformBytes(sum[:])
	if err != nil {
		// SetUniformBytes refuses only a length other than 64.
		panic(err)
	}
	return g
}

// CPaceGenerator is the encoded generator, exposed for the vectors.
func CPaceGenerator(prs, ci, sid []byte) []byte {
	return cpaceGenerator(prs, ci, sid).Bytes()
}

// CPaceParty is one side of one CPace run. It is used once: Finish forgets
// the scalar.
type CPaceParty struct {
	y         *ristretto255.Scalar
	sid       []byte
	ownY      []byte
	ownAD     []byte
	initiator bool
}

// NewCPaceParty samples a scalar as Section 8.3 recommends (32 random bytes
// with the bits above 252 cleared) and computes this side's message.
func NewCPaceParty(random io.Reader, prs, ci, sid, ad []byte, initiator bool) (*CPaceParty, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(random, raw); err != nil {
		return nil, ErrCPaceRandom
	}
	raw[31] &= 0x0f
	return NewCPacePartyWithScalar(raw, prs, ci, sid, ad, initiator)
}

// NewCPacePartyWithScalar is NewCPaceParty with a given little-endian
// scalar. Only the vectors and tests have a reason to choose one.
func NewCPacePartyWithScalar(scalar, prs, ci, sid, ad []byte, initiator bool) (*CPaceParty, error) {
	y, err := ristretto255.NewScalar().SetCanonicalBytes(scalar)
	if err != nil || y.Equal(ristretto255.NewScalar()) == 1 {
		return nil, ErrCPaceScalar
	}
	own := ristretto255.NewElement().ScalarMult(y, cpaceGenerator(prs, ci, sid)).Bytes()
	return &CPaceParty{y: y, sid: append([]byte(nil), sid...), ownY: own,
		ownAD: append([]byte(nil), ad...), initiator: initiator}, nil
}

// Message is this side's element, sent with its associated data.
func (p *CPaceParty) Message() []byte { return append([]byte(nil), p.ownY...) }

// CPaceScalarMultVfy is scalar_mult_vfy: nil when X does not decode or the
// product is the identity, which is how the draft's G.I return reads here.
func CPaceScalarMultVfy(scalar *ristretto255.Scalar, x []byte) []byte {
	if len(x) != CPaceElementBytes {
		return nil
	}
	point, err := ristretto255.NewElement().SetCanonicalBytes(x)
	if err != nil {
		return nil
	}
	k := ristretto255.NewElement().ScalarMult(scalar, point)
	if k.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil
	}
	return k.Bytes()
}

// CPaceTranscriptIR is transcript_ir(Ya, ADa, Yb, ADb).
func CPaceTranscriptIR(ya, ada, yb, adb []byte) []byte {
	return append(CPaceLVCat(ya, ada), CPaceLVCat(yb, adb)...)
}

// CPaceISK is ISK = SHA-512(lv_cat(DSI || "_ISK", sid, K) || transcript).
func CPaceISK(sid, k, transcript []byte) []byte {
	pre := CPaceLVCat([]byte(CPaceDSI+"_ISK"), sid, k)
	sum := sha512.Sum512(append(pre, transcript...))
	return sum[:]
}

// Finish takes the peer's message and returns the 64-byte ISK. It aborts on
// an invalid or identity peer element, as the draft requires.
func (p *CPaceParty) Finish(peerY, peerAD []byte) ([]byte, error) {
	if p == nil || p.y == nil {
		return nil, ErrCPaceScalar
	}
	k := CPaceScalarMultVfy(p.y, peerY)
	p.y = nil
	if k == nil {
		return nil, ErrCPaceInvalidPeer
	}
	var transcript []byte
	if p.initiator {
		transcript = CPaceTranscriptIR(p.ownY, p.ownAD, peerY, peerAD)
	} else {
		transcript = CPaceTranscriptIR(peerY, peerAD, p.ownY, p.ownAD)
	}
	return CPaceISK(p.sid, k, transcript), nil
}
