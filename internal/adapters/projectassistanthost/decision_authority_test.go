package projectassistanthost

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func decisionRefusalCode(err error) string {
	var refused *pa.DecisionRefusal
	if errors.As(err, &refused) {
		return refused.Code
	}
	return ""
}

// A Bot's own ask_person card authorizes a Board write only once the person
// answered it on the console's answer route, and only from the native
// session of the Bot chat that asked it.
func TestAnsweredQuestionCardIsTheBotChatsAuthority(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "asker")
	if _, err := f.send(t, bot, "Should we open a Board item for this?"); err != nil {
		t.Fatal(err)
	}
	session := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	offer, err := session.q.Tools.(askingTools).AskPerson(ctx, pa.AskPersonRequest{
		Question: "Open a Board item for the shared event stream follow-ups?", SuggestedReplies: []string{"Yes, open it", "No"}})
	if err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Asked."}
	fixtureEventually(t, func() bool {
		p, err := f.h.store.Permission(ctx, bot.Key, offer.ID)
		return err == nil && p.State == pa.PermissionPending && p.Fence.BindingID != ""
	})

	if _, err := f.h.DecisionAuthority(ctx, "fake-native", offer.ID, time.Hour); decisionRefusalCode(err) != pa.DecisionUnanswered {
		t.Fatalf("an unanswered card authorized a write: %v", err)
	}
	if _, err := f.h.DecisionAuthority(ctx, "fake-native", "no-such-card", time.Hour); decisionRefusalCode(err) != pa.DecisionUnknown {
		t.Fatalf("a made-up card id = %v", err)
	}

	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(ctx, pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "press"},
		pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, OptionID: "reply-1"}); err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)

	got, err := f.h.DecisionAuthority(ctx, "fake-native", offer.ID, time.Hour)
	if err != nil {
		t.Fatalf("the person's answer did not authorize the Bot chat: %v", err)
	}
	if got.OfferID != offer.ID || got.BotID != bot.Key.BotID || got.Answer != "Yes, open it" ||
		got.ConversationID != offer.Fence.ConversationID || got.CanonicalID != offer.Fence.Project.CanonicalID || got.AnsweredAt.IsZero() {
		t.Fatalf("authority = %+v", got)
	}
	if _, err := f.h.DecisionAuthority(ctx, "another-native-session", offer.ID, time.Hour); decisionRefusalCode(err) != pa.DecisionOtherConversation {
		t.Fatalf("another conversation used this Bot's card: %v", err)
	}
	if _, err := f.h.DecisionAuthority(ctx, "fake-native", offer.ID, -time.Nanosecond); decisionRefusalCode(err) != pa.DecisionExpired {
		t.Fatalf("an answer outside the window = %v", err)
	}
}
