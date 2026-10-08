package agenthandoff

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPairTranscriptNeedsBothMachineSignaturesAndComparedFingerprint(t *testing.T) {
	source := ed25519.NewKeyFromSeed(bytesOf(1))
	target := ed25519.NewKeyFromSeed(bytesOf(2))
	sourcePrivate, _ := ecdh.X25519().NewPrivateKey(bytesOf(3))
	targetPrivate, _ := ecdh.X25519().NewPrivateKey(bytesOf(4))
	transcript := PairTranscript{PairID: "pair-1", SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourcePublicKey:     base64.StdEncoding.EncodeToString(source.Public().(ed25519.PublicKey)),
		TargetPublicKey:     base64.StdEncoding.EncodeToString(target.Public().(ed25519.PublicKey)),
		SourceFingerprint:   cloud.Fingerprint(source.Public().(ed25519.PublicKey)),
		TargetFingerprint:   cloud.Fingerprint(target.Public().(ed25519.PublicKey)),
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(sourcePrivate.PublicKey().Bytes()),
		TargetEncryptionKey: base64.StdEncoding.EncodeToString(targetPrivate.PublicKey().Bytes())}
	transcript.SourceOfferSignature = SignOffer(transcript, source)
	transcript.TargetAcceptSignature = SignAcceptance(transcript, target)
	if err := VerifyTranscript(transcript, "machine-b", transcript.SourceFingerprint); err != nil {
		t.Fatalf("signed pair refused: %v", err)
	}
	if err := VerifyTranscript(transcript, "machine-b", transcript.TargetFingerprint); err == nil {
		t.Fatal("a target-provided fingerprint replaced the out-of-band source comparison")
	}
	changed := transcript
	changed.TargetEncryptionKey = base64.StdEncoding.EncodeToString(bytesOf(5))
	if err := VerifyTranscript(changed, "machine-b", transcript.SourceFingerprint); err == nil {
		t.Fatal("changed target encryption key survived the acceptance signature")
	}
}

func TestSealedPeerMessageRejectsKeySubstitutionAndChangedBody(t *testing.T) {
	source := ed25519.NewKeyFromSeed(bytesOf(1))
	other := ed25519.NewKeyFromSeed(bytesOf(9))
	sourcePrivate, _ := ecdh.X25519().NewPrivateKey(bytesOf(3))
	targetPrivate, _ := ecdh.X25519().NewPrivateKey(bytesOf(4))
	keys := PairKeys{PairID: "pair-1", SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(sourcePrivate.PublicKey().Bytes()),
		TargetEncryptionKey: base64.StdEncoding.EncodeToString(targetPrivate.PublicKey().Bytes())}
	body := []byte("handoff work")
	sum := sha256.Sum256(body)
	request := Request{RequestID: "request-1", Kind: Handoff,
		Source:  Endpoint{"machine-a", "session-a", "11111111111111111111111111111111"},
		Target:  Endpoint{"machine-b", "session-b", "22222222222222222222222222222222"},
		GrantID: "grant-1", BodyDigest: hex.EncodeToString(sum[:])}
	envelope, err := Seal(request, body, keys, sourcePrivate, source)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(envelope, keys, targetPrivate, source.Public().(ed25519.PublicKey))
	if err != nil || string(opened) != string(body) {
		t.Fatalf("opened = %q, %v", opened, err)
	}
	if _, err := Open(envelope, keys, targetPrivate, other.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("a relay-supplied replacement source key authenticated the envelope")
	}
	envelope.Request.BodyDigest = hex.EncodeToString(bytesOf(6))
	if _, err := Open(envelope, keys, targetPrivate, source.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("the signed body digest changed without refusal")
	}
}

func bytesOf(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}
