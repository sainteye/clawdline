package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What a delivery branch held the moment its task ended, said at that moment
// (work-system-review §2.3 W1-a, §3.2 G2).
//
// The shape this answers is one day's measurement: sixteen deliveries already
// merged into master, every one refused `unverified_landing /
// nothing_delivered` because no child had committed to its own branch, and
// every one of them told nothing at the one moment somebody could still have
// committed. The broker had asked git at that moment and thrown the answer
// away.

// checkedOutTask stores the record a worktree dispatch writes, with the real
// checkout beside it: a branch cut from the repository's head, standing at it.
func checkedOutTask(t *testing.T, b *Broker, ctx context.Context, repo, id string) Record {
	t.Helper()
	base, err := b.Git.ResolveCommit(ctx, repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, id)
	if err := b.Git.AddWorktree(ctx, repo, path, BranchName(id), base); err != nil {
		t.Fatal(err)
	}
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "isolated", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"}, LeaseScope: LeaseWorktree,
		Isolation: IsolationWorktree, ProjectDir: repo, Repository: repo, TimeoutMinutes: 240,
		Root:     &RootRef{SessionID: rootConversation, Assistant: "claude"},
		Worktree: &Worktree{Repository: repo, Path: path, Branch: BranchName(id), Base: base, Head: base}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	return r
}

// ① A child that committed nothing is told so, in the one moment its checkout
// is still there — and the fact is on the landing record, not only in the
// directory that is about to be swept.
//
// Before: the landing said `not yet on its target` and the line said "mark
// landing pending so other roots can see it", which asked for the state the
// broker had just written itself.
func TestAnEmptyDeliveryBranchIsRecordedAndSaidWhileItCanStillBeFixed(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "f0000000-0000-4000-8000-000000000001"
	r := checkedOutTask(t, b, ctx, repo, id)

	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Landing == nil || settled.Landing.State != LandingPending {
		t.Fatalf("landing = %+v, want a pending one", settled.Landing)
	}
	if settled.Landing.Settlement != SettlementEmpty {
		t.Fatalf("settlement = %q, want %q: the branch stood at its base and the broker had just read it",
			settled.Landing.Settlement, SettlementEmpty)
	}
	if !strings.Contains(settled.Landing.Note, "nothing was committed") {
		t.Errorf("the landing note says %q, which does not say what the branch held", settled.Landing.Note)
	}

	line := b.FinishedLine(settled, "n1")
	for _, want := range []string{"nothing is committed", r.Worktree.Branch, r.Worktree.Path, "abandoned"} {
		if !strings.Contains(line, want) {
			t.Errorf("the completion line does not say %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "mark landing pending") {
		t.Errorf("the completion line still asks for the state the broker just wrote:\n%s", line)
	}

	// The evidence outlives the checkout. This is the whole point of keeping
	// it: four hours later the directory is gone and the branch alone cannot
	// say whether anything was ever there to commit.
	if err := os.RemoveAll(r.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	after, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Landing.Settlement != SettlementEmpty {
		t.Fatalf("after the checkout went, the record says %q", after.Landing.Settlement)
	}
}

// ② A delivery that was committed is not reported as empty, and is not told
// to go and commit. The same line, the opposite advice.
func TestACommittedDeliveryIsNotCalledEmpty(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "f0000000-0000-4000-8000-000000000002"
	r := checkedOutTask(t, b, ctx, repo, id)
	gitIn(t, r.Worktree.Path, "commit", "-q", "--allow-empty", "-m", "the delivery")

	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Landing.Settlement != SettlementCarried {
		t.Fatalf("settlement = %q, want %q", settled.Landing.Settlement, SettlementCarried)
	}
	line := b.FinishedLine(settled, "n1")
	if !strings.Contains(line, "committed on branch "+r.Worktree.Branch) {
		t.Errorf("the completion line does not name the branch to merge:\n%s", line)
	}
	for _, unwanted := range []string{"nothing is committed", "commit there"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("a delivery that was committed was told %q:\n%s", unwanted, line)
		}
	}
}

// ③ A branch git could not count is neither empty nor delivered. Unknown is
// the third answer and the line says to go and look.
func TestABranchThatCouldNotBeCountedIsNotCalledEmpty(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "f0000000-0000-4000-8000-000000000003"
	r := checkedOutTask(t, b, ctx, repo, id)
	// A repository that is not there is the shape this Mac actually meets:
	// a checkout root somebody moved, a disk that did not mount.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Landing.Settlement != SettlementUnreadable {
		t.Fatalf("settlement = %q, want %q", settled.Landing.Settlement, SettlementUnreadable)
	}
	if line := b.FinishedLine(settled, "n1"); !strings.Contains(line, "could not be read") ||
		!strings.Contains(line, r.Worktree.Branch) {
		t.Errorf("the completion line does not say the branch could not be read:\n%s", line)
	}
}

// ④ A task that wrote the shared checkout has no branch, and is told the one
// thing it can be told: record the landing, or abandon it. Never the empty
// action.
func TestASharedCheckoutDeliveryIsToldWhatToRecord(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "f0000000-0000-4000-8000-000000000004"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "shared", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"}, LeaseScope: LeaseShared,
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Landing.Settlement != "" {
		t.Fatalf("a task with no branch answered %q; nobody asked it anything", settled.Landing.Settlement)
	}
	line := b.FinishedLine(settled, "n1")
	if strings.Contains(line, "mark landing pending") {
		t.Errorf("the completion line still asks for an action already done:\n%s", line)
	}
	if !strings.Contains(line, "record the landing") {
		t.Errorf("the completion line does not say what to record:\n%s", line)
	}
}

// ⑤ A landing written over the settlement keeps it. The record of what the
// branch held is the broker's reading of a moment that has passed, not a
// claim the caller is making, so recording `pending` again does not erase it.
func TestALandingWriteKeepsWhatTheBranchHeld(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "f0000000-0000-4000-8000-000000000005"
	checkedOutTask(t, b, ctx, repo, id)
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	after, err := b.Land(ctx, id, LandingRequest{State: string(LandingPending),
		Target: "main", Note: "waiting for review", Machine: true})
	if err != nil {
		t.Fatal(err)
	}
	if after.Landing.Settlement != SettlementEmpty {
		t.Fatalf("settlement after a landing write = %q", after.Landing.Settlement)
	}
	abandoned, err := b.Land(ctx, id, LandingRequest{State: string(LandingAbandoned),
		Note: "not going in", Machine: true})
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.Landing.Settlement != SettlementEmpty {
		t.Fatalf("settlement after abandoning = %q", abandoned.Landing.Settlement)
	}
}

// ⑥ W2 / G2: the advice separates what the branch carries from what the
// checkout holds, because they are asked of two different things and one is
// routinely unknown while the other is exact.
//
// Before: `!commitsKnown || !dirtyKnown` gave one sentence about a missing
// commit count, so nineteen rows whose branches were all readable and all
// counted as 0 were told to go and look at the commit count.
func TestTheAdviceNamesTheFactThatIsActuallyUnknown(t *testing.T) {
	r := Record{Worktree: &Worktree{Branch: "clawdline/task/x", Base: "abc"}, Claims: []string{}}

	_, why := landingAdvice(r, 0, true, false, false, "")
	if strings.Contains(why, "commit count") {
		t.Errorf("a known count of 0 with an unreadable checkout blamed the commit count: %q", why)
	}
	if !strings.Contains(why, "could not read its checkout") {
		t.Errorf("it does not say the checkout is what could not be read: %q", why)
	}

	_, why = landingAdvice(r, 3, true, false, false, "")
	if !strings.Contains(why, "3 commit(s)") {
		t.Errorf("a branch carrying three commits was not said to: %q", why)
	}

	_, why = landingAdvice(r, 0, false, false, true, "")
	if !strings.Contains(why, "could not count") {
		t.Errorf("an uncountable branch does not say so: %q", why)
	}

	do, why := landingAdvice(r, 0, true, false, true, "")
	if do != DoNothingToLand || why != "" {
		t.Errorf("an empty clean checkout answered %q / %q", do, why)
	}

	_, why = landingAdvice(r, 0, true, true, true, "clawdline/kept/x")
	if !strings.Contains(why, "clawdline/kept/x") {
		t.Errorf("a reclaimed checkout does not name the branch its changes were kept on: %q", why)
	}

	// The words changed; what the gate decides did not. `nothing_to_land` says
	// this task wrote nothing, and it is admitted only when both questions
	// were answered and both answers were negative — an unknown is not
	// permission, whichever of the two it is.
	for _, commits := range []int{0, 3} {
		for _, commitsKnown := range []bool{false, true} {
			for _, dirty := range []bool{false, true} {
				for _, dirtyKnown := range []bool{false, true} {
					admitted := deliveryEvidence(commits, commitsKnown, dirty, dirtyKnown, "") == ""
					want := commitsKnown && commits == 0 && dirtyKnown && !dirty
					if admitted != want {
						t.Errorf("commits=%d known=%v dirty=%v known=%v: admitted=%v, want %v",
							commits, commitsKnown, dirty, dirtyKnown, admitted, want)
					}
				}
			}
		}
	}
}

// ⑦ `outstanding` is counted. It was declared and never written, so every
// completion notice this broker sent said `"outstanding": 0` — a field that
// prints "nothing else of yours is running" where it means "nobody counted"
// (work-system-review §2.2).
func TestTheCompletionNoticeCountsTheRootsOtherRunningTasks(t *testing.T) {
	b, ctx := newTestBroker(t)
	other := "5b0c7e1e-0000-4000-8000-000000000009"
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%2", Assistant: session.AssistantClaude, ConversationID: other},
		}
	}
	live := func(id, root string) {
		t.Helper()
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "live", State: StateBriefed,
			CreatedAt: time.Now(), Claims: []string{},
			Root: &RootRef{SessionID: root, Assistant: "claude"}}
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
	}
	live("f1000000-0000-4000-8000-000000000001", rootConversation)
	live("f1000000-0000-4000-8000-000000000002", rootConversation)
	live("f1000000-0000-4000-8000-000000000003", other)

	finishing := "f1000000-0000-4000-8000-000000000004"
	live(finishing, rootConversation)
	settled, err := b.Settle(ctx, finishing, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := b.NoticeWire(ctx, settled)
	if err != nil {
		t.Fatal(err)
	}
	var body noticeBody
	inner := strings.TrimSuffix(strings.TrimPrefix(wire, "<clawdline-notice>"), "</clawdline-notice>")
	if err := json.Unmarshal([]byte(inner), &body); err != nil {
		t.Fatal(err)
	}
	if body.Outstand != 2 {
		t.Fatalf(`"outstanding" = %d, want 2: this root has two other tasks still running and another root's `+
			`task is not one of them`, body.Outstand)
	}
	if strings.ContainsAny(wire, "\n\r") {
		t.Fatal("the notice is not one physical line")
	}
}

// ⑧ The inventory answers for a settled empty branch with the sentence that
// names the checkout, end to end — the row a root reads before it decides
// what to record.
func TestAnUnlandedRowSaysWhichFactIsMissing(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "f2000000-0000-4000-8000-000000000001"
	r := checkedOutTask(t, b, ctx, repo, id)
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	// The sweep has taken the checkout and recorded nothing about it, which
	// is the state every one of the nineteen rows was actually in.
	if err := os.RemoveAll(r.Worktree.Path); err != nil {
		t.Fatal(err)
	}
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	var row *InventoryRow
	for i := range inv.Unlanded {
		if inv.Unlanded[i].Task == id {
			row = &inv.Unlanded[i]
		}
	}
	if row == nil {
		t.Fatalf("the task is not unlanded: %+v", inv.Unlanded)
	}
	if strings.Contains(row.Why, "no commit count") {
		t.Errorf("the row blames the commit count, which is known and is 0: %q", row.Why)
	}
	if !strings.Contains(row.Why, "could not read its checkout") {
		t.Errorf("the row does not name what is actually missing: %q", row.Why)
	}
}
