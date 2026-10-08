package cloudops

import (
	"context"
	"crypto/ed25519"
	"testing"
)

const pinnedGeneration = "0123456789abcdef0123456789abcdef"

type localRouterFunc func(context.Context, LocalRequest) (LocalResponse, error)

func (fn localRouterFunc) Do(ctx context.Context, req LocalRequest) (LocalResponse, error) {
	return fn(ctx, req)
}

func TestPinnedCloudReadsCarryMachineAndExecution(t *testing.T) {
	for _, input := range []map[string]any{
		{"type": "transcript", "session": pane, "limit": 100, "expected_generation": pinnedGeneration},
		{"type": "info", "session": pane, "parts": "full", "expected_generation": pinnedGeneration},
	} {
		r := &router{}
		a := open(r).Handle(context.Background(), request(t, ClassCtl, input))
		if !a.OK() || len(r.seen) != 1 {
			t.Fatalf("pinned read: %+v, routes %+v", a, r.seen)
		}
		if got := r.last().Header; got["X-Clawdline-Target-Machine"] != "mac-01" || got["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatalf("target headers: %v", got)
		}
	}
	r := &router{}
	a := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "transcript", "session": pane, "limit": 100, "expected_generation": "bad",
	}))
	if a.Status != 400 || len(r.seen) != 0 {
		t.Fatalf("malformed generation reached route: %+v %+v", a, r.seen)
	}
}

func TestRevocationDuringPinnedTranscriptReadDropsContent(t *testing.T) {
	r := &router{body: `{"private":"text"}`}
	allowed := true
	b := open(localRouterFunc(func(ctx context.Context, req LocalRequest) (LocalResponse, error) {
		answer, err := r.Do(ctx, req)
		allowed = false // The roster changes while the local read is in flight.
		return answer, err
	}))
	checks := 0
	b.Authority = func(context.Context, string, ed25519.PublicKey, bool) Authority {
		checks++
		return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: allowed, WriteGateAllows: true}
	}
	a := b.Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "transcript", "session": pane, "limit": 100, "expected_generation": pinnedGeneration,
	}))
	if a.Status != 403 || a.Code != "unknown_sender" || checks != 1 || len(r.seen) != 1 {
		t.Fatalf("revocation after local read: %+v checks=%d routes=%+v", a, checks, r.seen)
	}
	if string(a.Payload) == `{"private":"text"}` {
		t.Fatal("revoked viewer received content")
	}
}
