package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/machineusage"
)

const heavyID = "a5000000-0000-4000-8000-000000000001"

// roomy is a machine with memory to spare.
func roomy() (machineusage.Sample, error) {
	return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 3 << 30}, nil
}

// heavyHarness is runHeavy's world: a fake clock that moves only when the
// command sleeps, and a command that records how it was run.
type heavyHarness struct {
	mu      sync.Mutex
	clock   time.Time
	slept   []time.Duration
	ran     [][]string
	env     [][]string
	stderr  bytes.Buffer
	exit    int
	runErr  error
	during  func()
	samples []func() (machineusage.Sample, error)
}

func (h *heavyHarness) deps(b *broker, env map[string]string) heavyDeps {
	h.clock = time.Unix(1_790_000_000, 0)
	return heavyDeps{
		broker: b,
		sample: func() (machineusage.Sample, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.samples) == 0 {
				return roomy()
			}
			next := h.samples[0]
			if len(h.samples) > 1 {
				h.samples = h.samples[1:]
			}
			return next()
		},
		sleep: func(d time.Duration) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.slept = append(h.slept, d)
			h.clock = h.clock.Add(d)
		},
		now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		},
		run: func(argv, env []string) (int, error) {
			h.mu.Lock()
			h.ran = append(h.ran, argv)
			h.env = append(h.env, env)
			during := h.during
			h.mu.Unlock()
			if during != nil {
				during()
			}
			return h.exit, h.runErr
		},
		getenv:    envOf(env),
		stderr:    &h.stderr,
		renew:     10 * time.Millisecond,
		poll:      5 * time.Second,
		pid:       os.Getpid(),
		requestID: heavyID,
	}
}

func paths(s *standIn) []string {
	var out []string
	for _, r := range s.requests() {
		out = append(out, r.EscapedPath)
	}
	return out
}

// A second build waits in line for the slot, runs once it is granted, keeps
// the slot alive while it runs, gives it back, and leaves with the command's
// own exit status. The command knows it holds the slot, so a heavy inside it
// does not queue behind itself.
func TestAHeavyCommandWaitsForTheSlotThenRunsAndGivesItBack(t *testing.T) {
	asks := 0
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/v1/orchestrator/leases":
			asks++
			if asks == 1 {
				return 200, `{"ok":true,"state":"queued","position":1,"retry_after_seconds":5,"lease":{"resource":"heavy_compile","key":"machine","holder":{"holder":"other: go test ./...","lease_id":"l1","acquired_at":1,"held_seconds":40,"liveness":"alive"},"queue":[]}}`
			}
			return 200, `{"ok":true,"state":"granted","lease_id":"l2","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		case "/v1/orchestrator/leases/renew", "/v1/orchestrator/leases/release":
			return 200, `{"ok":true,"state":"renewed","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		}
		return 404, `{"error":"not_found"}`
	})
	h := &heavyHarness{exit: 3, during: func() { time.Sleep(60 * time.Millisecond) }}
	code := runHeavy(heavyOptions{wait: time.Hour}, []string{"go", "test", "./..."},
		h.deps(b, map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))

	if code != 3 {
		t.Fatalf("exit %d, want the command's 3; stderr %s", code, h.stderr.String())
	}
	if len(h.ran) != 1 || strings.Join(h.ran[0], " ") != "go test ./..." {
		t.Fatalf("ran %v", h.ran)
	}
	if got := h.env[0]; len(got) != 1 || got[0] != heavyEnv+"="+heavyID {
		t.Fatalf("the command's environment %v", got)
	}
	p := paths(s)
	if len(p) < 4 || p[0] != "/v1/orchestrator/leases" || p[1] != "/v1/orchestrator/leases" ||
		p[2] != "/v1/orchestrator/leases/renew" || p[len(p)-1] != "/v1/orchestrator/leases/release" {
		t.Fatalf("asked %v", p)
	}
	var first map[string]any
	if err := json.Unmarshal(s.requests()[0].Body, &first); err != nil {
		t.Fatal(err)
	}
	if first["resource"] != "heavy_compile" || first["request_id"] != heavyID || first["session_id"] != thinConversation ||
		first["pid"] != float64(os.Getpid()) || !strings.HasSuffix(first["holder"].(string), ": go test ./...") {
		t.Fatalf("the ask %v", first)
	}
	if s.requests()[0].Token != thinToken {
		t.Fatal("the ask did not carry the orchestrator token")
	}
	if len(h.slept) != 1 || h.slept[0] != 5*time.Second {
		t.Fatalf("slept %v, wanted one retry_after", h.slept)
	}
	if !strings.Contains(h.stderr.String(), "number 1 in line, held by other: go test ./...") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// A daemon that does not answer is no reason not to build: the command runs,
// and says it ran without the slot.
func TestAHeavyCommandRunsWhenTheDaemonDoesNotAnswer(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, "" })
	b.base = "http://127.0.0.1:1" // nothing listens here
	b.client.Timeout = 2 * time.Second
	h := &heavyHarness{}
	if code := runHeavy(heavyOptions{wait: time.Hour}, []string{"make"}, h.deps(b, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if !strings.Contains(h.stderr.String(), "running without the compile slot") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// A refusal this command does not know runs the command too, and names the
// refusal; nothing is released that was never held.
func TestAnUnknownRefusalRunsWithoutTheSlot(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 400, `{"error":{"code":"bad_lease","message":"no"}}`
	})
	h := &heavyHarness{}
	if code := runHeavy(heavyOptions{wait: time.Hour}, []string{"make"}, h.deps(b, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if p := paths(s); len(p) != 1 {
		t.Fatalf("asked %v", p)
	}
	if !strings.Contains(h.stderr.String(), "bad_lease") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// A line that does not move within --max-wait is left, politely, and the
// command runs beside the holder.
func TestALineLongerThanMaxWaitIsLeftAndTheCommandRuns(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			return 200, `{"ok":true,"state":"queued","position":2,"retry_after_seconds":5,"lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		}
		return 200, `{"ok":true,"state":"cancelled","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
	})
	h := &heavyHarness{}
	if code := runHeavy(heavyOptions{wait: 12 * time.Second}, []string{"make"}, h.deps(b, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	p := paths(s)
	if p[len(p)-1] != "/v1/orchestrator/leases/cancel" {
		t.Fatalf("asked %v", p)
	}
	for _, path := range p {
		if strings.HasSuffix(path, "/release") || strings.HasSuffix(path, "/renew") {
			t.Fatalf("a slot never held was renewed or released: %v", p)
		}
	}
	if !strings.Contains(h.stderr.String(), "running anyway") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// Memory short waits, says why once, and starts when it is back.
func TestAHeavyCommandWaitsForMemory(t *testing.T) {
	h := &heavyHarness{samples: []func() (machineusage.Sample, error){
		func() (machineusage.Sample, error) {
			return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 300 << 20}, nil
		},
		func() (machineusage.Sample, error) {
			return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 3 << 30,
				Pressure: &machineusage.Pressure{MemorySome: 25}}, nil
		},
		roomy,
	}}
	if code := runHeavy(heavyOptions{wait: time.Hour, noSlot: true}, []string{"make"}, h.deps(nil, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if len(h.slept) != 2 {
		t.Fatalf("slept %v", h.slept)
	}
	out := h.stderr.String()
	if !strings.Contains(out, "waiting for memory: 300 MB available, 1024 MB wanted") || !strings.Contains(out, "memory is back") {
		t.Fatalf("stderr %q", out)
	}
}

// Memory that never comes back is waited for until --max-wait, then the
// command runs anyway.
func TestMemoryThatNeverComesBackIsWaitedForOnlyUntilMaxWait(t *testing.T) {
	short := func() (machineusage.Sample, error) {
		return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 100 << 20}, nil
	}
	h := &heavyHarness{samples: []func() (machineusage.Sample, error){short}}
	if code := runHeavy(heavyOptions{wait: time.Minute, noSlot: true}, []string{"make"}, h.deps(nil, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if len(h.slept) != 12 || !strings.Contains(h.stderr.String(), "waited past --max-wait for memory") {
		t.Fatalf("slept %d times; stderr %q", len(h.slept), h.stderr.String())
	}
}

// A platform with no memory reader does not wait for memory.
func TestNoMemoryReaderIsNoWait(t *testing.T) {
	h := &heavyHarness{samples: []func() (machineusage.Sample, error){
		func() (machineusage.Sample, error) { return machineusage.Sample{}, machineusage.ErrUnsupported },
	}}
	if code := runHeavy(heavyOptions{wait: time.Hour, noSlot: true}, []string{"make"}, h.deps(nil, nil)); code != 0 || len(h.slept) != 0 || h.stderr.Len() != 0 {
		t.Fatalf("exit %d slept %v stderr %q", code, h.slept, h.stderr.String())
	}
}

// Inside a heavy command, heavy runs directly: asking for the slot its own
// parent holds would queue behind itself for ever.
func TestAHeavyInsideAHeavyRunsDirectly(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, "{}" })
	h := &heavyHarness{exit: 5}
	code := runHeavy(heavyOptions{wait: time.Hour}, []string{"go", "vet"}, h.deps(b, map[string]string{heavyEnv: "parent-id"}))
	if code != 5 || len(s.requests()) != 0 || len(h.ran) != 1 || h.env[0][0] != heavyEnv+"=parent-id" {
		t.Fatalf("exit %d asked %v ran %v env %v", code, paths(s), h.ran, h.env)
	}
}

// The floor is a quarter of the machine, at most one GiB, unless asked for.
func TestTheMemoryFloor(t *testing.T) {
	for _, c := range []struct {
		total, avail, floor int64
		pressure            float64
		ok                  bool
	}{
		{total: 2 << 30, avail: 600 << 20, ok: true},   // a quarter of 2 GiB is 512 MiB
		{total: 2 << 30, avail: 500 << 20, ok: false},  // below it
		{total: 16 << 30, avail: 1100 << 20, ok: true}, // capped at 1 GiB
		{total: 16 << 30, avail: 1100 << 20, floor: 2 << 30, ok: false},
		{total: 16 << 30, avail: 8 << 30, pressure: 12, ok: false},
		{total: 16 << 30, avail: 8 << 30, pressure: 9, ok: true},
	} {
		s := machineusage.Sample{MemTotal: c.total, MemAvailable: c.avail}
		if c.pressure > 0 {
			s.Pressure = &machineusage.Pressure{MemorySome: c.pressure}
		}
		if ok, why := memoryRoom(s, c.floor); ok != c.ok {
			t.Errorf("%+v: %v (%s)", c, ok, why)
		}
	}
}

func TestSizesAreReadAsAPersonWritesThem(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "1G": 1 << 30, "1500M": 1500 << 20, "800mb": 800 << 20, "1.5GiB": 3 << 29, "4096": 4096} {
		if got, err := parseBytes(in); err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"lots", "-1G"} {
		if _, err := parseBytes(bad); err == nil {
			t.Errorf("%q was read as a size", bad)
		}
	}
}

// The command's own exit status is kept, and one that cannot start is 127.
func TestTheCommandsOwnExitStatusIsKept(t *testing.T) {
	if os.Getenv("CLAWDLINE_HEAVY_HELPER") == "1" {
		os.Exit(3)
	}
	say := func(string, ...any) {}
	code, err := runForeground([]string{os.Args[0], "-test.run=^TestTheCommandsOwnExitStatusIsKept$"}, []string{"CLAWDLINE_HEAVY_HELPER=1"})
	if got := heavyExit(code, err, say); got != 3 {
		t.Fatalf("exit %d (%v), want 3", got, err)
	}
	code, err = runForeground([]string{"/no/such/command-for-heavy"}, nil)
	if got := heavyExit(code, err, say); got != 127 || err == nil {
		t.Fatalf("a missing command: %d %v", got, err)
	}
	if got := heavyExit(0, errors.New("x"), say); got != 127 {
		t.Fatalf("an error that is not an exit: %d", got)
	}
}

// The broker counts a label's bytes. A long command line, or one in another
// script, is cut to fit on a character boundary.
func TestALongCommandLineIsCutToTheBrokersBytes(t *testing.T) {
	long := strings.Repeat("go test -run TestSomething ./internal/... ", 20)
	if got := heavyHolder([]string{"sh", "-c", long}); len(got) > 200 || !strings.HasSuffix(got, "…") {
		t.Fatalf("holder is %d bytes: %q", len(got), got)
	}
	wide := strings.Repeat("編譯", 200)
	got := heavyReason(wide, nil)
	if len(got) > 500 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("reason is %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if got := clipBytes("short\nline", 200); got != "short line" {
		t.Fatalf("%q", got)
	}
}
