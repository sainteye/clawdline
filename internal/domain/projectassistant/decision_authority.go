package projectassistant

import (
	"strings"
	"time"
)

// DecisionAuthority is a Bot's ask_person question that the person answered
// on the console's answer route, found by the daemon in that Bot's own chat.
// It is the proof a Board write made by the Bot rests on: the Bot names the
// card, and the daemon, never the model, says whether the person answered it.
type DecisionAuthority struct {
	OfferID         string
	BotID           string
	ConversationID  string
	NativeSessionID string
	// CanonicalID is the canonical key of the Project the Bot's chat is in.
	CanonicalID string
	Question    string
	Answer      string
	AnsweredAt  time.Time
}

// DecisionRefusal is why a named decision card does not authorize a write.
type DecisionRefusal struct{ Code string }

func (r *DecisionRefusal) Error() string { return "decision card refused: " + r.Code }

// Decision refusal codes.
const (
	DecisionUnknown           = "decision_unknown"
	DecisionUnanswered        = "decision_unanswered"
	DecisionNotThePersons     = "decision_not_the_persons"
	DecisionOtherConversation = "decision_other_conversation"
	DecisionExpired           = "decision_expired"
)

// CheckDecisionAuthority admits offer as the person's answer behind a write
// made from the native conversation `conversation`. The offer must be an
// ask_person question of this Bot's own (not a host memory card, not a
// question relayed for another Bot), answered by the person within window,
// and the conversation must be the live, unrevoked binding of the same Bot
// chat the question was asked in. answer is the person's message that
// answered it; its time is the answer's time.
func CheckDecisionAuthority(offer PermissionOffer, found bool, bindings map[string]ExecutionBinding,
	answer *Message, conversation string, now time.Time, window time.Duration) (DecisionAuthority, error) {
	conversation = strings.TrimSpace(conversation)
	if !found || offer.Kind != DecisionOfferKind || offer.Decision == nil || offer.Decision.Memory != "" {
		return DecisionAuthority{}, &DecisionRefusal{DecisionUnknown}
	}
	if offer.Decision.SourceBotID != "" {
		return DecisionAuthority{}, &DecisionRefusal{DecisionNotThePersons}
	}
	if offer.State != PermissionResolved || answer == nil || answer.Kind != UserMessage ||
		answer.DecisionReply == nil || answer.DecisionReply.OfferID != offer.ID {
		return DecisionAuthority{}, &DecisionRefusal{DecisionUnanswered}
	}
	bound := false
	for _, b := range bindings {
		if conversation != "" && b.NativeSessionID == conversation && !b.Revoked &&
			b.Fence.Key == offer.Fence.Key && b.Fence.ConversationID == offer.Fence.ConversationID &&
			b.Fence.Project == offer.Fence.Project {
			bound = true
			break
		}
	}
	if !bound {
		return DecisionAuthority{}, &DecisionRefusal{DecisionOtherConversation}
	}
	if answer.At.IsZero() || now.Sub(answer.At) > window {
		return DecisionAuthority{}, &DecisionRefusal{DecisionExpired}
	}
	return DecisionAuthority{OfferID: offer.ID, BotID: offer.Fence.Key.BotID, ConversationID: offer.Fence.ConversationID,
		NativeSessionID: conversation, CanonicalID: offer.Fence.Project.CanonicalID,
		Question: offer.Decision.Question, Answer: answer.DecisionReply.Answer, AnsweredAt: answer.At}, nil
}
