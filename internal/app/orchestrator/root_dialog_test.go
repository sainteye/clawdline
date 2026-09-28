package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A tab whose screen can be changed between readings, as a person answering
// a dialog changes it.
type changingScreen struct {
	mu   sync.Mutex
	name string
}

func (c *changingScreen) set(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.name = name
}

func (c *changingScreen) read(t *testing.T) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return screen(t, c.name)
}

func codexRootRequest(t *testing.T, requestID string) RootAssignmentRequest {
	return RootAssignmentRequest{RequestID: requestID, Assistant: "codex", ProjectDir: t.TempDir(),
		Label: "Board item", Assignment: Assignment{Objective: "o", Scope: "s", Constraints: "c",
			RelevantReferences: "r", Acceptance: "a"}}
}

// testdata/codex-update.txt is the screen two Board items' new Codex Sessions
// opened on, 2026-09-27; both assignments were recorded failed.
func TestACodexRootAtItsFirstScreenDialogIsBriefedOnceThePersonAnswers(t *testing.T) {
	b, ctx := newTestBroker(t)
	launcher := &recordingLauncher{pane: "%90"}
	b.Launcher = launcher
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%90", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
		}
	}
	tab := &changingScreen{name: "codex-update"}
	b.Screen = func(_ context.Context, id string) (string, bool) { return tab.read(t), id == "%90" }
	var settled []RootAssignment
	b.RootAssignmentSettled = func(_ context.Context, a RootAssignment) { settled = append(settled, a) }

	req := codexRootRequest(t, "c9100001-0000-4000-8000-000000000001")
	a, _, err := b.OpenRootAssignment(ctx, req.RequestID, req)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != AssignmentAwaitingDialog || a.AwaitingDialogSince == 0 || a.Failure != "" {
		t.Fatalf("a Root whose first screen is a dialog is %q (failure %q), want awaiting_dialog", a.State, a.Failure)
	}
	if keys.count("%90") != 0 {
		t.Fatalf("something was typed at the dialog: %v", keys.lines["%90"])
	}
	if !strings.Contains(launcher.line(), "check_for_update_on_startup=false") {
		t.Fatalf("the Codex launch still checks for an update: %s", launcher.line())
	}

	// Still the dialog: nothing happens, however many beats look.
	b.Pass(ctx)
	b.Pass(ctx)
	if keys.count("%90") != 0 || len(settled) != 0 {
		t.Fatalf("a beat acted on an unanswered dialog: typed %v, settled %+v", keys.lines["%90"], settled)
	}

	// The person answered it and Codex drew its composer.
	tab.set("codex-composer")
	if p := b.Pass(ctx); p.RootDialogs != 1 {
		t.Fatalf("the pass settled %d Roots at a dialog, want 1", p.RootDialogs)
	}
	if keys.count("%90") != 1 || !strings.Contains(keys.lines["%90"][0], a.BriefPath) {
		t.Fatalf("the assignment was typed %v, want once, naming %s", keys.lines["%90"], a.BriefPath)
	}
	if len(settled) != 1 || settled[0].State != AssignmentBriefed || settled[0].ID != a.ID {
		t.Fatalf("the settlement reported %+v", settled)
	}
	b.Pass(ctx)
	if keys.count("%90") != 1 || len(settled) != 1 {
		t.Fatal("a briefed Root was briefed again")
	}
}

// Closing the tab is how a person gives up on it.
func TestARootLeftAtADialogFailsWhenItsTabIsClosed(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Launcher = &recordingLauncher{pane: "%91"}
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%91", Backend: session.BackendTmux, Assistant: session.AssistantCodex}}
	}
	b.Screen = func(_ context.Context, id string) (string, bool) { return screen(t, "codex-update"), id == "%91" }
	var settled []RootAssignment
	b.RootAssignmentSettled = func(_ context.Context, a RootAssignment) { settled = append(settled, a) }
	req := codexRootRequest(t, "c9100002-0000-4000-8000-000000000002")
	a, _, err := b.OpenRootAssignment(ctx, req.RequestID, req)
	if err != nil || a.State != AssignmentAwaitingDialog {
		t.Fatalf("%+v %v", a, err)
	}

	// A reading whose tmux could not answer says nothing about the tab.
	others := []session.Session{{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude}}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: others, Complete: false, Sources: map[string]bool{"tmux": false}}
	}
	b.Pass(ctx)
	if len(settled) != 0 {
		t.Fatalf("an incomplete reading settled the Root: %+v", settled)
	}

	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: others, Complete: true, Sources: map[string]bool{"tmux": true}}
	}
	b.Pass(ctx)
	if len(settled) != 1 || settled[0].State != AssignmentFailed || settled[0].Failure != rootDialogClosedFailure {
		t.Fatalf("the closed tab settled %+v", settled)
	}
	if keys.count("%91") != 0 {
		t.Fatal("something was typed")
	}
}
