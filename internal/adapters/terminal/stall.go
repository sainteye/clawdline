package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// An iTerm2 that stops answering Apple Events leaves, by itself, nothing but
// `signal: killed` in this daemon's log: not which script, not which session,
// not how long it waited, and nothing about what iTerm2 was doing. Between
// 2026-09-23 and 2026-10-02 this machine's log held 536 such failures in 29
// clusters; the eleven long ones ran 3 to 23 minutes at 8 to 11 a minute, and
// the rest were 1 to 4 failures inside a minute that cleared by themselves.
// So every failure is named in one line (scriptFailure), and a run of
// timeouts writes one diagnosis of the moment it is happening in (stallWatch).

// Why one osascript run failed, as a caller can branch on it.
const (
	// reasonKilledAtDeadline: the run outlived its own deadline and was killed.
	reasonKilledAtDeadline = "killed_at_limit"
	// reasonCancelled: whoever asked stopped waiting before the deadline.
	reasonCancelled = "cancelled"
	// reasonAppleEvent: osascript answered an Apple Event error number.
	reasonAppleEvent = "apple_event_error"
	// reasonOther: anything else, such as a script that threw.
	reasonOther = "other"
)

// errAETimeout is the Apple Event error iTerm2 answers when it took the event
// and did not reply within the event's own timeout.
const errAETimeout = -1712

// scriptFailure is one failed iTerm2 osascript run.
type scriptFailure struct {
	At      time.Time
	Kind    string // list, find, send, close, key, capture, reveal, open, ...
	Session string // the iTerm2 session id, when the run was about one
	Elapsed time.Duration
	Limit   time.Duration // the run's own deadline, 0 when it had none
	Reason  string
	Code    int    // the Apple Event error number, with reasonAppleEvent
	Err     string // the process error, with reasonOther
	// Said is the end of what osascript wrote on stderr. It stays in this
	// machine's log and its diagnoses and is never part of a refusal.
	Said string
}

// timedOut is whether the failure says iTerm2 did not answer in time — the
// only failures a stall is made of. A script that threw, or a caller that
// gave up, says nothing about whether iTerm2 is answering.
func (f scriptFailure) timedOut() bool {
	return f.Reason == reasonKilledAtDeadline || (f.Reason == reasonAppleEvent && f.Code == errAETimeout)
}

// line is the failure as the log carries it and as a diagnosis lists it.
func (f scriptFailure) line() string {
	session := f.Session
	if session == "" {
		session = "-"
	}
	out := fmt.Sprintf("kind=%s session=%s elapsed=%s reason=%s", f.Kind, session,
		f.Elapsed.Round(time.Millisecond), f.Reason)
	switch f.Reason {
	case reasonKilledAtDeadline:
		out += " limit=" + f.Limit.String()
	case reasonAppleEvent:
		out += " code=" + strconv.Itoa(f.Code)
	case reasonOther:
		out += fmt.Sprintf(" err=%q", f.Err)
	}
	if f.Said != "" {
		out += fmt.Sprintf(" said=%q", f.Said)
	}
	return out
}

var appleEventCode = regexp.MustCompile(`\((-\d{1,6})\)`)

// classifyScriptFailure reads why a run failed from the context it ran under,
// what osascript said on stderr, and the process error.
func classifyScriptFailure(ctxErr error, stderr string, err error) (reason string, code int) {
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return reasonKilledAtDeadline, 0
	case errors.Is(ctxErr, context.Canceled):
		return reasonCancelled, 0
	}
	if m := appleEventCode.FindAllStringSubmatch(stderr, -1); len(m) > 0 {
		n, convErr := strconv.Atoi(m[len(m)-1][1])
		if convErr == nil {
			return reasonAppleEvent, n
		}
	}
	return reasonOther, 0
}

// The trigger. An episode runs 8 to 11 timeouts a minute, so five are reached
// within the first 30 to 40 seconds of one; of the 29 clusters in the log, the
// 19 that cleared by themselves had at most four inside two minutes, and every
// one that ran a minute or longer had at least eleven.
const (
	stallTrigger = 5
	stallWindow  = 2 * time.Minute
)

// StallLimits are the stall watch's registered bounds (capacity rows
// iterm.stall_*), handed over by the composition root.
type StallLimits struct {
	// Failures is how many recent failures are held for the next diagnosis.
	Failures int
	// Diagnoses is how many diagnosis files are kept; older ones are removed.
	Diagnoses int
	// Cooldown is the least time between two diagnoses.
	Cooldown time.Duration
	// SectionBytes is the most of one step's output a diagnosis carries.
	SectionBytes int
	// Step is the longest any one step may take before it is abandoned.
	Step time.Duration
}

// The registered defaults (internal/domain/capacity, docs/limits.md N64).
const (
	stallFailuresLimit  = 32
	stallDiagnosesLimit = 20
	stallCooldownLimit  = 30 * time.Minute
	stallSectionLimit   = 512 << 10
	stallStepLimit      = 30 * time.Second
)

// DefaultStallLimits are the register's own values.
func DefaultStallLimits() StallLimits {
	return StallLimits{Failures: stallFailuresLimit, Diagnoses: stallDiagnosesLimit,
		Cooldown: stallCooldownLimit, SectionBytes: stallSectionLimit, Step: stallStepLimit}
}

// stallStep is one section of a diagnosis. Run is given a context bounded by
// Timeout (at most the Step limit); a step that does not come back by then is
// recorded as timed out and the file goes on without it.
type stallStep struct {
	Title   string
	Timeout time.Duration
	Run     func(ctx context.Context) ([]byte, error)
}

// stallFilePrefix names a diagnosis file in the logs directory.
const stallFilePrefix = "iterm-stall-"

// stallWatch holds the recent failures and decides when to write a diagnosis.
// One diagnosis runs at a time, in its own goroutine; record never waits on
// it.
type stallWatch struct {
	mu      sync.Mutex
	dir     string // the logs directory; "" writes nothing
	limits  StallLimits
	now     func() time.Time
	steps   func(dir string) []stallStep
	logf    func(format string, args ...any)
	recent  []scriptFailure
	last    time.Time // when the last diagnosis was started
	running bool
	done    func(path string, err error) // for tests: called when a diagnosis ends
}

func newStallWatch(steps func(dir string) []stallStep) *stallWatch {
	return &stallWatch{limits: DefaultStallLimits(), now: time.Now, steps: steps, logf: log.Printf}
}

// itermStall is this process's watch. It holds failures from the start and
// writes nothing until ConfigureITermStall names a directory.
var itermStall = newStallWatch(itermStallSteps)

// ConfigureITermStall names the directory diagnoses are written to — this
// daemon's own logs directory — and the registered bounds. Limits that are not
// positive keep the defaults.
func ConfigureITermStall(dir string, limits StallLimits) {
	itermStall.configure(dir, limits)
}

func (w *stallWatch) configure(dir string, limits StallLimits) {
	def := DefaultStallLimits()
	if limits.Failures <= 0 {
		limits.Failures = def.Failures
	}
	if limits.Diagnoses <= 0 {
		limits.Diagnoses = def.Diagnoses
	}
	if limits.Cooldown <= 0 {
		limits.Cooldown = def.Cooldown
	}
	if limits.SectionBytes <= 0 {
		limits.SectionBytes = def.SectionBytes
	}
	if limits.Step <= 0 {
		limits.Step = def.Step
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dir, w.limits = dir, limits
	if over := len(w.recent) - limits.Failures; over > 0 {
		w.recent = append([]scriptFailure(nil), w.recent[over:]...)
	}
}

// record logs one failure and, when it completes a run of timeouts, starts a
// diagnosis. It answers whether it started one.
func (w *stallWatch) record(f scriptFailure) bool {
	if f.At.IsZero() {
		f.At = w.now()
	}
	w.logf("iterm: osascript failed: %s", f.line())
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recent = append(w.recent, f)
	if over := len(w.recent) - w.limits.Failures; over > 0 {
		w.recent = append(w.recent[:0:0], w.recent[over:]...)
	}
	if !f.timedOut() || w.dir == "" || w.running {
		return false
	}
	n := 0
	for _, r := range w.recent {
		if r.timedOut() && !r.At.Before(f.At.Add(-stallWindow)) && !r.At.After(f.At) {
			n++
		}
	}
	if n < stallTrigger {
		return false
	}
	if !w.last.IsZero() && f.At.Sub(w.last) < w.limits.Cooldown {
		return false
	}
	// The cooldown is claimed when the diagnosis starts, not when it is
	// written: a diagnosis that could not be written is not retried on the
	// next failure, which in an episode is seconds away.
	w.last, w.running = f.At, true
	failures := append([]scriptFailure(nil), w.recent...)
	dir, limits := w.dir, w.limits
	go func() {
		path, err := w.diagnose(dir, limits, f.At, n, failures)
		w.mu.Lock()
		w.running = false
		done := w.done
		w.mu.Unlock()
		if done != nil {
			done(path, err)
		}
	}()
	return true
}

// diagnose writes one diagnosis file and keeps only the newest ones.
func (w *stallWatch) diagnose(dir string, limits StallLimits, at time.Time, n int, failures []scriptFailure) (string, error) {
	// Before the steps: a step may write into it (sample's report does).
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.logf("iterm: stall diagnosis not written: %v", err)
		return "", err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "== iTerm2 stall diagnosis ==\n")
	fmt.Fprintf(&b, "triggered: %s\n", at.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "trigger: %d timed-out Apple Events within %s (threshold %d)\n", n, stallWindow, stallTrigger)
	fmt.Fprintf(&b, "\n== recent failures (newest last, at most %d) ==\n", limits.Failures)
	for _, f := range failures {
		fmt.Fprintf(&b, "%s %s\n", f.At.UTC().Format(time.RFC3339Nano), f.line())
	}
	for _, step := range w.steps(dir) {
		b.WriteString("\n")
		runStallStep(&b, step, limits)
	}
	name := stallFilePrefix + at.UTC().Format("20060102T150405.000000000Z") + ".txt"
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		os.Remove(tmp)
		w.logf("iterm: stall diagnosis not written: %v", err)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		w.logf("iterm: stall diagnosis not written: %v", err)
		return "", err
	}
	w.logf("iterm: stall diagnosis written: %s (%d timed-out Apple Events within %s)", path, n, stallWindow)
	if err := pruneStallDiagnoses(dir, limits.Diagnoses); err != nil {
		w.logf("iterm: older stall diagnoses not removed: %v", err)
	}
	return path, nil
}

// runStallStep writes one section. Each step has its own deadline and a step
// that fails or hangs is written down as such; the file never stops at it.
func runStallStep(b *bytes.Buffer, step stallStep, limits StallLimits) {
	timeout := step.Timeout
	if timeout <= 0 || timeout > limits.Step {
		timeout = limits.Step
	}
	fmt.Fprintf(b, "== %s ==\n", step.Title)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	type answer struct {
		out []byte
		err error
	}
	// Buffered, so a step that ignores its context and comes back late does
	// not stay blocked on a send nobody is waiting for.
	got := make(chan answer, 1)
	started := time.Now()
	go func() {
		out, err := step.Run(ctx)
		got <- answer{out, err}
	}()
	var a answer
	timedOut := false
	select {
	case a = <-got:
		timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	case <-ctx.Done():
		timedOut = true
	}
	fmt.Fprintf(b, "elapsed: %s\n", time.Since(started).Round(time.Millisecond))
	switch {
	case timedOut:
		fmt.Fprintf(b, "status: timed out after %s\n", timeout)
	case a.err != nil:
		fmt.Fprintf(b, "status: failed: %v\n", a.err)
	default:
		b.WriteString("status: ok\n")
	}
	out, dropped := a.out, 0
	if len(out) > limits.SectionBytes {
		dropped = len(out) - limits.SectionBytes
		out = out[:limits.SectionBytes]
	}
	if len(out) > 0 {
		b.Write(out)
		if out[len(out)-1] != '\n' {
			b.WriteString("\n")
		}
	}
	if dropped > 0 {
		fmt.Fprintf(b, "[truncated: %d bytes not written]\n", dropped)
	}
}

// pruneStallDiagnoses keeps the newest keep diagnoses. The names sort by the
// time they were triggered, so the oldest are first.
func pruneStallDiagnoses(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), stallFilePrefix) && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var errs []error
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			errs = append(errs, err)
		}
		names = names[1:]
	}
	return errors.Join(errs...)
}

// ITermStallReading is what the stall watch holds now: the failures kept for
// the next diagnosis, and the diagnosis files in its directory. A directory
// that does not exist yet holds none; one that cannot be read is an error,
// never a zero.
func ITermStallReading() (failures, diagnoses int, err error) {
	return itermStall.reading()
}

func (w *stallWatch) reading() (int, int, error) {
	w.mu.Lock()
	failures, dir := len(w.recent), w.dir
	w.mu.Unlock()
	if dir == "" {
		return failures, 0, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return failures, 0, nil
	}
	if err != nil {
		return failures, 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), stallFilePrefix) && strings.HasSuffix(e.Name(), ".txt") {
			n++
		}
	}
	return failures, n, nil
}
