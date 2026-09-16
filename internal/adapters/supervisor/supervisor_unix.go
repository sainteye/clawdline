//go:build darwin || linux

// Package supervisor owns process trees: starting one, and taking all of it
// away again.
//
// No language and no pseudoterminal library gives you this. Rust's kill takes
// the direct child, Node has no killpg at all, and none of the Go pty packages
// offer it either. It has to be written once per platform, and it is not
// optional: what runs under a session here is an assistant, and the assistant
// spawns node, git and compilers of its own. "Cancel" that reaches only the top
// process leaves a tree running that nobody is watching.
package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Process is a running tree, addressed by its group rather than by its root.
type Process struct {
	Cmd  *exec.Cmd
	PGID int
}

// Start runs a command as the leader of a new process group.
//
// Setpgid is what makes the tree addressable: every descendant inherits the
// group unless it deliberately leaves, so one signal reaches all of them. Doing
// this at spawn time is the only chance — a tree that was never grouped cannot
// be grouped afterwards.
func Start(name string, args []string, dir string, env []string) (*Process, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Without a group there is no tree to address later, so this is
		// reported rather than swallowed: a caller that believes it can cancel
		// later is worse off than one told it cannot.
		return nil, fmt.Errorf("started %s but could not read its process group: %w", name, err)
	}
	return &Process{Cmd: cmd, PGID: pgid}, nil
}

// Stop takes the whole tree away.
//
// The negative pid is the group. TERM first, because a process that can clean
// up should be given the chance, then KILL for whatever ignored it. The wait
// between them is a value rather than a constant so a test can reach the
// second stage without waiting out a real grace period.
func (p *Process) Stop(grace time.Duration) error {
	if p == nil || p.PGID == 0 {
		return fmt.Errorf("there is no process group to stop")
	}
	if err := syscall.Kill(-p.PGID, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !p.groupAlive() {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(-p.PGID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

// groupAlive asks whether anything is still in the group. Signal 0 delivers
// nothing and only reports whether a target exists.
func (p *Process) groupAlive() bool {
	err := syscall.Kill(-p.PGID, syscall.Signal(0))
	return err == nil
}

// Detach lets a tree outlive this daemon.
//
// A session somebody is working in should not die because the coordinator
// restarted. Releasing it is a decision with a name rather than a side effect
// of exiting.
func (p *Process) Detach() error {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return fmt.Errorf("there is nothing to detach")
	}
	return p.Cmd.Process.Release()
}

var _ = os.Getpid
