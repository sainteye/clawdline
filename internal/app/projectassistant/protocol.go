package projectassistant

import (
	"context"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// RuntimeLimits are host-owned operational bounds, deliberately without defaults.
// The host registers its selected values in domain/capacity.
type RuntimeLimits struct {
	MaxBots        int
	MaxProcesses   int
	PromptTimeout  time.Duration
	CleanupTimeout time.Duration
}

type Provider interface {
	Capabilities(context.Context, pa.Bot) (pa.Capabilities, error)
	Open(context.Context, OpenRequest, func(context.Context, ProtocolEvent) error) (ProviderSession, error)
}

type OpenRequest struct {
	Bot       pa.Bot
	Binding   pa.ExecutionBinding
	Directory string
	Resume    bool
	// Tools is execution-bound by the host. No model-supplied identity or
	// machine credential can construct authority through this interface.
	Tools NativeTools `json:"-"`
}

type NativeTools interface {
	SendMessage(context.Context, string, string) error
	Dispatch(context.Context, string, string, []string) (pa.WorkLink, error)
	Status(context.Context, string) (pa.WorkLink, error)
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

type PendingPrompt interface {
	Wait(context.Context) (PromptResult, error)
}

type PromptRequest struct {
	Turn    pa.Turn
	Text    string
	Context pa.ContextSnapshot
	Quote   *PromptQuote
	Handoff *SessionHandoff
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
}
