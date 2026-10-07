package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
			if asks <= 2 {
				// The line moves while this one waits: number 2, then 1.
				return 200, `{"ok":true,"state":"queued","position":` + strconv.Itoa(3-asks) + `,"retry_after_seconds":5,"lease":{"resource":"heavy_compile","key":"machine","holder":{"holder":"other: go test ./...","lease_id":"l1","acquired_at":1,"held_seconds":40,"liveness":"alive"},"queue":[]}}`
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
	if len(p) < 5 || p[0] != "/v1/orchestrator/leases" || p[1] != "/v1/orchestrator/leases" || p[2] != "/v1/orchestrator/leases" ||
		p[3] != "/v1/orchestrator/leases/renew" || p[len(p)-1] != "/v1/orchestrator/leases/release" {
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
	if len(h.slept) != 2 || h.slept[0] != 5*time.Second {
		t.Fatalf("slept %v, wanted two retry_afters", h.slept)
	}
	// One line when it joins the line and one when the slot is its own: a
	// moving place in line is not news worth waking an agent for.
	lines := stderrLines(h)
	if len(lines) != 2 || !strings.Contains(lines[0], "number 2 in line, held by other: go test ./...") ||
		!strings.Contains(lines[1], "the compile slot is ours after 10s") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

func stderrLines(h *heavyHarness) []string {
	return strings.Split(strings.TrimSuffix(h.stderr.String(), "\n"), "\n")
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
// command is not run: running beside the holder is the overlap heavy exists to
// prevent. The exit status is heavyExitTimedOut, which no command's own
// failure is mistaken for, and the wait said two lines in all.
func TestALineLongerThanMaxWaitIsLeftAndTheCommandIsNotRun(t *testing.T) {
	asks := 0
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			asks++
			if asks == 2 {
				return 429, `{"error":{"code":"queue_full","message":"full"}}`
			}
			return 200, `{"ok":true,"state":"queued","position":` + strconv.Itoa(asks) + `,"retry_after_seconds":5,"lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		}
		return 200, `{"ok":true,"state":"cancelled","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
	})
	h := &heavyHarness{exit: 0}
	if code := runHeavy(heavyOptions{wait: 12 * time.Second}, []string{"make"}, h.deps(b, nil)); code != heavyExitTimedOut || len(h.ran) != 0 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if heavyExitTimedOut != 75 {
		t.Fatalf("the timeout's exit status is %d; callers are told 75", heavyExitTimedOut)
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
	lines := stderrLines(h)
	if len(lines) != 2 || !strings.Contains(lines[0], "waiting for the compile slot (number 1 in line)") ||
		!strings.Contains(lines[1], "the command was not run (exit 75)") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// A queue that is full when heavy first asks is a wait like any other: one
// line, then quiet.
func TestAFullQueueIsOneLineToo(t *testing.T) {
	asks := 0
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			asks++
			switch {
			case asks <= 3:
				return 429, `{"error":{"code":"queue_full","message":"full"}}`
			case asks <= 5:
				return 200, `{"ok":true,"state":"queued","position":` + strconv.Itoa(40-asks) + `,"retry_after_seconds":5,"lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
			}
			return 200, `{"ok":true,"state":"granted","lease_id":"l2","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		}
		return 200, `{"ok":true,"state":"renewed","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
	})
	h := &heavyHarness{}
	if code := runHeavy(heavyOptions{wait: time.Hour}, []string{"make"}, h.deps(b, nil)); code != 0 || len(h.ran) != 1 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	lines := stderrLines(h)
	if len(lines) != 2 || !strings.Contains(lines[0], "its queue is full") || !strings.Contains(lines[1], "the compile slot is ours") {
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
	lines := stderrLines(h)
	if len(lines) != 2 || !strings.Contains(lines[0], "waiting for memory: 300 MB available, 1024 MB wanted") || !strings.Contains(lines[1], "memory is back") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
}

// Memory that never comes back is waited for until --max-wait, quietly, and
// then the command is not run: the same exit status as a slot never had, since
// both share the one deadline.
func TestMemoryThatNeverComesBackIsWaitedForOnlyUntilMaxWait(t *testing.T) {
	short := func() (machineusage.Sample, error) {
		return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 100 << 20}, nil
	}
	h := &heavyHarness{samples: []func() (machineusage.Sample, error){short}}
	if code := runHeavy(heavyOptions{wait: 2 * time.Minute, noSlot: true}, []string{"make"}, h.deps(nil, nil)); code != heavyExitTimedOut || len(h.ran) != 0 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	lines := stderrLines(h)
	if len(h.slept) != 24 || len(lines) != 2 || !strings.Contains(lines[0], "waiting for memory") ||
		!strings.Contains(lines[1], "the command was not run (exit 75)") {
		t.Fatalf("slept %d times; stderr %q", len(h.slept), h.stderr.String())
	}
}

// A slot already granted is given back when memory never comes: nobody else
// waits behind a command that will not run.
func TestASlotIsGivenBackWhenMemoryTimesOut(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			return 200, `{"ok":true,"state":"granted","lease_id":"l2","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
		}
		return 200, `{"ok":true,"state":"released","lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
	})
	h := &heavyHarness{samples: []func() (machineusage.Sample, error){func() (machineusage.Sample, error) {
		return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 100 << 20}, nil
	}}}
	if code := runHeavy(heavyOptions{wait: time.Minute}, []string{"make"}, h.deps(b, nil)); code != heavyExitTimedOut || len(h.ran) != 0 {
		t.Fatalf("exit %d ran %v", code, h.ran)
	}
	if p := paths(s); p[len(p)-1] != "/v1/orchestrator/leases/release" {
		t.Fatalf("asked %v", p)
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
	code := runHeavy(heavyOptions{wait: time.Hour, handoff: true}, []string{"go", "vet"}, h.deps(b, map[string]string{heavyEnv: "parent-id"}))
	if code != 5 || len(s.requests()) != 0 || len(h.ran) != 1 || h.env[0][0] != heavyEnv+"=parent-id" {
		t.Fatalf("exit %d asked %v ran %v env %v", code, paths(s), h.ran, h.env)
	}
}

func TestHeavyHandoff(t *testing.T) {
	const callbackID = "a5000000-0000-4000-8000-000000000002"
	for _, tc := range []struct {
		name, lease, callback string
		env                   map[string]string
		memoryShort           bool
		noSlot                bool
		wantHandoff           bool
		wait                  time.Duration
	}{
		{name: "free slot", lease: "granted", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "busy slot", lease: "queued", callback: "ok", env: map[string]string{"CODEX_THREAD_ID": thinConversation}, wantHandoff: true},
		{name: "short memory", lease: "granted", callback: "ok", env: map[string]string{"CODEX_THREAD_ID": thinConversation}, memoryShort: true, wantHandoff: true},
		{name: "memory without slot", callback: "ok", env: map[string]string{"CODEX_THREAD_ID": thinConversation}, memoryShort: true, noSlot: true, wantHandoff: true},
		{name: "no conversation", lease: "queued"},
		{name: "child", lease: "queued", env: map[string]string{"CODEX_THREAD_ID": thinConversation, "CLAWDLINE_TASK_SECRET": "child"}},
		{name: "callback capacity", lease: "queued", callback: "callback_capacity", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "root unresolved", lease: "queued", callback: "root_unresolved", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "unsupported callback", lease: "queued", callback: "no_callback_capability", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "unreachable callback", lease: "queued", callback: "unreachable", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "other callback refusal", lease: "queued", callback: "bad_task", env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "custom state directory", lease: "queued", env: map[string]string{"CODEX_THREAD_ID": thinConversation, "CLAWDLINE_NEXT_DIR": "/elsewhere"}},
		{name: "daemon unreachable", lease: "none", memoryShort: true, env: map[string]string{"CODEX_THREAD_ID": thinConversation}},
		{name: "callback timeout cap", lease: "queued", callback: "ok", env: map[string]string{"CODEX_THREAD_ID": thinConversation}, wantHandoff: true, wait: 220 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asks, callbacks := 0, 0
			s, b := newStandIn(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/v1/orchestrator/leases":
					asks++
					state := tc.lease
					if asks > 1 {
						state = "granted"
					}
					return 200, `{"ok":true,"state":"` + state + `","position":3,"lease":{"resource":"heavy_compile","key":"machine","holder":null,"queue":[]}}`
				case "/v1/orchestrator/callbacks":
					callbacks++
					if tc.callback != "ok" {
						return 429, `{"error":{"code":"` + tc.callback + `","message":"refused"}}`
					}
					return 200, `{"ok":true,"task":{"id":"` + callbackID + `","state":"briefed"}}`
				default:
					return 200, `{"ok":true}`
				}
			})
			if tc.callback == "unreachable" {
				b.client.Transport = heavyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/v1/orchestrator/callbacks" {
						return nil, errors.New("connection lost")
					}
					return http.DefaultTransport.RoundTrip(r)
				})
			}
			h := &heavyHarness{}
			if tc.memoryShort {
				h.samples = []func() (machineusage.Sample, error){func() (machineusage.Sample, error) {
					return machineusage.Sample{MemTotal: 4 << 30, MemAvailable: 100 << 20}, nil
				}, roomy}
			}
			d := h.deps(b, tc.env)
			if tc.lease == "none" {
				d.broker = nil
			}
			d.executable = "/usr/local/bin/clawdline"
			d.cwd = "/repo"
			wait := tc.wait
			if wait == 0 {
				wait = 40 * time.Minute
			}
			code := runHeavy(heavyOptions{wait: wait, handoff: true, noSlot: tc.noSlot, reason: "checks", port: 7727}, []string{"go", "test", "./..."}, d)
			if tc.wantHandoff {
				if code != 76 || callbacks != 1 || len(h.ran) != 0 || len(h.slept) != 0 || !strings.Contains(h.stderr.String(), callbackID) || !strings.Contains(h.stderr.String(), "End your turn") {
					t.Fatalf("exit %d callbacks %d ran %v slept %v stderr %q", code, callbacks, h.ran, h.slept, h.stderr.String())
				}
				var body map[string]any
				for _, request := range s.requests() {
					if request.EscapedPath == "/v1/orchestrator/callbacks" {
						_ = json.Unmarshal(request.Body, &body)
					}
				}
				wantTimeout := float64(100)
				if tc.wait > 0 {
					wantTimeout = 240
				}
				if body["timeout_minutes"] != wantTimeout || body["root"].(map[string]any)["session_id"] != thinConversation {
					t.Fatalf("callback body %v", body)
				}
				argv := body["argv"].([]any)
				if strings.Join([]string{argv[0].(string), argv[1].(string)}, " ") != "/usr/local/bin/clawdline heavy" || strings.Contains(strings.Join(anyStrings(argv), " "), "--handoff") || argv[len(argv)-3] != "go" {
					t.Fatalf("callback argv %v", argv)
				}
				if tc.name == "busy slot" {
					t.Logf("exit 76: %s", strings.TrimSpace(h.stderr.String()))
				}
				if tc.noSlot {
					for _, path := range paths(s) {
						if path == "/v1/orchestrator/leases" {
							t.Fatal("--no-slot asked for a lease")
						}
					}
				}
			} else if code != 0 || len(h.ran) != 1 || len(h.slept) != map[bool]int{true: 1, false: 0}[tc.lease == "queued" || tc.memoryShort] {
				t.Fatalf("exit %d callbacks %d ran %v slept %v stderr %q", code, callbacks, h.ran, h.slept, h.stderr.String())
			} else {
				wantCallbacks := 0
				if tc.callback != "" && tc.callback != "unreachable" {
					wantCallbacks = 1
				}
				if callbacks != wantCallbacks || ((tc.lease == "queued" || tc.memoryShort) && !strings.Contains(h.stderr.String(), "handoff unavailable")) {
					t.Fatalf("callbacks %d want %d stderr %q", callbacks, wantCallbacks, h.stderr.String())
				}
			}
		})
	}
}

type heavyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f heavyRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func anyStrings(a []any) []string {
	out := make([]string, len(a))
	for i, v := range a {
		out[i], _ = v.(string)
	}
	return out
}

func TestAChildWorktreeCannotHandOff(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".config", "clawdline-next")
	id := "a5000000-0000-4000-8000-000000000003"
	cwd := filepath.Join(state, "worktrees", "group", id)
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(state, "tasks", id, "CHILD.md")
	if err := os.MkdirAll(filepath.Dir(brief), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brief, []byte("child"), 0600); err != nil {
		t.Fatal(err)
	}
	if !heavyChildWorktree(cwd, envOf(map[string]string{"HOME": home})) {
		t.Fatal("the child worktree was not recognised")
	}
	if heavyChildWorktree(filepath.Join(state, "worktrees", "group", "not-a-task"), envOf(map[string]string{"HOME": home})) {
		t.Fatal("an unrelated worktree was recognised as a child")
	}
	asks := 0
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/v1/orchestrator/leases" {
			asks++
			if asks == 1 {
				return 200, `{"ok":true,"state":"queued","position":1}`
			}
			return 200, `{"ok":true,"state":"granted"}`
		}
		if r.URL.Path == "/v1/orchestrator/callbacks" {
			t.Fatal("a child started a callback")
		}
		return 200, `{"ok":true}`
	})
	h := &heavyHarness{}
	d := h.deps(b, map[string]string{"HOME": home, "CODEX_THREAD_ID": thinConversation})
	d.cwd, d.executable = cwd, "/usr/local/bin/clawdline"
	if code := runHeavy(heavyOptions{wait: time.Minute, handoff: true}, []string{"make"}, d); code != 0 || len(h.ran) != 1 || len(h.slept) != 1 {
		t.Fatalf("exit %d ran %v slept %v stderr %q", code, h.ran, h.slept, h.stderr.String())
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
