package projectassistantacp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Limits struct {
	RPCPending       int
	Notifications    int
	FrameBytes       int
	CaptureBytes     int // Process census only; native ACP text has no capture ceiling.
	StderrBytes      int
	OwnedDescendants int
	WriteTimeout     time.Duration
	CleanupTimeout   time.Duration
	RPCTimeout       time.Duration
	DescendantPoll   time.Duration
}

func (l Limits) Validate() error {
	if l.RPCPending < 1 || l.CaptureBytes < 1 || l.FrameBytes < 1 || l.StderrBytes < 1 || l.OwnedDescendants < 1 || l.WriteTimeout <= 0 || l.CleanupTimeout <= 0 || l.RPCTimeout <= 0 || l.DescendantPoll <= 0 {
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
	cmd          *exec.Cmd
	stdin        *os.File
	stdout       io.ReadCloser
	stderrReader io.ReadCloser
	limits       Limits
	write        chan struct{}
	done         chan struct{}
	stopSurvey   context.CancelFunc
	surveyDone   chan struct{}
	closeOnce    sync.Once
	mu           sync.Mutex
	stderr       []byte
	waitErr      error
	cleanupErr   error
	// cleanupFinal marks a cleanup error that a later census cannot settle
	// (a phase in finalCleanupPhases).
	cleanupFinal bool
	reverifyMu   sync.Mutex
	// signal is a package-private failure-injection seam; nil sends real signals.
	signal         func(pid int, sig syscall.Signal) error
	owned          map[int]string
	rootObserved   bool
	rootStamp      string
	censusOverride func(context.Context) ([]processIdentity, error)
	tcp            *tcpObserver
	// lastCensus is when the last successful census started; gap is the
	// widest period since then without one (nil when there was none).
	lastCensus time.Time
	gap        *censusGap
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

// exitingProcess is a BSD/macOS ps "E" flag: the kernel has begun exit for
// this process (for example while it tears down a large heap after SIGKILL).
// It can no longer run its own code, and kill(-pgid) skips it, so a group of
// only such members answers EPERM. Linux ps does not use this letter.
func exitingProcess(row processIdentity) bool {
	return len(row.state) > 1 && strings.ContainsRune(row.state[1:], 'E')
}

// stoppedProcess means the process can take no further action of its own.
func stoppedProcess(row processIdentity) bool { return deadProcess(row) || exitingProcess(row) }

func liveGroup(rows []processIdentity, group int) bool {
	for _, row := range rows {
		if row.pgid == group && !stoppedProcess(row) {
			return true
		}
	}
	return false
}

// liveOwnedGroup reports a live member of the group our root created. A
// process group id cannot be allocated again while that group still exists,
// so a leader with our group id and a different start time proves our group
// is gone; its members are a stranger's, not ours.
func liveOwnedGroup(rows []processIdentity, group int, rootStamp string) bool {
	for _, row := range rows {
		if row.pid == group && rootStamp != "" && row.stamp != "" && row.stamp != rootStamp {
			return false
		}
	}
	return liveGroup(rows, group)
}

// A denied group signal is only harmless when a complete fresh census shows
// no live member of our own group. Never blanket-ignore EPERM.
func confirmStoppedGroup(rows []processIdentity, group int, rootStamp string, signalErr error) error {
	if liveOwnedGroup(rows, group, rootStamp) {
		return signalFailure("signal_group_live", signalErr)
	}
	return nil
}

// finalCleanupPhases are the only cleanup failures a later census can never
// settle, each with the reason. Every other phase, including one added later,
// is re-verifiable: a later Close re-runs the complete evidence check in
// reverify, which releases nothing without a complete fresh census.
var finalCleanupPhases = map[string]string{
	// The root was never seen in a census taken while it was still unreaped.
	// After Wait reaps it, its start time, and so our group's identity and
	// every attribution through it, can no longer be learned.
	"root_identity": "the root's identity can no longer be observed once it is reaped",
	// The Agent created more descendants than the bound records; the
	// unrecorded ones were dropped by the bound itself, not by a passing
	// failure, and a repeated census does not bring them back.
	"owned_descendants": "descendants beyond the ownership bound were never recorded",
}

// reverifiable reports whether no part of err names a final phase. A phase is
// the Field of each pa.Failure in the error tree (ProcessSignalFailure carries
// its phase there too).
func reverifiable(err error) bool {
	if err == nil {
		return false
	}
	return !hasFinalPhase(err)
}

func hasFinalPhase(err error) bool {
	switch e := err.(type) {
	case *pa.Failure:
		_, final := finalCleanupPhases[e.Field]
		return final
	case interface{ Unwrap() []error }:
		for _, inner := range e.Unwrap() {
			if inner != nil && hasFinalPhase(inner) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		if inner := e.Unwrap(); inner != nil {
			return hasFinalPhase(inner)
		}
	}
	return false
}

// censusGap is a period without a complete census: from the start of the
// last successful census until the end of the next one. A descendant that
// left our group and lost its parent inside it may never have been seen.
// Repeated failures widen one window, so it never grows a list.
type censusGap struct {
	from, to time.Time // zero from: since before the first census; zero to: still open
	open     bool
}

// gapSlack covers the census's own start-time resolution (one second for ps
// lstart) and the distance between our clock reading and the kernel's.
const gapSlack = 2 * time.Second

func (p *Process) noteCensus(started time.Time, ctx context.Context, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		if p.gap != nil && p.gap.open {
			p.gap.to, p.gap.open = time.Now(), false
		}
		p.lastCensus = started
		return
	}
	// The caller abandoning a census (Close stopping the survey) leaves no
	// longer interval than the poll itself; a real failure does.
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return
	}
	if p.gap == nil {
		p.gap = &censusGap{from: p.lastCensus}
	}
	p.gap.open = true
}

// gapSettled reports, under p.mu, whether a complete census taken after the
// gap closed shows nothing that might be an unseen descendant from it: a live
// process outside our ownership record that started within the gap and whose
// oldest such ancestor hangs from the orphan reaper or from one of ours.
func (p *Process) gapSettled(rows []processIdentity) bool {
	g := p.gap
	if g == nil {
		return true
	}
	if g.open {
		return false
	}
	live := make(map[int]processIdentity, len(rows))
	for _, row := range rows {
		live[row.pid] = row
	}
	inGap := func(row processIdentity) bool {
		if stamp, ours := p.owned[row.pid]; ours && stamp == row.stamp {
			return false
		}
		born, ok := censusStampTime(row.stamp)
		if !ok {
			return true
		}
		return (g.from.IsZero() || !born.Before(g.from.Add(-gapSlack))) && !born.After(g.to.Add(gapSlack))
	}
	for _, row := range rows {
		if stoppedProcess(row) || !inGap(row) {
			continue
		}
		top := row
		for seen := 0; seen < len(rows); seen++ {
			parent, ok := live[top.ppid]
			if !ok || !inGap(parent) {
				break
			}
			top = parent
		}
		if _, ours := p.owned[top.ppid]; ours || orphanReaper(top.ppid) {
			return false
		}
		if _, ok := live[top.ppid]; !ok {
			return false
		}
	}
	return true
}

// orphanReaper reports whether a parent may be the process that adopts
// orphans. XNU always reparents to launchd (pid 1) and has no subreapers.
// Elsewhere a subreaper such as systemd --user can adopt them, so any parent
// may be one.
func orphanReaper(ppid int) bool { return ppid == 1 || !orphansGoToInit }

// censusStampTime reads a start time in either census format: RFC 3339 from
// the macOS sysctl census, ps lstart elsewhere.
func censusStampTime(stamp string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.Join(strings.Fields(stamp), " "), time.Local); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(strings.Fields(stamp), " "), time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
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
		p.cleanupFinal = true
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
				p.cleanupFinal = true
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
	if o.CombinedOutput {
		// Pin while the direct child cannot yet be reaped or its PID reused.
		// A missing optional observation capability never grants ownership or
		// prevents the existing command owner from retaining its process.
		observe, cancel := context.WithTimeout(context.Background(), o.Limits.CleanupTimeout)
		p.tcp = newTCPObserver(observe, cmd.Process.Pid, o.Limits, p.tcpIdentities)
		cancel()
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

// errWriteNotStarted lets the ACP client leave other in-flight requests alone
// when this caller is cancelled before writing a byte.
var errWriteNotStarted = errors.New("ACP write not started")

func (p *Process) Write(ctx context.Context, b []byte) error {
	select {
	case p.write <- struct{}{}:
	case <-ctx.Done():
		return errors.Join(errWriteNotStarted, ctx.Err())
	case <-p.done:
		return refusal(pa.ExecutionEffectUnknown, "acp_closed")
	}
	defer func() { <-p.write }()
	if err := ctx.Err(); err != nil {
		return errors.Join(errWriteNotStarted, err)
	}
	deadline, _ := ctx.Deadline()
	if err := p.stdin.SetWriteDeadline(deadline); err != nil {
		return refusal(pa.ExecutionEffectUnknown, "stdin_deadline")
	}
	// Interrupt a blocked pipe on Stop even for an otherwise open-ended turn.
	// Drain the callback before releasing the lock, so it cannot affect the
	// next writer's deadline.
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = p.stdin.SetWriteDeadline(time.Now())
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	n, err := p.stdin.Write(b)
	if n == 0 && ctx.Err() != nil {
		return errors.Join(errWriteNotStarted, ctx.Err())
	}
	if err != nil || n != len(b) {
		// A possibly transmitted prefix must never be retried on this pipe.
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
	first := false
	p.closeOnce.Do(func() {
		first = true
		if p.tcp != nil && p.tcp.root != nil {
			defer p.tcp.root.close()
		}
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
	if !first {
		p.reverify(ctx)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cleanupErr
}

// reverify retries any cleanup failure not in finalCleanupPhases on a later
// Close: it takes a complete fresh census, signals any still-live owned
// identity again, and clears the error only when the direct child has been
// reaped, no owned identity or member of our group is live, and no process
// from a census gap might be an unseen descendant. Any failure keeps the old
// error; nothing is released or resent without that complete evidence.
func (p *Process) reverify(ctx context.Context) {
	p.reverifyMu.Lock()
	defer p.reverifyMu.Unlock()
	p.mu.Lock()
	err, final := p.cleanupErr, p.cleanupFinal
	p.mu.Unlock()
	if err == nil || final || !reverifiable(err) {
		return
	}
	// A survey that has not returned may still be inside a census.
	if !waitOwnedDone(ctx, p.surveyDone) {
		return
	}
	cleanup, cancel := context.WithTimeout(ctx, p.limits.CleanupTimeout)
	defer cancel()
	rows, censusErr := p.identities(cleanup)
	if censusErr != nil {
		return
	}
	p.pruneOwned(rows)
	if p.ownedCount() > 0 {
		if p.signalOwned(cleanup, rows, false) != nil || p.signalOwned(cleanup, nil, true) != nil {
			return
		}
	}
	if !waitOwnedDone(cleanup, p.done) {
		_ = p.cmd.Process.Kill()
		if !waitOwnedDone(cleanup, p.done) {
			return
		}
	}
	fresh, censusErr := p.identities(cleanup)
	if censusErr != nil {
		return
	}
	p.pruneOwned(fresh)
	p.mu.Lock()
	defer p.mu.Unlock()
	live := make(map[int]processIdentity, len(fresh))
	for _, row := range fresh {
		live[row.pid] = row
	}
	for pid, stamp := range p.owned {
		if row, ok := live[pid]; ok && row.stamp == stamp && !stoppedProcess(row) {
			return
		}
	}
	if liveOwnedGroup(fresh, p.cmd.Process.Pid, p.rootStamp) {
		return
	}
	if !p.gapSettled(fresh) {
		return
	}
	p.cleanupErr, p.gap = nil, nil
}

// pruneOwned forgets identities that a complete census shows absent or
// replaced. It never adopts a new process.
func (p *Process) pruneOwned(rows []processIdentity) {
	live := make(map[int]string, len(rows))
	for _, row := range rows {
		live[row.pid] = row.stamp
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for pid, stamp := range p.owned {
		if current, ok := live[pid]; !ok || current != stamp {
			delete(p.owned, pid)
		}
	}
}

func (p *Process) ownedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.owned)
}

func (p *Process) setCleanupError(err error) {
	p.mu.Lock()
	if p.cleanupErr == nil {
		p.cleanupErr = err
	}
	if err != nil && !reverifiable(err) {
		p.cleanupFinal = true
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
