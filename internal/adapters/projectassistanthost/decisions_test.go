package projectassistanthost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
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
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
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
	unrelated, err := f.send(t, b, "unrelated topic")
	if err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)
	fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "Here is the unrelated answer."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, b.Key, unrelated.ResourceID)
		return err == nil && turn.State == pa.TurnSucceeded
	})

	query := pa.ReadViewRequest{Key: b.Key, ConversationID: b.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 1}
	snapshot, err := service.View(ctx, query)
	if err != nil || snapshot.View.Head.CurrentFence == offer.Fence || snapshot.View.Head.PendingPermissionCount != 1 {
		t.Fatalf("the open decision did not survive the recipient's new binding: %+v, %v", snapshot.View.Head, err)
	}
	query.Mode, query.SourceEpoch = pa.ReadViewPermissions, snapshot.View.SourceEpoch
	collection, err := service.View(ctx, query)
	if err != nil || len(collection.View.Items) != 1 || collection.View.Items[0].PermissionSummary == nil {
		t.Fatalf("the open decision is missing from the recipient chat: %+v, %v", collection.View.Items, err)
	}
	query.Mode, query.Cursor = pa.ReadViewDetail, collection.View.Items[0].PermissionSummary.DetailCursor
	detail, err := service.View(ctx, query)
	if err != nil || len(detail.View.Items) != 1 || detail.View.Items[0].Permission == nil {
		t.Fatalf("the original decision could not be opened: %+v, %v", detail.View.Items, err)
	}
	card := detail.View.Items[0].Permission
	if card.ID != offer.ID || card.Fence != snapshot.View.Head.CurrentFence {
		t.Fatalf("the card lost its identity or the current chat fence: %+v", card)
	}
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "decide"}, pa.PermissionAnswer{Fence: card.Fence, OfferID: card.ID, OptionID: "reply-1"}); err != nil {
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
	if got, err := f.h.store.Export(ctx, b.Key); err != nil || !answerMarked(got.Messages, offer.ID, "Yes, roll it out") {
		t.Fatalf("the answer message does not name the card it answered: %v", err)
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

// longAnswer is a typed reply of about 700 Chinese characters, longer than
// the question bound that used to refuse it.
var longAnswer = strings.Repeat("我同意，但請先在測試環境跑過一次。", 45)

func TestPeerQuestionRefusesExpiredHiddenAndOverflowingOffers(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	_, b, _, offer := startPeerQuestion(t, f)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	answer := func(key string) error {
		_, e := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: key}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, Text: longAnswer})
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
	if err = pa.CheckDecision(offer, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}, bot, offer.ExpiresAt.Add(-1)); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a hidden Bot's offer stayed answerable: %v", err)
	}
	bot.Hidden = false
	if err = pa.CheckDecision(offer, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}, bot, offer.ExpiresAt); pa.Code(err) != pa.Expired {
		t.Fatalf("an expired offer stayed answerable: %v", err)
	}
	if err = answer("own"); err != nil {
		t.Fatalf("the person's own reply was refused: %v", err)
	}
	fixtureReceive(t, f.p.sent)
	if got := decisionOffers(t, f, b.Key); len(got) != 1 || got[0].Decision.AnswerText != longAnswer {
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

// A login or quota fallback revokes the chat's native binding while the card
// stays pending; the person's press is still their message to the chat.
func TestDecisionPressSurvivesARevokedBindingAndReplaysItsReceipt(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	_, b, _, offer := startPeerQuestion(t, f)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := f.send(t, b, "unrelated topic")
	if err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)
	fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "Here is the unrelated answer."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, b.Key, unrelated.ResourceID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	x, err := f.h.store.Export(ctx, b.Key)
	if err != nil || len(x.Bindings) == 0 {
		t.Fatalf("no binding to revoke: %+v, %v", x.Bindings, err)
	}
	conv, err := f.h.store.Conversation(ctx, b.Key, b.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	var binding pa.ExecutionBinding
	for _, candidate := range x.Bindings {
		if candidate.Fence.BindingID == conv.BindingID {
			binding = candidate
		}
	}
	if binding.Fence.BindingID == "" {
		t.Fatalf("the chat's binding %q is not exported", conv.BindingID)
	}
	binding.Revoked = true
	if _, err = f.h.store.Commit(ctx, app.Mutation{Command: projectionCommand(b.Key, "revoke"), ExpectedVersion: x.Bot.Version, Binding: &binding, Receipt: pa.Receipt{ID: "revoke", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	press := pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-1"}
	first, err := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "press"}, press)
	if err != nil {
		t.Fatalf("a press after a fallback revoked the binding = %v", err)
	}
	fixtureReceive(t, f.p.sent)
	again, err := service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "press-retry"}, press)
	if err != nil || again.ID != first.ID {
		t.Fatalf("the retried press = %+v, %v; want receipt %q", again, err, first.ID)
	}
	press.OptionID = "reply-2"
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "press-other"}, press); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a second, different answer = %v", err)
	}
}

func answerMarked(messages []pa.Message, offerID, answer string) bool {
	for _, m := range messages {
		if m.Kind == pa.UserMessage && m.DecisionReply != nil {
			d := m.DecisionReply
			return d.OfferID == offerID && d.Answer == answer && d.Question == "Authorize the dev rollout now?" && strings.Contains(m.Text, answer)
		}
	}
	return false
}

type questionTools interface {
	ListQuestions(context.Context) ([]pa.PendingQuestion, error)
	WithdrawQuestion(context.Context, pa.WithdrawQuestionRequest) (pa.PermissionOffer, error)
	AskPerson(context.Context, pa.AskPersonRequest) (pa.PermissionOffer, error)
}

// A Bot reviewing a newer request withdraws the question a peer was waiting
// on: the card closes with its reason, the waiting exchange completes and
// the requester is told there was no decision.
func TestBotWithdrawsAQuestionAPeerWaitedOn(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	a, b, x, offer := startPeerQuestion(t, f)
	if _, err := f.send(t, b, "A newer request supersedes the rollout."); err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)
	main := fixtureReceive(t, f.p.sessions)
	tools, ok := main.q.Tools.(questionTools)
	if !ok {
		t.Fatal("the Bot's turn has no question tools")
	}
	listed, err := tools.ListQuestions(ctx)
	if err != nil || len(listed) != 1 || listed[0].QuestionID != offer.ID || listed[0].FromBotID != a.Key.BotID || len(listed[0].SuggestedReplies) != 2 {
		t.Fatalf("list_questions = %+v, %v", listed, err)
	}
	if _, err = tools.WithdrawQuestion(ctx, pa.WithdrawQuestionRequest{QuestionID: offer.ID}); pa.Code(err) != pa.Invalid {
		t.Fatalf("a withdrawal without a reason was accepted: %v", err)
	}
	if _, err = tools.WithdrawQuestion(ctx, pa.WithdrawQuestionRequest{QuestionID: "decision-missing", Reason: "gone"}); pa.Code(err) != pa.NotFound {
		t.Fatalf("an unknown question was withdrawn: %v", err)
	}
	got, err := tools.WithdrawQuestion(ctx, pa.WithdrawQuestionRequest{QuestionID: offer.ID, Reason: "Superseded by the newer request."})
	if err != nil || got.State != pa.PermissionCancelled {
		t.Fatalf("withdraw = %+v, %v", got, err)
	}
	if cards := decisionOffers(t, f, b.Key); len(cards) != 1 || cards[0].State != pa.PermissionCancelled || cards[0].Decision.WithdrawnReason != "Superseded by the newer request." {
		t.Fatalf("the card did not close with its reason: %+v", cards)
	}
	if _, err = tools.WithdrawQuestion(ctx, pa.WithdrawQuestionRequest{QuestionID: offer.ID, Reason: "again"}); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a closed question was withdrawn twice: %v", err)
	}
	if listed, err = tools.ListQuestions(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("a withdrawn question is still listed: %+v, %v", listed, err)
	}
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Answer(ctx, pa.Command{Key: b.Key, Operation: pa.AnswerOperation, IdempotencyKey: "late"}, pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-1"}); err == nil {
		t.Fatal("a withdrawn question was still answerable")
	}
	done, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil || done.Outcome != pa.ExchangeCompleted || !strings.Contains(done.Reply, "Superseded by the newer request.") || done.Delivery != "queued" {
		t.Fatalf("the waiting exchange did not complete with the withdrawal: %+v, %v", done, err)
	}
	main.reply <- app.PromptResult{FinalText: "Handled the newer request."}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin || !strings.Contains(reply.Text, "withdrawn") {
		t.Fatalf("the requester was not told the question was withdrawn: %+v %q", reply.Turn, reply.Text)
	}
	follow.reply <- app.PromptResult{FinalText: "Noted."}
}

// A memory card is the host's prompt, not the Bot's question: it cannot be
// withdrawn and is not listed.
func TestBotCannotWithdrawAMemoryCard(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "keeper")
	if _, err := f.send(t, b, "Remember this."); err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	main := fixtureReceive(t, f.p.sessions)
	memory, err := f.h.store.OfferDecision(ctx, main.q.Binding.Fence, prompt.Turn.ID, pa.AskPersonRequest{Question: pa.MemoryProposalQuestion, SuggestedReplies: []string{"Save", "Don't save"}, Memory: "The rollout window is Tuesday."}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tools := main.q.Tools.(questionTools)
	if listed, err := tools.ListQuestions(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("a memory card was listed as a question: %+v, %v", listed, err)
	}
	if _, err = tools.WithdrawQuestion(ctx, pa.WithdrawQuestionRequest{QuestionID: memory.ID, Reason: "no"}); pa.Code(err) != pa.NotFound {
		t.Fatalf("a memory card was withdrawn: %v", err)
	}
	main.reply <- app.PromptResult{FinalText: "ok"}
}
