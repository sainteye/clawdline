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

func TestInventoryMissingCheckoutReadsUnresolvedBranchesImmediately(t *testing.T) {
	for _, state := range []LandingState{"", LandingPending, "future-review"} {
		name := string(state)
		if name == "" {
			name = "legacy without landing"
		}
		t.Run(name, func(t *testing.T) {
			b, ctx, repo := archivedInventory(t, 1)
			records, _, err := b.Records(ctx)
			if err != nil {
				t.Fatal(err)
			}
			r := records[0]
			r.Worktree.Branch = "restored-delivery"
			if state == "" {
				r.Landing = nil
			} else {
				r.Landing = &Landing{State: state, Target: "main"}
			}
			// A retired record may use the same branch name. Restricting
			// batch inputs alone cannot keep this unresolved row fresh.
			retired := r
			retired.Landing = &Landing{State: LandingLanded, Target: "main"}
			lookup := b.branchPresence([]Record{retired, r})
			if exists, known := lookup(ctx, repo, r.Worktree.Branch); exists || !known {
				t.Fatal("initial branch absence not established")
			}
			gitIn(t, repo, "checkout", "-q", "-b", r.Worktree.Branch)
			gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "restored work")
			gitIn(t, repo, "checkout", "-q", "main")
			now := time.Now()
			got := b.rowWithBranches(ctx, r, now, b.occupancySnapshot(ctx), lookup)
			want := b.row(ctx, r, now, b.occupancySnapshot(ctx))
			if want.Section != VisibilityUnlanded || want.Do != DoLandOrAbandon {
				t.Fatalf("invalid immediate-read control: %+v", want)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("missing checkout changed unresolved work: got %s/%s branched=%v; want %s/%s branched=%v", got.Section, got.Do, got.Branched, want.Section, want.Do, want.Branched)
			}
		})
	}
}

func TestInventoryMissingCheckoutRestoredDuringRequestStaysVisible(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 3)
	now := time.Now()
	b.Clock = func() time.Time { return now }
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, unresolved, retired := records[0], records[1], records[2]
	first.ChildTerminalID, first.ChildBackend = "%77", "tmux"
	unresolved.Landing = nil
	unresolved.Worktree.Branch = "restored-delivery"
	retired.Worktree.Branch = unresolved.Worktree.Branch
	for _, r := range []Record{first, unresolved, retired} {
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: first.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: now}, nil); err != nil {
		t.Fatal(err)
	}
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, complete: true, sources: map[string]bool{"tmux": true}})
	created := false
	b.ProcessCWDs = func(context.Context) ([]string, error) {
		if !created {
			// The first cleanup row has already read the batch. Restore the
			// later legacy task's branch before that task is classified.
			gitIn(t, repo, "checkout", "-q", "-b", unresolved.Worktree.Branch)
			gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "restored work")
			gitIn(t, repo, "checkout", "-q", "main")
			created = true
		}
		return nil, nil
	}
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("fixture did not restore the branch inside the request")
	}
	if len(inv.Unlanded) != 1 || inv.Unlanded[0].Task != unresolved.ID || inv.Unlanded[0].Do != DoLandOrAbandon {
		t.Fatalf("restored unresolved task disappeared: unlanded=%+v", inv.Unlanded)
	}
	fresh, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(InventoryPayload(inv), InventoryPayload(fresh)) {
		t.Fatal("same-request restoration differs from a fresh inventory or generation")
	}
	inflight, err := b.Inflight(ctx, repo, "")
	if err != nil || len(inflight) != 1 || inflight[0].Task != unresolved.ID {
		t.Fatalf("restored task missing from inflight: rows=%+v err=%v", inflight, err)
	}
}

// A delivery settled as incorporated, nothing_to_land or abandoned is as
// finished as a landed one. Its clean checkout must be offered for disposal,
// named by its settlement, instead of vanishing from the inventory and leaving
// tools/check-worktrees.sh to call it unknown. Work the checkout still holds
// keeps it: uncommitted content, commits past what was settled, or a carrier
// landing that no longer stands.
func TestInventoryOffersSettledCleanCheckouts(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1", Assistant: session.AssistantClaude}},
		sources: map[string]bool{"tmux": true}, complete: true})
	b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }

	// checkout dispatches an isolated task, gives it a checkout on disk,
	// optionally commits one file in it, and settles it with its tab gone.
	checkout := func(id, file string) Record {
		t.Helper()
		r := isolatedTask(t, b, ctx, repo, id)
		gitIn(t, repo, "worktree", "add", "-q", r.Worktree.Path, BranchName(id))
		if file != "" {
			commitFile(t, r.Worktree.Path, file, file+"\n")
		}
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error {
			r.ChildTerminalID, r.ChildBackend = "%77", "tmux"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	settle := func(id string, req LandingRequest) {
		t.Helper()
		req.Machine = true
		if _, err := b.Land(ctx, id, req); err != nil {
			t.Fatalf("%s %s: %v", id, req.State, err)
		}
	}

	incorporated := checkout("e1000001-0000-4000-8000-000000000001", "original.go")
	carrier := checkout("e1000002-0000-4000-8000-000000000002", "carrier.go")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "integrate", BranchName(carrier.ID))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	settle(carrier.ID, LandingRequest{State: "landed", Target: "main", Commit: merge})
	settle(incorporated.ID, LandingRequest{State: "incorporated", Target: "main", Commit: merge,
		CarrierTask: carrier.ID, Note: "the carrier resolved both deliveries"})

	nothing := checkout("e1000003-0000-4000-8000-000000000003", "")
	settle(nothing.ID, LandingRequest{State: "nothing_to_land"})
	abandoned := checkout("e1000004-0000-4000-8000-000000000004", "")
	settle(abandoned.ID, LandingRequest{State: "abandoned", Note: "not wanted"})
	abandonedWork := checkout("e1000005-0000-4000-8000-000000000005", "kept.go")
	settle(abandonedWork.ID, LandingRequest{State: "abandoned", Note: "not wanted"})
	abandonedDirty := checkout("e1000006-0000-4000-8000-000000000006", "")
	settle(abandonedDirty.ID, LandingRequest{State: "abandoned", Note: "not wanted"})
	if err := os.WriteFile(filepath.Join(abandonedDirty.Worktree.Path, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	droppable := func() map[string]map[string]any {
		t.Helper()
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]map[string]any{}
		for _, item := range InventoryPayload(inv)["droppable"].([]map[string]any) {
			out[item["task"].(string)] = item
		}
		return out
	}
	got := droppable()
	for _, want := range []struct {
		id, settlement, why string
	}{
		{incorporated.ID, "incorporated", "carried_by_landing"},
		{carrier.ID, "landed", WhyMergedClean},
		{nothing.ID, "nothing_to_land", WhyBranchEmpty},
		{abandoned.ID, "abandoned", WhyBranchEmpty},
	} {
		item, ok := got[want.id]
		if !ok {
			t.Errorf("%s: a clean %s checkout is not droppable", want.id, want.settlement)
			continue
		}
		if item["settlement"] != want.settlement || item["why"] != want.why || item["path"] == nil {
			t.Errorf("%s: settlement=%v why=%v path=%v, want %s/%s", want.id, item["settlement"], item["why"], item["path"], want.settlement, want.why)
		}
	}
	for id, reason := range map[string]string{
		abandonedWork.ID:  "an abandoned checkout with commits nobody landed",
		abandonedDirty.ID: "an abandoned checkout with uncommitted content",
	} {
		if _, ok := got[id]; ok {
			t.Errorf("%s is droppable: %s", id, reason)
		}
	}

	// New work after the settlement keeps the checkout.
	gitIn(t, nothing.Worktree.Path, "commit", "-q", "--allow-empty", "-m", "later")
	gitIn(t, incorporated.Worktree.Path, "commit", "-q", "--allow-empty", "-m", "later")
	got = droppable()
	for _, id := range []string{nothing.ID, incorporated.ID} {
		if _, ok := got[id]; ok {
			t.Errorf("%s is droppable with a commit made after its settlement", id)
		}
	}
	gitIn(t, incorporated.Worktree.Path, "reset", "-q", "--hard", "HEAD~1")
	if _, ok := droppable()[incorporated.ID]; !ok {
		t.Fatal("control: the incorporated checkout did not return once the later commit was gone")
	}
	// An incorporation is only as good as its carrier's landing.
	if _, err := b.mutate(ctx, carrier.ID, "task.test", func(r *Record) error {
		r.Landing.State = LandingPending
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := droppable()[incorporated.ID]; ok {
		t.Fatal("an incorporated checkout is droppable while its carrier's landing is pending")
	}
}
