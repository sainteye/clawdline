package projectassistant

import (
	"context"
	"encoding/json"
	"time"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// RuntimeLimits are host-owned operational bounds, deliberately without defaults.
// The host registers its selected values in domain/capacity.
type RuntimeLimits struct {
	MaxBots      int
	MaxProcesses int
	// PromptTimeout is optional for hosts that explicitly bound ordinary turns.
	// Production leaves it zero; scheduled routines carry their own deadline.
	PromptTimeout  time.Duration
	CleanupTimeout time.Duration
}

// ControlWindow bounds maintenance and issued work authority independently of
// a production prompt. Embedded hosts may request a shorter operation budget.
func (l RuntimeLimits) ControlWindow() time.Duration {
	if l.PromptTimeout > 0 {
		return l.PromptTimeout
	}
	return time.Duration(capacity.MaximumBotControlWindowMilliseconds) * time.Millisecond
}

type Provider interface {
	Capabilities(context.Context, pa.Bot) (pa.Capabilities, error)
	Open(context.Context, OpenRequest, func(context.Context, ProtocolEvent) error) (ProviderSession, error)
}

type OpenRequest struct {
	Extensions NativeExtensions
	// PrimaryAgent remains configured on the Bot; Binding.Agent is the effective identity.
	PrimaryAgent pa.Agent
	Bot          pa.Bot
	Binding      pa.ExecutionBinding
	Directory    string
	Resume       bool
	// PersistNativeSession is set only by the source-owned installed provider.
	// It never arrives through an HTTP command or model argument.
	PersistNativeSession bool `json:"-"`
	// Tools is execution-bound by the host. No model-supplied identity or
	// machine credential can construct authority through this interface.
	Tools NativeTools `json:"-"`
}

// ProjectDispatchTools accepts a registered Project name or folder. The host
// resolves it and authorizes the destination under the active Bot turn.
type ProjectDispatchTools interface {
	DispatchToProject(ctx context.Context, id, title, brief string, claims []string, project string) (pa.WorkLink, error)
}

// LeadBotTools is a private capability scoped to the current Lead's direct user turn.
type LeadBotTools interface {
	LeadBotAvailable(context.Context) error
	ManageBots(context.Context, LeadBotRequest) (json.RawMessage, error)
}

type LeadBotRequest struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Query          map[string]string `json:"query,omitempty"`
	Body           json.RawMessage   `json:"body,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

// PersonaDispatchTools also names the Session persona the work Session starts
// with. The broker admits the id; an empty persona is an ordinary dispatch.
type PersonaDispatchTools interface {
	DispatchWithPersona(ctx context.Context, id, title, brief string, claims []string, project, persona string) (pa.WorkLink, error)
}

type NativeTools interface {
	SendMessage(context.Context, string, string) error
	Dispatch(ctx context.Context, id, title, brief string, claims []string) (pa.WorkLink, error)
	Status(context.Context, string) (pa.WorkLink, error)
	// Memory returns the Bot's live entries; EditMemory changes them for the
	// current turn, the call ID making a repeated call the same change.
	Memory(context.Context) ([]pa.MemoryEntry, error)
	EditMemory(context.Context, string, MemoryEdit) error
}

// MemoryIndexTools returns the CAS revision together with the body-free index.
type MemoryIndexTools interface {
	MemoryIndex(context.Context) (MemoryIndex, error)
}

// MemoryDetailTools is additive; only the current actor can resolve an entry ID.
type MemoryDetailTools interface {
	MemoryDetail(context.Context, string) (pa.MemoryDetail, error)
}

// CollaborationTools is optional and entirely bound by the current Host turn.
type CollaborationTools interface {
	ListBots(context.Context, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error)
	MessageBot(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
}

// OutputTools is additive so older native tool adapters remain compatible.
type OutputTools interface {
	PublishOutput(context.Context, string, OutputRequest) (pa.Output, error)
	ListArtifacts(context.Context) ([]ArtifactInfo, error)
}

// ArtifactInfo gives only the current actor a path to its own immutable HTML.
// The model edits a Project-local copy and explicitly publishes that copy.
type ArtifactInfo struct {
	pa.Output
	Path string `json:"path"`
}

type OutputRequest struct {
	Path     string `json:"path"`
	Name     string `json:"name,omitempty"`
	Title    string `json:"title,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	Notify   *bool  `json:"notify,omitempty"`
	HTML     bool   `json:"-"`
}

// Send performs one bounded write only. Waiting for the model must happen in
// PendingPrompt.Wait, outside Memory.WithCurrent and outside any store lock.
type ProviderSession interface {
	SessionID() string
	Send(context.Context, PromptRequest) (PendingPrompt, error)
	Answer(context.Context, string, string) error
	Cancel(context.Context) error
	Close(context.Context) error
}

// SteeringSession is a native session that can take a message into the turn
// it is running (ACP _session/steering), as Codync steers.
type SteeringSession interface {
	Steer(context.Context, string) (SteerOutcome, error)
}

// SteerOutcome is the agent's answer to a steer: joined the running turn, or
// that turn had just ended and the message needs a prompt of its own.
type SteerOutcome string

const (
	SteerJoined SteerOutcome = "injected"
	SteerNext   SteerOutcome = "promptRequired"
)

type PendingPrompt interface {
	Wait(context.Context) (PromptResult, error)
}

type PromptRequest struct {
	Turn    pa.Turn
	Text    string
	Context pa.ContextSnapshot
	Quote   *PromptQuote
	Handoff *SessionHandoff
	// MemoryChanged tells a resumed session that memory changed since it last
	// heard it; as in Codync, the prompt then carries all of Context's memory.
	MemoryChanged bool
	// Compacted tells a resumed session that the agent compacted its own
	// context during the previous turn; the prompt repeats Context's memory
	// once (it also covers MemoryChanged).
	Compacted bool
	Images    []PromptImage
	Files     []PromptFile
}

// PromptImage is one picture sent with the person's message.
type PromptImage struct {
	MediaType string
	Data      []byte
}
type PromptQuote struct {
	MessageID string
	Text      string
}
type PromptResult struct{ FinalText string }

// Usage contains counts only, never prompts, credentials or native stderr.
// A host can wire these readings into its registered capacity measurements.
type SessionUsage struct {
	// PendingRPC includes the shared ACP request/owned MCP connection budget.
	PendingRPC          int
	PendingPermissions  int
	QueuedNotifications int
	CapturedBytes       int
	StderrBytes         int
}

// NativeContextState is a live session's own reported model, available commands,
// and token reading. Unknown is separate from an observed zero.
type NativeContextState struct {
	TurnID            string
	Fence             pa.Fence
	NativeSessionID   string
	Model             string
	CommandsKnown     bool
	CompactAdvertised bool
	UsageKnown        bool
	UsedTokens        int64
	WindowTokens      int64
	// The agent's own compactions during CompactedTurnID, as reported by the
	// agent: Compactions counts them; Before/After are used tokens around them
	// (zero when the agent did not report a reading).
	CompactedTurnID string
	Compactions     int
	CompactedBefore int64
	CompactedAfter  int64
}

// NativeContextReader is optional so providers without native context reports
// remain explicitly unqualified. It does not change persisted Bot formats.
// Implementations read cached state only; they must not perform external IO or
// wait for a model, command, or pending prompt.
type NativeContextReader interface {
	NativeContext() NativeContextState
}

type NativeCompactionState string

const (
	NativeCompactionCompleted NativeCompactionState = "completed"
	NativeCompactionFailed    NativeCompactionState = "failed"
	NativeCompactionCancelled NativeCompactionState = "cancelled"
	NativeCompactionUnknown   NativeCompactionState = "unknown"
)

// NativeCompactionResult reports the adapter's explicit terminal fact. Failed
// and cancelled do not assert that the native transcript remained unchanged.
type NativeCompactionResult struct {
	Fence           pa.Fence
	NativeSessionID string
	ID              string
	State           NativeCompactionState
}

// NativeCompactor is a raw maintenance port that never injects chat context.
// Its caller must own the idle actor and persist intent before entering it.
// One client lifetime permits one attempt; unknown effects are not replayed.
// This optional port alone provides neither durable receipts nor UI controls.
type NativeCompactor interface {
	BeginCompaction(context.Context, pa.Fence) (PendingNativeCompaction, error)
}

type PendingNativeCompaction interface {
	Wait(context.Context) (NativeCompactionResult, error)
}

type RuntimeUsage struct {
	ActiveActors int
	Processes    int
	Sessions     []SessionUsage
	Known        bool
}

// Event IDs are stable within a provider turn. Incoming calls have a separate
// lane from pending prompt responses, following Codync host/src/agent/acp.rs.
type ProtocolEvent struct {
	ID         string
	Fence      pa.Fence
	TurnID     string
	Message    string
	Trace      *pa.Trace
	Permission *pa.PermissionOffer
	Output     *pa.Output
	// ToolDiscovery is set by the Host for its own read-only tool catalog
	// lookup, which is journaled but cannot execute a model tool.
	ToolDiscovery bool
}

// PromptFile contains only an owned, verified path for this native execution.
// It is never projected into a public response.
type PromptFile struct {
	File pa.InputFile
	Path string
}
type InputReader interface {
	Resolve(context.Context, pa.Fence, string, ...InputOwner) (pa.InputFile, string, error)
	SentPath(context.Context, pa.Fence, pa.InputFile) (string, error)
}

// InputRemover discards only files proven to belong to a never-started,
// durably withdrawn message. Runtime invokes it after releasing actor locks.
type InputRemover interface {
	Remove(context.Context, pa.Fence, pa.InputFile) error
}

// RoutineTools is available only on the current owned native execution.
type RoutineTools interface {
	Routines(context.Context, RoutineRequest) (RoutineResult, error)
}
type RoutineRequest struct {
	Minutes         int64               `json:"minutes,omitempty"`
	Content         string              `json:"content,omitempty"`
	Action          string              `json:"-"`
	IdempotencyKey  string              `json:"idempotency_key,omitempty"`
	ID              string              `json:"id,omitempty"`
	Name            string              `json:"name,omitempty"`
	Instruction     string              `json:"instruction,omitempty"`
	IntervalSeconds int64               `json:"interval_seconds,omitempty"`
	Triggers        []pa.RoutineTrigger `json:"triggers,omitempty"`
	TimeoutSeconds  int64               `json:"timeout_seconds,omitempty"`
	Enabled         *bool               `json:"enabled,omitempty"`
}
type RoutineWebhookStatus struct {
	PublicStatus string `json:"public_status"`
	PublicURL    string `json:"public_url,omitempty"`
}

type RoutineResult struct {
	Webhook     *RoutineWebhookStatus `json:"webhook,omitempty"`
	FollowUps   []pa.Routine          `json:"follow_ups,omitempty"`
	Hook        *RoutineHookResult    `json:"hook,omitempty"`
	Progress    *string               `json:"progress,omitempty"`
	Tokens      int                   `json:"tokens,omitempty"`
	MaxTokens   int                   `json:"max_tokens,omitempty"`
	Definitions []pa.Routine          `json:"definitions,omitempty"`
	ID          string                `json:"id,omitempty"`
}

// NetworkTools is optional and bound to the owning native execution.
type NetworkTools interface {
	NetworkBot(context.Context, NetworkToolRequest) (network.Response, error)
}
type NetworkToolRequest struct {
	Kind          string           `json:"kind"`
	RequestID     string           `json:"idempotency_key"`
	CorrelationID string           `json:"correlation_id,omitempty"`
	AuthorityID   string           `json:"authority_id"`
	Target        wire.BotEndpoint `json:"target"`
	Text          string           `json:"text,omitempty"`
}

// ReactionTools targets only the actual message captured by the running turn.
type ReactionTools interface {
	React(context.Context, string, string) (pa.Receipt, error)
}

// BackgroundTools puts long work in the Bot's own task queue. The caller's
// native execution binding supplies the origin; arguments supply only a brief.
type BackgroundTools interface {
	BackgroundAvailable() bool
	AddBackgroundWork(context.Context, string, string, string) (pa.BackgroundTask, error)
}
