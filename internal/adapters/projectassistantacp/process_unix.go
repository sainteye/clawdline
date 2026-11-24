//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func absoluteExecutable(path string) bool { return filepath.IsAbs(path) }
func configureProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func (p *Process) identities(ctx context.Context) ([]processIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, p.limits.CleanupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	override := p.censusOverride
	p.mu.Unlock()
	started := time.Now()
	var rows []processIdentity
	var err error
	if override != nil {
		rows, err = override(ctx)
	} else {
		rows, err = processIdentities(ctx, p.limits.CaptureBytes)
	}
	p.noteCensus(started, ctx, err)
	return rows, err
}

func psIdentities(ctx context.Context, budget int) ([]processIdentity, error) {
	// Only identifiers, group, state and start time are read. Arguments/environment may
	// contain secrets and are deliberately excluded from this census.
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=,ruid=,stat=,lstart=")
	capture := &censusCapture{budget: budget}
	cmd.Stdout = capture
	err := cmd.Run()
	b := capture.buf.Bytes()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Join(err, ctx.Err())
		}
		return nil, err
	}
	var result []processIdentity
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 6 {
			return nil, errors.New("invalid process census row")
		}
		pid, e := strconv.Atoi(f[0])
		if e != nil {
			return nil, e
		}
		ppid, e := strconv.Atoi(f[1])
		if e != nil {
			return nil, e
		}
		pgid, e := strconv.Atoi(f[2])
		if e != nil {
			return nil, e
		}
		uid, e := strconv.Atoi(f[3])
		if e != nil {
			return nil, e
		}
		result = append(result, processIdentity{pid: pid, ppid: ppid, pgid: pgid, uid: uid, state: f[4], stamp: strings.Join(f[5:], " ")})
	}
	return result, nil
}
func (p *Process) kill(pid int, sig syscall.Signal) error {
	if p.signal != nil {
		return p.signal(pid, sig)
	}
	return syscall.Kill(pid, sig)
}
func (p *Process) captureOwned(ctx context.Context) ([]processIdentity, error) {
	rows, err := p.identities(ctx)
	if err != nil {
		return nil, signalFailure("descendant_inventory", err)
	}
	return rows, p.captureRows(rows)
}
func (p *Process) signalOwned(ctx context.Context, rows []processIdentity, force bool) error {
	err := ctx.Err()
	if err == nil && rows == nil {
		rows, err = p.identities(ctx)
	}
	if err != nil {
		// The os.Process handle still safely identifies our direct child when
		// process inventory is denied. This does not prove descendant cleanup.
		var directErr error
		if force {
			directErr = p.cmd.Process.Kill()
		} else {
			directErr = p.cmd.Process.Signal(os.Interrupt)
		}
		if errors.Is(directErr, os.ErrProcessDone) {
			directErr = nil
		}
		return signalFailure("signal_inventory", errors.Join(err, directErr))
	}
	p.mu.Lock()
	owned := make(map[int]string, len(p.owned))
	for id, stamp := range p.owned {
		owned[id] = stamp
	}
	p.mu.Unlock()
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	var result error
	group := p.cmd.Process.Pid
	ownedGroup := false
	for _, row := range rows {
		stamp, ok := owned[row.pid]
		if !ok || stamp != row.stamp {
			continue
		}
		if row.pgid == group {
			ownedGroup = true
		}
		if deadProcess(row) {
			continue
		}
		if signalErr := p.kill(row.pid, sig); signalErr != nil && signalErr != syscall.ESRCH {
			fresh, censusErr := p.identities(ctx)
			if censusErr != nil {
				result = errors.Join(result, signalFailure("signal_pid_inventory", errors.Join(signalErr, censusErr)))
				continue
			}
			for _, current := range fresh {
				if current.pid == row.pid && current.stamp == stamp && !stoppedProcess(current) {
					result = errors.Join(result, signalFailure("signal_pid_live", signalErr))
					break
				}
			}
		}
	}
	// The complete fresh census distinguishes an already stopped group (zombies
	// or members already exiting) from a denied signal against a live owned
	// group. Never blanket-ignore EPERM.
	if ownedGroup && liveGroup(rows, group) {
		if signalErr := p.kill(-group, sig); signalErr != nil && signalErr != syscall.ESRCH {
			fresh, censusErr := p.identities(ctx)
			if censusErr != nil {
				result = errors.Join(result, signalFailure("signal_group_inventory", errors.Join(signalErr, censusErr)))
			} else {
				result = errors.Join(result, confirmStoppedGroup(fresh, group, p.rootStamp, signalErr))
			}
		}
	}
	return result
}
