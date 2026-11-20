package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestBackgroundWorkLetsChatContinueAndQuotesItsReport(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.o.Limits.PromptTimeout = 0
	f.o.Reader = f.o.Database
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.p.promptContexts = make(chan context.Context, pa.MaxAutomationBacklog)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "coordinator")
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.send(t, bot, "research"); err != nil {
		t.Fatal(err)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if _, bounded := fixtureReceive(t, f.p.promptContexts).Deadline(); bounded {
		t.Fatal("ordinary chat inherited a fixed deadline")
	}
	tools, ok := main.q.Tools.(app.BackgroundTools)
	if !ok || !tools.BackgroundAvailable() {
		t.Fatal("background tool is missing in the owner's main chat")
	}
	task, err := tools.AddBackgroundWork(ctx, "research", "Compare sources", "Look up the synthetic fixture facts.")
	if err != nil || task.OriginMessageID == "" {
		t.Fatalf("admission = %+v, %v", task, err)
	}
	repeated, err := tools.AddBackgroundWork(ctx, "research", "Compare sources", "Look up the synthetic fixture facts.")
	if err != nil || repeated.ID != task.ID {
		t.Fatalf("repeated admission = %+v, %v", repeated, err)
	}
	f.h.tickBackgroundTasks(ctx)
	if f.p.opens.Load() != 1 {
		t.Fatal("worker opened before the coordinating turn ended")
	}
	main.reply <- app.PromptResult{FinalText: "I handed this to background work."}
	fixtureEventually(t, func() bool {
		_, chat, _, err := service.Get(ctx, bot.Key)
		return err == nil && chat.ActiveTurnID == ""
	})
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	brief := fixtureReceive(t, f.p.sent)
	if _, bounded := fixtureReceive(t, f.p.promptContexts).Deadline(); bounded {
		t.Fatal("a Bot background thread inherited the main chat's prompt deadline")
	}
	if worker.q.Bot.Key == bot.Key || !strings.Contains(brief.Text, task.Brief) {
		t.Fatal("worker did not receive the assignment in its own session")
	}
	if tools, ok := worker.q.Tools.(app.BackgroundTools); !ok || tools.BackgroundAvailable() {
		t.Fatal("worker was able to delegate again")
	}
	if _, _, _, err := service.Get(ctx, worker.q.Bot.Key); pa.Code(err) != pa.NotFound {
		t.Fatalf("worker's management route was exposed: %v", err)
	}
	view, err := service.View(ctx, pa.ReadViewRequest{Key: bot.Key, ConversationID: bot.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 10})
	if err != nil || len(view.View.Head.BackgroundTasks) != 1 ||
		view.View.Head.BackgroundTasks[0].Worker == nil || *view.View.Head.BackgroundTasks[0].Worker != worker.q.Bot.Key {
		t.Fatalf("coordinator task list = %+v, %v", view.View.Head.BackgroundTasks, err)
	}
	if err = worker.sink(ctx, app.ProtocolEvent{
		ID: "synthetic-worker-trace", Fence: worker.q.Binding.Fence, TurnID: brief.Turn.ID,
		Trace: &pa.Trace{ID: "worker-trace", TurnID: brief.Turn.ID, Category: "tool_call", Text: "Read public records"},
	}); err != nil {
		t.Fatal(err)
	}
	view, err = service.View(ctx, pa.ReadViewRequest{Key: bot.Key, ConversationID: bot.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 10})
	if err != nil || len(view.View.Head.BackgroundTasks) != 1 || view.View.Head.BackgroundTasks[0].Activity != "Read public records" {
		t.Fatalf("live worker activity missing: %+v, %v", view.View.Head.BackgroundTasks, err)
	}
	if _, err = f.send(t, bot, "quick question"); err != nil {
		t.Fatal(err)
	}
	chatTurn := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if chatTurn.q.Bot.Key != bot.Key {
		t.Fatal("the main chat sent its next turn to a worker")
	}
	if _, bounded := fixtureReceive(t, f.p.promptContexts).Deadline(); bounded {
		t.Fatal("a follow-up in the main chat inherited a fixed deadline")
	}
	chatTurn.reply <- app.PromptResult{FinalText: "Quick answer."}
	worker.reply <- app.PromptResult{FinalText: "The source records agree."}

	fixtureEventually(t, func() bool {
		view, err := service.View(ctx, pa.ReadViewRequest{Key: bot.Key, ConversationID: bot.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 10})
		if err != nil || len(view.View.Head.BackgroundTasks) != 1 || view.View.Head.BackgroundTasks[0].State != "succeeded" {
			return false
		}
		for _, row := range view.View.Items {
			if row.Message != nil && row.Message.Text == "The source records agree." && row.Message.QuoteMessageID == task.OriginMessageID {
				return true
			}
		}
		return false
	})

	thread, err := service.View(ctx, pa.ReadViewRequest{Key: worker.q.Bot.Key, ConversationID: task.ID, Mode: pa.ReadViewSnapshot, PageSize: 10})
	if err != nil || thread.View.Head.Conversation.ID != task.ID {
		t.Fatalf("recent thread cannot be read: %v", err)
	}
	if _, err = service.Send(ctx, pa.Command{Key: worker.q.Bot.Key, Operation: pa.SendOperation, IdempotencyKey: "follow-up"}, pa.SendMessage{
		Fence: thread.View.Head.CurrentFence, Text: "Check the second record again.",
	}); err != nil {
		t.Fatal(err)
	}
	reopened := fixtureReceive(t, f.p.sessions)
	followUp := fixtureReceive(t, f.p.sent)
	if _, bounded := fixtureReceive(t, f.p.promptContexts).Deadline(); bounded {
		t.Fatal("a reopened background thread inherited the main chat's prompt deadline")
	}
	if reopened.q.Bot.Key != worker.q.Bot.Key || !strings.Contains(followUp.Text, "Check the second record again.") {
		t.Fatal("follow-up did not reopen the existing worker thread")
	}
	_, err = service.Stop(ctx, app.StopRequest{
		Command: pa.Command{Key: worker.q.Bot.Key, Operation: pa.StopOperation, IdempotencyKey: "stop-follow-up"},
		Fence:   reopened.q.Binding.Fence, TurnID: followUp.Turn.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixtureEventually(t, func() bool {
		view, err := service.View(ctx, pa.ReadViewRequest{Key: bot.Key, ConversationID: bot.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: 10})
		return err == nil && len(view.View.Head.BackgroundTasks) == 1 && view.View.Head.BackgroundTasks[0].State == "unknown"
	})
}

func TestCollaborationBackgroundRecipientCanAnswerItsOwnChat(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Limits.MaxBots, f.o.Limits.MaxProcesses = 4, 3
	f.start(t)
	f.enable(t)
	a, b := f.create(t, "requester"), f.create(t, "recipient")
	if _, err := f.send(t, a, "Ask the peer."); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if _, err := f.send(t, b, "Separate human question."); err != nil {
		t.Fatal(err)
	}
	foreground := fixtureReceive(t, f.p.sessions)
	own := fixtureReceive(t, f.p.sent)
	if !strings.Contains(own.Text, "Separate human question.") || foreground.q.Bot.Key != b.Key {
		t.Fatalf("unexpected foreground prompt: %q", own.Text)
	}
	tools, ok := sender.q.Tools.(interface {
		MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	})
	if !ok {
		t.Fatal("sender cannot start an exchange")
	}
	x, err := tools.MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Check one synthetic fact.", IdempotencyKey: "check"})
	if err != nil || x.RequestWorkerID == "" {
		t.Fatalf("recipient did not use a background Thread: %+v, %v", x, err)
	}
	sender.reply <- app.PromptResult{FinalText: "I asked the peer."}
	f.h.tickBackgroundTasks(ctx)
	worker := fixtureReceive(t, f.p.sessions)
	peer := fixtureReceive(t, f.p.sent)
	if worker.q.Bot.Key != x.Recipient().Key || peer.Turn.Origin != pa.BotRequestOrigin || !strings.Contains(peer.Text, x.Request) {
		got, _ := f.h.store.Exchange(ctx, a.Key, x.ID)
		private, _ := f.h.store.Export(ctx, x.Recipient().Key)
		t.Fatalf("work did not open with the recipient's private worker: origin=%s exchange=%+v worker=%+v", peer.Turn.Origin, got, private.Turns)
	}
	foreground.reply <- app.PromptResult{FinalText: "I can answer while the peer work runs."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Export(ctx, b.Key)
		return err == nil && len(got.Turns) == 1 && got.Turns[0].State == pa.TurnSucceeded &&
			len(got.Exchanges) == 1 && got.Exchanges[0].Outcome == pa.ExchangeSent
	})
	worker.reply <- app.PromptResult{FinalText: "Synthetic fact checked."}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin || !strings.Contains(reply.Text, "Synthetic fact checked.") {
		t.Fatalf("worker reply did not reach the sender: %+v", reply.Turn)
	}
	follow.reply <- app.PromptResult{FinalText: "The peer confirmed the fact."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Exchange(ctx, a.Key, x.ID)
		return err == nil && got.Outcome == pa.ExchangeCompleted && got.Delivery == "delivered"
	})
}
