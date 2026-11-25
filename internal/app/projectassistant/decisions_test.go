package projectassistant

import (
	"context"
	"errors"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type decisionServiceRepo struct {
	*serviceRepo
	offer     pa.PermissionOffer
	offerErr  error
	offerRead int
}

func (r *decisionServiceRepo) Permission(context.Context, pa.BotKey, string) (pa.PermissionOffer, error) {
	r.offerRead++
	return r.offer, r.offerErr
}

// Binding answers NotFound: the answer path must not depend on it.
func (r *decisionServiceRepo) Binding(context.Context, pa.BotKey, string) (pa.ExecutionBinding, error) {
	return pa.ExecutionBinding{}, &pa.Failure{Code: pa.NotFound, Field: "binding"}
}

func newDecisionFixture(t *testing.T) (*serviceFixture, *decisionServiceRepo) {
	t.Helper()
	f := newServiceFixture(t)
	repo := &decisionServiceRepo{serviceRepo: f.repo}
	f.repo.bot = pa.Bot{Key: f.key, ConversationID: "conversation-one"}
	f.repo.conversation = pa.Conversation{Key: f.key, ID: "conversation-one", BindingID: "binding-one", Generation: 1}
	fence := pa.Fence{Key: f.key, ConversationID: "conversation-one", BindingID: "binding-one", Generation: 1}
	repo.offer = pa.PermissionOffer{ID: "offer-one", Kind: pa.DecisionOfferKind, Fence: fence, State: pa.PermissionPending,
		Decision:  &pa.DecisionRequest{Question: "Roll out now?"},
		Options:   []pa.PermissionOption{{ID: "reply-1", Label: "Yes"}, {ID: "reply-2", Label: "Not now"}},
		ExpiresAt: time.Unix(1700000000, 0).Add(time.Hour)}
	f.s.opts.Repository = repo
	return f, repo
}

func (f *serviceFixture) answer(ctx context.Context, key string, a pa.PermissionAnswer) (pa.Receipt, error) {
	c := f.command(pa.AnswerOperation)
	c.IdempotencyKey = key
	return f.s.Answer(ctx, c, a)
}

func TestDecisionPressReplaysItsReceiptAfterTheChatMoved(t *testing.T) {
	f, repo := newDecisionFixture(t)
	ctx := context.Background()
	press := pa.PermissionAnswer{Fence: repo.offer.Fence, OfferID: repo.offer.ID, OptionID: "reply-1"}
	first, err := f.answer(ctx, "press-one", press)
	if err != nil || len(f.runtime.submissions) != 1 {
		t.Fatalf("first press = %+v, %v", first, err)
	}
	// What the runtime's commit leaves behind: the receipt under the offer's
	// key, and the offer resolved. Then a restart moves the chat's binding.
	s := f.runtime.submissions[0]
	f.repo.receipts[string(s.Command.Operation)+":"+s.Command.IdempotencyKey] = pa.Receipt{Command: s.Command, ID: "receipt-one", State: pa.EffectAccepted}
	repo.offer = *s.Decision
	f.repo.conversation.BindingID, f.repo.conversation.Generation = "binding-two", 2

	again, err := f.answer(ctx, "press-two", press)
	if err != nil || again.ID != "receipt-one" || len(f.runtime.submissions) != 1 {
		t.Fatalf("a press whose reply was lost did not get its receipt back: %+v, %v", again, err)
	}
	other := press
	other.OptionID = "reply-2"
	if _, err = f.answer(ctx, "press-three", other); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a different answer to an answered card = %v", err)
	}
}

func TestDecisionAnswerRefusalsBeforeTheCommitAreTypedAndRetryable(t *testing.T) {
	f, repo := newDecisionFixture(t)
	press := pa.PermissionAnswer{Fence: repo.offer.Fence, OfferID: repo.offer.ID, OptionID: "reply-1"}

	repo.offerErr = errors.New("scripted store read failure")
	_, err := f.answer(context.Background(), "unreadable", press)
	var refusal *pa.Failure
	if !errors.As(err, &refusal) || refusal.Code != pa.Offline || refusal.Field != "decision_offer" {
		t.Fatalf("an unreadable card = %#v", err)
	}
	if f.runtime.calls != 0 {
		t.Fatal("a suggested reply whose card could not be read went to the native permission path")
	}

	repo.offerErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.answer(ctx, "cancelled", press)
	if pa.Code(err) != pa.Offline || !errors.As(err, &refusal) || refusal.Field == "" {
		t.Fatalf("a cancelled answer that wrote nothing = %v", err)
	}
	if f.runtime.calls != 0 {
		t.Fatal("a cancelled answer reached the runtime")
	}
}

func TestDecisionAcceptsALongTypedAnswer(t *testing.T) {
	f, repo := newDecisionFixture(t)
	long := ""
	for len(long) < 3*pa.MaxDecisionQuestionBytes {
		long += "這是使用者自己打的長回覆。"
	}
	if _, err := f.answer(context.Background(), "long", pa.PermissionAnswer{Fence: repo.offer.Fence, OfferID: repo.offer.ID, Text: long}); err != nil {
		t.Fatalf("a %d-byte typed answer was refused: %v", len(long), err)
	}
	if got := f.runtime.submissions[0].Decision.Decision.AnswerText; got != long {
		t.Fatalf("answer text changed: %d bytes", len(got))
	}
}
