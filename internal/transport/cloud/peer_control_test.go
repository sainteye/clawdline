package cloud

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerControlStatusRecoversIDsWithoutPrivateMaterialAndHidesRevokedAuthority(t *testing.T) {
	ctx := context.Background()
	st, err := peerstore.Open(filepath.Join(t.TempDir(), "private-peers"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	source := ed25519.NewKeyFromSeed([]byte(strings.Repeat("a", 32)))
	target := ed25519.NewKeyFromSeed([]byte(strings.Repeat("b", 32)))
	sourceX, _ := ecdh.X25519().NewPrivateKey([]byte(strings.Repeat("c", 32)))
	targetX, _ := ecdh.X25519().NewPrivateKey([]byte(strings.Repeat("d", 32)))
	pair := peerstore.Pair{ID: "pair-active", AccountID: "account-1",
		SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourcePublicKey:     base64.StdEncoding.EncodeToString(source.Public().(ed25519.PublicKey)),
		TargetPublicKey:     base64.StdEncoding.EncodeToString(target.Public().(ed25519.PublicKey)),
		SourceFingerprint:   domaincloud.Fingerprint(source.Public().(ed25519.PublicKey)),
		TargetFingerprint:   domaincloud.Fingerprint(target.Public().(ed25519.PublicKey)),
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(sourceX.PublicKey().Bytes()),
		TargetEncryptionKey: base64.StdEncoding.EncodeToString(targetX.PublicKey().Bytes()),
		LocalPrivateKey:     sourceX.Bytes(), ExpiresAt: now.Add(2 * time.Hour)}
	transcript := agenthandoff.PairTranscript{PairID: pair.ID,
		SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey}
	pair.SourceSignature = agenthandoff.SignOffer(transcript, source)
	transcript.SourceOfferSignature = pair.SourceSignature
	pair.TargetSignature = agenthandoff.SignAcceptance(transcript, target)
	if err := st.PinPair(ctx, pair, "machine-a", pair.TargetFingerprint); err != nil {
		t.Fatal(err)
	}
	pending := pair
	pending.ID = "pair-pending"
	pending.TargetPublicKey, pending.TargetEncryptionKey, pending.TargetSignature = "", "", ""
	if err := st.SavePending(ctx, pending); err != nil {
		t.Fatal(err)
	}
	grant := peerstore.Grant{ID: "grant-active", PairID: pair.ID,
		Source: agenthandoff.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: strings.Repeat("1", 32)},
		Target: agenthandoff.Endpoint{MachineID: "machine-b", SessionID: "session-b",
			ExecutionGeneration: strings.Repeat("2", 32)},
		SourceKeyFingerprint: pair.SourceFingerprint, AllowMessage: true, AllowHandoff: true,
		ExpiresAt: now.Add(time.Hour)}
	if err := st.PinGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	base := PeerControlResult{Action: "status", LocalMachineID: "machine-a", LocalFingerprint: pair.SourceFingerprint}
	listed, err := peerControlStatus(ctx, st, base, "account-1", now)
	if err != nil || listed.Pairs == nil || listed.Grants == nil || len(*listed.Pairs) != 2 || len(*listed.Grants) != 1 ||
		(*listed.Pairs)[0].State != "waiting_for_target" || (*listed.Pairs)[1].State != "active" ||
		(*listed.Grants)[0].GrantID != grant.ID || len((*listed.Grants)[0].Scopes) != 2 {
		t.Fatalf("status lost restart-safe authority IDs: %+v, %v", listed, err)
	}
	encoded, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"local_private_key", "source_public_key", "target_public_key",
		"source_encryption_key", "target_encryption_key", "source_signature", "target_signature", "body"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("status leaked %s: %s", forbidden, encoded)
		}
	}
	if err := st.RevokeGrant(ctx, grant.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokePair(ctx, pair.ID, now); err != nil {
		t.Fatal(err)
	}
	listed, err = peerControlStatus(ctx, st, base, "account-1", now.Add(time.Second))
	if err != nil || listed.Pairs == nil || listed.Grants == nil || len(*listed.Pairs) != 1 ||
		(*listed.Pairs)[0].PairID != pending.ID || len(*listed.Grants) != 0 {
		t.Fatalf("revoked authority remained active: %+v, %v", listed, err)
	}
}
