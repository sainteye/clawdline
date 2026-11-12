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
)

func absoluteExecutable(path string) bool { return filepath.IsAbs(path) }
func configureProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func (p *Process) identities() ([]processIdentity, error) {
	base, override := p.censusBudget()
	ctx, cancel := context.WithTimeout(base, p.limits.CleanupTimeout)
	defer cancel()
	if override != nil {
		return override(ctx)
	}
	// Only identifiers, group, state and start time are read. Arguments/environment may
	// contain secrets and are deliberately excluded from this census.
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=,stat=,lstart=")
	capture := &censusCapture{budget: p.limits.CaptureBytes}
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
		if len(f) < 5 {
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
		result = append(result, processIdentity{pid: pid, ppid: ppid, pgid: pgid, state: f[3], stamp: strings.Join(f[4:], " ")})
	}
	return result, nil
}
func (p *Process) captureOwned() {
	rows, err := p.identities()
	if err != nil {
		p.mu.Lock()
		stopping := p.closing
		p.mu.Unlock()
		// Closing intentionally cancels an in-flight survey before replacing
		// it with the one remaining cleanup budget. Signal/cleanup operations
		// still preserve failures of their fresh census.
		if stopping && errors.Is(err, context.Canceled) {
			return
		}
		p.setCleanupError(signalFailure("descendant_inventory", err))
		return
	}
	_ = p.captureRows(rows)
}
func (p *Process) signalOwned(force bool) error {
	rows, err := p.identities()
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
		if signalErr := syscall.Kill(row.pid, sig); signalErr != nil && signalErr != syscall.ESRCH {
			fresh, censusErr := p.identities()
			if censusErr != nil {
				result = errors.Join(result, signalFailure("signal_pid_inventory", errors.Join(signalErr, censusErr)))
				continue
			}
			for _, current := range fresh {
				if current.pid == row.pid && current.stamp == stamp && !deadProcess(current) {
					result = errors.Join(result, signalFailure("signal_pid_live", signalErr))
					break
				}
			}
		}
	}
	// The complete fresh census distinguishes an already stopped zombie group
	// from a denied signal against a live owned group. Never blanket-ignore EPERM.
	if ownedGroup && liveGroup(rows, group) {
		if signalErr := syscall.Kill(-group, sig); signalErr != nil && signalErr != syscall.ESRCH {
			fresh, censusErr := p.identities()
			if censusErr != nil {
				result = errors.Join(result, signalFailure("signal_group_inventory", errors.Join(signalErr, censusErr)))
			} else {
				result = errors.Join(result, confirmStoppedGroup(fresh, group, signalErr))
			}
		}
	}
	return result
}
