// Package projectassistant defines the persistent-Bot module's ports. It
// imports only its domain and standard-library types, never app/projects,
// legacy app/ports, transport, store or orchestrator implementations.
package projectassistant

import (
	"context"
	"database/sql"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Submission struct {
	Network *pa.NetworkWork
	// Room makes this a group member's room turn: Notice must be set and
	// the ticket's member must be Command.Key. Only the room driver sets it.
	Room       *pa.RoomTicket
	InputOwner InputOwner
	// InputCheck revalidates the human grant after streaming and before admission.
	InputCheck func(context.Context) error
	// AuthorityCheck revalidates the sender's exact grant revision after the
	// slow admission reads and before anything is written. A re-pair, a
	// downgrade or a revocation during those reads refuses the send.
	AuthorityCheck func(context.Context) error
	Command        pa.Command
	Message        pa.SendMessage
	Feature        pa.FeatureState
	Source         pa.SourceStatus
	// Notice marks a message Clawdline itself sends the agent, such as the
	// resume prompt after a restart; it is shown as a notice, not as the
	// person's words.
	Notice   bool
	LeadCall bool
	// Decision is the person's answer to a decision offer, resolved in the
	// same commit as their message so an offer is answered at most once.
	Decision *pa.PermissionOffer
}
type StopRequest struct {
	Command pa.Command
	Fence   pa.Fence
	// Stop cancels queued messages as well as the active turn. Closing the UI
	// never calls Stop. Unknown external work is recorded, never replayed.
	// Naming a turn that is still queued removes only that message (API 63).
	TurnID string
}
type AnswerRequest struct {
	Command pa.Command
	Answer  pa.PermissionAnswer
	Feature pa.FeatureState
	Source  pa.SourceStatus
}

// Runtime owns serial per-Bot admission, lazy process binding, incoming RPCs
// independent of pending prompt calls, cancellation and owned process cleanup.
// Submit durably accepts before returning; it is not a completion receipt.
type Runtime interface {
	Submit(context.Context, Submission) (pa.Receipt, error)
	Stop(context.Context, StopRequest) (pa.Receipt, error)
	SendNow(context.Context, SendNowRequest) (pa.Receipt, error)
	Answer(context.Context, AnswerRequest) (pa.Receipt, error)
	Disable(context.Context, pa.ProjectSelection) error
}

// SendNowRequest is Codync's steer: send one queued message into the turn
// that is running now instead of waiting for it to end.
type SendNowRequest struct {
	Command pa.Command
	Fence   pa.Fence
	TurnID  string
}

type HistoryQuery struct {
	Key            pa.BotKey
	ConversationID string
	BeforeSeq      int64
	PageSize       int
}
type MessagePage struct {
	Items         []pa.Message
	NextBeforeSeq int64
	HasMore       bool
}

// Mutation is one compare-and-swap against the Bot aggregate version. Facts,
// projections, event sequence allocation and command receipt commit atomically.
// A failure writes none. Same key/different hash is a conflict; unknown effects
// retain their receipts. All non-nil facts must belong to Command.Key.
type Mutation struct {
	QueuedMessageEdit *pa.Message
	QuietCompletion   bool
	StartFreshSession bool
	DesignateLead     bool
	ArtifactTitle     *pa.ArtifactDisplayTitle
	AgentRest         *pa.AgentRest
	ClearAgentRest    pa.Agent
	ClearLoginProfile string
	Routine           *pa.Routine
	RotateRoutineHook bool
	RoutineNotStarted bool
	RoutineRun        *pa.RoutineRun
	Command           pa.Command
	ExpectedVersion   int64
	Receipt           pa.Receipt
	Bot               *pa.Bot
	Conversation      *pa.Conversation
	Turn              *pa.Turn
	Binding           *pa.ExecutionBinding
	Messages          []pa.Message
	Permissions       []pa.PermissionOffer
	WorkLinks         []pa.WorkLink
	Memory            *pa.MemoryRevision
	Events            []pa.Event
}
type Repository interface {
	Bot(context.Context, pa.BotKey) (pa.Bot, error)
	List(context.Context, pa.ListBots) (pa.BotPage, error)
	Conversation(context.Context, pa.BotKey, string) (pa.Conversation, error)
	Turn(context.Context, pa.BotKey, string) (pa.Turn, error)
	PendingTurns(context.Context, pa.BotKey) ([]pa.Turn, error)
	Binding(context.Context, pa.BotKey, string) (pa.ExecutionBinding, error)
	Messages(context.Context, HistoryQuery) (MessagePage, error)
	Permission(context.Context, pa.BotKey, string) (pa.PermissionOffer, error)
	Receipt(context.Context, pa.Command) (*pa.Receipt, error)
	Commit(context.Context, Mutation) (pa.Receipt, error)
	MarkRead(context.Context, pa.Command, pa.ReadCursor) (pa.ReadCursor, error)
	Observe(context.Context, pa.Command, string, int64) (pa.Receipt, error)
	Export(context.Context, pa.BotKey) (Export, error)
}

// CollaborationRepository owns a distinct two-aggregate transaction. No IO,
// actor wait, or second Commit may be hidden inside these operations.
type CollaborationRepository interface {
	Directory(context.Context, pa.Fence, string, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error)
	AdmitExchange(context.Context, ExchangeAdmission) (pa.Exchange, error)
	Exchange(context.Context, pa.BotKey, string) (pa.Exchange, error)
	FinishExchange(context.Context, Mutation, string) (pa.Exchange, error)
	StartExchange(context.Context, pa.Fence, string, string) error
	PromoteExchangeReply(context.Context, pa.BotKey, time.Time) (bool, error)
	DropExchangeReplies(context.Context, pa.BotKey, time.Time) error
}

// DecisionRepository holds questions a Bot asks the person. Only the
// console's authorized answer route resolves one, through Submit.
type DecisionRepository interface {
	OfferDecision(context.Context, pa.Fence, string, pa.AskPersonRequest, time.Time) (pa.PermissionOffer, error)
	ForwardDecision(context.Context, pa.BotKey, pa.Turn, string, time.Time) (pa.Exchange, error)
}

// RoomRepository finishes a group member's room turn: the member's own
// terminal facts, the final ticket check and the room line commit in one
// transaction. A refused ticket still finishes the member's own turn; only
// the room line is withheld. No IO or actor wait happens inside it.
type RoomRepository interface {
	FinishRoomTurn(context.Context, Mutation, string) (RoomPublication, error)
}

// RoomPublication is what a finished room turn did to the room. Entry is the
// line written (nil for a pass, a refusal or an unsuccessful turn); Refusal
// is the typed reason the ticket no longer held.
type RoomPublication struct {
	Ticket  pa.RoomTicket `json:"ticket"`
	TurnID  string        `json:"turn_id"`
	State   pa.TurnState  `json:"state"`
	Entry   *pa.RoomEntry `json:"entry,omitempty"`
	Refusal *pa.Failure   `json:"refusal,omitempty"`
}

// RoomGate is the room's say over a member's room turn. Admit runs when the
// turn is dequeued and when a permission card of that turn is answered;
// Finished is told every terminal outcome after it commits and must not
// block or call back into the runtime synchronously.
type RoomGate interface {
	Admit(context.Context, pa.RoomTicket) error
	Finished(RoomPublication)
}

type ExchangeAdmission struct {
	SourceFence   pa.Fence
	SourceTurnID  string
	SourceVersion int64
	TargetVersion int64
	Command       pa.Command
	Exchange      pa.Exchange
	At            time.Time
}

// Export is a versioned data transfer, not a second Markdown authority. Restore
// is PA-03's installer/store operation and must reject a newer unknown version.
type Export struct {
	Reactions      []pa.ReactionRecord                `json:"Reactions,omitempty"`
	ArtifactTitles map[string]pa.ArtifactDisplayTitle `json:"ArtifactTitles,omitempty"`
	Exchanges      []pa.Exchange                      `json:"Exchanges,omitempty"`
	// Groups are the rooms this Bot belongs to, or belonged to when they
	// were deleted (Group.Deleted marks the tombstone), with their members
	// and room state. They are versioned by GroupSchemaVersion, separately
	// from SchemaVersion; both are absent for a Bot in no group.
	Groups             []GroupExport   `json:"Groups,omitempty"`
	GroupSchemaVersion int             `json:"GroupSchemaVersion,omitempty"`
	Routines           []pa.Routine    `json:"Routines,omitempty"`
	RoutineRuns        []pa.RoutineRun `json:"RoutineRuns,omitempty"`
	ContractVersion    string
	SchemaVersion      int
	Bot                pa.Bot
	Conversation       pa.Conversation
	Messages           []pa.Message
	Turns              []pa.Turn
	Bindings           []pa.ExecutionBinding
	Permissions        []pa.PermissionOffer
	WorkLinks          []pa.WorkLink
	Memory             pa.MemoryRevision
	MemoryEntries      []pa.MemoryEntry
	Context            pa.ContextSnapshot
	Events             []pa.Event
	Receipts           []pa.Receipt
	ReadCursors        []pa.ReadCursor
}

// GroupExport is one group as stored, with its room coordination state.
type GroupExport struct {
	Group pa.Group     `json:"group"`
	Room  pa.RoomState `json:"room"`
}

type ForgetRequest struct {
	Command          pa.Command
	Project          pa.ProjectScope
	ExpectedRevision int64
	SourceIDs        []string
}
type Memory interface {
	Current(context.Context, pa.BotKey) (pa.MemoryRevision, error)
	Snapshot(context.Context, pa.BotKey) (pa.ContextSnapshot, error)
	// WithCurrent serializes against Save/Forget for this Bot while the
	// callback performs one bounded stdin write, never an entire model turn.
	// It rechecks revision/provenance and invokes the callback at most once.
	// It holds no SQLite transaction during IO. A callback error can mean an
	// unknown external effect and must not cause an automatic callback retry.
	WithCurrent(context.Context, pa.BotKey, int64, func(pa.ContextSnapshot) error) error
	Save(context.Context, MemoryWrite) (pa.MemoryRevision, error)
	// Forget atomically persists tombstones, revision and event/receipt; it
	// invalidates all cached summaries before any new turn can capture one.
	Forget(context.Context, ForgetRequest) (pa.MemoryRevision, error)
}

type MemoryWrite struct {
	Command          pa.Command
	ExpectedRevision int64
	Entry            pa.MemoryEntry
}
type WorkBridge interface {
	// Dispatch returns a persisted WorkLink only after the existing broker
	// accepts exact claims and an isolated worktree. It never invents a run.
	Dispatch(context.Context, pa.WorkRequest) (pa.WorkLink, error)
	Status(context.Context, pa.AssistantExecutionActor, string) (pa.WorkLink, error)
}
type EventFeed interface {
	// Read is durable cursor replay. An expired cursor returns
	// pa_cursor_expired so clients refresh a snapshot instead of losing facts.
	Read(context.Context, pa.EventQuery) (pa.EventPage, error)
}

// BotDeleter removes a Bot's aggregate, rich events and receipts. A minimal
// replayable roster removal remains so disconnected viewers can converge.
type BotDeleter interface {
	DeleteBot(context.Context, pa.BotKey) error
}

// RuleStore lists and revokes the standing rules an agent keeps for a Project
// directory after Always allow or Never.
type RuleStore interface {
	List(dir string) ([]pa.PermissionRule, error)
	Remove(dir string, rule pa.PermissionRule) error
}

// SourceRuleStore exposes Host-owned exact rules through the existing human
// settings surface. It never grants permission on behalf of a native provider.
type SourceRuleStore interface {
	Handles(pa.PermissionRule) bool
	List(context.Context, pa.BotKey, pa.ProjectScope) ([]pa.PermissionRule, error)
	Remove(context.Context, pa.BotKey, pa.ProjectScope, pa.PermissionRule) error
}

// PersonalWorkspaceDirectory is the source-owned workspace namespace, separate
// from native profiles and other daemon data.
const PersonalWorkspaceDirectory = "bot-workspaces"

// PersonalWorkspaceProvisioner is a source-owned capability. Only a human
// create or host bootstrap can allocate a new directory.
type PersonalWorkspaceProvisioner interface {
	ProvisionPersonalWorkspace(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error)
}

// TeamProjectResolver resolves a manager's folder choice against the same
// registry as the human Bot editor.
type TeamProjectResolver interface {
	ResolveTeamFolder(context.Context, string, string) (pa.ProjectResolution, error)
}

type ProjectResolver interface {
	Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error)
}

// SchemaInstaller installs ONLY feature-owned tables on the supplied database.
// It must not change the host's PRAGMA user_version, drop data, or open a live
// store. PA-03 supplies its implementation; PA-05 calls it only on opt-in.
type SchemaInstaller interface {
	Version() int
	Install(context.Context, *sql.DB) error
}
