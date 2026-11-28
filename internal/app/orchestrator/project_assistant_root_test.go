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

func TestBotRootRecordsAppearanceWithoutBreakingRetryDigest(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	q.Title = "Clean finished task caches"
	q.Brief = "Remove stale build caches."
	q.Claims = []string{}
	q.Choice.Mode = pa.DispatchModeRoot
	initial := pa.Appearance{Shape: pa.Hex, Color: "#75c6a5"}
	c.BotName = "Test dispatcher"
	c.BotAppearance = initial

	first, replayed, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || replayed {
		t.Fatalf("first open = %+v replayed=%v err=%v", first, replayed, err)
	}
	if first.BotOwner == nil || first.BotOwner.BotAppearance != initial {
		t.Fatalf("recorded appearance = %+v, want %+v", first.BotOwner, initial)
	}
	c.BotAppearance = pa.Appearance{Shape: pa.Wedge, Color: "#7eafff"}
	again, replayed, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || !replayed || again.ID != first.ID || again.BotOwner == nil || again.BotOwner.BotAppearance != initial {
		t.Fatalf("retry after appearance change = %+v replayed=%v err=%v, want replay of %s with %+v", again, replayed, err, first.ID, initial)
	}
	if typed != 1 {
		t.Fatalf("briefing typed %d times, want once", typed)
	}
}

func TestBotIsToldWhenItsRootClosesWithoutDeliveryReport(t *testing.T) {
	b, ctx, q, _ := assistantRootFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	b.Clock = func() time.Time { return now }
	var closed []AssistantRootClosure
	b.AssistantRootClosed = func(_ context.Context, c AssistantRootClosure) error {
		closed = append(closed, c)
		return nil
	}
	a, _, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || a.Executor == nil {
		t.Fatalf("open = %+v %v", a, err)
	}
	now = now.Add(5 * time.Minute)
	sess := session.Session{ID: a.Executor.TerminalID, Backend: session.Backend(a.Executor.Backend),
		Assistant: session.Assistant(a.Assistant), ConversationID: rootConversation}
	if err := b.RecordRootClosure(ctx, a.ID, sess, "close", false); err != nil {
		t.Fatal(err)
	}
	b.RecoverEffects(ctx)
	b.RecoverEffects(ctx)
	if len(closed) != 1 {
		t.Fatalf("Bot closure notified %d times, want once: %+v", len(closed), closed)
	}
	if closed[0].AssignmentID != a.ID || closed[0].TerminalID != a.Executor.TerminalID || !closed[0].Safe || closed[0].Forced || !closed[0].ClosedAt.Equal(now) {
		t.Fatalf("closure notice = %+v", closed[0])
	}
	stored, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	link := AssistantRootLink(stored)
	if link.State != pa.WorkUnknown || !link.FinishedAt.Equal(now) {
		t.Fatalf("closed Root link = %+v, want unknown finished at %v", link, now)
	}
}

func TestBotDeliveredRootDoesNotSendClosureAfterDelivery(t *testing.T) {
	b, ctx, q, _ := assistantRootFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	b.Clock = func() time.Time { return now }
	b.AssistantRootDelivered = func(context.Context, AssistantRootDelivery) error { return nil }
	var closed []AssistantRootClosure
	b.AssistantRootClosed = func(_ context.Context, c AssistantRootClosure) error {
		closed = append(closed, c)
		return nil
	}
	a, _, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || a.Executor == nil {
		t.Fatalf("open = %+v %v", a, err)
	}
	row := session.Session{ID: a.Executor.TerminalID, Backend: session.Backend(a.Executor.Backend),
		Assistant: session.Assistant(a.Assistant), ConversationID: rootConversation,
		State: session.StateIdle, Evidence: session.EvidenceRegistry,
		Observation: session.Observation{ObservedAt: now, Freshness: session.FreshnessCurrent}}
	b.Live = func(context.Context) []session.Session { return []session.Session{row} }
	if _, err := b.ReportSessionDelivery(ctx, a.Executor.TerminalID, "Done."); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	b.RecoverEffects(ctx)
	if err := b.RecordRootClosure(ctx, a.ID, row, "close", false); err != nil {
		t.Fatal(err)
	}
	b.RecoverEffects(ctx)
	if len(closed) != 0 {
		t.Fatalf("delivered Root sent closure notice: %+v", closed)
	}
	stored, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	link := AssistantRootLink(stored)
	if link.State != pa.WorkCompleted || link.ResultMessageID != "work-result-"+a.ID || link.FinishedAt.IsZero() {
		t.Fatalf("delivered then closed link = %+v, want completed with result id and finished_at", link)
	}
}

func TestBotRootAwaitingDialogFailureNotifiesBotImmediately(t *testing.T) {
	b, ctx, q, _ := assistantRootFixture(t)
	b.Screen = func(context.Context, string) (string, bool) {
		return screen(t, "codex-update"), true
	}
	var closed []AssistantRootClosure
	b.AssistantRootClosed = func(_ context.Context, c AssistantRootClosure) error {
		closed = append(closed, c)
		return nil
	}
	a, _, err := b.DispatchAssistantRoot(ctx, q)
	if err != nil || a.State != AssignmentAwaitingDialog || a.Executor == nil {
		t.Fatalf("open awaiting dialog = %+v %v", a, err)
	}
	if link := AssistantRootLink(a); link.State != pa.WorkWaitingForPerson {
		t.Fatalf("awaiting dialog link = %+v, want waiting_for_person", link)
	}
	// The terminal disappears with a complete reading -> b.Pass fails it and fires AssistantRootClosed immediately.
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{
			Sessions: []session.Session{{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude}},
			Complete: true,
			Sources:  map[string]bool{a.Executor.Backend: true},
		}
	}
	b.Pass(ctx)
	out, err := b.RootAssignmentByID(ctx, a.ID)
	if err != nil || out.State != AssignmentFailed {
		t.Fatalf("after Pass = %+v %v, want failed", out, err)
	}
	if len(closed) != 1 || closed[0].AssignmentID != a.ID {
		t.Fatalf("closure notifications = %+v, want 1 for %s", closed, a.ID)
	}
	if link := AssistantRootLink(out); link.State != pa.WorkFailed || link.FailureReason != rootDialogClosedFailure {
		t.Fatalf("failed dialog link = %+v, want failed with reason %q", link, rootDialogClosedFailure)
	}
}
