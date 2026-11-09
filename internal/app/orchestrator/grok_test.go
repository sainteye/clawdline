package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestGrokDispatchOwnsTheWorkspaceAndBriefsOnlyOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	const id = "d8400001-0000-4000-8000-000000000001"
	b.OwnedProviderAvailable = func(_ context.Context, assistant string) bool { return assistant == "grok" }
	opens, sends := 0, 0
	var row session.Session
	b.OpenOwnedChild = func(_ context.Context, r Record, cwd, _ string, _ squadfiles.Files, _ workflowlaunch.Prepared) (OwnedChild, error) {
		opens++
		if r.Assistant != "grok" || r.PermissionMode != "full" || r.Worktree == nil || cwd != r.Worktree.Path {
			t.Fatal("provider, permission or workspace changed")
		}
		out := ownedFixture(cwd)
		out.Row.Assistant = session.AssistantGrok
		out.ReasoningEffort = ""
		row = out.Row
		return out, nil
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sources: map[string]bool{"owned": true}, Sessions: []session.Session{row}}
	}
	b.Type = func(ctx context.Context, pane, line string) error {
		sends++
		r, _, err := b.Record(ctx, id)
		if err != nil || r.ChildConversationID != row.ConversationID || pane != row.ID {
			t.Fatal("first input preceded durable ownership")
		}
		_, err = b.Accept(ctx, id, w1Secret)
		return err
	}
	writeBrief(t, b, id, repo, map[string]any{"assistant": "grok", "permission_mode": "full", "isolation": "worktree", "claims": []string{"program.go"}})
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true, Generation: inv.Generation}
	out, err := b.Dispatch(ctx, req)
	if err != nil || out.Record.State != StateBriefed {
		t.Fatalf("dispatch=%+v %v", out.Record, err)
	}
	if _, err := b.Dispatch(ctx, req); err != nil {
		t.Fatal(err)
	}
	if opens != 1 || sends != 1 {
		t.Fatalf("opens=%d sends=%d", opens, sends)
	}
}

func TestGrokEditsCeilingIsRefusedBeforeOpeningAWorker(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.OwnedProviderAvailable = func(context.Context, string) bool { return true }
	b.OpenOwnedChild = func(context.Context, Record, string, string, squadfiles.Files, workflowlaunch.Prepared) (OwnedChild, error) {
		t.Fatal("unsupported permission opened a worker")
		return OwnedChild{}, nil
	}
	const id = "d8400002-0000-4000-8000-000000000002"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "grok", "permission_mode": "edits"})
	_, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	var refusal Refusal
	if !errors.As(err, &refusal) || refusal.Code != "provider_permission_unsupported" {
		t.Fatalf("permission refusal=%v", err)
	}
}

func TestGrokCompletionNoticesUseTheNativeReceiptPath(t *testing.T) {
	row := session.Session{Assistant: session.AssistantGrok, Backend: session.BackendOwned, Binding: session.BindingProtocol}
	if !nativeNoticeTarget(row) {
		t.Fatal("Grok notice fell back to terminal typing")
	}
	row.Backend = session.BackendTmux
	if nativeNoticeTarget(row) {
		t.Fatal("unverified Grok TUI admitted as protocol")
	}
	if ComposerReady("fake prompt", session.AssistantGrok) || ReadComposer("fake prompt", session.AssistantGrok) != ComposerNone {
		t.Fatal("unverified terminal composer admitted")
	}
}
