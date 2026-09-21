package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Where a person takes part, against a real store: every refusal of
// board-redesign §4.4 writes nothing, and each has a control beside it that
// the same check answers yes to (DG-8); the server — never the session —
// decides whether to ask; what it decided and what the session reported are
// counted from the rows; and a proposal, a person's answer, a decision and the
// broker's landing move one work item end to end.

const theRoot = "root-conv"

func newParticipation(t *testing.T) (*Participation, *WorkBoard, *store.Store, *boardClock) {
	t.Helper()
	w, st, clock := newBoard(t)
	p := NewParticipation(w)
	p.Proposals.Location, p.Digests.Location = time.UTC, time.UTC
	return p, w, st, clock
}

func taskID(n int) string {
	return "7a5c0000-0000-4000-8000-0000000000" + string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// sent is a dispatched task: kind, a work id (or none), a root (or none).
func sent(t *testing.T, st *store.Store, id, workID, kind, owner string, at time.Time) orchestrator.Record {
	t.Helper()
	r := orchestrator.Record{ID: id, Kind: kind, Title: "task " + id[len(id)-2:], WorkID: workID,
		State: orchestrator.StateBriefed, CreatedAt: at}
	if owner != "" {
		r.Root = &orchestrator.RootRef{SessionID: owner, Assistant: "claude"}
	}
	putTask(t, st, r)
	return r
}

// owes gives a task its root's to-do, as the broker would.
func owes(t *testing.T, st *store.Store, task, workID, owner string, at time.Time) {
	t.Helper()
	_, _, err := st.UpdateBrokerTask(context.Background(), task, func(tx *store.Tx, _ store.BrokerRow) (*store.BrokerWrite, error) {
		return nil, tx.PutTodo(work.Todo{ID: work.TodoID(work.OriginDispatch, task), Origin: work.OriginDispatch,
			Task: task, WorkID: workID, Title: "task " + task[len(task)-2:], Project: "/p", Owner: owner,
			State: work.TodoStateOpen, Reason: work.ReasonDispatched, CreatedAt: at, UpdatedAt: at}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func codeOf(err error) string {
	var we *WorkError
	if errors.As(err, &we) {
		return we.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func pendingCount(t *testing.T, p *Participation) int64 {
	t.Helper()
	c, err := p.Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c.Pending
}

func propose(p *Participation, workID, taskID string, effects ...string) (ProposalView, error) {
	return p.Propose(context.Background(), ProposalRequest{Session: theRoot, WorkID: workID, TaskID: taskID,
		Title: "ship the thing", Project: "/p", Effects: effects}, nil)
}

// proposal_below_threshold: nothing a person would want to follow — only a
// review and a scheduled run — and nothing is written. The control: a child
// the root sent for it is I1, and the same door records it.
func TestProposalBelowThresholdIsRefusedAndWritesNothing(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	line := newWorkID()
	sent(t, st, taskID(1), line, "code-review", theRoot, clock.at)
	sent(t, st, taskID(2), line, "custom", "", clock.at)
	_, err := propose(p, line, "")
	if codeOf(err) != work.RefuseBelowThreshold {
		t.Fatalf("a review and a scheduled run: %v", err)
	}
	if n := pendingCount(t, p); n != 0 {
		t.Fatalf("a refusal wrote %d proposals", n)
	}
	// Control: the same line with a child its root sent is worth asking.
	sent(t, st, taskID(3), line, "custom", theRoot, clock.at)
	v, err := propose(p, line, "")
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if len(v.Proposal.Signals) != 1 || v.Proposal.Signals[0] != work.SignalCrossSession || len(v.Ignored) != 2 {
		t.Fatalf("control signals %v ignored %v", v.Proposal.Signals, v.Ignored)
	}
}

// proposal_from_child: a child proposes; the proposal is its root's, in the
// root's "to confirm" area, and nobody is asked — though the root, heard a
// minute ago, would have been. The control is the root itself.
func TestAChildsProposalGoesToItsRootsConfirmArea(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	p.Heard.Mark(clock.at.Add(-time.Minute), theRoot)
	line := newWorkID()
	child := sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	v, err := p.Propose(context.Background(), ProposalRequest{Session: theRoot, WorkID: line, TaskID: child.ID,
		Title: "ship", Project: "/p", FromChild: true, ChildTask: child.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Proposal.Ask || v.Proposal.AskReason != work.AskFromChild || v.Proposal.Channel != work.ChannelToConfirm ||
		v.Proposal.Session != theRoot || v.Proposal.Source != "child:"+child.ID || v.Proposal.Question != "" {
		t.Fatalf("a child's proposal: %+v", v.Proposal)
	}
	// Control: the root proposing another line of its own is asked.
	other := newWorkID()
	sent(t, st, taskID(2), other, "custom", theRoot, clock.at)
	c, err := propose(p, other, "")
	if err != nil || !c.Proposal.Ask || c.Proposal.AskReason != work.AskHumanPresent || c.Proposal.Question == "" {
		t.Fatalf("control: %+v %v", c.Proposal, err)
	}
}

// proposal_duplicate: a line already waiting is not proposed twice; one a
// person said no to comes back only on a signal it did not have, and only
// once.
func TestADuplicateProposalIsRefused(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	first, err := propose(p, line, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := propose(p, line, ""); codeOf(err) != work.RefuseDuplicate {
		t.Fatalf("a second while the first waits: %v", err)
	}
	if _, err := p.Answer(ctx, first.Proposal.ID, work.AnswerNo, "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := propose(p, line, ""); codeOf(err) != work.RefuseDuplicate {
		t.Fatalf("the same signals after a no: %v", err)
	}
	// Control: a signal it did not have (an effect) — asked once more.
	again, err := propose(p, line, "", "deploy")
	if err != nil {
		t.Fatalf("a new signal after a no: %v", err)
	}
	if _, err := p.Answer(ctx, again.Proposal.ID, work.AnswerNo, "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(48 * time.Hour)
	owes(t, st, taskID(1), line, theRoot, clock.at.Add(-49*time.Hour))
	if _, err := propose(p, line, "", "deploy", "spend"); codeOf(err) != work.RefuseDuplicate {
		t.Fatalf("a third time, even with a new signal: %v", err)
	}
	if n := pendingCount(t, p); n != 0 {
		t.Fatalf("refusals wrote %d pending proposals", n)
	}
}

// proposal_already_tracked: a line that is a work item already is bound, not
// asked about. The control is a line that is not.
func TestATrackedLineIsNotProposed(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	item, err := w.Create(ctx, NewWork{Title: "already", Project: "/p", Place: work.PlaceBacklog, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sent(t, st, taskID(1), item.Item.ID, "custom", theRoot, clock.at)
	if _, err := propose(p, item.Item.ID, ""); codeOf(err) != work.RefuseAlreadyTracked {
		t.Fatalf("a Backlog item: %v", err)
	}
	// A line a person answered track for is tracked too.
	line := newWorkID()
	sent(t, st, taskID(2), line, "custom", theRoot, clock.at)
	v, err := propose(p, line, "")
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if _, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := propose(p, line, "", "deploy"); codeOf(err) != work.RefuseAlreadyTracked {
		t.Fatalf("a line answered track: %v", err)
	}
}

// proposal_budget_exhausted: once per session per turn, three a day; past
// either, the proposal is recorded in the "to confirm" area, not asked. A new
// message from the person is a new turn.
func TestTheBudgetMovesAsksToTheConfirmArea(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	p.Heard.Mark(clock.at.Add(-2*time.Minute), theRoot)
	lines := make([]string, 6)
	for i := range lines {
		lines[i] = newWorkID()
		sent(t, st, taskID(i+1), lines[i], "custom", theRoot, clock.at)
	}
	ask := func(i int) work.Proposal {
		t.Helper()
		v, err := propose(p, lines[i], "")
		if err != nil {
			t.Fatal(err)
		}
		return v.Proposal
	}
	if a := ask(0); !a.Ask {
		t.Fatalf("the turn's first: %+v", a)
	}
	if b := ask(1); b.Ask || b.AskReason != work.AskBudgetExhausted || b.Channel != work.ChannelToConfirm {
		t.Fatalf("the turn's second: %+v", b)
	}
	// A new message: a new turn, one more ask.
	clock.at = clock.at.Add(time.Minute)
	p.Heard.Mark(clock.at, theRoot)
	if c := ask(2); !c.Ask {
		t.Fatalf("the next turn's first: %+v", c)
	}
	clock.at = clock.at.Add(time.Minute)
	p.Heard.Mark(clock.at, theRoot)
	if d := ask(3); !d.Ask {
		t.Fatalf("the day's third: %+v", d)
	}
	clock.at = clock.at.Add(time.Minute)
	p.Heard.Mark(clock.at, theRoot)
	if e := ask(4); e.Ask || e.AskReason != work.AskBudgetExhausted {
		t.Fatalf("the day's fourth, in a new turn: %+v", e)
	}
	// The next day the budget is back.
	clock.at = clock.at.Add(24 * time.Hour)
	p.Heard.Mark(clock.at, theRoot)
	if f := ask(5); !f.Ask {
		t.Fatalf("the next day: %+v", f)
	}
}

// human_absent: nobody wrote to the session through this daemon inside half
// an hour — the proposal waits in the "to confirm" area. The control is the
// same session a person wrote to five minutes ago.
func TestNobodyThereMeansNotAsked(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	lines := []string{newWorkID(), newWorkID(), newWorkID()}
	for i, l := range lines {
		sent(t, st, taskID(i+1), l, "custom", theRoot, clock.at)
	}
	v, err := propose(p, lines[0], "")
	if err != nil || v.Proposal.Ask || v.Proposal.AskReason != work.AskHumanAbsent {
		t.Fatalf("never heard: %+v %v", v.Proposal, err)
	}
	p.Heard.Mark(clock.at.Add(-31*time.Minute), theRoot)
	if v, _ := propose(p, lines[1], ""); v.Proposal.Ask || v.Proposal.AskReason != work.AskHumanAbsent {
		t.Fatalf("heard 31 minutes ago: %+v", v.Proposal)
	}
	p.Heard.Mark(clock.at.Add(-5*time.Minute), theRoot)
	if v, _ := propose(p, lines[2], ""); !v.Proposal.Ask {
		t.Fatalf("control, heard 5 minutes ago: %+v", v.Proposal)
	}
}

// I2 and I3 from facts: a to-do owed for more than a day is long-lived, and
// a landing on main is an effect outside the machine — neither is anything
// the session said.
func TestTheSignalsAreReadFromFacts(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	line := newWorkID()
	r := sent(t, st, taskID(1), line, "custom", theRoot, clock.at.Add(-30*time.Hour))
	owes(t, st, r.ID, line, theRoot, clock.at.Add(-30*time.Hour))
	r.State, r.FinishedAt = orchestrator.StateSuccess, clock.at.Add(-time.Hour)
	r.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", At: clock.at.Add(-time.Hour)}
	putTask(t, st, r)
	v, err := propose(p, line, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []work.Signal{work.SignalCrossSession, work.SignalLongLived, work.SignalExternalEffect}
	if len(v.Proposal.Signals) != 3 || v.Proposal.Signals[1] != want[1] || v.Proposal.Signals[2] != want[2] {
		t.Fatalf("signals %v", v.Proposal.Signals)
	}
	if _, err := propose(p, newWorkID(), "", "teleport"); codeOf(err) != "unknown_effect" {
		t.Fatalf("an effect outside the vocabulary: %v", err)
	}
}

// The rules propose a line nobody proposed — once, into the "to confirm"
// area, even with the person there — but only after its root has had the
// grace to propose it itself; and they leave alone a to-do on no line (a
// task stored before lines were bound) and a line with nothing to say.
func TestTheRulesProposeALineOnceAfterItsRootsGrace(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	p.Heard.Mark(clock.at, theRoot)
	named, steps := newWorkID(), newWorkID()
	sent(t, st, taskID(1), named, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), named, theRoot, clock.at)
	sent(t, st, taskID(2), "", "custom", theRoot, clock.at)
	owes(t, st, taskID(2), "", theRoot, clock.at)
	sent(t, st, taskID(3), steps, "code-review", theRoot, clock.at)
	owes(t, st, taskID(3), steps, theRoot, clock.at)
	// Inside the grace the line is its root's to propose.
	clock.at = clock.at.Add(p.Proposals.RuleAfter - time.Second)
	if n, err := p.RuleProposals(ctx); err != nil || n != 0 {
		t.Fatalf("inside the grace the rules made %d: %v", n, err)
	}
	clock.at = clock.at.Add(time.Second)
	// Past the grace and still nothing: a dispatch on its own is I1, and I1
	// is what every line here carries. The rules wait for a line that has
	// outlived a day of nobody following it (RuleWorthy).
	if n, err := p.RuleProposals(ctx); err != nil || n != 0 {
		t.Fatalf("I1 alone made %d: %v", n, err)
	}
	clock.at = clock.at.Add(work.LongLived)
	if n, err := p.RuleProposals(ctx); err != nil || n != 1 {
		t.Fatalf("the rules made %d: %v", n, err)
	}
	page, err := p.ProposalList(ctx, work.ProposalPending, "", "")
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("pending: %+v %v", page.Rows, err)
	}
	got := page.Rows[0]
	if got.WorkID != named || got.Ask || got.AskReason != work.AskByRule || got.Channel != work.ChannelToConfirm ||
		got.Source != work.SourceRule || got.Session != theRoot {
		t.Fatalf("the rule's proposal: %+v", got)
	}
	if n, _ := p.RuleProposals(ctx); n != 0 {
		t.Fatalf("proposed again: %d", n)
	}
	if c, _ := p.Counts(ctx); c.AskTrue != 0 || c.AskFalse != 1 || !c.Matched {
		t.Fatalf("counts: %+v", c)
	}
}

// A proposal about a task stored on no line puts the task on the line it
// names — the task's own, by the rules a dispatch is bound by — so the item a
// "track" makes follows that task, and the task's landing closes it.
func TestAProposalAboutAnUnboundTaskBindsIt(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	task := sent(t, st, taskID(1), "", "custom", theRoot, clock.at)
	bound := []string{}
	p.Bind = func(_ context.Context, id, line, from string) error {
		bound = append(bound, id+" "+line+" "+from)
		r := task
		r.WorkID, r.WorkFrom = line, from
		putTask(t, st, r)
		task = r
		return nil
	}
	v, err := propose(p, "", task.ID)
	if err != nil || v.Proposal.WorkID != task.ID {
		t.Fatalf("propose: %+v %v", v.Proposal, err)
	}
	if len(bound) != 1 || bound[0] != task.ID+" "+task.ID+" "+work.WorkDispatch {
		t.Fatalf("bindings: %v", bound)
	}
	answered, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, "user", "local", nil, nil)
	if err != nil || answered.Item == nil {
		t.Fatalf("track: %+v %v", answered, err)
	}
	if answered.Item.Derived.Tasks.Total != 1 || answered.Item.Derived.Reason != work.ReasonTaskRunning {
		t.Fatalf("the item does not follow the task: %+v", answered.Item.Derived)
	}
	task.State, task.FinishedAt = orchestrator.StateSuccess, clock.at
	task.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", At: clock.at, Commit: "abc"}
	putTask(t, st, task)
	clock.at = clock.at.Add(time.Minute)
	if pass := w.Sweep(ctx); pass.Moved != 1 || pass.Err != "" {
		t.Fatalf("the landing pass: %+v", pass)
	}
	if item, _ := w.Item(ctx, task.ID); item.Item.State != work.ItemDone || item.Item.ClosedReason != work.ClosedLanded {
		t.Fatalf("after the landing: %+v", item.Item)
	}
	// Control: a task on another line is not moved to the one a proposal names.
	other := sent(t, st, taskID(2), newWorkID(), "custom", theRoot, clock.at)
	if _, err := propose(p, newWorkID(), other.ID); codeOf(err) != "work_id_mismatch" {
		t.Fatalf("a task on another line: %v", err)
	}
}

// A relayed answer rests on a run said to the proposal's own root: a message
// to another session answers nothing here, and the move records whose word
// it was, where and when.
func TestARelayedAnswerIsTheRootsPersonsWord(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	v, err := propose(p, line, "")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := &work.Run{ID: newWorkID(), Session: "another-conv", Terminal: "%9", Principal: "local", At: clock.at}
	if _, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, elsewhere.Actor(), "machine", elsewhere, nil); codeOf(err) != "run_other_session" {
		t.Fatalf("a message to another session: %v", err)
	}
	if n := pendingCount(t, p); n != 1 {
		t.Fatalf("a refused relay answered the proposal (%d pending)", n)
	}
	earlier := &work.Run{ID: newWorkID(), Session: theRoot, Terminal: "%1", Principal: "local", At: clock.at.Add(-time.Hour)}
	if _, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, earlier.Actor(), "machine", earlier, nil); codeOf(err) != "run_before_question" {
		t.Fatalf("a message from before the proposal: %v", err)
	}
	here := &work.Run{ID: newWorkID(), Session: theRoot, Terminal: "%1", Principal: "device:phone", At: clock.at}
	answered, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, here.Actor(), "machine", here, nil)
	if err != nil || answered.Item == nil {
		t.Fatalf("the root's own run: %+v %v", answered, err)
	}
	m := moves(t, st, line)
	var ev map[string]any
	if len(m) != 1 || m[0].Actor != "user_via_session:"+here.ID || json.Unmarshal(m[0].Evidence, &ev) != nil {
		t.Fatalf("the move: %+v", m)
	}
	run, _ := ev["run"].(map[string]any)
	if run["session"] != theRoot || run["id"] != here.ID || run["principal"] != "device:phone" {
		t.Fatalf("the move's run: %v", ev)
	}
}

// What the server decided and what the sessions reported are counted from
// the rows: every ask:true reported asked is matched, and one reported asked
// when the server said not to is a briefing not followed.
func TestAskedInlineAndAskTrueAreCountedAgainstEachOther(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	p.Heard.Mark(clock.at.Add(-time.Minute), theRoot)
	lines := []string{newWorkID(), newWorkID()}
	for i, l := range lines {
		sent(t, st, taskID(i+1), l, "custom", theRoot, clock.at)
	}
	asked, _ := propose(p, lines[0], "")
	held, _ := propose(p, lines[1], "")
	if !asked.Proposal.Ask || held.Proposal.Ask {
		t.Fatalf("asked %+v held %+v", asked.Proposal, held.Proposal)
	}
	c, _ := p.Counts(ctx)
	if c.AskTrue != 1 || c.AskedInline != 0 || c.Matched {
		t.Fatalf("before the report: %+v", c)
	}
	if _, err := p.ReportAsked(ctx, asked.Proposal.ID, theRoot); err != nil {
		t.Fatal(err)
	}
	// A second report is the same fact: counted once.
	if _, err := p.ReportAsked(ctx, asked.Proposal.ID, theRoot); err != nil {
		t.Fatal(err)
	}
	c, _ = p.Counts(ctx)
	if c.AskTrue != 1 || c.AskedInline != 1 || c.Unprompted != 0 || !c.Matched {
		t.Fatalf("after the report: %+v", c)
	}
	if _, err := p.ReportAsked(ctx, held.Proposal.ID, "someone-else"); codeOf(err) != "not_your_proposal" {
		t.Fatalf("another session's report: %v", err)
	}
	// Asked anyway: recorded, not refused — refusing it would hide it.
	v, err := p.ReportAsked(ctx, held.Proposal.ID, theRoot)
	if err != nil || !v.Proposal.Unprompted() {
		t.Fatalf("an unprompted ask: %+v %v", v.Proposal, err)
	}
	c, _ = p.Counts(ctx)
	if c.AskTrue != 1 || c.AskedInline != 2 || c.Unprompted != 1 || c.Matched {
		t.Fatalf("after an unprompted ask: %+v", c)
	}
}

type pushes struct {
	mu   sync.Mutex
	tags []string
	sent int
}

func (p *pushes) push(_ context.Context, _, _, tag string) (int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tags = append(p.tags, tag)
	return p.sent, 0, nil
}

// The whole chain: a line of work sent out a child, the server lets it be
// asked, the person says track, the work item is on the board by a move from
// the proposal; the root asks a blocking decision, which is pushed once, and
// a non-blocking one, which is not; the person's answer is the item's newest
// fact; the broker's landing closes it.
func TestAProposalADecisionAndTheMovesEndToEnd(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	bell := &pushes{sent: 1}
	p.Push = bell.push
	line := newWorkID()
	task := sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	p.Heard.Mark(clock.at, theRoot)

	clock.at = clock.at.Add(time.Minute)
	v, err := propose(p, line, task.ID)
	if err != nil || !v.Proposal.Ask {
		t.Fatalf("propose: %+v %v", v.Proposal, err)
	}
	if _, err := p.ReportAsked(ctx, v.Proposal.ID, theRoot); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Minute)
	said := &work.Run{ID: newWorkID(), Session: theRoot, Terminal: "%1", Principal: "local", At: clock.at}
	if _, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, said.Actor(), "machine", nil, nil); codeOf(err) != "invalid_actor" {
		t.Fatalf("a relayed actor with no run behind it: %v", err)
	}
	answered, err := p.Answer(ctx, v.Proposal.ID, work.AnswerTrack, said.Actor(), "machine", said, nil)
	if err != nil {
		t.Fatal(err)
	}
	if answered.Item == nil || answered.Item.Item.Place != work.PlaceBoard || answered.Item.Item.Owner != theRoot ||
		answered.Item.Derived.Reason != work.ReasonTaskRunning {
		t.Fatalf("the item the answer made: %+v", answered.Item)
	}
	m := moves(t, st, line)
	if len(m) != 1 || m[0].From != work.PlaceProposal || m[0].To != work.PlaceBoard || m[0].Trigger != "proposal_track" ||
		m[0].Actor != said.Actor() {
		t.Fatalf("the answer's move: %+v", m)
	}
	// The answer is recorded once; a second is refused.
	if _, err := p.Answer(ctx, v.Proposal.ID, work.AnswerNo, "user", "local", nil, nil); codeOf(err) != "proposal_answered" {
		t.Fatalf("a second answer: %v", err)
	}

	options := []work.Option{{ID: "a", Label: "keep the old API"}, {ID: "b", Label: "break it"}}
	blocking, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, WorkID: line, Question: "Break the API?",
		Options: options, Default: "a", Blocking: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, WorkID: line, Question: "Which colour?",
		Options: []work.Option{{ID: "red", Label: "red"}, {ID: "blue", Label: "blue"}}, Default: "red"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bell.tags) != 1 || bell.tags[0] != "decision-"+blocking.ID {
		t.Fatalf("pushes %v: only the blocking decision is pushed", bell.tags)
	}
	if d, _ := p.Decision(ctx, blocking.ID); d.Push != work.PushSent || d.PushedAt.IsZero() {
		t.Fatalf("the blocking push: %+v", d)
	}
	if d, _ := p.Decision(ctx, quiet.ID); d.Push != work.PushNone {
		t.Fatalf("the quiet one: %+v", d)
	}
	clock.at = clock.at.Add(time.Hour)
	if _, err := p.AnswerDecision(ctx, blocking.ID, "c", "user", "local", nil, nil); codeOf(err) != "invalid_answer" {
		t.Fatalf("an answer that is not an option: %v", err)
	}
	if _, err := p.AnswerDecision(ctx, blocking.ID, "b", "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	m = moves(t, st, line)
	if len(m) != 2 || m[1].Trigger != "decision_answered" || m[1].From != work.PlaceBoard || m[1].To != work.PlaceBoard {
		t.Fatalf("the decision's move: %+v", m)
	}

	// The broker's facts: the task delivers and lands; one sweep closes it.
	task.State, task.FinishedAt = orchestrator.StateSuccess, clock.at
	task.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", At: clock.at, Commit: "abc"}
	putTask(t, st, task)
	clock.at = clock.at.Add(time.Minute)
	if pass := w.Sweep(ctx); pass.Moved != 1 || pass.Err != "" {
		t.Fatalf("the landing pass: %+v", pass)
	}
	item, _ := w.Item(ctx, line)
	if item.Item.State != work.ItemDone || item.Item.ClosedReason != work.ClosedLanded {
		t.Fatalf("after the landing: %+v", item.Item)
	}
	m = moves(t, st, line)
	triggers := []string{}
	for _, x := range m {
		triggers = append(triggers, x.Trigger)
	}
	if len(m) != 3 || triggers[0] != "proposal_track" || triggers[1] != "decision_answered" || triggers[2] != work.TriggerLanded {
		t.Fatalf("the chain's moves: %v", triggers)
	}
	c, _ := p.Counts(ctx)
	if c.AskTrue != 1 || c.AskedInline != 1 || !c.Matched {
		t.Fatalf("the counts: %+v", c)
	}
}

// Nobody answers: a proposal's wait passes and it stays a to-do — no work
// item — and the digest says so once; a decision's due passes and its default
// stands, on the item, and a late answer is refused; a push that never
// recorded an outcome is unknown, not sent again.
func TestUnansweredDefaultsStand(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	v, err := propose(p, line, "")
	if err != nil {
		t.Fatal(err)
	}
	tracked, err := w.Create(ctx, NewWork{Title: "on the board", Project: "/p", Place: work.PlaceBoard, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, WorkID: tracked.Item.ID, Question: "Ship Friday?",
		Options: []work.Option{{ID: "yes", Label: "yes"}, {ID: "no", Label: "no"}}, Default: "no", Blocking: true,
		Due: 2 * time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Decision(ctx, d.ID); got.Push != work.PushNotSubscribed {
		t.Fatalf("no push path: %+v", got)
	}
	// Past the stall window the open decision holds the item on the board.
	clock.at = clock.at.Add(90 * time.Minute)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Decision(ctx, d.ID); got.State != work.DecisionOpen {
		t.Fatalf("before its due: %+v", got)
	}
	clock.at = clock.at.Add(time.Hour)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := p.Decision(ctx, d.ID)
	if got.State != work.DecisionDefaulted || got.Answer != "no" || got.AnsweredBy != work.ActorRule {
		t.Fatalf("after its due: %+v", got)
	}
	if _, err := p.AnswerDecision(ctx, d.ID, "yes", "user", "local", nil, nil); codeOf(err) != "decision_closed" {
		t.Fatalf("a late answer: %v", err)
	}
	if m := moves(t, st, tracked.Item.ID); len(m) != 2 || m[1].Trigger != "decision_defaulted" || m[1].Actor != work.ActorRule {
		t.Fatalf("the default's move: %+v", m)
	}
	// Seven days: the proposal expires, and nothing is on anybody's board.
	clock.at = clock.at.Add(7 * 24 * time.Hour)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Proposal(ctx, v.Proposal.ID); got.State != work.ProposalExpired {
		t.Fatalf("after the wait: %+v", got)
	}
	if _, err := st.WorkItem(ctx, line); !errors.Is(err, store.ErrNoWork) {
		t.Fatalf("an unanswered proposal made a work item: %v", err)
	}
	// A late answer is still a person's answer.
	late, err := p.Answer(ctx, v.Proposal.ID, work.AnswerLater, "user", "local", nil, nil)
	if err != nil || late.Item == nil || late.Item.Item.Place != work.PlaceBacklog {
		t.Fatalf("a late answer: %+v %v", late, err)
	}
	// A push recorded and never given an outcome.
	stuck := work.Decision{ID: newWorkID(), Session: theRoot, Question: "stuck?", Options: []work.Option{{ID: "a", Label: "a"},
		{ID: "b", Label: "b"}}, Default: "a", Blocking: true, State: work.DecisionOpen, CreatedAt: clock.at,
		DueAt: clock.at.Add(time.Hour), Push: work.PushPending}
	if err := st.WriteWork(ctx, func(tx *store.WorkTx) error { return tx.PutDecision(stuck, nil, 0) }); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(11 * time.Minute)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Decision(ctx, stuck.ID); got.Push != work.PushUnknown {
		t.Fatalf("a stuck push: %+v", got)
	}
}

// An open decision holds the quiet clock: an item waiting on a person has not
// stalled. The control is the same item once the decision is answered.
func TestAnOpenDecisionHoldsTheQuietClock(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	item, err := w.Create(ctx, NewWork{Title: "waits on you", Project: "/p", Place: work.PlaceBoard, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, WorkID: item.Item.ID, Question: "Which way?",
		Options: []work.Option{{ID: "l", Label: "left"}, {ID: "r", Label: "right"}}, Default: "l"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(4 * 24 * time.Hour)
	w.Sweep(ctx)
	if v, _ := w.Item(ctx, item.Item.ID); v.Item.Place != work.PlaceBoard || !v.Derived.StallAt.IsZero() {
		t.Fatalf("waiting on a person: %+v %+v", v.Item, v.Derived)
	}
	if _, err := p.AnswerDecision(ctx, d.ID, "r", "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(3*24*time.Hour + time.Minute)
	w.Sweep(ctx)
	if v, _ := w.Item(ctx, item.Item.ID); v.Item.Place != work.PlaceBacklog {
		t.Fatalf("control, three quiet days after the answer: %+v", v.Item)
	}
	if m := moves(t, st, item.Item.ID); m[len(m)-1].Trigger != work.TriggerStalled {
		t.Fatalf("moves %+v", m)
	}
}

// The daily digest asks about a delivery once it has waited three days, in
// the transaction that writes the digest; seven days from that ask the
// delivery closes as unconfirmed. The digest is written once however many
// passes see it due, and lists every automatic move of its day.
func TestTheDailyDigestAsksAboutADeliveryOnce(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	item, err := w.Create(ctx, NewWork{Title: "delivered", Project: "/p", Place: work.PlaceBoard, Owner: theRoot,
		Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := sent(t, st, taskID(1), item.Item.ID, "custom", theRoot, clock.at)
	task.State, task.FinishedAt = orchestrator.StateSuccess, clock.at.Add(time.Hour)
	putTask(t, st, task)
	clock.at = clock.at.Add(2 * time.Hour)
	w.Sweep(ctx) // → awaiting_closure, a broker move
	// The next day's digest lists that move; nothing is asked yet.
	clock.at = time.Date(2026, 9, 19, 0, 5, 0, 0, time.UTC)
	if wrote, err := p.WriteDigest(ctx, work.DigestDaily); err != nil || !wrote {
		t.Fatalf("the first daily: %v %v", wrote, err)
	}
	if wrote, _ := p.WriteDigest(ctx, work.DigestDaily); wrote {
		t.Fatal("the same day's digest written twice")
	}
	body := digestBody(t, st, "daily:2026-09-18")
	if body.Automatic.Total != 1 || body.Automatic.Lines[0].What != work.TriggerDelivered || body.ClosureAsked.Total != 0 ||
		body.AwaitingClosure != 1 {
		t.Fatalf("the first daily: %+v", body)
	}
	// Three days after the delivery the digest asks, once.
	clock.at = time.Date(2026, 9, 22, 0, 5, 0, 0, time.UTC)
	if wrote, err := p.WriteDigest(ctx, work.DigestDaily); err != nil || !wrote {
		t.Fatalf("the asking daily: %v %v", wrote, err)
	}
	body = digestBody(t, st, "daily:2026-09-21")
	if body.ClosureAsked.Total != 1 || body.ClosureAsked.Lines[0].WorkID != item.Item.ID {
		t.Fatalf("the asking daily: %+v", body.ClosureAsked)
	}
	v, _ := w.Item(ctx, item.Item.ID)
	if v.Item.AskedAt.IsZero() || v.Derived.ClosureDueAt.IsZero() {
		t.Fatalf("asked: %+v %+v", v.Item, v.Derived)
	}
	clock.at = clock.at.Add(24 * time.Hour)
	p.WriteDigest(ctx, work.DigestDaily)
	if body := digestBody(t, st, "daily:2026-09-22"); body.ClosureAsked.Total != 0 {
		t.Fatalf("asked twice: %+v", body.ClosureAsked)
	}
	// Seven days from the ask: unconfirmed, and the broker's record untouched.
	clock.at = v.Item.AskedAt.Add(7*24*time.Hour + time.Minute)
	w.Sweep(ctx)
	v, _ = w.Item(ctx, item.Item.ID)
	if v.Item.State != work.ItemDone || v.Item.ClosedReason != work.ClosedUnconfirmed || v.Derived.Landed {
		t.Fatalf("after the wait: %+v", v.Item)
	}
}

// The weekly digest asks about a Backlog item nobody has looked at for
// thirty days, once in those thirty days, and lets nothing go.
func TestTheWeeklyDigestAsksAboutAStaleBacklogOnce(t *testing.T) {
	p, w, st, clock := newParticipation(t)
	ctx := context.Background()
	item, err := w.Create(ctx, NewWork{Title: "someday", Project: "/p", Place: work.PlaceBacklog, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.at = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC) // a Monday, 31 days on
	if wrote, err := p.WriteDigest(ctx, work.DigestWeekly); err != nil || !wrote {
		t.Fatalf("weekly: %v %v", wrote, err)
	}
	key, _, _ := work.DigestWindow(work.DigestWeekly, clock.at, p.Digests)
	body := digestBody(t, st, key)
	if body.BacklogStale.Total != 1 || body.BacklogStale.Lines[0].WorkID != item.Item.ID {
		t.Fatalf("weekly: %+v", body.BacklogStale)
	}
	clock.at = clock.at.Add(7 * 24 * time.Hour)
	p.WriteDigest(ctx, work.DigestWeekly)
	key, _, _ = work.DigestWindow(work.DigestWeekly, clock.at, p.Digests)
	if body := digestBody(t, st, key); body.BacklogStale.Total != 0 {
		t.Fatalf("asked again inside thirty days: %+v", body.BacklogStale)
	}
	if v, _ := w.Item(ctx, item.Item.ID); v.Item.Place != work.PlaceBacklog || v.Item.State != work.ItemPlanned {
		t.Fatalf("the Backlog let something go: %+v", v.Item)
	}
}

// Blocking decisions are pushed at most thirty an hour; past it one is
// recorded over_budget and waits on the board.
func TestBlockingPushesHaveABudget(t *testing.T) {
	p, _, _, _ := newParticipation(t)
	ctx := context.Background()
	bell := &pushes{sent: 1}
	p.Push = bell.push
	var last work.Decision
	for i := 0; i <= decisionPushHourLimit; i++ {
		d, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, Question: "q", Blocking: true,
			Options: []work.Option{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}}, Default: "a"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		last = d
	}
	if len(bell.tags) != decisionPushHourLimit {
		t.Fatalf("%d pushes", len(bell.tags))
	}
	if got, _ := p.Decision(ctx, last.ID); got.Push != work.PushOverBudget {
		t.Fatalf("the one past the budget: %+v", got)
	}
	// A decision without a safe default is refused: nobody answering is the
	// ordinary case.
	if _, err := p.OpenDecision(ctx, DecisionRequest{Session: theRoot, Question: "q",
		Options: []work.Option{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}}}, nil); codeOf(err) != "decision_default_required" {
		t.Fatalf("no default: %v", err)
	}
}

func digestBody(t *testing.T, st *store.Store, key string) DigestBody {
	t.Helper()
	rows, err := st.Digests(context.Background(), "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == key {
			var b DigestBody
			if err := json.Unmarshal(r.Body, &b); err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	t.Fatalf("no digest %s among %d", key, len(rows))
	return DigestBody{}
}

// What waits for a person is bounded (DG-2): at a lowered proposals.open a
// new proposal is refused by name and the one waiting stays; at a lowered
// decisions.open a new decision is refused and none open is let go. The
// control is the same request once a slot is free.
func TestWhatWaitsForAPersonIsBounded(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	p.ProposalLimit, p.DecisionLimit = 1, 1
	lines := []string{newWorkID(), newWorkID()}
	for i, l := range lines {
		sent(t, st, taskID(i+1), l, "custom", theRoot, clock.at)
	}
	first, err := propose(p, lines[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := propose(p, lines[1], ""); codeOf(err) != "proposals_full" {
		t.Fatalf("a proposal past the limit: %v", err)
	}
	if n := pendingCount(t, p); n != 1 {
		t.Fatalf("pending %d at the limit", n)
	}
	if _, err := p.Answer(ctx, first.Proposal.ID, work.AnswerNo, "user", "local", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := propose(p, lines[1], ""); err != nil {
		t.Fatalf("control, a slot free: %v", err)
	}
	ask := DecisionRequest{Session: theRoot, Question: "q", Default: "a",
		Options: []work.Option{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}}}
	d, err := p.OpenDecision(ctx, ask, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.OpenDecision(ctx, ask, nil); codeOf(err) != "decisions_full" {
		t.Fatalf("a decision past the limit: %v", err)
	}
	if got, _ := p.Decision(ctx, d.ID); got.State != work.DecisionOpen {
		t.Fatalf("the open one was let go: %+v", got)
	}
}

// ——— The exit a proposal was missing ———

// lands closes a task's to-do the way the broker's landing record does, and
// marks the task itself landed on the default branch.
func lands(t *testing.T, st *store.Store, task, workID, owner string, at time.Time) {
	t.Helper()
	row, err := st.BrokerTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.Decode(row.Record)
	if err != nil {
		t.Fatal(err)
	}
	r.State, r.FinishedAt = orchestrator.StateSuccess, at
	r.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", At: at, Commit: "abc"}
	putTask(t, st, r)
	_, _, err = st.UpdateBrokerTask(context.Background(), task, func(tx *store.Tx, _ store.BrokerRow) (*store.BrokerWrite, error) {
		prev, err := tx.Todo(work.TodoID(work.OriginDispatch, task))
		if err != nil {
			return nil, err
		}
		next := prev.Todo
		next.State, next.Reason, next.UpdatedAt, next.ClosedAt = work.TodoStateDone, work.ReasonLanded, at, at
		return nil, tx.PutTodo(next, &prev)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stateOf(t *testing.T, p *Participation, id string) (work.ProposalState, string) {
	t.Helper()
	got, err := p.Proposal(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.State, got.WithdrawnReason
}

// The defect this exists for: a proposal has two exits and both are about the
// person — an answer, and the wait. Neither is about the subject, so a line
// that landed an hour after it was proposed went on asking "shall I follow
// this?" for the rest of its seven days. The sweep re-decides it: every to-do
// of the line is over, so the question is over, and it says which fact ended
// it. The control beside it is a line still owed, which stays pending.
func TestASettledSubjectWithdrawsItsProposal(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	landed, owed := newWorkID(), newWorkID()
	sent(t, st, taskID(1), landed, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), landed, theRoot, clock.at)
	sent(t, st, taskID(2), owed, "custom", theRoot, clock.at)
	owes(t, st, taskID(2), owed, theRoot, clock.at)
	over, err := propose(p, landed, "")
	if err != nil {
		t.Fatal(err)
	}
	still, err := propose(p, owed, "")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing has happened to either subject yet.
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := stateOf(t, p, over.Proposal.ID); got != work.ProposalPending {
		t.Fatalf("withdrawn before its subject moved: %s", got)
	}
	clock.at = clock.at.Add(time.Hour)
	lands(t, st, taskID(1), landed, theRoot, clock.at)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, why := stateOf(t, p, over.Proposal.ID)
	if got != work.ProposalWithdrawn || why != work.WithdrawnSubjectSettled {
		t.Fatalf("a landed subject left its proposal %s (%s)", got, why)
	}
	if got, _ := stateOf(t, p, still.Proposal.ID); got != work.ProposalPending {
		t.Fatalf("control: a line still owed is %s", got)
	}
	// Withdrawing is not deleting: the row is still readable, under its own
	// state, and it is no longer in the "to confirm" area.
	page, err := p.ProposalList(ctx, work.ProposalWithdrawn, "", "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != over.Proposal.ID {
		t.Fatalf("withdrawn list: %+v %v", page.Rows, err)
	}
	if page.Counts[work.ProposalPending] != 1 || page.Counts[work.ProposalWithdrawn] != 1 {
		t.Fatalf("counts: %+v", page.Counts)
	}
	// And it is not a question any more: answering it is refused by name.
	_, err = p.Answer(ctx, over.Proposal.ID, work.AnswerTrack, "user", "local", nil, nil)
	if codeOf(err) != "proposal_withdrawn" {
		t.Fatalf("answering a withdrawn proposal: %v", err)
	}
}

// A root that inspected the subject may close its own pending proposal with
// the evidence it found. The row remains queryable under its own state, and
// that state is the gate's durable reason not to ask about the same line
// again. This is deliberately unlike `no`, which permits one new proposal on
// a new signal.
func TestAResolvedProposalIsKeptAndCannotBeProposedAgain(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line := newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), line, theRoot, clock.at)
	proposal, err := propose(p, line, "")
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := p.ResolveProposal(ctx, proposal.Proposal.ID, "The reported gap is already covered.",
		"internal/domain/work/proposals.go:417", "root:"+theRoot, theRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Proposal.State != work.ProposalResolved || resolved.Proposal.ResolutionEvidence == "" {
		t.Fatalf("resolved proposal: %+v", resolved.Proposal)
	}
	page, err := p.ProposalList(ctx, work.ProposalResolved, "", "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != proposal.Proposal.ID {
		t.Fatalf("resolved list: %+v %v", page.Rows, err)
	}
	if page.Counts[work.ProposalPending] != 0 || page.Counts[work.ProposalResolved] != 1 {
		t.Fatalf("counts: %+v", page.Counts)
	}

	// A genuinely new signal still does not revive a condition somebody
	// inspected and proved gone.
	_, err = p.Propose(ctx, ProposalRequest{Session: theRoot, WorkID: line, TaskID: taskID(1),
		Title: "same line", Project: "/p", Effects: []string{string(work.EffectPublish)}}, nil)
	if codeOf(err) != work.RefuseResolved {
		t.Fatalf("resolved line was proposed again: %v", err)
	}
}

// A rule's own proposal is withdrawn when the rules would not make it now:
// I1 alone is every dispatch there is. A person's counterpart is not — nobody
// withdraws a question somebody chose to ask.
func TestARuleWithdrawsWhatItWouldNoLongerPropose(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	line, mine := newWorkID(), newWorkID()
	sent(t, st, taskID(1), line, "custom", theRoot, clock.at)
	owes(t, st, taskID(1), line, theRoot, clock.at)
	sent(t, st, taskID(2), mine, "custom", theRoot, clock.at)
	owes(t, st, taskID(2), mine, theRoot, clock.at)
	byRule, err := p.Propose(ctx, ProposalRequest{Session: theRoot, WorkID: line, TaskID: taskID(1),
		Title: "made before the bar was raised", Project: "/p"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Written as the rules wrote it before the bar was raised.
	if err := p.Board.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		prev, err := tx.Proposal(byRule.Proposal.ID)
		if err != nil {
			return err
		}
		next := prev
		next.Source = work.SourceRule
		return tx.PutProposal(next, &prev, 0)
	}); err != nil {
		t.Skip("the store cannot restate a proposal's source")
	}
	chosen, err := propose(p, mine, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := stateOf(t, p, chosen.Proposal.ID); got != work.ProposalPending {
		t.Fatalf("a session's own proposal was withdrawn: %s", got)
	}
}
