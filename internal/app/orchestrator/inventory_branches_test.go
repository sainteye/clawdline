package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestInventoryBatchesBranchPresence(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 32)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range records {
		r.Worktree.Branch = fmt.Sprintf("removed-delivery-%d", i)
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Live)+len(inv.Unlanded)+len(inv.Droppable)+len(inv.Unreadable) != 0 {
		t.Fatal("removed branches became actionable")
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct{ Event string }
		if json.Unmarshal(line, &event) == nil && event.Event == "start" {
			commands++
		}
	}
	// One repository resolution and one branch read, independent of the
	// number of retired deliveries whose clean removal was recorded.
	if commands != 2 {
		t.Fatalf("inventory started %d Git processes, want 2", commands)
	}
}

func TestInventoryBranchReadingIsFreshAndMatchesIndividualReads(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 1)
	b.Clock = func() time.Time { return time.Unix(1_800_000_000, 0) }
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := records[0]
	r.Worktree.Branch = "delivery"
	r.ChildTerminalID, r.ChildBackend = "%77", "tmux"
	if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, complete: true, sources: map[string]bool{"tmux": true}})
	b.ProcessCWDs = func(_ context.Context) ([]string, error) { return nil, nil }
	read := func(want int) Inventory {
		t.Helper()
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil || len(inv.Droppable) != want {
			t.Fatalf("droppable=%d want=%d err=%v", len(inv.Droppable), want, err)
		}
		if want != 0 {
			individual := b.row(ctx, r, inv.At, b.occupancySnapshot(ctx))
			if !reflect.DeepEqual(inv.Droppable[0], individual) {
				t.Fatal("batch changed the individual row's evidence")
			}
		}
		return inv
	}
	missing := read(0)
	gitIn(t, repo, "branch", "delivery")
	created := read(1)
	if missing.Generation == created.Generation {
		t.Fatal("new branch was hidden by a previous read")
	}
	gitIn(t, repo, "checkout", "-q", "delivery")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "delivery")
	advanced := read(0)
	if advanced.Generation == created.Generation {
		t.Fatal("unmerged work retained stale disposal eligibility")
	}
}

func TestInventoryBranchBatchFailureFallsBackToIndividualEvidence(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	// Exceed the platform's argument limit so the batch cannot start. An
	// unrelated malformed legacy row must not hide this readable branch.
	lookup := b.branchPresence([]Record{
		{State: StateSuccess, Worktree: &Worktree{Repository: repo, Branch: strings.Repeat("x", 1<<20)}},
		{State: StateSuccess, Worktree: &Worktree{Repository: repo, Branch: "main"}},
	})
	if exists, known := lookup(ctx, repo, "main"); !exists || !known {
		t.Fatal("failed batch poisoned a readable branch")
	}
	if exists, known := lookup(ctx, repo, "missing"); exists || !known {
		t.Fatal("failed batch changed an individual missing-branch result")
	}
}

func TestInventoryBranchBatchKeepsRepositoryAndLegacySemantics(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 1)
	other := gitRepo(t)
	gitIn(t, repo, "branch", "topic/leaf")
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	records[0].Landing = &Landing{State: LandingPending, Target: "main"}
	var selected []Record
	for _, branch := range []string{"main", "topic", "topic/leaf", "missing", "m*", "topic/", ""} {
		r := records[0]
		w := *r.Worktree
		w.Branch = branch
		r.Worktree = &w
		selected = append(selected, r)
	}
	lookup := b.branchPresence(append(selected, Record{State: StateSuccess, Worktree: &Worktree{Repository: other, Branch: "topic/leaf"}}))
	if exists, known := lookup(ctx, other, "topic/leaf"); exists || !known {
		t.Fatal("branch presence crossed repository boundaries")
	}
	now := time.Now()
	for _, r := range selected {
		got := b.rowWithBranches(ctx, r, now, b.occupancySnapshot(ctx), lookup)
		want := b.row(ctx, r, now, b.occupancySnapshot(ctx))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("branch %q changed inventory evidence", r.Worktree.Branch)
		}
	}
}

func TestInventoryExistingCheckoutDoesNotUseEarlierBranchAbsence(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 1)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := records[0]
	r.Worktree.Branch, r.Worktree.Path = "delivery", repo
	r.ChildTerminalID, r.ChildBackend = "%77", "tmux"
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, complete: true, sources: map[string]bool{"tmux": true}})
	b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
	lookup := b.branchPresence([]Record{r})
	if exists, known := lookup(ctx, repo, "delivery"); exists || !known {
		t.Fatal("expected the initial branch to be absent")
	}
	// Another writer creates unmerged work after the batch was read, before
	// this existing checkout is classified. It must not become an orphan.
	gitIn(t, repo, "checkout", "-q", "-b", "delivery")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "new delivery")
	gitIn(t, repo, "checkout", "-q", "main")
	now := time.Now()
	got := b.rowWithBranches(ctx, r, now, b.occupancySnapshot(ctx), lookup)
	want := b.row(ctx, r, now, b.occupancySnapshot(ctx))
	if got.Do == DoDispose || !reflect.DeepEqual(got, want) {
		t.Fatal("earlier branch absence changed an existing checkout's disposal evidence")
	}
}
