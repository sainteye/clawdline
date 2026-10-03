package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A root cancelling a child it dispatched by mistake (cancel.go).

// runningChild stores a briefed child of rootConversation with a tmux tab and
// a shared claim, as a dispatch leaves it.
func runningChild(t *testing.T, b *Broker, ctx context.Context, id string) Record {
	t.Helper()
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "wrong brief", State: StateBriefed,
		CreatedAt: b.now(), Claims: []string{"a.go"}, LeaseScope: LeaseShared, TimeoutMinutes: 240,
		ChildTerminalID: "%9", ChildBackend: "tmux",
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude", Label: "root tab"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	return r
}

func asRoot() CancelCaller { return CancelCaller{SessionID: rootConversation} }

func cancelRefusal(t *testing.T, err error) (string, Refusal) {
	t.Helper()
	var ref Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("answered %v, want a typed refusal", err)
	}
	return ref.Code, ref
}

// The root cancels: the task is cancelled with the reason as its verdict, its
// tab is closed by a recorded effect, its claim and slot are free, and the
// root is owed one notice that says it was cancelled and why.
func TestARootCancelsAChildItDispatchedByMistake(t *testing.T) {
	b, ctx := newTestBroker(t)
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	id := "ca000000-0000-4000-8000-000000000001"
	runningChild(t, b, ctx, id)

	out, err := b.CancelTask(ctx, id, "  wrong\nbrief ", "key-1", asRoot())
	if err != nil {
		t.Fatal(err)
	}
	r := out.Record
	if out.Replayed || r.State != StateCancelled {
		t.Fatalf("answered %+v", out)
	}
	if r.Verdict != "Cancelled by its root: wrong brief" {
		t.Errorf("verdict %q", r.Verdict)
	}
	if r.Cancellation == nil || r.Cancellation.By != "root:"+rootConversation || r.Cancellation.Key != "key-1" {
		t.Errorf("cancellation %+v", r.Cancellation)
	}

	// The tab: one close effect, recorded with the settlement and run.
	effects, err := b.Store.Effects(ctx, EffectCloseChild, id)
	if err != nil || len(effects) != 1 || effects[0].State != store.EffectDone {
		t.Fatalf("close effects %+v, %v; want one, done", effects, err)
	}
	if len(launcher.closed) != 1 || launcher.closed[0] != [2]string{"%9", ChildSessionName(id)} {
		t.Fatalf("closed %v", launcher.closed)
	}
	if got := TabPlanSentence(tabPolicy(r, StateCancelled, LingerDefault)); got != "stopped and closed when it is cancelled" {
		t.Errorf("the cancelled tab plan reads %q", got)
	}
	if _, owed := lingerOwed(t, b, ctx, id); owed {
		t.Error("a cancelled tab is closed at once; no linger is owed")
	}
	if ev, ok := latestEvent(t, b, ctx, "task.cancelled", id); !ok || !strings.Contains(string(mustJSON(t, ev)), TabRuleCancelled) {
		t.Errorf("the settlement's event does not carry the cancelled tab rule: %v", ev)
	}

	// The claim and the child slot: a live task holds both, a cancelled one
	// neither.
	live, err := b.liveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range live {
		if l.ID == id {
			t.Fatal("the cancelled task is still live, holding its claim and its slot")
		}
	}

	// One notice, which says what happened.
	got, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notice == nil || got.Notice.State != NoticePending {
		t.Fatalf("notice %+v, want one pending", got.Notice)
	}
	line := b.FinishedLine(got, got.Notice.ID)
	for _, want := range []string{"finished: cancelled", "Cancelled by its root: wrong brief", "its tab was stopped",
		"claims released"} {
		if !strings.Contains(line, want) {
			t.Errorf("the notice line does not say %q:\n%s", want, line)
		}
	}
	wire, err := b.NoticeWire(ctx, got)
	if err != nil || !strings.Contains(wire, `"claims_released":true`) {
		t.Errorf("notice wire %s, %v", wire, err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Who may: the root, or the person. Any other Session is refused with a code
// that says who may, and nothing is settled.
func TestOnlyTheRootOrThePersonMayCancel(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "ca000000-0000-4000-8000-000000000002"
	runningChild(t, b, ctx, id)

	_, err := b.CancelTask(ctx, id, "mine now", "k", CancelCaller{SessionID: "other-conversation"})
	code, ref := cancelRefusal(t, err)
	if code != "not_task_root" || ref.Status != 403 || !strings.Contains(ref.Message, "root tab") ||
		!strings.Contains(ref.Message, "the person") {
		t.Fatalf("another Session: %+v", ref)
	}
	if _, err := b.CancelTask(ctx, id, "r", "k", CancelCaller{}); err == nil {
		t.Fatal("a Session that named nobody was let through")
	} else if code, _ := cancelRefusal(t, err); code != "session_required" {
		t.Fatalf("an unnamed Session: %s", code)
	}
	if r, _, _ := b.Record(ctx, id); r.State != StateBriefed {
		t.Fatalf("a refused cancel changed the task: %s", r.State)
	}

	out, err := b.CancelTask(ctx, id, "not this one", "k", CancelCaller{Person: true, Principal: "local"})
	if err != nil || out.Record.State != StateCancelled || out.Record.Cancellation.By != "person:local" ||
		!strings.HasPrefix(out.Record.Verdict, "Cancelled by the person: ") {
		t.Fatalf("the person: %+v, %v", out.Record, err)
	}

	// A task with no root is the person's alone.
	orphan := "ca000000-0000-4000-8000-000000000003"
	r := runningChild(t, b, ctx, orphan)
	r.Root = nil
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CancelTask(ctx, orphan, "r", "k", asRoot()); err == nil {
		t.Fatal("a Session cancelled a task with no root")
	} else if code, _ := cancelRefusal(t, err); code != "not_task_root" {
		t.Fatalf("no root: %s", code)
	}
}

// A root that carries a squad capability is named by it. A Session without
// one cannot cancel that root's child by naming the root's conversation, and
// another Session's capability is not the root's.
func TestABoundRootIsNamedByItsCapabilityNotItsConversationID(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "ca000000-0000-4000-8000-000000000004"
	runningChild(t, b, ctx, id)
	mine := boundCapability(t, b, ctx, rootConversation, "terminal-root")
	theirs := boundCapability(t, b, ctx, "other-conversation", "terminal-other")

	for _, c := range []struct {
		name   string
		caller CancelCaller
		code   string
	}{
		{"the root's id without its capability", asRoot(), "session_actor_required"},
		{"another Session's capability naming the root", CancelCaller{Capability: theirs, SessionID: rootConversation}, "not_task_root"},
		{"a capability that is nobody's", CancelCaller{Capability: "not-a-capability"}, "session_actor_required"},
	} {
		_, err := b.CancelTask(ctx, id, "r", "k", c.caller)
		if err == nil {
			t.Fatalf("%s: cancelled", c.name)
		}
		if code, _ := cancelRefusal(t, err); code != c.code {
			t.Errorf("%s: %s, want %s", c.name, code, c.code)
		}
	}
	out, err := b.CancelTask(ctx, id, "duplicate", "k", CancelCaller{Capability: mine})
	if err != nil || out.Record.State != StateCancelled {
		t.Fatalf("the root with its capability: %+v, %v", out, err)
	}
}

// boundCapability binds a squad launch to one conversation and answers its
// capability.
func boundCapability(t *testing.T, b *Broker, ctx context.Context, conversation, terminal string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	scope, ok := projects.ResolveScope(project)
	if !ok {
		t.Fatal("project scope unavailable")
	}
	document, _ := json.Marshal(map[string]string{"definition_id": "clawdline.persona.backend", "scope_id": scope.ID})
	launch, err := b.Store.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Store.RecordSquadTerminal(ctx, launch.ID, terminal); err != nil {
		t.Fatal(err)
	}
	if err := b.Store.BindSquadConversation(ctx, launch.ID, conversation); err != nil {
		t.Fatal(err)
	}
	return launch.ActorCapability
}

// The same request twice is one cancel: the resend answers the same success,
// replayed, and settles nothing again. A different request for a task that
// has ended is a conflict naming the state.
func TestACancelSentTwiceIsOneAndAnEndedTaskIsAConflict(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Launcher = &fakeLauncher{}
	id := "ca000000-0000-4000-8000-000000000005"
	runningChild(t, b, ctx, id)

	first, err := b.CancelTask(ctx, id, "duplicate", "key-1", asRoot())
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.CancelTask(ctx, id, "duplicate", "key-1", asRoot())
	if err != nil || !again.Replayed || again.Record.State != StateCancelled ||
		!again.Record.FinishedAt.Equal(first.Record.FinishedAt) {
		t.Fatalf("the resend: %+v, %v", again, err)
	}
	if effects, _ := b.Store.Effects(ctx, EffectCloseChild, id); len(effects) != 1 {
		t.Fatalf("%d close effects after a resend, want 1", len(effects))
	}

	_, err = b.CancelTask(ctx, id, "duplicate", "key-2", asRoot())
	code, ref := cancelRefusal(t, err)
	if code != "task_already_terminal" || ref.Status != 409 || ref.Extra["state"] != "cancelled" {
		t.Fatalf("another key: %+v", ref)
	}
	// A resend is still the caller's: another Session holding the key is
	// refused who it is before it is told anything.
	if _, err := b.CancelTask(ctx, id, "duplicate", "key-1", CancelCaller{SessionID: "other"}); err == nil {
		t.Fatal("another Session's resend was answered as a success")
	}

	done := "ca000000-0000-4000-8000-000000000006"
	finished(t, b, ctx, done)
	_, err = b.CancelTask(ctx, done, "too late", "key-3", asRoot())
	code, ref = cancelRefusal(t, err)
	if code != "task_already_terminal" || ref.Extra["state"] != "success" {
		t.Fatalf("a finished task: %+v", ref)
	}
}

// The reason is required and bounded; nothing is settled for a bad one.
func TestACancelNeedsAReasonOfBoundedLength(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "ca000000-0000-4000-8000-000000000007"
	runningChild(t, b, ctx, id)
	for reason, want := range map[string]string{
		" \n\t ":                                    "reason_required",
		strings.Repeat("x", CancelReasonLimit+1):    "reason_too_long",
		strings.Repeat("x ", CancelReasonLimit/2+1): "reason_too_long",
	} {
		_, err := b.CancelTask(ctx, id, reason, "k", asRoot())
		if code, _ := cancelRefusal(t, err); code != want {
			t.Errorf("reason of %d bytes: %s, want %s", len(reason), code, want)
		}
	}
	if _, err := b.CancelTask(ctx, id, strings.Repeat("x", CancelReasonLimit), strings.Repeat("k", cancelKeyLimit+1), asRoot()); err == nil {
		t.Fatal("an overlong key was accepted")
	}
	if r, _, _ := b.Record(ctx, id); r.State != StateBriefed {
		t.Fatalf("a refused cancel changed the task: %s", r.State)
	}
	if _, err := b.CancelTask(ctx, id, strings.Repeat("x", CancelReasonLimit), "k", asRoot()); err != nil {
		t.Fatalf("a reason at the limit: %v", err)
	}
}

// A child that has commits is not thrown away: its branch stays, the landing
// is pending with a note that counts the commits, the notice says so, and the
// landing list carries it.
func TestACancelledChildsCommitsAreKeptForItsRoot(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "ca000000-0000-4000-8000-000000000008"
	r := checkedOutTask(t, b, ctx, repo, id)
	gitIn(t, r.Worktree.Path, "commit", "-q", "--allow-empty", "-m", "one")
	gitIn(t, r.Worktree.Path, "commit", "-q", "--allow-empty", "-m", "two")

	out, err := b.CancelTask(ctx, id, "wrong scope", "k", asRoot())
	if err != nil {
		t.Fatal(err)
	}
	l := out.Record.Landing
	if l == nil || l.State != LandingPending || l.Settlement != SettlementCarried {
		t.Fatalf("landing %+v, want a pending one carrying commits", l)
	}
	if want := "branch " + r.Worktree.Branch + " was kept and has 2 commit(s) to look at"; !strings.Contains(l.Note, want) {
		t.Errorf("the landing note %q does not say %q", l.Note, want)
	}
	if exists, known := b.Git.BranchExists(ctx, repo, r.Worktree.Branch); !known || !exists {
		t.Fatal("the cancelled child's branch is gone")
	}
	if _, err := os.Stat(r.Worktree.Path); err != nil {
		t.Fatalf("the cancelled child's checkout is gone: %v", err)
	}
	got, _, _ := b.Record(ctx, id)
	line := b.FinishedLine(got, "n1")
	for _, want := range []string{"2 commit(s) to look at", "task land " + id + " abandoned"} {
		if !strings.Contains(line, want) {
			t.Errorf("the notice line does not say %q:\n%s", want, line)
		}
	}
	pending, err := b.PendingLandings(ctx, LandingReading{})
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, p := range pending {
		listed = listed || p.Record.ID == id
	}
	if !listed {
		t.Fatal("the landing list does not carry the cancelled child's branch")
	}
}

// The daemon stops after the cancel is committed and before the tab is
// closed. The close is a recorded effect: the next broker runs it once, and a
// resend in between answers the cancel that succeeded without a second close.
func TestARestartBetweenTheCancelAndTheCloseStillClosesTheTabOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	id := "ca000000-0000-4000-8000-000000000009"
	runningChild(t, b, ctx, id)

	b.EffectFault = func(p string, e store.Effect) {
		if p == "committed" {
			panic("the daemon stopped")
		}
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = b.CancelTask(ctx, id, "duplicate", "key-1", asRoot())
	}()
	b.EffectFault = nil
	if len(launcher.closed) != 0 {
		t.Fatalf("closed before the crash point: %v", launcher.closed)
	}
	r, _, _ := b.Record(ctx, id)
	if r.State != StateCancelled {
		t.Fatalf("the cancel was not committed: %s", r.State)
	}
	again, err := b.CancelTask(ctx, id, "duplicate", "key-1", asRoot())
	if err != nil || !again.Replayed {
		t.Fatalf("the resend after the crash: %+v, %v", again, err)
	}

	// The next broker, on the same store, past the age at which an unheld
	// effect is unattended.
	next := &Broker{Store: b.Store, Tasks: b.Tasks, Git: b.Git, Dir: b.Dir, Launcher: launcher,
		Clock: func() time.Time { return time.Now().Add(time.Hour) }}
	if n := next.RecoverEffects(ctx); n != 1 {
		t.Fatalf("recovery settled %d effect(s), want 1", n)
	}
	if len(launcher.closed) != 1 {
		t.Fatalf("closed %d time(s), want once", len(launcher.closed))
	}
	if n := next.RecoverEffects(ctx); n != 0 || len(launcher.closed) != 1 {
		t.Fatalf("a second recovery settled %d and closed %d", n, len(launcher.closed))
	}
}
