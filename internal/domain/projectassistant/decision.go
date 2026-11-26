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

// Roster decision kinds distinguish a Bot's words from a host-owned prompt.
const (
	RosterDecisionQuestion   = "question"
	RosterDecisionMemorySave = "memory_save"
)

// DecisionOptionPrefix starts every suggested reply's option id.
const DecisionOptionPrefix = "reply-"

const (
	// MaxDecisionOffersPerBot bounds pending questions in one Bot's chat.
	MaxDecisionOffersPerBot = 8
	// MaxDecisionSuggestedReplies bounds the suggested replies on one card.
	MaxDecisionSuggestedReplies = 3
	MaxDecisionQuestionBytes    = 2000
	MaxDecisionReplyBytes       = 500
	// MaxDecisionPromptOverheadBytes is reserved for what DecisionPrompt
	// adds around the person's answer: the quoted question and request
	// with their headers. Quoting at most triples a text of empty lines,
	// so 3*(MaxDecisionQuestionBytes+MaxDecisionRequestQuoteBytes) plus
	// the headers fits.
	MaxDecisionPromptOverheadBytes = 16 << 10
	// MaxDecisionAnswerBytes bounds the person's own typed answer. The
	// answer becomes their message, so it is the message bound minus the
	// quoted context, never the much smaller question bound.
	MaxDecisionAnswerBytes = MaxUserMessageBytes - MaxDecisionPromptOverheadBytes
	// MaxDecisionRequestQuoteBytes bounds the requester's message the card
	// and the person's answer quote as context.
	MaxDecisionRequestQuoteBytes = 1500
	// MaxDecisionOfferSeconds is how long a question stays answerable. A
	// peer exchange has no other expiry: the host's routine sweep completes
	// a waiting exchange once its offer passes this bound.
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
	// WithdrawnReason is the Bot's own short reason for withdrawing the
	// question before the person answered it.
	WithdrawnReason string `json:"withdrawn_reason,omitempty"`
}

// DecisionReply marks the person's message that answered a question card, so
// a reader can show it as the card's answer instead of the quoted prompt the
// Bot receives. It is display data copied from the resolved offer.
type DecisionReply struct {
	OfferID     string `json:"offer_id"`
	Question    string `json:"question"`
	Answer      string `json:"answer"`
	SourceBotID string `json:"source_bot_id,omitempty"`
}

// NewDecisionReply describes the message that answers a resolved question.
func NewDecisionReply(offer PermissionOffer) *DecisionReply {
	if offer.Kind != DecisionOfferKind || offer.Decision == nil || offer.Decision.Memory != "" {
		return nil
	}
	d := offer.Decision
	return &DecisionReply{OfferID: offer.ID, Question: d.Question, Answer: d.AnswerText, SourceBotID: d.SourceBotID}
}

// WithdrawQuestionRequest is the withdraw_question tool's input: a question
// the Bot asked the person that is no longer needed, and why.
type WithdrawQuestionRequest struct {
	QuestionID string `json:"question_id"`
	Reason     string `json:"reason"`
}

// PendingQuestion is one open question card in the Bot's own chat, as the
// list_questions tool returns it.
type PendingQuestion struct {
	QuestionID       string    `json:"question_id"`
	Question         string    `json:"question"`
	SuggestedReplies []string  `json:"suggested_replies"`
	FromBotID        string    `json:"from_bot_id,omitempty"`
	Request          string    `json:"request,omitempty"`
	AskedAt          time.Time `json:"asked_at"`
}

// CheckWithdrawQuestion admits a withdrawal; the reason reuses the bound of a
// suggested reply.
func CheckWithdrawQuestion(q WithdrawQuestionRequest) error {
	if strings.TrimSpace(q.QuestionID) == "" {
		return fail(Invalid, "withdraw_question_id")
	}
	reason := strings.TrimSpace(q.Reason)
	if reason == "" || !utf8.ValidString(reason) {
		return fail(Invalid, "withdraw_question_reason")
	}
	if len(reason) > MaxDecisionReplyBytes {
		return fail(CapacityFull, "withdraw_question_reason_bytes")
	}
	return nil
}

// QuestionWithdrawable reports whether a Bot may withdraw this offer: a
// pending ask_person question, never a host-owned memory card or a native
// tool permission.
func QuestionWithdrawable(offer PermissionOffer) bool {
	return offer.Kind == DecisionOfferKind && offer.Decision != nil && offer.Decision.Memory == "" && offer.State == PermissionPending
}

// PendingQuestionOf is the list_questions view of a pending question.
func PendingQuestionOf(offer PermissionOffer) PendingQuestion {
	q := PendingQuestion{QuestionID: offer.ID, Question: offer.Decision.Question, FromBotID: offer.Decision.SourceBotID, Request: offer.Decision.Request, SuggestedReplies: []string{}}
	if !offer.ExpiresAt.IsZero() {
		q.AskedAt = offer.ExpiresAt.Add(-DecisionOfferTTL)
	}
	for _, o := range offer.Options {
		q.SuggestedReplies = append(q.SuggestedReplies, o.Label)
	}
	return q
}

// ExpiredQuestionReply is what a requesting Bot hears when the question it
// waited on expired before the person answered it.
func ExpiredQuestionReply(question string) string {
	return "The question for the user expired before the user answered it, so there is no user decision.\nQuestion: " + strings.TrimSpace(question)
}

// WithdrawnQuestionReply is what a requesting Bot receives when the Bot it
// asked withdraws the question it was waiting on the person for.
func WithdrawnQuestionReply(question, reason string) string {
	return "The question for the user was withdrawn before the user answered it, so there is no user decision.\nQuestion: " + strings.TrimSpace(question) + "\nReason: " + strings.TrimSpace(reason)
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
	if len(text) > MaxDecisionAnswerBytes {
		return "", fail(CapacityFull, "decision_answer_bytes")
	}
	return text, nil
}

// CheckDecision admits an answer only to a pending, unexpired offer in the
// chat it was raised in, while the Bot is visible. The answer is the
// person's message to the conversation, which outlives a native binding a
// login or quota fallback revoked; the send fences execution itself.
func CheckDecision(offer PermissionOffer, answer PermissionAnswer, bot Bot, now time.Time) error {
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
	if bot.Hidden {
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
