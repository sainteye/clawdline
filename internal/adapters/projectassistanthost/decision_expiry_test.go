package projectassistanthost

import (
	"context"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A peer's question for the person that nobody answers before its offer
// expires closes as expired, and the requesting Bot hears that instead of
// waiting forever.
func TestUnansweredPeerQuestionExpiresAndTellsTheRequester(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	a, b, x, offer := startPeerQuestion(t, f)
	if offer.ExpiresAt.IsZero() {
		t.Fatalf("the offer has no expiry: %+v", offer)
	}
	if err := f.h.expireWaitingDecisions(ctx, offer.ExpiresAt.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if got, err := f.h.store.Exchange(ctx, a.Key, x.ID); err != nil || got.Outcome != pa.ExchangeNeedsPerson {
		t.Fatalf("a question still answerable was expired: %+v, %v", got, err)
	}
	if err := f.h.expireWaitingDecisions(ctx, offer.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if cards := decisionOffers(t, f, b.Key); len(cards) != 1 || cards[0].State != pa.PermissionExpired {
		t.Fatalf("the card did not close as expired: %+v", cards)
	}
	done, err := f.h.store.Exchange(ctx, a.Key, x.ID)
	if err != nil || done.Outcome != pa.ExchangeCompleted || !strings.Contains(done.Reply, "expired") || done.Delivery != "queued" {
		t.Fatalf("the waiting exchange did not complete with the expiry: %+v, %v", done, err)
	}
	follow := fixtureReceive(t, f.p.sessions)
	reply := fixtureReceive(t, f.p.sent)
	if follow.q.Bot.Key != a.Key || reply.Turn.Origin != pa.BotReplyOrigin || !strings.Contains(reply.Text, "Authorize the dev rollout now?") {
		t.Fatalf("the requester was not told the question expired: %+v %q", reply.Turn, reply.Text)
	}
	follow.reply <- app.PromptResult{FinalText: "Noted."}
	if err := f.h.expireWaitingDecisions(ctx, offer.ExpiresAt.Add(time.Hour)); err != nil {
		t.Fatalf("a second sweep failed: %v", err)
	}
}
