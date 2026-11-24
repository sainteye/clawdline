package projectassistant

import (
	"strings"
	"time"
	"unicode/utf8"
)

// DecisionOfferKind marks a PermissionOffer that holds a question a Bot asked
// the person with ask_person. Only the console's authorized answer route can
// answer it; no Bot tool can.
const DecisionOfferKind = "decision"

// DecisionOptionPrefix starts every suggested reply's option id.
const DecisionOptionPrefix = "reply-"

const (
	// MaxDecisionOffersPerBot bounds pending questions in one Bot's chat.
	MaxDecisionOffersPerBot = 8
	// MaxDecisionSuggestedReplies bounds the suggested replies on one card.
	MaxDecisionSuggestedReplies = 3
	MaxDecisionQuestionBytes    = 2000
	MaxDecisionReplyBytes       = 500
	// MaxDecisionRequestQuoteBytes bounds the requester's message the card
	// and the person's answer quote as context.
	MaxDecisionRequestQuoteBytes = 1500
	// MaxDecisionOfferSeconds is how long a question stays answerable. A
	// peer exchange has no other expiry, so the offer and its waiting
	// exchange expire together at this bound.
	MaxDecisionOfferSeconds = 24 * 60 * 60
)

// DecisionOfferTTL is MaxDecisionOfferSeconds as a duration.
const DecisionOfferTTL = MaxDecisionOfferSeconds * time.Second

// DecisionRequest is Bot-authored display data; it never authorizes anything.
type DecisionRequest struct {
	Question    string `json:"question"`
	ExchangeID  string `json:"exchange_id,omitempty"`
	SourceBotID string `json:"source_bot_id,omitempty"`
	Request     string `json:"request,omitempty"`
	// RequestTurnID is the peer-request turn that asked, in the hidden
	// worker or the recipient; it is not a turn of the chat showing the card.
	RequestTurnID string `json:"request_turn_id,omitempty"`
	AnswerText    string `json:"answer_text,omitempty"`
	// Memory is the literal short fact a Bot proposed with edit_memory in a
	// turn answering another Bot. Only the person's MemorySaveOptionID press
	// on the console's answer route writes it, as the person's own entry.
	Memory string `json:"memory,omitempty"`
}

const (
	// MemorySaveOptionID and MemorySkipOptionID are a memory card's only
	// replies; they carry DecisionOptionPrefix so they reach the decision
	// answer route.
	MemorySaveOptionID = DecisionOptionPrefix + "memory-save"
	MemorySkipOptionID = DecisionOptionPrefix + "memory-skip"
	// MemoryProposalQuestion is a memory card's question; the console shows
	// its own translation beside the proposed fact.
	MemoryProposalQuestion = "Save this to memory?"
)

// AskPersonRequest is the ask_person tool's input.
type AskPersonRequest struct {
	Question         string   `json:"question"`
	SuggestedReplies []string `json:"suggested_replies"`
	// Memory is set only by the host for an edit_memory proposal; a Bot's
	// ask_person input cannot carry it.
	Memory string `json:"-"`
}

// CheckMemoryProposal admits one short fact a Bot proposes to remember.
func CheckMemoryProposal(text string) error {
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) {
		return fail(Invalid, "memory_proposal")
	}
	if len(text) > MaxDecisionQuestionBytes {
		return fail(CapacityFull, "memory_proposal_bytes")
	}
	return ValidateMessage(text)
}

func CheckAskPerson(q AskPersonRequest) error {
	q.Question = strings.TrimSpace(q.Question)
	if q.Question == "" || !utf8.ValidString(q.Question) {
		return fail(Invalid, "decision_question")
	}
	if len(q.Question) > MaxDecisionQuestionBytes {
		return fail(CapacityFull, "decision_question_bytes")
	}
	if len(q.SuggestedReplies) < 1 || len(q.SuggestedReplies) > MaxDecisionSuggestedReplies {
		return fail(Invalid, "decision_suggested_replies")
	}
	seen := map[string]bool{}
	for _, r := range q.SuggestedReplies {
		r = strings.TrimSpace(r)
		if r == "" || !utf8.ValidString(r) || seen[r] {
			return fail(Invalid, "decision_suggested_reply")
		}
		if len(r) > MaxDecisionReplyBytes {
			return fail(CapacityFull, "decision_reply_bytes")
		}
		seen[r] = true
	}
	return nil
}

// DecisionAnswerText resolves the person's press: a suggested reply's full
// text, or their own typed reply when no option is chosen.
func DecisionAnswerText(offer PermissionOffer, answer PermissionAnswer) (string, error) {
	if offer.Kind != DecisionOfferKind || offer.Decision == nil {
		return "", fail(Invalid, "decision_offer")
	}
	if answer.OptionID != "" {
		if answer.Text != "" {
			return "", fail(Invalid, "decision_answer")
		}
		for _, o := range offer.Options {
			if o.ID == answer.OptionID {
				return o.Label, nil
			}
		}
		return "", fail(Invalid, "decision_option")
	}
	text := strings.TrimSpace(answer.Text)
	if text == "" || !utf8.ValidString(text) {
		return "", fail(Invalid, "decision_answer")
	}
	if len(text) > MaxDecisionQuestionBytes {
		return "", fail(CapacityFull, "decision_answer_bytes")
	}
	return text, nil
}

// CheckDecision admits an answer only to a pending, unexpired offer in the
// chat it was raised in, while the Bot is visible and its binding stands.
func CheckDecision(offer PermissionOffer, answer PermissionAnswer, bot Bot, revoked bool, now time.Time) error {
	if offer.Kind != DecisionOfferKind || offer.Decision == nil || answer.OfferID != offer.ID {
		return fail(Invalid, "decision_offer")
	}
	if answer.Fence.Key != offer.Fence.Key || bot.Key != offer.Fence.Key {
		return fail(ScopeDenied, "decision_key")
	}
	if offer.State != PermissionPending {
		return fail(StaleVersion, "decision_answered")
	}
	if !offer.ExpiresAt.IsZero() && !now.Before(offer.ExpiresAt) {
		return fail(Expired, "decision_expired")
	}
	if bot.Hidden || revoked {
		return fail(ScopeDenied, "decision_withdrawn")
	}
	return nil
}

// DecisionPrompt is the person's message: the exchange quoted as data, then
// their own words as the instruction.
func DecisionPrompt(offer PermissionOffer, answer string) string {
	d := offer.Decision
	var b strings.Builder
	if d.Request != "" {
		b.WriteString("> Request from another Bot:\n")
		quote(&b, d.Request)
	}
	b.WriteString("> Your question for me:\n")
	quote(&b, d.Question)
	b.WriteString("\n")
	b.WriteString(answer)
	return b.String()
}

func quote(b *strings.Builder, text string) {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

// ClipUTF8 shortens text to at most n bytes at a rune boundary.
func ClipUTF8(text string, n int) string {
	if len(text) <= n {
		return text
	}
	text = text[:n]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
