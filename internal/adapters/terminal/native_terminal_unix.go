//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func nativeTerminalIdentity(s session.Session) (string, error) {
	procs, foreground, err := ttyProcesses(s.TTY)
	if err != nil {
		return "", err
	}
	return nativeTerminalIdentityOf(s, procs, foreground, nativeTerminalExecutable)
}

func nativeTerminalIdentityOf(s session.Session, procs []ttyProc, foreground int, executable func(int) (string, error)) (string, error) {
	// A CLI may be the root of its tty, with no shell underneath it. Verify
	// its kernel command independently of the cached PID classification.
	verified := false
	command := ""
	for _, process := range procs {
		if process.PID == s.PID && process.PGID == foreground && nativeTerminalProcessMatches(s.Assistant, process, executable) {
			verified = true
			command = process.Comm
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
	return fmt.Sprintf("%s:%s:%s:%d:%d:%s", s.Backend, s.ID, s.Assistant, sight.PID, sight.Start.UnixNano(), command), nil
}

func (t *Tmux) NativeTerminalIdentity(ctx context.Context, s session.Session) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return nativeTerminalIdentity(s)
}

var nativeTerminalClaudeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func nativeTerminalProcessMatches(assistant session.Assistant, process ttyProc, executable func(int) (string, error)) bool {
	if assistantOfComm(process.Comm) == assistant {
		return true
	}
	// Gemini's standalone CLI is the native `agy` binary. Its pid and birth were
	// verified against the executable at discovery; `ours` re-checks the birth.
	if assistant == session.AssistantAntigravity && process.Comm == "agy" {
		return true
	}
	// Native Claude uses its version as the kernel command. A version string
	// alone proves nothing: require the kernel image at the standard install root.
	if assistant != session.AssistantClaude || !nativeTerminalClaudeVersion.MatchString(process.Comm) || executable == nil {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	image, err := executable(process.PID)
	return err == nil && image == filepath.Join(home, ".local", "share", "claude", "versions", process.Comm)
}
