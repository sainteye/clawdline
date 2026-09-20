package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/lane"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// W1 (docs/design-decisions.md §6): the shapes the first wave of this broker
// carried over from the Swift app's incidents. Every test here names the
// criterion it answers, and the report says how each was seen to fail on the
// code before the change.

const w1Secret = "5ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2"

// rawRecord reads a task's stored JSON straight from the file, past every
// layer this package has, so an assertion about "unchanged" is about bytes.
func rawRecord(t *testing.T, dir, id string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "clawdline.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var record string
	if err := db.QueryRow(`SELECT record FROM broker_tasks WHERE id = ?`, id).Scan(&record); err != nil {
		t.Fatal(err)
	}
	return record
}

// corrupt replaces a stored record with bytes no decoder takes, the way a
// half-written row or a record from a newer schema would look to this one.
func corrupt(t *testing.T, dir, id string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "clawdline.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const broken = `{"task_id": "` // truncated mid-object
	if _, err := db.Exec(`UPDATE broker_tasks SET record = ? WHERE id = ?`, broken, id); err != nil {
		t.Fatal(err)
	}
	return broken
}

// writeBrief puts a task.json a dispatch would accept in the task's directory.
func writeBrief(t *testing.T, b *Broker, id, project string, extra map[string]any) {
	t.Helper()
	brief := map[string]any{
		"clawdline_protocol": 1, "task_id": id, "assistant": "claude", "project_dir": project,
		"title": "w1", "instructions": "do the thing", "claims": []string{},
		"root": map[string]any{"session_id": rootConversation, "assistant": "claude"},
	}
	for k, v := range extra {
		brief[k] = v
	}
	body, _ := json.Marshal(brief)
	dir := b.Tasks.Path(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func refusalCode(err error) string {
	var ref Refusal
	if errors.As(err, &ref) {
		return ref.Code
	}
	return ""
}

// ① A row that cannot be decoded is listed as unreadable — in the broker's
// list, the inventory and inflight — and the same id dispatched again is a
// 409 that writes nothing. Before: `records` `continue`d past it, and the
// dispatch's read error fell through to an upsert that replaced the row.
func TestAnUnreadableRowIsListedAndItsIDIsRefused(t *testing.T) {
	b, ctx := newTestBroker(t)
	dir := b.Dir
	id := "c1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"}, ProjectDir: dir, Repository: dir,
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	before := corrupt(t, dir, id)

	records, bad, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 || len(bad) != 1 || bad[0].ID != id || bad[0].StoredState != string(StateBriefed) {
		t.Fatalf("list: records=%d unreadable=%+v; want the one row, named unreadable", len(records), bad)
	}
	if _, _, err := b.Record(ctx, id); refusalCode(err) != "task_unreadable" {
		t.Fatalf("reading the row answered %v, want task_unreadable (not 404, not 500)", err)
	}

	rows, err := b.rows(ctx, dir)
	if err != nil || len(rows) != 1 || rows[0].Section != VisibilityUnreadable || rows[0].State != StateUnreadable {
		t.Fatalf("inventory rows = %+v (err %v), want one unreadable row", rows, err)
	}
	if generation(dir, rows) == generation(dir, nil) {
		t.Error("the inventory receipt does not move when an unreadable row appears")
	}
	payload := InventoryPayload(Inventory{Unreadable: rows})
	if listed, _ := payload["unreadable"].([]map[string]any); len(listed) != 1 || listed[0]["task"] != id {
		t.Errorf("inventory payload unreadable = %v", payload["unreadable"])
	}
	inflight, err := b.Inflight(ctx, dir, "")
	if err != nil || len(inflight) != 1 || inflight[0].Task != id {
		t.Errorf("inflight = %+v (err %v), want the unreadable row", inflight, err)
	}

	// A caller with a perfectly good task.json for the same id: exactly the
	// dispatch that used to write a new task over the row.
	writeBrief(t, b, id, dir, nil)
	_, err = b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "task_unreadable" || ref.Status != 409 {
		t.Fatalf("dispatching the same id answered %v, want 409 task_unreadable", err)
	}
	if got := rawRecord(t, dir, id); got != before {
		t.Fatalf("the unreadable row was written over: %q", got)
	}
	if p := b.Pass(ctx); p.Unreadable != 1 {
		t.Errorf("the beat counted %d unreadable rows, want 1", p.Unreadable)
	}
}

// A create never replaces: the store refuses a second task under one id even
// when the caller never read the first.
func TestACreateNeverReplacesAStoredTask(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c2222222-2222-4222-8222-222222222222"
	first := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "first", State: StateQueued,
		CreatedAt: time.Now(), Claims: []string{}}
	if _, err := b.create(ctx, first, HashSecret("s")); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Title = "second"
	if _, err := b.create(ctx, second, HashSecret("t")); refusalCode(err) != "task_exists" {
		t.Fatalf("a second create answered %v, want task_exists", err)
	}
	if got, _, _ := b.Record(ctx, id); got.Title != "first" {
		t.Fatalf("the stored task became %q", got.Title)
	}
	if b.Store.Stats().Failures != 0 {
		t.Error("a refused create was counted as a store failure")
	}
}

// fakeLauncher records what the broker asks a terminal to close.
type fakeLauncher struct {
	mu     sync.Mutex
	closed [][2]string
}

func (f *fakeLauncher) ITermRunning(context.Context) (bool, error) { return false, nil }
func (f *fakeLauncher) TmuxReach(context.Context) int              { return 0 }
func (f *fakeLauncher) NewITermTab(context.Context, string) (string, error) {
	return "", errors.New("no")
}
func (f *fakeLauncher) NewTmuxWindow(context.Context, string, string) (string, error) {
	return "", errors.New("no")
}
func (f *fakeLauncher) NewTmuxSession(context.Context, string, string, string) (string, error) {
	return "", errors.New("no")
}
func (f *fakeLauncher) CloseTmuxSession(_ context.Context, pane, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, [2]string{pane, name})
	return true, nil
}

// ③ The four-minute verdict asks the tab's own source. A tmux listing that
// failed decides nothing — whatever else the reading saw — and a complete one
// without the pane decides spawn_failed; a pane holding a dialog is closed,
// and only because its pane proves it is the task's own.
func TestTheSpawnClockAsksTheTabsOwnSource(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	spawning := func(id, pane string) {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateSpawning, CreatedAt: now,
			Claims: []string{}, TimeoutMinutes: 240, ChildTerminalID: pane, ChildBackend: "tmux",
			SpawnedAt: now.Add(-5 * time.Minute)}
		if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
			t.Fatal(err)
		}
	}
	state := func(id string) State {
		r, _, err := b.Record(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return r.State
	}
	gone := "d1111111-1111-4111-8111-111111111111"
	spawning(gone, "%9")
	var inv session.Inventory
	b.Reading = func(context.Context) session.Inventory { return inv }
	iterm := session.Session{ID: "w0t0p0:ABCD", Backend: session.BackendITerm, Assistant: session.AssistantClaude}

	// A reading with nothing in it at all decides nothing (the Swift app's
	// `3a7adb8e`), complete or not.
	inv = session.Inventory{Complete: true}
	b.Pass(ctx)
	if got := state(gone); got != StateSpawning {
		t.Fatalf("an empty reading decided %q", got)
	}

	// The reading the first version judged from: tmux could not be listed,
	// iTerm2 could, so the reading "saw something" — and the tmux pane was
	// called gone on no evidence about tmux.
	inv = session.Inventory{Complete: false, Sessions: []session.Session{iterm}}
	b.Pass(ctx)
	if got := state(gone); got != StateSpawning {
		t.Fatalf("a reading with no word from tmux decided %q", got)
	}
	inv = session.Inventory{Complete: false, Sessions: []session.Session{iterm},
		Sources: map[string]bool{"tmux": false, "iterm": true, "ps": true}}
	b.Pass(ctx)
	if got := state(gone); got != StateSpawning {
		t.Fatalf("a tmux listing that failed decided %q", got)
	}

	// iTerm2 incomplete, as on this Mac every time; tmux complete and the
	// pane not in it: that is an answer.
	inv = session.Inventory{Complete: false, Sessions: []session.Session{iterm},
		Sources: map[string]bool{"tmux": true, "iterm": false, "ps": true}}
	p := b.Pass(ctx)
	if got := state(gone); got != StateSpawnFailed || p.SpawnFail != 1 {
		t.Fatalf("a complete tmux listing without the pane left %q (pulse %+v)", got, p)
	}
	if len(launcher.closed) != 0 {
		t.Fatalf("a tab that is gone left nothing provably ours, and %v was closed", launcher.closed)
	}

	// A pane that is there and holding a dialog: spawn_failed, and closed.
	stuck := "d2222222-2222-4222-8222-222222222222"
	spawning(stuck, "%8")
	trust := screen(t, "claude-trust")
	b.Screen = func(_ context.Context, id string) (string, bool) { return trust, id == "%8" }
	inv = session.Inventory{Complete: false, Sources: map[string]bool{"tmux": true, "iterm": false},
		Sessions: []session.Session{iterm, {ID: "%8", Backend: session.BackendTmux, Assistant: session.AssistantClaude}}}
	p = b.Pass(ctx)
	if got := state(stuck); got != StateSpawnFailed || p.Closed != 1 {
		t.Fatalf("a pane holding a dialog left %q (pulse %+v)", got, p)
	}
	if len(launcher.closed) != 1 || launcher.closed[0] != [2]string{"%8", ChildSessionName(stuck)} {
		t.Fatalf("closed %v, want the dialog pane's own session", launcher.closed)
	}
	if n, _ := b.Store.EventCount(ctx, "task.child.closed"); n != 1 {
		t.Errorf("task.child.closed events = %d, want 1", n)
	}
}

// ④ A turn starting is life, not a receipt: the task stays `spawning` until
// the child signs, over HTTP or with accepted.json.
func TestATurnIsNotAReceiptAndAcceptedIs(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	ids := []string{"e1111111-1111-4111-8111-111111111111", "e2222222-2222-4222-8222-222222222222"}
	for i, id := range ids {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateSpawning, CreatedAt: now,
			Claims: []string{}, TimeoutMinutes: 240, ChildTerminalID: "%" + string(rune('5'+i)),
			ChildBackend: "tmux", SpawnedAt: now}
		if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
			t.Fatal(err)
		}
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sources: map[string]bool{"tmux": true}, Sessions: []session.Session{
			{ID: "%5", Backend: session.BackendTmux, Assistant: session.AssistantClaude, State: session.StateWorking},
			{ID: "%6", Backend: session.BackendTmux, Assistant: session.AssistantClaude, State: session.StateWorking},
		}}
	}
	b.Pass(ctx)
	for _, id := range ids {
		if r, _, _ := b.Record(ctx, id); r.State != StateSpawning {
			t.Fatalf("a tab that started a turn moved the task to %q", r.State)
		}
	}
	if e, ok := b.ExecutorOf(ids[0]); !ok || e.Status != ExecutorObserved {
		t.Errorf("the turn was not kept as evidence of life: %+v", e)
	}

	// Over HTTP: the wrong secret changes nothing, the right one proves it.
	if _, err := b.Accept(ctx, ids[0], "not-it"); refusalCode(err) != "forbidden" {
		t.Fatalf("a wrong secret answered %v", err)
	}
	r, err := b.Accept(ctx, ids[0], "s")
	if err != nil || r.State != StateBriefed || r.AcceptedAt.IsZero() {
		t.Fatalf("accepted answered %+v / %v, want briefed with accepted_at", r.State, err)
	}
	if again, err := b.Accept(ctx, ids[0], "s"); err != nil || !again.AcceptedAt.Equal(r.AcceptedAt) {
		t.Fatalf("a repeated receipt answered %v and moved accepted_at", err)
	}

	// Through the file, for a child with no loopback: a wrong secret is not
	// a receipt, the right one is.
	path := filepath.Join(b.Tasks.Path(ids[1]), "accepted.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"task_secret":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Pass(ctx)
	if r, _, _ := b.Record(ctx, ids[1]); r.State != StateSpawning {
		t.Fatalf("an accepted.json with the wrong secret moved the task to %q", r.State)
	}
	if err := os.WriteFile(path, []byte(`{"task_secret":"s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Pass(ctx)
	if r, _, _ := b.Record(ctx, ids[1]); r.State != StateBriefed || r.AcceptedAt.IsZero() {
		t.Fatalf("accepted.json with the task's secret left %q", r.State)
	}
}

// ⑤ `/complete` settles nothing by itself, and a result.json written after
// it is what the record carries — symbols, artifacts, verification, review.
func TestCompleteOnlyCollectsTheFile(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "f1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}, TimeoutMinutes: 240}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	if err := b.Complete(ctx, id, "s"); refusalCode(err) != "result_not_written" {
		t.Fatalf("/complete with no file answered %v, want result_not_written", err)
	}
	if after, _, _ := b.Record(ctx, id); after.State != StateBriefed || after.Result != nil {
		t.Fatalf("/complete without a file settled the task: %q %+v", after.State, after.Result)
	}

	result := map[string]any{
		"clawdline_protocol": 1, "task_id": id, "task_secret": "s", "status": "success",
		"summary": "done", "symbols": []string{"Lease", "DeclaredWrites"}, "artifacts": []string{"artifacts/report.md"},
		"verification": map[string]any{"runs": 1, "seconds": 3, "last": "pass", "scope": "orchestrator"},
		"review":       map[string]any{"verdict": "safe_to_land", "axes": []any{}},
	}
	body, _ := json.Marshal(result)
	if err := os.MkdirAll(b.Tasks.Path(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "result.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Complete(ctx, id, "s"); err != nil {
		t.Fatalf("/complete after the file answered %v", err)
	}
	after, _, _ := b.Record(ctx, id)
	if after.State != StateSuccess || after.Result == nil {
		t.Fatalf("state %q result %+v", after.State, after.Result)
	}
	if strings.Join(after.Result.Symbols, ",") != "Lease,DeclaredWrites" ||
		len(after.Result.Artifacts) != 1 || after.Result.Verify == nil || !strings.Contains(string(after.Result.Review), "safe_to_land") {
		t.Fatalf("the record lost what the file said: %+v", after.Result)
	}
	if after.Result.Secret != "" {
		t.Error("the task secret was stored with the result")
	}
	if err := b.Complete(ctx, id, "s"); refusalCode(err) != "already_done" {
		t.Errorf("a second /complete answered %v", err)
	}
}

// A timeout is the broker's sentence, never a result in the child's name.
func TestABrokerVerdictIsNotAResult(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "f2222222-2222-4222-8222-222222222222"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateBriefed, CreatedAt: time.Now(),
		Claims: []string{}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	settled, err := b.Settle(ctx, id, StateTimeout, "The task passed its timeout without writing a result.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Result != nil || settled.Verdict == "" {
		t.Fatalf("result %+v verdict %q", settled.Result, settled.Verdict)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "one")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// ⑦ An isolated task's declared list is kept, unchanged, and is the write set
// its landing row carries — while it reserves nothing in the shared tree, in
// either direction.
func TestAnIsolatedTaskKeepsItsDeclaredWrites(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	otherRoot := "5b0c7e1e-0000-4000-8000-000000000002"
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%2", Assistant: session.AssistantClaude, ConversationID: otherRoot},
		}
	}
	dispatch := func(id string, extra map[string]any) Dispatched {
		t.Helper()
		writeBrief(t, b, id, repo, extra)
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Generation: inv.Generation, Offered: true})
		if err != nil {
			t.Fatalf("dispatch %s: %v", id[:8], err)
		}
		return out
	}
	isolated := "a7777777-7777-4777-8777-777777777777"
	out := dispatch(isolated, map[string]any{"isolation": "worktree", "claims": []string{"a.go", "docs/"}})
	r := out.Record
	if strings.Join(r.Claims, ",") != "a.go,docs/" || r.Scope() != LeaseWorktree || len(r.Lease()) != 0 {
		t.Fatalf("claims %v scope %q lease %v: the declared list was not kept as declared", r.Claims, r.Scope(), r.Lease())
	}
	if declared, known := r.DeclaredWrites(); !known || strings.Join(declared, ",") != "a.go,docs/" {
		t.Fatalf("declared writes %v known=%v", declared, known)
	}

	// Leases, both directions, against live tasks of another root. A live
	// isolated task holds no lease; an isolated dispatch asks for none (the
	// Swift app's order: the lease is dropped before it compares). The
	// control is the one pair that must still collide: shared against shared.
	live := func(id string, scope string, claims []string) {
		t.Helper()
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "live", State: StateBriefed,
			CreatedAt: time.Now(), Claims: claims, LeaseScope: scope, ProjectDir: repo, Repository: repo,
			TimeoutMinutes: 240, Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
		if scope == LeaseWorktree {
			r.Isolation = IsolationWorktree
			r.Worktree = &Worktree{Repository: repo, Branch: BranchName(id), Base: "x", Path: filepath.Join(b.Dir, id)}
		}
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
	}
	byOther := map[string]any{"session_id": otherRoot, "assistant": "claude"}
	live("b0000001-0000-4000-8000-000000000001", LeaseWorktree, []string{"b.go"})
	dispatch("b0000002-0000-4000-8000-000000000002", map[string]any{"claims": []string{"b.go"}, "root": byOther})
	live("b0000003-0000-4000-8000-000000000003", LeaseShared, []string{"c.go"})
	dispatch("b0000004-0000-4000-8000-000000000004", map[string]any{
		"isolation": "worktree", "claims": []string{"c.go"}, "root": byOther})
	blocked := "b0000005-0000-4000-8000-000000000005"
	writeBrief(t, b, blocked, repo, map[string]any{"claims": []string{"c.go"}, "root": byOther})
	current, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Dispatch(ctx, DispatchRequest{TaskID: blocked, Secret: w1Secret, Generation: current.Generation, Offered: true})
	if refusalCode(err) != "workspace_busy" {
		t.Fatalf("a shared dispatch over another root's shared lease answered %v; the arbitration is not running at all", err)
	}

	// The isolated one finished (its tab never opened here) and owes a
	// landing. Its record says pending and names no target, so it is unlanded
	// whatever its branch holds (W3, D01, D19); B1 filed an empty branch as
	// droppable on the word of the checkout's HEAD. The unlanded row carries
	// the declared list as its write set.
	commit := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "work")
	commit.Dir = r.Worktree.Path
	commit.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit in the task's checkout: %v %s", err, out)
	}
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	var row *InventoryRow
	for i := range inv.Unlanded {
		if inv.Unlanded[i].Task == isolated {
			row = &inv.Unlanded[i]
		}
	}
	if row == nil {
		t.Fatalf("the isolated task is not in unlanded: %+v", inv.Unlanded)
	}
	if strings.Join(row.DeclaredWrites, ",") != "a.go,docs/" || len(row.Claims) != 0 || row.LeaseScope != LeaseWorktree {
		t.Fatalf("unlanded row declared %v claims %v scope %q", row.DeclaredWrites, row.Claims, row.LeaseScope)
	}
	payload := InventoryPayload(inv)
	for _, item := range payload["unlanded"].([]map[string]any) {
		if item["task"] == isolated {
			if got, _ := item["declared_writes"].([]string); strings.Join(got, ",") != "a.go,docs/" {
				t.Errorf("wire declared_writes = %v", item["declared_writes"])
			}
		}
	}
}

// A record written before W1 by the broker that erased an isolated task's
// list does not read as "declared nothing".
func TestAnErasedListReadsAsUnknown(t *testing.T) {
	old := Record{Isolation: IsolationWorktree, Worktree: &Worktree{Branch: "b"}, Claims: []string{}}
	if declared, known := old.DeclaredWrites(); known {
		t.Fatalf("a pre-W1 isolated record answered %v as known", declared)
	}
	if old.Scope() != LeaseWorktree || len(old.Lease()) != 0 {
		t.Fatalf("scope %q lease %v", old.Scope(), old.Lease())
	}
}

// G34: a progress.json the broker refuses is said once — an event and a count
// — instead of vanishing.
func TestARefusedProgressFileIsSaidOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "b1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateBriefed, CreatedAt: time.Now(),
		Claims: []string{}, TimeoutMinutes: 240, ChildTerminalID: "%3"}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("字", progressLimit+1)
	body, _ := json.Marshal(map[string]string{"task_secret": "s", "note": long})
	if err := os.MkdirAll(b.Tasks.Path(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "progress.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if p := b.Pass(ctx); p.NotesRefused != 1 || p.Notes != 0 {
		t.Fatalf("pulse %+v, want one refused note", p)
	}
	for i := 0; i < 3; i++ {
		if p := b.Pass(ctx); p.NotesRefused != 0 {
			t.Fatalf("the same body was refused again on pass %d", i+2)
		}
	}
	if n, _ := b.Store.EventCount(ctx, "task.progress.refused"); n != 1 {
		t.Fatalf("task.progress.refused events = %d, want 1", n)
	}
}

// D22 at dispatch: a machine whose terminal lanes are full answers 429 before
// anything is recorded — not a task that exists only to be spawn_failed.
func TestAFullMachineRefusesADispatchBeforeRecordingIt(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Lanes = lane.New(1)
	hold, err := b.Lanes.Acquire(ctx, "terminal:tmux:%1")
	if err != nil {
		t.Fatal(err)
	}
	defer hold()
	id := "b2222222-2222-4222-8222-222222222222"
	writeBrief(t, b, id, b.Dir, nil)
	_, err = b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "terminal_busy" || ref.Status != 429 {
		t.Fatalf("a full machine answered %v, want 429 terminal_busy", err)
	}
	if _, _, err := b.Record(ctx, id); !isNotFound(err) {
		t.Fatalf("a refused dispatch left a record behind (%v)", err)
	}
}

// A root whose terminal is busy is backpressure: the notice waits, and the
// wait is not an attempt toward dead letter.
func TestABusyTerminalDefersANoticeWithoutSpendingAnAttempt(t *testing.T) {
	b, ctx := newTestBroker(t)
	r := finished(t, b, ctx, "b3333333-3333-4333-8333-333333333333")
	b.Type = func(context.Context, string, string) error {
		return lane.Busy{Key: "terminal:tmux:%1", Limit: 16, Waited: true}
	}
	b.PumpNotices(ctx)
	after, _, err := b.Record(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Notice.Attempts != 0 || after.Notice.State != NoticePending {
		t.Fatalf("a busy terminal cost the notice an attempt: %+v", after.Notice)
	}
	if b.Observed().Deferred != 1 {
		t.Error("the notice was not deferred")
	}
}
