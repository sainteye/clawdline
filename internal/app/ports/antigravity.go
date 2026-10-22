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
