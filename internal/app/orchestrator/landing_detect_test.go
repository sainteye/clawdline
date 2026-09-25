package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The broker records a landing once the branch is merged (landing_detect.go).
//
// Every case is a real repository in a temporary directory: the claim is
// about what git answers, and a fake git would only answer what the test
// already believed.

// deliveredTask is a finished isolated task whose branch carries one commit,
// with its pending landing as the settlement wrote it.
func deliveredTask(t *testing.T, b *Broker, ctx context.Context, repo, id string) Record {
	t.Helper()
	r := checkedOutTask(t, b, ctx, repo, id)
	commitFile(t, r.Worktree.Path, "delivery-"+id[len(id)-2:]+".go", "package a\n")
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Landing == nil || settled.Landing.State != LandingPending {
		t.Fatalf("landing after settle = %+v, want pending", settled.Landing)
	}
	return settled
}

func landingOf(t *testing.T, b *Broker, ctx context.Context, id string) Landing {
	t.Helper()
	r, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Landing == nil {
		t.Fatalf("task %s has no landing", id)
	}
	return *r.Landing
}

// ① A merged branch is recorded landed, on the target it was merged into, at
// the commit the target stood at — with the proof the route writes.
func TestAMergedDeliveryIsRecordedLandedByTheBeat(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000001"
	r := deliveredTask(t, b, ctx, repo, id)
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge the delivery", r.Worktree.Branch)
	tip := gitIn(t, repo, "rev-parse", "main")
	head := gitIn(t, repo, "rev-parse", r.Worktree.Branch)

	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("detectLandings recorded %d, want 1", n)
	}
	l := landingOf(t, b, ctx, id)
	if l.State != LandingLanded || l.Target != "main" || l.Commit != tip {
		t.Fatalf("landing = %s on %q at %s, want landed on main at %s", l.State, l.Target, l.Commit, tip)
	}
	if l.DeliveryHead != head || l.Base != r.Worktree.Base || l.TargetCommit != tip {
		t.Errorf("proof = head %s base %s target %s; want %s %s %s",
			l.DeliveryHead, l.Base, l.TargetCommit, head, r.Worktree.Base, tip)
	}
	if l.Note != landingDetectNote {
		t.Errorf("note = %q, want the broker to say it recorded this", l.Note)
	}
	// The broker noticing a merge is not the root reading the delivery.
	after, _, _ := b.Record(ctx, id)
	if after.Notice != nil && after.Notice.State == NoticeAcknowledged {
		t.Errorf("the detector acknowledged the root's completion notice")
	}
}

// ② A target the root named is the one asked about, even when another branch
// holds the delivery too.
func TestADetectedLandingUsesTheTargetTheRootNamed(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000002"
	r := deliveredTask(t, b, ctx, repo, id)
	gitIn(t, repo, "branch", "release", "main")
	if _, err := b.Land(ctx, id, LandingRequest{State: string(LandingPending), Target: "release", Machine: true}); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "merge", "-q", "--ff-only", r.Worktree.Branch) // main
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("recorded %d landings while the named target does not hold the delivery", n)
	}
	gitIn(t, repo, "branch", "-f", "release", "main")
	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("recorded %d, want 1 once release holds it", n)
	}
	if l := landingOf(t, b, ctx, id); l.Target != "release" {
		t.Fatalf("target = %q, want the root's release", l.Target)
	}
}

// ③ Unmerged, cherry-picked, empty: pending, untouched.
func TestADeliveryThatIsNotOnItsTargetStaysPending(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	unmerged := deliveredTask(t, b, ctx, repo, "d0000000-0000-4000-8000-000000000003")
	picked := deliveredTask(t, b, ctx, repo, "d0000000-0000-4000-8000-000000000004")
	empty := checkedOutTask(t, b, ctx, repo, "d0000000-0000-4000-8000-000000000005")
	if _, err := b.Settle(ctx, empty.ID, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	// Another commit on main first: a pick onto the very parent its original
	// had, in the same second, is the same commit, not a copy.
	commitFile(t, repo, "between.go", "package a\n")
	gitIn(t, repo, "cherry-pick", picked.Worktree.Branch)

	before := map[string]Landing{}
	for _, id := range []string{unmerged.ID, picked.ID, empty.ID} {
		before[id] = landingOf(t, b, ctx, id)
	}
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("recorded %d landings; none of these is on its target", n)
	}
	for id, was := range before {
		if now := landingOf(t, b, ctx, id); !now.sameAs(was) || now.State != LandingPending {
			t.Errorf("task %s: landing moved from %+v to %+v", id, was, now)
		}
	}
	// None of these is a failure: each is a root's decision, not a log line.
	if said := b.landingDetect.said; said != 0 {
		t.Errorf("the detector logged %d line(s) about deliveries it should simply leave: %v",
			said, b.landingDetect.logged)
	}
}

// ④ A branch somebody is still integrating in a linked checkout is not a
// target, and two candidate branches are a root's decision.
func TestTheBrokerNamesATargetOnlyWhenThereIsOne(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000006"
	r := deliveredTask(t, b, ctx, repo, id)

	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	integration := filepath.Join(scratch, "integrate")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "integrate", integration, "main")
	gitIn(t, integration, "merge", "-q", "--no-ff", "-m", "integrate", r.Worktree.Branch)
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("recorded %d; the only branch holding it is an integration in progress", n)
	}

	gitIn(t, repo, "branch", "other", "integrate")
	gitIn(t, repo, "merge", "-q", "--ff-only", "integrate") // main
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("recorded %d; main and other both hold it", n)
	}
	gitIn(t, repo, "branch", "-D", "other")
	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("recorded %d, want 1 once main is the only candidate", n)
	}
	if l := landingOf(t, b, ctx, id); l.Target != "main" {
		t.Fatalf("target = %q, want main", l.Target)
	}
}

// A parked integration branch that nobody has checked out is not a target,
// even when it is the only branch holding the delivery: the primary
// checkout's branch is the line work lands on.
func TestAParkedIntegrationBranchIsNotNamedTheTarget(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000016"
	r := deliveredTask(t, b, ctx, repo, id)

	gitIn(t, repo, "branch", "parked", "main")
	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	park := filepath.Join(scratch, "park")
	gitIn(t, repo, "worktree", "add", "-q", park, "parked")
	gitIn(t, park, "merge", "-q", "--no-ff", "-m", "park", r.Worktree.Branch)
	gitIn(t, repo, "worktree", "remove", park)
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("recorded %d; the only holder is a parked branch, not main", n)
	}
	gitIn(t, repo, "merge", "-q", "--ff-only", "parked") // main
	gitIn(t, repo, "branch", "-D", "parked")
	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("recorded %d, want 1 once main holds it", n)
	}
}

// ⑤ One repository that cannot be read does not stop the others, and is said
// once, not on every look.
func TestAnUnreadableRepositoryDoesNotStopTheOthers(t *testing.T) {
	b, ctx := newTestBroker(t)
	broken := gitRepo(t)
	good := gitRepo(t)
	// Sorted first, so the broken one is met before the good one.
	bad := deliveredTask(t, b, ctx, broken, "d0000000-0000-4000-8000-000000000007")
	ok := deliveredTask(t, b, ctx, good, "d0000000-0000-4000-8000-000000000008")
	gitIn(t, good, "merge", "-q", "--no-ff", "-m", "merge", ok.Worktree.Branch)
	if err := os.RemoveAll(filepath.Join(broken, ".git")); err != nil {
		t.Fatal(err)
	}

	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("recorded %d, want the readable repository's one", n)
	}
	if l := landingOf(t, b, ctx, ok.ID); l.State != LandingLanded {
		t.Fatalf("the readable task is %s", l.State)
	}
	if l := landingOf(t, b, ctx, bad.ID); l.State != LandingPending {
		t.Fatalf("the unreadable task is %s, want pending", l.State)
	}
	b.landingDetect.mu.Lock()
	said := b.landingDetect.logged[bad.ID]
	b.landingDetect.mu.Unlock()
	if said == "" {
		t.Fatalf("the unreadable repository was not said")
	}
	// Said once: the same failure on the next looks is not a new line.
	for i := 0; i < 3; i++ {
		b.detectLandings(ctx)
	}
	b.landingDetect.mu.Lock()
	lines := b.landingDetect.said
	b.landingDetect.mu.Unlock()
	if lines != 1 {
		t.Fatalf("the same failure was said %d times over four looks, want 1", lines)
	}
}

// ⑥ A second look changes nothing: not the record, not the event log.
func TestASecondLookChangesNothing(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000009"
	r := deliveredTask(t, b, ctx, repo, id)
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", r.Worktree.Branch)
	if n := b.detectLandings(ctx); n != 1 {
		t.Fatalf("first look recorded %d, want 1", n)
	}
	first := landingOf(t, b, ctx, id)
	row, err := b.Store.BrokerTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// The target moves on; the landing already stands on its proof.
	commitFile(t, repo, "later.go", "package a\n")
	if n := b.detectLandings(ctx); n != 0 {
		t.Fatalf("second look recorded %d, want 0", n)
	}
	again, err := b.Store.BrokerTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != row.Version {
		t.Fatalf("the record was written again: version %d -> %d", row.Version, again.Version)
	}
	if now := landingOf(t, b, ctx, id); !now.sameAs(first) || !now.At.Equal(first.At) {
		t.Fatalf("landing changed: %+v -> %+v", first, now)
	}
}

// ⑦ One look examines at most landingDetectLimit tasks, and the next starts
// where it stopped, so every row is reached.
func TestALookIsBoundedAndTheNextOneCarriesOn(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	total := landingDetectLimit + 3
	ids := make([]string, total)
	for i := range ids {
		ids[i] = fmt.Sprintf("d1000000-0000-4000-8000-%012d", i)
		r := deliveredTask(t, b, ctx, repo, ids[i])
		gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+ids[i], r.Worktree.Branch)
	}
	if n := b.detectLandings(ctx); n != landingDetectLimit {
		t.Fatalf("first look recorded %d, want the limit %d", n, landingDetectLimit)
	}
	if n := b.detectLandings(ctx); n != total-landingDetectLimit {
		t.Fatalf("second look recorded %d, want the remaining %d", n, total-landingDetectLimit)
	}
	for _, id := range ids {
		if l := landingOf(t, b, ctx, id); l.State != LandingLanded {
			t.Errorf("task %s is %s after two looks", id, l.State)
		}
	}
}

// ⑧ The beat runs it: the first pass, and then one pass in landingDetectEvery.
func TestTheBeatLooksForMergedDeliveries(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "d0000000-0000-4000-8000-000000000010"
	r := deliveredTask(t, b, ctx, repo, id)
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge", r.Worktree.Branch)
	if n := b.detectLandingsDue(ctx, 2); n != 0 {
		t.Fatalf("a pass that is not due recorded %d", n)
	}
	p := b.Pass(ctx)
	if p.Landed != 1 {
		t.Fatalf("the first pass recorded %d landings, want 1", p.Landed)
	}
	if l := landingOf(t, b, ctx, id); l.State != LandingLanded {
		t.Fatalf("landing is %s after the pass", l.State)
	}
}
