package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A request worker starts with no memory of its own. With shared memory on
// the machine, its fresh session must still open and carry that memory,
// instead of ending as pa_context_stale with field execution.
func TestBotRequestWorkerWithOnlySharedMemoryFinishes(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.SharedMemory = func(context.Context, pa.Bot) string { return "- Shared synthetic fact.\n" }
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "requester"), f.create(t, "recipient")
	if _, err := f.send(t, a, "Ask the peer."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	opening := func(q app.PromptRequest) string {
		t.Helper()
		if q.Handoff == nil {
			t.Fatalf("fresh session %s has no opening", q.Turn.ID)
		}
		// The native adapter renders the opening from the delivered context.
		text, err := q.Handoff.Opening(q.Turn, q.Context)
		if err != nil {
			t.Fatalf("opening for %s refused: %v", q.Turn.ID, err)
		}
		return text
	}
	if text := opening(fixtureReceive(t, f.p.sent)); !strings.Contains(text, "Shared synthetic fact.") {
		t.Fatalf("the sender's opening lacks shared memory: %q", text)
	}
	tools := sender.q.Tools.(interface {
		MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	})
	x, err := tools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Check one synthetic fact.", IdempotencyKey: "check"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("request was not assigned to a background worker: %+v, %v", x, err)
	}
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	request := fixtureReceive(t, f.p.sent)
	if request.Turn.ID != x.RequestTurnID || request.Turn.Origin != pa.BotRequestOrigin {
		t.Fatalf("unexpected request prompt: %+v", request.Turn)
	}
	if text := opening(request); !strings.Contains(text, "Shared synthetic fact.") {
		t.Fatalf("the worker's opening lacks shared memory: %q", text)
	}
	worker.reply <- app.PromptResult{FinalText: "Synthetic fact checked."}
	sender.reply <- app.PromptResult{FinalText: "I asked the peer."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, x.Recipient().Key, x.RequestTurnID)
		return err == nil && turn.State != pa.TurnRunning
	})
	turn, err := f.h.store.Turn(ctx, x.Recipient().Key, x.RequestTurnID)
	got, _ := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil || turn.State != pa.TurnSucceeded || turn.Failure != nil || got.Outcome != pa.ExchangeCompleted {
		t.Fatalf("request turn did not finish: state=%s failure=%+v outcome=%s err=%v", turn.State, turn.Failure, got.Outcome, err)
	}
}
