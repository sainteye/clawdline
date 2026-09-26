//go:build darwin

package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Closing a child's iTerm2 tab.
//
// **iTerm2 asks a person before it closes a tab with a job running in it.** On
// this Mac the default profile's "Prompt Before Closing" is "if there are jobs
// besides" ssh, telnet, rlogin and slogin, and the scripting `close` has no way
// round it: measured, three closes of a tab running `sleep 300` each put up
// "Close tab #N? This tab is running sleep." on the person's screen, held the
// Apple Event past its ten-second limit, and left the tab open until somebody
// answered — which is also why a close that ran out of time was seen to close
// its tab later. A tab whose shell is at its prompt closes in 0.15 s and asks
// nothing. A finished child's tab is never at its prompt: the assistant is
// still running there, at rest. So the child is ended first, and the tab closed
// after, as the Swift app did (Targets.end: the quit, the wait, the close).
//
// The job is ended by the kernel's answer, not iTerm2's: iTerm2's own `jobName`
// variable was measured still naming a command the shell had finished. And it
// is ended only when it is provably the child's — every process of the tty's
// foreground group had started before the task ended — so a command a person
// began in that tab afterwards is never signalled, and the tab is left open.

// itermChildEnd is how long an ended job is given to leave before the tab is
// left open instead.
const itermChildEnd = 5 * time.Second

// itermEnder is the close of a child's iTerm2 tab, with every step it takes
// on the machine passed in, so the order and the refusals can be tested
// without a terminal.
type itermEnder struct {
	// find looks for the session and answers its tty when it is there.
	find func(ctx context.Context, id string) (sighting, string)
	// processes is every process on the tty and the tty's foreground group.
	processes func(tty string) ([]ttyProc, int, error)
	// signal sends SIGTERM to a process group; gone is whether it has none
	// left, as the kernel answers it.
	signal func(pgid int) error
	gone   func(pgid int) bool
	close  func(ctx context.Context, id string) error
	wait   time.Duration
	tick   time.Duration
}

var itermChildEnder = itermEnder{
	find:      findITermSession,
	processes: ttyProcesses,
	signal:    func(pgid int) error { return syscall.Kill(-pgid, syscall.SIGTERM) },
	gone:      groupGone,
	close:     closeITermByID,
	wait:      itermChildEnd,
	tick:      100 * time.Millisecond,
}

// CloseITermChild closes a child's iTerm2 session by the id iTerm2 gave it,
// ending the child's foreground job first when one is running (see above).
// startedBefore is when the task ended: a job whose processes all began before
// then is the child's; anything later is somebody else's, and the tab is left
// open with the reason. It answers whether it closed anything. A session that
// is gone, or not seen past a window that will not list, closes nothing and is
// not an error; a close iTerm2 did not answer is Unconfirmed.
func (l Launcher) CloseITermChild(ctx context.Context, id string, startedBefore time.Time) (bool, error) {
	if id == "" {
		return false, nil
	}
	err := itermChildEnder.end(ctx, id, startedBefore)
	var unsent Unsent
	if errors.As(err, &unsent) {
		return false, nil
	}
	return err == nil, err
}

// end is the ladder: find the session, read its tty from the kernel, end the
// child's job when there is one and it is provably the child's, confirm the
// shell is back in front, and close. Every refusal before the close leaves
// the tab as it was.
func (e itermEnder) end(ctx context.Context, id string, startedBefore time.Time) error {
	seen, tty := e.find(ctx, id)
	switch {
	case seen == sightingGone:
		return Unsent{Why: "That session is gone"}
	case seen != sightingThere || tty == "":
		return Unsent{Why: "That session was not seen, so nothing was closed"}
	}
	procs, fg, err := e.processes(tty)
	if err != nil {
		return Failure{Message: "What runs in the tab could not be read, so it was left open."}
	}
	if job := foregroundJob(procs, fg); len(job) > 0 {
		if why := notTheChilds(job, startedBefore); why != "" {
			return Failure{Message: why + " The tab was left open."}
		}
		if err := e.signal(fg); err != nil && !errors.Is(err, syscall.ESRCH) {
			return Failure{Message: "The child could not be asked to leave (" + err.Error() + "), so the tab was left open."}
		}
		if !e.waitGone(ctx, fg) {
			return Failure{Message: "The child did not leave within " + e.wait.String() + " of being asked, " +
				"so the tab was left open."}
		}
		// What is in front now is read again: a job that came up while the
		// child left is not the child's either.
		procs, fg, err = e.processes(tty)
		if err != nil {
			return Failure{Message: "What runs in the tab could not be read after the child left, so it was left open."}
		}
		if len(foregroundJob(procs, fg)) > 0 {
			return Failure{Message: "Something else came to the front of the tab as the child left, so it was left open."}
		}
	}
	return e.close(ctx, id)
}

func (e itermEnder) waitGone(ctx context.Context, pgid int) bool {
	deadline := time.Now().Add(e.wait)
	for {
		if e.gone(pgid) {
			return true
		}
		if !time.Now().Before(deadline) || !pause(ctx, e.tick) {
			return false
		}
	}
}

// notTheChilds says why a job may not be ended as the child's: a process in
// it that started after the task ended, or one whose start the kernel would
// not give. Whole seconds on both sides, and a process that started in the
// task's last second is not given the benefit of the doubt.
func notTheChilds(job []ttyProc, startedBefore time.Time) string {
	if startedBefore.IsZero() {
		return "When the task ended is not known, so what runs in the tab cannot be told to be the child's."
	}
	for _, p := range job {
		if p.Start.IsZero() {
			return fmt.Sprintf("When %s (%d) started could not be read.", p.Comm, p.PID)
		}
		if !p.Start.Add(time.Second).Before(startedBefore) {
			return fmt.Sprintf("%s (%d) started after the task ended, so it is not the child's.", p.Comm, p.PID)
		}
	}
	return ""
}

// ttyProcesses asks the kernel for every process on a tty (`/dev/ttys039` or
// `ttys039`) and the tty's foreground process group.
func ttyProcesses(tty string) ([]ttyProc, int, error) {
	path := tty
	if !strings.HasPrefix(path, "/dev/") {
		path = "/dev/" + path
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, 0, errors.New("no device number for " + path)
	}
	rows, err := unix.SysctlKinfoProcSlice("kern.proc.tty", int(st.Rdev))
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, errors.New("no process on " + path)
	}
	fg := int(rows[0].Eproc.Tpgid)
	out := make([]ttyProc, 0, len(rows))
	for _, r := range rows {
		if int(r.Eproc.Tpgid) != fg {
			return nil, 0, errors.New("the foreground group of " + path + " changed while it was read")
		}
		p := ttyProc{PID: int(r.Proc.P_pid), PPID: int(r.Eproc.Ppid), PGID: int(r.Eproc.Pgid),
			Comm: unix.ByteSliceToString(r.Proc.P_comm[:])}
		if tv := r.Proc.P_starttime; tv.Sec > 0 {
			p.Start = time.Unix(tv.Sec, 0)
		}
		out = append(out, p)
	}
	return out, fg, nil
}

// groupGone is the kernel's answer that a process group has no process left.
// A group it could not be asked about is not gone.
func groupGone(pgid int) bool {
	rows, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	return err == nil && len(rows) == 0
}
