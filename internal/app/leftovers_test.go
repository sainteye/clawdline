package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// What a child did not do, from its delivery to a Backlog row — and the four
// places nothing happens.
//
// The path under test is one sentence: a child names what it left in
// `result.json`, its root sees those names when it integrates, raising one is
// one call, and a person's answer is the only thing that makes a row. Every
// test below is one link of that, and each has the "and otherwise nothing"
// beside it, because "nothing was created" is the property this design is
// mostly made of (§9's reason: no session puts anything on a person's board).

// delivered is a finished child whose result named these leftovers.
func delivered(t *testing.T, st *store.Store, id, owner string, at time.Time, left ...work.Leftover) orchestrator.Record {
	t.Helper()
	r := orchestrator.Record{ID: id, Kind: "custom", Title: "task " + id[len(id)-2:],
		State: orchestrator.StateSuccess, CreatedAt: at, FinishedAt: at, ProjectDir: "/p",
		Result: &taskdir.Result{Protocol: 1, TaskID: id, Status: "success", Summary: "done what I could",
			Leftovers: left}}
	if owner != "" {
		r.Root = &orchestrator.RootRef{SessionID: owner, Assistant: "claude"}
	}
	putTask(t, st, r)
	return r
}

func raise(p *Participation, task, title string) (ProposalView, error) {
	return p.Propose(context.Background(), ProposalRequest{Session: theRoot, TaskID: task, Leftover: title}, nil)
}

// The whole path, and the four places it stops.
func TestLeftoverBecomesABacklogRowOnlyWhenSomebodyAnswers(t *testing.T) {
	ctx := context.Background()
	p, board, st, clock := newParticipation(t)
	// Every binding the participation asks the broker for. A leftover must
	// ask for none: without this the guard that stops it can be deleted and
	// every test still passes, because a Participation with no broker binds
	// nothing anyway.
	var bound []string
	p.Bind = func(_ context.Context, task, workID, from string) error {
		bound = append(bound, task+"->"+workID+" ("+from+")")
		return nil
	}
	delivered(t, st, taskID(1), theRoot, clock.at,
		work.Leftover{Title: "the four daemon defects", Why: "out of time after the third",
			Acceptance: "each one has a failing test"},
		work.Leftover{Title: "the half-done feature", Why: "its other half is another task's claim"})

	// The root sees them as candidates, and raising one records a proposal
	// that asks nobody: a child's delivery is not a person being present.
	v, err := raise(p, taskID(1), "the four daemon defects")
	if err != nil {
		t.Fatalf("raising a leftover: %v", err)
	}
	switch {
	case v.Proposal.State != work.ProposalPending:
		t.Fatalf("state %q", v.Proposal.State)
	case !v.Proposal.Leftover():
		t.Fatalf("signals %v do not say leftover", v.Proposal.Signals)
	case v.Proposal.Title != "the four daemon defects":
		t.Fatalf("title %q is not the child's", v.Proposal.Title)
	case v.Proposal.TaskID != taskID(1):
		t.Fatalf("task %q: the proposal must carry the delivery that raised it", v.Proposal.TaskID)
	case v.Proposal.WorkID == taskID(1):
		t.Fatalf("the leftover took the delivery's own line of work")
	case v.Proposal.Channel != work.ChannelToConfirm:
		t.Fatalf("channel %q", v.Proposal.Channel)
	case !strings.Contains(v.Proposal.Question, "out of time after the third"):
		t.Fatalf("the question does not carry the why: %q", v.Proposal.Question)
	case v.Item != nil:
		t.Fatalf("proposing made a work item; only an answer may")
	}

	// Nothing on the board or in the Backlog until a person answers.
	if _, err := board.Item(ctx, v.Proposal.WorkID); codeOf(err) != "work_not_found" {
		t.Fatalf("an unanswered leftover left an item: %v", err)
	}

	// One answer, one row — in the Backlog, with no owner and no commitment,
	// and a first move that names the delivery it came out of.
	answered, err := p.Answer(ctx, v.Proposal.ID, work.AnswerLater, "user", "local", nil, nil)
	if err != nil {
		t.Fatalf("answering later: %v", err)
	}
	if answered.Item == nil {
		t.Fatal("later made no item")
	}
	it := answered.Item.Item
	switch {
	case it.Place != work.PlaceBacklog || it.State != work.ItemPlanned:
		t.Fatalf("item is %s/%s", it.Place, it.State)
	case it.Title != "the four daemon defects":
		t.Fatalf("row title %q", it.Title)
	case it.Owner != "" || it.Commitment != "":
		t.Fatalf("a Backlog row has no owner and no commitment: %q %q", it.Owner, it.Commitment)
	}
	moves, _, err := board.Moves(ctx, it.ID, 0)
	if err != nil || len(moves) == 0 {
		t.Fatalf("moves: %v %d", err, len(moves))
	}
	var evidence map[string]any
	if err := json.Unmarshal(moves[0].Evidence, &evidence); err != nil {
		t.Fatalf("move evidence: %v", err)
	}
	if evidence["leftover_of_task"] != taskID(1) {
		t.Fatalf("the row does not say which delivery raised it: %v", evidence)
	}

	// The delivery itself is never asked to join the new line. It reported
	// that this work was *not* done; binding it would say the opposite, and
	// would take a finished task off the line it really belongs to.
	if len(bound) != 0 {
		t.Fatalf("the delivery was bound onto the leftover's line: %v", bound)
	}

	// A second leftover of the same delivery is a second subject, not a
	// duplicate of the first: this is what one task id shared by several
	// proposals would otherwise cost.
	second, err := raise(p, taskID(1), "the half-done feature")
	if err != nil {
		t.Fatalf("the delivery's second leftover: %v", err)
	}
	if second.Proposal.WorkID == v.Proposal.WorkID {
		t.Fatal("two leftovers of one delivery share a line of work")
	}

	// The same leftover twice is a duplicate, and writes nothing.
	before := pendingCount(t, p)
	if _, err := raise(p, taskID(1), "the half-done feature"); codeOf(err) != work.RefuseDuplicate {
		t.Fatalf("raising one leftover twice: %v", err)
	}
	if n := pendingCount(t, p); n != before {
		t.Fatalf("a refusal wrote a proposal: %d -> %d", before, n)
	}

	// Answered no: still nothing, and it is not asked again.
	if _, err := p.Answer(ctx, second.Proposal.ID, work.AnswerNo, "user", "local", nil, nil); err != nil {
		t.Fatalf("answering no: %v", err)
	}
	if _, err := board.Item(ctx, second.Proposal.WorkID); codeOf(err) != "work_not_found" {
		t.Fatalf("`no` left an item: %v", err)
	}
	if _, err := raise(p, taskID(1), "the half-done feature"); codeOf(err) != work.RefuseDuplicate {
		t.Fatalf("a declined leftover was raised again: %v", err)
	}
}

// Nobody answers: the safe default stands and the Backlog is untouched. This
// is the clause the whole design rests on — a candidate is not a row.
func TestUnansweredLeftoverExpiresAndMakesNothing(t *testing.T) {
	ctx := context.Background()
	p, board, st, clock := newParticipation(t)
	delivered(t, st, taskID(2), theRoot, clock.at, work.Leftover{Title: "the flaky test", Why: "not mine to fix"})
	v, err := raise(p, taskID(2), "the flaky test")
	if err != nil {
		t.Fatalf("raising: %v", err)
	}
	clock.at = clock.at.Add(p.Proposals.Expiry + time.Hour)
	if err := p.Sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	after, err := p.Proposal(ctx, v.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != work.ProposalExpired {
		t.Fatalf("state %q", after.State)
	}
	if _, err := board.Item(ctx, v.Proposal.WorkID); codeOf(err) != "work_not_found" {
		t.Fatalf("an expired leftover left an item: %v", err)
	}
	page, err := board.Backlog(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("the Backlog holds %d rows nobody asked for", len(page.Rows))
	}
}

// What is refused, each writing nothing: a leftover that delivery never named,
// another root's delivery, a work_id on something that has no line yet, and a
// result whose leftovers are not readable.
func TestLeftoverRefusals(t *testing.T) {
	ctx := context.Background()
	p, _, st, clock := newParticipation(t)
	delivered(t, st, taskID(3), theRoot, clock.at, work.Leftover{Title: "the one it named"})
	delivered(t, st, taskID(4), "another-root", clock.at, work.Leftover{Title: "somebody else's"})
	delivered(t, st, taskID(5), theRoot, clock.at)

	for _, c := range []struct {
		why, task, title, code string
	}{
		{"a title that delivery never named", taskID(3), "invented", "leftover_not_found"},
		{"a delivery with no leftovers at all", taskID(5), "anything", "leftover_not_found"},
		{"another root's delivery", taskID(4), "somebody else's", "not_the_root"},
		{"no delivery at all", "", "the one it named", "subject_required"},
	} {
		if _, err := raise(p, c.task, c.title); codeOf(err) != c.code {
			t.Fatalf("%s: %v", c.why, err)
		}
		if n := pendingCount(t, p); n != 0 {
			t.Fatalf("%s wrote %d proposals", c.why, n)
		}
	}
	// A leftover has no line of work to name yet.
	_, err := p.Propose(ctx, ProposalRequest{Session: theRoot, TaskID: taskID(3), Leftover: "the one it named",
		WorkID: newWorkID()}, nil)
	if codeOf(err) != "invalid_work_id" {
		t.Fatalf("a leftover with a work_id: %v", err)
	}
	// Control: the same call without the work id is recorded.
	if _, err := raise(p, taskID(3), "the one it named"); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// A result whose leftovers are past their bounds is not put in front of a
// person in part. The child's own validator refuses such a result; a record
// that holds one anyway is refused here rather than truncated.
func TestUnreadableLeftoversAreRefusedRatherThanTrimmed(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	long := work.Leftover{Title: strings.Repeat("x", work.LeftoverTitleLimit+1)}
	delivered(t, st, taskID(6), theRoot, clock.at, work.Leftover{Title: "readable"}, long)
	if _, err := raise(p, taskID(6), "readable"); codeOf(err) != "invalid_leftover" {
		t.Fatalf("an unreadable list: %v", err)
	}
	if n := pendingCount(t, p); n != 0 {
		t.Fatalf("a refusal wrote %d proposals", n)
	}
}
