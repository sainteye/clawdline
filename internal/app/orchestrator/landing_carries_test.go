package orchestrator

import (
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
)

// A task that cherry-picked another delivery and declared it in `carries` is
// what closes that delivery: when the carrier's own landing is proved, the
// carried one is recorded incorporated by patch-id, and one whose commits are
// not all there stays pending with the reason.
func TestALandedCarrierIncorporatesTheDeliveriesItCarried(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	pickedID := "d3000021-0000-4000-8000-000000000021"
	partialID := "d3000022-0000-4000-8000-000000000022"
	carrierID := "d3000023-0000-4000-8000-000000000023"

	isolatedTask(t, b, ctx, repo, pickedID)
	picked := onBranch(t, repo, BranchName(pickedID), "picked.go")
	isolatedTask(t, b, ctx, repo, partialID)
	partialFirst := onBranch(t, repo, BranchName(partialID), "partial-one.go")
	onBranch(t, repo, BranchName(partialID), "partial-two.go")
	for _, id := range []string{pickedID, partialID} {
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}

	isolatedTask(t, b, ctx, repo, carrierID)
	gitIn(t, repo, "checkout", "-q", BranchName(carrierID))
	// The carrier's own commit first, so a pick is never the same commit
	// object as the original (same parent, same second) and only patch-ids
	// can tell that it is carried.
	commitFile(t, repo, "carrier.go", "carrier\n")
	gitIn(t, repo, "cherry-pick", picked)
	gitIn(t, repo, "cherry-pick", partialFirst) // only one of the two commits
	gitIn(t, repo, "checkout", "-q", "main")
	if _, err := b.Settle(ctx, carrierID, StateSuccess, "integrated", &taskdir.Result{
		Protocol: 1, TaskID: carrierID, Status: "success", Summary: "integrated",
		Carries: []string{pickedID, partialID},
	}); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "integrate", BranchName(carrierID))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	if _, err := land(b, ctx, carrierID, "landed", "main", merge); err != nil {
		t.Fatalf("carrier landing: %v", err)
	}

	got, _, err := b.Record(ctx, pickedID)
	if err != nil {
		t.Fatal(err)
	}
	if l := got.Landing; l == nil || l.State != LandingIncorporated || l.CarrierTask != carrierID || l.Commit != merge ||
		l.Target != "main" || l.DeliveryHead != picked || !strings.Contains(l.Note, "patch-equivalent") {
		t.Fatalf("carried delivery landing = %+v", got.Landing)
	}
	if n := eventCount(t, b.Dir, pickedID, "landing.incorporated"); n != 1 {
		t.Fatalf("landing.incorporated events = %d, want 1", n)
	}

	got, _, err = b.Record(ctx, partialID)
	if err != nil {
		t.Fatal(err)
	}
	if l := got.Landing; l == nil || l.State != LandingPending || !strings.Contains(l.Note, "1 of its 2 commits") || !strings.Contains(l.Note, carrierID) {
		t.Fatalf("partly carried delivery landing = %+v", got.Landing)
	}
}
