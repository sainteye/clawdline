package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestPeerEncryptionMatchesPrivateRailVector(t *testing.T) {
	pair := PairKeys{
		PairID:          "11111111-1111-4111-8111-111111111111",
		SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourceEncryptionKey: "e06Qm75//kTEZaIgA31gjuNYl9Me+XLwf3SJLLD3PxM=",
		TargetEncryptionKey: "D6poTtKIZ7l/Smot7l34zpdOdrcBjj8iocTPJnhXDyA=",
	}
	sourcePrivate, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	targetPrivate, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, sourceID, err := PairKey(pair, sourcePrivate, true)
	if err != nil {
		t.Fatal(err)
	}
	targetKey, targetID, err := PairKey(pair, targetPrivate, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sourceKey); got != "3803c2287cbad7409a120f50d43449b51efcea5f1a1bed8d7a55829b0255027d" {
		t.Fatalf("pair key = %s", got)
	}
	if !bytes.Equal(sourceKey, targetKey) || sourceID != targetID ||
		targetID != "peer-799207c9a5943290af8fddcf8e6d7e32" {
		t.Fatalf("source and target derived different pair keys or ids: %q %q", sourceID, targetID)
	}
	request, _, _ := validRequestAndFacts()
	envelope := Envelope{
		Request: request, KeyID: targetID, Nonce: "AAECAwQFBgcICQoL",
		Ciphertext: "fLNO5h6c86ygSnst6rL5eGihEoJT",
	}
	signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32))
	input, err := SignatureInput(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Signature = base64Signature(signer, input)
	plain, err := Open(envelope, pair, targetPrivate, signer.Public().(ed25519.PublicKey))
	if err != nil || string(plain) != "hello" {
		t.Fatalf("vector open = %q, %v", plain, err)
	}
	if _, err := Open(envelope, pair, targetPrivate, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32)).Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("unpaired signing key opened the envelope")
	}
	envelope.Request.Target.ExecutionGeneration = "33333333333333333333333333333333"
	if _, err := Open(envelope, pair, targetPrivate, signer.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("changed target generation kept the original signature")
	}
}

func base64Signature(private ed25519.PrivateKey, body []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))
}
