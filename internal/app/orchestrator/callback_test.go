//go:build darwin || linux

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A callback is a command the daemon runs for a root and reports on when it
// exits. These run real processes: a callback that only pretends to run
// proves nothing about the process group it has to stop.

func callbackRequest(t *testing.T, id string, argv ...string) CallbackRequest {
	t.Helper()
	return CallbackRequest{
		TaskID: id, Title: "The wait is over", Argv: argv, Dir: t.TempDir(), TimeoutMinutes: 5,
		Root: RootRef{SessionID: rootConversation, Assistant: "claude"},
	}
}

// settledWithin waits for the callback to end on its own.
func settledWithin(t *testing.T, b *Broker, ctx context.Context, id string, d time.Duration) Record {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		r, _, err := b.Record(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.State.Terminal() {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("callback %s is still %s after %s", id, r.State, d)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// alive reports whether a process group still has a member.
func alive(pgid int) bool { return syscall.Kill(-pgid, 0) == nil }

func TestACallbackSettlesOnItsExitAndOwesItsRootANotice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		argv  []string
		state State
		head  string
		last  string
	}{
		{"success", []string{"sh", "-c", "echo deployed"}, StateSuccess, "exit 0 after", "deployed"},
		{"failure", []string{"sh", "-c", "echo first; echo the stamp never matched; exit 3"}, StateFailure, "exit 3 after", "the stamp never matched"},
		{"no such command", []string{"/nonexistent/clawdline-callback-test"}, StateFailure, "exit 127 after", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			id := "c0000000-0000-4000-8000-000000000001"
			out, err := b.StartCallback(ctx, callbackRequest(t, id, tc.argv...))
			if err != nil {
				t.Fatal(err)
			}
			// The answer is the task as it stands once the start has run: a
			// command that ends before the answer is read back — dash reports
			// a missing command in about a millisecond — is already settled,
			// and `clawdline callback` prints that ending instead of "end
			// your turn".
			if (out.Record.State != StateBriefed && out.Record.State != tc.state) || out.Record.Callback == nil ||
				out.Record.Kind != TaskKindCallback || out.Record.Callback.Intent != CallbackIntentWait {
				t.Fatalf("started as %+v", out.Record)
			}
			r := settledWithin(t, b, ctx, id, 10*time.Second)
			if r.State != tc.state {
				t.Fatalf("state %s, want %s: %s", r.State, tc.state, r.Verdict)
			}
			if !strings.HasPrefix(r.Verdict, tc.head) || !strings.Contains(r.Verdict, tc.last) {
				t.Fatalf("verdict %q does not start %q and carry %q", r.Verdict, tc.head, tc.last)
			}
			if r.Notice == nil {
				t.Fatal("a settled callback owes its root a notice and has none")
			}
			if got := b.FinishedLine(r, r.Notice.ID); !strings.HasPrefix(got, "callback "+id[:8]+" finished: "+string(tc.state)) {
				t.Fatalf("notice line %q", got)
			}
			if changed, err := b.Acknowledge(ctx, id, r.Notice.ID); err != nil || !changed {
				t.Fatalf("first result read was changed=%v, err=%v", changed, err)
			}
			if changed, err := b.Acknowledge(ctx, id, r.Notice.ID); err != nil || changed {
				t.Fatalf("repeated result read was changed=%v, err=%v", changed, err)
			}
			if r.Callback.Exit == nil {
				t.Fatal("the exit status was not recorded")
			}
		})
	}
}

func TestTheSameCallbackStartedTwiceRunsOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000002"
	req := callbackRequest(t, id, "sh", "-c", "echo once >> ran")
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	again, err := b.StartCallback(ctx, req)
	if err != nil || !again.Replayed {
		t.Fatalf("the retry was %+v, %v; want a replay", again, err)
	}
	req.Intent = CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); refusalCode(err) != "intent_conflict" {
		t.Fatalf("a changed intent was replayed: %v", err)
	}
	settledWithin(t, b, ctx, id, 10*time.Second)
	raw, _ := os.ReadFile(filepath.Join(req.Dir, "ran"))
	if string(raw) != "once\n" {
		t.Fatalf("the command ran %q", raw)
	}
}

func TestACallbackIsRefusedBeforeAnythingStarts(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000003"
	for _, tc := range []struct {
		name string
		edit func(*CallbackRequest)
		code string
	}{
		{"no command", func(r *CallbackRequest) { r.Argv = nil }, "bad_task"},
		{"too many words", func(r *CallbackRequest) { r.Argv = make([]string, MaxCallbackArgs+1); r.Argv[0] = "true" }, "bad_task"},
		{"too long", func(r *CallbackRequest) { r.Argv = []string{"echo", strings.Repeat("x", CallbackCommandLimit)} }, "bad_task"},
		{"a credential in env", func(r *CallbackRequest) { r.Env = map[string]string{"GITHUB_TOKEN": "x"} }, "bad_task"},
		{"relative dir", func(r *CallbackRequest) { r.Dir = "here" }, "bad_task"},
		{"long timeout", func(r *CallbackRequest) { r.TimeoutMinutes = 241 }, "bad_task"},
		{"no root", func(r *CallbackRequest) { r.Root.SessionID = "" }, "root_session_required"},
		{"unknown intent", func(r *CallbackRequest) { r.Intent = "something-else" }, "bad_task"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := callbackRequest(t, id, "touch", "ran")
			tc.edit(&req)
			_, err := b.StartCallback(ctx, req)
			if got := refusalCode(err); got != tc.code {
				t.Fatalf("refusal %q (%v), want %q", got, err, tc.code)
			}
			if _, _, err := b.Record(ctx, id); !isNotFound(err) {
				t.Fatalf("a refused callback left a task: %v", err)
			}
		})
	}
}

func TestADispatchCannotCallItselfACallback(t *testing.T) {
	b, _ := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000004"
	root, _ := json.Marshal(map[string]any{"session_id": rootConversation, "assistant": "claude"})
	p, claims := 1, []string{}
	_, err := b.admit(id, draft{Protocol: &p, TaskID: id, Assistant: "claude", ProjectDir: t.TempDir(), Kind: TaskKindCallback,
		Title: "Do the thing", Instructions: "Do it", Claims: &claims, Root: (*json.RawMessage)(&root)}, false, false)
	if refusalCode(err) != "bad_task" {
		t.Fatalf("a dispatch of kind callback was %v; it has no command for the daemon to run and must be refused", err)
	}
}

func TestCallbacksHaveTheirOwnCapAndTakeNoChildSlot(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.MaxChildren = 1
	for i := 0; i < MaxCallbacksPerRoot; i++ {
		r := Record{
			Protocol: Protocol, ID: "c1000000-0000-4000-8000-00000000000" + string(rune('0'+i)), Assistant: "claude",
			Kind: TaskKindCallback, Title: "t", State: StateBriefed, CreatedAt: time.Now(), Claims: []string{},
			Root:     &RootRef{SessionID: rootConversation, Assistant: "claude"},
			Callback: &Callback{Argv: []string{"true"}, Dir: t.TempDir()},
		}
		if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := b.StartCallback(ctx, callbackRequest(t, "c0000000-0000-4000-8000-000000000005", "true"))
	if got := refusalCode(err); got != "callback_capacity" {
		t.Fatalf("callback %d was %q, want callback_capacity", MaxCallbacksPerRoot+1, got)
	}
	live, err := b.liveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(childrenOf(live)); n != 0 {
		t.Fatalf("%d running callbacks count as %d children", MaxCallbacksPerRoot, n)
	}
}

func TestCancellingACallbackStopsItsWholeProcessGroup(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000006"
	// The grandchild is what a deploy script leaves behind.
	if _, err := b.StartCallback(ctx, callbackRequest(t, id, "sh", "-c", "sleep 60 & sleep 60")); err != nil {
		t.Fatal(err)
	}
	r, _, _ := b.Record(ctx, id)
	if r.Callback.PGID == 0 || !alive(r.Callback.PGID) {
		t.Fatalf("not running: %+v", r.Callback)
	}
	if _, err := b.CancelTask(ctx, id, "wrong target", "", CancelCaller{Person: true, Principal: "local"}); err != nil {
		t.Fatal(err)
	}
	r = settledWithin(t, b, ctx, id, 10*time.Second)
	if r.State != StateCancelled {
		t.Fatalf("state %s after cancel", r.State)
	}
	deadline := time.Now().Add(10 * time.Second)
	for alive(r.Callback.PGID) {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d outlived its cancel", r.Callback.PGID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestACallbackPastItsTimeoutIsStopped(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000007"
	req := callbackRequest(t, id, "sleep", "60")
	req.TimeoutMinutes = 1
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r, _, _ := b.Record(ctx, id)
	later := time.Now().Add(2 * time.Minute)
	b.Clock = func() time.Time { return later }
	if !b.tendCallback(ctx, r) {
		t.Fatal("the beat did not settle a callback past its timeout")
	}
	r = settledWithin(t, b, ctx, id, time.Second)
	if r.State != StateTimeout || !strings.HasPrefix(r.Verdict, "stopped at its 1-minute timeout") {
		t.Fatalf("%s: %q", r.State, r.Verdict)
	}
	if r.Notice == nil || !strings.HasPrefix(b.FinishedLine(r, r.Notice.ID), "callback "+id[:8]+" finished: timeout") {
		t.Fatalf("timeout has no actionable completion notice: %+v", r.Notice)
	}
	if changed, err := b.Acknowledge(ctx, id, r.Notice.ID); err != nil || !changed {
		t.Fatalf("timeout result read was changed=%v, err=%v", changed, err)
	}
	if changed, err := b.Acknowledge(ctx, id, r.Notice.ID); err != nil || changed {
		t.Fatalf("repeated timeout read was changed=%v, err=%v", changed, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for alive(r.Callback.PGID) {
		if time.Now().After(deadline) {
			t.Fatal("the command outlived its timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stranded is a callback a daemon recorded and then stopped watching: the
// record is live, and its directory is whatever the run left.
func stranded(t *testing.T, b *Broker, ctx context.Context, id string, pid int) (Record, callbackFiles) {
	t.Helper()
	r := Record{
		Protocol: Protocol, ID: id, Assistant: "claude", Kind: TaskKindCallback, Title: "t", State: StateBriefed,
		CreatedAt: time.Now().Add(-time.Hour), Claims: []string{}, TimeoutMinutes: 240,
		Root:     &RootRef{SessionID: rootConversation, Assistant: "claude"},
		Callback: &Callback{Argv: []string{"true"}, Dir: t.TempDir(), PID: pid, PGID: pid},
		Dir:      b.Tasks.Path(id),
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	return r, callbackFiles{r.Dir}
}

func TestARestartedDaemonSettlesACallbackFromItsExitFile(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000008"
	r, files := stranded(t, b, ctx, id, 999999)
	_ = os.WriteFile(files.attempt(), []byte("x"), 0o600)
	_ = os.WriteFile(files.log(), []byte("pushed\nall green\n"), 0o600)
	_ = os.WriteFile(files.exit(), []byte("0"), 0o600)
	if !b.tendCallback(ctx, r) {
		t.Fatal("the beat did not settle from the exit file")
	}
	r, _, _ = b.Record(ctx, id)
	if r.State != StateSuccess || !strings.Contains(r.Verdict, "all green") {
		t.Fatalf("%s: %q", r.State, r.Verdict)
	}
}

// The attempt marker is on disk and there is no exit status: whatever became
// of the command, a recovery must not run a deploy a second time.
func TestAnAttemptedCallbackWithNoExitIsNeverRunAgain(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000009"
	r, files := stranded(t, b, ctx, id, 0)
	_ = os.WriteFile(files.attempt(), []byte("x"), 0o600)
	got := runCallbackStart(ctx, b, store.Effect{Kind: EffectCallbackStart, Subject: id})
	if _, err := os.Stat(files.pid()); err == nil {
		t.Fatalf("the command was started again: %s", got.outcome)
	}
	r, _, _ = b.Record(ctx, id)
	if r.State != StateFailure || !strings.HasPrefix(r.Verdict, "outcome unknown") {
		t.Fatalf("%s: %q", r.State, r.Verdict)
	}
}

func TestARestartedDaemonAdoptsACallbackStillRunning(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000010"
	r, files := stranded(t, b, ctx, id, 0)
	r.CreatedAt = time.Now()
	proc, err := startCallbackProcess(Record{ID: id, Dir: r.Dir, Callback: &Callback{Argv: []string{"sleep", "60"}, Dir: r.Callback.Dir}},
		files, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopGroup(proc.pgid, time.Second); proc.wait() })
	got := runCallbackStart(ctx, b, store.Effect{Kind: EffectCallbackStart, Subject: id})
	r, _, _ = b.Record(ctx, id)
	if r.Callback.PID != proc.pid || r.State.Terminal() {
		t.Fatalf("not adopted (%s): %+v, %s", got.outcome, r.Callback, r.State)
	}
}

// A start that waited on the lock while the task was settled
// must find it settled and not begin. Without the reread under the lock this
// starts a command for a task that already told its root it ended.
func TestNoCommandStartsAfterItsCallbackIsSettled(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000011"
	r, files := stranded(t, b, ctx, id, 0)
	if _, err := b.Settle(ctx, id, StateCancelled, "cancelled", nil); err != nil {
		t.Fatal(err)
	}
	_, err := startCallbackProcess(r, files, b.callbackLive(ctx, id))
	if !errors.Is(err, errCallbackSettled) {
		t.Fatalf("a start after settlement answered %v", err)
	}
	if attempted(files) {
		t.Fatal("the attempt marker was written for a settled callback")
	}
	if held, _ := lockHeld(files.lock()); held {
		t.Fatal("the refused start kept the lock")
	}
}

// The other side of the fence: while a settler holds the lock, a start
// cannot take it, and a beat cannot settle a start that holds it.
func TestTheLockFencesStartingFromSettling(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000012"
	r, files := stranded(t, b, ctx, id, 0)
	held, err := fence(files.lock())
	if err != nil || held == nil {
		t.Fatalf("fence: %v", err)
	}
	if _, err := startCallbackProcess(r, files, b.callbackLive(ctx, id)); err == nil || attempted(files) {
		t.Fatal("a start began while a settler held the lock")
	}
	unfence(held)

	proc, err := startCallbackProcess(r, files, b.callbackLive(ctx, id))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopGroup(proc.pgid, time.Second); proc.wait() })
	_ = os.Remove(files.attempt()) // as if the marker were not there: the lock alone must stop the beat
	if b.tendCallback(ctx, r) {
		t.Fatal("the beat settled a callback whose command holds the lock")
	}
	if now, _, _ := b.Record(ctx, id); now.State.Terminal() {
		t.Fatalf("settled as %s while running", now.State)
	}
}

func TestAVerdictKeepsTheLastLineOfALongOutput(t *testing.T) {
	dir := t.TempDir()
	files := callbackFiles{dir}
	var out strings.Builder
	for i := 0; i < 2000; i++ {
		out.WriteString(strings.Repeat("noise ", 20) + "\n")
	}
	out.WriteString("FAILED: the stamp is behind\n")
	_ = os.WriteFile(files.log(), []byte(out.String()), 0o600)
	got := callbackVerdict("exit 1 after 2m", files)
	if !strings.HasPrefix(got, "exit 1 after 2m") || !strings.HasSuffix(strings.TrimSpace(got), "FAILED: the stamp is behind") {
		t.Fatalf("verdict %q", got)
	}
	if n := len([]rune(got)); n > summaryLimit {
		t.Fatalf("verdict is %d runes, past %d", n, summaryLimit)
	}
}

func TestAFinishedCallbacksFilesAreReclaimedAfterAWeek(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c0000000-0000-4000-8000-000000000013"
	_, files := stranded(t, b, ctx, id, 0)
	_ = os.WriteFile(files.log(), []byte("out\n"), 0o600)
	_ = os.WriteFile(files.exit(), []byte("0"), 0o600)
	r, err := b.Settle(ctx, id, StateSuccess, "exit 0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := b.reclaimCallback(ctx, r, r.FinishedAt.Add(24*time.Hour), false); d.Outcome != "" {
		t.Fatalf("a day-old callback was %+v", d)
	}
	d := b.reclaimCallback(ctx, r, r.FinishedAt.Add(CallbackRetentionDays*24*time.Hour), false)
	if d.Outcome != ReclaimRemoved || d.Reason != WhyCallbackFiles {
		t.Fatalf("%+v", d)
	}
	if _, err := os.Stat(files.log()); !os.IsNotExist(err) {
		t.Fatal("output.log survived its reclaim")
	}
}
