package projectassistant

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The source repository fake implements the port's atomic CAS/receipt facts.
// Admission, worktree effects, capacity and worker launch use the REAL broker
// and its empty SQLite store; the fake launcher never invokes a model.
type bridgeRepository struct {
	Repository
	mu             sync.Mutex
	data           Export
	receipts       map[string]pa.Receipt
	failProjection bool
}

func (r *bridgeRepository) Bot(context.Context, pa.BotKey) (pa.Bot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.Bot, nil
}
func (r *bridgeRepository) Export(context.Context, pa.BotKey) (Export, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	x := r.data
	x.Messages = slices.Clone(x.Messages)
	x.WorkLinks = slices.Clone(x.WorkLinks)
	x.Events = slices.Clone(x.Events)
	return x, nil
}
func (r *bridgeRepository) Receipt(_ context.Context, c pa.Command) (*pa.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.receipts[c.IdempotencyKey]; ok {
		if _, err := pa.DecideReplay(c, &old); err != nil {
			return nil, err
		}
		return &old, nil
	}
	return nil, nil
}
func (r *bridgeRepository) Commit(_ context.Context, m Mutation) (pa.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.receipts[m.Command.IdempotencyKey]; ok {
		if _, err := pa.DecideReplay(m.Command, &old); err != nil {
			return pa.Receipt{}, err
		}
		return old, nil
	}
	if m.ExpectedVersion != r.data.Bot.Version {
		return pa.Receipt{}, bridgeFailure(pa.StaleVersion, "bot")
	}
	if r.failProjection && len(m.WorkLinks) > 0 {
		return pa.Receipt{}, errors.New("injected source transaction failure")
	}
	if m.Bot != nil {
		r.data.Bot = *m.Bot
	}
	r.data.Bot.Version = m.ExpectedVersion + 1
	if m.Conversation != nil {
		r.data.Conversation = *m.Conversation
	}
	for _, w := range m.WorkLinks {
		found := false
		for i, old := range r.data.WorkLinks {
			if old.ID == w.ID {
				r.data.WorkLinks[i] = w
				found = true
				break
			}
		}
		if !found {
			r.data.WorkLinks = append(r.data.WorkLinks, w)
		}
	}
	r.data.Messages = append(r.data.Messages, m.Messages...)
	r.data.Events = append(r.data.Events, m.Events...)
	receipt := m.Receipt
	receipt.Command = m.Command
	r.receipts[m.Command.IdempotencyKey] = receipt
	return receipt, nil
}

type bridgeLauncher struct {
	ports.Launcher
	calls atomic.Int64
}

func (*bridgeLauncher) ITermRunning(context.Context) (bool, error) { return false, nil }
func (*bridgeLauncher) TmuxReach(context.Context) int              { return 2 }
func (l *bridgeLauncher) NewTmuxSession(context.Context, string, string, string) (string, error) {
	l.calls.Add(1)
	return "%80", nil
}
func (*bridgeLauncher) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return true, nil
}

func bridgeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newBridgeFixture(t *testing.T) (*BrokerWorkBridge, *orchestrator.Broker, *bridgeRepository, *orchestrator.AssistantCurrent, pa.WorkRequest, *bridgeLauncher) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	repoDir := t.TempDir()
	repoDir, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	bridgeGit(t, repoDir, "init", "-q", "-b", "main")
	if err = os.WriteFile(filepath.Join(repoDir, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bridgeGit(t, repoDir, "add", "a.go")
	bridgeGit(t, repoDir, "commit", "-q", "-m", "Initial fixture")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	key := pa.BotKey{MachineID: "source", BotID: "bot"}
	scope := pa.ProjectScope{MachineID: key.MachineID, ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	fence := pa.Fence{Key: key, Project: scope, ConversationID: "chat", Generation: 1, BindingID: "binding"}
	current := &orchestrator.AssistantCurrent{Fence: fence, Feature: pa.FeatureState{Enabled: true}, Source: pa.SourceStatus{MachineID: key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive},
		Project: pa.ProjectResolution{Scope: scope, Directory: repoDir, Current: true, Available: true}, ActiveTurnID: "turn", DispatchAllowed: true, AllowedClaims: []string{"a.go"}, Assistant: "codex", PermissionMode: "edits", TimeoutMinutes: 30}
	authority, err := orchestrator.NewAssistantAuthority(orchestrator.AssistantAuthorityOptions{Current: func(context.Context, pa.BotKey) (orchestrator.AssistantCurrent, error) { return *current, nil }, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	launcher := &bridgeLauncher{}
	broker := &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Git: gitadapter.New(), Dir: dir, AssistantAuthority: authority, Launcher: launcher, ScreenReadEvery: time.Millisecond,
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Live: func(context.Context) []session.Session {
			return []session.Session{{ID: "%80", Backend: session.BackendTmux, Assistant: session.AssistantCodex}}
		}, Type: func(context.Context, string, string) error { return nil }}
	screen, err := os.ReadFile("../orchestrator/testdata/codex-composer.txt")
	if err != nil {
		t.Fatal(err)
	}
	broker.Screen = func(context.Context, string) (string, bool) { return string(screen), true }
	repository := &bridgeRepository{data: Export{Bot: pa.Bot{Key: key, Project: scope, ConversationID: "chat", Version: 1}, Conversation: pa.Conversation{Key: key, ID: "chat", BindingID: "binding", Generation: 1, ActiveTurnID: "turn"}}, receipts: map[string]pa.Receipt{}}
	bridge, err := NewWorkBridge(WorkBridgeOptions{Repository: repository, Broker: broker, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	broker.AssistantWorkChanged = bridge.Sync
	actor, err := authority.Issue(ctx, fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	q := pa.WorkRequest{Actor: actor, Command: pa.Command{Key: key, Operation: pa.DispatchOperation, IdempotencyKey: "one", ContentHash: strings.Repeat("a", 64)}, TurnID: "turn", Brief: "Change only a.go in the isolated worktree.", Claims: []string{"a.go"}}
	return bridge, broker, repository, current, q, launcher
}

func TestWorkBridgeKeepsRuntimeReceiptAndPersistsOneResultThroughLanding(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	// This is the exact ownership boundary of Engine.DispatchWork: it wrote
	// the original command unknown BEFORE making its first bridge call.
	bot, _ := repo.Bot(ctx, q.Command.Key)
	if _, err := bridge.commit(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}); err != nil {
		t.Fatal(err)
	}
	link, err := bridge.Dispatch(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if link.State != pa.WorkAccepted || link.TaskID == "" {
		t.Fatalf("accepted link %+v", link)
	}
	x, _ := repo.Export(ctx, q.Command.Key)
	if len(x.WorkLinks) != 1 || len(x.Messages) != 0 {
		t.Fatalf("link was not persisted %+v", x)
	}
	if _, err = bridge.Dispatch(ctx, q); err != nil || launcher.calls.Load() != 1 {
		t.Fatalf("replay %v launches=%d", err, launcher.calls.Load())
	}
	r, _, _ := broker.Record(ctx, link.TaskID)
	if err = os.WriteFile(filepath.Join(r.Worktree.Path, "a.go"), []byte("package a\n// ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bridgeGit(t, r.Worktree.Path, "add", "a.go")
	bridgeGit(t, r.Worktree.Path, "commit", "-q", "-m", "Scoped fixture delivery")
	commit := bridgeGit(t, r.Worktree.Path, "rev-parse", "HEAD")
	if _, err = broker.Settle(ctx, r.ID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "The scoped change is ready."}); err != nil {
		t.Fatal(err)
	}
	link, err = bridge.Status(ctx, q.Actor, r.ID)
	if err != nil || link.State != pa.WorkCompleted {
		t.Fatalf("completion %+v %v", link, err)
	}
	x, _ = repo.Export(ctx, q.Command.Key)
	if len(x.Messages) != 1 || x.Messages[0].Kind != pa.WorkMessage || x.Messages[0].Text != "The scoped change is ready." {
		t.Fatalf("visible result %+v", x.Messages)
	}
	bridgeGit(t, r.Repository, "merge", "--ff-only", r.Worktree.Branch)
	if _, err = broker.Land(ctx, r.ID, orchestrator.LandingRequest{State: "landed", Target: "main", Commit: commit, Machine: true}); err != nil {
		t.Fatal(err)
	}
	link, err = bridge.Status(ctx, q.Actor, r.ID)
	if err != nil || link.State != pa.WorkLanded || link.LandingCommit != commit {
		t.Fatalf("landing %+v %v", link, err)
	}
	if _, err = bridge.Status(ctx, q.Actor, r.ID); err != nil {
		t.Fatal(err)
	}
	x, _ = repo.Export(ctx, q.Command.Key)
	if len(x.Messages) != 1 || x.Conversation.LastMessageSeq != 1 {
		t.Fatalf("completion/landing duplicated unread %+v", x)
	}
	original, err := repo.Receipt(ctx, q.Command)
	if err != nil || original.State != pa.EffectUnknown || original.ResourceID != q.TurnID {
		t.Fatalf("Engine receipt overwritten %+v %v", original, err)
	}
	for _, event := range x.Events {
		if event.Kind == pa.TraceUpdated {
			t.Fatal("work result emitted a trace/unread surrogate")
		}
	}
}

func TestWorkBridgeRecoversAcceptedTaskAfterProjectionFailureAndRestart(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	repo.failProjection = true
	if _, err := bridge.Dispatch(ctx, q); err == nil {
		t.Fatal("source write failure was hidden")
	}
	id := orchestrator.AssistantTaskID(q.Command)
	r, _, err := broker.Record(ctx, id)
	if err != nil || r.AssistantOwner == nil {
		t.Fatalf("broker acceptance lost %+v %v", r, err)
	}
	x, _ := repo.Export(ctx, q.Command.Key)
	if len(x.WorkLinks) != 0 {
		t.Fatal("failed transaction left a link")
	}
	repo.failProjection = false
	restarted, err := NewWorkBridge(WorkBridgeOptions{Repository: repo, Broker: broker, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	broker.AssistantWorkChanged = restarted.Sync
	link, err := restarted.Status(ctx, q.Actor, id)
	if err != nil || link.TaskID != id {
		t.Fatalf("status did not reconstruct %+v %v", link, err)
	}
	if _, err = restarted.Dispatch(ctx, q); err != nil || launcher.calls.Load() != 1 {
		t.Fatalf("recovery relaunched: %v %d", err, launcher.calls.Load())
	}
	changed := q
	changed.Brief += " Different arguments."
	if _, err = restarted.Dispatch(ctx, changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("same command different effect: %v", err)
	}
}

func TestWorkBridgeDoesNotReplayPreparedUnknownWithoutBrokerTask(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	id := orchestrator.AssistantTaskID(q.Command)
	prepare := bridgeCommand(q.Command.Key, "bridge-prepare-"+id, orchestrator.AssistantRequestHash(q))
	bot, _ := repo.Bot(ctx, q.Command.Key)
	if _, err := bridge.commit(ctx, bot, Mutation{Command: prepare, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: id}}); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Dispatch(ctx, q); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatalf("unknown preparation replayed: %v", err)
	}
	if _, err := bridge.Status(ctx, q.Actor, id); !bridgeTaskMissing(err) {
		t.Fatalf("status created a task: %v", err)
	}
	rows, _, err := broker.Records(ctx)
	if err != nil || len(rows) != 0 || launcher.calls.Load() != 0 {
		t.Fatalf("unknown performed effects %+v %v %d", rows, err, launcher.calls.Load())
	}
	changed := q
	changed.Brief += " Different arguments."
	if _, err := bridge.Dispatch(ctx, changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("unknown preparation key conflict lost: %v", err)
	}
}

func TestWorkBridgeRejectsUnauthenticatedInputBeforeAnyReceipt(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	q.Actor.Fence.Project.ProjectID = "other"
	if _, err := bridge.Dispatch(ctx, q); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("forged actor accepted: %v", err)
	}
	if len(repo.receipts) != 0 || launcher.calls.Load() != 0 {
		t.Fatal("forged actor wrote a receipt or launched")
	}
	broker.AssistantAuthority = nil
	if _, err := NewWorkBridge(WorkBridgeOptions{Repository: repo, Broker: broker, Clock: time.Now}); err == nil {
		t.Fatal("nil verifier passed constructor")
	}
}

func TestWorkBridgeBoardLinkCanArriveAfterAcceptance(t *testing.T) {
	for _, phase := range []string{"accepted", "completed"} {
		t.Run(phase, func(t *testing.T) {
			bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
			ctx := context.Background()
			bot, _ := repo.Bot(ctx, q.Command.Key)
			if _, err := bridge.commit(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}); err != nil {
				t.Fatal(err)
			}
			link, err := bridge.Dispatch(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r, _, err := broker.Record(ctx, link.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			var commit string
			if phase == "completed" {
				if err = os.WriteFile(filepath.Join(r.Worktree.Path, "a.go"), []byte("package a\n// linked delivery\n"), 0600); err != nil {
					t.Fatal(err)
				}
				bridgeGit(t, r.Worktree.Path, "add", "a.go")
				bridgeGit(t, r.Worktree.Path, "commit", "-q", "-m", "Scoped linked delivery")
				commit = bridgeGit(t, r.Worktree.Path, "rev-parse", "HEAD")
				if _, err = broker.Settle(ctx, r.ID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "One visible result."}); err != nil {
					t.Fatal(err)
				}
				link, err = bridge.Status(ctx, q.Actor, r.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, _ := repo.Export(ctx, q.Command.Key)
			state := link.State
			boardID := "a1111111-1111-4111-8111-111111111111"
			if err = broker.BindWork(ctx, link.TaskID, boardID, "fixture"); err != nil {
				t.Fatal(err)
			}
			link, err = bridge.Status(ctx, q.Actor, link.TaskID)
			if err != nil || link.BoardItemID != boardID || link.State != state {
				t.Fatalf("Board binding did not reach same-state link: %+v %v", link, err)
			}
			after, _ := repo.Export(ctx, q.Command.Key)
			if len(after.Messages) != len(before.Messages) || after.Conversation.LastMessageSeq != before.Conversation.LastMessageSeq || len(after.Events) != len(before.Events)+1 {
				t.Fatalf("Board binding changed visible/unread facts: before=%+v after=%+v", before, after)
			}
			for i := 0; i < 3; i++ {
				if _, err = bridge.Status(ctx, q.Actor, r.ID); err != nil {
					t.Fatal(err)
				}
			}
			repeated, _ := repo.Export(ctx, q.Command.Key)
			if len(repeated.Events) != len(after.Events) || len(repeated.Messages) != len(after.Messages) || repeated.Conversation.LastMessageSeq != after.Conversation.LastMessageSeq {
				t.Fatal("identical Status snapshot added events or unread")
			}
			if phase == "completed" {
				bridgeGit(t, r.Repository, "merge", "--ff-only", r.Worktree.Branch)
				if _, err = broker.Land(ctx, r.ID, orchestrator.LandingRequest{State: "landed", Target: "main", Commit: commit, Machine: true}); err != nil {
					t.Fatal(err)
				}
				link, err = bridge.Status(ctx, q.Actor, r.ID)
				if err != nil || link.State != pa.WorkLanded || link.BoardItemID != boardID || link.LandingCommit != commit {
					t.Fatalf("linked landing projection %+v %v", link, err)
				}
				landed, _ := repo.Export(ctx, q.Command.Key)
				if len(landed.Messages) != 1 || landed.Conversation.LastMessageSeq != 1 {
					t.Fatal("Board/landing repeated the visible result")
				}
				if _, err = bridge.Status(ctx, q.Actor, r.ID); err != nil {
					t.Fatal(err)
				}
				repeated, _ = repo.Export(ctx, q.Command.Key)
				if len(repeated.Events) != len(landed.Events) {
					t.Fatal("repeated landed snapshot added an event")
				}
			}
			original, err := repo.Receipt(ctx, q.Command)
			if err != nil || original.State != pa.EffectUnknown || original.ResourceID != q.TurnID {
				t.Fatalf("Runtime receipt changed %+v %v", original, err)
			}
			if launcher.calls.Load() != 1 {
				t.Fatal("projection change relaunched worker")
			}
		})
	}
}
