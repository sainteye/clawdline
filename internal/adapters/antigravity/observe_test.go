package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const conversationA = "11111111-1111-4111-8111-111111111111"
const conversationB = "22222222-2222-4222-8222-222222222222"

type processFixture map[int]ports.AntigravityProcess

type cancelBeforePersistenceReader struct {
	base   processFixture
	cancel context.CancelFunc
	reads  int
}

func (r *cancelBeforePersistenceReader) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	p, err := r.base.ReadAntigravityProcess(ctx, pid)
	r.reads++
	if r.reads == 4 {
		r.cancel()
	}
	return p, err
}

func TestObservationCancellationBeforePersistencePublishesNothing(t *testing.T) {
	f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.b.reader = &cancelBeforePersistenceReader{base: f.procs, cancel: cancel}
	_, err := f.b.Observe(ctx, f.w, "PreInvocation", payload(conversationA, f.pathA, nil))
	wantCode(t, err, "antigravity_observation_cancelled")
	if len(f.b.entries) != 0 {
		t.Fatal("cancelled observation published authority")
	}
	if _, err = os.Lstat(f.state); !os.IsNotExist(err) {
		t.Fatal("cancelled observation wrote durable authority")
	}
}

func (p processFixture) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	if err := ctx.Err(); err != nil {
		return ports.AntigravityProcess{}, err
	}
	v, ok := p[pid]
	if !ok {
		return v, errors.New("synthetic missing process")
	}
	return v, nil
}

type bridgeFixture struct {
	b                              *Bridge
	procs                          processFixture
	home, exe, state, pathA, pathB string
	now                            time.Time
	w                              Witness
	s                              session.Session
}

func fixture(t *testing.T) *bridgeFixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &bridgeFixture{home: home, exe: filepath.Join(home, "agy"), state: filepath.Join(home, "state.json"), now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	if err = os.WriteFile(f.exe, []byte("synthetic standalone executable fingerprint"), 0700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, id := range []string{conversationA, conversationB} {
		path := filepath.Join(home, ".gemini", "antigravity-cli", "brain", id, ".system_generated", "logs", "transcript_full.jsonl")
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		paths[id] = path
	}
	f.pathA, f.pathB = paths[conversationA], paths[conversationB]
	f.procs = processFixture{
		41: {PID: 41, ParentPID: 1, Birth: "birth-a", Executable: f.exe, TTY: "ttyA", TerminalID: "%1"},
		42: {PID: 42, ParentPID: 41, Birth: "helper-a", Executable: "/synthetic/helper", TTY: "ttyA", TerminalID: "%1"},
		51: {PID: 51, ParentPID: 1, Birth: "birth-b", Executable: f.exe, TTY: "ttyB", TerminalID: "%2"},
		52: {PID: 52, ParentPID: 51, Birth: "helper-b", Executable: "/synthetic/helper", TTY: "ttyB", TerminalID: "%2"},
	}
	for pid, p := range f.procs {
		p.BootID = "boot-fixture"
		p.KernelGeneration = fmt.Sprintf("generation-%d", pid)
		f.procs[pid] = p
	}
	f.b, err = NewBridge(home, f.state, f.exe, f.procs, func(_ string, id string) (string, bool) { p, ok := paths[id]; return p, ok })
	if err != nil {
		t.Fatal(err)
	}
	f.b.now = func() time.Time { return f.now }
	f.w = Witness{HelperPID: 42, HelperBirth: "helper-a", HelperBootID: "boot-fixture", HelperGeneration: "generation-42", At: f.now, Sequence: 1}
	f.s = session.Session{PID: 41, ID: "%1", TTY: "ttyA", Backend: session.BackendTmux, Assistant: session.AssistantAntigravity}
	return f
}
func payload(id, path string, extra map[string]any) io.Reader {
	m := map[string]any{"conversationId": id, "transcriptPath": path, "modelName": "gemini-3.8-flash-high"}
	for k, v := range extra {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return strings.NewReader(string(data))
}
func observeA(f *bridgeFixture, event string, extra map[string]any) (ports.AntigravityBinding, error) {
	return f.b.Observe(context.Background(), f.w, event, payload(conversationA, f.pathA, extra))
}
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *BridgeError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestObservationExactIdentityAndResume(t *testing.T) {
	f := fixture(t)
	// Resume's first observation is PreInvocation, without SessionStart.
	a, err := observeA(f, "PreInvocation", map[string]any{"pid": 51, "email": "synthetic@example.invalid", "toolCall": map[string]any{"args": "secret-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	w := Witness{HelperPID: 52, HelperBirth: "helper-b", HelperBootID: "boot-fixture", HelperGeneration: "generation-52", At: f.now, Sequence: 1}
	b, err := f.b.Observe(context.Background(), w, "PreInvocation", payload(conversationB, f.pathB, nil))
	if err != nil {
		t.Fatal(err)
	}
	if a.Process.PID != 41 || b.Process.PID != 51 || a.ConversationID == b.ConversationID {
		t.Fatalf("crossed identities: %+v %+v", a, b)
	}
	got, err := f.b.AntigravityBinding(context.Background(), f.s)
	if err != nil || got.ConversationID != conversationA {
		t.Fatalf("A: %+v %v", got, err)
	}
	other := f.s
	other.PID = 51
	other.TTY = "ttyB"
	other.ID = "%2"
	got, err = f.b.AntigravityBinding(context.Background(), other)
	if err != nil || got.ConversationID != conversationB {
		t.Fatalf("B: %+v %v", got, err)
	}
	data, err := os.ReadFile(f.state)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"email", "example.invalid", "secret-fixture", "toolCall", "workspacePaths"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("persisted non-allowlisted %s", private)
		}
	}
}

func TestObservationRejectsUnprovedFacts(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(*bridgeFixture)
	}{
		{"helper birth", "antigravity_helper_unverified", func(f *bridgeFixture) { f.w.HelperBirth = "forged" }},
		{"helper generation", "antigravity_helper_unverified", func(f *bridgeFixture) { f.w.HelperGeneration = "" }},
		{"helper boot", "antigravity_helper_unverified", func(f *bridgeFixture) { f.w.HelperBootID = "different-boot" }},
		{"missing ancestry", "antigravity_process_unreadable", func(f *bridgeFixture) { delete(f.procs, 41) }},
		{"old IDE", "antigravity_ancestry_unverified", func(f *bridgeFixture) { p := f.procs[41]; p.Executable = "/synthetic/ide/agy"; f.procs[41] = p }},
		{"TTY", "antigravity_terminal_mismatch", func(f *bridgeFixture) { p := f.procs[42]; p.TTY = "ttyB"; f.procs[42] = p }},
		{"pane", "antigravity_terminal_mismatch", func(f *bridgeFixture) { p := f.procs[42]; p.TerminalID = "%2"; f.procs[42] = p }},
		{"stale", "antigravity_observation_stale", func(f *bridgeFixture) { f.w.At = f.now.Add(-MaxObservationAge - time.Nanosecond) }},
		{"future", "antigravity_observation_stale", func(f *bridgeFixture) { f.w.At = f.now.Add(time.Nanosecond) }},
		{"zero sequence", "antigravity_observation_stale", func(f *bridgeFixture) { f.w.Sequence = 0 }},
		{"ambiguous ancestry", "antigravity_ancestry_ambiguous", func(f *bridgeFixture) { p := f.procs[41]; p.ParentPID = 51; f.procs[41] = p }},
		{"replaced executable", "antigravity_ancestry_unverified", func(f *bridgeFixture) {
			if err := os.WriteFile(f.exe, []byte("changed binary"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"path symlink", "antigravity_transcript_unverified", func(f *bridgeFixture) {
			if err := os.Remove(f.pathA); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(f.pathB, f.pathA); err != nil {
				t.Fatal(err)
			}
		}},
		{"resolver escape", "antigravity_transcript_unverified", func(f *bridgeFixture) {
			f.pathA = f.pathB
			f.b.resolve = func(string, string) (string, bool) { return f.pathB, true }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture(t)
			tc.mutate(f)
			_, err := observeA(f, "PreInvocation", nil)
			wantCode(t, err, tc.code)
			if len(f.b.entries) != 0 {
				t.Fatal("rejected observation became authoritative")
			}
		})
	}
}

func TestObservationReplayReuseSwitchAndRestart(t *testing.T) {
	f := fixture(t)
	if _, err := observeA(f, "PreInvocation", nil); err != nil {
		t.Fatal(err)
	}
	f.w.Sequence = 2
	_, err := observeA(f, "PreInvocation", nil)
	wantCode(t, err, "antigravity_observation_replayed")
	f.w.Sequence = 1
	_, err = observeA(f, "PostInvocation", nil)
	wantCode(t, err, "antigravity_observation_replayed")
	f.w.Sequence = 2
	if _, err = f.b.Observe(context.Background(), f.w, "PreInvocation", payload(conversationB, f.pathB, nil)); err != nil {
		t.Fatal(err)
	}
	s := f.s
	s.ConversationID = conversationA
	_, err = f.b.AntigravityBinding(context.Background(), s)
	wantCode(t, err, "antigravity_binding_unverified")
	restarted, err := NewBridge(f.home, f.state, f.exe, f.procs, f.b.resolve)
	if err != nil {
		t.Fatal(err)
	}
	_, err = restarted.AntigravityBinding(context.Background(), f.s)
	wantCode(t, err, "antigravity_binding_unverified")
	p := f.procs[41]
	p.Birth = "reused-pid"
	f.procs[41] = p
	_, err = f.b.AntigravityBinding(context.Background(), f.s)
	wantCode(t, err, "antigravity_binding_unverified")
}

func TestObservationCountersAllowNewInvocationsAndFenceOldEvents(t *testing.T) {
	f := fixture(t)
	if _, err := observeA(f, "PreInvocation", map[string]any{"invocationNum": 0}); err != nil {
		t.Fatal(err)
	}
	f.w.Sequence++
	if _, err := observeA(f, "PostInvocation", map[string]any{"invocationNum": 0}); err != nil {
		t.Fatal(err)
	}
	f.w.Sequence++
	_, err := observeA(f, "PreInvocation", map[string]any{"invocationNum": 0})
	wantCode(t, err, "antigravity_observation_replayed")
	if _, err = observeA(f, "PreInvocation", map[string]any{"invocationNum": 1}); err != nil {
		t.Fatal(err)
	}
}

func TestExecutableFingerprintChecksKernelChangeStamp(t *testing.T) {
	f := fixture(t)
	info, err := os.Stat(f.exe)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.exe)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	if err = os.WriteFile(f.exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(f.exe, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = f.b.VerifyAntigravityProcess(context.Background(), 41)
	wantCode(t, err, "antigravity_process_unverified")
}

func TestObservationStateFreshnessAndStop(t *testing.T) {
	f := fixture(t)
	f.w.At = f.now.Add(-MaxObservationAge)
	if _, err := observeA(f, "PreInvocation", nil); err != nil {
		t.Fatal(err)
	}
	f.w.At = f.now
	f.w.Sequence++
	got, err := observeA(f, "PostInvocation", nil)
	if err != nil || got.State == session.StateIdle {
		t.Fatalf("model completion became idle: %+v %v", got, err)
	}
	f.w.Sequence++
	got, err = observeA(f, "Stop", map[string]any{"fullyIdle": false})
	if err != nil || got.State != session.StateUnknown {
		t.Fatalf("background stop: %+v %v", got, err)
	}
	f.w.Sequence++
	got, err = observeA(f, "Stop", map[string]any{"fullyIdle": true, "error": "synthetic failed turn"})
	if err != nil || got.State != session.StateUnknown {
		t.Fatalf("error stop: %+v %v", got, err)
	}
	f.w.Sequence++
	got, err = observeA(f, "Stop", map[string]any{"fullyIdle": true})
	if err != nil || got.State != session.StateIdle {
		t.Fatalf("idle stop: %+v %v", got, err)
	}
	f.now = f.now.Add(MaxObservationAge + time.Nanosecond)
	got, err = f.b.AntigravityBinding(context.Background(), f.s)
	if err != nil || got.StateFresh || got.State != session.StateUnknown || got.ConversationID != conversationA {
		t.Fatalf("expired state lost identity or authority: %+v %v", got, err)
	}
}

func TestObservationLaunchCapabilityAndDiskFailure(t *testing.T) {
	f := fixture(t)
	capPath := filepath.Join(f.home, "capability")
	if err := os.WriteFile(capPath, []byte("synthetic-capability"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileDigest(capPath)
	if err != nil {
		t.Fatal(err)
	}
	c := LaunchCommitment{ID: "launch-a", CapabilityPath: capPath, CapabilityDigest: digest, RequestedConversationID: conversationA, Process: f.procs[41]}
	if err = f.b.CommitLaunch(c); err != nil {
		t.Fatal(err)
	}
	if err = f.b.CommitLaunch(c); err != nil {
		t.Fatal(err)
	}
	if _, err = f.b.AntigravityBinding(context.Background(), f.s); err == nil {
		t.Fatal("provisional launch became confirmed")
	}
	_, err = observeA(f, "PreInvocation", nil)
	wantCode(t, err, "antigravity_launch_unverified")
	f.w.LaunchID = c.ID
	f.w.Capability = "forged"
	_, err = observeA(f, "PreInvocation", nil)
	wantCode(t, err, "antigravity_capability_invalid")
	f.w.Capability = "synthetic-capability"
	if _, err = observeA(f, "PreInvocation", nil); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(capPath); err != nil {
		t.Fatal(err)
	}
	_, err = f.b.AntigravityBinding(context.Background(), f.s)
	wantCode(t, err, "antigravity_launch_unverified")
	f = fixture(t)
	f.b.statePath = filepath.Join(f.home, "missing", "state.json")
	_, err = observeA(f, "PreInvocation", nil)
	wantCode(t, err, "antigravity_store_write_failed")
	if len(f.b.entries) != 0 {
		t.Fatal("failed write published state")
	}
}

func TestObservationBoundsAndCancellation(t *testing.T) {
	f := fixture(t)
	_, err := DecodeObservation(strings.NewReader(strings.Repeat(" ", MaxHookPayloadBytes+1)))
	wantCode(t, err, "antigravity_payload_too_large")
	for _, bad := range []string{`null`, `[]`, `{} {}`, `{"conversationId":"../bad"}`, `{"conversationId":"x","conversationId":"y"}`} {
		_, err = DecodeObservation(strings.NewReader(bad))
		wantCode(t, err, "antigravity_payload_invalid")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.b.Observe(ctx, f.w, "PreInvocation", payload(conversationA, f.pathA, nil))
	wantCode(t, err, "antigravity_helper_unverified")
	for i := 0; i < MaxLiveBindings; i++ {
		f.b.entries[fmt.Sprint(i)] = retained{Binding: ports.AntigravityBinding{ObservedAt: f.now}}
	}
	_, err = observeA(f, "PreInvocation", nil)
	wantCode(t, err, "antigravity_bindings_full")
	for key, v := range f.b.entries {
		v.Binding.ObservedAt = f.now.Add(-MaxObservationRetention - time.Nanosecond)
		f.b.entries[key] = v
	}
	if _, err = observeA(f, "PreInvocation", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.b.entries) != 1 {
		t.Fatal("expired entries not reclaimed")
	}
}

func TestObservationResponseNeverGrantsPermission(t *testing.T) {
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", "Stop"} {
		response, err := ObserverResponse(event)
		if err != nil {
			t.Fatal(err)
		}
		want := "{}\n"
		if event == "PreToolUse" {
			want = "{\"decision\":\"ask\"}\n"
		}
		if string(response) != want {
			t.Fatalf("%s: %s", event, response)
		}
	}
	_, err := ObserverResponse("PermissionApproved")
	wantCode(t, err, "antigravity_event_unsupported")
}

func TestObservationConcurrentReaders(t *testing.T) {
	f := fixture(t)
	if _, err := observeA(f, "PreInvocation", nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 5; n++ {
				if _, err := f.b.AntigravityBinding(context.Background(), f.s); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

type countingProcessReader struct {
	base  processFixture
	reads int
}

func (r *countingProcessReader) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	r.reads++
	return r.base.ReadAntigravityProcess(ctx, pid)
}

func TestObservationWarmInventory(t *testing.T) {
	for _, count := range []int{0, 1, 20} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := fixture(t)
			paths := map[string]string{}
			f.b.resolve = func(_ string, id string) (string, bool) { p, ok := paths[id]; return p, ok }
			var rows []session.Session
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("33333333-3333-4333-8333-%012d", i)
				path := filepath.Join(f.home, ".gemini", "antigravity-cli", "brain", id, ".system_generated", "logs", "transcript_full.jsonl")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
				paths[id] = path
				pid := 101 + i*2
				tty, pane := fmt.Sprintf("tty%d", i), fmt.Sprintf("%%%d", i+100)
				f.procs[pid] = ports.AntigravityProcess{BootID: "boot-fixture", KernelGeneration: fmt.Sprintf("generation-%d", pid), PID: pid, ParentPID: 1, Birth: fmt.Sprintf("birth-%d", i), Executable: f.exe, TTY: tty, TerminalID: pane}
				f.procs[pid+1] = ports.AntigravityProcess{BootID: "boot-fixture", KernelGeneration: fmt.Sprintf("generation-%d", pid+1), PID: pid + 1, ParentPID: pid, Birth: fmt.Sprintf("helper-%d", i), Executable: "/synthetic/helper", TTY: tty, TerminalID: pane}
				w := Witness{HelperPID: pid + 1, HelperBirth: f.procs[pid+1].Birth, HelperBootID: f.procs[pid+1].BootID, HelperGeneration: f.procs[pid+1].KernelGeneration, At: f.now, Sequence: 1}
				if _, err := f.b.Observe(context.Background(), w, "PreInvocation", payload(id, path, nil)); err != nil {
					t.Fatal(err)
				}
				rows = append(rows, session.Session{PID: pid, ID: pane, TTY: tty, Backend: session.BackendTmux, Assistant: session.AssistantAntigravity})
				// Identity polling must not parse an unchanged conversation body.
				if err := os.WriteFile(path, []byte("synthetic unparseable body"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			reader := &countingProcessReader{base: f.procs}
			f.b.reader = reader
			start := time.Now()
			for repeat := 0; repeat < 100; repeat++ {
				for _, s := range rows {
					if _, err := f.b.AntigravityBinding(context.Background(), s); err != nil {
						t.Fatal(err)
					}
				}
			}
			if reader.reads != count*100 {
				t.Fatalf("unexpected process work: %d", reader.reads)
			}
			t.Logf("rows=%d polls=100 process_reads=%d elapsed=%s", count, reader.reads, time.Since(start))
		})
	}
}

func TestObservationRejectsChangedBootAndExecGeneration(t *testing.T) {
	for _, field := range []string{"boot", "generation"} {
		t.Run(field, func(t *testing.T) {
			f := fixture(t)
			if _, err := observeA(f, "PreInvocation", nil); err != nil {
				t.Fatal(err)
			}
			p := f.procs[41]
			if field == "boot" {
				p.BootID = "different-boot"
			} else {
				p.KernelGeneration = "different-exec"
			}
			f.procs[41] = p
			if _, err := f.b.AntigravityBinding(context.Background(), f.s); err == nil {
				t.Fatal("old observation survived a changed kernel generation")
			}
		})
	}
}

func TestPreparedTmuxBindingSeparatesAbsentFromUnknown(t *testing.T) {
	f := fixture(t)
	native := f.procs[41]
	native.Birth = "2026-10-01T12:00:00Z"
	f.procs[41] = native
	got, found, err := f.b.PreparedTmuxBinding(context.Background(), native)
	if err != nil || found || got.Verified || len(f.b.entries) != 0 {
		t.Fatal("absence manufactured a native identity", got, found, err)
	}
	binding, err := observeA(f, "PreInvocation", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err = f.b.PreparedTmuxBinding(context.Background(), native)
	if err != nil || !found || got != binding {
		t.Fatal("positive native binding lost", got, found, err)
	}
	reopened, err := NewBridge(f.home, f.state, f.exe, f.procs, f.b.resolve)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err = reopened.PreparedTmuxBinding(context.Background(), native); err == nil || !found {
		t.Fatal("persisted unverified entry became absence/live authority", found, err)
	}
	if err := os.Remove(f.pathA); err != nil {
		t.Fatal(err)
	}
	if _, found, err = f.b.PreparedTmuxBinding(context.Background(), native); err == nil || !found {
		t.Fatal("missing transcript became absence", found, err)
	}
	changed := native
	changed.ParentPID++
	f.procs[41] = changed
	if _, _, err = f.b.PreparedTmuxBinding(context.Background(), native); err == nil {
		t.Fatal("changed native parent accepted")
	}
}
