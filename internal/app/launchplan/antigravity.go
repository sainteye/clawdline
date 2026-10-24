// Package launchplan holds private prepared launch values shared by app
// entry points and the broker. It owns no launch effects or policy authority.
package launchplan

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// PreparedAntigravity is one private durable intent. None of its fields are
// browser response authority. It cannot live in ports: projects transitively
// depends on ports through the native transcript process reader.
type PreparedAntigravity struct {
	LaunchID   string
	Request    projects.LaunchRequest
	PrivateEnv []string
	// AttachTerminal binds the actual opened terminal; it never opens another.
	AttachTerminal func(context.Context, string, string) error
	// AbortBeforeOpen may run only after a positively proved no-open outcome.
	AbortBeforeOpen func(context.Context) error
}
