package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// W6: reclamation, linger, handoffs, Feature Roots and graphs
// (docs/design-decisions.md §6 W6).

func w6Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// finishedCheckout stores a settled isolated task with a real checkout, and
// lets shape change the checkout before the record is written.
func w6Checkout(t *testing.T, b *Broker, ctx context.Context, repo, id, pane string, shape func(path string)) Record {
	t.Helper()
	base := w6Git(t, repo, "rev-parse", "HEAD")
	path := b.worktreePath(repo, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.Git.AddWorktree(ctx, repo, path, BranchName(id), base); err != nil {
		t.Fatal(err)
	}
	if shape != nil {
		shape(path)
	}
	head := w6Git(t, path, "rev-parse", "HEAD")
	if err := os.MkdirAll(b.Tasks.Path(id), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(b.Tasks.Path(id), "result.json"), []byte(`{"status":"success"}`), 0o600)
	_ = os.MkdirAll(filepath.Join(b.Tasks.Path(id), "work", "tmp"), 0o700)
	_ = os.WriteFile(filepath.Join(b.Tasks.Path(id), "work", "tmp", "scratch"), []byte("scratch"), 0o600)
	_ = os.MkdirAll(filepath.Join(b.Tasks.Path(id), "artifacts"), 0o700)
	_ = os.WriteFile(filepath.Join(b.Tasks.Path(id), "artifacts", "report.md"), []byte("# report"), 0o600)
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: id[:8], State: StateSuccess,
		CreatedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now().Add(-time.Hour), Claims: []string{"a.go"},
		LeaseScope: LeaseWorktree, Isolation: IsolationWorktree, ProjectDir: repo, Repository: repo,
		Dir: b.Tasks.Path(id), ChildTerminalID: pane, ChildBackend: "tmux",
		Worktree: &Worktree{Repository: repo, Path: path, Branch: BranchName(id), Base: base, Head: head},
		Landing:  &Landing{State: LandingPending}}
	if err := b.save(ctx, r, HashSecret("s"), "task.success"); err != nil {
		t.Fatal(err)
	}
	return r
}

func w6Decision(rep ReclaimReport, task, subject string) ReclaimDecision {
	for _, d := range rep.Decisions {
		if d.Task == task && d.Subject == subject {
			return d
		}
	}
	return ReclaimDecision{}
}

func reclaimEvents(t *testing.T, b *Broker, prefix string) int {
	t.Helper()
	n := 0
	for _, subject := range []string{ReclaimWorktree, ReclaimTaskDir} {
		for _, outcome := range []string{ReclaimRemoved, ReclaimPreserved, ReclaimKept} {
			kind := "reclaim." + subject + "." + outcome
			if !strings.HasPrefix(kind, prefix) {
				continue
			}
			c, err := b.Store.EventCount(context.Background(), kind)
			if err != nil {
				t.Fatal(err)
			}
			n += c
		}
	}
	return n
}

// cutover B4: one sweep removes what is proved landed or empty, keeps what is
// not landed on a preservation branch and a proved patch before removing it,
// and leaves alone anything whose owner may be there, or that is not its own.
func TestTheSweepRemovesOnlyWhatItCanProve(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	b.ReclaimGrace = -1
	reading := session.Inventory{
		Sessions: []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%9", Assistant: session.AssistantClaude},
		},
		Sources: map[string]bool{"tmux": true, "iterm": false},
	}
	b.Reading = func(context.Context) session.Inventory { return reading }
	cwds := []string{"/"}
	var cwdErr error
	b.ProcessCWDs = func(context.Context) ([]string, error) { return cwds, cwdErr }

	landed := "c6000001-0000-4000-8000-000000000001"
	dirty := "c6000002-0000-4000-8000-000000000002"
	empty := "c6000003-0000-4000-8000-000000000003"
	present := "c6000004-0000-4000-8000-000000000004"
	unknown := "c6000005-0000-4000-8000-000000000005"

	lr := w6Checkout(t, b, ctx, repo, landed, "%21", func(path string) {
		_ = os.WriteFile(filepath.Join(path, "b.go"), []byte("package a\n"), 0o644)
		w6Git(t, path, "add", ".")
		w6Git(t, path, "commit", "-q", "-m", "landed work")
	})
	w6Git(t, repo, "merge", "-q", "--ff-only", BranchName(landed))
	if _, err := b.mutate(ctx, landed, "task.landed", func(r *Record) error {
		r.Landing = &Landing{State: LandingLanded, Target: "main", Commit: lr.Worktree.Head, At: time.Now().Add(-time.Hour),
			DeliveryHead: lr.Worktree.Head}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w6Checkout(t, b, ctx, repo, dirty, "%22", func(path string) {
		_ = os.WriteFile(filepath.Join(path, "c.go"), []byte("package a\n"), 0o644)
		w6Git(t, path, "add", ".")
		w6Git(t, path, "commit", "-q", "-m", "unlanded work")
		_ = os.WriteFile(filepath.Join(path, "a.go"), []byte("package a // edited\n"), 0o644)
		_ = os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("only here\n"), 0o644)
	})
	w6Checkout(t, b, ctx, repo, empty, "%23", nil)
	w6Checkout(t, b, ctx, repo, present, "%9", nil)
	u := w6Checkout(t, b, ctx, repo, unknown, "", nil)
	_ = u
	committed := "c6000007-0000-4000-8000-000000000007"
	nested := "c6000008-0000-4000-8000-000000000008"
	filtered := "c6000009-0000-4000-8000-000000000009"
	occupied := "c600000a-0000-4000-8000-00000000000a"
	w6Checkout(t, b, ctx, repo, committed, "%27", func(path string) {
		_ = os.WriteFile(filepath.Join(path, "d.go"), []byte("package a\n"), 0o644)
		w6Git(t, path, "add", ".")
		w6Git(t, path, "commit", "-q", "-m", "committed, not landed")
	})
	w6Checkout(t, b, ctx, repo, nested, "%28", func(path string) {
		inner := filepath.Join(path, "vendored")
		_ = os.MkdirAll(inner, 0o755)
		w6Git(t, inner, "init", "-q")
		_ = os.WriteFile(filepath.Join(inner, "own.txt"), []byte("only in the inner repository"), 0o644)
	})
	w6Checkout(t, b, ctx, repo, filtered, "%29", func(path string) {
		_ = os.WriteFile(filepath.Join(path, ".gitattributes"), []byte("*.ipynb filter=nbstripout\n"), 0o644)
	})
	oc := w6Checkout(t, b, ctx, repo, occupied, "%2a", nil)
	// Spelled as a person's shell might have it: another case, and a
	// directory under it that does not exist yet.
	cwds = append(cwds, strings.Replace(oc.Worktree.Path, "worktrees", "WorkTrees", 1)+"/sub")
	foreign := filepath.Join(b.WorktreeRoot(), RepoSlug(repo), "f0000000-0000-4000-8000-00000000000f")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(foreign, "keep.txt"), []byte("not ours"), 0o600)

	// A dry run decides everything and touches nothing.
	dry, err := b.Reclaim(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{landed, dirty, empty, present, unknown} {
		if !dirExists(b.worktreePath(repo, id)) {
			t.Fatalf("a dry run removed %s", id[:8])
		}
	}
	if d := w6Decision(dry, dirty, ReclaimWorktree); d.Outcome != ReclaimWouldPreserve {
		t.Fatalf("dry run on the dirty checkout: %+v", d)
	}
	if n := reclaimEvents(t, b, "reclaim."); n != 0 {
		t.Fatalf("a dry run wrote %d events", n)
	}

	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		landed:    {ReclaimRemoved, WhyLanded},
		dirty:     {ReclaimPreserved, WhyPreserved},
		empty:     {ReclaimRemoved, WhyEmpty},
		present:   {ReclaimKept, WhyOwnerPresent},
		unknown:   {ReclaimKept, WhyOwnerUnknown},
		committed: {ReclaimRemoved, WhyCommittedOnBranch},
		nested:    {ReclaimKept, WhyNested},
		filtered:  {ReclaimKept, WhyFiltered},
		occupied:  {ReclaimKept, WhyOwnerPresent},
	}
	for id, w := range want {
		d := w6Decision(rep, id, ReclaimWorktree)
		if d.Outcome != w[0] || d.Reason != w[1] {
			t.Errorf("%s: %s/%s, want %s/%s (%v)", id[:8], d.Outcome, d.Reason, w[0], w[1], d.Evidence)
		}
		gone := !dirExists(b.worktreePath(repo, id))
		if gone != (w[0] != ReclaimKept) {
			t.Errorf("%s: on disk=%v after %s", id[:8], !gone, d.Outcome)
		}
	}
	// The branches stay, every one of them.
	for id := range want {
		if exists, known := b.Git.BranchExists(ctx, repo, BranchName(id)); !known || !exists {
			t.Errorf("the delivery branch of %s is gone", id[:8])
		}
	}
	// What the dirty checkout held is on the preservation branch, untracked
	// file and edit alike, and the patch is the one the manifest names.
	d := w6Decision(rep, dirty, ReclaimWorktree)
	if got := w6Git(t, repo, "show", PreservationBranch(dirty)+":untracked.txt"); got != "only here" {
		t.Fatalf("the untracked file was not kept: %q", got)
	}
	if got := w6Git(t, repo, "show", PreservationBranch(dirty)+":a.go"); !strings.Contains(got, "edited") {
		t.Fatalf("the edit was not kept: %q", got)
	}
	patch, err := os.ReadFile(d.Evidence["patch"].(string))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := b.Git.ApplyToTree(ctx, repo, d.Evidence["head"].(string), patch)
	if err != nil || tree != d.Evidence["tree"] {
		t.Fatalf("the saved patch does not give the snapshot's tree: %v %s", err, tree)
	}
	// The foreign directory is counted and untouched.
	if rep.ForeignCount != 1 || !dirExists(foreign) {
		t.Fatalf("foreign %d, on disk %v", rep.ForeignCount, dirExists(foreign))
	}
	// The committed, unlanded checkout's commits are on a preservation
	// branch too, now that no checkout holds its delivery branch.
	if got := w6Git(t, repo, "rev-parse", PreservationBranch(committed)); got != w6Git(t, repo, "rev-parse", BranchName(committed)) {
		t.Fatalf("the preservation branch of the committed checkout is %s", got)
	}
	// Task directories: only work/ goes, and only where the owner is gone;
	// the brief, the result and the artifacts stay everywhere.
	for id := range want {
		work := dirExists(filepath.Join(b.Tasks.Path(id), "work"))
		ownerHere := id == present || id == unknown || id == occupied
		if work != ownerHere {
			t.Errorf("%s: work/ on disk=%v", id[:8], work)
		}
		for _, keep := range []string{"result.json", "artifacts/report.md"} {
			if _, err := os.Stat(filepath.Join(b.Tasks.Path(id), keep)); err != nil {
				t.Errorf("%s: %s was removed", id[:8], keep)
			}
		}
	}
	// A checkout the sweep took is not unknown to the landing route.
	if r, _, _ := b.Record(ctx, empty); b.nothingToLandRefusal(ctx, r) != "" {
		t.Errorf("nothing_to_land after the empty checkout was taken: %q", b.nothingToLandRefusal(ctx, r))
	}
	if r, _, _ := b.Record(ctx, dirty); !strings.Contains(b.nothingToLandRefusal(ctx, r), "commit") {
		t.Errorf("nothing_to_land for the dirty one: %q", b.nothingToLandRefusal(ctx, r))
	}

	// Said once (#46): a second sweep changes nothing and writes no event.
	before := reclaimEvents(t, b, "reclaim.")
	if _, err := b.Reclaim(ctx, false); err != nil {
		t.Fatal(err)
	}
	if after := reclaimEvents(t, b, "reclaim."); after != before {
		t.Fatalf("a second sweep wrote %d more events", after-before)
	}

	// The control (DG-8): the present owner's tab leaves, and the same sweep
	// that kept it removes it — the rule was keeping it because of the tab.
	reading.Sessions = reading.Sessions[:1]
	rep, err = b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := w6Decision(rep, present, ReclaimWorktree); d.Outcome != ReclaimRemoved {
		t.Fatalf("with its owner gone: %+v", d)
	}
	// A process table that cannot be read decides nothing either.
	cwdErr = errors.New("lsof: not permitted")
	cwds = nil
	rep, _ = b.Reclaim(ctx, false)
	if d := w6Decision(rep, occupied, ReclaimWorktree); d.Outcome != ReclaimKept || d.Reason != WhyOwnerUnknown {
		t.Fatalf("with no process table: %+v", d)
	}
	cwdErr, cwds = nil, []string{"/"}
	// And a tmux source that did not answer decides nothing.
	reading.Sources = map[string]bool{"tmux": false}
	other := w6Checkout(t, b, ctx, repo, "c6000006-0000-4000-8000-000000000006", "%26", nil)
	rep, _ = b.Reclaim(ctx, false)
	if d := w6Decision(rep, other.ID, ReclaimWorktree); d.Outcome != ReclaimKept || d.Reason != WhyOwnerUnknown {
		t.Fatalf("with tmux unanswered: %+v", d)
	}
}

// A checkout changed between the snapshot and the removal is kept.
func TestACheckoutWrittenDuringTheSweepIsKept(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "c6100001-0000-4000-8000-000000000001"
	r := w6Checkout(t, b, ctx, repo, id, "%31", nil)
	snap, err := b.Git.SnapshotCheckout(ctx, r.Worktree.Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(r.Worktree.Path, "late.txt"), []byte("written after the snapshot"), 0o644)
	b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }
	d := b.removeCheckout(ctx, r, snap, ReclaimDecision{Task: id, Subject: ReclaimWorktree, Path: r.Worktree.Path,
		Evidence: map[string]any{}})
	if d.Outcome != ReclaimKept || d.Reason != WhyChanged || !dirExists(r.Worktree.Path) {
		t.Fatalf("%+v", d)
	}
}

// A path this broker did not give out is never its to remove, however it is
// spelled.
func TestOnlyOwnedPathsAreRemoved(t *testing.T) {
	root := t.TempDir()
	id := "c6200001-0000-4000-8000-000000000001"
	inside := filepath.Join(root, "slug", id)
	_ = os.MkdirAll(inside, 0o700)
	outside := t.TempDir()
	link := filepath.Join(root, "slug", "c6200002-0000-4000-8000-000000000002")
	_ = os.Symlink(outside, link)
	cases := []struct {
		path, name string
		want       bool
	}{
		{inside, id, true},
		{filepath.Join(root, "slug", "..", "slug", id), id, true},
		{outside, filepath.Base(outside), false},
		{link, "c6200002-0000-4000-8000-000000000002", false},
		{filepath.Join(root, "slug", id, "..", "..", ".."), id, false},
		{root, filepath.Base(root), false},
	}
	for _, c := range cases {
		if got := ownedPath(root, c.path, c.name); got != c.want {
			t.Errorf("ownedPath(%s) = %v", c.path, got)
		}
	}
	if err := removeOwned(root, link, "c6200002-0000-4000-8000-000000000002"); err == nil {
		t.Fatal("a symlink out of the root was removed")
	}
	if !dirExists(outside) {
		t.Fatal("the directory a link pointed at is gone")
	}
}

// openingLauncher opens a tmux session that answers with a fixed pane.
type openingLauncher struct {
	fakeLauncher
	pane string
}

func (o *openingLauncher) TmuxReach(context.Context) int { return 2 }
func (o *openingLauncher) NewTmuxSession(context.Context, string, string, string) (string, error) {
	return o.pane, nil
}

// #26: a linger written by a settlement is kept by the store, so a broker that
// restarts inside it still closes the tab — twenty seconds after its first
// look, and never on a reading with no terminals in it.
func TestALingerSurvivesARestart(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	id := "c6300001-0000-4000-8000-000000000001"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed, CreatedAt: now,
		Claims: []string{}, ChildTerminalID: "%41", ChildBackend: "tmux"}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	if rows, _ := b.Store.Lingers(ctx); len(rows) != 1 || !rows[0].Deadline.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("lingers after the settlement: %+v", rows)
	}

	// A new broker on the same store, an hour later.
	later := now.Add(time.Hour)
	launcher := &fakeLauncher{}
	b2 := &Broker{Store: b.Store, Tasks: b.Tasks, Git: b.Git, Dir: b.Dir, Launcher: launcher,
		Clock: func() time.Time { return later }}
	full := reading{sessions: map[string]session.Session{"%41": {ID: "%41", Assistant: session.AssistantClaude, State: session.StateIdle}},
		sources: map[string]bool{"tmux": true}, complete: true}
	if n := b2.closeLingers(ctx, full); n != 0 || len(launcher.closed) != 0 {
		t.Fatal("closed at once on the first look after a restart")
	}
	later = later.Add(21 * time.Second)
	if n := b2.closeLingers(ctx, reading{sessions: map[string]session.Session{}, sources: map[string]bool{"tmux": true}}); n != 0 {
		t.Fatal("a reading with no terminals in it decided")
	}
	waiting := reading{sessions: map[string]session.Session{"%41": {ID: "%41", Assistant: session.AssistantClaude, State: session.StateWaiting}},
		sources: map[string]bool{"tmux": true}, complete: true}
	if n := b2.closeLingers(ctx, waiting); n != 0 || len(launcher.closed) != 0 {
		t.Fatal("a tab with a question on it was closed")
	}
	if n := b2.closeLingers(ctx, full); n != 1 || len(launcher.closed) != 1 || launcher.closed[0][0] != "%41" {
		t.Fatalf("closed %d, launcher %v", n, launcher.closed)
	}
	if rows, _ := b.Store.Lingers(ctx); len(rows) != 0 {
		t.Fatalf("the linger is still owed: %+v", rows)
	}
	// A -1 setting owes no close at all.
	b.ChildLinger = func() time.Duration { return -1 }
	id2 := "c6300002-0000-4000-8000-000000000002"
	r.ID, r.State = id2, StateBriefed
	_ = b.save(ctx, r, HashSecret("s"), "task.queued")
	_, _ = b.Settle(ctx, id2, StateSuccess, "done", nil)
	if rows, _ := b.Store.Lingers(ctx); len(rows) != 0 {
		t.Fatalf("a kept-open child owes a close: %+v", rows)
	}
}

type typedKeys struct {
	mu    sync.Mutex
	lines map[string][]string
}

func (x *typedKeys) Type(_ context.Context, terminal, text string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lines == nil {
		x.lines = map[string][]string{}
	}
	x.lines[terminal] = append(x.lines[terminal], text)
	return nil
}

func (x *typedKeys) count(terminal string) int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.lines[terminal])
}

// A handoff opens its receiver and types the line once; the sender is told
// once; a resend is the same handoff. The machine role's holder is sent to
// succession, with the request that does it.
func TestAHandoffIsTypedOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	project := t.TempDir()
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &openingLauncher{pane: "%51"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%51", Assistant: session.AssistantClaude},
		}
	}
	yes := true
	id := "c6400001-0000-4000-8000-000000000001"
	req := HandoffRequest{ID: id, ProjectDir: project, FromSession: rootConversation, Plain: &yes, Title: "continue W6"}
	_, _, err := b.OpenHandoff(ctx, req)
	if refusalCode(err) != "bad_task" {
		t.Fatalf("without a package: %v", err)
	}
	if rem, ok := err.(Refusal).Extra["remediation"].(Remedy); !ok || rem.Route != "/v1/orchestrator/handoffs" {
		t.Fatalf("the package refusal names no way out: %+v", err.(Refusal).Extra)
	}
	pkg := filepath.Join(b.HandoffRoot(), id, "handoff.md")
	_ = os.MkdirAll(filepath.Dir(pkg), 0o700)
	_ = os.WriteFile(pkg, []byte("REFERENCES\nVERIFICATION\nOPEN THREADS\n"), 0o600)
	h, replayed, err := b.OpenHandoff(ctx, req)
	if err != nil || replayed || h.State != HandoffDelivered {
		t.Fatalf("%+v replayed=%v err=%v", h, replayed, err)
	}
	if keys.count("%51") != 1 || !strings.Contains(keys.lines["%51"][0], pkg) {
		t.Fatalf("receiver typedKeys %v", keys.lines["%51"])
	}
	if keys.count("%1") != 1 || !strings.Contains(keys.lines["%1"][0], `"handoff_receipt"`) {
		t.Fatalf("sender told %v", keys.lines["%1"])
	}
	if h, replayed, err := b.OpenHandoff(ctx, req); err != nil || !replayed || h.ID != id {
		t.Fatalf("resend: %+v %v %v", h, replayed, err)
	}
	if keys.count("%51") != 1 || keys.count("%1") != 1 {
		t.Fatal("a resend typedKeys again")
	}
	// Nobody on this machine by that conversation.
	req.ID, req.FromSession = "c6400002-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000000"
	if _, _, err := b.OpenHandoff(ctx, req); refusalCode(err) != "sender_not_found" {
		t.Fatalf("an absent sender: %v", err)
	}
}

// A Root Assignment is an independent root: typedKeys once, replayed by its
// request id, refused under a reused one, and carrying nothing of a child.
func TestARootAssignmentIsNobodysChild(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &openingLauncher{pane: "%61"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%61", Assistant: session.AssistantClaude}}
	}
	req := RootAssignmentRequest{RequestID: "c6500001-0000-4000-8000-000000000001", Assistant: "claude",
		ProjectDir: t.TempDir(), Label: "Feature X", Assignment: Assignment{Objective: "o", Scope: "s",
			Constraints: "c", RelevantReferences: "r", Acceptance: "a"}}
	if _, _, err := b.OpenRootAssignment(ctx, "other", req); refusalCode(err) != "idempotency_mismatch" {
		t.Fatalf("a key that is not the request id: %v", err)
	}
	a, replayed, err := b.OpenRootAssignment(ctx, req.RequestID, req)
	if err != nil || replayed || a.State != AssignmentBriefed || a.Ownership != "independent_root" {
		t.Fatalf("%+v %v %v", a, replayed, err)
	}
	if keys.count("%61") != 1 || !strings.Contains(keys.lines["%61"][0], a.BriefPath) {
		t.Fatalf("typedKeys %v", keys.lines["%61"])
	}
	brief, _ := os.ReadFile(a.BriefPath)
	for _, head := range []string{"OBJECTIVE", "SCOPE", "CONSTRAINTS", "RELEVANT REFERENCES", "ACCEPTANCE"} {
		if !strings.Contains(string(brief), head+"\n") {
			t.Fatalf("the brief has no %s", head)
		}
	}
	body, _ := json.Marshal(a)
	for _, lineage := range []string{"task_id", "secret", "timeout", "result", "parent", "landing", "handoff"} {
		if strings.Contains(string(body), `"`+lineage) {
			t.Fatalf("a Root Assignment carries %q: %s", lineage, body)
		}
	}
	if again, replayed, err := b.OpenRootAssignment(ctx, req.RequestID, req); err != nil || !replayed || again.ID != a.ID {
		t.Fatalf("resend: %+v %v %v", again, replayed, err)
	}
	if keys.count("%61") != 1 {
		t.Fatal("a resend typedKeys the brief again")
	}
	req.Label = "Feature Y"
	if _, _, err := b.OpenRootAssignment(ctx, req.RequestID, req); refusalCode(err) != "request_conflict" {
		t.Fatalf("another assignment under the same request id: %v", err)
	}
}

func w6Graph(current string) *Graph {
	return &Graph{ID: "c6600000-0000-4000-8000-000000000000", Destination: "split and join", CurrentNode: current,
		Nodes: []GraphNode{
			{ID: "a", Title: "left", Kind: "delivery", Acceptance: []string{"x"}},
			{ID: "b", Title: "right", Kind: "delivery", Acceptance: []string{"x"}},
			{ID: "join", Title: "join", Kind: "review", DependsOn: []string{"a", "b"}, Acceptance: []string{"x"}},
		}}
}

// A graph node waits for what it depends on, runs once, and is done only when
// its task says so.
func TestAGraphNodeWaitsForItsDependencies(t *testing.T) {
	b, ctx := newTestBroker(t)
	raw, _ := json.Marshal(w6Graph("join"))
	if _, err := admitGraph(raw); err != nil {
		t.Fatal(err)
	}
	cyc := w6Graph("a")
	cyc.Nodes[0].DependsOn = []string{"join"}
	raw, _ = json.Marshal(cyc)
	if _, err := admitGraph(raw); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a cycle was admitted: %v", err)
	}
	save := func(id, node string, state State) {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: node, State: state, CreatedAt: time.Now(),
			Claims: []string{}, Graph: w6Graph(node)}
		if err := b.save(ctx, r, HashSecret("s"), "task.x"); err != nil {
			t.Fatal(err)
		}
	}
	join := Record{Graph: w6Graph("join")}
	if err := b.checkGraph(ctx, join); refusalCode(err) != "graph_frontier_blocked" {
		t.Fatalf("join before anything ran: %v", err)
	}
	save("c6600001-0000-4000-8000-000000000001", "a", StateBriefed)
	if err := b.checkGraph(ctx, Record{Graph: w6Graph("a")}); refusalCode(err) != "graph_node_active" {
		t.Fatalf("a second a: %v", err)
	}
	save("c6600002-0000-4000-8000-000000000002", "b", StateFailure)
	err := b.checkGraph(ctx, join)
	if refusalCode(err) != "graph_dependency_failed" {
		t.Fatalf("join after b failed: %v", err)
	}
	views, _ := b.Graphs(ctx)
	if len(views) != 1 || strings.Join(views[0].Frontier, ",") != "b" {
		t.Fatalf("frontier %+v", views)
	}
	other := w6Graph("b")
	other.Destination = "something else"
	if err := b.checkGraph(ctx, Record{Graph: other}); refusalCode(err) != "graph_definition_conflict" {
		t.Fatalf("a second definition: %v", err)
	}
}

// Every remedy is built from the table, with what the refusal knows filled in.
func TestARemedyIsBuiltFromTheTable(t *testing.T) {
	rem, ok := RemedyFor("stale_inventory", map[string]any{"project": "/r"})
	if !ok || rem.Method != "GET" || rem.Header != HeaderMachine || !strings.Contains(rem.Command, "project=%2Fr") {
		t.Fatalf("%+v", rem)
	}
	if _, ok := RemedyFor("no_such_code", nil); ok {
		t.Fatal("a code with no row got a remedy")
	}
	if _, ok := ErrReclaimRunning.Extra["remediation"].(Remedy); !ok {
		t.Fatal("reclaim_running names no way out")
	}
}

var _ = store.Reclaim{}
