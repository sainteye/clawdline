package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func storedRootForClose(t *testing.T, b *Broker, ctx context.Context) RootAssignment {
	t.Helper()
	a := RootAssignment{ID: "10000000-0000-4000-8000-000000000001", State: AssignmentBriefed,
		Assistant: "codex", ProjectDir: t.TempDir(), BriefedAt: time.Now().Unix(),
		Executor: &openedSession{TerminalID: "%4", Backend: "tmux", OpenedAt: time.Now().Unix()}}
	a.BriefPath = filepath.Join(b.AssignmentRoot(), a.ID, "ASSIGNMENT.md")
	body, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Store.CreateOpened(ctx, store.TableRootAssignments, store.Opened{ID: a.ID,
		State: a.State, Record: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRootClosureIsSeparateDurableAndIdempotent(t *testing.T) {
	b, ctx := newTestBroker(t)
	a := storedRootForClose(t, b, ctx)
	closed := session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "20000000-0000-4000-8000-000000000002"}
	if err := b.RecordRootClosure(ctx, a.ID, closed, "close", false); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordRootClosure(ctx, a.ID, closed, "close", false); err != nil {
		t.Fatal(err)
	}
	got, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != AssignmentBriefed || got.Closure == nil || !got.Closure.Safe || got.Closure.Forced ||
		got.Closure.TerminalID != closed.ID || got.Closure.ConversationID != closed.ConversationID {
		t.Fatalf("wrong durable close: %+v", got)
	}
	if n, err := b.Store.EventCount(ctx, "root_assignment.closed"); err != nil || n != 1 {
		t.Fatalf("close events: %d, %v", n, err)
	}
	// A different Session reusing the pane cannot replace the first receipt.
	if err := b.RecordRootClosure(ctx, a.ID, session.Session{ID: "%5", Backend: session.BackendTmux,
		Assistant: session.AssistantCodex}, "close", false); err == nil {
		t.Fatal("another terminal was accepted as the executor")
	}
}

func TestForcedRootCloseIsNotCompletion(t *testing.T) {
	b, ctx := newTestBroker(t)
	a := storedRootForClose(t, b, ctx)
	closed := session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex}
	if err := b.RecordRootClosure(ctx, a.ID, closed, "archive", true); err != nil {
		t.Fatal(err)
	}
	got, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Closure == nil || got.Closure.Safe || !got.Closure.Forced {
		t.Fatalf("forced close was treated as safe: %+v", got.Closure)
	}
	rd := reading{sessions: map[string]session.Session{"%8": {ID: "%8"}}, sources: map[string]bool{"tmux": true}}
	b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
	p := b.rootCleanup(ctx, rd, got)
	if p.Completion || p.Eligible || p.Reason != "close_forced_or_uncertain" {
		t.Fatalf("forced cleanup: %+v", p)
	}
}

func TestRootCleanupKeepsActiveUnknownAndUnregisteredScratch(t *testing.T) {
	b, ctx := newTestBroker(t)
	a := storedRootForClose(t, b, ctx)
	closed := session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex}
	if err := b.RecordRootClosure(ctx, a.ID, closed, "scheduled", false); err != nil {
		t.Fatal(err)
	}
	a, _ = b.RootAssignmentByID(ctx, a.ID)
	active := reading{sessions: map[string]session.Session{"%4": closed}, sources: map[string]bool{"tmux": true}}
	if p := b.rootCleanup(ctx, active, a); p.Owner != "present" || p.Eligible || p.Reason != "owner_present" {
		t.Fatalf("active owner: %+v", p)
	}
	unknown := reading{sessions: map[string]session.Session{"%8": {ID: "%8"}}, sources: map[string]bool{"tmux": false}}
	if p := b.rootCleanup(ctx, unknown, a); p.Owner != "unknown" || p.Eligible || p.Reason != "owner_unknown" {
		t.Fatalf("unknown owner: %+v", p)
	}
	thatDir := t.TempDir()
	a.ProjectDir = gitRepo(t)
	// A dirty Root checkout and a symlink escape remain untouched because
	// neither is registered as rebuildable scratch.
	if err := os.WriteFile(filepath.Join(a.ProjectDir, "uncommitted.txt"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(thatDir, filepath.Join(a.ProjectDir, "linked-output")); err != nil {
		t.Fatal(err)
	}
	b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
	absent := reading{sessions: map[string]session.Session{"%8": {ID: "%8"}}, sources: map[string]bool{"tmux": true}}
	p := b.rootCleanup(ctx, absent, a)
	if !p.Completion || p.Owner != "absent" || p.Scratch != "unregistered" ||
		p.Preservation != "unverified" || p.Eligible || p.Reason != "scratch_not_registered" {
		t.Fatalf("unregistered scratch: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(a.ProjectDir, "uncommitted.txt")); err != nil {
		t.Fatalf("dirty worktree was changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(a.ProjectDir, "linked-output")); err != nil {
		t.Fatalf("symlink was changed: %v", err)
	}
	b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, errors.New("process table unavailable") }
	if p := b.rootCleanup(ctx, absent, a); p.Owner != "unknown" || p.Eligible {
		t.Fatalf("unreadable process table: %+v", p)
	}
}

func TestRootCleanupListReadsProcessDirectoriesOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	a := storedRootForClose(t, b, ctx)
	second := a
	second.ID = "10000000-0000-4000-8000-000000000003"
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sessions: []session.Session{{ID: "%8"}},
			Sources: map[string]bool{"tmux": true}}
	}
	reads := 0
	b.ProcessCWDs = func(context.Context) ([]string, error) {
		reads++
		return nil, nil
	}
	assignments := []RootAssignment{a, second}
	b.ProjectRootCleanup(ctx, assignments)
	if reads != 1 || assignments[0].Cleanup.Owner != "absent" || assignments[1].Cleanup.Owner != "absent" {
		t.Fatalf("process reads=%d, projections=%+v %+v", reads, assignments[0].Cleanup, assignments[1].Cleanup)
	}
}
