package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// T2 (docs/design-decisions.md §6; board-redesign §9 step 2): the session
// to-do list, made, followed, handed off and dropped by the broker's facts
// alone. The criterion is failure injection — a child that dies, a root that
// dies, a landing before the result, a result.json sent twice, a restart —
// and every automatic transition is tested from both sides: the fact has not
// arrived and nothing moves; it arrives and it moves.

// todoClock is a settable clock on whole seconds, which is what the store
// keeps.
type todoClock struct{ at time.Time }

func (c *todoClock) now() time.Time          { return c.at }
func (c *todoClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTodoBroker(t *testing.T) (*Broker, context.Context, *todoClock) {
	t.Helper()
	b, ctx := newTestBroker(t)
	clock := &todoClock{at: time.Unix(1_789_700_000, 0)}
	b.Clock = clock.now
	return b, ctx, clock
}

// seen makes the broker's reading of the machine: the conversations named are
// running, beside one other assistant, and the process table answered
// completely or it did not.
func seen(b *Broker, complete bool, conversations ...string) {
	b.Reading = func(context.Context) session.Inventory {
		rows := []session.Session{{ID: "%90", Assistant: session.AssistantClaude, ConversationID: "c0ffee00-0000-4000-8000-000000000000"}}
		for i, c := range conversations {
			rows = append(rows, session.Session{ID: fmt.Sprintf("%%%d", i+1), Assistant: session.AssistantClaude, ConversationID: c})
		}
		return session.Inventory{Sessions: rows, Complete: complete, Sources: map[string]bool{"ps": complete}}
	}
}

// t2Task is a live task owned by the test root.
func t2Task(id string, claims []string, at time.Time) Record {
	return Record{Protocol: Protocol, ID: id, Assistant: "claude", Kind: "custom", Title: "t2 " + id[:8],
		State: StateBriefed, CreatedAt: at, Claims: claims, LeaseScope: LeaseShared, ProjectDir: "/tmp",
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
}

// admit records a task the way a dispatch does: create, with its to-do.
func admit(t *testing.T, b *Broker, ctx context.Context, r Record) {
	t.Helper()
	if _, err := b.create(ctx, r, HashSecret("s")); err != nil {
		t.Fatalf("create %s: %v", r.ID[:8], err)
	}
}

func todoOf(t *testing.T, b *Broker, ctx context.Context, taskID string) store.TodoRow {
	t.Helper()
	row, err := b.Store.Todo(ctx, work.TodoID(work.OriginDispatch, taskID))
	if err != nil {
		t.Fatalf("to-do of %s: %v", taskID[:8], err)
	}
	return row
}

func expectTodo(t *testing.T, b *Broker, ctx context.Context, taskID string, state work.TodoState, reason string) store.TodoRow {
	t.Helper()
	row := todoOf(t, b, ctx, taskID)
	if row.State != state || row.Reason != reason {
		t.Fatalf("to-do of %s is %s/%s, want %s/%s", taskID[:8], row.State, row.Reason, state, reason)
	}
	return row
}

// db opens the broker's file on a second handle, for counting and for
// injecting failures the broker cannot see coming.
func db(t *testing.T, b *Broker) *sql.DB {
	t.Helper()
	h, err := sql.Open("sqlite", filepath.Join(b.Dir, "clawdline.sqlite3")+"?_busy_timeout=2000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func count(t *testing.T, h *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func todoEvents(t *testing.T, h *sql.DB, taskID, kind string) int {
	return count(t, h, `SELECT COUNT(*) FROM events WHERE subject = ? AND kind = ?`,
		work.TodoID(work.OriginDispatch, taskID), kind)
}

func writeResult(t *testing.T, b *Broker, id, status string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"clawdline_protocol": 1, "task_id": id, "task_secret": "s",
		"status": status, "summary": "done"})
	if err := os.MkdirAll(b.Tasks.Path(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "result.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Creation: the dispatch's own write makes the to-do, and a write that cannot
// make it records no task either.
func TestADispatchOpensItsRootsTodoInTheSameWrite(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	h := db(t, b)
	id := "70d00001-0000-4000-8000-000000000001"
	admit(t, b, ctx, t2Task(id, []string{"a.go"}, clock.now()))
	row := expectTodo(t, b, ctx, id, work.TodoStateOpen, work.ReasonDispatched)
	if row.Owner != rootConversation || row.OwnerAssistant != "claude" || row.Task != id {
		t.Fatalf("to-do = %+v", row)
	}
	// The same dispatch again is refused and makes nothing twice.
	if _, err := b.create(ctx, t2Task(id, []string{"a.go"}, clock.now()), HashSecret("s")); refusalCode(err) != "task_exists" {
		t.Fatalf("a second create answered %v", err)
	}
	if n := count(t, h, `SELECT COUNT(*) FROM todos WHERE task_id = ?`, id); n != 1 || todoEvents(t, h, id, "todo.opened") != 1 {
		t.Fatalf("%d to-dos, %d opened events for one dispatch", n, todoEvents(t, h, id, "todo.opened"))
	}

	// Control: a task with no root owes no session anything.
	detached := t2Task("70d00002-0000-4000-8000-000000000002", nil, clock.now())
	detached.Root = nil
	admit(t, b, ctx, detached)
	if _, err := b.Store.Todo(ctx, work.TodoID(work.OriginDispatch, detached.ID)); !errors.Is(err, store.ErrNoTodo) {
		t.Fatalf("a detached task has a to-do: %v", err)
	}

	// Failure injection: the to-do cannot be written, so the dispatch is not
	// recorded at all — no task that nobody would be reminded of.
	if _, err := h.Exec(`CREATE TRIGGER no_todo BEFORE INSERT ON todos BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	lost := "70d00003-0000-4000-8000-000000000003"
	if _, err := b.create(ctx, t2Task(lost, nil, clock.now()), HashSecret("s")); err == nil {
		t.Fatal("a dispatch whose to-do could not be written was recorded")
	}
	if n := count(t, h, `SELECT COUNT(*) FROM broker_tasks WHERE id = ?`, lost); n != 0 {
		t.Fatal("the task was stored without its to-do")
	}
}

// Closing by the landing record, in the landing's own transaction: landed.
func TestALandingClosesItsTodoInTheSameWrite(t *testing.T) {
	b, ctx, _ := newTodoBroker(t)
	b.Clock = nil // the proof reads git, which keeps real time
	h := db(t, b)
	repo := gitRepo(t)
	base := gitIn(t, repo, "rev-parse", "HEAD")
	id := "70d00010-0000-4000-8000-000000000010"
	writeBrief(t, b, id, repo, map[string]any{"claims": []string{"a.go"}})
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Generation: inv.Generation, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Record.State.Terminal() {
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Delivered is not landed: still owed.
	expectTodo(t, b, ctx, id, work.TodoStateOpen, work.ReasonDispatched)

	// The fact does not arrive: a landing the proof refuses moves nothing.
	if _, err := land(b, ctx, id, "landed", "main", base); refusalCode(err) != "unverified_landing" {
		t.Fatalf("landing the base answered %v", err)
	}
	expectTodo(t, b, ctx, id, work.TodoStateOpen, work.ReasonDispatched)

	// Failure injection: the to-do's write fails, and the landing goes with
	// it — the two are one fact or neither.
	commit := commitFile(t, repo, "a.go", "package a // landed\n")
	if _, err := h.Exec(`CREATE TRIGGER no_close BEFORE UPDATE ON todos BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := land(b, ctx, id, "landed", "main", commit); err == nil {
		t.Fatal("a landing whose to-do could not close was recorded")
	}
	if r, _, _ := b.Record(ctx, id); r.Landing == nil || r.Landing.State != LandingPending {
		t.Fatalf("the landing was recorded without its to-do: %+v", r.Landing)
	}
	expectTodo(t, b, ctx, id, work.TodoStateOpen, work.ReasonDispatched)
	if _, err := h.Exec(`DROP TRIGGER no_close`); err != nil {
		t.Fatal(err)
	}

	// The fact arrives: closed, once.
	if _, err := land(b, ctx, id, "landed", "main", commit); err != nil {
		t.Fatalf("the real landing: %v", err)
	}
	expectTodo(t, b, ctx, id, work.TodoStateDone, work.ReasonLanded)
	if _, err := land(b, ctx, id, "landed", "main", commit); err != nil {
		t.Fatalf("the resend: %v", err)
	}
	if n := todoEvents(t, h, id, "todo.done"); n != 1 {
		t.Fatalf("%d done events for one landing", n)
	}
}

// Closing by the landing record: abandoned and nothing_to_land; and by a task
// that ends owing none.
func TestEachSettlementClosesItsTodo(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	machine := func(id, state string) error {
		_, err := b.Land(ctx, id, LandingRequest{State: state, Machine: true})
		return err
	}

	abandoned := "70d00020-0000-4000-8000-000000000020"
	admit(t, b, ctx, t2Task(abandoned, []string{"a.go"}, clock.now()))
	if _, err := b.Settle(ctx, abandoned, StateFailure, "", nil); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, abandoned, work.TodoStateOpen, work.ReasonDispatched)
	if err := machine(abandoned, "abandoned"); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, abandoned, work.TodoStateDone, work.ReasonAbandoned)

	nothing := "70d00021-0000-4000-8000-000000000021"
	admit(t, b, ctx, t2Task(nothing, []string{}, clock.now()))
	// A landing obligation named while it runs is still an obligation.
	if err := machine(nothing, "pending"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(ctx, nothing, StateSuccess, "", nil); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, nothing, work.TodoStateOpen, work.ReasonDispatched)
	if err := machine(nothing, "nothing_to_land"); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, nothing, work.TodoStateDone, work.ReasonNothingToLand)

	owedNone := "70d00022-0000-4000-8000-000000000022"
	admit(t, b, ctx, t2Task(owedNone, []string{}, clock.now()))
	expectTodo(t, b, ctx, owedNone, work.TodoStateOpen, work.ReasonDispatched)
	if _, err := b.Settle(ctx, owedNone, StateSuccess, "", nil); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, owedNone, work.TodoStateDone, work.ReasonNothingOwed)

	// A landing obligation opened on it afterwards reopens it; settling that
	// closes it again.
	if err := machine(owedNone, "pending"); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, owedNone, work.TodoStateOpen, work.ReasonObligationOpened)
	if err := machine(owedNone, "abandoned"); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, owedNone, work.TodoStateDone, work.ReasonAbandoned)
}

// board-redesign §9: "a child dies". The beat's own clock ends the task; one that
// owes a landing stays owed, one that owes nothing closes.
func TestAChildThatDiesLeavesItsTodoOwedOrClosed(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	seen(b, true, rootConversation)
	owing := t2Task("70d00030-0000-4000-8000-000000000030", []string{"a.go"}, clock.now())
	owing.TimeoutMinutes = 1
	free := t2Task("70d00031-0000-4000-8000-000000000031", []string{}, clock.now())
	free.TimeoutMinutes = 1
	admit(t, b, ctx, owing)
	admit(t, b, ctx, free)

	// The fact has not arrived: inside its minute, nothing moves.
	clock.advance(30 * time.Second)
	b.Pass(ctx)
	expectTodo(t, b, ctx, owing.ID, work.TodoStateOpen, work.ReasonDispatched)
	expectTodo(t, b, ctx, free.ID, work.TodoStateOpen, work.ReasonDispatched)

	clock.advance(time.Minute)
	if p := b.Pass(ctx); p.TimedOut != 2 {
		t.Fatalf("the beat timed out %d tasks, want 2", p.TimedOut)
	}
	expectTodo(t, b, ctx, owing.ID, work.TodoStateOpen, work.ReasonDispatched)
	expectTodo(t, b, ctx, free.ID, work.TodoStateDone, work.ReasonNothingOwed)
}

// board-redesign §9: "root dies". Handed off only on a reading that positively
// says the root is gone; back when it returns; dropped only after the grace
// with the root still positively gone.
func TestARootThatDiesHandsItsTodoOff(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	h := db(t, b)
	r := t2Task("70d00040-0000-4000-8000-000000000040", []string{"a.go"}, clock.now())
	admit(t, b, ctx, r)

	pass := func() Pulse { clock.advance(time.Second); return b.Pass(ctx) }

	seen(b, true, rootConversation)
	pass()
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonDispatched)
	// Gone from a reading whose process table did not answer completely:
	// unknown, and unknown moves nothing.
	seen(b, false)
	pass()
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonDispatched)
	// A reading that is not fresh has not said anybody left either.
	seen(b, true)
	b.observed.mu.Lock()
	b.observed.seen.at = clock.now().Add(-time.Hour)
	b.observed.mu.Unlock()
	if got := b.ownerLiveness(rootConversation, "claude"); got != work.Unknown {
		t.Fatalf("a stale reading placed the root as %s", got)
	}

	// Positively gone.
	if p := pass(); p.Todos != 1 {
		t.Fatalf("the pass moved %d to-dos, want 1", p.Todos)
	}
	gone := expectTodo(t, b, ctx, r.ID, work.TodoStateHandedOff, work.ReasonOwnerGone)
	if gone.HandedOffAt.IsZero() || todoEvents(t, h, r.ID, "todo.handed_off") != 1 {
		t.Fatalf("handed off %+v with %d events", gone, todoEvents(t, h, r.ID, "todo.handed_off"))
	}
	// The same reading again writes nothing.
	if p := pass(); p.Todos != 0 || todoEvents(t, h, r.ID, "todo.handed_off") != 1 {
		t.Fatalf("an unchanged reading moved %d to-dos", p.Todos)
	}

	// Back again: its own again.
	seen(b, true, rootConversation)
	pass()
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonOwnerReturned)

	// Gone again, and the grace.
	seen(b, true)
	pass()
	handed := expectTodo(t, b, ctx, r.ID, work.TodoStateHandedOff, work.ReasonOwnerGone)
	clock.at = handed.HandedOffAt.Add(work.UnownedGrace - time.Minute)
	b.Pass(ctx)
	expectTodo(t, b, ctx, r.ID, work.TodoStateHandedOff, work.ReasonOwnerGone)
	// Past the grace, on a reading that cannot say: kept.
	clock.at = handed.HandedOffAt.Add(work.UnownedGrace + time.Minute)
	seen(b, false)
	b.Pass(ctx)
	expectTodo(t, b, ctx, r.ID, work.TodoStateHandedOff, work.ReasonOwnerGone)
	// Past the grace and positively gone: dropped. The task and its landing
	// obligation are untouched — only the reminder stops.
	seen(b, true)
	pass()
	expectTodo(t, b, ctx, r.ID, work.TodoStateDropped, work.ReasonUnowned)
	after, _, err := b.Record(ctx, r.ID)
	if err != nil || after.State != StateBriefed {
		t.Fatalf("dropping the to-do touched the task: %v %s", err, after.State)
	}
}

// board-redesign §9: "the landing arrives before the result". A landing named before the result is an
// obligation; a settled landing before the task ended is refused and moves
// nothing; a result that arrives after the landing settled changes nothing.
func TestALandingBeforeTheResult(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	h := db(t, b)
	r := t2Task("70d00050-0000-4000-8000-000000000050", []string{"a.go"}, clock.now())
	r.TimeoutMinutes = 1
	admit(t, b, ctx, r)
	if _, err := b.Land(ctx, r.ID, LandingRequest{State: "pending", Machine: true}); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonDispatched)
	if _, err := b.Land(ctx, r.ID, LandingRequest{State: "abandoned", Machine: true}); refusalCode(err) != "not_terminal" {
		t.Fatalf("settling a live task's landing answered %v", err)
	}
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonDispatched)

	// The child dies; the root settles the landing; then the result appears.
	clock.advance(2 * time.Minute)
	b.Pass(ctx)
	if _, err := b.Land(ctx, r.ID, LandingRequest{State: "abandoned", Machine: true}); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, r.ID, work.TodoStateDone, work.ReasonAbandoned)
	writeResult(t, b, r.ID, "success")
	if err := b.Complete(ctx, r.ID, "s"); refusalCode(err) != "already_done" {
		t.Fatalf("the late result answered %v", err)
	}
	b.Pass(ctx)
	expectTodo(t, b, ctx, r.ID, work.TodoStateDone, work.ReasonAbandoned)
	if n := count(t, h, `SELECT COUNT(*) FROM todos WHERE task_id = ?`, r.ID); n != 1 || todoEvents(t, h, r.ID, "todo.done") != 1 {
		t.Fatalf("%d to-dos and %d done events", n, todoEvents(t, h, r.ID, "todo.done"))
	}
}

// board-redesign §9: "`result.json` is sent twice". Collected once, by whichever of
// the beat and /complete gets there first; the to-do moves once.
func TestAResultSentTwiceMovesTheTodoOnce(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	h := db(t, b)
	r := t2Task("70d00060-0000-4000-8000-000000000060", []string{}, clock.now())
	admit(t, b, ctx, r)
	b.Pass(ctx)
	expectTodo(t, b, ctx, r.ID, work.TodoStateOpen, work.ReasonDispatched)

	writeResult(t, b, r.ID, "success")
	if err := b.Complete(ctx, r.ID, "s"); err != nil {
		t.Fatal(err)
	}
	writeResult(t, b, r.ID, "success")
	if err := b.Complete(ctx, r.ID, "s"); refusalCode(err) != "already_done" {
		t.Fatalf("the second /complete answered %v", err)
	}
	b.Pass(ctx)
	b.Pass(ctx)
	expectTodo(t, b, ctx, r.ID, work.TodoStateDone, work.ReasonNothingOwed)
	if n := count(t, h, `SELECT COUNT(*) FROM todos WHERE task_id = ?`, r.ID); n != 1 {
		t.Fatalf("%d to-dos", n)
	}
	if o, d := todoEvents(t, h, r.ID, "todo.opened"), todoEvents(t, h, r.ID, "todo.done"); o != 1 || d != 1 {
		t.Fatalf("opened %d, done %d", o, d)
	}
}

// Restart: a to-do the store was written without is made when the daemon
// starts, one whose fact was recorded behind it is followed, and nothing is
// made twice. A start costs what is still owed, not the history: every task
// with a root is examined once in its life, and one with no root never.
func TestARestartNeitherMissesNorDuplicates(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	dir := b.Dir
	kept := t2Task("70d00070-0000-4000-8000-000000000070", []string{"a.go"}, clock.now())
	admit(t, b, ctx, kept)
	// Written by a writer that kept no to-dos — the store before T2, or a
	// crash window — while still owed, and one that is over.
	missed := t2Task("70d00071-0000-4000-8000-000000000071", []string{"a.go"}, clock.now())
	if err := b.save(ctx, missed, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	over := t2Task("70d00072-0000-4000-8000-000000000072", []string{}, clock.now())
	over.State = StateSuccess
	if err := b.save(ctx, over, HashSecret("s"), "task.success"); err != nil {
		t.Fatal(err)
	}
	// History: tasks that ended long ago, with a root and without one.
	const history = 40
	for i := 0; i < history; i++ {
		old := t2Task(fmt.Sprintf("70d2%04d-0000-4000-8000-000000000000", i), []string{}, clock.now())
		old.State = StateSuccess
		if i%2 == 1 {
			old.Root = nil
		}
		if err := b.save(ctx, old, HashSecret("s"), "task.success"); err != nil {
			t.Fatal(err)
		}
	}
	// A fact recorded behind an existing to-do's back.
	behind := t2Task("70d00073-0000-4000-8000-000000000073", []string{"a.go"}, clock.now())
	admit(t, b, ctx, behind)
	settled := behind
	settled.State, settled.Landing = StateFailure, &Landing{State: LandingAbandoned}
	if _, _, err := b.Store.UpdateBrokerTask(ctx, behind.ID, func(_ *store.Tx, row store.BrokerRow) (*store.BrokerWrite, error) {
		return &store.BrokerWrite{Row: b.storedRow(settled)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	expectTodo(t, b, ctx, behind.ID, work.TodoStateOpen, work.ReasonDispatched)
	before := todoOf(t, b, ctx, kept.ID)
	// A pass nobody started owes no reconcile: a beat's cost is the live
	// tasks (G33), and a missing to-do waits for the start that owes it.
	if p := b.Pass(ctx); p.Todos != 0 {
		t.Fatalf("an ordinary pass reconciled %d to-dos", p.Todos)
	}
	if err := b.Store.Close(); err != nil {
		t.Fatal(err)
	}

	// The restart: a new store handle, a new broker, started as the daemon
	// starts it (Run owes the reconcile), and its first pass.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	start := func() *Broker {
		nb := &Broker{Store: st, Tasks: taskdir.New(dir), Git: git.New(), Dir: dir, Clock: clock.now}
		seen(nb, true, rootConversation)
		stopped, cancel := context.WithCancel(ctx)
		cancel()
		nb.Run(stopped, time.Hour, nil)
		return nb
	}
	b2 := start()
	// Two more than the to-dos it makes and follows: the two still owed were
	// stored on no line, and a start binds each to the line its dispatch
	// would have been given (lines.go) — the one that is over is left alone.
	if p := b2.Pass(ctx); p.Todos != 3+history/2+2 {
		t.Fatalf("the first pass moved %d to-dos, want %d (one made open, the rest recorded over, one followed, two bound)",
			p.Todos, 3+history/2+2)
	}
	if got := expectTodo(t, b2, ctx, missed.ID, work.TodoStateOpen, work.ReasonDispatched); got.WorkID != missed.ID {
		t.Fatalf("the missed to-do is on line %q", got.WorkID)
	}
	if got := expectTodo(t, b2, ctx, over.ID, work.TodoStateDone, work.ReasonNothingOwed); got.WorkID != "" {
		t.Fatalf("a to-do that is over was bound: %q", got.WorkID)
	}
	expectTodo(t, b2, ctx, behind.ID, work.TodoStateDone, work.ReasonAbandoned)
	if after := todoOf(t, b2, ctx, kept.ID); after.Version != before.Version+1 || after.State != before.State ||
		after.WorkID != kept.ID {
		t.Fatalf("the kept to-do was rewritten beyond its binding: %+v -> %+v", before, after)
	}
	if _, err := st.Todo(ctx, work.TodoID(work.OriginDispatch, "70d20001-0000-4000-8000-000000000000")); !errors.Is(err, store.ErrNoTodo) {
		t.Fatalf("a task with no root was given a to-do: %v", err)
	}
	// Again, and on another start: nothing moves, and the start reads only
	// what is still owed — the two open to-dos' tasks, beside the beat's own
	// read of the live ones — whatever the history holds.
	if p := b2.Pass(ctx); p.Todos != 0 {
		t.Fatalf("the second pass moved %d", p.Todos)
	}
	// The control: what an ordinary pass reads — the live tasks, twice.
	ordinary, _ := st.ReadCounts()
	b2.Pass(ctx)
	after, _ := st.ReadCounts()
	b3 := start()
	if p := b3.Pass(ctx); p.Todos != 0 {
		t.Fatalf("a second start moved %d", p.Todos)
	}
	read, _ := st.ReadCounts()
	if extra := (read - after) - (after - ordinary); extra != 2 {
		t.Fatalf("a start read %d task records beyond an ordinary pass, with 2 still owed among %d",
			extra, history+4)
	}
	h := db(t, b2)
	if n := count(t, h, `SELECT COUNT(*) FROM todos`); n != 4+history/2 {
		t.Fatalf("%d to-dos, want %d", n, 4+history/2)
	}
	if n := count(t, h, `SELECT COUNT(*) FROM events WHERE kind = 'todo.opened'`); n != 3 {
		t.Fatalf("%d opened events, want 3", n)
	}
	if n := count(t, h, `SELECT COUNT(*) FROM events WHERE kind IN ('task.bound','todo.bound')`); n != 4 {
		t.Fatalf("%d binding events, want 4: two tasks and their two to-dos, once each", n)
	}
}

// #5: not asked, not pushed, not on the board.
func TestTodosNeverReachAPerson(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	pushes := 0
	b.Push = func(context.Context, string, string, string, string) (int, int, error) { pushes++; return 1, 0, nil }
	h := db(t, b)
	r := t2Task("70d00080-0000-4000-8000-000000000080", []string{"a.go"}, clock.now())
	admit(t, b, ctx, r)
	seen(b, true)
	for i := 0; i < 3; i++ {
		clock.advance(work.UnownedGrace)
		b.Pass(ctx)
	}
	expectTodo(t, b, ctx, r.ID, work.TodoStateDropped, work.ReasonUnowned)
	if pushes != 0 || count(t, h, `SELECT COUNT(*) FROM broker_notifications`) != 0 {
		t.Fatalf("a to-do pushed %d notifications", pushes)
	}
	if n := count(t, h, `SELECT COUNT(*) FROM events WHERE kind LIKE 'todo.%' AND kind NOT IN
		('todo.opened','todo.open','todo.handed_off','todo.done','todo.dropped')`); n != 0 {
		t.Fatalf("%d to-do events of a kind the list does not have", n)
	}
}

// D36: a dispatch carries its work item, and a respawn is the same work.
func TestADispatchCarriesItsWorkID(t *testing.T) {
	b, ctx, _ := newTodoBroker(t)
	b.Clock = nil
	repo := gitRepo(t)
	workID := "0f0f0f0f-1234-4000-8000-00000000abcd"
	// The item that dispatch names, because a dispatch may name only an item
	// there is (BD-4): the line it carries is that item's from here on.
	plannedItem(t, b, ctx, workID, repo, time.Now())
	dispatch := func(id string, extra map[string]any) (Dispatched, error) {
		writeBrief(t, b, id, repo, extra)
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		return b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Generation: inv.Generation, Offered: true})
	}
	for _, bad := range []any{"not-a-uuid", 42, "0F0F0F0F-1234-4000-8000-00000000ABCD"} {
		id := "70d00090-0000-4000-8000-00000000009" + fmt.Sprint(len(fmt.Sprint(bad))%10)
		if _, err := dispatch(id, map[string]any{"work_id": bad}); refusalCode(err) != "bad_task" {
			t.Fatalf("work_id %v answered %v", bad, err)
		}
	}
	id := "70d000a0-0000-4000-8000-0000000000a0"
	out, err := dispatch(id, map[string]any{"work_id": workID})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.WorkID != workID || todoOf(t, b, ctx, id).WorkID != workID {
		t.Fatalf("record %q, to-do %q", out.Record.WorkID, todoOf(t, b, ctx, id).WorkID)
	}
	var brief map[string]any
	body, _ := os.ReadFile(filepath.Join(b.Tasks.Path(id), "task.json"))
	if err := json.Unmarshal(body, &brief); err != nil || brief["work_id"] != workID {
		t.Fatalf("task.json lost the work id: %v %v", brief["work_id"], err)
	}
	if out.Record.WorkFrom != work.WorkNamed {
		t.Fatalf("a named line says it came from %q", out.Record.WorkFrom)
	}
	// None named: the dispatch begins a line of its own, named by its task,
	// and its to-do is on it from the transaction that made it (D36).
	plain := "70d000a1-0000-4000-8000-0000000000a1"
	if out, err := dispatch(plain, nil); err != nil || out.Record.WorkID != plain || out.Record.WorkFrom != work.WorkDispatch ||
		todoOf(t, b, ctx, plain).WorkID != plain {
		t.Fatalf("a dispatch with no work id carries %q from %q (%v)", out.Record.WorkID, out.Record.WorkFrom, err)
	}
	// Control: a step of other work nobody placed is on no line.
	step := "70d000a2-0000-4000-8000-0000000000a2"
	if out, err := dispatch(step, map[string]any{"kind": "code-review"}); err != nil || out.Record.WorkID != "" ||
		todoOf(t, b, ctx, step).WorkID != "" {
		t.Fatalf("an unplaced review carries %q (%v)", out.Record.WorkID, err)
	}
	// A respawn copies the brief, work id and all.
	if out.Record.State != StateSpawnFailed {
		t.Skipf("the test tab opened (%s); nothing to respawn", out.Record.State)
	}
	again, err := b.Respawn(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Record.WorkID != workID || todoOf(t, b, ctx, again.Record.ID).WorkID != workID {
		t.Fatalf("the respawn lost the work id: %q", again.Record.WorkID)
	}
}

// §5.2 (3): the session reads its list by its conversation id; a terminal id
// is refused by name rather than answered with an empty list.
func TestASessionReadsItsOwnTodos(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	for i := 0; i < todoPageLimit+2; i++ {
		clock.advance(time.Second)
		admit(t, b, ctx, t2Task(fmt.Sprintf("70d1%04d-0000-4000-8000-000000000000", i), []string{}, clock.now()))
	}
	closed := "70d10000-0000-4000-8000-000000000000"
	if _, err := b.Settle(ctx, closed, StateSuccess, "", nil); err != nil {
		t.Fatal(err)
	}
	page, err := b.SessionTodos(ctx, rootConversation, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Todos) != todoPageLimit || page.Next == "" || page.Counts[work.TodoStateOpen] != todoPageLimit+1 ||
		page.Counts[work.TodoStateDone] != 1 || page.Filter != TodosOutstanding {
		t.Fatalf("page: %d rows, next %q, counts %v", len(page.Todos), page.Next, page.Counts)
	}
	if got := page.Todos[0].Escalation; len(got) != 1 || got[0] != work.SignalCrossSession {
		t.Fatalf("escalation %v", got)
	}
	rest, err := b.SessionTodos(ctx, rootConversation, "", page.Next)
	if err != nil || len(rest.Todos) != 1 || rest.Next != "" {
		t.Fatalf("second page: %d rows, next %q, %v", len(rest.Todos), rest.Next, err)
	}
	if rest.Todos[0].ID == page.Todos[len(page.Todos)-1].ID {
		t.Fatal("the cursor repeated a row")
	}
	done, err := b.SessionTodos(ctx, rootConversation, TodosClosed, "")
	if err != nil || len(done.Todos) != 1 || done.Todos[0].Task != closed || len(done.Todos[0].Escalation) != 0 {
		t.Fatalf("closed: %+v %v", done.Todos, err)
	}
	if _, err := b.SessionTodos(ctx, rootConversation, "later", ""); refusalCode(err) != "bad_request" {
		t.Fatalf("an unknown filter answered %v", err)
	}
	if _, err := b.SessionTodos(ctx, rootConversation, "", "nonsense"); refusalCode(err) != "bad_request" {
		t.Fatalf("a foreign cursor answered %v", err)
	}
	// "%1" is the root's terminal in this broker's reading.
	if _, err := b.SessionTodos(ctx, "%1", "", ""); refusalCode(err) != "session_id_is_terminal" {
		t.Fatalf("a terminal id answered %v", err)
	}
	// Control: a conversation that owes nothing is an empty list, not an error.
	empty, err := b.SessionTodos(ctx, "9e9e9e9e-0000-4000-8000-000000000000", "", "")
	if err != nil || len(empty.Todos) != 0 {
		t.Fatalf("a session that owes nothing: %+v %v", empty, err)
	}
}

// D36: a task stored on no line is bound once, and its to-do with it, in one
// transaction; binding it again to the same line writes nothing, and to
// another is refused. Its to-do says cross_session until a work item holds
// the line — being on a line is not being followed.
func TestBindingPutsATaskAndItsToDoOnALine(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	legacy := t2Task("70d000b0-0000-4000-8000-0000000000b0", []string{"a.go"}, clock.now())
	admit(t, b, ctx, legacy)
	if got := todoOf(t, b, ctx, legacy.ID); got.WorkID != "" {
		t.Fatalf("admitted without a dispatch, the to-do is on %q", got.WorkID)
	}
	line, from, bound := LineOf(legacy)
	if bound || line != legacy.ID || from != work.WorkDispatch {
		t.Fatalf("the rules put it on %q from %q (bound %v)", line, from, bound)
	}
	if err := b.BindWork(ctx, legacy.ID, line, from); err != nil {
		t.Fatal(err)
	}
	r, _, err := b.Record(ctx, legacy.ID)
	if err != nil || r.WorkID != line || r.WorkFrom != work.WorkDispatch {
		t.Fatalf("the record: %q %q %v", r.WorkID, r.WorkFrom, err)
	}
	first := todoOf(t, b, ctx, legacy.ID)
	if first.WorkID != line || first.State != work.TodoStateOpen {
		t.Fatalf("the to-do: %+v", first.Todo)
	}
	if err := b.BindWork(ctx, legacy.ID, line, from); err != nil {
		t.Fatalf("the same line again: %v", err)
	}
	if again := todoOf(t, b, ctx, legacy.ID); again.Version != first.Version {
		t.Fatal("binding the same line again wrote the to-do")
	}
	if err := b.BindWork(ctx, legacy.ID, "0f0f0f0f-0000-4000-8000-0000000000b1", work.WorkNamed); refusalCode(err) != "work_id_mismatch" {
		t.Fatalf("another line: %v", err)
	}
	page, err := b.SessionTodos(ctx, rootConversation, TodosOutstanding, "")
	if err != nil || len(page.Todos) != 1 {
		t.Fatalf("the list: %+v %v", page, err)
	}
	if s := page.Todos[0].Escalation; len(s) != 1 || s[0] != work.SignalCrossSession {
		t.Fatalf("a line no work item holds carries %v", s)
	}
}

// A task stored on no line that a proposal was already made about — before
// the broker bound lines — is bound to the line that proposal named, so a
// work item a person made from it follows the task; not to a line of the
// rules' own that the item would never see.
func TestAStartBindsATaskToTheLineAProposalNamed(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	legacy := t2Task("70d000c0-0000-4000-8000-0000000000c0", []string{"a.go"}, clock.now())
	admit(t, b, ctx, legacy)
	named := "0f0f0f0f-0000-4000-8000-0000000000c1"
	if err := b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		return tx.PutProposal(work.Proposal{ID: "0f0f0f0f-0000-4000-8000-0000000000c2", WorkID: named, TaskID: legacy.ID,
			Session: rootConversation, Source: work.SourceSession, Project: "/tmp", Title: "t", Signals: []work.Signal{},
			Effects: []work.Effect{}, AskReason: work.AskHumanAbsent, Channel: work.ChannelToConfirm,
			State: work.ProposalAnswered, Answer: work.AnswerTrack, AnsweredBy: "user", AnsweredAt: clock.now(),
			CreatedAt: clock.now(), ExpiresAt: clock.now().Add(time.Hour)}, nil, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReconcileTodos(ctx); err != nil {
		t.Fatal(err)
	}
	r, _, err := b.Record(ctx, legacy.ID)
	if err != nil || r.WorkID != named || r.WorkFrom != work.WorkProposal {
		t.Fatalf("bound to %q from %q (%v)", r.WorkID, r.WorkFrom, err)
	}
	if got := todoOf(t, b, ctx, legacy.ID); got.WorkID != named {
		t.Fatalf("the to-do is on %q", got.WorkID)
	}
	// Control: one nobody proposed is on its own line.
	plain := t2Task("70d000c3-0000-4000-8000-0000000000c3", []string{"a.go"}, clock.now())
	admit(t, b, ctx, plain)
	if _, err := b.ReconcileTodos(ctx); err != nil {
		t.Fatal(err)
	}
	if got := todoOf(t, b, ctx, plain.ID); got.WorkID != plain.ID {
		t.Fatalf("the control is on %q", got.WorkID)
	}
}
