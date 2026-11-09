//go:build darwin || linux

package orchestrator

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
	"testing"
	"time"
)

func TestGrokCallbackReturnsToItsActualOwnedRoot(t *testing.T) {
	b, ctx := newTestBroker(t)
	req := callbackRequest(t, "a1000000-0000-4000-8000-000000000001", "true")
	req.Root.Assistant = "grok"
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "owned:acp:synthetic-root", Assistant: session.AssistantGrok, Backend: session.BackendOwned, Binding: session.BindingProtocol, ConversationID: rootConversation, CWD: req.Dir}}
	}
	out, err := b.StartCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.Root.Assistant != "grok" || out.Record.RootTerminalID != "owned:acp:synthetic-root" {
		t.Fatalf("wrong callback root: %+v", out.Record.Root)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 5*time.Second)
	if r.State != StateSuccess || r.Root.Assistant != "grok" || r.Notice == nil {
		t.Fatalf("callback did not settle for Grok: %+v", r)
	}
}
