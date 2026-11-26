package projectassistant

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Origins are durable authority facts. Text never selects a tool policy.
type TurnOrigin string

const (
	UserOrigin       TurnOrigin = "user"
	HostNoticeOrigin TurnOrigin = "host_notice"
	BotRequestOrigin TurnOrigin = "bot_request"
	BotReplyOrigin   TurnOrigin = "bot_reply"
	// CompactOrigin is the person's "Compact now": one idle turn whose only
	// prompt is the agent's own /compact command (docs/bots.md).
	CompactOrigin                TurnOrigin = "compact"
	MaxCollaborationOutstanding             = 64
	MaxCollaborationPerSender               = 16
	MaxCollaborationHops                    = 8
	MaxCollaborationRequestBytes            = 32000
	MaxCollaborationReplyBytes              = 32000
	// A Bot handling another Bot's request may tell its own person a few
	// short things in its main chat; more is refused so it cannot flood it.
	MaxPeerNotesPerRequest = 3
	MaxPeerNoteBytes       = 2000
	// A follow-up quotes at most this many earlier rounds of the same pair,
	// newest first, and at most this many request and reply bytes in total.
	// One round always fits: it is bounded by one request and one reply.
	MaxCollaborationQuoteRounds = 4
	MaxCollaborationQuoteBytes  = 3 * (MaxCollaborationRequestBytes + MaxCollaborationReplyBytes) / 2
)

type ExchangeOutcome string

const (
	ExchangeQueued    ExchangeOutcome = "queued"
	ExchangeSent      ExchangeOutcome = "sent"
	ExchangeCompleted ExchangeOutcome = "completed"
	ExchangeFailed    ExchangeOutcome = "failed"
	ExchangeCancelled ExchangeOutcome = "cancelled"
	ExchangeUnknown   ExchangeOutcome = "unknown"
	// ExchangeNeedsPerson: the recipient finished its turn by asking the
	// person; the requester is answered after the person decides.
	ExchangeNeedsPerson ExchangeOutcome = "needs_person"
)

// ExchangeEndpoint is a conversation snapshot, never a capability or native ID.
type ExchangeEndpoint struct {
	Key            BotKey `json:"key"`
	ConversationID string `json:"conversation_id"`
}
type Exchange struct {
	ID              string           `json:"id"`
	Project         ProjectScope     `json:"project"`
	TargetProject   ProjectScope     `json:"target_project,omitzero"`
	Source          ExchangeEndpoint `json:"source"`
	Target          ExchangeEndpoint `json:"target"`
	SourceTurnID    string           `json:"source_turn_id"`
	RequestTurnID   string           `json:"request_turn_id"`
	RequestWorkerID string           `json:"request_worker_id,omitempty"`
	ReplyTurnID     string           `json:"reply_turn_id,omitempty"`
	Request         string           `json:"request"`
	Reply           string           `json:"reply,omitempty"`
	RequestAt       time.Time        `json:"request_at,omitzero"`
	ReplyAt         time.Time        `json:"reply_at,omitzero"`
	Detail          string           `json:"detail,omitempty"`
	Outcome         ExchangeOutcome  `json:"outcome"`
	Revision        int64            `json:"revision"`
	Hops            int              `json:"hops"`
	// ReplyTo names the earlier exchange of the same pair this request
	// follows up, as the requester chose it with reply_to_exchange_id.
	ReplyTo string `json:"reply_to,omitempty"`
	// Delivery concerns the source reply turn; it does not assert observation/read.
	Delivery string `json:"delivery,omitempty"`
}

// DestinationProject preserves the shared scope on exchanges stored before
// the sender and recipient could have separate workspaces.
func (x Exchange) DestinationProject() ProjectScope {
	if x.TargetProject == (ProjectScope{}) {
		return x.Project
	}
	return x.TargetProject
}

func (x Exchange) ParticipantProject(key BotKey) ProjectScope {
	switch key {
	case x.Source.Key:
		return x.Project
	case x.Target.Key, x.Recipient().Key:
		return x.DestinationProject()
	default:
		return ProjectScope{}
	}
}

// A direct request from the person lets the Lead also reach hidden Bots.
// Every recipient uses its own workspace, memory and permissions.
func MayContactBot(source, target Bot, turn Turn) bool {
	return source.Key != target.Key && source.Key.MachineID == target.Key.MachineID && !source.Hidden &&
		(!target.Hidden || source.Lead && DirectUserTurn(turn))
}

// Recipient identifies the execution conversation. Target remains the visible
// participant for projection, notification and exchange authorization.
func (x Exchange) Recipient() ExchangeEndpoint {
	if x.RequestWorkerID == "" {
		return x.Target
	}
	return ExchangeEndpoint{Key: BotKey{MachineID: x.Target.Key.MachineID, BotID: x.RequestWorkerID}, ConversationID: x.ID}
}

type BotDirectoryQuery struct {
	Query    string `json:"query,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}
type BotDirectoryEntry struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Agent Agent  `json:"agent"`
}
type BotDirectoryPage struct {
	Bots       []BotDirectoryEntry `json:"bots"`
	NextCursor string              `json:"next_cursor,omitempty"`
}
type BotMessageRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	TargetBotID    string `json:"target_bot_id"`
	Text           string `json:"text"`
	// ReplyToExchangeID quotes an earlier finished exchange this Bot sent to
	// the same target. Without it the request carries no earlier round.
	ReplyToExchangeID string `json:"reply_to_exchange_id,omitempty"`
}

func CheckCollaborationRequest(q BotMessageRequest, origin TurnOrigin, hops int) error {
	if IsPeerRequest(origin) {
		return fail(ScopeDenied, "bot_request_tools")
	}
	if hops < 0 || hops >= MaxCollaborationHops {
		return fail(CapacityFull, "bot_hops")
	}
	if q.IdempotencyKey == "" || q.TargetBotID == "" || !utf8.ValidString(q.Text) || strings.TrimSpace(q.Text) == "" {
		return fail(Invalid, "bot_request")
	}
	if len(q.Text) > MaxCollaborationRequestBytes {
		return fail(CapacityFull, "bot_request_bytes")
	}
	return nil
}

// CheckReplyTo admits a follow-up only for the requester of an earlier
// exchange with the same target whose reply is final. The quote is built from
// stored records, so text a sender writes can never stand in for it.
func CheckReplyTo(prior Exchange, source, target BotKey) error {
	if prior.Source.Key != source {
		return fail(ScopeDenied, "reply_to_exchange_requester")
	}
	if prior.Target.Key != target {
		return fail(ScopeDenied, "reply_to_exchange_pair")
	}
	switch prior.Outcome {
	case ExchangeCompleted, ExchangeFailed, ExchangeCancelled, ExchangeUnknown:
		return nil
	case ExchangeNeedsPerson:
		return fail(ScopeDenied, "reply_to_exchange_waiting_person")
	default:
		return fail(ScopeDenied, "reply_to_exchange_open")
	}
}

// QuoteRounds keeps the newest earlier rounds within the round and byte
// budget. chain starts with the referenced exchange and continues through
// each one's ReplyTo; it stops at the first round that does not fit or that
// is not a finished round of the same pair. omitted reports a cut chain.
func QuoteRounds(chain []Exchange, source, target BotKey) (kept []Exchange, omitted bool) {
	total := 0
	for _, x := range chain {
		size := len(x.Request) + len(x.Reply)
		if len(kept) == MaxCollaborationQuoteRounds || CheckReplyTo(x, source, target) != nil || total+size > MaxCollaborationQuoteBytes {
			return kept, true
		}
		total += size
		kept = append(kept, x)
	}
	return kept, len(kept) > 0 && kept[len(kept)-1].ReplyTo != ""
}

func (x Exchange) Outstanding() bool { return x.Outcome == ExchangeQueued || x.Outcome == ExchangeSent }

// ReplyTruncatedMarker begins the line CollaborationReply appends when it had
// to cut a reply, so every reader — the stored exchange, the requester's
// prompt, the console and a network receipt — sees that the text is partial.
const ReplyTruncatedMarker = "[Clawdline: reply truncated"

// ReplyUsage is what the reply bound has done since this process started:
// the largest reply it stored and how many replies it had to cut.
type ReplyUsage struct{ Largest, Truncated int64 }

var replyLargest, replyTruncated atomic.Int64

// BoundUsage is what one per-message bound has seen since this process
// started: the largest value it admitted and how many it refused.
type BoundUsage struct{ Largest, Refused int64 }

type boundUsage struct{ largest, refused atomic.Int64 }

// admit counts n against limit and reports whether it fits.
func (u *boundUsage) admit(n, limit int) bool {
	if n > limit {
		u.refused.Add(1)
		return false
	}
	for {
		old := u.largest.Load()
		if int64(n) <= old || u.largest.CompareAndSwap(old, int64(n)) {
			return true
		}
	}
}

func (u *boundUsage) usage() BoundUsage {
	return BoundUsage{Largest: u.largest.Load(), Refused: u.refused.Load()}
}

var peerNoteBytes, peerNotesPerRequest boundUsage

// PeerNoteBytesFit reports whether a peer note of n UTF-8 bytes is within
// MaxPeerNoteBytes, and counts it for bots.peer_note_bytes.
func PeerNoteBytesFit(n int) bool { return peerNoteBytes.admit(n, MaxPeerNoteBytes) }

// PeerNoteFits reports whether one more peer note fits a request that has
// already sent sent of them, and counts it for bots.peer_notes_per_request.
func PeerNoteFits(sent int) bool { return peerNotesPerRequest.admit(sent+1, MaxPeerNotesPerRequest) }

// PeerNoteBytesUsage is the diagnostics reading for bots.peer_note_bytes.
func PeerNoteBytesUsage() BoundUsage { return peerNoteBytes.usage() }

// PeerNotesPerRequestUsage is the diagnostics reading for bots.peer_notes_per_request.
func PeerNotesPerRequestUsage() BoundUsage { return peerNotesPerRequest.usage() }

// CollaborationReplyUsage is the diagnostics reading for bots.review_reply_bytes.
func CollaborationReplyUsage() ReplyUsage {
	return ReplyUsage{Largest: replyLargest.Load(), Truncated: replyTruncated.Load()}
}

func noteReply(n int) {
	for {
		old := replyLargest.Load()
		if int64(n) <= old || replyLargest.CompareAndSwap(old, int64(n)) {
			return
		}
	}
}

// CollaborationReply bounds a stored reply at a UTF-8 boundary. A reply over
// the bound keeps its beginning and ends with a marker naming the original and
// kept lengths, so nobody mistakes the kept part for the whole answer.
func CollaborationReply(text string) string {
	if len(text) <= MaxCollaborationReplyBytes {
		noteReply(len(text))
		return text
	}
	original := strconv.Itoa(len(text))
	// The marker's own digits are at most those of the original length.
	room := MaxCollaborationReplyBytes - len(replyTruncatedLine(original, original))
	kept := text[:room]
	for !utf8.ValidString(kept) {
		kept = kept[:len(kept)-1]
	}
	out := kept + replyTruncatedLine(original, strconv.Itoa(len(kept)))
	replyTruncated.Add(1)
	noteReply(len(out))
	return out
}

func replyTruncatedLine(original, kept string) string {
	return "\n\n" + ReplyTruncatedMarker + ": original " + original + " bytes, first " + kept + " bytes kept. The rest was not stored or delivered.]"
}

// ReplyTruncated reports whether a stored reply carries the truncation marker.
func ReplyTruncated(reply string) bool {
	return strings.Contains(reply, "\n\n"+ReplyTruncatedMarker+": original ")
}

func ValidateTurnOrigin(t Turn) error {
	if (t.Origin == NetworkRequestOrigin) != (t.Network != nil) {
		return fail(Invalid, "network_origin")
	}
	if t.CharterEdit != nil && t.Origin != UserOrigin {
		return fail(Invalid, "charter_edit_origin")
	}
	if t.CharterFix != nil && (t.Origin != UserOrigin || t.CharterEdit != nil) {
		return fail(Invalid, "charter_fix_origin")
	}
	switch t.Origin {
	case LeadCallOrigin:
		if !ValidLeadCallKey(t.IdempotencyKey) || t.ExchangeID != "" || t.Hops != 0 || t.RoutineRunID != "" || t.Room != nil {
			return fail(Invalid, "lead_call_origin")
		}
	case "", UserOrigin, HostNoticeOrigin:
		if t.ExchangeID != "" || t.Hops != 0 {
			return fail(Invalid, "turn_origin")
		}
	case CompactOrigin:
		if t.ExchangeID != "" || t.Hops != 0 || t.RoutineRunID != "" || t.Room != nil {
			return fail(Invalid, "compact_origin")
		}
	case NetworkRequestOrigin:
		if t.ExchangeID != "" || t.Hops != 0 || t.RoutineRunID != "" {
			return fail(Invalid, "network_origin")
		}
	case BotRequestOrigin, BotReplyOrigin:
		if t.ExchangeID == "" || t.Hops < 1 || t.Hops > MaxCollaborationHops || t.RoutineRunID != "" {
			return fail(Invalid, "turn_origin")
		}
	default:
		return fail(Invalid, "turn_origin")
	}
	return nil
}
