// Package projectassistant defines the persistent-Bot module's ports. It
// imports only its domain and standard-library types, never app/projects,
// legacy app/ports, transport, store or orchestrator implementations.
package projectassistant

import (
	"context"
	"database/sql"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Submission struct {
	Command pa.Command
	Message pa.SendMessage
	Feature pa.FeatureState
	Source  pa.SourceStatus
	// Notice marks a message Clawdline itself sends the agent, such as the
	// resume prompt after a restart; it is shown as a notice, not as the
	// person's words.
	Notice bool
}
type StopRequest struct {
	Command pa.Command
	Fence   pa.Fence
	// Stop cancels queued messages as well as the active turn. Closing the UI
	// never calls Stop. Unknown external work is recorded, never replayed.
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
	Answer(context.Context, AnswerRequest) (pa.Receipt, error)
	Disable(context.Context, pa.ProjectSelection) error
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
	Command         pa.Command
	ExpectedVersion int64
	Receipt         pa.Receipt
	Bot             *pa.Bot
	Conversation    *pa.Conversation
	Turn            *pa.Turn
	Binding         *pa.ExecutionBinding
	Messages        []pa.Message
	Permissions     []pa.PermissionOffer
	WorkLinks       []pa.WorkLink
	Memory          *pa.MemoryRevision
	Events          []pa.Event
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

// Export is a versioned data transfer, not a second Markdown authority. Restore
// is PA-03's installer/store operation and must reject a newer unknown version.
type Export struct {
	ContractVersion string
	SchemaVersion   int
	Bot             pa.Bot
	Conversation    pa.Conversation
	Messages        []pa.Message
	Turns           []pa.Turn
	Bindings        []pa.ExecutionBinding
	Permissions     []pa.PermissionOffer
	WorkLinks       []pa.WorkLink
	Memory          pa.MemoryRevision
	MemoryEntries   []pa.MemoryEntry
	Context         pa.ContextSnapshot
	Events          []pa.Event
	Receipts        []pa.Receipt
	ReadCursors     []pa.ReadCursor
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
