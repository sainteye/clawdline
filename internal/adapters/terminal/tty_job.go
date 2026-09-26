//go:build darwin || linux

package terminal

import (
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What is in front of a terminal, asked of the kernel.
//
// **The kernel and not the terminal emulator.** iTerm2's own `jobName` was
// measured still naming a command the shell had finished, and tmux's
// `pane_current_command` names what is in front without saying which process
// it is — which is enough to see an assistant and not enough to signal one.
// The tty's foreground process group is the terminal's own answer to both
// questions at once.

// ttySight is what holds tty now, for the session the reading named.
//
// The assistant is looked for across the whole foreground group rather than at
// its leader alone, because the kernel's short name is sixteen characters with
// no arguments and an assistant started through a wrapper reads as the
// wrapper. When the row already carries a pid — the process table classified
// it, which is better evidence than a name — a group holding that pid is that
// row's assistant whatever the names say.
func ttySight(tty string, s session.Session) (farewellSight, error) {
	procs, fg, err := ttyProcesses(tty)
	if err != nil {
		return farewellSight{}, err
	}
	return ttySightOf(procs, fg, s), nil
}

// ttySightOf is ttySight once the kernel has answered, so what it decides can
// be checked without a terminal.
func ttySightOf(procs []ttyProc, fg int, s session.Session) farewellSight {
	job := foregroundJob(procs, fg)
	if len(job) == 0 {
		// The shell at its prompt: nothing holds the terminal.
		return farewellSight{}
	}
	sight := farewellSight{Job: true, Group: fg}
	leader := job[0]
	for _, p := range job {
		if p.PID == fg {
			leader = p
		}
		if a := assistantOfComm(p.Comm); a != "" && sight.Assistant == "" {
			sight.Assistant = a
		}
		if s.PID != 0 && p.PID == s.PID && s.Assistant != "" {
			sight.Assistant = s.Assistant
		}
	}
	sight.PID, sight.Start = leader.PID, leader.Start
	// A pid the row named, found in this group, is the identity to hold the
	// escalation to: it is the process the close was asked about, and the
	// leader may be a shell that spawned it.
	for _, p := range job {
		if s.PID != 0 && p.PID == s.PID {
			sight.PID, sight.Start = p.PID, p.Start
		}
	}
	return sight
}

// ttySignal asks a terminal's foreground process group to leave.
//
// The group and not the one process: an assistant runs helpers beside itself —
// on this Mac one terminal carried five processes named `codex` — and a
// terminal whose leader left with its helpers still on the tty is a terminal
// that cannot be closed without hanging them up.
//
// A group the kernel says has nobody in it is already the answer this signal
// was asking for, so it is not a failure.
var ttySignal = func(group int, step escalation) error {
	sig := syscall.SIGTERM
	if step == escalateKill {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-group, sig); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

// ttyProc is one process on a terminal, as the kernel describes it.
type ttyProc struct {
	PID, PPID, PGID int
	Comm            string
	Start           time.Time
}

// foregroundJob is the tty's foreground group when it is a job — anything but
// the shell at its prompt — and nil when it is the shell.
//
// The shell is the root of the tty's process tree (a shell iTerm2 started
// directly), or the root's child when the root is `login`, as it is under
// iTerm2's default profile. A foreground group led by anything else — the
// assistant, a command, a shell started inside the shell — is a job.
func foregroundJob(procs []ttyProc, fg int) []ttyProc {
	onTTY := make(map[int]ttyProc, len(procs))
	for _, p := range procs {
		onTTY[p.PID] = p
	}
	var group []ttyProc
	for _, p := range procs {
		if p.PGID == fg {
			group = append(group, p)
		}
	}
	if len(group) == 0 {
		return nil
	}
	leader, ok := onTTY[fg]
	if !ok {
		// A group whose leader has left the tty is not the shell.
		return group
	}
	parent, parentOnTTY := onTTY[leader.PPID]
	switch {
	case !parentOnTTY:
		return nil
	case strings.TrimPrefix(parent.Comm, "-") == "login":
		if _, grand := onTTY[parent.PPID]; !grand {
			return nil
		}
	}
	return group
}

// processSight is what holds tty now, for a session only the process table
// saw (ProcessCloser).
func processSight(tty string, s session.Session) (farewellSight, error) {
	procs, fg, err := ttyProcesses(tty)
	if err != nil {
		return farewellSight{}, err
	}
	return processSightOf(procs, fg, s), nil
}

// processSightOf is processSight once the kernel has answered.
//
// **It is not ttySightOf, because there is no shell to come back to.** That
// one reads a terminal the way a tab reads: a job in front of a shell, and the
// tab safe to close once the shell is in front again. A session no backend
// lists may have no shell under it at all — a tmux pane started straight into
// `claude` has the assistant as the root of its tty, which ttySightOf reads as
// "the shell at its prompt" and would report as nothing to end. Here the
// question is only whether the process the reading named is still on this tty,
// and still the one in front of it.
func processSightOf(procs []ttyProc, fg int, s session.Session) farewellSight {
	if s.PID == 0 {
		// Nothing to look for: a row with no pid has no process to end, and
		// "not found" would be read as gone. The foreground, unnamed, is
		// refused by ours.
		return farewellSight{Job: true}
	}
	for _, p := range procs {
		if p.PID != s.PID {
			continue
		}
		if p.PGID != fg || fg == 0 {
			// Still here, and no longer what the terminal is attached to:
			// something else was brought in front of it.
			return farewellSight{Job: true, PID: p.PID, Start: p.Start}
		}
		return farewellSight{Job: true, Assistant: s.Assistant, PID: p.PID, Start: p.Start, Group: fg}
	}
	return farewellSight{Gone: true}
}
