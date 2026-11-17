package agenthandoff

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func botWireFixture(t *testing.T) (BotHeader, []byte, PairKeys, *ecdh.PrivateKey, *ecdh.PrivateKey, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	a, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"text":"synthetic peer work"}`)
	sum := sha256.Sum256(body)
	h := BotHeader{Version: BotWireVersion, RequestID: "request-1", Kind: "message",
		Source: BotEndpoint{AccountID: "account-a", MachineID: "machine-a", ProjectID: "project-a", BotID: "bot-a", ConversationID: "conversation-a", Generation: 3},
		Target: BotEndpoint{AccountID: "account-a", MachineID: "machine-b", ProjectID: "project-b", BotID: "bot-b", ConversationID: "conversation-b", Generation: 0},
		PairID: "pair-ab", AuthorityID: "bot-consent-ab", AuthorityEpoch: 1, BodyDigest: hex.EncodeToString(sum[:])}
	pair := PairKeys{PairID: h.PairID, SourceMachineID: h.Source.MachineID, TargetMachineID: h.Target.MachineID,
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(a.PublicKey().Bytes()), TargetEncryptionKey: base64.StdEncoding.EncodeToString(b.PublicKey().Bytes())}
	return h, body, pair, a, b, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32)), ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))
}

func TestBotWireUsesDedicatedDomainAndBothPairDirections(t *testing.T) {
	h, body, pair, a, b, signA, signB := botWireFixture(t)
	e, err := SealBot(h, body, pair, a, signA)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenBot(e, pair, b, signA.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err = requestFrame.open(h, e.frame(), pair, b, false, signA.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Session domain opened a Bot envelope")
	}
	if _, err = discoveryFrame.open(h, e.frame(), pair, b, false, signA.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Session discovery domain opened a Bot envelope")
	}
	h.Source, h.Target = h.Target, h.Source
	h.Kind, h.RequestID, h.CorrelationID = "reply", "reply-1", "request-1"
	e, err = SealBot(h, body, pair, b, signB)
	if err != nil {
		t.Fatal(err)
	}
	got, err = OpenBot(e, pair, a, signB.Public().(ed25519.PublicKey))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("reverse reply: %q %v", got, err)
	}
	if _, err = OpenBot(e, pair, b, signB.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("sender private key opened its own recipient rail")
	}
}

func TestBotWireBindsEveryRoutingAndAuthorityField(t *testing.T) {
	h, body, pair, a, b, signA, signB := botWireFixture(t)
	e, err := SealBot(h, body, pair, a, signA)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*BotEnvelope){
		"version":             func(e *BotEnvelope) { e.Header.Version++ },
		"request":             func(e *BotEnvelope) { e.Header.RequestID = "another-request" },
		"correlation":         func(e *BotEnvelope) { e.Header.CorrelationID = "another-request" },
		"kind":                func(e *BotEnvelope) { e.Header.Kind = "directory" },
		"source-account":      func(e *BotEnvelope) { e.Header.Source.AccountID = "account-b" },
		"target-account":      func(e *BotEnvelope) { e.Header.Target.AccountID = "account-b" },
		"source-machine":      func(e *BotEnvelope) { e.Header.Source.MachineID = "machine-c" },
		"target-machine":      func(e *BotEnvelope) { e.Header.Target.MachineID = "machine-c" },
		"source-project":      func(e *BotEnvelope) { e.Header.Source.ProjectID = "project-c" },
		"target-project":      func(e *BotEnvelope) { e.Header.Target.ProjectID = "project-c" },
		"source-bot":          func(e *BotEnvelope) { e.Header.Source.BotID = "bot-c" },
		"target-bot":          func(e *BotEnvelope) { e.Header.Target.BotID = "bot-c" },
		"source-conversation": func(e *BotEnvelope) { e.Header.Source.ConversationID = "conversation-c" },
		"target-conversation": func(e *BotEnvelope) { e.Header.Target.ConversationID = "conversation-c" },
		"source-generation":   func(e *BotEnvelope) { e.Header.Source.Generation++ },
		"target-generation":   func(e *BotEnvelope) { e.Header.Target.Generation++ },
		"pair":                func(e *BotEnvelope) { e.Header.PairID = "pair-ac" },
		"authority":           func(e *BotEnvelope) { e.Header.AuthorityID = "other-consent" },
		"epoch":               func(e *BotEnvelope) { e.Header.AuthorityEpoch++ },
		"body":                func(e *BotEnvelope) { e.Header.BodyDigest = "invalid" },
		"nonce":               func(e *BotEnvelope) { e.Nonce = "AAECAwQFBgcICQoL" },
		"key":                 func(e *BotEnvelope) { e.KeyID = "other-key" },
		"ciphertext":          func(e *BotEnvelope) { e.Ciphertext = "AAAA" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := e
			mutate(&changed)
			if _, err := OpenBot(changed, pair, b, signA.Public().(ed25519.PublicKey)); err != BotInvalidWire {
				t.Fatalf("changed wire accepted: %v", err)
			}
		})
	}
	if _, err := OpenBot(e, pair, b, signB.Public().(ed25519.PublicKey)); err != BotInvalidWire {
		t.Fatalf("wrong pinned signer: %v", err)
	}
	if _, err := SealBot(h, []byte("changed"), pair, a, signA); err != BotInvalidWire {
		t.Fatalf("changed plaintext sealed: %v", err)
	}
}

func TestBotWireReceiptLaneSurvivesCapturedAuthorityChanges(t *testing.T) {
	h, body, _, _, _, _, _ := botWireFixture(t)
	actor, id, before := BotReceiptIdentity(h)
	h.Source.Generation++
	h.Target.Generation++
	h.AuthorityEpoch++
	afterActor, afterID, after := BotReceiptIdentity(h)
	if actor != afterActor || id != afterID || before == after {
		t.Fatal("changed captures escaped the original receipt lane")
	}
	if err := ValidateBot(h, body); err != nil {
		t.Fatal(err)
	}
	h.Target.BotID = "new-bot"
	afterActor, _, _ = BotReceiptIdentity(h)
	if actor == afterActor {
		t.Fatal("different stored Bot reused another Bot's receipt actor")
	}
}
