package agenthandoff

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/sainteye/clawdline/internal/domain/cloud"
	"golang.org/x/crypto/hkdf"
)

var ErrPeerEnvelope = errors.New("peer_envelope_invalid")

// PairKeys come only from a locally pinned, signed pairing transcript.
// An API response or a relay hint cannot directly construct trusted PairKeys.
type PairKeys struct {
	PairID              string
	SourceMachineID     string
	TargetMachineID     string
	SourceEncryptionKey string
	TargetEncryptionKey string
}

// Envelope is the private peer rail's peer_envelope body. SourcePublicKey is
// deliberately absent: it is an untrusted lookup hint, not verification data.
type Envelope struct {
	Request    Request `json:"request"`
	Ciphertext string  `json:"ciphertext"`
	Nonce      string  `json:"nonce"`
	KeyID      string  `json:"key_id"`
	Signature  string  `json:"signature"`
}

func canonicalRequest(request Request) ([]byte, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	parsed, err := cloud.Parse(encoded)
	if err != nil {
		return nil, err
	}
	return cloud.CanonicalBytes(parsed), nil
}

// PairKey derives the exact pair key and key ID from a pinned transcript.
// The caller retains its own X25519 private key in owner-only storage.
func PairKey(pair PairKeys, localPrivate *ecdh.PrivateKey, sourceSide bool) ([]byte, string, error) {
	if pair.PairID == "" || pair.SourceMachineID == "" || pair.TargetMachineID == "" || localPrivate == nil {
		return nil, "", ErrPeerEnvelope
	}
	sourcePublic, err := decodeCanonical(pair.SourceEncryptionKey, 32)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	targetPublic, err := decodeCanonical(pair.TargetEncryptionKey, 32)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	local, remote := targetPublic, sourcePublic
	if sourceSide {
		local, remote = sourcePublic, targetPublic
	}
	if !bytes.Equal(localPrivate.PublicKey().Bytes(), local) {
		return nil, "", ErrPeerEnvelope
	}
	peerPublic, err := ecdh.X25519().NewPublicKey(remote)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	shared, err := localPrivate.ECDH(peerPublic)
	if err != nil {
		return nil, "", ErrPeerEnvelope
	}
	salt := sha256.Sum256([]byte("clawdline-peer-kdf-v1\n" + pair.PairID + "\n" +
		pair.SourceMachineID + "\n" + pair.TargetMachineID))
	info := []byte("clawdline-peer-aes-gcm-v1\n" + pair.SourceEncryptionKey + "\n" + pair.TargetEncryptionKey)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt[:], info), key); err != nil {
		return nil, "", ErrPeerEnvelope
	}
	idInput := "clawdline-peer-key-id-v1\n" + pair.PairID + "\n" +
		pair.SourceEncryptionKey + "\n" + pair.TargetEncryptionKey
	idSum := sha256.Sum256([]byte(idInput))
	return key, "peer-" + hex.EncodeToString(idSum[:])[:32], nil
}

// SignatureInput is the peer rail's Ed25519 input, separate from the older
// Cloud envelope signature format.
func SignatureInput(envelope Envelope) ([]byte, error) {
	request, err := canonicalRequest(envelope.Request)
	if err != nil || envelope.Nonce == "" || envelope.KeyID == "" || envelope.Ciphertext == "" {
		return nil, ErrPeerEnvelope
	}
	input := make([]byte, 0, len(request)+len(envelope.Nonce)+len(envelope.KeyID)+len(envelope.Ciphertext)+32)
	input = append(input, "clawdline-peer-v1\n"...)
	input = append(input, request...)
	input = append(input, '\n')
	input = append(input, envelope.Nonce...)
	input = append(input, '\n')
	input = append(input, envelope.KeyID...)
	input = append(input, '\n')
	input = append(input, envelope.Ciphertext...)
	return input, nil
}

// Open verifies the pinned peer signature, derived key ID and GCM tag before
// returning plaintext. Admission and receipt claims follow this operation.
func Open(envelope Envelope, pair PairKeys, localPrivate *ecdh.PrivateKey, pinnedSigner ed25519.PublicKey) ([]byte, error) {
	if len(pinnedSigner) != ed25519.PublicKeySize ||
		envelope.Request.Source.MachineID != pair.SourceMachineID ||
		envelope.Request.Target.MachineID != pair.TargetMachineID {
		return nil, ErrPeerEnvelope
	}
	input, err := SignatureInput(envelope)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	sig, err := decodeCanonical(envelope.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(pinnedSigner, input, sig) {
		return nil, ErrPeerEnvelope
	}
	key, expectedID, err := PairKey(pair, localPrivate, false)
	if err != nil || envelope.KeyID != expectedID {
		return nil, ErrPeerEnvelope
	}
	nonce, err := decodeCanonical(envelope.Nonce, 12)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	ciphertext, err := decodeCanonical(envelope.Ciphertext, -1)
	if err != nil || len(ciphertext) < 16 {
		return nil, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	request, err := canonicalRequest(envelope.Request)
	if err != nil {
		return nil, ErrPeerEnvelope
	}
	aad := make([]byte, 0, len(request)+len(envelope.KeyID)+32)
	aad = append(aad, "clawdline-peer-aad-v1\n"...)
	aad = append(aad, request...)
	aad = append(aad, '\n')
	aad = append(aad, envelope.KeyID...)
	plain, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil || len(plain) == 0 {
		return nil, ErrPeerEnvelope
	}
	sum := sha256.Sum256(plain)
	if envelope.Request.BodyDigest != hex.EncodeToString(sum[:]) {
		return nil, ErrPeerEnvelope
	}
	return plain, nil
}

// Seal encrypts exactly one request with a fresh nonce and signs its routing
// header and ciphertext as the source machine. The caller must first verify
// its local grant and source execution; Cloud performs a separate fresh check.
func Seal(request Request, body []byte, pair PairKeys, localPrivate *ecdh.PrivateKey,
	signer ed25519.PrivateKey) (Envelope, error) {
	if len(body) == 0 || len(signer) != ed25519.PrivateKeySize ||
		request.Source.MachineID != pair.SourceMachineID ||
		request.Target.MachineID != pair.TargetMachineID {
		return Envelope{}, ErrPeerEnvelope
	}
	sum := sha256.Sum256(body)
	if request.BodyDigest != hex.EncodeToString(sum[:]) {
		return Envelope{}, ErrPeerEnvelope
	}
	key, keyID, err := PairKey(pair, localPrivate, true)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	canonical, err := canonicalRequest(request)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	aad := make([]byte, 0, len(canonical)+len(keyID)+32)
	aad = append(aad, "clawdline-peer-aad-v1\n"...)
	aad = append(aad, canonical...)
	aad = append(aad, '\n')
	aad = append(aad, keyID...)
	envelope := Envelope{Request: request, Nonce: base64.StdEncoding.EncodeToString(nonce),
		KeyID: keyID, Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, body, aad))}
	input, err := SignatureInput(envelope)
	if err != nil {
		return Envelope{}, ErrPeerEnvelope
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return envelope, nil
}

func decodeCanonical(raw string, length int) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || (length >= 0 && len(decoded) != length) ||
		base64.StdEncoding.EncodeToString(decoded) != raw {
		return nil, ErrPeerEnvelope
	}
	return decoded, nil
}
