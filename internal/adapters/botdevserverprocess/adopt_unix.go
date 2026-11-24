//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package botdevserverprocess

import (
	"context"
	"errors"
	"io"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Adopt returns a handle for a dev server a previous Host launched, only when
// its root PID is still alive with the journalled start second and still
// leads its own process group. Output from before the restart is gone.
func (l *Launcher) Adopt(ctx context.Context, _ servers.Spec, m servers.Mark) (servers.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.PID <= 0 || m.Started <= 0 || !sameRoot(m) {
		return nil, nil
	}
	if group, err := syscall.Getpgid(m.PID); err != nil || group != m.PID {
		return nil, nil
	}
	return &adopted{mark: m, poll: l.limits.DescendantPoll}, nil
}

type adopted struct {
	mark servers.Mark
	poll time.Duration
}

func sameRoot(m servers.Mark) bool {
	started := ownrecords.ProcessStart(m.PID)
	return !started.IsZero() && started.Unix() == m.Started
}

// groupAlive reports whether any member of the recorded process group is
// left. POSIX does not reuse a PID while a group with that ID exists, so a
// missing root with a surviving group is still this server's group; a live
// root with a different start time is a reused PID and is never ours.
func (a *adopted) groupAlive() (bool, bool) {
	started := ownrecords.ProcessStart(a.mark.PID)
	if !started.IsZero() && started.Unix() != a.mark.Started {
		return false, false
	}
	err := syscall.Kill(-a.mark.PID, 0)
	return err == nil || errors.Is(err, syscall.EPERM), true
}

func (a *adopted) Output() io.Reader          { return nil }
func (a *adopted) Mark() (servers.Mark, bool) { return a.mark, true }

func (a *adopted) WaitExit(ctx context.Context) error {
	ticker := time.NewTicker(a.poll)
	defer ticker.Stop()
	for sameRoot(a.mark) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return failure(pa.OutcomeUnknown, "server_exit") // An adopted exit status is not observable.
}

// Close stops the whole group: SIGTERM, then SIGKILL for the second half of
// the caller's cleanup budget. It succeeds only once no member is left.
func (a *adopted) Close(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return failure(pa.Invalid, "server_cleanup_deadline")
	}
	escalate := time.Now().Add(time.Until(deadline) / 2)
	ticker := time.NewTicker(a.poll)
	defer ticker.Stop()
	terminated, killed := false, false
	for {
		if alive, ours := a.groupAlive(); !alive || !ours {
			return nil
		}
		var signal syscall.Signal
		if !terminated {
			signal, terminated = syscall.SIGTERM, true
		} else if !killed && time.Now().After(escalate) {
			signal, killed = syscall.SIGKILL, true
		}
		if signal != 0 {
			if err := syscall.Kill(-a.mark.PID, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
				return failure(pa.ExecutionEffectUnknown, "server_signal")
			}
		}
		select {
		case <-ctx.Done():
			return failure(pa.ExecutionEffectUnknown, "server_cleanup")
		case <-ticker.C:
		}
	}
}
