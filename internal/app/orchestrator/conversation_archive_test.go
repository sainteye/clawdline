package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

type archiveCalls struct {
	mu  sync.Mutex
	ids []string
}

func (a *archiveCalls) archive(_ context.Context, assistant, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ids = append(a.ids, assistant+":"+id)
	return nil
}

func (a *archiveCalls) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ids)
}

const archivedConversation = "c6900000-0000-4000-8000-000000000001"

// settledScheduledCodexRun records a finished scheduled Codex run whose tab
// owes a close, with archive set as given.
func settledScheduledCodexRun(t *testing.T, b *Broker, ctx context.Context, id string, archive bool) {
	t.Helper()
	r := Record{Protocol: Protocol, ID: id, Assistant: "codex", Title: "hourly", State: StateBriefed,
		CreatedAt: b.now(), Claims: []string{}, ChildTerminalID: "%41", ChildBackend: "tmux",
		ChildConversationID: archivedConversation, ScheduleID: "5c000000-0000-4000-8000-0000000000a1",
		ScheduleTitle: "hourly", ScheduleArchiveConversation: archive}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
}

func idleCodexTab() reading {
	return reading{sessions: map[string]session.Session{"%41": {ID: "%41", Assistant: session.AssistantCodex,
		State: session.StateIdle, ConversationID: archivedConversation}},
		sources: map[string]bool{"tmux": true}, complete: true}
}

// A scheduled Codex run that asks for it has its conversation archived once
// the broker has closed its tab — not in the pass that closes it, which only
// asks Codex to exit, but by the beat's recovery half a minute later — and
// exactly once. The record says when.
func TestAScheduledCodexRunIsArchivedAfterItsTabCloses(t *testing.T) {
	b, ctx := newTestBroker(t)
	// The outbox stamps an effect with the wall clock, so this one starts there.
	now := time.Now()
	b.Clock = func() time.Time { return now }
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	calls := &archiveCalls{}
	b.ArchiveConversation = calls.archive
	id := "c6900001-0000-4000-8000-000000000001"
	settledScheduledCodexRun(t, b, ctx, id, true)

	// A broker that has been watching for a while, so the close is made now
	// and the effect's wall-clock stamp is this test's now.
	b.lingerStarted = now.Add(-time.Hour)
	if n := b.closeLingers(ctx, idleCodexTab()); n != 1 || len(launcher.closed) != 1 {
		t.Fatalf("closed %d, launcher %v", n, launcher.closed)
	}
	if calls.count() != 0 {
		t.Fatal("the conversation was archived in the pass that closed its tab")
	}
	now = now.Add(10 * time.Second)
	b.RecoverEffects(ctx)
	if calls.count() != 0 {
		t.Fatal("the conversation was archived before Codex had had time to exit")
	}
	now = now.Add(25 * time.Second)
	b.RecoverEffects(ctx)
	if calls.count() != 1 || calls.ids[0] != "codex:"+archivedConversation {
		t.Fatalf("archive calls: %v", calls.ids)
	}
	r, _, err := b.Record(ctx, id)
	if err != nil || r.ChildConversationArchivedAt.IsZero() {
		t.Fatalf("the record does not say it was archived: %+v %v", r.ChildConversationArchivedAt, err)
	}
	now = now.Add(time.Minute)
	b.RecoverEffects(ctx)
	if calls.count() != 1 {
		t.Fatalf("archived again: %v", calls.ids)
	}
}

// A run whose schedule does not ask is never archived; a tab a person closed
// first is archived as one the broker closed.
func TestOnlyAnAskingRunIsArchivedAndAGoneTabCounts(t *testing.T) {
	b, ctx := newTestBroker(t)
	// The outbox stamps an effect with the wall clock, so this one starts there.
	now := time.Now()
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	calls := &archiveCalls{}
	b.ArchiveConversation = calls.archive
	settledScheduledCodexRun(t, b, ctx, "c6900002-0000-4000-8000-000000000002", false)
	b.closeLingers(ctx, idleCodexTab())
	now = now.Add(21 * time.Second)
	b.closeLingers(ctx, idleCodexTab())
	now = now.Add(time.Minute)
	b.RecoverEffects(ctx)
	if calls.count() != 0 {
		t.Fatalf("a run that did not ask was archived: %v", calls.ids)
	}

	settledScheduledCodexRun(t, b, ctx, "c6900003-0000-4000-8000-000000000003", true)
	gone := reading{sessions: map[string]session.Session{"%99": {ID: "%99", Assistant: session.AssistantClaude}},
		sources: map[string]bool{"tmux": true}, complete: true}
	b.closeLingers(ctx, gone)
	now = now.Add(21 * time.Second)
	b.closeLingers(ctx, gone)
	now = now.Add(time.Minute)
	b.RecoverEffects(ctx)
	if calls.count() != 1 {
		t.Fatalf("a run whose tab was closed by somebody else: archive calls %v", calls.ids)
	}
}

// A tab still holding the conversation when the archive falls due — its
// close did not happen — is Codex at work, and the conversation stays.
func TestAConversationWhoseTabIsStillOpenIsNotArchived(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Now()
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	calls := &archiveCalls{}
	b.ArchiveConversation = calls.archive
	id := "c6900004-0000-4000-8000-000000000004"
	settledScheduledCodexRun(t, b, ctx, id, true)
	b.lingerStarted = now.Add(-time.Hour)
	b.closeLingers(ctx, idleCodexTab())
	still := idleCodexTab()
	b.Live = func(context.Context) []session.Session { return []session.Session{still.sessions["%41"]} }
	now = now.Add(time.Minute)
	b.RecoverEffects(ctx)
	if calls.count() != 0 {
		t.Fatalf("archived a conversation whose tab is open: %v", calls.ids)
	}
	if body, ok := latestEvent(t, b, ctx, "task.child.conversation.archive_failed", id); !ok || body["error"] != "tab_open" {
		t.Fatalf("no refusal recorded: %v", body)
	}
}
