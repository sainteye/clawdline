package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/lane"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func assistantBrokerFixture(t *testing.T) (*Broker, context.Context, *AssistantCurrent, pa.WorkRequest, *atomic.Int64) {
	t.Helper()
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	key := pa.BotKey{MachineID: "source", BotID: "bot"}
	scope := pa.ProjectScope{MachineID: key.MachineID, ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	c := &AssistantCurrent{Fence: pa.Fence{Key: key, Project: scope, ConversationID: "chat", BindingID: "binding", Generation: 1},
		Source: pa.SourceStatus{MachineID: key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}, Feature: pa.FeatureState{Enabled: true},
		Project: pa.ProjectResolution{Scope: scope, Directory: repo, Current: true, Available: true}, ActiveTurnID: "turn", DispatchAllowed: true, AllowedClaims: []string{"a.go", "new.go"}, Assistant: "codex", PermissionMode: "edits", TimeoutMinutes: 30}
	authority, err := NewAssistantAuthority(AssistantAuthorityOptions{Current: func(_ context.Context, k pa.BotKey) (AssistantCurrent, error) {
		if k != c.Fence.Key {
			return AssistantCurrent{}, assistantFailure(pa.ScopeDenied, "bot")
		}
		return *c, nil
	}, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	b.AssistantAuthority = authority
	b.AssistantWorkChanged = func(context.Context, AssistantWorkSnapshot) error { return nil }
	b.SquadActorRequired = true
	b.Launcher = &recordingLauncher{pane: "%80"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%80", Backend: session.BackendTmux, Assistant: session.AssistantCodex}}
	}
	b.Screen = func(_ context.Context, id string) (string, bool) { return screen(t, "codex-composer"), id == "%80" }
	var calls atomic.Int64
	b.Type = func(_ context.Context, terminal, text string) error {
		calls.Add(1)
		rows, _, err := b.Records(ctx)
		if err != nil || len(rows) != 1 || rows[0].State != StateQueued {
			t.Errorf("launch preceded durable admission: %v %+v", err, rows)
		}
		return nil
	}
	actor, err := authority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	q := pa.WorkRequest{Actor: actor, Command: pa.Command{Key: key, Operation: pa.DispatchOperation, IdempotencyKey: "one", ContentHash: strings.Repeat("a", 64)}, TurnID: "turn", Brief: "Change only a.go in the isolated worktree.", Claims: []string{"a.go"}}
	return b, ctx, c, q, &calls
}

func TestAssistantBrokerAcceptsIndependentOwnerAndReportsResultAndLanding(t *testing.T) {
	b, ctx, current, q, calls := assistantBrokerFixture(t)
	var states []pa.WorkState
	b.AssistantWorkChanged = func(_ context.Context, s AssistantWorkSnapshot) error {
		states = append(states, s.Link.State)
		return nil
	}
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := out.Record
	if r.AssistantOwner == nil || r.Root != nil || r.RootTerminalID != "" || r.ScheduleID != "" || r.Callback != nil || r.WorkID != "" {
		t.Fatalf("fabricated Session or automation owner: %+v", r)
	}
	if r.Worktree == nil || r.Worktree.Path == current.Project.Directory || r.Isolation != IsolationWorktree || r.Scope() != LeaseWorktree || len(r.Claims) != 1 || r.Claims[0] != "a.go" {
		t.Fatalf("isolation/claims: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(r.Worktree.Path, "a.go")); err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(r)
	brief, err := os.ReadFile(filepath.Join(b.Tasks.Path(r.ID), "CHILD.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), q.Actor.CapabilityID) || strings.Contains(string(brief), q.Actor.CapabilityID) {
		t.Fatal("private actor credential escaped")
	}
	if !strings.Contains(string(brief), "Bot") {
		t.Fatal("brief does not name its actual owner")
	}
	s, err := b.AssistantStatus(ctx, q.Actor, r.ID)
	if err != nil || s.Link.State != pa.WorkAccepted {
		t.Fatalf("accepted projection %+v %v", s, err)
	}
	if _, err = b.mutate(ctx, r.ID, "task.accepted", func(r *Record) error { r.State = StateBriefed; return nil }); err != nil {
		t.Fatal(err)
	}
	s, err = b.AssistantStatus(ctx, q.Actor, r.ID)
	if err != nil || s.Link.State != pa.WorkExecuting {
		t.Fatalf("executing %+v %v", s, err)
	}
	commit := commitFile(t, r.Worktree.Path, "a.go", "package a\n// changed\n")
	if _, err = b.Settle(ctx, r.ID, StateSuccess, "", &taskdir.Result{Status: "success", Summary: "The scoped change is ready."}); err != nil {
		t.Fatal(err)
	}
	s, err = b.AssistantStatus(ctx, q.Actor, r.ID)
	if err != nil || s.Link.State != pa.WorkCompleted || s.Link.LandingCommit != "" || s.Result != "The scoped change is ready." {
		t.Fatalf("completed %+v %v", s, err)
	}
	gitIn(t, current.Project.Directory, "merge", "--ff-only", r.Worktree.Branch)
	if _, err = land(b, ctx, r.ID, "landed", "main", commit); err != nil {
		t.Fatal(err)
	}
	s, err = b.AssistantStatus(ctx, q.Actor, r.ID)
	if err != nil || s.Link.State != pa.WorkLanded || s.Link.LandingCommit != commit {
		t.Fatalf("landed %+v %v", s, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("worker launch count %d", calls.Load())
	}
	if len(states) < 3 || states[len(states)-1] != pa.WorkLanded {
		t.Fatalf("source callback states %v", states)
	}
	if _, err = b.Dispatch(ctx, DispatchRequest{TaskID: r.ID, Secret: w1Secret}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("ordinary door replayed Bot task: %v", err)
	}
}

func TestAssistantBrokerRejectsForgedExpiredRevokedAndCrossScopeActors(t *testing.T) {
	cases := map[string]func(*Broker, *AssistantCurrent, *pa.WorkRequest){
		"missing validator": func(b *Broker, _ *AssistantCurrent, _ *pa.WorkRequest) { b.AssistantAuthority = nil },
		"missing sink":      func(b *Broker, _ *AssistantCurrent, _ *pa.WorkRequest) { b.AssistantWorkChanged = nil },
		"forged credential": func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) {
			q.Actor.CapabilityID = strings.Repeat("b", 64)
		},
		"forged operations": func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) {
			q.Actor.Operations = append(q.Actor.Operations, pa.ActorOperation("approve"))
		},
		"forged source":    func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.Actor.Fence.Key.MachineID = "other" },
		"forged bot":       func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.Actor.Fence.Key.BotID = "other" },
		"forged project":   func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.Actor.Fence.Project.ProjectID = "other" },
		"old generation":   func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Fence.Generation++ },
		"old binding":      func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Fence.BindingID = "new" },
		"old conversation": func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Fence.ConversationID = "new" },
		"expired": func(b *Broker, _ *AssistantCurrent, _ *pa.WorkRequest) {
			b.AssistantAuthority.o.Clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
		},
		"revoked binding":    func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.BindingRevoked = true },
		"revoked source":     func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Source.State = pa.SourceRevoked },
		"stale source":       func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Source.Fresh = false },
		"read only":          func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Source.Writable = false },
		"disabled":           func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Feature.Enabled = false },
		"dispatch policy":    func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.DispatchAllowed = false },
		"canonical registry": func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Project.Scope.CanonicalID = "other" },
		"stale registry":     func(_ *Broker, c *AssistantCurrent, _ *pa.WorkRequest) { c.Project.Current = false },
		"wrong turn":         func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.TurnID = "other" },
		"old turn capability": func(_ *Broker, c *AssistantCurrent, q *pa.WorkRequest) {
			c.ActiveTurnID = "next-turn"
			q.TurnID = c.ActiveTurnID
		},
		"cross command":     func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.Command.Key.BotID = "other" },
		"unauthorized file": func(_ *Broker, _ *AssistantCurrent, q *pa.WorkRequest) { q.Claims = []string{"secret.go"} },
		"traversal": func(_ *Broker, c *AssistantCurrent, q *pa.WorkRequest) {
			c.AllowedClaims = []string{"../secret"}
			q.Claims = c.AllowedClaims
		},
		"windows traversal": func(_ *Broker, c *AssistantCurrent, q *pa.WorkRequest) {
			c.AllowedClaims = []string{"..\\secret"}
			q.Claims = c.AllowedClaims
		},
		"directory": func(_ *Broker, c *AssistantCurrent, q *pa.WorkRequest) {
			c.AllowedClaims = []string{"."}
			q.Claims = c.AllowedClaims
		},
		"absolute path": func(_ *Broker, c *AssistantCurrent, q *pa.WorkRequest) {
			c.AllowedClaims = []string{c.Project.Directory + "/a.go"}
			q.Claims = c.AllowedClaims
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			b, ctx, c, q, calls := assistantBrokerFixture(t)
			alter(b, c, &q)
			if _, err := b.DispatchAssistant(ctx, q); err == nil {
				t.Fatal("invalid actor/request accepted")
			}
			rows, _, err := b.Records(ctx)
			if err != nil || len(rows) != 0 || calls.Load() != 0 {
				t.Fatalf("refusal wrote/spawned %+v %d %v", rows, calls.Load(), err)
			}
		})
	}
}

func TestAssistantBrokerIdempotencyCapacityAndHistoricalRead(t *testing.T) {
	b, ctx, c, q, calls := assistantBrokerFixture(t)
	b.MaxChildren = 1
	one, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := b.DispatchAssistant(ctx, q)
	if err != nil || !replay.Replayed || replay.Record.ID != one.Record.ID || calls.Load() != 1 {
		t.Fatalf("replay %+v %v calls=%d", replay, err, calls.Load())
	}
	changed := q
	changed.Brief += " Different payload."
	if _, err = b.DispatchAssistant(ctx, changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("same command different arguments: %v", err)
	}
	changed = q
	changed.Command.ContentHash = strings.Repeat("b", 64)
	if _, err = b.DispatchAssistant(ctx, changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("same key different hash: %v", err)
	}
	changed = q
	changed.Command.IdempotencyKey = "two"
	if _, err = b.DispatchAssistant(ctx, changed); refusalCode(err) != "over_capacity" {
		t.Fatalf("per-Bot capacity: %v", err)
	}
	c.Fence.Generation++
	c.Fence.BindingID = "next-binding"
	actor, err := b.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.AssistantStatus(ctx, actor, one.Record.ID); err != nil {
		t.Fatalf("new generation cannot read historical task: %v", err)
	}
	if _, err = b.DispatchAssistant(ctx, q); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("old generation still executes: %v", err)
	}
	c.Feature.Enabled = false
	c.Source.Access = pa.HistoryOnly
	if _, err = b.AssistantStatus(ctx, actor, one.Record.ID); err != nil {
		t.Fatalf("disabled history cannot read: %v", err)
	}
}

func TestAssistantBrokerRestartRecoversAcceptedTaskWithoutLaunchingAgain(t *testing.T) {
	b, ctx, c, q, calls := assistantBrokerFixture(t)
	b.EffectFault = func(point string, _ store.Effect) {
		if point == "started" {
			panic("crash after durable acceptance")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("fault did not execute")
			}
		}()
		_, _ = b.DispatchAssistant(ctx, q)
	}()
	held, _, err := b.Record(ctx, AssistantTaskID(q.Command))
	if err != nil || held.State != StateQueued || held.AssistantOwner == nil {
		t.Fatalf("durable task lost: %+v %v", held, err)
	}
	restarted := &Broker{Store: b.Store, Tasks: b.Tasks, Git: b.Git, Dir: b.Dir, Live: b.Live, Launcher: b.Launcher, Type: b.Type, Screen: b.Screen, AssistantWorkChanged: b.AssistantWorkChanged}
	restarted.AssistantAuthority, err = NewAssistantAuthority(AssistantAuthorityOptions{Current: func(context.Context, pa.BotKey) (AssistantCurrent, error) { return *c, nil }, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.AssistantStatus(ctx, q.Actor, held.ID); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("restart kept old credential: %v", err)
	}
	q.Actor, err = restarted.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.DispatchAssistant(ctx, q)
	if err != nil || !recovered.Replayed || recovered.Record.State != StateQueued || calls.Load() != 0 {
		t.Fatalf("unknown effect replayed: %+v %v calls=%d", recovered, err, calls.Load())
	}
}

func TestAssistantBrokerDoesNotRelaxNormalSessionOrSquadAdmission(t *testing.T) {
	b, ctx, _, _, _ := assistantBrokerFixture(t)
	id := "a1000000-0000-4000-8000-000000000001"
	writeBrief(t, b, id, b.Dir, nil)
	if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true}); refusalCode(err) != "root_unresolved" {
		t.Fatalf("normal root check changed: %v", err)
	}
	if _, err := NewAssistantAuthority(AssistantAuthorityOptions{Clock: time.Now}); err == nil {
		t.Fatal("nil validator accepted")
	}
	// Existing Squad-bound roots must still prove the same launch capability.
	capability := boundCapability(t, b, ctx, rootConversation, "%1")
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}}
	}
	if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true}); refusalCode(err) != "session_actor_required" {
		t.Fatalf("normal Squad check changed: %v cap-present=%v", err, capability != "")
	}
}

func TestAssistantBrokerHonorsMachineAndTerminalBackpressure(t *testing.T) {
	t.Run("machine child capacity", func(t *testing.T) {
		b, ctx, c, q, calls := assistantBrokerFixture(t)
		b.MaxChildren = 1
		for i := 0; i < b.machineChildren(); i++ {
			id := fmt.Sprintf("a0000000-0000-4000-8000-%012d", i)
			r := Record{Protocol: Protocol, ID: id, Assistant: "codex", State: StateBriefed, ProjectDir: c.Project.Directory, Claims: []string{}, CreatedAt: time.Now()}
			if err := b.save(ctx, r, HashSecret("fixture"), "task.briefed"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.DispatchAssistant(ctx, q); refusalCode(err) != "over_capacity" {
			t.Fatalf("machine capacity bypassed: %v", err)
		}
		if calls.Load() != 0 {
			t.Fatal("full machine launched a worker")
		}
	})
	t.Run("terminal lane capacity", func(t *testing.T) {
		b, ctx, _, q, calls := assistantBrokerFixture(t)
		b.Lanes = lane.New(1)
		release, err := b.Lanes.Acquire(ctx, "occupied")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err = b.DispatchAssistant(ctx, q); refusalCode(err) != "terminal_busy" {
			t.Fatalf("terminal backpressure bypassed: %v", err)
		}
		if _, _, err = b.Record(ctx, AssistantTaskID(q.Command)); !isNotFound(err) || calls.Load() != 0 {
			t.Fatalf("busy lane recorded or launched: %v", err)
		}
	})
}

func TestAssistantBrokerRechecksRevocationBeforeDurableAcceptance(t *testing.T) {
	b, ctx, c, q, calls := assistantBrokerFixture(t)
	b.AssistantQuotas = func(time.Time) ([]AssistantQuotaSnapshot, error) { c.BindingRevoked = true; return nil, nil }
	if _, err := b.DispatchAssistant(ctx, q); pa.Code(err) != pa.Revoked {
		t.Fatalf("revoked during admission accepted: %v", err)
	}
	if _, _, err := b.Record(ctx, AssistantTaskID(q.Command)); !isNotFound(err) || calls.Load() != 0 {
		t.Fatalf("revocation left effects: %v", err)
	}
}

func TestAssistantBrokerRejectsSymlinkAndDirectoryClaims(t *testing.T) {
	for _, kind := range []string{"directory", "symlink file", "symlink parent"} {
		t.Run(kind, func(t *testing.T) {
			b, ctx, c, q, calls := assistantBrokerFixture(t)
			claim := "scope"
			switch kind {
			case "directory":
				if err := os.Mkdir(filepath.Join(c.Project.Directory, claim), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink file":
				if err := os.Symlink(filepath.Join(c.Project.Directory, "a.go"), filepath.Join(c.Project.Directory, claim)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "symlink parent":
				if err := os.Symlink(t.TempDir(), filepath.Join(c.Project.Directory, claim)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				claim += "/new.go"
			}
			c.AllowedClaims = []string{claim}
			q.Claims = []string{claim}
			if _, err := b.DispatchAssistant(ctx, q); pa.Code(err) != pa.ScopeDenied {
				t.Fatalf("unsafe claim accepted: %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("unsafe claim launched worker")
			}
		})
	}
}

func TestAssistantBrokerHistoryReadSurvivesExecutionRevocation(t *testing.T) {
	b, ctx, c, q, calls := assistantBrokerFixture(t)
	out, err := b.DispatchAssistant(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	readActor, err := b.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c.Feature.Enabled = false
	c.BindingRevoked = true
	c.ActiveTurnID = ""
	c.DispatchAllowed = false
	c.Source.Access = pa.HistoryOnly
	c.Source.Writable = false
	newReader, err := b.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("execution revocation removed historical read authority: %v", err)
	}
	for _, reader := range []pa.AssistantExecutionActor{readActor, newReader} {
		if snapshot, e := b.AssistantStatus(ctx, reader, out.Record.ID); e != nil || snapshot.Link.TaskID != out.Record.ID {
			t.Fatalf("history Status cannot recover accepted task: %+v %v", snapshot, e)
		}
	}
	if _, err = b.AssistantStatus(ctx, q.Actor, out.Record.ID); err == nil {
		t.Fatal("mixed execution actor silently downgraded to history")
	}
	if _, err = b.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("revoked execution authority reissued")
	}
	if _, err = b.DispatchAssistant(ctx, q); err == nil {
		t.Fatal("revoked actor still dispatched")
	}
	readRequest := q
	readRequest.Actor = newReader
	if _, err = b.DispatchAssistant(ctx, readRequest); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("history actor gained execution: %v", err)
	}
	base := *c
	for name, alter := range map[string]func(){
		"source revoked":    func() { c.Source.State = pa.SourceRevoked },
		"source unreadable": func() { c.Source.Readable = false },
		"stale fence":       func() { c.Fence.Generation++ },
	} {
		t.Run(name, func(t *testing.T) {
			*c = base
			alter()
			if _, e := b.AssistantStatus(ctx, newReader, out.Record.ID); e == nil {
				t.Fatal("history security check bypassed")
			}
		})
	}
	*c = base
	forged := newReader
	forged.CapabilityID = strings.Repeat("b", 64)
	if _, err = b.AssistantStatus(ctx, forged, out.Record.ID); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("forged history credential: %v", err)
	}
	b.AssistantAuthority.o.Clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err = b.AssistantStatus(ctx, newReader, out.Record.ID); pa.Code(err) != pa.Expired {
		t.Fatalf("expired history credential: %v", err)
	}
	b.AssistantAuthority.o.Clock = time.Now
	for name, alter := range map[string]func(){
		"other Bot":     func() { c.Fence.Key.BotID = "other" },
		"other Project": func() { c.Fence.Project.ProjectID = "other"; c.Project.Scope = c.Fence.Project },
		"other source": func() {
			c.Fence.Key.MachineID = "other"
			c.Fence.Project.MachineID = "other"
			c.Project.Scope = c.Fence.Project
			c.Source.MachineID = "other"
		},
		"other conversation": func() { c.Fence.ConversationID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			*c = base
			alter()
			reader, e := b.AssistantAuthority.Issue(ctx, c.Fence, []pa.ActorOperation{pa.ReadWork}, time.Now().Add(time.Hour))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.AssistantStatus(ctx, reader, out.Record.ID); pa.Code(e) != pa.ScopeDenied {
				t.Fatalf("valid reader crossed historical owner scope: %v", e)
			}
		})
	}
	*c = base
	if calls.Load() != 1 {
		t.Fatal("history recovery launched another worker")
	}
}

// A Project-wide policy admits a file no list names, as CodyNC lets a Bot
// work anywhere in its workspace, and still refuses every unsafe path.
func TestAssistantBrokerProjectWidePolicyKeepsPathChecks(t *testing.T) {
	b, ctx, c, q, _ := assistantBrokerFixture(t)
	c.AllowedClaims, c.AnyProjectFile = nil, true
	q.Claims = []string{"docs/unlisted.md"}
	if _, err := b.DispatchAssistant(ctx, q); err != nil {
		t.Fatalf("a Project file was refused under a Project-wide policy: %v", err)
	}
	for _, claim := range []string{"../outside.go", "..", ".", "docs/*.md", "/etc/hosts"} {
		b, ctx, c, q, calls := assistantBrokerFixture(t)
		c.AllowedClaims, c.AnyProjectFile = nil, true
		q.Claims = []string{claim}
		if _, err := b.DispatchAssistant(ctx, q); pa.Code(err) != pa.ScopeDenied {
			t.Fatalf("%q was admitted under a Project-wide policy: %v", claim, err)
		}
		if calls.Load() != 0 {
			t.Fatalf("%q launched a worker", claim)
		}
	}
	b, ctx, c, q, _ = assistantBrokerFixture(t)
	c.AnyProjectFile = true
	if err := os.Symlink(t.TempDir(), filepath.Join(c.Project.Directory, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	q.Claims = []string{"linked/new.go"}
	if _, err := b.DispatchAssistant(ctx, q); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a symlinked parent was admitted under a Project-wide policy: %v", err)
	}
}

// A worker whose first screen holds a dialog tells the Bot chat that the
// person must answer it, instead of looking like ordinary acceptance.
func TestAssistantWorkSnapshotShowsAWorkerWaitingForThePerson(t *testing.T) {
	owner := &AssistantOwner{Fence: pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}}, TurnID: "turn"}
	r := Record{ID: "task", State: StateSpawning, AssistantOwner: owner}
	if got := AssistantWorkSnapshotOf(r).Link.State; got != pa.WorkAccepted {
		t.Fatalf("a spawning worker without a dialog: %s", got)
	}
	r.AwaitingDialogSince = time.Unix(1700000000, 0)
	if got := AssistantWorkSnapshotOf(r).Link.State; got != pa.WorkWaitingForPerson {
		t.Fatalf("a worker holding a dialog was not shown as waiting for the person: %s", got)
	}
	r.State = StateSpawnFailed
	if got := AssistantWorkSnapshotOf(r).Link.State; got != pa.WorkFailed {
		t.Fatalf("an unanswered dialog that ended the spawn: %s", got)
	}
}
