package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestOpenRemoteTaskAntigravity(t *testing.T) {
	ctx := context.Background()
	brokerDir := t.TempDir()
	projectDir := t.TempDir()
	taskID := NewUUID()

	var typedSession, typedText string
	targetSession := session.Session{
		ID:        "owned:acp:synthetic-1",
		Backend:   session.BackendOwned,
		Assistant: session.AssistantAntigravity,
		State:     session.StateIdle,
		CWD:       projectDir,
	}

	b := &Broker{
		Dir: brokerDir,
		OpenOwnedSession: func(ctx context.Context, cwd, name, model string) (session.Session, error) {
			if cwd != projectDir {
				t.Fatalf("unexpected cwd: %s", cwd)
			}
			return targetSession, nil
		},
		Live: func(ctx context.Context) []session.Session {
			return []session.Session{targetSession}
		},
		Type: func(ctx context.Context, terminalID, text string) error {
			typedSession = terminalID
			typedText = text
			return nil
		},
	}

	body := agenthandoff.TaskBody{
		Assistant:    "antigravity",
		Title:        "Implement Remote Protocol",
		Instructions: "Verify remote handoff works with Antigravity agent.",
	}

	termID, err := b.OpenRemoteTask(ctx, taskID, projectDir, body)
	if err != nil {
		t.Fatalf("OpenRemoteTask failed: %v", err)
	}
	if termID != targetSession.ID {
		t.Fatalf("expected terminal ID %q, got %q", targetSession.ID, termID)
	}
	if typedSession != targetSession.ID {
		t.Fatalf("expected typed session %q, got %q", targetSession.ID, typedSession)
	}
	if !strings.Contains(typedText, "TASK.md") || !strings.Contains(typedText, "carry it out") {
		t.Fatalf("unexpected typed text: %q", typedText)
	}

	// Verify the brief TASK.md content
	briefDir := filepath.Join(brokerDir, "remote-tasks")
	entries, err := os.ReadDir(briefDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 task dir, got %d: %v", len(entries), err)
	}
	briefFile := filepath.Join(briefDir, entries[0].Name(), "TASK.md")
	content, err := os.ReadFile(briefFile)
	if err != nil {
		t.Fatalf("failed to read brief file: %v", err)
	}
	if !strings.Contains(string(content), "Implement Remote Protocol") ||
		!strings.Contains(string(content), "Verify remote handoff works with Antigravity agent.") {
		t.Fatalf("TASK.md does not contain expected body: %s", string(content))
	}
}

func TestOpenRemoteTaskUnknownAssistant(t *testing.T) {
	ctx := context.Background()
	b := &Broker{Dir: t.TempDir()}
	body := agenthandoff.TaskBody{
		Assistant:    "unregistered_agent",
		Title:        "Title",
		Instructions: "Instructions",
	}
	_, err := b.OpenRemoteTask(ctx, NewUUID(), t.TempDir(), body)
	if err == nil || err.Error() != "remote_task_invalid" {
		t.Fatalf("expected remote_task_invalid, got: %v", err)
	}
}

func TestComposerReadyOwnedSessionStates(t *testing.T) {
	ctx := context.Background()
	current := session.Session{
		ID:        "owned:acp:test",
		Backend:   session.BackendOwned,
		Assistant: session.AssistantAntigravity,
		State:     session.StateIdle,
	}

	b := &Broker{
		Live: func(ctx context.Context) []session.Session {
			return []session.Session{current}
		},
	}

	// 1. Idle -> ready
	ready, err := b.composerReady(ctx, current.ID, "antigravity")
	if !ready || err != nil {
		t.Fatalf("expected ready on idle, got ready=%v, err=%v", ready, err)
	}

	// 2. Waiting -> errShowingDialog
	current.State = session.StateWaiting
	ready, err = b.composerReady(ctx, current.ID, "antigravity")
	if ready || err != errShowingDialog {
		t.Fatalf("expected errShowingDialog on waiting, got ready=%v, err=%v", ready, err)
	}

	// 3. Working -> busy error
	current.State = session.StateWorking
	ready, err = b.composerReady(ctx, current.ID, "antigravity")
	if ready || err == nil || !strings.Contains(err.Error(), "busy working") {
		t.Fatalf("expected busy error on working, got ready=%v, err=%v", ready, err)
	}
}
