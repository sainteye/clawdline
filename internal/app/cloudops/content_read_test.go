package cloudops

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
)

func contentReadRequest(t *testing.T, input map[string]any) Command {
	t.Helper()
	if _, present := input["machine_id"]; !present {
		input["machine_id"] = "mac-01"
	}
	cmd := request(t, ClassCtl, input)
	cmd.Channel = "r/mac-01"
	return cmd
}

func contentReadAuthority(allowed bool) Authority {
	return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
		WriteGateAllows: true, ReadTranscriptAllows: allowed}
}

func TestReadContentRailPinsTheSelectedExecution(t *testing.T) {
	for _, input := range []struct {
		body map[string]any
		name string
	}{
		{map[string]any{"type": "info", "session": pane, "parts": "full", "expected_generation": pinnedGeneration}, "info.full"},
		{map[string]any{"type": "transcript", "session": pane, "limit": 100, "expected_generation": pinnedGeneration}, "transcript"},
	} {
		r := &router{}
		checks := 0
		b := open(r)
		b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
			checks++
			return contentReadAuthority(true)
		}
		a := b.Handle(context.Background(), contentReadRequest(t, input.body))
		if !a.OK() || a.Name != input.name || a.Session != pane || checks != 2 || len(r.seen) != 1 {
			t.Fatalf("%s: answer=%+v checks=%d routes=%+v", input.name, a, checks, r.seen)
		}
		if got := r.last().Header; got["X-Clawdline-Target-Machine"] != "mac-01" || got["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatalf("pinned target headers: %v", got)
		}
	}
}

func TestReadContentRailRejectsUnpinnedAndUnauthorizedRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  map[string]any
		allow bool
		wire  bool
		code  string
	}{
		{"missing generation", map[string]any{"type": "transcript", "session": pane, "limit": 10}, true, true, "execution_target_required"},
		{"missing cap", map[string]any{"type": "transcript", "session": pane, "limit": 10, "expected_generation": pinnedGeneration}, false, true, "read_transcript_required"},
		{"missing bridge", map[string]any{"type": "info", "session": pane, "parts": "full", "expected_generation": pinnedGeneration}, true, false, "read_transcript_unavailable"},
		{"summary", map[string]any{"type": "info", "session": pane, "parts": "summary", "expected_generation": pinnedGeneration}, true, true, "read_only_channel"},
		{"wrong machine", map[string]any{"type": "info", "machine_id": "elsewhere", "session": pane, "parts": "full", "expected_generation": pinnedGeneration}, true, true, "wrong_machine"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &router{}
			b := open(r)
			if tc.wire {
				b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(tc.allow) }
			}
			a := b.Handle(context.Background(), contentReadRequest(t, tc.body))
			if a.Code != tc.code || a.OK() || len(r.seen) != 0 {
				t.Fatalf("answer=%+v routes=%+v", a, r.seen)
			}
		})
	}
}

func TestReadContentRailRejectsCommandDisguises(t *testing.T) {
	for _, word := range []string{"send", "answer", "end", "receipt", "sessions.snapshot"} {
		t.Run(word, func(t *testing.T) {
			r := &router{}
			b := open(r)
			b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
			cmd := contentReadRequest(t, map[string]any{"type": word, "session": pane, "request": "test-request", "expected_generation": pinnedGeneration})
			a := b.Handle(context.Background(), cmd)
			if a.Code != "read_only_channel" || len(r.seen) != 0 {
				t.Fatalf("%s: answer=%+v routes=%+v", word, a, r.seen)
			}
		})
	}
}

func TestReadContentRailChecksRevocationBeforeSealing(t *testing.T) {
	allowed := true
	b := open(localRouterFunc(func(context.Context, LocalRequest) (LocalResponse, error) {
		allowed = false
		return LocalResponse{Status: 200, Body: []byte(`{"private":"text"}`)}, nil
	}))
	checks := 0
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		checks++
		a := contentReadAuthority(allowed)
		if !allowed {
			a.RosterAllowsSender = false
		}
		return a
	}
	a := b.Handle(context.Background(), contentReadRequest(t, map[string]any{
		"type": "transcript", "session": pane, "limit": 10, "expected_generation": pinnedGeneration,
	}))
	if checks != 2 || a.Code != "unknown_sender" || strings.Contains(string(a.Payload), "private") {
		t.Fatalf("answer=%+v checks=%d", a, checks)
	}
}
