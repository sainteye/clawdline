package botnetwork

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	cloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type identityBoundaryHost struct{ effects int }

func (*identityBoundaryHost) Check(context.Context, wire.BotEndpoint, bool) error { return nil }
func (*identityBoundaryHost) Directory(context.Context, wire.BotEndpoint) ([]app.Entry, error) {
	return nil, nil
}
func (h *identityBoundaryHost) Submit(context.Context, wire.BotHeader, string) (string, error) {
	h.effects++
	return "should-not-run", nil
}
func (*identityBoundaryHost) Result(_ context.Context, r app.Receipt) (app.Receipt, error) {
	return r, nil
}

func TestBotGrantCannotMoveToRecreatedConversation(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "grant.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	store, err := NewStore(ctx, db, "machine-b")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x11}, 32))
	b, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x22}, 32))
	sa := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, 32))
	sb := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))
	enc := base64.StdEncoding.EncodeToString
	pair := wire.PairTranscript{PairID: "identity-pair", SourceMachineID: "machine-a", TargetMachineID: "machine-b", SourcePublicKey: enc(sa.Public().(ed25519.PublicKey)), TargetPublicKey: enc(sb.Public().(ed25519.PublicKey)), SourceFingerprint: cloud.Fingerprint(sa.Public().(ed25519.PublicKey)), TargetFingerprint: cloud.Fingerprint(sb.Public().(ed25519.PublicKey)), SourceEncryptionKey: enc(a.PublicKey().Bytes()), TargetEncryptionKey: enc(b.PublicKey().Bytes())}
	pair.SourceOfferSignature = wire.SignOffer(pair, sa)
	pair.TargetAcceptSignature = wire.SignAcceptance(pair, sb)
	grant := app.Grant{ID: "old-bot-consent", AccountID: "synthetic-account", Epoch: 1, Source: wire.BotEndpoint{AccountID: "synthetic-account", MachineID: "machine-a", ProjectID: "project-a", BotID: "bot-a", ConversationID: "old-conversation-a", Generation: 1}, Target: wire.BotEndpoint{AccountID: "synthetic-account", MachineID: "machine-b", ProjectID: "project-b", BotID: "bot-b", ConversationID: "old-conversation-b", Generation: 0}, Pair: pair, ComparedFingerprint: pair.SourceFingerprint, ExpiresAt: time.Now().Add(time.Hour)}
	if err = store.PutGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{"source", "target"} {
		t.Run("management-"+side, func(t *testing.T) {
			baseline := grant
			baseline.ID = "management-" + side
			if err := store.PutGrant(ctx, baseline); err != nil {
				t.Fatal(err)
			}
			moved := baseline
			moved.Epoch++
			if side == "source" {
				moved.Source.ConversationID = "recreated-conversation-a"
			} else {
				moved.Target.ConversationID = "recreated-conversation-b"
			}
			if err := store.PutGrant(ctx, moved); pa.Code(err) != pa.StaleVersion {
				t.Fatalf("old grant moved to recreated %s conversation: %v", side, err)
			}
		})
	}
	host := &identityBoundaryHost{}
	service, err := app.New(app.Options{Authority: store, AccountID: grant.AccountID, MachineID: "machine-b", Private: b, Signer: sb, Repository: store, Host: host, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Handler{Service: service})
	defer server.Close()
	for _, side := range []string{"source", "target"} {
		t.Run("receiving-"+side, func(t *testing.T) {
			currentGrant, e := store.Grant(ctx, grant.ID)
			if e != nil {
				t.Fatal(e)
			}
			body := []byte(`{"text":"synthetic request"}`)
			header := wire.BotHeader{Version: wire.BotWireVersion, RequestID: "recreated-" + side, Kind: "message", Source: grant.Source, Target: grant.Target, PairID: pair.PairID, AuthorityID: grant.ID, AuthorityEpoch: currentGrant.Epoch, BodyDigest: app.Digest(body)}
			if side == "source" {
				header.Source.ConversationID = "recreated-conversation-a"
			} else {
				header.Target.ConversationID = "recreated-conversation-b"
			}
			envelope, e := wire.SealBot(header, body, wire.PairKeys{PairID: pair.PairID, SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID, SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey}, a, sa)
			if e != nil {
				t.Fatal(e)
			}
			encoded, _ := json.Marshal(envelope)
			response, e := server.Client().Post(server.URL+"/v1/bot-network/exchange", "application/json", bytes.NewReader(encoded))
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("recreated %s inherited old consent: HTTP %d", side, response.StatusCode)
			}
		})
	}
	if host.effects != 0 {
		t.Fatalf("recreated identities executed %d effects", host.effects)
	}
}
