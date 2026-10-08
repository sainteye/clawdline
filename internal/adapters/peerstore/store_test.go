package peerstore

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerPinsGrantsAndReceiptsSurviveRestartAndLocalRevoke(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private-peers")
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := ed25519.NewKeyFromSeed(fill(1))
	target := ed25519.NewKeyFromSeed(fill(2))
	sourceX, _ := ecdh.X25519().NewPrivateKey(fill(3))
	targetX, _ := ecdh.X25519().NewPrivateKey(fill(4))
	pair := Pair{ID: "pair-1", AccountID: "account-1", SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourcePublicKey:     base64.StdEncoding.EncodeToString(source.Public().(ed25519.PublicKey)),
		TargetPublicKey:     base64.StdEncoding.EncodeToString(target.Public().(ed25519.PublicKey)),
		SourceFingerprint:   cloud.Fingerprint(source.Public().(ed25519.PublicKey)),
		TargetFingerprint:   cloud.Fingerprint(target.Public().(ed25519.PublicKey)),
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(sourceX.PublicKey().Bytes()),
		TargetEncryptionKey: base64.StdEncoding.EncodeToString(targetX.PublicKey().Bytes()),
		LocalPrivateKey:     sourceX.Bytes(), ExpiresAt: time.Now().Add(2 * time.Hour)}
	transcript := agenthandoff.PairTranscript{PairID: pair.ID, SourceMachineID: pair.SourceMachineID,
		TargetMachineID: pair.TargetMachineID, SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey}
	pair.SourceSignature = agenthandoff.SignOffer(transcript, source)
	transcript.SourceOfferSignature = pair.SourceSignature
	pair.TargetSignature = agenthandoff.SignAcceptance(transcript, target)
	pending := pair
	pending.TargetPublicKey, pending.TargetEncryptionKey, pending.TargetSignature = "", "", ""
	if err := st.SavePending(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := st.PinPair(ctx, pair, "machine-a", pair.TargetFingerprint); err != nil {
		t.Fatal(err)
	}
	if err := st.PinPair(ctx, pair, "machine-a", pair.TargetFingerprint); err != nil {
		t.Fatalf("idempotent pair pin: %v", err)
	}
	grant := Grant{ID: "grant-1", PairID: pair.ID,
		Source: agenthandoff.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: "11111111111111111111111111111111"},
		Target: agenthandoff.Endpoint{MachineID: "machine-b", SessionID: "session-b",
			ExecutionGeneration: "22222222222222222222222222222222"},
		SourceKeyFingerprint: pair.SourceFingerprint, AllowMessage: true,
		ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.PinGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	body := []byte("hello")
	sum := sha256.Sum256(body)
	request := agenthandoff.Request{RequestID: "request-1", Kind: agenthandoff.Message,
		Source: grant.Source, Target: grant.Target, GrantID: grant.ID, BodyDigest: hex.EncodeToString(sum[:])}
	if err := st.SaveOutbox(ctx, OutboxItem{Request: request, RelayAccepted: "unknown", RelayDelivered: "unknown", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Outbox(ctx, request.RequestID); err != nil || saved.RelayAccepted != "unknown" || saved.RelayDelivered != "unknown" {
		t.Fatalf("a socket write cannot prove relay acceptance: %+v, %v", saved, err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "machine_offline"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutInbox(ctx, InboxItem{Request: request, Body: string(body), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if saved, err := st.Outbox(ctx, request.RequestID); err != nil || saved.RelayDelivered != "unknown" || !saved.RelayConflict {
		t.Fatalf("relay evidence after restart: %+v, %v", saved, err)
	}
	if saved, err := st.Inbox(ctx, request.RequestID); err != nil || saved.Body != "hello" {
		t.Fatalf("inbox after restart: %+v, %v", saved, err)
	}
	if usage, err := ReadUsage(ctx, dir); err != nil || usage != (Usage{Pairs: 1, Grants: 1, Inbox: 1, Outbox: 1}) {
		t.Fatalf("peer capacity after restart: %+v, %v", usage, err)
	}
	if err := st.RevokeGrant(ctx, grant.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Grant(ctx, grant.ID); err != nil || saved.RevokedAt.IsZero() {
		t.Fatalf("local grant revocation was not durable: %+v, %v", saved, err)
	}
	if err := st.RevokePair(ctx, pair.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Pair(ctx, pair.ID); err != nil || saved.RevokedAt.IsZero() {
		t.Fatalf("local pair revocation was not durable: %+v, %v", saved, err)
	}
}

func TestPeerInboxPagePinsExecutionAndBoundsTheCloudReply(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "private-peers"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	target := agenthandoff.Endpoint{MachineID: "machine-b", SessionID: "session-b",
		ExecutionGeneration: "22222222222222222222222222222222"}
	for _, row := range []struct{ id, generation string }{
		{"first", target.ExecutionGeneration},
		{"other-generation", "33333333333333333333333333333333"},
		{"last", target.ExecutionGeneration},
	} {
		request := agenthandoff.Request{RequestID: row.id, Target: target}
		request.Target.ExecutionGeneration = row.generation
		if err := st.PutInbox(ctx, InboxItem{Request: request, Body: row.id, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := st.InboxPage(ctx, target, "")
	if err != nil || len(first) != 1 || first[0].Request.RequestID != "last" || cursor != "last" {
		t.Fatalf("first page: %+v, %q, %v", first, cursor, err)
	}
	second, cursor, err := st.InboxPage(ctx, target, cursor)
	if err != nil || len(second) != 1 || second[0].Request.RequestID != "first" || cursor != "" {
		t.Fatalf("second page: %+v, %q, %v", second, cursor, err)
	}
	if _, _, err := st.InboxPage(ctx, target, "other-generation"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign generation cursor accepted: %v", err)
	}
}

func fill(value byte) []byte {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = value
	}
	return bytes
}
