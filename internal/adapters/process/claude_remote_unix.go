//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// WorkingDirectory reads only the selected process, never a provider directory scan.
func WorkingDirectory(ctx context.Context, pid int) (string, bool) {
	_, dirs, ok := systemOpenFiles(ctx, []int{pid})
	return dirs[pid], ok && dirs[pid] != ""
}

// VerifyRemoteControl rereads birth and native transport immediately before IO.
func VerifyRemoteControl(ctx context.Context, s session.Session) bool {
	if s.Backend != session.BackendClaudeRemote || s.PID <= 0 || s.ExpectedStart.IsZero() || NormalizeRemoteID(s.RemoteSessionID) == "" {
		return false
	}
	if !ownrecords.ProcessStart(s.PID).Equal(s.ExpectedStart) {
		return false
	}
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(s.PID), "-o", "command=")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || (classify(string(out)) != session.AssistantClaude && !nativeClaudeRemoteExecutable(fields[0])) {
		return false
	}
	return RemoteControlID(fields[1:]) == s.RemoteSessionID && ownrecords.ProcessStart(s.PID).Equal(s.ExpectedStart)
}

// RemoteControlGone proves the original process ended, including PID reuse.
func RemoteControlGone(ctx context.Context, s session.Session) (bool, error) {
	if s.PID <= 0 || s.ExpectedStart.IsZero() {
		return false, errors.New("missing process generation")
	}
	err := syscall.Kill(s.PID, 0)
	if err == syscall.ESRCH {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	start := ownrecords.ProcessStart(s.PID)
	if start.IsZero() {
		return false, errors.New("process generation unreadable")
	}
	return !start.Equal(s.ExpectedStart), nil
}
