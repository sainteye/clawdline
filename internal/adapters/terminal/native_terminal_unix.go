//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func nativeTerminalIdentity(s session.Session) (string, error) {
	procs, foreground, err := ttyProcesses(s.TTY)
	if err != nil {
		return "", err
	}
	return nativeTerminalIdentityOf(s, procs, foreground)
}

func nativeTerminalIdentityOf(s session.Session, procs []ttyProc, foreground int) (string, error) {
	// A CLI may be the root of its tty, with no shell underneath it. Verify
	// its kernel command independently of the cached PID classification.
	verified := false
	for _, process := range procs {
		if process.PID == s.PID && process.PGID == foreground && assistantOfComm(process.Comm) == s.Assistant {
			verified = true
			break
		}
	}
	if !verified {
		return "", Unsent{Why: "the foreground program is not the expected assistant"}
	}
	sight := processSightOf(procs, foreground, s)
	if s.PID <= 0 || sight.PID != s.PID || sight.Start.IsZero() {
		return "", Unsent{Why: "the assistant no longer owns this terminal"}
	}
	if _, why := ours(s, sight); why != "" {
		return "", Unsent{Why: why}
	}
	return fmt.Sprintf("%s:%s:%s:%d:%d", s.Backend, s.ID, s.Assistant, sight.PID, sight.Start.UnixNano()), nil
}

func (t *Tmux) NativeTerminalIdentity(ctx context.Context, s session.Session) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return nativeTerminalIdentity(s)
}
