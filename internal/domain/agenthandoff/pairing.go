package agenthandoff

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

var ErrPairTranscript = errors.New("peer_pair_transcript_invalid")

// PairTranscript is what both machines pin after comparing the opposite
// signing-key fingerprint out of band. The Cloud API is only a carrier.
type PairTranscript struct {
	PairID                string
	SourceMachineID       string
	TargetMachineID       string
	SourcePublicKey       string
	TargetPublicKey       string
	SourceFingerprint     string
	TargetFingerprint     string
	SourceEncryptionKey   string
	TargetEncryptionKey   string
	SourceOfferSignature  string
	TargetAcceptSignature string
}

func OfferBytes(pair PairTranscript) []byte {
	return []byte("clawdline-peer-pair-offer-v1\n" + pair.SourceMachineID + "\n" +
		pair.TargetMachineID + "\n" + pair.SourceFingerprint + "\n" +
		pair.TargetFingerprint + "\n" + pair.SourceEncryptionKey)
}

func AcceptBytes(pair PairTranscript) []byte {
	return []byte("clawdline-peer-pair-accept-v1\n" + pair.PairID + "\n" +
		pair.SourceMachineID + "\n" + pair.TargetMachineID + "\n" +
		pair.SourceEncryptionKey + "\n" + pair.TargetEncryptionKey + "\n" +
		pair.SourceOfferSignature)
}

func VerifyTranscript(pair PairTranscript, localMachineID, comparedPeerFingerprint string) error {
	if !validSegment(pair.PairID) || !validSegment(pair.SourceMachineID) ||
		!validSegment(pair.TargetMachineID) || pair.SourceMachineID == pair.TargetMachineID {
		return ErrPairTranscript
	}
	peerFingerprint := pair.SourceFingerprint
	if localMachineID == pair.SourceMachineID {
		peerFingerprint = pair.TargetFingerprint
	} else if localMachineID != pair.TargetMachineID {
		return ErrPairTranscript
	}
	if comparedPeerFingerprint == "" || comparedPeerFingerprint != peerFingerprint {
		return ErrPairTranscript
	}
	sourceKey, err := decodeCanonical(pair.SourcePublicKey, ed25519.PublicKeySize)
	if err != nil || cloud.Fingerprint(ed25519.PublicKey(sourceKey)) != pair.SourceFingerprint {
		return ErrPairTranscript
	}
	targetKey, err := decodeCanonical(pair.TargetPublicKey, ed25519.PublicKeySize)
	if err != nil || cloud.Fingerprint(ed25519.PublicKey(targetKey)) != pair.TargetFingerprint {
		return ErrPairTranscript
	}
	if _, err := decodeCanonical(pair.SourceEncryptionKey, 32); err != nil {
		return ErrPairTranscript
	}
	if _, err := decodeCanonical(pair.TargetEncryptionKey, 32); err != nil {
		return ErrPairTranscript
	}
	sourceSignature, err := decodeCanonical(pair.SourceOfferSignature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(sourceKey), OfferBytes(pair), sourceSignature) {
		return ErrPairTranscript
	}
	targetSignature, err := decodeCanonical(pair.TargetAcceptSignature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(targetKey), AcceptBytes(pair), targetSignature) {
		return ErrPairTranscript
	}
	return nil
}

// SignOffer and SignAcceptance are used by the local, person-confirmed CLI
// or Console action. They never send a request themselves.
func SignOffer(pair PairTranscript, key ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, OfferBytes(pair)))
}

func SignAcceptance(pair PairTranscript, key ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, AcceptBytes(pair)))
}
