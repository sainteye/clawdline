package orchestrator

import (
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
)

// revertedMerge merges a task's branch into main with a merge commit and then
// backs it out with a commit whose message does not use git's own "This
// reverts commit" wording, as main's df6468593 did. It returns the merge and
// the revert.
func revertedMerge(t *testing.T, repo, branch string) (merge, revert string) {
	t.Helper()
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+branch, branch)
	merge = gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "revert", "--no-edit", "-m", "1", merge)
	gitIn(t, repo, "commit", "-q", "--amend", "-m", "Back the merge out of the batch")
	return merge, gitIn(t, repo, "rev-parse", "HEAD")
}

func landingReason(err error) string {
	var refused Refusal
	if !errors.As(err, &refused) {
		return ""
	}
	if reason, ok := refused.Extra["reason"].(string); ok {
		return reason
	}
	return ""
}

// A merge that was backed out on the target is not a landing: every commit
// after the merge descends from it, so ancestry alone proves it landed on the
// revert commit itself, which is how four tasks came to read landed on
// df6468593.
func TestARevertedMergeIsNotProvedLanded(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000031-0000-4000-8000-000000000031"
	isolatedTask(t, b, ctx, repo, id)
	onBranch(t, repo, BranchName(id), "reverted.go")
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	merge, revert := revertedMerge(t, repo, BranchName(id))

	for _, commit := range []string{revert, merge} {
		_, err := land(b, ctx, id, "landed", "main", commit)
		if got := landingReason(err); got != UnverifiedReverted {
			t.Fatalf("landing on %s: err = %v (reason %q), want %s", commit[:9], err, got, UnverifiedReverted)
		}
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("landing = %+v, want pending", l)
	}

	// Landed again after the revert, the delivery is on the target and the
	// proof accepts the re-merge.
	gitIn(t, repo, "cherry-pick", "-m", "1", merge)
	again := gitIn(t, repo, "rev-parse", "HEAD")
	if _, err := land(b, ctx, id, "landed", "main", again); err != nil {
		t.Fatalf("landing the re-applied delivery: %v", err)
	}
}

// A later change elsewhere, or to the same file, does not make a delivery read
// as reverted: only a target tree that lacks every change the delivery made
// refuses.
func TestALaterEditDoesNotReadAsARevert(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000032-0000-4000-8000-000000000032"
	isolatedTask(t, b, ctx, repo, id)
	onBranch(t, repo, BranchName(id), "kept.go")
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", BranchName(id))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	commitFile(t, repo, "kept.go", "kept.go\nedited later\n")
	commitFile(t, repo, "other.go", "other\n")
	if _, err := land(b, ctx, id, "landed", "main", merge); err != nil {
		t.Fatalf("landing a delivery later edited on the target: %v", err)
	}
}

// A delivery carried by a merge that was then reverted is not incorporated,
// even though git cherry still finds its patch upstream of the revert.
func TestARevertedCarrierDoesNotIncorporate(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	pickedID := "d3000033-0000-4000-8000-000000000033"
	carrierID := "d3000034-0000-4000-8000-000000000034"
	isolatedTask(t, b, ctx, repo, pickedID)
	picked := onBranch(t, repo, BranchName(pickedID), "carried.go")
	if _, err := b.Settle(ctx, pickedID, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	isolatedTask(t, b, ctx, repo, carrierID)
	gitIn(t, repo, "checkout", "-q", BranchName(carrierID))
	commitFile(t, repo, "carrier.go", "carrier\n")
	gitIn(t, repo, "cherry-pick", picked)
	gitIn(t, repo, "checkout", "-q", "main")
	if _, err := b.Settle(ctx, carrierID, StateSuccess, "integrated", &taskdir.Result{
		Protocol: 1, TaskID: carrierID, Status: "success", Summary: "integrated", Carries: []string{pickedID},
	}); err != nil {
		t.Fatal(err)
	}
	_, revert := revertedMerge(t, repo, BranchName(carrierID))
	if _, err := land(b, ctx, carrierID, "landed", "main", revert); landingReason(err) != UnverifiedReverted {
		t.Fatalf("carrier landing on its revert: %v", err)
	}
	if l := landingOf(t, b, ctx, pickedID); l.State != LandingPending {
		t.Fatalf("carried landing = %+v, want pending", l)
	}
}

// A landing recorded before the proof could see a revert is corrected back
// to pending by the orchestrator token alone, with a note, keeping what it
// replaced; it can then be proved again when the work really lands.
func TestAWrongLandingIsCorrectedBackToPending(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d3000035-0000-4000-8000-000000000035"
	isolatedTask(t, b, ctx, repo, id)
	onBranch(t, repo, BranchName(id), "wrong.go")
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", BranchName(id))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	if _, err := land(b, ctx, id, "landed", "main", merge); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "revert", "--no-edit", "-m", "1", merge)

	if _, err := b.Land(ctx, id, LandingRequest{State: "pending", Note: "reverted on main"}); err == nil {
		t.Fatal("the task secret's absence was accepted for a correction to pending")
	}
	if _, err := b.Land(ctx, id, LandingRequest{State: "pending", Machine: true}); err == nil {
		t.Fatal("a correction to pending without a note was accepted")
	}
	got, err := b.Land(ctx, id, LandingRequest{State: "pending", Machine: true, Note: "reverted on main"})
	if err != nil {
		t.Fatalf("correction to pending: %v", err)
	}
	l := got.Landing
	if l == nil || l.State != LandingPending || l.Target != "main" || l.Commit != "" || l.Note != "reverted on main" ||
		l.CorrectedFrom == nil || l.CorrectedFrom.State != LandingLanded || l.CorrectedFrom.Commit != merge {
		t.Fatalf("corrected landing = %+v", l)
	}
	if n := eventCount(t, b.Dir, id, "landing.corrected"); n != 1 {
		t.Fatalf("landing.corrected events = %d, want 1", n)
	}
	// A resend is a replay, not a second correction.
	if _, err := b.Land(ctx, id, LandingRequest{State: "pending", Machine: true, Note: "reverted on main"}); err != nil {
		t.Fatal(err)
	}
	if n := eventCount(t, b.Dir, id, "landing.corrected"); n != 1 {
		t.Fatalf("landing.corrected events after replay = %d, want 1", n)
	}
}

// A carrier that landed honestly does not lend that landing to a delivery
// recorded incorporated after the carrier's merge was reverted.
func TestIncorporatedIsRefusedOnceTheCarrierWasReverted(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	pickedID := "d3000036-0000-4000-8000-000000000036"
	carrierID := "d3000037-0000-4000-8000-000000000037"
	isolatedTask(t, b, ctx, repo, pickedID)
	picked := onBranch(t, repo, BranchName(pickedID), "late.go")
	isolatedTask(t, b, ctx, repo, carrierID)
	gitIn(t, repo, "checkout", "-q", BranchName(carrierID))
	commitFile(t, repo, "carrier.go", "carrier\n")
	gitIn(t, repo, "cherry-pick", picked)
	gitIn(t, repo, "checkout", "-q", "main")
	if _, err := b.Settle(ctx, carrierID, StateSuccess, "integrated", nil); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "integrate", BranchName(carrierID))
	merge := gitIn(t, repo, "rev-parse", "HEAD")
	if _, err := land(b, ctx, carrierID, "landed", "main", merge); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "revert", "--no-edit", "-m", "1", merge)
	if _, err := b.Settle(ctx, pickedID, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	_, err := b.Land(ctx, pickedID, LandingRequest{State: "incorporated", Target: "main", Commit: merge,
		CarrierTask: carrierID, Note: "carried", Machine: true})
	if got := landingReason(err); got != UnverifiedReverted {
		t.Fatalf("incorporated after the carrier's revert: err = %v (reason %q)", err, got)
	}
}
