// Package projectassistant contains effect-free rules for persistent Bots.
// The source machine owns these facts; a Cloud roster is only a projection.
package projectassistant

import "time"

const (
	ContractVersion     = "project-assistants.v1"
	FixtureVersion      = 1
	SchemaVersion       = 1
	MaxNameRunes        = 128
	MaxInstructionBytes = 16 << 10
	MaxMessageBytes     = 32 << 10
	// Pictures one message may carry, and each one's size after normalizing.
	MaxAttachmentsPerMessage = 4
	MaxAttachmentBytes       = 12 << 20
	// Live memory entries one Bot keeps (the person's and its own), and the
	// matches one memory search returns.
	MaxMemoryEntries       = 256
	MaxMemorySearchResults = 30
	MaxPendingTurns        = 16
	MaxPageRows            = 100
	MaxEventPageRows       = 256
)

type BotKey struct {
	MachineID string `json:"machine_id"`
	BotID     string `json:"bot_id"`
}

type ProjectKind string

const (
	RepositoryProject ProjectKind = "repository"
	PlaceProject      ProjectKind = "place"
)

// ProjectID is the registered Project on MachineID. CanonicalID identifies the
// repository shared by linked worktrees, or the existing non-repository place.
// Display names and client-supplied paths never establish identity.
type ProjectScope struct {
	MachineID   string      `json:"machine_id"`
	ProjectID   string      `json:"project_id"`
	CanonicalID string      `json:"canonical_id"`
	Kind        ProjectKind `json:"kind"`
}

type ProjectSelection struct {
	MachineID string `json:"machine_id"`
	ProjectID string `json:"project_id"`
}

// ProjectResolution is supplied by the source machine's existing Project
// resolver immediately before execution. It is never accepted from a client.
type ProjectResolution struct {
	Scope     ProjectScope
	Directory string
	Current   bool
	Available bool
}

type Agent string

const (
	ClaudeCode Agent = "claude"
	Codex      Agent = "codex"
)

// Shape values preserve Codync's character vocabulary, rendered as outlines.
type Shape string

const (
	Blob       Shape = "blob"
	Squircle   Shape = "squircle"
	Wedge      Shape = "wedge"
	Pebble     Shape = "pebble"
	Tablet     Shape = "tablet"
	Hex        Shape = "hex"
	Cloud      Shape = "cloud"
	Teardrop   Shape = "teardrop"
	EntryShape       = Blob
	EntryColor       = "#e39567"
)

type Appearance struct {
	Shape Shape  `json:"shape"`
	Color string `json:"color"`
}

type PermissionPolicy string

const (
	Ask      PermissionPolicy = "ask"
	AutoRead PermissionPolicy = "auto_read"
	// Approve is Codync's "Approve automatically": each request is allowed
	// with the agent's allow-once option; the agent's own settings still apply.
	Approve PermissionPolicy = "approve"
)

// Instructions and RoleSnapshotRef are versioned together. Running turns keep
// their old snapshot; the next idle turn captures the latest revision.
type InstructionSnapshot struct {
	Revision        int64  `json:"revision"`
	Text            string `json:"text"`
	RoleSnapshotRef string `json:"role_snapshot_ref"`
}
type Bot struct {
	Key              BotKey           `json:"key"`
	Project          ProjectScope     `json:"project"`
	ConversationID   string           `json:"conversation_id"`
	Name             string           `json:"name"`
	Appearance       Appearance       `json:"appearance"`
	Agent            Agent            `json:"agent"`
	PermissionPolicy PermissionPolicy `json:"permission_policy"`
	// Mode is the agent mode the Bot works in, as in Codync: AutoMode (or
	// empty) is the agent's own Auto mode, otherwise one of AgentModes.
	Mode         string              `json:"mode,omitempty"`
	Instructions InstructionSnapshot `json:"instructions"`
	Version      int64               `json:"version"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type CreateBot struct {
	Target           ProjectSelection `json:"target"`
	Name             string           `json:"name"`
	Appearance       Appearance       `json:"appearance"`
	Agent            Agent            `json:"agent"`
	PermissionPolicy PermissionPolicy `json:"permission_policy"`
	Mode             string           `json:"mode,omitempty"`
	Instructions     string           `json:"instructions"`
	RoleSnapshotRef  string           `json:"role_snapshot_ref"`
}

// EditBot is a complete replacement of the three editable settings; Agent,
// model, Project and permission policy are intentionally not patchable in v1.
type EditBot struct {
	ExpectedVersion int64      `json:"expected_version"`
	Name            string     `json:"name"`
	Appearance      Appearance `json:"appearance"`
	Instructions    string     `json:"instructions"`
	// An empty policy leaves the permission setting as it is, so a console
	// older than this setting cannot reset it.
	PermissionPolicy PermissionPolicy `json:"permission_policy,omitempty"`
	Mode             string           `json:"mode,omitempty"`
}

type Conversation struct {
	Key            BotKey `json:"key"`
	ID             string `json:"id"`
	Generation     int64  `json:"generation"`
	BindingID      string `json:"binding_id"`
	LastMessageSeq int64  `json:"last_message_seq"`
	MemoryRevision int64  `json:"memory_revision"`
	ActiveTurnID   string `json:"active_turn_id"`
	Version        int64  `json:"version"`
}

type TurnState string

const (
	TurnAccepted          TurnState = "accepted"
	TurnQueued            TurnState = "queued"
	TurnRunning           TurnState = "running"
	TurnWaitingPermission TurnState = "waiting_permission"
	TurnSucceeded         TurnState = "succeeded"
	TurnFailed            TurnState = "failed"
	TurnCancelled         TurnState = "cancelled"
	TurnUnknown           TurnState = "unknown"
)

// Fence is checked against authoritative state for every incoming RPC, tool
// response, permission answer and event. A process restart changes Generation.
type Fence struct {
	Key            BotKey       `json:"key"`
	Project        ProjectScope `json:"project"`
	ConversationID string       `json:"conversation_id"`
	BindingID      string       `json:"binding_id"`
	Generation     int64        `json:"generation"`
}
type Turn struct {
	ID             string              `json:"id"`
	Fence          Fence               `json:"fence"`
	MessageID      string              `json:"message_id"`
	State          TurnState           `json:"state"`
	Instructions   InstructionSnapshot `json:"instructions"`
	MemoryRevision int64               `json:"memory_revision"`
	IdempotencyKey string              `json:"idempotency_key"`
	Failure        *Failure            `json:"failure,omitempty"`
	// StartedAt is when the turn began running; absent while it waits.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// JoinedTurnID names the running turn this queued message was sent into
	// with Send now, as Codync's steer; the message has no reply of its own.
	JoinedTurnID string `json:"joined_turn_id,omitempty"`
}

type ExecutionBinding struct {
	Fence               Fence  `json:"fence"`
	Agent               Agent  `json:"agent"`
	NativeSessionID     string `json:"native_session_id"`
	MemoryRevision      int64  `json:"memory_revision"`
	ForgottenThrough    int64  `json:"forgotten_through"`
	InstructionRevision int64  `json:"instruction_revision"`
	Revoked             bool   `json:"revoked"`
}

type MessageKind string

const (
	UserMessage      MessageKind = "user"
	AssistantMessage MessageKind = "assistant"
	WorkMessage      MessageKind = "work"
	NoticeMessage    MessageKind = "notice"
)

type Message struct {
	ID             string      `json:"id"`
	Fence          Fence       `json:"fence"`
	Seq            int64       `json:"seq"`
	TurnID         string      `json:"turn_id"`
	Kind           MessageKind `json:"kind"`
	Text           string      `json:"text"`
	QuoteMessageID string      `json:"quote_message_id"`
	At             time.Time   `json:"at"`
	// Attachments are pictures sent with the message, kept by content
	// digest; the model receives them as native image blocks.
	Attachments []Attachment `json:"attachments,omitempty"`
}
type SendMessage struct {
	Fence          Fence        `json:"fence"`
	Text           string       `json:"text"`
	QuoteMessageID string       `json:"quote_message_id"`
	Attachments    []Attachment `json:"attachments,omitempty"`
}

// Attachment is one stored picture: its SHA-256 (also its file name), media
// type and size. The bytes live beside the Store, never in the aggregate.
type Attachment struct {
	ID        string `json:"id"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
}

type PermissionOption struct {
	ID       string               `json:"id"`
	Label    string               `json:"label"`
	ReadOnly bool                 `json:"read_only"`
	Allow    bool                 `json:"allow"`
	Kind     PermissionOptionKind `json:"kind"`
}

type PermissionOptionKind string

const (
	AllowOnce    PermissionOptionKind = "allow_once"
	RejectOnce   PermissionOptionKind = "reject_once"
	AllowAlways  PermissionOptionKind = "allow_always"
	RejectAlways PermissionOptionKind = "reject_always"
)

// PermissionAction is what a native tool asks to do, as the adapter reported
// it: shown to the person before they answer, never an authorization.
type PermissionAction struct {
	Title     string           `json:"title"`
	ToolKind  string           `json:"tool_kind"`
	Detail    string           `json:"detail"`
	Diffs     []PermissionDiff `json:"diffs"`
	Shortened bool             `json:"shortened"`
}

type PermissionDiff struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type PermissionOffer struct {
	Action           *PermissionAction  `json:"action,omitempty"`
	ID               string             `json:"id"`
	Fence            Fence              `json:"fence"`
	TurnID           string             `json:"turn_id"`
	NativeRequestID  string             `json:"native_request_id"`
	Options          []PermissionOption `json:"options"`
	ExpiresAt        time.Time          `json:"expires_at"`
	State            PermissionState    `json:"state"`
	SelectedOptionID string             `json:"selected_option_id"`
}

type PermissionState string

const (
	PermissionPending   PermissionState = "pending"
	PermissionResolved  PermissionState = "answered"
	PermissionExpired   PermissionState = "expired"
	PermissionCancelled PermissionState = "cancelled"
	PermissionUnknown   PermissionState = "unknown"
)

type PermissionAnswer struct {
	Fence    Fence  `json:"fence"`
	OfferID  string `json:"offer_id"`
	OptionID string `json:"option_id"`
	TurnID   string `json:"turn_id"`
}

type PermissionContext struct {
	Fence          Fence
	ActiveTurnID   string
	BindingRevoked bool
	Now            time.Time
	Source         SourceStatus
	Feature        FeatureState
}

type WorkState string

const (
	WorkAccepted WorkState = "accepted"
	// WorkWaitingForPerson is a started worker whose first screen holds a
	// dialog, such as a folder trust question, that only the person answers.
	WorkWaitingForPerson WorkState = "waiting_for_person"
	WorkExecuting        WorkState = "executing"
	WorkFailed           WorkState = "failed"
	WorkCompleted        WorkState = "completed"
	WorkLanded           WorkState = "landed"
	WorkUnknown          WorkState = "unknown"
)

type WorkLink struct {
	ID              string    `json:"id"`
	Fence           Fence     `json:"fence"`
	TurnID          string    `json:"turn_id"`
	TaskID          string    `json:"task_id"`
	BoardItemID     string    `json:"board_item_id"`
	State           WorkState `json:"state"`
	ResultMessageID string    `json:"result_message_id"`
	LandingCommit   string    `json:"landing_commit"`
}

type MemorySource struct {
	ID       string       `json:"id"`
	Key      BotKey       `json:"key"`
	Project  ProjectScope `json:"project"`
	Kind     string       `json:"kind"`
	Revision int64        `json:"revision"`
}
type MemoryRevision struct {
	Key                BotKey       `json:"key"`
	Project            ProjectScope `json:"project"`
	Revision           int64        `json:"revision"`
	ForgottenThrough   int64        `json:"forgotten_through"`
	ForgottenSourceIDs []string     `json:"forgotten_source_ids"`
}

type MemoryEntry struct {
	ID        string         `json:"id"`
	Key       BotKey         `json:"key"`
	Project   ProjectScope   `json:"project"`
	Text      string         `json:"text"`
	Sources   []MemorySource `json:"sources"`
	Revision  int64          `json:"revision"`
	Forgotten bool           `json:"forgotten"`
}

// A summary is usable only when its provenance is complete. Histories remain
// readable by people after forgetting; they are not automatically model input.
type ContextSnapshot struct {
	Memory             MemoryRevision `json:"memory"`
	Text               string         `json:"text"`
	Sources            []MemorySource `json:"sources"`
	ProvenanceComplete bool           `json:"provenance_complete"`
}

type SourceState string

const (
	SourceOnline      SourceState = "online"
	SourceOffline     SourceState = "offline"
	SourceUnsupported SourceState = "unsupported"
	SourceUnpaired    SourceState = "unpaired"
	SourceRevoked     SourceState = "revoked"
)

type SourceStatus struct {
	MachineID    string         `json:"machine_id"`
	State        SourceState    `json:"state"`
	Readable     bool           `json:"readable"`
	Writable     bool           `json:"writable"`
	Fresh        bool           `json:"fresh"`
	ObservedAt   time.Time      `json:"observed_at"`
	Access       Access         `json:"access"`
	Capabilities []Capabilities `json:"capabilities"`
}
type ReadCursor struct {
	Key            BotKey `json:"key"`
	ConversationID string `json:"conversation_id"`
	ReaderID       string `json:"reader_id"`
	MessageSeq     int64  `json:"message_seq"`
}
type BotSummary struct {
	Bot                Bot          `json:"bot"`
	LatestMessage      string       `json:"latest_message"`
	Activity           string       `json:"activity"`
	ActivityAt         time.Time    `json:"activity_at"`
	UnreadCount        int64        `json:"unread_count"`
	ReadCursor         ReadCursor   `json:"read_cursor"`
	PendingPermissions int64        `json:"pending_permissions"`
	Version            int64        `json:"version"`
	Source             SourceStatus `json:"source"`
}
type ListBots struct {
	MachineID string `json:"machine_id"`
	Query     string `json:"query"`
	Cursor    string `json:"cursor"`
	PageSize  int    `json:"page_size"`
}
type BotPage struct {
	Items      []BotSummary   `json:"items"`
	Sources    []SourceStatus `json:"sources"`
	NextCursor string         `json:"next_cursor"`
}

type EventKind string

const (
	BotCreated         EventKind = "bot_created"
	BotUpdated         EventKind = "bot_updated"
	MessageCreated     EventKind = "message_created"
	TurnUpdated        EventKind = "turn_updated"
	PermissionOffered  EventKind = "permission_offered"
	PermissionAnswered EventKind = "permission_answered"
	WorkUpdated        EventKind = "work_updated"
	MemoryChanged      EventKind = "memory_changed"
	ReadAdvanced       EventKind = "read_advanced"
	TraceUpdated       EventKind = "trace_updated"
	SourceUpdated      EventKind = "source_updated"
	FeatureDisabled    EventKind = "feature_disabled"
)

type Trace struct {
	TurnID   string `json:"turn_id"`
	Category string `json:"category"`
	Text     string `json:"text"`
	Complete bool   `json:"complete"`
}

// Event cursors are source-local durable sequences, not message sequences.
// Trace, permission and work events do not increment unread message counts.
type Event struct {
	Version    string           `json:"version"`
	ID         string           `json:"id"`
	Seq        int64            `json:"seq"`
	Fence      Fence            `json:"fence"`
	Kind       EventKind        `json:"kind"`
	At         time.Time        `json:"at"`
	Message    *Message         `json:"message,omitempty"`
	Turn       *Turn            `json:"turn,omitempty"`
	Permission *PermissionOffer `json:"permission,omitempty"`
	Work       *WorkLink        `json:"work,omitempty"`
	Memory     *MemoryRevision  `json:"memory,omitempty"`
	Bot        *BotSummary      `json:"bot,omitempty"`
	Read       *ReadCursor      `json:"read,omitempty"`
	Trace      *Trace           `json:"trace,omitempty"`
	Source     *SourceStatus    `json:"source,omitempty"`
}
type EventQuery struct {
	Key      BotKey
	AfterSeq int64
	PageSize int
}
type EventPage struct {
	Items   []Event
	NextSeq int64
	HasMore bool
}

type EffectState string

const (
	EffectAccepted  EffectState = "accepted"
	EffectExecuting EffectState = "executing"
	EffectUnknown   EffectState = "unknown"
	EffectDelivered EffectState = "delivered"
	EffectObserved  EffectState = "observed"
	EffectRead      EffectState = "read"
	EffectFailed    EffectState = "failed"
)

type Operation string

const (
	CreateOperation     Operation = "create"
	EditOperation       Operation = "edit"
	SendOperation       Operation = "send"
	StopOperation       Operation = "stop"
	SendNowOperation    Operation = "send_now"
	AnswerOperation     Operation = "answer"
	ReadOperation       Operation = "read"
	ObserveOperation    Operation = "observe"
	ForgetOperation     Operation = "forget"
	MemorySaveOperation Operation = "memory_save"
	DispatchOperation   Operation = "dispatch"
)

type Command struct {
	Key            BotKey    `json:"key"`
	Operation      Operation `json:"operation"`
	IdempotencyKey string    `json:"idempotency_key"`
	// SHA-256 of the canonical validated command, excluding transport metadata.
	ContentHash string `json:"content_hash"`
}
type Receipt struct {
	Command    Command     `json:"command"`
	ID         string      `json:"id"`
	State      EffectState `json:"state"`
	ResourceID string      `json:"resource_id"`
	EventSeq   int64       `json:"event_seq"`
}

type NativeMemoryMode string

const (
	NativeMemoryUnknown  NativeMemoryMode = "unknown"
	NativeMemoryDisabled NativeMemoryMode = "disabled"
	NativeMemoryIsolated NativeMemoryMode = "isolated"
	NativeMemoryUnsafe   NativeMemoryMode = "unsafe"
)

// Capability facts must be backed by a local protocol/version probe and a
// write-denial test. An installed executable or login alone proves neither.
type Capabilities struct {
	Key                  BotKey           `json:"key"`
	Project              ProjectScope     `json:"project"`
	Agent                Agent            `json:"agent"`
	CLIVersion           string           `json:"cli_version"`
	AdapterVersion       string           `json:"adapter_version"`
	Evidence             string           `json:"evidence"`
	ReadOnlyEnforced     bool             `json:"read_only_enforced"`
	NativeMemory         NativeMemoryMode `json:"native_memory"`
	NativeMemoryVerified bool             `json:"native_memory_verified"`
	Resume               bool             `json:"resume"`
	SendMessage          bool             `json:"send_message"`
	Unavailable          *Failure         `json:"unavailable,omitempty"`
}
