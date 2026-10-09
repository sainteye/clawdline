package cloudops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
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

func TestPinnedImageUsesReadTranscriptAndTheExactSession(t *testing.T) {
	var route LocalRequest
	b := open(localRouterFunc(func(_ context.Context, request LocalRequest) (LocalResponse, error) {
		route = request
		return LocalResponse{Status: 200, ContentType: "image/png", Body: []byte("png")}, nil
	}))
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		return contentReadAuthority(true)
	}
	request := contentReadRequest(t, map[string]any{"type": "image", "session": pane,
		"id": "picture-1", "expected_generation": pinnedGeneration})
	answer := b.Handle(context.Background(), request)
	if !answer.OK() || answer.Name != "image.picture-1" || route.Path != "/v1/artifacts/images/picture-1" ||
		route.Query["session"] != pane || route.Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
		t.Fatalf("pinned image request: answer=%+v route=%+v", answer, route)
	}
	var payload map[string]any
	if err := json.Unmarshal(answer.Payload, &payload); err != nil || payload["read"] != "image.picture-1" ||
		payload["machine_id"] != "mac-01" || payload["session_id"] != pane || payload["seq"] != float64(411) {
		t.Fatalf("image reply does not prove its request: %v %v", payload, err)
	}
	for _, body := range []map[string]any{
		{"type": "image", "session": pane, "id": "picture-1"},
		{"type": "image", "session": pane, "id": "picture-1", "expected_generation": "wrong"},
	} {
		if refused := b.Handle(context.Background(), contentReadRequest(t, body)); refused.OK() {
			t.Fatalf("unpinned image crossed read rail: %+v", refused)
		}
	}
	denied := open(localRouterFunc(func(context.Context, LocalRequest) (LocalResponse, error) {
		t.Fatal("revoked image read reached local route")
		return LocalResponse{}, nil
	}))
	denied.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		return contentReadAuthority(false)
	}
	if refusal := denied.Handle(context.Background(), request); refusal.Code != "read_transcript_required" {
		t.Fatalf("image permission refusal: %+v", refusal)
	}
}

func contentReadAuthority(allowed bool) Authority {
	return Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
		WriteGateAllows: true, ReadTranscriptAllows: allowed}
}

func TestOriginalDetailCommandsAcceptAnExactExecutionPin(t *testing.T) {
	for _, word := range []string{"focus", "smart-title"} {
		r := &router{}
		b := open(r)
		cmd := request(t, ClassCtl, map[string]any{
			"type": word, "session": pane, "request": "press-1",
			"execution_generation": pinnedGeneration,
		})
		answer := b.Handle(context.Background(), cmd)
		if !answer.OK() || len(r.seen) != 1 {
			t.Fatalf("%s pinned command: answer=%+v routes=%+v", word, answer, r.seen)
		}
		if got := r.last().Header; got["X-Clawdline-Target-Machine"] != "mac-01" ||
			got["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatalf("%s lost its selected execution: %v", word, got)
		}
	}
}

func TestReadContentRailPinsTheSelectedExecution(t *testing.T) {
	for _, input := range []struct {
		body map[string]any
		name string
	}{
		{map[string]any{"type": "info", "session": pane, "parts": "full", "expected_generation": pinnedGeneration}, "info.full"},
		{map[string]any{"type": "info", "session": pane, "parts": "list", "expected_generation": pinnedGeneration}, "info.list"},
		{map[string]any{"type": "skills", "session": pane, "expected_generation": pinnedGeneration}, "skills"},
		{map[string]any{"type": "transcript", "session": pane, "limit": 100, "expected_generation": pinnedGeneration}, "transcript"},
		{map[string]any{"type": "transcript", "session": pane, "limit": 100, "before": 12345,
			"expected_generation": pinnedGeneration}, "transcript.before.12345"},
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
		if input.name == "transcript.before.12345" && r.last().Query["before"] != "12345" {
			t.Fatalf("pinned transcript cursor: %+v", r.last().Query)
		}
		if input.name == "info.list" && r.last().Query["parts"] != "list" {
			t.Fatalf("pinned list title query: %+v", r.last().Query)
		}
		if got := answerOf(t, a); got["machine_id"] != "mac-01" || got["session_id"] != pane ||
			got["expected_generation"] != pinnedGeneration || got["seq"] != float64(411) {
			t.Fatalf("the read reply cannot be matched to its pinned request: %v", got)
		}
	}
}

func TestOriginalSessionPanelsUseThePinnedReadRail(t *testing.T) {
	for _, tc := range []struct {
		word string
		body map[string]any
		name string
	}{
		{"git", map[string]any{}, "git"},
		{"git-diff", map[string]any{"request": "diff-1", "path": "README.md"}, "read:diff-1"},
		{"screen", map[string]any{}, "screen"},
		{"agent", map[string]any{"agent": "agent-1", "limit": 100}, "agent:agent-1"},
		{"agent", map[string]any{"agent": "agent-1", "limit": 100, "before": 17}, "agent:agent-1.before.17"},
		{"shell", map[string]any{"shell": "shell-1", "bytes": 4096}, "shell:shell-1"},
		{"documents", map[string]any{}, "documents"},
		{"document", map[string]any{"request": "doc-1", "scope": "project", "task": "", "path": "README.md"}, "read:doc-1"},
	} {
		t.Run(tc.name+"/"+tc.word, func(t *testing.T) {
			r := &router{}
			if tc.word == "documents" {
				r.body = `{"documents":[]}`
			}
			if tc.word == "document" {
				r.body, r.media = "# report\n", "text/markdown; charset=utf-8"
			}
			b := open(r)
			b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
				return contentReadAuthority(true)
			}
			body := map[string]any{"type": tc.word, "session": pane, "expected_generation": pinnedGeneration}
			for key, value := range tc.body {
				body[key] = value
			}
			answer := b.Handle(context.Background(), contentReadRequest(t, body))
			if !answer.OK() || answer.Name != tc.name || len(r.seen) != 1 ||
				r.last().Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
				t.Fatalf("pinned panel read: answer=%+v routes=%+v", answer, r.seen)
			}
			if got := answerOf(t, answer); got["expected_generation"] != pinnedGeneration || got["seq"] != float64(411) {
				t.Fatalf("read reply has no request proof: %v", got)
			}
			delete(body, "expected_generation")
			if stale := b.Handle(context.Background(), contentReadRequest(t, body)); stale.OK() {
				t.Fatalf("unpinned read crossed r/: %+v", stale)
			}
		})
	}
}

func TestPinnedContentReadRejectsGenerationChangedWhileReading(t *testing.T) {
	checks := 0
	b := open(localRouterFunc(func(context.Context, LocalRequest) (LocalResponse, error) {
		return LocalResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"secret":"old execution"}`)}, nil
	}))
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	b.AdmitExecution = func(context.Context, string, string, string) error {
		checks++
		return errors.New("execution_generation_changed")
	}
	answer := b.Handle(context.Background(), contentReadRequest(t, map[string]any{
		"type": "git", "session": pane, "expected_generation": pinnedGeneration,
	}))
	if answer.OK() || answer.Code != "execution_generation_changed" || checks != 1 ||
		strings.Contains(string(answer.Payload), "old execution") {
		t.Fatalf("changed execution exposed a prior read: %+v checks=%d", answer, checks)
	}
}

func TestReadContentRepliesWithTheSameNameKeepSeparateExecutionIdentities(t *testing.T) {
	b := open(&router{})
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	first := contentReadRequest(t, map[string]any{
		"type": "info", "session": pane, "parts": "full", "expected_generation": pinnedGeneration,
	})
	secondGeneration := strings.Repeat("f", 32)
	second := contentReadRequest(t, map[string]any{
		"type": "info", "session": pane, "parts": "full", "expected_generation": secondGeneration,
	})
	second.Sequence = first.Sequence + 1
	one := answerOf(t, b.Handle(context.Background(), first))
	two := answerOf(t, b.Handle(context.Background(), second))
	if one["read"] != two["read"] || one["expected_generation"] != pinnedGeneration ||
		two["expected_generation"] != secondGeneration || one["seq"] == two["seq"] {
		t.Fatalf("same-name replies cannot be distinguished: first=%v second=%v", one, two)
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
		{"skills missing generation", map[string]any{"type": "skills", "session": pane}, true, true, "execution_target_required"},
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

func TestPeerInboxRequiresThePinnedContentRailAndCurrentViewerGrant(t *testing.T) {
	input := map[string]any{"type": "peer-inbox", "machine_id": "mac-01", "session": pane,
		"request": "inbox-1", "expected_generation": pinnedGeneration}
	r := &router{}
	b := open(r)
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		return contentReadAuthority(true)
	}
	ctl := request(t, ClassCtl, input)
	if answer := b.Handle(context.Background(), ctl); answer.Code != "read_only_channel" || len(r.seen) != 0 {
		t.Fatalf("ordinary control read crossed content gate: %+v, %+v", answer, r.seen)
	}
	read := contentReadRequest(t, input)
	if answer := b.Handle(context.Background(), read); !answer.OK() || answer.Name != "read:inbox-1" ||
		len(r.seen) != 1 || r.last().Path != "/v1/cloud/peer/inbox" ||
		r.last().Query["machine_id"] != "mac-01" || r.last().Query["session_id"] != pane ||
		r.last().Query["execution_generation"] != pinnedGeneration {
		t.Fatalf("pinned peer inbox read: %+v, %+v", answer, r.seen)
	}
	r.seen = nil
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		return contentReadAuthority(false)
	}
	if answer := b.Handle(context.Background(), read); answer.Code != "read_transcript_required" || len(r.seen) != 0 {
		t.Fatalf("revoked viewer read crossed content gate: %+v, %+v", answer, r.seen)
	}
}
