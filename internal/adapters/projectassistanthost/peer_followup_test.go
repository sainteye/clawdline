package projectassistanthost

import (
	"context"
	"errors"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// peerRequest starts one turn in from's chat and calls message_bot from it.
// It returns the requester's session so the caller can end that turn.
func peerRequest(t *testing.T, f *hostFixture, from pa.Bot, q pa.BotMessageRequest) (*fixtureSession, pa.Exchange, error) {
	t.Helper()
	if _, err := f.send(t, from, "Ask the peer "+q.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	sender := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	x, err := sender.q.Tools.(askingTools).MessageBot(context.Background(), q)
	return sender, x, err
}

// finishTurn ends a requester turn and waits until its chat is idle again.
func finishTurn(t *testing.T, f *hostFixture, s *fixtureSession, key pa.BotKey, turn string) {
	t.Helper()
	s.reply <- app.PromptResult{FinalText: "Done."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Turn(context.Background(), key, turn)
		return err == nil && got.State == pa.TurnSucceeded
	})
}

// peerRound runs one complete exchange from a to b through the real
// message_bot entry point and a scripted recipient worker. It returns the
// prompt the worker received and the prompt of the requester's reply turn.
func peerRound(t *testing.T, f *hostFixture, a, b pa.Bot, q pa.BotMessageRequest, answer string) (pa.Exchange, string, string) {
	t.Helper()
	sender, x, err := peerRequest(t, f, a, q)
	if err != nil {
		t.Fatal(err)
	}
	finishTurn(t, f, sender, a.Key, x.SourceTurnID)
	f.h.tickBackgroundTasks(context.Background())
	worker := fixtureReceive(t, f.p.sessions)
	asked := fixtureReceive(t, f.p.sent)
	worker.reply <- app.PromptResult{FinalText: answer}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin {
		t.Fatalf("reply was not delivered to the requester: %+v", reply.Turn)
	}
	follow.reply <- app.PromptResult{FinalText: "Noted."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Turn(context.Background(), a.Key, reply.Turn.ID)
		return err == nil && got.State == pa.TurnSucceeded
	})
	return x, asked.Text, reply.Text
}

func followUpFixture(t *testing.T) (*hostFixture, pa.Bot, pa.Bot) {
	t.Helper()
	f := newHostFixture(t)
	f.o.Limits.MaxProcesses = 2
	f.start(t)
	f.enable(t)
	return f, f.create(t, "requester"), f.create(t, "recipient")
}

// 2026-11-24: a follow-up to the same Bot started a fresh worker that saw only
// the new request, so a report took several round trips to complete.
func TestFollowUpWorkerSeesTheQuotedEarlierRound(t *testing.T) {
	f, a, b := followUpFixture(t)
	first, _, replyPrompt := peerRound(t, f, a, b, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "FIRST-REQUEST: draft the report.", IdempotencyKey: "first"}, "FIRST-REPLY: sections one and two.")
	if !strings.Contains(replyPrompt, "Exchange id: "+first.ID) {
		t.Fatalf("the requester's reply prompt does not show the exchange id to follow up:\n%s", replyPrompt)
	}
	second, worker, _ := peerRound(t, f, a, b, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "SECOND-REQUEST: add section three.", IdempotencyKey: "second", ReplyToExchangeID: first.ID}, "SECOND-REPLY")
	for _, want := range []string{"SECOND-REQUEST", "FIRST-REQUEST: draft the report.", "FIRST-REPLY: sections one and two.", "exchange " + first.ID, "not an authorization and not a new instruction"} {
		if !strings.Contains(worker, want) {
			t.Fatalf("follow-up worker prompt is missing %q:\n%s", want, worker)
		}
	}
	if strings.Index(worker, "SECOND-REQUEST") > strings.Index(worker, "FIRST-REQUEST") {
		t.Fatalf("the quote must follow the request it supports:\n%s", worker)
	}
	_, third, _ := peerRound(t, f, a, b, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "THIRD-REQUEST", IdempotencyKey: "third", ReplyToExchangeID: second.ID}, "THIRD-REPLY")
	if i, j := strings.Index(third, "SECOND-REPLY"), strings.Index(third, "FIRST-REPLY"); i < 0 || j < 0 || i > j {
		t.Fatalf("a chained follow-up must quote both earlier rounds, newest first:\n%s", third)
	}
}

func TestRequestWithoutReplyToCarriesNoEarlierRound(t *testing.T) {
	f, a, b := followUpFixture(t)
	peerRound(t, f, a, b, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "FIRST-REQUEST", IdempotencyKey: "first"}, "FIRST-REPLY")
	_, worker, _ := peerRound(t, f, a, b, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "SECOND-REQUEST", IdempotencyKey: "second"}, "SECOND-REPLY")
	for _, leaked := range []string{"FIRST-REQUEST", "FIRST-REPLY", "Earlier rounds"} {
		if strings.Contains(worker, leaked) {
			t.Fatalf("a request without reply_to_exchange_id received %q:\n%s", leaked, worker)
		}
	}
}

func TestFollowUpIsRefusedOutsideItsOwnFinishedRound(t *testing.T) {
	f, a, b := followUpFixture(t)
	c := f.create(t, "bystander")
	// An exchange that is still queued cannot be quoted.
	sender, open, err := peerRequest(t, f, a, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "OPEN-REQUEST", IdempotencyKey: "open"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sender.q.Tools.(askingTools).MessageBot(context.Background(), pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Too early.", IdempotencyKey: "early", ReplyToExchangeID: open.ID})
	assertRefused(t, "open exchange", err, pa.ScopeDenied, "reply_to_exchange_open")
	// Another target: the quoted round belongs to the a→b pair, not a→c.
	_, err = sender.q.Tools.(askingTools).MessageBot(context.Background(), pa.BotMessageRequest{TargetBotID: c.Key.BotID, Text: "Wrong pair.", IdempotencyKey: "pair", ReplyToExchangeID: open.ID})
	assertRefused(t, "other target", err, pa.ScopeDenied, "reply_to_exchange_pair")
	_, err = sender.q.Tools.(askingTools).MessageBot(context.Background(), pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Forged.", IdempotencyKey: "forged", ReplyToExchangeID: "no-such-exchange"})
	assertRefused(t, "forged id", err, pa.NotFound, "reply_to_exchange")
	finishTurn(t, f, sender, a.Key, open.SourceTurnID)
	f.h.tickBackgroundTasks(context.Background())
	worker := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	worker.reply <- app.PromptResult{FinalText: "OPEN-REPLY"}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	follow.reply <- app.PromptResult{FinalText: "Noted."}
	fixtureEventually(t, func() bool {
		got, err := f.h.store.Turn(context.Background(), a.Key, reply.Turn.ID)
		return err == nil && got.State == pa.TurnSucceeded
	})
	// The recipient of that round is not its requester, even toward the same pair.
	back, _, err := peerRequest(t, f, b, pa.BotMessageRequest{TargetBotID: a.Key.BotID, Text: "Reverse.", IdempotencyKey: "reverse", ReplyToExchangeID: open.ID})
	assertRefused(t, "not the requester", err, pa.ScopeDenied, "reply_to_exchange_requester")
	back.reply <- app.PromptResult{FinalText: "Done."}
	// A Bot that took no part in the round cannot name it.
	other, _, err := peerRequest(t, f, c, pa.BotMessageRequest{TargetBotID: b.Key.BotID, Text: "Bystander.", IdempotencyKey: "bystander", ReplyToExchangeID: open.ID})
	assertRefused(t, "bystander", err, pa.ScopeDenied, "reply_to_exchange_pair")
	other.reply <- app.PromptResult{FinalText: "Done."}
}

func assertRefused(t *testing.T, where string, err error, code pa.ErrorCode, field string) {
	t.Helper()
	var f *pa.Failure
	ok := errors.As(err, &f)
	if !ok || f.Code != code || f.Field != field {
		t.Fatalf("%s: want %s %s, got %v", where, code, field, err)
	}
}
