package cloudops

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestGraphifyReadsCarryOnlyKnownProjectOrSessionIdentity(t *testing.T) {
	for _, test := range []struct {
		class Class
		body  map[string]any
		path  string
	}{
		{ClassCtl, map[string]any{"type": "project-graphify", "project": "p1", "request": "view-1", "session": MachineReplySession}, "/v1/projects/p1/graphify"},
		{ClassCtl, map[string]any{"type": "session-graphify", "session": "%19"}, "/v1/sessions/%2519/graphify"},
	} {
		r := &router{}
		a := open(r).Handle(context.Background(), request(t, test.class, test.body))
		if a.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != test.path || len(r.last().Query) != 0 {
			t.Fatalf("%+v %v", a, r.seen)
		}
		test.body["path"] = "/outside"
		r = &router{}
		a = open(r).Handle(context.Background(), request(t, test.class, test.body))
		if a.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("client path accepted: %+v %v", a, r.seen)
		}
	}
}

func TestGraphifyContentReadPinsTheSelectedExecution(t *testing.T) {
	r := &router{}
	b := open(r)
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	a := b.Handle(context.Background(), contentReadRequest(t, map[string]any{"type": "session-graphify", "session": pane, "expected_generation": pinnedGeneration}))
	if !a.OK() || len(r.seen) != 1 || r.last().Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
		t.Fatalf("%+v %v", a, r.seen)
	}
	r = &router{}
	b = open(r)
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	a = b.Handle(context.Background(), contentReadRequest(t, map[string]any{"type": "session-graphify", "session": pane}))
	if a.OK() || len(r.seen) != 0 {
		t.Fatalf("unpinned read: %+v %v", a, r.seen)
	}
}
