package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two settlements git can prove without anybody's record (landing_detect.go,
// landing_carries.go): a task whose tab never opened has nothing to land, and
// a delivery that reached its target as patch-equivalent commits it still
// keeps is incorporated. Real repositories, as in landing_detect_test.go.

// A task that failed before its tab opened, with nothing on its branch and a
// clean checkout, is recorded nothing_to_land as it ends — though it declared
// paths to write — and says its tab never opened.
func TestASpawnFailedTaskWhoseTabNeverOpenedHasNothingToLand(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000001-0000-4000-8000-000000000001"
	checkedOutTask(t, b, ctx, repo, id) // declares a.go
	settled, err := b.Settle(ctx, id, StateSpawnFailed, "The daemon that admitted this task stopped before it opened the tab.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if l := settled.Landing; l == nil || l.State != LandingNothingToLand || l.Note != landingUnopenedNote {
		t.Fatalf("landing = %+v, want nothing_to_land with the never-opened note", l)
	}
}

// One whose tab opened stays pending: somebody may have typed in it.
func TestASpawnFailedTaskWhoseTabOpenedStillOwesItsLanding(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000002-0000-4000-8000-000000000002"
	r := checkedOutTask(t, b, ctx, repo, id)
	r.ChildTerminalID = "%7"
	if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, id, StateSpawnFailed, "held a dialog", nil); err != nil {
		t.Fatal(err)
	}
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("detectLandings recorded %d", n)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("landing = %+v, want pending", l)
	}
}

// A never-opened task already recorded pending — every one settled before
// this — is settled by the beat once its checkout is clean, and not while it
// holds a change.
func TestTheBeatSettlesANeverOpenedTaskOnceItsCheckoutIsClean(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000003-0000-4000-8000-000000000003"
	r := checkedOutTask(t, b, ctx, repo, id)
	stray := filepath.Join(r.Worktree.Path, "stray.go")
	if err := os.WriteFile(stray, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, id, StateSpawnFailed, "stopped before the tab", nil); err != nil {
		t.Fatal(err)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("landing with a change in the checkout = %+v, want pending", l)
	}
	b.detectLandings(ctx)
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("beat over a dirty checkout: landing = %+v, want pending", l)
	}

	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	b.detectLandings(ctx)
	if l := landingOf(t, b, ctx, id); l.State != LandingNothingToLand || l.Note != landingUnopenedNote {
		t.Fatalf("beat over a clean checkout: landing = %+v, want nothing_to_land", l)
	}
}

// A delivery cherry-picked onto main, with no carrier declaring it, is
// recorded incorporated by the beat, and the note names the commit it is on
// main as.
func TestACherryPickedDeliveryIsRecordedIncorporated(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000004-0000-4000-8000-000000000004"
	r := deliveredTask(t, b, ctx, repo, id)
	// Another commit on main first: a pick onto the very parent its original
	// had, in the same second, is the same commit, not a copy.
	commitFile(t, repo, "between.go", "package a\n")
	gitIn(t, repo, "cherry-pick", r.Worktree.Branch)
	picked := gitIn(t, repo, "rev-parse", "HEAD")
	head := gitIn(t, repo, "rev-parse", r.Worktree.Branch)

	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("detectLandings recorded %d, want 1", n)
	}
	l := landingOf(t, b, ctx, id)
	if l.State != LandingIncorporated || l.Target != "main" || l.Commit != picked || l.CarrierTask != "" {
		t.Fatalf("landing = %s on %q at %s carrier %q, want incorporated on main at %s with no carrier",
			l.State, l.Target, l.Commit, l.CarrierTask, picked)
	}
	if l.DeliveryHead != head || l.Base != r.Worktree.Base {
		t.Errorf("proof = head %s base %s, want %s %s", l.DeliveryHead, l.Base, head, r.Worktree.Base)
	}
	if !strings.Contains(l.Note, shortCommit(picked)) {
		t.Errorf("note = %q, want it to name %s", l.Note, shortCommit(picked))
	}
	if said := b.landingDetect.said; said != 0 {
		t.Errorf("the detector logged %d line(s): %v", said, b.landingDetect.logged)
	}
}

// A delivery cherry-picked and then reverted is still found by git cherry
// upstream of the revert; the trees refuse it, and it stays pending, quietly.
func TestACherryPickedThenRevertedDeliveryStaysPending(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000005-0000-4000-8000-000000000005"
	r := deliveredTask(t, b, ctx, repo, id)
	commitFile(t, repo, "between.go", "package a\n")
	gitIn(t, repo, "cherry-pick", r.Worktree.Branch)
	gitIn(t, repo, "revert", "--no-edit", "HEAD")

	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("detectLandings recorded %d, want 0 for a reverted pick", n)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("landing = %+v, want pending", l)
	}
	if said := b.landingDetect.said; said != 0 {
		t.Errorf("the detector logged %d line(s) about a delivery it should simply leave: %v",
			said, b.landingDetect.logged)
	}
}

// Half of it reverted is not kept: one path back as it was keeps it pending,
// though another path still holds its change.
func TestAPartlyRevertedPickStaysPending(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d4000006-0000-4000-8000-000000000006"
	r := checkedOutTask(t, b, ctx, repo, id)
	if err := os.WriteFile(filepath.Join(r.Worktree.Path, "one.go"), []byte("package one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r.Worktree.Path, "two.go", "package two\n")
	gitIn(t, r.Worktree.Path, "add", "one.go")
	gitIn(t, r.Worktree.Path, "commit", "-q", "--amend", "--no-edit")
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "between.go", "package a\n")
	gitIn(t, repo, "cherry-pick", r.Worktree.Branch)
	gitIn(t, repo, "rm", "-q", "one.go")
	gitIn(t, repo, "commit", "-q", "-m", "take one.go back out")

	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("detectLandings recorded %d, want 0", n)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingPending {
		t.Fatalf("landing = %+v, want pending", l)
	}
}
