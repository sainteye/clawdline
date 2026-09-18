package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// One session speaking to another, and a session asking who it is.
//
// These are the two routes that make a fleet out of a list of tabs. Both are
// the machine's own — the orchestrator token, never a paired phone — because
// both put bytes into somebody's keyboard.

const messageTextLimit = 100_000

// Message is what one session sends another.
type Message struct {
	From string
	To   string
	Text string
}

// sessionMessage is the `clawdline.message` envelope, one physical line.
type sessionMessage struct {
	Protocol string        `json:"protocol"`
	Version  int           `json:"version"`
	Kind     string        `json:"kind"`
	Source   messageSource `json:"source"`
	Body     string        `json:"body"`
}

type messageSource struct {
	ID        string `json:"id"`
	Assistant string `json:"assistant"`
	Label     string `json:"label"`
}

// ScopeMessages is the receipt scope of the message route (D03).
const ScopeMessages = "orchestrator.messages"

// Relayed is a relayed message's answer.
type Relayed struct {
	At time.Time
	// Replayed is true when this request had already been answered and the
	// answer is that one, returned again rather than typed again.
	Replayed bool
}

// Relay types one session's message into another's composer — once per
// Idempotency-Key.
//
// The two lookups are deliberately different. A **source** may be named by its
// terminal id or by its conversation id, because the sender is describing
// itself and knows both. A **target** may be named only by terminal id: a
// message addressed to a conversation would follow that conversation into
// whichever tab currently holds it, and the sender meant the tab.
//
// The key used to be checked for presence and then forgotten, so a caller
// retrying a send it was unsure landed typed the message twice (G15). Now the
// key is a receipt (D03): the same key and body is the first answer again,
// the same key with a different body is refused, and the typing is an outbox
// effect recorded with the receipt — so a daemon that dies after accepting a
// message and before typing it types it after the restart, once, and a daemon
// that dies mid-typing says "unknown" rather than typing it again.
func (b *Broker) Relay(ctx context.Context, m Message, key string) (Relayed, error) {
	if m.From == "" || m.To == "" {
		return Relayed{}, refuse(http.StatusBadRequest, "bad_request",
			"The closed body needs only from_session, to_session, 0…100000 characters of text and optional images.")
	}
	if strings.TrimSpace(m.Text) == "" {
		return Relayed{}, refuse(http.StatusBadRequest, "bad_request",
			"A session message needs text or at least one local image.")
	}
	if utf8.RuneCountInString(m.Text) > messageTextLimit {
		return Relayed{}, refuse(http.StatusBadRequest, "bad_request",
			"The closed body needs only from_session, to_session, 0…100000 characters of text and optional images.")
	}
	if m.From == m.To {
		return Relayed{}, refuse(http.StatusConflict, "same_session",
			"A session message must go to a different session.")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Relayed{}, refuse(http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
	}

	receipt := store.ReceiptKey{Scope: ScopeMessages, Actor: "machine", Key: key}
	sum := sha256.Sum256([]byte(m.From + "\x00" + m.To + "\x00" + m.Text))
	claim, err := b.Store.ClaimReceipt(ctx, receipt, hex.EncodeToString(sum[:]), store.ReceiptPolicy{}, b.now())
	if err != nil {
		return Relayed{}, storeError(err)
	}
	if answered, err, done := b.claimed(ctx, receipt, claim); done {
		return answered, err
	}
	// From here this broker holds the reservation. Every refusal before the
	// intent is recorded gives it back: nothing happened, and a resend once
	// the target is there is still the first delivery.
	recorded := false
	defer func() {
		if !recorded {
			_ = b.Store.ReleaseReceipt(context.WithoutCancel(ctx), receipt)
		}
	}()

	var source session.Session
	found := 0
	for _, s := range b.Live(ctx) {
		if !s.IsAssistant() {
			continue
		}
		if s.ID == m.From || (s.ConversationID != "" && s.ConversationID == m.From) {
			source = s
			found++
		}
	}
	if found != 1 {
		return Relayed{}, refuse(http.StatusNotFound, "source_not_found",
			"No current assistant session has that terminal or conversation id.")
	}
	target, ok := b.sessionByTerminal(ctx, m.To)
	if !ok || !target.IsAssistant() {
		return Relayed{}, refuse(http.StatusNotFound, "target_not_found",
			"No current assistant session has that terminal id.")
	}
	// A target showing a menu would read the message as an answer to it. This
	// is the one refusal on this route that is about the far side's screen.
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		return Relayed{}, refuse(http.StatusConflict, "target_busy",
			"The target is showing a menu; typing would answer it instead of delivering the message.")
	}

	label := source.Label
	if label == "" {
		label = source.ID
	}
	encoded, err := json.Marshal(sessionMessage{
		Protocol: "clawdline.message",
		Version:  1,
		Kind:     "session_message",
		Source:   messageSource{ID: source.ID, Assistant: string(source.Assistant), Label: label},
		Body:     m.Text,
	})
	if err != nil || strings.ContainsAny(string(encoded), "\n\r") {
		return Relayed{}, refuse(http.StatusInternalServerError, "encoding_failed",
			"The session message could not be encoded safely.")
	}
	if b.Type == nil {
		return Relayed{}, refuse(http.StatusBadGateway, "delivery_failed",
			"this daemon cannot type into a terminal")
	}
	payload, _ := json.Marshal(messageEffect{Target: target.ID, Source: source.ID,
		Wire: "<clawdline-message>" + string(encoded) + "</clawdline-message>"})
	accepted, _ := json.Marshal(map[string]any{"from": source.ID, "key": key})
	ids, err := b.Store.RecordIntent(ctx,
		[]store.Event{{Kind: "session.message.accepted", Subject: target.ID, Payload: accepted}},
		[]store.Effect{{Kind: EffectMessage, Subject: target.ID, Payload: payload, Receipt: &receipt}})
	if err != nil {
		return Relayed{}, storeError(err)
	}
	recorded = true
	results := b.runRecorded(ctx, ids)
	if len(results) != 1 || results[0].answer == nil {
		outcome := ""
		if len(results) == 1 {
			outcome = results[0].outcome
		}
		return Relayed{}, refuse(http.StatusServiceUnavailable, "orchestrator_store_unavailable",
			"The message was accepted and its delivery could not be recorded: "+outcome)
	}
	return relayedOf(*results[0].answer, false)
}

// claimed answers a request whose receipt says it is not new, and done is
// false only for a new one.
func (b *Broker) claimed(ctx context.Context, receipt store.ReceiptKey, claim store.ReceiptClaim) (Relayed, error, bool) {
	switch claim.Outcome {
	case store.ReceiptNew:
		return Relayed{}, nil, false
	case store.ReceiptReplay:
		r, err := relayedOf(claim.Answer, true)
		return r, err, true
	case store.ReceiptMismatch:
		return Relayed{}, refuse(http.StatusConflict, "idempotency_key_reused",
			"This Idempotency-Key was already used for a different request; nothing was done. Use a new key."), true
	case store.ReceiptPending:
		return Relayed{}, refuseWith(http.StatusConflict, "request_in_progress",
			"The same request is being carried out now; ask again for its answer.",
			map[string]any{"retry_after": retrySeconds(claim.RetryAfter)}), true
	case store.ReceiptExpired:
		return Relayed{}, refuse(http.StatusConflict, "receipt_expired",
			"This Idempotency-Key's window has passed. The request was answered once and is not carried out "+
				"again; send it under a new key if it is still wanted."), true
	case store.ReceiptFull:
		return Relayed{}, refuseWith(http.StatusTooManyRequests, "receipts_full",
			"This route holds as many answered requests as it keeps inside their window; nothing was done.",
			map[string]any{"retry_after": retrySeconds(claim.RetryAfter)}), true
	case store.ReceiptOrphaned:
		unknown := refusalAnswer(refuse(http.StatusConflict, "request_outcome_unknown",
			"The daemon stopped while this request was being carried out, so whether it took effect is "+
				"unknown. It was not repeated; look before asking again under a new key."))
		if err := b.Store.CompleteReceipt(ctx, receipt, *unknown); err != nil {
			return Relayed{}, storeError(err), true
		}
		r, err := relayedOf(*unknown, false)
		return r, err, true
	}
	return Relayed{}, refuse(http.StatusInternalServerError, "receipt_unreadable", "The request's receipt said nothing this broker knows."), true
}

// relayedOf is a receipt's answer said again.
func relayedOf(a store.ReceiptAnswer, replayed bool) (Relayed, error) {
	if a.Status == http.StatusOK {
		var m messageAnswer
		if err := json.Unmarshal(a.Body, &m); err != nil {
			return Relayed{}, refuse(http.StatusInternalServerError, "receipt_unreadable", err.Error())
		}
		return Relayed{At: time.Unix(m.At, 0), Replayed: replayed}, nil
	}
	var r storedRefusal
	if err := json.Unmarshal(a.Body, &r); err != nil {
		return Relayed{}, refuse(http.StatusInternalServerError, "receipt_unreadable", err.Error())
	}
	return Relayed{}, Refusal{Status: a.Status, Code: r.Code, Message: r.Message, Extra: r.Extra}
}

func retrySeconds(d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		return 1
	}
	return s
}

// Identity is the answer to whoami.
type Identity struct {
	ConversationID string
	TerminalID     string
	Assistant      string
	Complete       bool
	ObservedAt     time.Time
}

// WhoAmI resolves a conversation id to the one terminal that proves it.
//
// The route answers five fields and nothing else — no task, no role, no secret.
// A caller asking "which tab am I" is asking about the machine; what it is
// working on is its own business and is not the broker's to hand out to
// whoever knows a conversation id.
func (b *Broker) WhoAmI(ctx context.Context, conversationID string) (Identity, error) {
	if conversationID == "" {
		return Identity{}, refuse(http.StatusBadRequest, "conversation_id_required",
			"The closed query needs exactly one conversation_id.")
	}
	if !isLowercaseUUID(conversationID) {
		return Identity{}, refuse(http.StatusBadRequest, "conversation_id_malformed",
			"conversation_id must be one lowercase UUID.")
	}
	s, err := b.terminalFor(ctx, conversationID, "")
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		ConversationID: s.ConversationID,
		TerminalID:     s.ID,
		Assistant:      string(s.Assistant),
		Complete:       true,
		ObservedAt:     b.now(),
	}, nil
}

// SessionDelivery is a root's own receipt: one sentence saying this turn
// delivered something.
//
// It is deliberately weaker than a landing and says so in its own vocabulary:
// it produces the check that means "delivered, awaiting approval", never
// "landed". Only a root may write one — a child reports through its task
// result, and letting it use this route would give one piece of work two
// completion signals with different evidence behind them.
type SessionDelivery struct {
	Terminal string    `json:"-"`
	Summary  string    `json:"title"`
	Scope    string    `json:"scope"`
	Evidence string    `json:"evidence"`
	At       time.Time `json:"-"`
	Created  bool      `json:"-"`
}

// ReportSessionDelivery records that a session's current turn delivered.
func (b *Broker) ReportSessionDelivery(ctx context.Context, terminalID, summary string) (SessionDelivery, error) {
	if terminalID == "" || strings.Contains(terminalID, "/") {
		return SessionDelivery{}, refuse(http.StatusBadRequest, "bad_request",
			"The route must name one session id.")
	}
	s, ok := b.sessionByTerminal(ctx, terminalID)
	if !ok || !s.IsAssistant() {
		return SessionDelivery{}, refuse(http.StatusNotFound, "session_not_found",
			"No current assistant session named "+terminalID+".")
	}
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > sessionSummaryLimit ||
		strings.ContainsRune(trimmed, 0) {
		return SessionDelivery{}, refuse(http.StatusBadRequest, "bad_request",
			"summary must be 1–500 characters without NUL.")
	}
	if s.ConversationID == "" {
		return SessionDelivery{}, refuse(http.StatusConflict, "session_unbound",
			"The current assistant process and conversation could not be bound.")
	}
	// A terminal that is a live Clawdline child reports through its result.
	records, err := b.records(ctx)
	if err != nil {
		return SessionDelivery{}, err
	}
	for _, r := range records {
		if r.ChildTerminalID == terminalID && !r.State.Terminal() {
			return SessionDelivery{}, refuse(http.StatusConflict, "child_session",
				"A Clawdline child reports through its task result, not this route.")
		}
	}
	at := b.now()
	payload, _ := json.Marshal(map[string]any{
		"terminal": terminalID, "conversation": s.ConversationID,
		"assistant": string(s.Assistant), "summary": trimmed, "at": at.Unix(),
	})
	if err := b.Store.Append(ctx, store.Event{
		Kind: "session.delivered", Subject: terminalID, Payload: payload,
	}); err != nil {
		return SessionDelivery{}, err
	}
	return SessionDelivery{
		Terminal: terminalID,
		Summary:  trimmed,
		Scope:    "session",
		Evidence: "authenticated_session_delivery",
		At:       at,
		Created:  true,
	}, nil
}

func isLowercaseUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	want := []int{8, 4, 4, 4, 12}
	groups := strings.Split(v, "-")
	if len(groups) != len(want) {
		return false
	}
	for i, g := range groups {
		if len(g) != want[i] {
			return false
		}
		for _, r := range g {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	return true
}

func quote(v string) string {
	body, _ := json.Marshal(v)
	return string(body)
}
