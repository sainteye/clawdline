package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// 2026-11-24: a recipient's final answer streamed in several segments, and
// the requester received only the last status lines. The chat fallback is
// the last segment; the requester must get the whole answer.
const (
	splitAnswerLast = "➖ clawdline.com: no website change."
	splitAnswer     = "ANSWER-BODY nothing in this authorization needs doing.\n\n✅ GitHub: on origin/main.\n\n" + splitAnswerLast
)

func assertWholeAnswer(t *testing.T, where, got string) {
	t.Helper()
	if !strings.Contains(got, "ANSWER-BODY") || !strings.Contains(got, "✅ GitHub") || !strings.Contains(got, splitAnswerLast) {
		t.Fatalf("%s lost the answer body: %q", where, tail(got))
	}
}

func TestPeerReplyCarriesTheWholeAnswer(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "requester"), f.create(t, "recipient")
	if _, err := f.send(t, a, "Ask the peer."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := sender.q.Tools.(askingTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Check the card.", IdempotencyKey: "split-answer"})
	if err != nil {
		t.Fatal(err)
	}
	sender.reply <- app.PromptResult{FinalText: "I asked the peer."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, a.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	worker.reply <- app.PromptResult{FinalText: splitAnswerLast, AnswerText: splitAnswer}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin {
		t.Fatalf("reply was not delivered to the requester: %+v", reply.Turn)
	}
	assertWholeAnswer(t, "exchange prompt", reply.Text)
	stored, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertWholeAnswer(t, "stored exchange reply", stored.Reply)
	follow.reply <- app.PromptResult{FinalText: "Summarized."}
}

func TestForwardedDecisionCarriesTheWholeAnswer(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.start(t)
	f.enable(t)
	a, b, x, offer := startPeerQuestion(t, f)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "decide"}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-1"}); err != nil {
		t.Fatal(err)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	main.reply <- app.PromptResult{FinalText: splitAnswerLast, AnswerText: splitAnswer}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin {
		t.Fatalf("decision was not forwarded: %+v", reply.Turn)
	}
	assertWholeAnswer(t, "decision prompt", reply.Text)
	stored, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertWholeAnswer(t, "stored decision reply", stored.Reply)
	// The person's own chat keeps its fallback: the last segment only.
	messages, err := f.h.store.Export(ctx, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages.Messages {
		if m.Kind == pa.AssistantMessage && strings.Contains(m.Text, "ANSWER-BODY") {
			t.Fatalf("the chat fallback changed: %q", m.Text)
		}
	}
	follow.reply <- app.PromptResult{FinalText: "Summarized."}
}
