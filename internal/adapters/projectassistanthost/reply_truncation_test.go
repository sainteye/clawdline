package projectassistanthost

import (
	"context"
	"strconv"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// overLimitReply is a peer reply longer than the reply bound whose last
// section must not silently disappear (2026-11-24: a review stopped
// mid-sentence). It stays under pa.MaxMessageBytes, which bounds one chat
// message on its own.
func overLimitReply() string {
	return strings.Repeat("Finding: the recovery path is unproved. ", (pa.MaxCollaborationReplyBytes+500)/40) + "FINAL-SECTION"
}

// assertTruncationTold checks that what a requester reads says it was cut,
// how long the original was, and that it is not the whole answer.
func assertTruncationTold(t *testing.T, where, got, original string) {
	t.Helper()
	if strings.Contains(got, "FINAL-SECTION") {
		t.Fatalf("%s: over-limit reply was not bounded", where)
	}
	for _, want := range []string{"[Clawdline: reply truncated", "original " + strconv.Itoa(len(original)) + " bytes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%s: missing %q; the requester cannot tell the reply was cut (tail %q)", where, want, tail(got))
		}
	}
}

func tail(s string) string {
	if len(s) > 300 {
		return s[len(s)-300:]
	}
	return s
}

func assertPromptRefusesPartialConclusion(t *testing.T, where, prompt string) {
	t.Helper()
	if !strings.Contains(prompt, "Do not treat it as the complete answer") {
		t.Fatalf("%s: correlated reply prompt does not warn about the truncated reply (tail %q)", where, tail(prompt))
	}
}

func TestOverLimitPeerReplyIsMarkedForTheRequester(t *testing.T) {
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
	x, err := sender.q.Tools.(askingTools).MessageBot(ctx, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Review the synthetic recovery path.", IdempotencyKey: "long-review"})
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
	long := overLimitReply()
	before := pa.CollaborationReplyUsage()
	worker.reply <- app.PromptResult{FinalText: long}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin {
		t.Fatalf("reply was not delivered to the requester: %+v", reply.Turn)
	}
	assertTruncationTold(t, "local exchange prompt", reply.Text, long)
	assertPromptRefusesPartialConclusion(t, "local exchange prompt", reply.Text)
	stored, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertTruncationTold(t, "local exchange stored reply", stored.Reply, long)
	if after := pa.CollaborationReplyUsage(); after.Truncated <= before.Truncated {
		t.Fatalf("diagnostics did not count the truncation: before %+v after %+v", before, after)
	}
	follow.reply <- app.PromptResult{FinalText: "Asked for the rest."}
}

func TestOverLimitDecisionAnswerIsMarkedForTheRequester(t *testing.T) {
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
	long := overLimitReply()
	before := pa.CollaborationReplyUsage()
	main.reply <- app.PromptResult{FinalText: long}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin {
		t.Fatalf("decision was not forwarded: %+v", reply.Turn)
	}
	assertTruncationTold(t, "decision prompt", reply.Text, long)
	assertPromptRefusesPartialConclusion(t, "decision prompt", reply.Text)
	stored, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertTruncationTold(t, "decision stored reply", stored.Reply, long)
	if after := pa.CollaborationReplyUsage(); after.Truncated <= before.Truncated {
		t.Fatalf("diagnostics did not count the truncation: before %+v after %+v", before, after)
	}
	follow.reply <- app.PromptResult{FinalText: "Asked for the rest."}
}

// Both network receipts (Bot network and assistant network) read the stored
// turn reply through networkTurnReply.
func TestOverLimitNetworkReplyIsMarkedInTheReceipt(t *testing.T) {
	long := overLimitReply()
	messages := []pa.Message{
		{TurnID: "other", Kind: pa.AssistantMessage, Text: "unrelated"},
		{TurnID: "turn", Kind: pa.NoticeMessage, Text: "notice"},
		{TurnID: "turn", Kind: pa.AssistantMessage, Text: long},
	}
	before := pa.CollaborationReplyUsage()
	assertTruncationTold(t, "network receipt", networkTurnReply(messages, "turn"), long)
	if after := pa.CollaborationReplyUsage(); after.Truncated <= before.Truncated {
		t.Fatalf("diagnostics did not count the truncation: before %+v after %+v", before, after)
	}
	if got := networkTurnReply(messages[:1], "turn"); got != "" {
		t.Fatalf("a turn without an answer got %q", got)
	}
}
