package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The user's sentence, as a test, in the vocabulary that existed before this
// change: a proposal whose subject has been delivered and landed must not
// still be sitting in the "to confirm" area asking to be answered.
func TestASettledSubjectLeavesTheToConfirmArea(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), line, theRoot, clock.at)
	if _, err := propose(p, line, ""); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Hour)
	row, err := st.BrokerTask(ctx, taskID(1))
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.Decode(row.Record)
	if err != nil {
		t.Fatal(err)
	}
	r.State, r.FinishedAt = orchestrator.StateSuccess, clock.at
	r.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", At: clock.at, Commit: "abc"}
	putTask(t, st, r)
	if _, _, err := st.UpdateBrokerTask(ctx, taskID(1), func(tx *store.Tx, _ store.BrokerRow) (*store.BrokerWrite, error) {
		prev, err := tx.Todo(work.TodoID(work.OriginDispatch, taskID(1)))
		if err != nil {
			return nil, err
		}
		next := prev.Todo
		next.State, next.Reason, next.UpdatedAt, next.ClosedAt = work.TodoStateDone, work.ReasonLanded, clock.at, clock.at
		return nil, tx.PutTodo(next, &prev)
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := p.ProposalList(ctx, work.ProposalPending, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("the subject is delivered and landed, and %d proposal(s) still ask about it: %+v",
			len(page.Rows), page.Rows[0].State)
	}
}

// A line withdrawn because a dispatch alone is not worth asking about is
// asked about once it has been owed for a day — the signal that makes it
// worth asking. A withdrawal is the server's own statement, not an answer, so
// it does not stand in the way of the question it did not answer.
func TestAWithdrawnLineIsAskedAgainOnceItIsStuck(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), line, theRoot, clock.at)
	clock.at = clock.at.Add(p.Proposals.RuleAfter)
	if n, err := p.RuleProposals(ctx); err != nil || n != 0 {
		t.Fatalf("a dispatch alone made %d: %v", n, err)
	}
	clock.at = clock.at.Add(work.LongLived)
	if n, err := p.RuleProposals(ctx); err != nil || n != 1 {
		t.Fatalf("a line owed past a day made %d: %v", n, err)
	}
	page, err := p.ProposalList(ctx, work.ProposalPending, "", "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].WorkID != line {
		t.Fatalf("pending: %+v %v", page.Rows, err)
	}
	// And now that it carries I2 the rules do not take it back again.
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, why := stateOf(t, p, page.Rows[0].ID); got != work.ProposalPending {
		t.Fatalf("withdrew the question it had just asked: %s (%s)", got, why)
	}
}
