// Package projectassistant contains effect-free rules for persistent Bots.
// The source machine owns these facts; a Cloud roster is only a projection.
package projectassistant

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const (
	ContractVersion = "project-assistants.v1"
	FixtureVersion  = 1
	SchemaVersion   = 16
	MaxNameRunes    = 128
	// What a group works on, as Codync's group description: shown to the
	// person and quoted to each member in its room prompt.
	MaxGroupDescriptionRunes = 500
	MaxInstructionBytes      = 16 << 10
	MaxMessageBytes          = 32 << 10
	// A single user message must fit an escaped history/event record inside
	// the Cloud read envelope, alongside its identity and control metadata.
	MaxUserMessageBytes = 512 << 10
	// Maximum decoded size of one normalized picture.
	MaxAttachmentBytes = 12 << 20
	// Live memory entries one Bot keeps (the person's and its own), and the
	// matches one memory search returns.
	MaxMemoryEntries       = 256
	MaxMemorySearchResults = 30
	MaxAutomationBacklog   = 16
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
	PersonalWorkspace ProjectKind = "personal"
)

// ProjectID identifies a registered Project or a source-owned personal workspace.
// CanonicalID identifies the repository shared by linked worktrees, the existing
// non-repository place, or the exact personal workspace.
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

func (a Agent) Valid() bool { return a == ClaudeCode || a == Codex || a == Grok || a == Gemini }

const (
	ClaudeCode Agent = "claude"
	Codex      Agent = "codex"
	Grok       Agent = "grok"
	Gemini     Agent = "antigravity"
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

type ReplyInput string

const (
	ReplyInputQueue ReplyInput = "queue"
	ReplyInputSteer ReplyInput = "steer"
)

func (v ReplyInput) Effective() ReplyInput {
	if v == "" {
		return ReplyInputQueue
	}
	return v
}

func (v ReplyInput) Valid() bool { return v.Effective() == ReplyInputQueue || v == ReplyInputSteer }

type Bot struct {
	Extensions      ExtensionSelection `json:"extensions,omitempty,omitzero"`
	LoginProfile    string             `json:"login_profile,omitempty"`
	RuntimeSettings RuntimeSettings    `json:"runtime_settings,omitempty,omitzero"`
	Notify          string             `json:"notify,omitempty"`
	// WorkReview is "off" when finished, failed or waiting dispatched work
	// only publishes its result. Empty and "on" queue one review turn.
	WorkReview       string             `json:"work_review,omitempty"`
	ReplyInput       ReplyInput         `json:"reply_input,omitempty"`
	Key              BotKey             `json:"key"`
	Project          ProjectScope       `json:"project"`
	ConversationID   string             `json:"conversation_id"`
	Name             string             `json:"name"`
	Appearance       Appearance         `json:"appearance"`
	Agent            Agent              `json:"agent"`
	FallbackAgents   FallbackConfig     `json:"fallback_agents,omitempty,omitzero"`
	FallbackSteps    FallbackStepConfig `json:"fallback_steps,omitempty,omitzero"`
	PermissionPolicy PermissionPolicy   `json:"permission_policy"`
	// Mode is the agent mode the Bot works in, as in Codync: AutoMode (or
	// empty) is the agent's own Auto mode, otherwise one of AgentModes.
	Mode string `json:"mode,omitempty"`
	// Hidden keeps the Bot out of the list and its badge, as Codync's Hide
	// from list does; its chat, memory and settings stay.
	Hidden         bool   `json:"hidden,omitempty"`
	SidebarPinned  *bool  `json:"sidebar_pinned,omitempty"`
	SidebarSection string `json:"sidebar_section,omitempty"`
	Lead           bool   `json:"lead,omitempty"`
	// ManageTeam records the explicit team management grant independently of
	// the lead role. Managed creation can inherit this explicit grant.
	ManageTeam bool `json:"manage_team,omitempty"`
	// SettingsVersion changes on configuration writes independently of chat activity.
	SettingsVersion   int64 `json:"settings_version,omitempty"`
	BackgroundThreads int   `json:"background_threads,omitempty"`
	// CreatedBy names the manager Bot that created this one. Only the host
	// sets it, in its manager operation; no person or model request does.
	CreatedBy    *BotKey             `json:"created_by,omitempty"`
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
	FallbackAgents   []Agent          `json:"fallback_agents,omitempty"`
	FallbackSteps    []FallbackStep   `json:"fallback_steps,omitempty"`
	PermissionPolicy PermissionPolicy `json:"permission_policy"`
	Mode             string           `json:"mode,omitempty"`
	Instructions     string           `json:"instructions"`
	RoleSnapshotRef  string           `json:"role_snapshot_ref"`
	// ManageTeam is the person's switch; omitted (false) keeps the default.
	ManageTeam bool `json:"manage_team,omitempty"`
}

// EditBot preserves optional settings omitted by older clients; Project is fixed.
type RosterEdit struct {
	NewSession    bool    `json:"new_session,omitempty"`
	Pinned        *bool   `json:"pinned,omitempty"`
	Section       *string `json:"section,omitempty"`
	DesignateLead bool    `json:"designate_lead,omitempty"`
}

type EditBot struct {
	Roster          *RosterEdit         `json:"roster,omitempty"`
	Extensions      *ExtensionSelection `json:"extensions,omitempty"`
	LoginProfile    *string             `json:"login_profile,omitempty"`
	Agent           Agent               `json:"agent,omitempty"`
	RuntimeSettings *RuntimeSettings    `json:"runtime_settings,omitempty"`
	Notify          string              `json:"notify,omitempty"`
	WorkReview      string              `json:"work_review,omitempty"`
	ReplyInput      ReplyInput          `json:"reply_input,omitempty"`
	FallbackAgents  *[]Agent            `json:"fallback_agents,omitempty"`
	FallbackSteps   *[]FallbackStep     `json:"fallback_steps,omitempty"`
	ExpectedVersion int64               `json:"expected_version"`
	Name            string              `json:"name"`
	Appearance      Appearance          `json:"appearance"`
	Instructions    string              `json:"instructions"`
	// An empty policy leaves the permission setting as it is, so a console
	// older than this setting cannot reset it.
	PermissionPolicy PermissionPolicy `json:"permission_policy,omitempty"`
	Mode             string           `json:"mode,omitempty"`
	// ManageTeam nil leaves the switch as it is, so an older console cannot
	// reset it; only the person's edit reaches this field.
	ManageTeam        *bool `json:"manage_team,omitempty"`
	BackgroundThreads int   `json:"background_threads,omitempty"`
	// Base is the settings the editor started from. With it, a save conflicts
	// only on a field both this edit and another writer changed; without it
	// (older clients) ExpectedVersion must equal the Bot's version.
	Base *EditBase `json:"base,omitempty"`
	// OverwriteConflicts keeps this edit's value for every conflicting field.
	OverwriteConflicts bool `json:"overwrite_conflicts,omitempty"`
}

// EditBase holds the editable settings as the editor read them, in stored form.
type EditBase struct {
	Name              string
	Appearance        Appearance
	Instructions      string
	Agent             Agent
	PermissionPolicy  PermissionPolicy
	Mode              string
	RuntimeSettings   RuntimeSettings
	LoginProfile      string
	Extensions        ExtensionSelection
	FallbackAgents    []Agent
	FallbackSteps     []FallbackStep
	Notify            string
	ReplyInput        ReplyInput `json:",omitempty"`
	BackgroundThreads int
	ManageTeam        bool
}

// ContextSample is the last provider-reported usage after a successful turn.
type ContextSample struct {
	TurnID         string `json:"turn_id"`
	BindingID      string `json:"binding_id"`
	Generation     int64  `json:"generation"`
	MemoryRevision int64  `json:"memory_revision"`
	UsedTokens     int64  `json:"used_tokens"`
	WindowTokens   int64  `json:"window_tokens"`
}

type Conversation struct {
	ContextUsage           ContextSample `json:"context_usage,omitzero"`
	SessionResetGeneration int64         `json:"session_reset_generation,omitempty"`
	SessionResetAfter      int64         `json:"session_reset_after,omitempty"`
	MainBindingID          string        `json:"main_binding_id,omitempty"`
	Key                    BotKey        `json:"key"`
	ID                     string        `json:"id"`
	Generation             int64         `json:"generation"`
	BindingID              string        `json:"binding_id"`
	LastMessageSeq         int64         `json:"last_message_seq"`
	MemoryRevision         int64         `json:"memory_revision"`
	ActiveTurnID           string        `json:"active_turn_id"`
	Version                int64         `json:"version"`
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
	// QueueEditID identifies a paused, never-started human message. Only an
	// explicit save, cancel, or turn withdrawal releases this hold.
	QueueEditID string `json:"queue_edit_id,omitempty"`
	// Steering records a queued message whose Send now has no settled native response.
	Steering bool `json:"steering,omitempty"`
	// ReleasedRestart marks the person's message that released a queue held
	// by a turn a restart interrupted. It runs before automatic work already
	// waiting in that queue and first inspects what the interrupted turn did.
	ReleasedRestart bool         `json:"released_restart,omitempty"`
	Network         *NetworkWork `json:"network,omitempty"`
	Origin          TurnOrigin   `json:"origin,omitempty"`
	ExchangeID      string       `json:"exchange_id,omitempty"`
	// DecisionExchangeID marks the person's answer to a decision offer that
	// a peer request raised; its result is forwarded to the requester.
	DecisionExchangeID string              `json:"decision_exchange_id,omitempty"`
	Hops               int                 `json:"hops,omitempty"`
	Attempts           []AttemptFact       `json:"attempts,omitempty"`
	RoutineRunID       string              `json:"routine_run_id,omitempty"`
	ID                 string              `json:"id"`
	Fence              Fence               `json:"fence"`
	MessageID          string              `json:"message_id"`
	State              TurnState           `json:"state"`
	Instructions       InstructionSnapshot `json:"instructions"`
	MemoryRevision     int64               `json:"memory_revision"`
	IdempotencyKey     string              `json:"idempotency_key"`
	Failure            *Failure            `json:"failure,omitempty"`
	// StartedAt is when the turn began running; absent while it waits.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// JoinedTurnID names the running turn this queued message was sent into
	// with Send now, as Codync's steer; the message has no reply of its own.
	JoinedTurnID string `json:"joined_turn_id,omitempty"`
	// Room is the provenance of a group member's room turn: an ordinary turn
	// of the member's own conversation under its own fence, plus the ticket
	// the room handed it. The room line it publishes takes its Author from
	// Room.Member, never from text the model wrote.
	Room *RoomTicket `json:"room,omitempty"`
}

type ExecutionBinding struct {
	LeadRole string `json:"lead_role,omitempty"`
	// PrimarySupersededAt records the configuration revision that replaced
	// this binding's primary Agent. Only the source store writes this fact.
	PrimarySupersededAt int64  `json:"primary_superseded_at,omitempty"`
	LoginProfile        string `json:"login_profile,omitempty"`
	SessionSlot         string `json:"session_slot,omitempty"`
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

// CountsAsUnread reports whether a message raises the unread badge. As in
// Codync (host/src/store/bots.rs UNREAD_MESSAGE), only what the Bot finished
// saying to the person counts: a Bot reply or a work result. The person's own
// message never does, so a chat that is still working shows no badge; host
// notices are narration.
func CountsAsUnread(kind MessageKind) bool {
	return kind == AssistantMessage || kind == WorkMessage
}

type Message struct {
	Reactions      []ReactionActor `json:"reactions,omitempty"`
	Exchange       *Exchange       `json:"exchange,omitempty"`
	ID             string          `json:"id"`
	Fence          Fence           `json:"fence"`
	Seq            int64           `json:"seq"`
	TurnID         string          `json:"turn_id"`
	Kind           MessageKind     `json:"kind"`
	Text           string          `json:"text"`
	QuoteMessageID string          `json:"quote_message_id"`
	At             time.Time       `json:"at"`
	// Attachments are pictures sent with the message, kept by content
	// digest; the model receives them as native image blocks.
	Attachments []Attachment `json:"attachments,omitempty"`
	Files       []InputFile  `json:"files,omitempty"`
	Output      *Output      `json:"output,omitempty"`
	// Notice names a notice Clawdline wrote by key and values, so each
	// reader renders it in its own language. Text is the English fallback.
	Notice *NoticeRef `json:"notice,omitempty"`
}

// NoticeCompacted is the key of the notice for a turn during which the agent
// summarized its own context; the token counts are before and after.
const NoticeCompacted = "compacted"

const (
	NoticeRestartContinuation = "restart_continuation"
	NoticeWorkReview          = "work_review"
	NoticeWorkReviewHeld      = "work_review_held"
	NoticeWorkReviewWaiting   = "work_review_waiting"
	NoticePeerDispatch        = "peer_dispatch"
	NoticePeerDispatchFailed  = "peer_dispatch_failed"
)

// NoticeCleanupReleased is the key of the notice that a Bot was released
// although its earlier Agent processes could not all be confirmed stopped.
// Cause is "degraded" (bounded re-checks ran out) or "person".
const NoticeCleanupReleased = "cleanup_released"

// NoticeRef is a language-neutral notice: a key and its values.
type NoticeRef struct {
	Key                 string      `json:"key"`
	TokensBefore        int64       `json:"tokens_before,omitempty"`
	TokensAfter         int64       `json:"tokens_after,omitempty"`
	Cause               string      `json:"cause,omitempty"`
	Requester           string      `json:"requester,omitempty"`
	RequesterAppearance *Appearance `json:"requester_appearance,omitempty"`
	TaskTitle           string      `json:"task_title,omitempty"`
	FailureCode         string      `json:"failure_code,omitempty"`
	FailureField        string      `json:"failure_field,omitempty"`
}

// PeerDispatchAttribution carries the original requester's display snapshot
// from the host to the broker's admission record.
type PeerDispatchAttribution struct {
	Requester  string     `json:"requester,omitempty"`
	Appearance Appearance `json:"appearance,omitzero"`
	Language   string     `json:"language,omitempty"`
}
type SendMessage struct {
	Handoff        *HandoffRef  `json:"handoff,omitempty"`
	UploadIDs      []string     `json:"upload_ids,omitempty"`
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

// TeamPermissionKind identifies host-owned team mutations on the chat timeline.
func TeamPermissionKind(kind string) bool {
	return kind == "team_create" || kind == "team_update"
}

type PermissionOffer struct {
	AnchorMessageSeq int64              `json:"anchor_message_seq,omitempty"`
	Action           *PermissionAction  `json:"action,omitempty"`
	ID               string             `json:"id"`
	Fence            Fence              `json:"fence"`
	TurnID           string             `json:"turn_id"`
	NativeRequestID  string             `json:"native_request_id"`
	Options          []PermissionOption `json:"options"`
	// Zero means a native offer follows the life of its open turn.
	ExpiresAt        time.Time       `json:"expires_at"`
	State            PermissionState `json:"state"`
	SelectedOptionID string          `json:"selected_option_id"`
	// Kind is empty for a native tool permission and DecisionOfferKind for
	// a question a Bot asked the person with ask_person.
	Kind     string           `json:"kind,omitempty"`
	Decision *DecisionRequest `json:"decision,omitempty"`
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
	// Text is the person's own reply to a decision offer.
	Text string `json:"text,omitempty"`
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
	ID          string `json:"id"`
	Fence       Fence  `json:"fence"`
	TurnID      string `json:"turn_id"`
	TaskID      string `json:"task_id"`
	BoardItemID string `json:"board_item_id"`
	// BotRequestID is the Bot request card the task works on, when the
	// dispatch named one (docs/bot-request-cards.md §8.1).
	BotRequestID    string    `json:"bot_request_id,omitempty"`
	State           WorkState `json:"state"`
	ResultMessageID string    `json:"result_message_id"`
	LandingCommit   string    `json:"landing_commit"`
	Title           string    `json:"title,omitempty"`
	DeliveryCommit  string    `json:"delivery_commit,omitempty"`
	LandingTarget   string    `json:"landing_target,omitempty"`
	WorkerTerminal  string    `json:"worker_terminal,omitempty"`
	WorkerSession   string    `json:"worker_session,omitempty"`
	TargetProjectID string    `json:"target_project_id,omitempty"`
	Project         string    `json:"project,omitempty"`
	StartedAt       time.Time `json:"started_at,omitzero"`
	FinishedAt      time.Time `json:"finished_at,omitzero"`
	// Landing is the broker's landing state for the task (pending, landed,
	// incorporated, abandoned, nothing_to_land); empty when the task has no
	// landing record yet or the source is older.
	Landing string `json:"landing,omitempty"`
	// LandingNote is the broker's note on that landing, such as why it is
	// still pending.
	LandingNote string `json:"landing_note,omitempty"`
	// FailureReason is the broker's stable code for why the Session never
	// started; set only when its task ended spawn_failed.
	FailureReason string `json:"failure_reason,omitempty"`
}

// SettledLanding reports a landing state after which nothing more is owed.
func SettledLanding(state string) bool {
	return state == "landed" || state == "incorporated" || state == "abandoned" || state == "nothing_to_land"
}

// Unsettled reports a finished dispatch whose landing is still owed: it stays
// listed, without any time limit, until its landing settles. A completed link
// from an older source carries no landing state and stays listed as owed; a
// failed link is owed only when the broker opened a pending landing for it.
func (l WorkLink) Unsettled() bool {
	switch l.State {
	case WorkCompleted:
		return !SettledLanding(l.Landing)
	case WorkFailed:
		return l.Landing == "pending"
	}
	return false
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
	Kind      string         `json:"kind,omitempty"`
	Title     string         `json:"title,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty,omitzero"`
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
	MachineID           string         `json:"machine_id"`
	State               SourceState    `json:"state"`
	Readable            bool           `json:"readable"`
	Writable            bool           `json:"writable"`
	Fresh               bool           `json:"fresh"`
	ObservedAt          time.Time      `json:"observed_at"`
	Access              Access         `json:"access"`
	Capabilities        []Capabilities `json:"capabilities"`
	DevServersSupported bool           `json:"dev_servers_supported,omitempty"`
	LeadCallSupported   bool           `json:"lead_call_supported,omitempty"`
}
type ReadCursor struct {
	Key            BotKey `json:"key"`
	ConversationID string `json:"conversation_id"`
	ReaderID       string `json:"reader_id"`
	MessageSeq     int64  `json:"message_seq"`
}

// BotCompletion identifies a committed successful main-chat task, without its text.
type BotCompletion struct {
	TurnID         string `json:"turn_id"`
	ConversationID string `json:"conversation_id"`
}

// NotifiableCompletion excludes hidden Bots, peer requests, group members and joined inputs.
func NotifiableCompletion(bot Bot, turn Turn) bool {
	return (bot.Notify != "off") && !bot.Hidden && turn.State == TurnSucceeded && turn.Origin != BotRequestOrigin && !IsLeadCallTurn(turn) && turn.Room == nil && turn.JoinedTurnID == ""
}

type BackgroundWork struct {
	Working    int64 `json:"working"`
	Waiting    int64 `json:"waiting"`
	NeedsInput int64 `json:"needs_input"`
}

// DispatchedWork counts the broker Sessions a Bot dispatched, from its work
// links. Running includes accepted and executing work. Executing counts only
// work currently executing. NeedsYou is waiting for the person, and Delivered
// is finished work whose landing is still owed (Unsettled).
type DispatchedWork struct {
	Running   int64 `json:"running"`
	Executing int64 `json:"executing"`
	NeedsYou  int64 `json:"needs_you"`
	Delivered int64 `json:"delivered"`
}

// MaxCompletionWakeRetrySeconds is how long the broker keeps retrying to hand
// a finished dispatch back to its Bot while the Bot host is unavailable;
// registered as bots.completion_wake_retry_seconds. It used to also hide a
// delivered Session from the Bot's list after a day; that list now keeps
// every unsettled delivery until its landing settles.
const MaxCompletionWakeRetrySeconds = 24 * 60 * 60

// CompletionWakeRetryWindow is MaxCompletionWakeRetrySeconds as a duration.
const CompletionWakeRetryWindow = MaxCompletionWakeRetrySeconds * time.Second

type BotSummary struct {
	CompletedTurnID string `json:"completed_turn_id,omitempty"`
	Bot             Bot    `json:"bot"`
	// Status describes the Bot’s own chat. Activity carries a live tool title
	// or a localized key. BackgroundWork describes independent workers.
	Status        string   `json:"status,omitempty"`
	Failure       *Failure `json:"failure,omitempty"`
	LatestMessage string   `json:"latest_message"`
	// LatestMessageKind lets a list say where LatestMessage appears in the chat.
	LatestMessageKind  MessageKind `json:"latest_message_kind,omitempty"`
	Activity           string      `json:"activity"`
	ActivityAt         time.Time   `json:"activity_at"`
	UnreadCount        int64       `json:"unread_count"`
	ReadCursor         ReadCursor  `json:"read_cursor"`
	PendingPermissions int64       `json:"pending_permissions"`
	// ActiveWork counts work this Bot handed elsewhere that has not finished:
	// Board work links still accepted, waiting for the person or executing,
	// plus background tasks still waiting or active. Absent means none.
	ActiveWork     int64           `json:"active_work,omitempty"`
	BackgroundWork *BackgroundWork `json:"background_work,omitempty"`
	DispatchedWork *DispatchedWork `json:"dispatched_work,omitempty"`
	Version        int64           `json:"version"`
	Source         SourceStatus    `json:"source"`
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
	BotCreated          EventKind = "bot_created"
	BotUpdated          EventKind = "bot_updated"
	MessageCreated      EventKind = "message_created"
	QueuedMessageEdited EventKind = "queued_message_edited"
	ArtifactRenamed     EventKind = "artifact_renamed"
	TurnUpdated         EventKind = "turn_updated"
	PermissionOffered   EventKind = "permission_offered"
	PermissionAnswered  EventKind = "permission_answered"
	WorkUpdated         EventKind = "work_updated"
	MemoryChanged       EventKind = "memory_changed"
	ReadAdvanced        EventKind = "read_advanced"
	TraceUpdated        EventKind = "trace_updated"
	SourceUpdated       EventKind = "source_updated"
	FeatureDisabled     EventKind = "feature_disabled"
)

type Trace struct {
	ID       string `json:"id,omitempty"` // Stable step within a turn; empty for legacy category snapshots.
	TurnID   string `json:"turn_id"`
	Category string `json:"category"`
	Text     string `json:"text"`
	Complete bool   `json:"complete"`
}

// Event cursors are source-local durable sequences, not message sequences.
// Trace, permission and work events do not increment unread message counts.
type Event struct {
	Version              string           `json:"version"`
	ID                   string           `json:"id"`
	Seq                  int64            `json:"seq"`
	Fence                Fence            `json:"fence"`
	Kind                 EventKind        `json:"kind"`
	At                   time.Time        `json:"at"`
	Message              *Message         `json:"message,omitempty"`
	Turn                 *Turn            `json:"turn,omitempty"`
	Permission           *PermissionOffer `json:"permission,omitempty"`
	Work                 *WorkLink        `json:"work,omitempty"`
	WorkAnchorMessageSeq int64            `json:"work_anchor_message_seq,omitempty"`
	Memory               *MemoryRevision  `json:"memory,omitempty"`
	Bot                  *BotSummary      `json:"bot,omitempty"`
	Read                 *ReadCursor      `json:"read,omitempty"`
	Trace                *Trace           `json:"trace,omitempty"`
	Source               *SourceStatus    `json:"source,omitempty"`
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
	CreateOperation         Operation = "create"
	EditOperation           Operation = "edit"
	UploadOperation         Operation = "upload"
	RenameArtifactOperation Operation = "rename-artifact"
	SendOperation           Operation = "send"
	StopOperation           Operation = "stop"
	SendNowOperation        Operation = "send_now"
	QueueEditOperation      Operation = "queue_edit"
	RuleRemoveOperation     Operation = "rule_remove"
	DeleteOperation         Operation = "delete"
	VisibilityOperation     Operation = "visibility"
	AnswerOperation         Operation = "answer"
	ReadOperation           Operation = "read"
	ObserveOperation        Operation = "observe"
	ForgetOperation         Operation = "forget"
	MemorySaveOperation     Operation = "memory_save"
	DispatchOperation       Operation = "dispatch"
)

type Command struct {
	Key            BotKey    `json:"key"`
	Operation      Operation `json:"operation"`
	IdempotencyKey string    `json:"idempotency_key"`
	// SHA-256 of the canonical validated command, excluding transport metadata.
	ContentHash     string `json:"content_hash"`
	ClientAttemptID string `json:"client_attempt_id,omitempty"`
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

// Bot request card notices (docs/bot-request-cards.md §5, §7.2). Each is a
// host notice whose idempotency key names the card, so one handoff or one
// completion wakes a Bot once, and a turn can tell which card woke it.
const (
	BotRequestHandoffPrefix = "bot-request-handoff-"
	BotRequestDonePrefix    = "bot-request-done-"
	botRequestCardIDLength  = 36
)

// BotRequestHandoffKey is the notice key of the handoff that gave the card's
// baton to a Bot at that version.
func BotRequestHandoffKey(cardID string, version int64) string {
	return BotRequestHandoffPrefix + cardID + "-" + strconv.FormatInt(version, 10)
}

// BotRequestHandoffCard answers the card a handoff notice turn was woken for,
// and the card version it named; empty for every other turn.
func BotRequestHandoffCard(turn Turn) (string, int64) {
	if turn.Origin != HostNoticeOrigin || turn.RoutineRunID != "" || turn.Room != nil || turn.Network != nil {
		return "", 0
	}
	rest, ok := strings.CutPrefix(turn.IdempotencyKey, BotRequestHandoffPrefix)
	if !ok || len(rest) < botRequestCardIDLength+2 || rest[botRequestCardIDLength] != '-' {
		return "", 0
	}
	v, err := strconv.ParseInt(rest[botRequestCardIDLength+1:], 10, 64)
	if err != nil || v <= 0 {
		return "", 0
	}
	return rest[:botRequestCardIDLength], v
}

// WithBotRequest carries the card a dispatch_work or message_bot call names
// from the tool call to the host, like WithDispatchChoice.
func WithBotRequest(ctx context.Context, cardID string) context.Context {
	return context.WithValue(ctx, botRequestKey{}, cardID)
}

// BotRequestFrom answers the card the tool call named, or empty.
func BotRequestFrom(ctx context.Context) string {
	id, _ := ctx.Value(botRequestKey{}).(string)
	return id
}

type botRequestKey struct{}
