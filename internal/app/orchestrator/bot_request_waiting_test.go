package orchestrator

import (
	"context"
	"testing"

	br "github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// waitingCard is a card held by xavia, waiting on question decision-1.
func waitingCard(t *testing.T, b *Broker, message string) botrequest.Card {
	t.Helper()
	ctx := context.Background()
	auth := botrequest.Authority{Bot: "xavia", ConversationID: "chat", TurnID: "turn-" + message, MessageID: message}
	c, err := botrequest.NewCard(botrequest.CardID("m", "xavia", "chat", message), "project", "/project", "xavia", auth,
		botrequest.CreateInput{Title: "Publish the post", Checklist: []string{"draft", "published"}}, b.now())
	if err != nil {
		t.Fatal(err)
	}
	if c, _, err = b.Store.CreateBotRequest(ctx, c); err != nil {
		t.Fatal(err)
	}
	res, err := b.Store.UpdateBotRequest(ctx, br.BotRequestWrite{CardID: c.ID, Actor: botrequest.Actor{Bot: "xavia"}, Now: b.now(),
		Apply: func(c *botrequest.Card) ([]botrequest.Event, error) {
			if _, err := c.SetWaiting(botrequest.Actor{Bot: "xavia"}, "decision-1"); err != nil {
				return nil, err
			}
			return []botrequest.Event{{Kind: botrequest.EventWaitingSet, Detail: "decision-1"}}, nil
		}})
	if err != nil || res.Card.Waiting != "decision-1" {
		t.Fatalf("set waiting: %v %+v", err, res.Card)
	}
	return res.Card
}

func cardEvents(t *testing.T, b *Broker, id string) []br.BotRequestEvent {
	t.Helper()
	events, err := b.Store.BotRequestEvents(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// The person's answer to the question a card waits on takes the card out
// of "Waiting for you" in the same commit (docs/bot-request-cards.md §7.3).
func TestTheAnswerClearsTheCardWaitingOnIt(t *testing.T) {
	b, _, _ := followupBroker(t)
	card := waitingCard(t, b, "msg-1")
	other := waitingCard(t, b, "msg-2")
	commitDecision(t, b, decisionOffer(pa.PermissionPending))
	if got, _ := b.Store.BotRequestCard(context.Background(), card.ID); got.Waiting != "decision-1" {
		t.Fatalf("asking cleared the wait: %+v", got)
	}
	commitDecision(t, b, decisionOffer(pa.PermissionResolved))
	for _, id := range []string{card.ID, other.ID} {
		got, err := b.Store.BotRequestCard(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Waiting != "" {
			t.Fatalf("card %s still waits after the answer: %q", id, got.Waiting)
		}
		events := cardEvents(t, b, id)
		last := events[len(events)-1]
		if last.Kind != botrequest.EventWaitingCleared || last.Actor != "person" || last.Detail != "decision-1" || last.Version != got.Version {
			t.Fatalf("clearing event %+v (card version %d)", last, got.Version)
		}
	}
	// A retried commit of the same answer writes nothing more.
	before := len(cardEvents(t, b, card.ID))
	commitDecision(t, b, decisionOffer(pa.PermissionResolved))
	if after := len(cardEvents(t, b, card.ID)); after != before {
		t.Fatalf("a repeated answer wrote %d more events", after-before)
	}
}

// A withdrawn or expired question no longer waits for anyone either.
func TestAClosedQuestionClearsTheWaitAsItsBot(t *testing.T) {
	for _, state := range []pa.PermissionState{pa.PermissionCancelled, pa.PermissionExpired} {
		b, _, _ := followupBroker(t)
		card := waitingCard(t, b, "msg-1")
		commitDecision(t, b, decisionOffer(state))
		got, _ := b.Store.BotRequestCard(context.Background(), card.ID)
		events := cardEvents(t, b, card.ID)
		if last := events[len(events)-1]; got.Waiting != "" || last.Kind != botrequest.EventWaitingCleared || last.Actor != "bot:xavia" {
			t.Fatalf("%s: waiting %q, last event %+v", state, got.Waiting, last)
		}
	}
}

// Another question of the same Bot, a native permission, or a done card
// leave a card alone.
func TestOnlyTheNamedQuestionClearsAnOpenCard(t *testing.T) {
	b, _, _ := followupBroker(t)
	card := waitingCard(t, b, "msg-1")
	other := decisionOffer(pa.PermissionResolved)
	other.ID = "decision-2"
	commitDecision(t, b, other)
	native := decisionOffer(pa.PermissionResolved)
	native.Kind, native.Decision = "", nil
	commitDecision(t, b, native)
	if got, _ := b.Store.BotRequestCard(context.Background(), card.ID); got.Waiting != "decision-1" {
		t.Fatalf("an unrelated close cleared the wait: %+v", got)
	}
}
