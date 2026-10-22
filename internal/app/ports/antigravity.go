package ports

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// AntigravityProcess is a kernel observation, never a hook-payload claim.
// Birth must distinguish reused PIDs; an unavailable birth is not evidence.
// TerminalID is resolved by the terminal backend, not from untrusted stdin.
type AntigravityProcess struct {
	PID, ParentPID                     int
	Birth, Executable, TTY, TerminalID string
	// KernelGeneration fences exec; BootID is required by the Linux reader.
	// Darwin has no boot UUID here and leaves BootID empty.
	BootID, KernelGeneration string
}

type AntigravityProcessReader interface {
	ReadAntigravityProcess(context.Context, int) (AntigravityProcess, error)
}

// AntigravityProcessVerifier admits only the pinned standalone executable.
// A process name or a command-line substring is insufficient.
type AntigravityProcessVerifier interface {
	VerifyAntigravityProcess(context.Context, int) (AntigravityProcess, error)
}

// AntigravityNonCLI distinguishes a proved different executable from a
// process lookup failure. Only the former is a complete negative discovery.
type AntigravityNonCLI interface{ AntigravityNotStandalone() bool }

type AntigravityBinding struct {
	Process                               AntigravityProcess
	ConversationID, TranscriptPath, Model string
	Event                                 string
	ObservedAt                            time.Time
	Sequence                              uint64
	Verified                              bool
	StateFresh                            bool
	// State is an observation only. Even a fully idle Stop does not authorize
	// sending without a fresh composer/menu/draft check.
	State session.State
}

type AntigravityBindings interface {
	AntigravityBinding(context.Context, session.Session) (AntigravityBinding, error)
}

// AntigravityFirstInputTarget is private launch composition, never a browser
// supplied terminal or a manufactured conversation. Check must freshly verify
// the exact native process, foreground, terminal, kernel workspace, intact
// private capability/context and provider permission/free-use policy.
type AntigravityFirstInputTarget struct {
	LaunchID, Backend string
	Process           AntigravityProcess
}
type AntigravityFirstInputAttempt interface {
	Check(context.Context) error
	BeforePaste(context.Context) error
	BeforeEnter(context.Context) error
}
type AntigravityFirstInputs interface {
	Target(context.Context, string) (AntigravityFirstInputTarget, error)
	Check(context.Context, AntigravityFirstInputTarget) error
	// Reserve returns nil for any existing/recovered consumed slot. Only the
	// durable creator receives a volatile attempt that may execute effects.
	Reserve(context.Context, AntigravityFirstInputTarget, string, string) (AntigravityFirstInputAttempt, error)
}

// AntigravityFirstInputTerminal fences each native IO step, including tmux's
// separate buffer load and paste. Unsupported transports cannot use Type/Send
// as a fallback. Cleanup of a private paste buffer is not provider input.
type AntigravityFirstInputTerminal interface {
	TypeAntigravityFirst(context.Context, session.Session, string, func(context.Context) error) error
	EnterAntigravityFirst(context.Context, session.Session, func(context.Context) error) error
}
