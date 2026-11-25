package projectassistant

import (
	"strings"
	"testing"
	"time"
)

func decisionFixture() PermissionOffer {
	key := BotKey{MachineID: "machine", BotID: "bot"}
	return PermissionOffer{ID: "offer", Kind: DecisionOfferKind, Fence: Fence{Key: key}, State: PermissionPending,
		Decision: &DecisionRequest{Question: "Roll out now?"}, Options: []PermissionOption{{ID: "reply-1", Label: "Yes"}},
		ExpiresAt: time.Unix(1700003600, 0)}
}

func TestDecisionTypedAnswerTakesALongReplyUpToItsOwnBound(t *testing.T) {
	offer := decisionFixture()
	// About 700 Chinese characters, the length the old question-sized bound
	// always refused.
	long := strings.Repeat("長", 700)
	if text, err := DecisionAnswerText(offer, PermissionAnswer{Text: long}); err != nil || text != long {
		t.Fatalf("a %d-byte reply = %v", len(long), err)
	}
	limit := strings.Repeat("a", MaxDecisionAnswerBytes)
	if _, err := DecisionAnswerText(offer, PermissionAnswer{Text: limit}); err != nil {
		t.Fatalf("a reply at the bound = %v", err)
	}
	if _, err := DecisionAnswerText(offer, PermissionAnswer{Text: limit + "a"}); Code(err) != CapacityFull {
		t.Fatalf("a reply over the bound = %v", err)
	}
}

func TestDecisionPromptAtEveryBoundStillFitsAMessage(t *testing.T) {
	offer := decisionFixture()
	// Empty lines are the worst case for quoting: each byte becomes "> \n".
	offer.Decision.Question = "q" + strings.Repeat("\n", MaxDecisionQuestionBytes-2) + "q"
	offer.Decision.Request = "r" + strings.Repeat("\n", MaxDecisionRequestQuoteBytes-2) + "r"
	prompt := DecisionPrompt(offer, strings.Repeat("a", MaxDecisionAnswerBytes))
	if err := ValidateInputSend(prompt, nil, nil); err != nil {
		t.Fatalf("a %d-byte decision prompt was refused: %v", len(prompt), err)
	}
}

func TestDecisionStaysAnswerableAfterItsBindingWasRevoked(t *testing.T) {
	offer := decisionFixture()
	bot := Bot{Key: offer.Fence.Key}
	answer := PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID}
	now := offer.ExpiresAt.Add(-time.Minute)
	if err := CheckDecision(offer, answer, bot, now); err != nil {
		t.Fatalf("a pending card in a visible Bot = %v", err)
	}
	bot.Hidden = true
	if err := CheckDecision(offer, answer, bot, now); Code(err) != ScopeDenied {
		t.Fatalf("a hidden Bot's card = %v", err)
	}
}

func TestNotSentKeepsTypedRefusalsAndClassifiesTheRest(t *testing.T) {
	typed := fail(Expired, "decision_expired")
	if NotSent(typed, "x") != typed {
		t.Fatal("a typed refusal was rewritten")
	}
	if err := NotSent(errContext, "answer_channel"); Code(err) != Offline || err.(*Failure).Field != "answer_channel" || err.(*Failure).RetryAfterSeconds < 1 {
		t.Fatalf("an untyped error before the commit = %#v", err)
	}
	if NotSent(nil, "x") != nil {
		t.Fatal("nil became an error")
	}
}

var errContext = &plainError{"context canceled"}

type plainError struct{ s string }

func (e *plainError) Error() string { return e.s }
