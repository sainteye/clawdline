package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A completion notice a root can follow word for word, and a child that wrote
// nothing that leaves no wrap-up behind (2026-10-02). The notice told a root
// whose merge the broker records by itself to "merge it, then record the
// landing", and told the root of a read-only child to "commit in <worktree>
// … or record abandoned" through a route no command reached.

// readOnlyTask is checkedOutTask for a child dispatched with `--claims ""`:
// it declared, explicitly, that it writes nothing.
func readOnlyTask(t *testing.T, b *Broker, ctx context.Context, repo, id string, root bool) Record {
	t.Helper()
	base, err := b.Git.ResolveCommit(ctx, repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id)
	if err := b.Git.AddWorktree(ctx, repo, path, BranchName(id), base); err != nil {
		t.Fatal(err)
	}
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "read-only", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}, LeaseScope: LeaseWorktree,
		Isolation: IsolationWorktree, ProjectDir: repo, Repository: repo, TimeoutMinutes: 240,
		Worktree: &Worktree{Repository: repo, Path: path, Branch: BranchName(id), Base: base, Head: base}}
	if root {
		r.Root = &RootRef{SessionID: rootConversation, Assistant: "claude"}
	}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	return r
}

// noticeLines records every notice typed, for what it said.
func noticeLines(b *Broker) *[]string {
	var lines []string
	b.Type = func(_ context.Context, _ string, text string) error {
		lines = append(lines, text)
		return nil
	}
	return &lines
}

// ① The case from the report: a read-only child finishes with nothing on its
// branch and nothing in its checkout. The broker records nothing_to_land before
// the line is typed, and the line says so instead of asking for a commit.
func TestAChildThatWroteNothingIsRecordedNothingToLandBeforeItsNoticeIsTyped(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "e8520000-0000-4000-8000-000000000001"
	readOnlyTask(t, b, ctx, repo, id, true)
	lines := noticeLines(b)
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	b.PumpNotices(ctx)

	l := landingOf(t, b, ctx, id)
	if l.State != LandingNothingToLand || l.Note != landingEmptyNote {
		t.Fatalf("landing = %s (%q), want nothing_to_land recorded by the broker", l.State, l.Note)
	}
	if len(*lines) != 1 {
		t.Fatalf("typed %d notices, want 1", len(*lines))
	}
	line := (*lines)[0]
	if !strings.Contains(line, "recorded nothing_to_land") {
		t.Errorf("the notice does not say the broker settled it:\n%s", line)
	}
	for _, unwanted := range []string{"commit in", "abandoned", "nothing is committed"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("a child with nothing to land still asks for %q:\n%s", unwanted, line)
		}
	}
	// Recording the landing is not the root reading the notice: it still ACKs.
	after, _, _ := b.Record(ctx, id)
	if after.Notice == nil || after.Notice.State == NoticeAcknowledged {
		t.Errorf("the broker's own nothing_to_land closed the root's notice: %+v", after.Notice)
	}
}

// ② The same without a root to tell: settlement records it before the beat.
func TestSettlementRecordsNothingToLandForAChildWithNoRoot(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "e8520000-0000-4000-8000-000000000002"
	readOnlyTask(t, b, ctx, repo, id, false)
	if _, err := b.Settle(ctx, id, StateFailure, "could not", nil); err != nil {
		t.Fatal(err)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingNothingToLand {
		t.Fatalf("at settlement: %s, want nothing_to_land", l.State)
	}
	b.detectLandings(ctx)
	if l := landingOf(t, b, ctx, id); l.State != LandingNothingToLand {
		t.Fatalf("after the redundant look: %s, want nothing_to_land", l.State)
	}
	if said := b.landingDetect.said; said != 0 {
		t.Errorf("the detector logged %d line(s): %v", said, b.landingDetect.logged)
	}
}

// ③ Everything that is not "declared nothing, wrote nothing, finished by
// itself" stays pending for a root: a change left in the checkout, a commit on
// the branch, paths it declared, a tab that timed out and may still write.
func TestOnlyAChildThatDeclaredAndWroteNothingIsSettledByTheBroker(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	noticeLines(b)

	dirty := readOnlyTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000003", true)
	if err := os.WriteFile(filepath.Join(dirty.Worktree.Path, "notes.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed := readOnlyTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000004", true)
	commitFile(t, committed.Worktree.Path, "wrote.go", "package a\n")
	declared := checkedOutTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000005")
	timedOut := readOnlyTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000006", true)

	for _, r := range []Record{dirty, committed, declared} {
		if _, err := b.Settle(ctx, r.ID, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Settle(ctx, timedOut.ID, StateTimeout, "timed out", nil); err != nil {
		t.Fatal(err)
	}
	b.PumpNotices(ctx)
	b.detectLandings(ctx)
	for _, r := range []Record{dirty, committed, declared, timedOut} {
		if l := landingOf(t, b, ctx, r.ID); l.State != LandingPending {
			t.Errorf("task %s: landing %s, want it left pending for a root", r.ID, l.State)
		}
	}
	if said := b.landingDetect.said; said != 0 {
		t.Errorf("leaving a root's decision to the root was logged %d time(s): %v", said, b.landingDetect.logged)
	}
	// The one with a change in its checkout is told to commit it, by path.
	settled, _, _ := b.Record(ctx, dirty.ID)
	if line := b.FinishedLine(settled, "n1"); !strings.Contains(line, "commit in "+dirty.Worktree.Path) {
		t.Errorf("a child with an uncommitted change is not told where to commit it:\n%s", line)
	}
}

// landCommandPattern is every `clawdline task land` a line names.
var landCommandPattern = regexp.MustCompile(`clawdline task land (\S+) (\S+)((?: --\S+ <\S+>)*)`)

// ④ A merged delivery is told the merge is enough. And every `task land` a
// notice names, filled in and sent with the orchestrator token as the command
// sends it, is accepted by the landing gate — the root that followed the old
// line to the letter was refused `forbidden`.
func TestEveryLandingANoticeNamesIsOneTheGateAccepts(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)

	carried := deliveredTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000007")
	line := b.FinishedLine(carried, "n1")
	if !strings.Contains(line, "the broker records the landing by itself") {
		t.Errorf("a committed delivery is not told the merge records itself:\n%s", line)
	}
	if strings.Contains(line, "then record the landing") {
		t.Errorf("a committed delivery is still asked to record by hand what a merge records:\n%s", line)
	}

	// Recorded by hand, the commit that carries it onto main is named.
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "carry the delivery", carried.Worktree.Branch)
	merged := gitIn(t, repo, "rev-parse", "main")

	empty := checkedOutTask(t, b, ctx, repo, "e8520000-0000-4000-8000-000000000008")
	emptySettled, err := b.Settle(ctx, empty.ID, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		r    Record
		fill map[string]string
	}{
		{carried, map[string]string{"<branch>": "main", "<commit>": merged}},
		{emptySettled, nil},
	}
	for _, c := range cases {
		named := landCommandPattern.FindAllStringSubmatch(b.FinishedLine(c.r, "n1"), -1)
		if len(named) == 0 {
			t.Fatalf("task %s: the notice names no clawdline task land:\n%s", c.r.ID, b.FinishedLine(c.r, "n1"))
		}
		// The first named action is the one tried; the rest are alternatives
		// to it and would be refused as another claim once it is recorded.
		m := named[0]
		if m[1] != c.r.ID {
			t.Fatalf("the command names task %s, not %s", m[1], c.r.ID)
		}
		req := LandingRequest{State: m[2], Machine: true}
		flags := strings.Fields(m[3])
		for i := 0; i+1 < len(flags); i += 2 {
			v, ok := c.fill[flags[i+1]]
			if !ok {
				t.Fatalf("no value for %s", flags[i+1])
			}
			switch flags[i] {
			case "--target":
				req.Target = v
			case "--commit":
				req.Commit = v
			default:
				t.Fatalf("the notice names a flag %s the command does not take", flags[i])
			}
		}
		got, err := b.Land(ctx, c.r.ID, req)
		if err != nil {
			t.Errorf("task %s: %q was refused: %v", c.r.ID, m[0], err)
			continue
		}
		if string(got.Landing.State) != m[2] {
			t.Errorf("task %s: recorded %s, want %s", c.r.ID, got.Landing.State, m[2])
		}
	}
}
