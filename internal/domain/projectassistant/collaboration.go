package projectassistant

import (
	"strings"
	"unicode/utf8"
)

// Origins are durable authority facts. Text never selects a tool policy.
type TurnOrigin string

const (
	UserOrigin                   TurnOrigin = "user"
	HostNoticeOrigin             TurnOrigin = "host_notice"
	BotRequestOrigin             TurnOrigin = "bot_request"
	BotReplyOrigin               TurnOrigin = "bot_reply"
	MaxCollaborationOutstanding             = 64
	MaxCollaborationPerSender               = 16
	MaxCollaborationHops                    = 8
	MaxCollaborationRequestBytes            = 32000
	MaxCollaborationReplyBytes              = 4000
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
	Detail          string           `json:"detail,omitempty"`
	Outcome         ExchangeOutcome  `json:"outcome"`
	Revision        int64            `json:"revision"`
	Hops            int              `json:"hops"`
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

func (x Exchange) Outstanding() bool { return x.Outcome == ExchangeQueued || x.Outcome == ExchangeSent }

// Stored replies are bounded at a UTF-8 boundary; unknown diagnostic evidence
// is not passed through this function or silently shortened.
func CollaborationReply(text string) string {
	if len(text) <= MaxCollaborationReplyBytes {
		return text
	}
	text = text[:MaxCollaborationReplyBytes]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

func ValidateTurnOrigin(t Turn) error {
	if (t.Origin == NetworkRequestOrigin) != (t.Network != nil) {
		return fail(Invalid, "network_origin")
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
