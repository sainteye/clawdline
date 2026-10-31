//go:build !darwin && !linux

package terminal

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (t *Tmux) NativeTerminalIdentity(context.Context, session.Session) (string, error) {
	return "", Unsent{Why: "the foreground assistant cannot be verified on this platform"}
}
