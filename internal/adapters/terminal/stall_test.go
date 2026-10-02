package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var stallT0 = time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

// stallHarness is a watch writing into a directory of the test's own, with
// steps the test supplies — never the real sample — and a log it can read.
type stallHarness struct {
	w     *stallWatch
	dir   string
	done  chan string
	mu    sync.Mutex
	lines []string
}

func newStallHarness(t *testing.T, steps []stallStep, limits StallLimits) *stallHarness {
	t.Helper()
	h := &stallHarness{dir: filepath.Join(t.TempDir(), "logs"), done: make(chan string, 8)}
	h.w = newStallWatch(func(string) []stallStep { return steps })
	h.w.logf = func(format string, args ...any) {
		h.mu.Lock()
		h.lines = append(h.lines, fmt.Sprintf(format, args...))
		h.mu.Unlock()
	}
	h.w.done = func(path string, err error) {
		if err != nil {
			path = "error: " + err.Error()
		}
		h.done <- path
	}
	h.w.configure(h.dir, limits)
	return h
}

func (h *stallHarness) timeout(at time.Time) bool {
	return h.w.record(scriptFailure{At: at, Kind: "capture", Session: "w0t0p0:ABC",
		Elapsed: 6 * time.Second, Limit: 6 * time.Second, Reason: reasonKilledAtDeadline})
}

func (h *stallHarness) wait(t *testing.T) string {
	t.Helper()
	select {
	case path := <-h.done:
		return path
	case <-time.After(10 * time.Second):
		t.Fatal("the diagnosis never ended")
		return ""
	}
}

func (h *stallHarness) files(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stallFilePrefix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (h *stallHarness) log() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.lines, "\n")
}

func TestAStallDiagnosisStartsAtTheThresholdAndNotBelow(t *testing.T) {
	h := newStallHarness(t, nil, DefaultStallLimits())
	// Failures that are not timeouts say nothing about a stall.
	h.w.record(scriptFailure{At: stallT0, Kind: "send", Reason: reasonOther, Err: "exit status 1"})
	h.w.record(scriptFailure{At: stallT0, Kind: "send", Reason: reasonCancelled})
	h.w.record(scriptFailure{At: stallT0, Kind: "send", Reason: reasonAppleEvent, Code: -1743})
	for n := 1; n < stallTrigger; n++ {
		if h.timeout(stallT0.Add(time.Duration(n) * time.Second)) {
			t.Fatalf("a diagnosis started after %d timeouts", n)
		}
	}
	// -1712 counts as a timeout as much as a kill at the limit does.
	if !h.w.record(scriptFailure{At: stallT0.Add(10 * time.Second), Kind: "list",
		Reason: reasonAppleEvent, Code: errAETimeout}) {
		t.Fatalf("no diagnosis after %d timeouts", stallTrigger)
	}
	path := h.wait(t)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"== iTerm2 stall diagnosis ==", "== recent failures",
		"kind=capture session=w0t0p0:ABC elapsed=6s reason=killed_at_limit limit=6s",
		"kind=list session=- elapsed=0s reason=apple_event_error code=-1712"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the diagnosis lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(h.log(), "iterm: stall diagnosis written: "+path) {
		t.Errorf("no log line names the file:\n%s", h.log())
	}
}

func TestTimeoutsSpreadPastTheWindowStartNoDiagnosis(t *testing.T) {
	h := newStallHarness(t, nil, DefaultStallLimits())
	// One timeout every 40 s: never five inside two minutes.
	for n := 0; n < 3*stallTrigger; n++ {
		if h.timeout(stallT0.Add(time.Duration(n) * 40 * time.Second)) {
			t.Fatalf("a diagnosis started at timeout %d, 40 s apart", n+1)
		}
	}
}

func TestAStallDiagnosisWaitsOutItsCooldown(t *testing.T) {
	h := newStallHarness(t, nil, DefaultStallLimits())
	at := stallT0
	burst := func() bool {
		started := false
		for n := 0; n < stallTrigger; n++ {
			at = at.Add(time.Second)
			started = h.timeout(at) || started
		}
		return started
	}
	if !burst() {
		t.Fatal("the first burst started nothing")
	}
	h.wait(t)
	at = stallT0.Add(stallCooldownLimit - time.Minute)
	if burst() {
		t.Fatal("a second diagnosis inside the cooldown")
	}
	at = stallT0.Add(stallCooldownLimit + time.Minute)
	if !burst() {
		t.Fatal("no diagnosis once the cooldown had passed")
	}
	h.wait(t)
	if got := len(h.files(t)); got != 2 {
		t.Fatalf("%d diagnoses written, want 2", got)
	}
}

func TestOnlyTheNewestStallDiagnosesAreKept(t *testing.T) {
	limits := DefaultStallLimits()
	limits.Diagnoses = 3
	h := newStallHarness(t, nil, limits)
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"iterm-stall-20260901T000000.000000000Z.txt",
		"iterm-stall-20260902T000000.000000000Z.txt", "iterm-stall-20260903T000000.000000000Z.txt",
		"iterm-stall-20260904T000000.000000000Z.txt", "daemon.log"} {
		if err := os.WriteFile(filepath.Join(h.dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for n := 0; n < stallTrigger; n++ {
		h.timeout(stallT0.Add(time.Duration(n) * time.Second))
	}
	path := h.wait(t)
	got := h.files(t)
	want := []string{"iterm-stall-20260903T000000.000000000Z.txt",
		"iterm-stall-20260904T000000.000000000Z.txt", filepath.Base(path)}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kept %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "daemon.log")); err != nil {
		t.Fatalf("a file that is not a diagnosis was removed: %v", err)
	}
}

// A probe or a sample that hangs is the very thing a stall makes likely. It
// is cut off at its own deadline and written down, and the steps after it
// still run — including one that ignores its context altogether.
func TestAHungStallStepTimesOutAndIsRecorded(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	steps := []stallStep{
		{Title: "probe", Timeout: 50 * time.Millisecond, Run: func(ctx context.Context) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
		{Title: "sample", Run: func(ctx context.Context) ([]byte, error) {
			<-release // ignores its context
			return []byte("late"), nil
		}},
		{Title: "load average", Run: func(ctx context.Context) ([]byte, error) {
			return []byte("partial"), errors.New("sysctl said no")
		}},
		{Title: "frontmost application", Run: func(ctx context.Context) ([]byte, error) {
			return []byte(`"CFBundleIdentifier"="com.example.app"`), nil
		}},
	}
	limits := DefaultStallLimits()
	limits.Step = 200 * time.Millisecond
	h := newStallHarness(t, steps, limits)
	started := time.Now()
	for n := 0; n < stallTrigger; n++ {
		h.timeout(stallT0.Add(time.Duration(n) * time.Second))
	}
	if waited := time.Since(started); waited > 100*time.Millisecond {
		t.Fatalf("recording the failures waited %s on the diagnosis", waited)
	}
	// While it runs, another burst starts nothing.
	for n := 0; n < stallTrigger; n++ {
		if h.timeout(stallT0.Add(stallCooldownLimit + time.Hour + time.Duration(n)*time.Second)) {
			t.Fatal("a second diagnosis started while one was running")
		}
	}
	path := h.wait(t)
	if took := time.Since(started); took > 3*time.Second {
		t.Fatalf("the diagnosis took %s; its steps are bounded at %s", took, limits.Step)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"== probe ==", "status: timed out after 50ms",
		"== sample ==", "status: timed out after 200ms",
		"== load average ==", "status: failed: sysctl said no", "partial",
		"== frontmost application ==", "status: ok", "com.example.app",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the diagnosis lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "late") {
		t.Errorf("an abandoned step's late answer was written:\n%s", text)
	}
}

func TestAStallSectionIsCutAtItsLimitAndSaysSo(t *testing.T) {
	steps := []stallStep{{Title: "sample", Run: func(ctx context.Context) ([]byte, error) {
		return []byte(strings.Repeat("a", 100)), nil
	}}}
	limits := DefaultStallLimits()
	limits.SectionBytes = 10
	h := newStallHarness(t, steps, limits)
	for n := 0; n < stallTrigger; n++ {
		h.timeout(stallT0.Add(time.Duration(n) * time.Second))
	}
	body, err := os.ReadFile(h.wait(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), strings.Repeat("a", 11)) ||
		!strings.Contains(string(body), "[truncated: 90 bytes not written]") {
		t.Fatalf("the section was not cut at 10 bytes:\n%s", body)
	}
}

func TestEachFailedRunIsOneLogLineNamingWhatWhoHowLongAndWhy(t *testing.T) {
	h := newStallHarness(t, nil, DefaultStallLimits())
	h.w.record(scriptFailure{At: stallT0, Kind: "send", Session: "w1t2p0:XYZ", Elapsed: 1234 * time.Millisecond,
		Reason: reasonAppleEvent, Code: errAETimeout, Said: "execution error: AppleEvent timed out. (-1712)"})
	h.w.record(scriptFailure{At: stallT0, Kind: "open", Elapsed: 20 * time.Second, Limit: 20 * time.Second,
		Reason: reasonKilledAtDeadline})
	h.w.record(scriptFailure{At: stallT0, Kind: "test", Elapsed: time.Second, Reason: reasonOther, Err: "exit status 1"})
	want := []string{
		`iterm: osascript failed: kind=send session=w1t2p0:XYZ elapsed=1.234s reason=apple_event_error code=-1712 said="execution error: AppleEvent timed out. (-1712)"`,
		`iterm: osascript failed: kind=open session=- elapsed=20s reason=killed_at_limit limit=20s`,
		`iterm: osascript failed: kind=test session=- elapsed=1s reason=other err="exit status 1"`,
	}
	if got := h.log(); got != strings.Join(want, "\n") {
		t.Fatalf("log:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestAFailedRunIsClassifiedByWhatStoppedIt(t *testing.T) {
	killed := &exec.ExitError{}
	for _, c := range []struct {
		name   string
		ctxErr error
		stderr string
		reason string
		code   int
	}{
		{"its own deadline", context.DeadlineExceeded, "", reasonKilledAtDeadline, 0},
		{"the caller left", context.Canceled, "", reasonCancelled, 0},
		{"an Apple Event timeout", nil, "execution error: Error: AppleEvent timed out. (-1712)", reasonAppleEvent, -1712},
		{"automation refused", nil, "execution error: Not authorized to send Apple events to iTerm2. (-1743)", reasonAppleEvent, -1743},
		{"a script that threw", nil, "execution error: Error: internal-detail", reasonOther, 0},
	} {
		reason, code := classifyScriptFailure(c.ctxErr, c.stderr, killed)
		if reason != c.reason || code != c.code {
			t.Errorf("%s: %s %d, want %s %d", c.name, reason, code, c.reason, c.code)
		}
	}
}

// A step may write into the logs directory before the diagnosis does — sample
// is told to put its report there — so the directory exists before any step
// runs. The first real run on a fresh directory found it did not.
func TestTheLogsDirectoryExistsBeforeAnyStepRuns(t *testing.T) {
	var h *stallHarness
	steps := []stallStep{{Title: "sample", Run: func(ctx context.Context) ([]byte, error) {
		if _, err := os.Stat(h.dir); err != nil {
			return nil, err
		}
		return []byte("the directory was there"), nil
	}}}
	h = newStallHarness(t, steps, DefaultStallLimits())
	for n := 0; n < stallTrigger; n++ {
		h.timeout(stallT0.Add(time.Duration(n) * time.Second))
	}
	body, err := os.ReadFile(h.wait(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "the directory was there") {
		t.Fatalf("a step ran before the logs directory existed:\n%s", body)
	}
}

// A logs directory that cannot be read is not a directory with no diagnoses.
func TestAnUnreadableStallDirectoryIsNotZeroDiagnoses(t *testing.T) {
	h := newStallHarness(t, nil, DefaultStallLimits())
	if _, n, err := h.w.reading(); err != nil || n != 0 {
		t.Fatalf("a directory not made yet: %d %v, want 0 and no error", n, err)
	}
	if err := os.WriteFile(h.dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.w.reading(); err == nil {
		t.Fatal("an unreadable directory read as a count")
	}
}
