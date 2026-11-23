package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type typedLine struct{ terminal, text string }

// dispatchedWork dispatches one Bot task through the real door and returns it
// briefed and running in %80, with every later typed line recorded.
func dispatchedWork(t *testing.T) (*Broker, context.Context, pa.WorkRequest, Record, *[]typedLine) {
	t.Helper()
	b, ctx, current, q, _ := assistantBrokerFixture(t)
	current.BotAgent = "claude"
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.mutate(ctx, out.Record.ID, "task.accepted", func(r *Record) error {
		r.State = StateBriefed
		r.AssistantOwner.BotName = "Atlas"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChildTerminalID != "%80" {
		t.Fatalf("worker terminal %q", r.ChildTerminalID)
	}
	var typed []typedLine
	b.Type = func(_ context.Context, terminal, text string) error {
		typed = append(typed, typedLine{terminal, text})
		return nil
	}
	return b, ctx, q, r, &typed
}

func TestABotMessageReachesTheWorkSessionItDispatched(t *testing.T) {
	b, ctx, q, r, typed := dispatchedWork(t)
	got, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "Look at the backend first.", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != StageDelivered || got.AcceptedAt.IsZero() || got.At.IsZero() || got.Replayed {
		t.Fatalf("answer %+v; want delivered with both moments", got)
	}
	if len(*typed) != 1 || (*typed)[0].terminal != "%80" {
		t.Fatalf("typed %+v; want one line in the worker's terminal", *typed)
	}
	m, ok := transcript.SessionMessage((*typed)[0].text, 1)
	if !ok {
		t.Fatalf("not a session message envelope: %q", (*typed)[0].text)
	}
	if m.Source != "Atlas (dispatcher)" || m.SourceAssistant != "claude" || !strings.Contains(m.Text, "來自 Atlas（派工者）") ||
		!strings.Contains(m.Text, "not the person's authorization") || !strings.HasSuffix(m.Text, "Look at the backend first.") {
		t.Fatalf("the worker cannot tell who sent it or that it grants nothing: %+v", m)
	}

	// The same key is the first answer again and types nothing more.
	again, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "Look at the backend first.", "k1")
	if err != nil || !again.Replayed || again.Stage != StageDelivered {
		t.Fatalf("resend %+v %v", again, err)
	}
	if _, err = b.AssistantMessageWork(ctx, q.Actor, r.ID, "Something else.", "k1"); refusalCode(err) != "idempotency_key_reused" {
		t.Fatalf("the key with another text answered %v", err)
	}
	if len(*typed) != 1 {
		t.Fatalf("typed %d times for one key", len(*typed))
	}
}

func TestABotCannotMessageWorkItDoesNotOwnOrThatIsNotRunning(t *testing.T) {
	b, ctx, q, r, typed := dispatchedWork(t)

	if _, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, strings.Repeat("x", pa.MaxWorkMessageBytes+1), "big"); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("oversize answered %v", err)
	}

	if _, err := b.mutate(ctx, r.ID, "test.owner", func(r *Record) error {
		r.AssistantOwner.Fence.ConversationID = "another-chat"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "hi", "foreign"); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("another conversation's task answered %v", err)
	}
	if _, err := b.mutate(ctx, r.ID, "test.owner", func(r *Record) error {
		r.AssistantOwner.Fence.ConversationID = q.Actor.Fence.ConversationID
		r.State = StateQueued
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "hi", "early"); pa.Code(err) != pa.Offline {
		t.Fatalf("an unbriefed task answered %v", err)
	}
	if _, err := b.mutate(ctx, r.ID, "task.accepted", func(r *Record) error { r.State = StateBriefed; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, r.ID, StateCancelled, "cancelled", (*taskdir.Result)(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "hi", "late"); pa.Code(err) != pa.Expired {
		t.Fatalf("a finished task answered %v", err)
	}
	if len(*typed) != 0 {
		t.Fatalf("a refused message was typed: %+v", *typed)
	}
	// A refusal gives the key back: it was never used for anything.
	if _, err := b.AssistantMessageWork(ctx, q.Actor, r.ID, "hi", "early"); pa.Code(err) != pa.Expired {
		t.Fatalf("a released key answered %v", err)
	}
}
