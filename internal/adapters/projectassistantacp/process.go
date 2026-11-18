package projectassistantacp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Limits struct {
	RPCPending          int
	IncomingPermissions int
	Notifications       int
	FrameBytes          int
	CaptureBytes        int // Process census only; native ACP text has no capture ceiling.
	StderrBytes         int
	OwnedDescendants    int
	WriteTimeout        time.Duration
	CleanupTimeout      time.Duration
	RPCTimeout          time.Duration
	DescendantPoll      time.Duration
}

func (l Limits) Validate() error {
	if l.RPCPending < 1 || l.IncomingPermissions < 1 || l.CaptureBytes < 1 || l.FrameBytes < 1 || l.StderrBytes < 1 || l.OwnedDescendants < 1 || l.WriteTimeout <= 0 || l.CleanupTimeout <= 0 || l.RPCTimeout <= 0 || l.DescendantPoll <= 0 {
		return refusal(pa.Invalid, "acp_limits")
	}
	return nil
}

type ProcessOptions struct {
	Configuration Configuration
	Directory     string
	Limits        Limits
	// CombinedOutput is for owned foreground commands only. ACP keeps its
	// protocol stdout separate from bounded diagnostic stderr by default.
	CombinedOutput bool
	// A package-private fault-injection barrier lets tests force child exit
	// before the initial inventory without racing a sleep against Wait.
	beforeInitialCapture func(*Process)
}
type Process struct {
	cmd            *exec.Cmd
	stdin          *os.File
	stdout         io.ReadCloser
	stderrReader   io.ReadCloser
	limits         Limits
	write          chan struct{}
	done           chan struct{}
	stopSurvey     context.CancelFunc
	surveyDone     chan struct{}
	closeOnce      sync.Once
	mu             sync.Mutex
	stderr         []byte
	waitErr        error
	cleanupErr     error
	owned          map[int]string
	rootObserved   bool
	rootStamp      string
	censusOverride func(context.Context) ([]processIdentity, error)
}

// A completed receipt wins over a simultaneously expired deadline.
func waitOwnedDone(ctx context.Context, done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
}

type processIdentity struct {
	pid, ppid int
	pgid      int
	state     string
	stamp     string
}

// ProcessSignalFailure preserves a stable domain code plus errno and phase.
// It never includes native prompts or stderr in its diagnostic text.
type ProcessSignalFailure struct {
	Phase   string
	Err     error
	Failure *pa.Failure
}

func (e *ProcessSignalFailure) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Failure.Code, e.Phase, e.Err)
}
func (e *ProcessSignalFailure) Unwrap() []error { return []error{e.Failure, e.Err} }
func signalFailure(phase string, err error) error {
	return &ProcessSignalFailure{Phase: phase, Err: err, Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: phase}}
}
func deadProcess(row processIdentity) bool {
	return len(row.state) > 0 && (row.state[0] == 'Z' || row.state[0] == 'X')
}
func liveGroup(rows []processIdentity, group int) bool {
	for _, row := range rows {
		if row.pgid == group && !deadProcess(row) {
			return true
		}
	}
	return false
}
func confirmStoppedGroup(rows []processIdentity, group int, signalErr error) error {
	if liveGroup(rows, group) {
		return signalFailure("signal_group_live", signalErr)
	}
	return nil
}

type censusCapture struct {
	buf    bytes.Buffer
	budget int
}

func (b *censusCapture) Write(p []byte) (int, error) {
	if len(p) > b.budget-b.buf.Len() {
		return 0, io.ErrShortBuffer
	}
	return b.buf.Write(p)
}

func (p *Process) captureRows(rows []processIdentity) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	live := make(map[int]processIdentity, len(rows))
	for _, row := range rows {
		live[row.pid] = row
	}
	for pid, stamp := range p.owned {
		if live[pid].stamp != stamp {
			delete(p.owned, pid)
		}
	}
	if root, ok := live[p.cmd.Process.Pid]; ok {
		if !p.rootObserved {
			p.rootStamp = root.stamp
			p.rootObserved = true
		}
		// The first unreaped root identity is immutable. A later process with
		// this PID must never become ours merely because its PID is reused.
		if root.stamp == p.rootStamp {
			p.owned[root.pid] = root.stamp
		}
	}
	if !p.rootObserved {
		p.cleanupErr = refusal(pa.ExecutionEffectUnknown, "root_identity")
		return p.cleanupErr
	}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			if _, ok := p.owned[row.pid]; ok {
				continue
			}
			parentStamp, owned := p.owned[row.ppid]
			if !owned || live[row.ppid].stamp != parentStamp {
				continue
			}
			if len(p.owned) >= p.limits.OwnedDescendants {
				p.cleanupErr = refusal(pa.CapacityFull, "owned_descendants")
				return p.cleanupErr
			}
			p.owned[row.pid] = row.stamp
			changed = true
		}
	}
	return nil
}

func StartProcess(ctx context.Context, o ProcessOptions) (*Process, error) {
	if err := o.Limits.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !absoluteExecutable(o.Configuration.Executable) || o.Directory == "" {
		return nil, refusal(pa.Invalid, "process_configuration")
	}
	cmd := exec.Command(o.Configuration.Executable, o.Configuration.Arguments...)
	cmd.Dir = o.Directory
	cmd.Env = append([]string{}, o.Configuration.Environment...)
	if err := configureProcess(cmd); err != nil {
		return nil, err
	}
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, err
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		outRead.Close()
		outWrite.Close()
		return nil, err
	}
	cmd.Stdin = inRead
	cmd.Stdout = outWrite
	cmd.Stderr = errWrite
	if o.CombinedOutput {
		cmd.Stderr = outWrite
	}
	p := &Process{cmd: cmd, stdin: inWrite, stdout: outRead, stderrReader: errRead, limits: o.Limits, write: make(chan struct{}, 1), done: make(chan struct{}), surveyDone: make(chan struct{}), owned: map[int]string{}}
	err = cmd.Start()
	inRead.Close()
	outWrite.Close()
	errWrite.Close()
	if err != nil {
		inWrite.Close()
		outRead.Close()
		errRead.Close()
		return nil, err
	}
	go func() {
		defer errRead.Close()
		buf := make([]byte, o.Limits.StderrBytes)
		for {
			n, e := errRead.Read(buf)
			if n > 0 {
				p.mu.Lock()
				remaining := o.Limits.StderrBytes - len(p.stderr)
				if n > remaining {
					n = remaining
				}
				p.stderr = append(p.stderr, buf[:n]...)
				p.mu.Unlock()
			}
			if e != nil {
				return
			}
		}
	}()
	if o.beforeInitialCapture != nil {
		o.beforeInitialCapture(p)
	}
	// Capture synchronously before Wait may reap a fast-exiting child. Until
	// then the PID cannot be reused, even when the child is already a zombie.
	// Returning from StartProcess is the initial ownership receipt.
	_, startupErr := p.captureOwned(context.Background())
	if startupErr != nil {
		p.setCleanupError(startupErr)
	}
	survey, stopSurvey := context.WithCancel(context.Background())
	p.stopSurvey = stopSurvey
	go func() { err := cmd.Wait(); p.mu.Lock(); p.waitErr = err; p.mu.Unlock(); close(p.done) }()
	go p.survey(survey)
	if startupErr != nil {
		_ = p.cmd.Process.Kill()
		cleanup, cancel := context.WithTimeout(context.Background(), o.Limits.CleanupTimeout)
		defer cancel()
		_ = p.Close(cleanup)
		return nil, startupErr
	}
	return p, nil
}

func (p *Process) Write(ctx context.Context, b []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, p.limits.WriteTimeout)
	defer cancel()
	select {
	case p.write <- struct{}{}:
	case <-writeCtx.Done():
		return refusal(pa.ExecutionEffectUnknown, "stdin_lock")
	}
	defer func() { <-p.write }()
	deadline, _ := writeCtx.Deadline()
	if err := p.stdin.SetWriteDeadline(deadline); err != nil {
		return refusal(pa.ExecutionEffectUnknown, "stdin_deadline")
	}
	// One syscall may write only a prefix. Never retry a possibly sent frame.
	n, err := p.stdin.Write(b)
	if err != nil || n != len(b) {
		return refusal(pa.ExecutionEffectUnknown, "stdin_partial")
	}
	return nil
}
func (p *Process) Stderr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(append([]byte(nil), p.stderr...))
}
func (p *Process) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		defer p.stdout.Close()
		defer p.stderrReader.Close()
		cleanup, cancel := context.WithTimeout(ctx, p.limits.CleanupTimeout)
		defer cancel()
		p.stopSurvey()
		p.stdin.Close()
		grace, graceCancel := context.WithTimeout(cleanup, p.limits.CleanupTimeout/2)
		if !waitOwnedDone(cleanup, p.surveyDone) {
			p.setCleanupError(signalFailure("cleanup_survey", cleanup.Err()))
		}
		// Survey before graceful exit, then retain exact PID/start identities for
		// escaped setsid descendants even if the ACP parent is already gone.
		if _, err := p.captureOwned(cleanup); err != nil {
			p.setCleanupError(err)
		}
		_ = waitOwnedDone(grace, p.done)
		graceCancel()
		rows, err := p.captureOwned(cleanup)
		if err != nil {
			p.setCleanupError(err)
		}
		// The fresh post-grace census has already removed every absent or
		// replaced identity. With no known owned process, another census for
		// each signal cannot authorize any action and can only exhaust the
		// remaining cleanup budget. Preserve any earlier inventory failure;
		// the direct child still has to provide its reap receipt below.
		p.mu.Lock()
		hasOwned := len(p.owned) > 0
		p.mu.Unlock()
		if hasOwned {
			// The post-grace census supplies TERM's fresh identities. KILL
			// still needs a fresh census because TERM affected that snapshot.
			if err := p.signalOwned(cleanup, rows, false); err != nil {
				p.setCleanupError(err)
			}
			if err := p.signalOwned(cleanup, nil, true); err != nil {
				p.setCleanupError(err)
			}
		}
		if !waitOwnedDone(cleanup, p.done) {
			_ = p.cmd.Process.Kill()
			if !waitOwnedDone(cleanup, p.done) {
				p.setCleanupError(signalFailure("process_cleanup", cleanup.Err()))
			}
		}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cleanupErr
}

func (p *Process) setCleanupError(err error) {
	p.mu.Lock()
	if p.cleanupErr == nil {
		p.cleanupErr = err
	}
	p.mu.Unlock()
}

func (p *Process) survey(ctx context.Context) {
	defer close(p.surveyDone)
	ticker := time.NewTicker(p.limits.DescendantPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err := p.captureOwned(ctx)
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return
			}
			if err != nil {
				p.setCleanupError(err)
			}
			if pa.Code(err) == pa.CapacityFull {
				p.stdin.Close()
				_ = p.signalOwned(ctx, nil, true)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
