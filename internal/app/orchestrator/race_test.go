package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Failure injection for the lost-update class: every one of these is a slow
// step during which somebody else changes the record, and the question is
// whether the slow step's stale copy gets written back over the change.

const rootConversation = "379d443c-db38-4d17-8ce2-b273dc748ea0"

func newTestBroker(t *testing.T) (*Broker, context.Context) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	b := &Broker{
		Store: st,
		Tasks: taskdir.New(dir),
		Git:   git.New(),
		Dir:   dir,
		Live: func(context.Context) []session.Session {
			return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}}
		},
	}
	return b, context.Background()
}

// finished stores a task that has just finished and owes its root a notice.
func finished(t *testing.T, b *Broker, ctx context.Context, id string) Record {
	t.Helper()
	r := Record{
		Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{},
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"},
	}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

// The ACK arrives while the pump is typing the notice. Before mutate, the pump
// saved its pre-typing copy afterwards — `delivered`, with the next retry armed —
// over the acknowledgement, and the root was told again and again about work it
// had already said it saw.
func TestAnAckThatLandsMidTypingIsNotUndone(t *testing.T) {
	b, ctx := newTestBroker(t)
	r := finished(t, b, ctx, "11111111-1111-4111-8111-111111111111")
	b.Type = func(ctx context.Context, terminal, text string) error {
		if _, err := b.Acknowledge(ctx, r.ID, r.Notice.ID); err != nil {
			t.Fatalf("the ack itself failed: %v", err)
		}
		return nil
	}
	b.PumpNotices(ctx)
	after, _, err := b.Record(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Notice.State != NoticeAcknowledged {
		t.Fatalf("notice is %q after an ack that landed mid-typing; the attempt was written over it", after.Notice.State)
	}
	if !after.Notice.NextRetryAt.IsZero() {
		t.Error("an acknowledged notice is still armed for another attempt")
	}
}

// `/complete` and the beat's collection of result.json both settle one task.
// Before Settle was a precondition inside mutate they both succeeded and the
// second minted a new notice id, orphaning the one the root had been given.
func TestATaskIsSettledOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	first := finished(t, b, ctx, "22222222-2222-4222-8222-222222222222")
	_, err := b.Settle(ctx, first.ID, StateFailure, "late", nil)
	if !errors.Is(err, errAlreadyTerminal) {
		t.Fatalf("a second settle was accepted (err=%v)", err)
	}
	after, _, _ := b.Record(ctx, first.ID)
	if after.State != StateSuccess {
		t.Errorf("state moved from success to %q", after.State)
	}
	if after.Notice.ID != first.Notice.ID {
		t.Error("the notice id changed; the root holds an id that no longer acknowledges anything")
	}
}

// A progress note proves `briefed` while the dispatch that spawned the child is
// still typing its briefing. The dispatch's own record, taken before the note,
// says `spawning`; writing it back whole would erase the proof.
func TestAProvenBriefingSurvivesTheDispatchThatWasStillTyping(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "33333333-3333-4333-8333-333333333333"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateQueued,
		CreatedAt: time.Now(), Claims: []string{}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Progress(ctx, id, "s", "read the brief, starting"); err != nil {
		t.Fatal(err)
	}
	// What Dispatch now does with what the spawn learned.
	after, err := b.mutate(ctx, id, "task.spawned", func(now *Record) error {
		now.ChildTerminalID = "%9"
		if now.State == StateQueued {
			now.State = StateSpawning
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if after.State != StateBriefed {
		t.Errorf("state is %q; the spawn's stale copy was written over a proven briefing", after.State)
	}
	if after.ChildTerminalID != "%9" {
		t.Error("what the spawn learned was lost")
	}
}
