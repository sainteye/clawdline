package agenthandoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	contract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

func fixture() (contract.Request, []byte, contract.Principal, contract.Facts) {
	body := []byte("Agent work")
	sum := sha256.Sum256(body)
	request := contract.Request{
		RequestID: "request-1", Kind: contract.Message,
		Source: contract.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: "11111111111111111111111111111111"},
		Target: contract.Endpoint{MachineID: "machine-b", SessionID: "session-b",
			ExecutionGeneration: "22222222222222222222222222222222"},
		GrantID: "grant-1", BodyDigest: hex.EncodeToString(sum[:]),
	}
	principal := contract.Principal{MachineID: "machine-a", KeyFingerprint: "key-a",
		PairID: "pair-1", MachinePeer: true}
	facts := contract.Facts{
		LocalMachineID: "machine-b", PairActive: true, GrantReadable: true,
		Grant: contract.Grant{
			ID: "grant-1", PairID: "pair-1", SourceMachineID: "machine-a", SourceSessionID: "session-a",
			SourceGeneration: request.Source.ExecutionGeneration,
			TargetMachineID:  "machine-b", TargetSessionID: "session-b",
			TargetGeneration:   request.Target.ExecutionGeneration,
			PeerKeyFingerprint: "key-a", AllowMessage: true, AllowHandoff: true,
			ExpiresAt: time.Unix(2000, 0),
		},
		PeerCapability: true, LocalCapability: true, MachineWritesAllowed: true,
		TargetCurrent: true, Now: time.Unix(1000, 0),
	}
	return request, body, principal, facts
}

func TestReceiverPersistsOneEffectAndRefusesChangedOrRevokedRetry(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	request, body, principal, facts := fixture()
	executed := 0
	r := Receiver{
		Receipts: s, Now: func() time.Time { return time.Unix(1000, 0) },
		Facts: func(context.Context, contract.Request, contract.Principal) (contract.Facts, error) {
			return facts, nil
		},
		Execute: func(context.Context, contract.Request, []byte) error {
			executed++
			return nil
		},
	}
	if got := r.Receive(ctx, request, body, principal); got.Code != "ok" || got.MachineExecution != "completed" {
		t.Fatalf("first result: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r.Receipts = s
	if got := r.Receive(ctx, request, body, principal); got.Code != "ok" || executed != 1 {
		t.Fatalf("restart replay: %+v, executions=%d", got, executed)
	}
	changed := request
	changed.Kind = contract.Handoff
	if got := r.Receive(ctx, changed, body, principal); got.Code != "handoff_request_id_reused" || executed != 1 {
		t.Fatalf("changed request: %+v, executions=%d", got, executed)
	}
	facts.Grant.Revoked = true
	if got := r.Receive(ctx, request, body, principal); got.Code != string(contract.GrantRevoked) || executed != 1 {
		t.Fatalf("revoked replay: %+v, executions=%d", got, executed)
	}
}

func TestReceiverRechecksGrantAfterClaim(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request, body, principal, facts := fixture()
	reads, executed := 0, 0
	r := Receiver{
		Receipts: s, Now: func() time.Time { return time.Unix(1000, 0) },
		Facts: func(context.Context, contract.Request, contract.Principal) (contract.Facts, error) {
			reads++
			if reads == 2 {
				facts.Grant.Revoked = true
			}
			return facts, nil
		},
		Execute: func(context.Context, contract.Request, []byte) error {
			executed++
			return nil
		},
	}
	if got := r.Receive(context.Background(), request, body, principal); got.Code != string(contract.GrantRevoked) || executed != 0 {
		t.Fatalf("revoke between reads: %+v, executions=%d", got, executed)
	}
	if reads != 2 {
		t.Fatalf("authorization reads = %d, want 2", reads)
	}
}

func TestReceiverTreatsUncertainEffectAsUnknown(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request, body, principal, facts := fixture()
	executed := 0
	r := Receiver{
		Receipts: s, Now: func() time.Time { return time.Unix(1000, 0) },
		Facts: func(context.Context, contract.Request, contract.Principal) (contract.Facts, error) {
			return facts, nil
		},
		Execute: func(context.Context, contract.Request, []byte) error {
			executed++
			return errors.New("the terminal outcome could not be proved")
		},
	}
	for i := 0; i < 2; i++ {
		got := r.Receive(context.Background(), request, body, principal)
		if got.Code != "handoff_outcome_unknown" || got.MachineExecution != "unknown" || got.AgentAcknowledged != "unknown" {
			t.Fatalf("uncertain result %d: %+v", i, got)
		}
	}
	if executed != 1 {
		t.Fatalf("an unknown result was executed %d times", executed)
	}
}
