package projectassistant

import (
	"errors"
	"testing"
	"time"
)

func TestDecisionAuthorityRestsOnlyOnThisBotChatsAnsweredCard(t *testing.T) {
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	fence := Fence{Key: BotKey{MachineID: "m", BotID: "xavia"}, Project: ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "/code/p"},
		ConversationID: "chat-1", BindingID: "b1", Generation: 1}
	offer := PermissionOffer{ID: "card-1", Fence: fence, Kind: DecisionOfferKind, State: PermissionResolved,
		Decision: &DecisionRequest{Question: "Open it?", AnswerText: "Yes"}}
	bindings := map[string]ExecutionBinding{"b1": {Fence: fence, NativeSessionID: "native-1"}}
	answer := &Message{Kind: UserMessage, At: now.Add(-time.Minute), DecisionReply: &DecisionReply{OfferID: "card-1", Answer: "Yes"}}

	got, err := CheckDecisionAuthority(offer, true, bindings, answer, "native-1", now, time.Hour)
	if err != nil || got.OfferID != "card-1" || got.BotID != "xavia" || got.CanonicalID != "/code/p" || got.Answer != "Yes" || !got.AnsweredAt.Equal(answer.At) {
		t.Fatalf("the person's answer = %+v, %v", got, err)
	}

	otherBot := fence
	otherBot.Key.BotID = "atlas"
	revoked := map[string]ExecutionBinding{"b1": {Fence: fence, NativeSessionID: "native-1", Revoked: true}}
	cases := []struct {
		name     string
		offer    func(PermissionOffer) PermissionOffer
		found    bool
		bindings map[string]ExecutionBinding
		answer   *Message
		conv     string
		want     string
	}{
		{name: "unknown card", found: false, want: DecisionUnknown},
		{name: "native permission", offer: func(o PermissionOffer) PermissionOffer { o.Kind = ""; return o }, found: true, want: DecisionUnknown},
		{name: "memory card", offer: func(o PermissionOffer) PermissionOffer {
			d := *o.Decision
			d.Memory = "a fact"
			o.Decision = &d
			return o
		}, found: true, want: DecisionUnknown},
		{name: "another Bot's question", offer: func(o PermissionOffer) PermissionOffer {
			d := *o.Decision
			d.SourceBotID = "atlas"
			o.Decision = &d
			return o
		}, found: true, want: DecisionNotThePersons},
		{name: "pending", offer: func(o PermissionOffer) PermissionOffer { o.State = PermissionPending; return o }, found: true, want: DecisionUnanswered},
		{name: "withdrawn", offer: func(o PermissionOffer) PermissionOffer { o.State = PermissionCancelled; return o }, found: true, want: DecisionUnanswered},
		{name: "no answer message", found: true, answer: &Message{}, want: DecisionUnanswered},
		{name: "another card's answer", found: true, answer: &Message{Kind: UserMessage, At: now, DecisionReply: &DecisionReply{OfferID: "card-2"}}, want: DecisionUnanswered},
		{name: "another conversation", found: true, conv: "native-2", want: DecisionOtherConversation},
		{name: "empty conversation", found: true, conv: " ", want: DecisionOtherConversation},
		{name: "revoked binding", found: true, bindings: revoked, want: DecisionOtherConversation},
		{name: "another Bot's binding", found: true, bindings: map[string]ExecutionBinding{"b1": {Fence: otherBot, NativeSessionID: "native-1"}}, want: DecisionOtherConversation},
		{name: "answered too long ago", found: true, answer: &Message{Kind: UserMessage, At: now.Add(-2 * time.Hour), DecisionReply: &DecisionReply{OfferID: "card-1"}}, want: DecisionExpired},
	}
	for _, c := range cases {
		o := offer
		if c.offer != nil {
			o = c.offer(o)
		}
		b, a, conv := bindings, answer, "native-1"
		if c.bindings != nil {
			b = c.bindings
		}
		if c.answer != nil {
			a = c.answer
		}
		if c.conv != "" {
			conv = c.conv
		}
		_, err := CheckDecisionAuthority(o, c.found, b, a, conv, now, time.Hour)
		var refused *DecisionRefusal
		if !errors.As(err, &refused) || refused.Code != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
}
