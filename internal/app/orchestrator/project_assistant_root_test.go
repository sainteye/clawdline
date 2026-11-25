package orchestrator

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func assistantRootFixture(t *testing.T) (*Broker, context.Context, pa.WorkRequest, *int) {
	t.Helper()
	b, ctx, _, q, _ := assistantBrokerFixture(t)
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	q.Title = "Integrate the reviewed branches"
	q.Brief = "Merge the reviewed branches into main and push."
	q.Claims = []string{}
	q.Choice.Mode = pa.DispatchModeRoot
	return b, ctx, q, &typed
}

// A Bot's Root is an ordinary Root Assignment: opened once across a retry,
// recorded with the Bot that opened it, and briefed with nothing that makes
// it anybody's child.
func TestBotOpensARootOnceAcrossARetry(t *testing.T) {
	b, ctx, q, typed := assistantRootFixture(t)
	first, replayed, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || replayed {
		t.Fatalf("first open = %+v replayed=%v err=%v", first, replayed, err)
	}
	again, replayed, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("retry = %s replayed=%v err=%v, want %s replayed", again.ID, replayed, err, first.ID)
	}
	if *typed != 1 {
		t.Fatalf("briefing typed %d times, want once", *typed)
	}
	rows, _, err := b.Records(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a Root created child task records: %v %+v", err, rows)
	}
	if first.Ownership != "independent_root" || first.BotOwner == nil || first.BotOwner.Fence != q.Actor.Fence || first.BotOwner.TurnID != "turn" || first.BotOwner.Command != q.Command {
		t.Fatalf("owner record = %+v %+v", first.Ownership, first.BotOwner)
	}
	brief, err := os.ReadFile(first.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENED BY A BOT", "not anybody's child", "clawdline session report", q.Brief} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}
	for _, child := range []string{"TASK_SECRET", "result.json", "Do not push", "task accept"} {
		if strings.Contains(string(brief), child) {
			t.Errorf("brief carries the child restriction %q", child)
		}
	}
	// The same key with different content is a conflict, never a second Root.
	changed := q
	changed.Brief = "Something else entirely."
	if _, _, err := b.DispatchAssistantRoot(ctx, changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("changed content = %v, want idempotency conflict", err)
	}
}

func TestBotRootRefusesCompletionTurnsAndPlainDispatchRefusesRootMode(t *testing.T) {
	b, ctx, q, typed := assistantRootFixture(t)
	withTurn := q
	withTurn.ContinueOnCompletion = true
	b.AssistantWorkCompleted = func(context.Context, AssistantWorkSnapshot) error { return nil }
	if _, _, err := b.DispatchAssistantRoot(ctx, withTurn); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("Root with a completion turn = %v, want scope denied", err)
	}
	if _, err := b.DispatchAssistant(ctx, q); pa.Code(err) != pa.Invalid {
		t.Fatalf("child door with mode root = %v, want invalid", err)
	}
	plain := q
	plain.Choice.Mode = ""
	if _, _, err := b.DispatchAssistantRoot(ctx, plain); pa.Code(err) != pa.Invalid {
		t.Fatalf("Root door without mode root = %v, want invalid", err)
	}
	if *typed != 0 {
		t.Fatalf("a refused request typed %d times", *typed)
	}
}

// The Root's first delivery report tells the opening Bot once; a second
// report, and a recovery pass, do not tell it again.
func TestBotIsToldOnceWhenItsRootReportsDelivery(t *testing.T) {
	b, ctx, q, _ := assistantRootFixture(t)
	now := time.Now().UTC()
	b.Clock = func() time.Time { return now }
	var told []AssistantRootDelivery
	b.AssistantRootDelivered = func(_ context.Context, d AssistantRootDelivery) error {
		told = append(told, d)
		return nil
	}
	a, _, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || a.Executor == nil {
		t.Fatalf("open = %+v %v", a, err)
	}
	row := session.Session{ID: a.Executor.TerminalID, Assistant: session.AssistantCodex, ConversationID: rootConversation,
		State: session.StateIdle, Evidence: session.EvidenceRegistry,
		Observation: session.Observation{ObservedAt: now, Freshness: session.FreshnessCurrent}}
	b.Live = func(context.Context) []session.Session { return []session.Session{row} }
	for _, summary := range []string{"Merged and pushed.", "Pushed again."} {
		if _, err := b.ReportSessionDelivery(ctx, a.Executor.TerminalID, summary); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10 * time.Minute)
		b.RecoverEffects(ctx)
	}
	b.RecoverEffects(ctx)
	if len(told) != 1 {
		t.Fatalf("Bot told %d times, want once: %+v", len(told), told)
	}
	d := told[0]
	if d.AssignmentID != a.ID || d.Summary != "Merged and pushed." || d.Owner.Fence != q.Actor.Fence || d.Title != q.Title {
		t.Fatalf("delivery = %+v", d)
	}
	stored, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil || stored.BotOwner == nil || stored.BotOwner.DeliveredAt == 0 {
		t.Fatalf("delivery receipt = %+v %v", stored.BotOwner, err)
	}
	link := AssistantRootLink(stored)
	if link.ID != a.ID || link.Fence != q.Actor.Fence || link.TurnID != "turn" || link.WorkerTerminal != a.Executor.TerminalID {
		t.Fatalf("link = %+v", link)
	}
}
