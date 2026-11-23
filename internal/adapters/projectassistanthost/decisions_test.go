package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type askingTools interface {
	MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	AskPerson(context.Context, pa.AskPersonRequest) (pa.PermissionOffer, error)
}

func decisionOffers(t *testing.T, f *hostFixture, key pa.BotKey) []pa.PermissionOffer {
	t.Helper()
	got, err := f.h.store.Export(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	var out []pa.PermissionOffer
	for _, p := range got.Permissions {
		if p.Kind == pa.DecisionOfferKind {
			out = append(out, p)
		}
	}
	return out
}

// startPeerQuestion runs Bot A's message_bot to Bot B's hidden worker and
// has the worker ask the person; it returns the waiting exchange and offer.
func startPeerQuestion(t *testing.T, f *hostFixture) (a, b pa.Bot, x pa.Exchange, offer pa.PermissionOffer) {
	t.Helper()
	ctx := context.Background()
	a, b = f.create(t, "requester"), f.create(t, "recipient")
	if _, err := f.send(t, a, "Ask the peer."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := sender.q.Tools.(askingTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Roll out the synthetic dev build.", IdempotencyKey: "rollout"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("exchange = %+v, %v", x, err)
	}
	sender.reply <- app.PromptResult{FinalText: "I asked the peer."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, a.Key, x.SourceTurnID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	if peer := fixtureReceive(t, f.p.sent); peer.Turn.Origin != pa.BotRequestOrigin {
		t.Fatalf("unexpected worker prompt: %+v", peer.Turn)
	}
	tools, ok := worker.q.Tools.(askingTools)
	if !ok {
		t.Fatal("the peer-request worker has no ask_person tool")
	}
	offer, err = tools.AskPerson(ctx, pa.AskPersonRequest{Question: "Authorize the dev rollout now?", SuggestedReplies: []string{"Yes, roll it out", "Not now"}})
	if err != nil {
		t.Fatal(err)
	}
	if offer.Fence.Key != b.Key || offer.Decision == nil || offer.Decision.ExchangeID != x.ID {
		t.Fatalf("offer was not placed in the recipient's main chat: %+v", offer)
	}
	if hidden := decisionOffers(t, f, x.Recipient().Key); len(hidden) != 0 {
		t.Fatalf("offer landed in the hidden worker: %+v", hidden)
	}
	worker.reply <- app.PromptResult{FinalText: "Waiting for the person to authorize the rollout."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Exchange(ctx, a.Key, x.ID)
		return err == nil && got.Outcome == pa.ExchangeNeedsPerson
	})
	return a, b, x, offer
}

func TestPeerQuestionWaitsForThePersonAndForwardsTheAnswer(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.start(t)
	f.enable(t)
	a, b, x, offer := startPeerQuestion(t, f)
	waiting, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil || waiting.Delivery != "" || waiting.ReplyTurnID != "" {
		t.Fatalf("the requester was answered before the person decided: %+v, %v", waiting, err)
	}
	pending := decisionOffers(t, f, b.Key)
	if len(pending) != 1 || pending[0].State != pa.PermissionPending {
		t.Fatalf("recipient chat has no pending decision: %+v", pending)
	}
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "decide"}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-1"}); err != nil {
		t.Fatal(err)
	}
	main := fixtureReceive(t, f.p.sessions)
	prompt := fixtureReceive(t, f.p.sent)
	if main.q.Bot.Key != b.Key || prompt.Turn.Origin != pa.UserOrigin || !pa.DirectUserTurn(prompt.Turn) || !directUserTurn(prompt.Turn) || prompt.Turn.ExchangeID != "" || prompt.Turn.Hops != 0 || prompt.Turn.DecisionExchangeID != x.ID ||
		!strings.Contains(prompt.Text, "Yes, roll it out") || !strings.Contains(prompt.Text, "Roll out the synthetic dev build.") {
		t.Fatalf("the answer was not the person's turn in the recipient's main chat: %+v %q", prompt.Turn, prompt.Text)
	}
	if got := decisionOffers(t, f, b.Key); len(got) != 1 || got[0].State != pa.PermissionResolved || got[0].Decision.AnswerText != "Yes, roll it out" {
		t.Fatalf("offer was not resolved with the answer: %+v", got)
	}
	// Single use: a second, different answer is refused.
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "decide-again"}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-2"}); err == nil {
		t.Fatal("a decision offer was answered twice")
	}
	main.reply <- app.PromptResult{FinalText: "Rollout authorized and started."}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin || !strings.Contains(reply.Text, "Rollout authorized and started.") {
		t.Fatalf("the decision was not forwarded to the requester: %+v %q", reply.Turn, reply.Text)
	}
	follow.reply <- app.PromptResult{FinalText: "The rollout is underway."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Exchange(ctx, a.Key, x.ID)
		return err == nil && got.Outcome == pa.ExchangeCompleted && got.Delivery == "delivered"
	})
}

func TestPeerQuestionRefusesExpiredHiddenAndOverflowingOffers(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.start(t)
	f.enable(t)
	_, b, _, offer := startPeerQuestion(t, f)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	answer := func(key string) error {
		_, e := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: key}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, Text: "My own answer"})
		return e
	}
	// A Bot's own key cannot answer another Bot's offer.
	if _, err = service.Answer(ctx, pa.Command{Key: f.create(t, "other").Key, Operation: pa.AnswerOperation, IdempotencyKey: "cross"}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, Text: "x"}); err == nil {
		t.Fatal("an answer from another chat was accepted")
	}
	bot, err := f.h.store.Bot(ctx, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	bot.Hidden = true
	if err = pa.CheckDecision(offer, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}, bot, false, offer.ExpiresAt.Add(-1)); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a hidden Bot's offer stayed answerable: %v", err)
	}
	bot.Hidden = false
	if err = pa.CheckDecision(offer, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}, bot, true, offer.ExpiresAt.Add(-1)); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a revoked binding's offer stayed answerable: %v", err)
	}
	if err = pa.CheckDecision(offer, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}, bot, false, offer.ExpiresAt); pa.Code(err) != pa.Expired {
		t.Fatalf("an expired offer stayed answerable: %v", err)
	}
	if err = answer("own"); err != nil {
		t.Fatalf("the person's own reply was refused: %v", err)
	}
	fixtureReceive(t, f.p.sent)
	if got := decisionOffers(t, f, b.Key); len(got) != 1 || got[0].Decision.AnswerText != "My own answer" {
		t.Fatalf("own reply not recorded: %+v", got)
	}
	if err = pa.CheckAskPerson(pa.AskPersonRequest{Question: "q", SuggestedReplies: []string{"1", "2", "3", "4"}}); pa.Code(err) != pa.Invalid {
		t.Fatalf("more than three suggested replies were accepted: %v", err)
	}
}

func TestOrdinaryTurnQuestionsAreBounded(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "solo")
	if _, err := f.send(t, bot, "Help me decide."); err != nil {
		t.Fatal(err)
	}
	session := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	tools := session.q.Tools.(askingTools)
	for i := 0; i < pa.MaxDecisionOffersPerBot; i++ {
		if _, err := tools.AskPerson(ctx, pa.AskPersonRequest{Question: "Question " + string(rune('A'+i)), SuggestedReplies: []string{"Yes"}}); err != nil {
			t.Fatalf("question %d: %v", i, err)
		}
	}
	if _, err := tools.AskPerson(ctx, pa.AskPersonRequest{Question: "One too many", SuggestedReplies: []string{"Yes"}}); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("the per-Bot question bound did not refuse: %v", err)
	}
	session.reply <- app.PromptResult{FinalText: "Asked."}
	fixtureEventually(t, func() bool {
		got := decisionOffers(t, f, bot.Key)
		pending := 0
		for _, p := range got {
			if p.State == pa.PermissionPending {
				pending++
			}
		}
		return pending == pa.MaxDecisionOffersPerBot
	})
}
