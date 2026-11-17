package botnetwork

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

func TestRetainedNetworkGrantCapAllowsExistingRevocation(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "grants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	store, err := NewStore(ctx, db, "machine-b")
	if err != nil {
		t.Fatal(err)
	}
	a := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	b := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32))
	x, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{3}, 32))
	y, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{4}, 32))
	pair := wire.PairTranscript{PairID: "fixture-pair", SourceMachineID: "machine-a", TargetMachineID: "machine-b", SourcePublicKey: base64.StdEncoding.EncodeToString(a.Public().(ed25519.PublicKey)), TargetPublicKey: base64.StdEncoding.EncodeToString(b.Public().(ed25519.PublicKey)), SourceFingerprint: cloud.Fingerprint(a.Public().(ed25519.PublicKey)), TargetFingerprint: cloud.Fingerprint(b.Public().(ed25519.PublicKey)), SourceEncryptionKey: base64.StdEncoding.EncodeToString(x.PublicKey().Bytes()), TargetEncryptionKey: base64.StdEncoding.EncodeToString(y.PublicKey().Bytes())}
	pair.SourceOfferSignature = wire.SignOffer(pair, a)
	pair.TargetAcceptSignature = wire.SignAcceptance(pair, b)
	grant := app.Grant{AccountID: "fixture-account", Epoch: 1, Pair: pair, ComparedFingerprint: pair.SourceFingerprint, Source: wire.BotEndpoint{AccountID: "fixture-account", MachineID: "machine-a", ProjectID: "project-a", BotID: "bot-a"}, Target: wire.BotEndpoint{AccountID: "fixture-account", MachineID: "machine-b", ProjectID: "project-b", BotID: "bot-b"}}
	for i := 0; i < pa.MaxNetworkGrants; i++ {
		grant.ID = fmt.Sprintf("grant-%d", i)
		if err := store.PutGrant(ctx, grant); err != nil {
			t.Fatalf("grant%d: %v", i, err)
		}
	}
	grant.ID = "grant-at-cap"
	if err := store.PutGrant(ctx, grant); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("new grant at cap: %v", err)
	}
	grant.ID = "grant-0"
	grant.Epoch++
	grant.Revoked = true
	if err := store.PutGrant(ctx, grant); err != nil {
		t.Fatalf("revocation at cap: %v", err)
	}
	prior, err := store.Grant(ctx, grant.ID)
	if err != nil || !prior.Revoked || prior.Epoch != 2 {
		t.Fatalf("retained revocation: %+v %v", prior, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM project_assistant_network_grants WHERE machine=?`, store.machine).Scan(&n); err != nil || n != pa.MaxNetworkGrants {
		t.Fatalf("actual retained grants%d: %v", n, err)
	}
}

func TestRetainedNetworkReceiptCapPreservesExactReplay(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "network.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	store, err := NewStore(ctx, db, "target-machine")
	if err != nil {
		t.Fatal(err)
	}
	header := wire.BotHeader{Version: wire.BotWireVersion, Kind: "message", PairID: "fixture-pair", AuthorityID: "fixture-consent", AuthorityEpoch: 1,
		Source:     wire.BotEndpoint{AccountID: "fixture-account", MachineID: "source-machine", ProjectID: "source-project", BotID: "source-bot", ConversationID: "source-chat", Generation: 1},
		Target:     wire.BotEndpoint{AccountID: "fixture-account", MachineID: "target-machine", ProjectID: "target-project", BotID: "target-bot", ConversationID: "target-chat", Generation: 1},
		BodyDigest: "ddab29ff2c393ee52855114aa7378cc92207f35c4325003d7fd5e517e4da9cca"}
	for i := 0; i < pa.MaxNetworkReceipts; i++ {
		header.RequestID = fmt.Sprintf("request-%d", i)
		if _, claimed, err := store.Claim(ctx, header); err != nil || !claimed {
			t.Fatalf("claim %d: claimed=%v %v", i, claimed, err)
		}
	}
	if used, err := store.Usage(ctx); err != nil || used != pa.MaxNetworkReceipts {
		t.Fatalf("actual retained usage: %d %v", used, err)
	}
	header.RequestID = "request-at-cap"
	if _, claimed, err := store.Claim(ctx, header); pa.Code(err) != pa.CapacityFull || claimed {
		t.Fatalf("cap did not refuse: claimed=%v %v", claimed, err)
	}
	header.RequestID = "request-0"
	if prior, claimed, err := store.Claim(ctx, header); err != nil || claimed || prior.State != "unknown" {
		t.Fatalf("exact replay lost its unknown barrier: %+v claimed=%v %v", prior, claimed, err)
	}
	header.BodyDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, claimed, err := store.Claim(ctx, header); pa.Code(err) != pa.IdempotencyConflict || claimed {
		t.Fatalf("changed replay: claimed=%v %v", claimed, err)
	}
	if used, err := store.Usage(ctx); err != nil || used != pa.MaxNetworkReceipts {
		t.Fatalf("refusal erased receipts: %d %v", used, err)
	}
}

func TestRetainedAssistantCapsPreserveRevocationAndReplay(t *testing.T) {
	f := newAssistantFixture(t)
	ctx := context.Background()
	for i := 1; i < pa.MaxNetworkAssistantGrants; i++ {
		grant := f.grant
		grant.ID = fmt.Sprintf("consent-%d", i)
		if err := f.store.PutAssistantGrant(ctx, grant, 0); err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
	}
	grant := f.grant
	grant.ID = "consent-at-cap"
	if err := f.store.PutAssistantGrant(ctx, grant, 0); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("grant cap did not refuse: %v", err)
	}
	for i := 0; i < pa.MaxNetworkAssistantReceipts; i++ {
		call := f.call
		call.RequestID = fmt.Sprintf("receipt-%d", i)
		call = f.sign(t, call, f.body)
		principal, _, err := f.service.Verify(ctx, call, f.body)
		if err != nil {
			t.Fatal(err)
		}
		if _, fresh, err := f.store.ClaimAssistant(ctx, call, principal); err != nil || !fresh {
			t.Fatalf("receipt %d: fresh=%v %v", i, fresh, err)
		}
	}
	call := f.call
	call.RequestID = "receipt-at-cap"
	call = f.sign(t, call, f.body)
	principal, _, err := f.service.Verify(ctx, call, f.body)
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.store.ClaimAssistant(ctx, call, principal); pa.Code(err) != pa.CapacityFull || fresh {
		t.Fatalf("receipt cap did not refuse: fresh=%v %v", fresh, err)
	}
	call.RequestID = "receipt-0"
	call = f.sign(t, call, f.body)
	principal, _, err = f.service.Verify(ctx, call, f.body)
	if err != nil {
		t.Fatal(err)
	}
	if prior, fresh, err := f.store.ClaimAssistant(ctx, call, principal); err != nil || fresh || prior.State != "unknown" {
		t.Fatalf("exact replay lost barrier: %+v fresh=%v %v", prior, fresh, err)
	}
	grant = f.grant
	grant.Epoch++
	grant.Revoked = true
	if err := f.store.PutAssistantGrant(ctx, grant, f.grant.Epoch); err != nil {
		t.Fatalf("full namespace blocked revocation: %v", err)
	}
	grants, receipts, err := f.store.AssistantUsage(ctx)
	if err != nil || grants != pa.MaxNetworkAssistantGrants || receipts != pa.MaxNetworkAssistantReceipts {
		t.Fatalf("retained actual usage: %d %d %v", grants, receipts, err)
	}
}
