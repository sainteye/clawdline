package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The board against a real store: the sweep follows the broker's rows, each
// change is one transaction with its move, a pass with nothing new writes
// nothing, a task nobody can read moves nothing, and a person's command is
// filed under its receipt in the transaction that made it.

type boardClock struct{ at time.Time }

func (c *boardClock) now() time.Time { return c.at }

func newBoard(t *testing.T) (*WorkBoard, *store.Store, *boardClock) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clock := &boardClock{at: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	w := NewWorkBoard(st)
	w.Now = clock.now
	w.Policy.Location = time.UTC
	return w, st, clock
}

func putTask(t *testing.T, st *store.Store, r orchestrator.Record) {
	t.Helper()
	ctx := context.Background()
	body, _ := json.Marshal(r)
	row, err := st.BrokerTask(ctx, r.ID)
	switch {
	case errors.Is(err, store.ErrNoTask):
		if _, err := st.CreateBrokerTask(ctx, store.BrokerRow{ID: r.ID, Project: "/p", Assistant: "claude",
			State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
			t.Fatal(err)
		}
	case err != nil:
		t.Fatal(err)
	default:
		row.Record, row.State, row.Texts = body, string(r.State), nil
		if err := st.SaveBrokerTask(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func moves(t *testing.T, st *store.Store, id string) []store.WorkMove {
	t.Helper()
	m, err := st.WorkMoves(context.Background(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheSweepFollowsTheBrokerIntoTheBoardAndOut(t *testing.T) {
	w, st, clock := newBoard(t)
	ctx := context.Background()
	item, err := w.Create(ctx, NewWork{Title: "ship the thing", Project: "/p", Place: work.PlaceBacklog,
		Actor: "user", Principal: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := item.Item.ID
	// Nothing names it: a pass moves nothing and writes nothing.
	if p := w.Sweep(ctx); p.Moved != 0 || p.Err != "" {
		t.Fatalf("an empty pass: %+v", p)
	}
	// A dispatch names it: it is on the board, and the move says who and why.
	clock.at = clock.at.Add(time.Hour)
	task := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000001", Kind: "custom", Title: "do it",
		WorkID: id, State: orchestrator.StateBriefed, CreatedAt: clock.at,
		Root: &orchestrator.RootRef{SessionID: "root-conv", Assistant: "claude"}}
	putTask(t, st, task)
	clock.at = clock.at.Add(time.Minute)
	if p := w.Sweep(ctx); p.Moved != 1 || p.Err != "" {
		t.Fatalf("the dispatch pass: %+v", p)
	}
	v, _ := w.Item(ctx, id)
	if v.Item.Place != work.PlaceBoard || v.Item.Owner != "root-conv" || v.Derived.Reason != work.ReasonTaskRunning {
		t.Fatalf("after the dispatch: %+v", v.Item)
	}
	m := moves(t, st, id)
	if len(m) != 2 || m[1].Trigger != work.TriggerDispatched || m[1].Actor != "root:root-conv" ||
		m[1].From != work.PlaceBacklog || m[1].To != work.PlaceBoard {
		t.Fatalf("moves %+v", m)
	}
	// The task delivers and lands; the read says so before any sweep has
	// recorded it, and one pass records it.
	task.State, task.FinishedAt = orchestrator.StateSuccess, clock.at
	task.Landing = &orchestrator.Landing{State: orchestrator.LandingLanded, At: clock.at, Commit: "abc"}
	putTask(t, st, task)
	if v, _ := w.Item(ctx, id); v.Derived.State != work.ItemDone || !v.Derived.Landed || v.Item.State != work.ItemActive {
		t.Fatalf("the read before the sweep: derived %+v stored %s", v.Derived, v.Item.State)
	}
	if p := w.Sweep(ctx); p.Moved != 1 {
		t.Fatalf("the landing pass: %+v", p)
	}
	v, _ = w.Item(ctx, id)
	if v.Item.State != work.ItemDone || v.Item.ClosedReason != work.ClosedLanded {
		t.Fatalf("after the landing: %+v", v.Item)
	}
	// Nothing new: nothing written, however many passes.
	before := len(moves(t, st, id))
	for i := 0; i < 5; i++ {
		clock.at = clock.at.Add(24 * time.Hour)
		if p := w.Sweep(ctx); p.Moved != 0 {
			t.Fatalf("pass %d moved %d", i, p.Moved)
		}
	}
	if after := len(moves(t, st, id)); after != before {
		t.Fatalf("idle passes wrote %d moves", after-before)
	}
	// It is on the board's done section inside the window, and off it after.
	page, err := w.Board(ctx, "/p", work.SectionDone, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Counts[work.SectionDone] != 1 || len(page.Rows) != 1 {
		t.Fatalf("done section inside the window: %+v", page.Counts)
	}
	clock.at = clock.at.Add(8 * 24 * time.Hour)
	if page, _ := w.Board(ctx, "/p", "", ""); len(page.Rows) != 0 {
		t.Fatalf("a closed item stayed on the page: %d", len(page.Rows))
	}
}

// A bound task whose record cannot be read might be the one still running:
// the item is left where it is, and the pass says why (DG-7).
func TestATaskNobodyCanReadMovesNothing(t *testing.T) {
	w, st, clock := newBoard(t)
	ctx := context.Background()
	v, err := w.Create(ctx, NewWork{Title: "t", Project: "/p", Place: work.PlaceBoard, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bad := `{"task_id":"7a5c0000-0000-4000-8000-000000000009","work_id":"` + v.Item.ID + `","created_at":"not a time"}`
	if _, err := st.CreateBrokerTask(ctx, store.BrokerRow{ID: "7a5c0000-0000-4000-8000-000000000009", Project: "/p",
		Assistant: "claude", State: "briefed", CreatedAt: clock.at, SecretHash: "h", Record: []byte(bad)}, nil); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(10 * 24 * time.Hour)
	p := w.Sweep(ctx)
	if p.Moved != 0 || p.Unknown != 1 {
		t.Fatalf("pass %+v", p)
	}
	got, _ := w.Item(ctx, v.Item.ID)
	if got.Item.Place != work.PlaceBoard || got.Unknown != 1 {
		t.Fatalf("item %+v unknown %d", got.Item, got.Unknown)
	}
	// Control: the same item with the row readable and failed goes back after
	// the window — it is the unknown row, not the clock, that held it.
	good := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000009", WorkID: v.Item.ID,
		State: orchestrator.StateFailure, CreatedAt: clock.at.Add(-10 * 24 * time.Hour)}
	putTask(t, st, good)
	if p := w.Sweep(ctx); p.Moved != 1 {
		t.Fatalf("control pass %+v", p)
	}
	if m := moves(t, st, v.Item.ID); m[len(m)-1].Trigger != work.TriggerStalled {
		t.Fatalf("control move %+v", m[len(m)-1])
	}
}

// A person's command and its receipt are one transaction; a refusal writes
// neither; a stale version is refused with the item as it is.
func TestACommandIsFiledWithTheChangeItMade(t *testing.T) {
	w, st, clock := newBoard(t)
	ctx := context.Background()
	v, err := w.Create(ctx, NewWork{Title: "t", Project: "/p", Place: work.PlaceBacklog, Actor: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Item.ID
	k := store.ReceiptKey{Scope: "work", Actor: "local", Key: "k1"}
	if c, err := st.ClaimReceipt(ctx, k, "digest", store.ReceiptPolicy{}, clock.at); err != nil || c.Outcome != store.ReceiptNew {
		t.Fatalf("claim %+v %v", c, err)
	}
	file := func(v WorkView) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		body, _ := json.Marshal(map[string]any{"place": v.Item.Place, "version": v.Item.Version})
		return k, store.ReceiptAnswer{Status: 200, Body: body}, true
	}
	got, err := w.Command(ctx, id, WorkCommand{Command: work.Command{Op: work.OpStart, Actor: "user", Owner: "root-conv"},
		Principal: "local"}, file)
	if err != nil || got.Item.Place != work.PlaceBoard || got.Item.Version != 1 {
		t.Fatalf("start %+v %v", got.Item, err)
	}
	again, err := st.ClaimReceipt(ctx, k, "digest", store.ReceiptPolicy{}, clock.at)
	if err != nil || again.Outcome != store.ReceiptReplay || string(again.Answer.Body) != `{"place":"board","version":1}` {
		t.Fatalf("replay %+v %v", again, err)
	}
	m := moves(t, st, id)
	var evidence map[string]any
	_ = json.Unmarshal(m[len(m)-1].Evidence, &evidence)
	if m[len(m)-1].Actor != "user" || evidence["principal"] != "local" || evidence["owner"] != "root-conv" {
		t.Fatalf("the move %+v %v", m[len(m)-1], evidence)
	}
	// A refusal writes nothing: no move, no version, no receipt.
	k2 := store.ReceiptKey{Scope: "work", Actor: "local", Key: "k2"}
	_, _ = st.ClaimReceipt(ctx, k2, "d2", store.ReceiptPolicy{}, clock.at)
	_, err = w.Command(ctx, id, WorkCommand{Command: work.Command{Op: work.OpAccept, Actor: "user"}},
		func(WorkView) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			return k2, store.ReceiptAnswer{Status: 200, Body: []byte(`{}`)}, true
		})
	var we *WorkError
	if !errors.As(err, &we) || we.Code != "not_awaiting_closure" {
		t.Fatalf("accept with nothing delivered: %v", err)
	}
	if after, _ := w.Item(ctx, id); after.Item.Version != 1 || len(moves(t, st, id)) != len(m) {
		t.Fatalf("a refusal wrote: version %d", after.Item.Version)
	}
	// A stale version is refused and handed the item as it is.
	stale := int64(0)
	_, err = w.Command(ctx, id, WorkCommand{Command: work.Command{Op: work.OpDefer, Actor: "user"}, ExpectedVersion: &stale}, nil)
	if !errors.As(err, &we) || we.Code != "version_conflict" || we.Current == nil || we.Current.Item.Version != 1 {
		t.Fatalf("stale: %v", err)
	}
}

// work.open: at the limit a new item is refused and nothing is let go.
func TestTheOpenLimitRefusesAndLetsNothingGo(t *testing.T) {
	w, _, _ := newBoard(t)
	w.OpenLimit = 2
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := w.Create(ctx, NewWork{Title: "t", Project: "/p", Place: work.PlaceBacklog, Actor: "user"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	_, err := w.Create(ctx, NewWork{Title: "t", Project: "/p", Place: work.PlaceBoard, Actor: "user"}, nil)
	var we *WorkError
	if !errors.As(err, &we) || we.Code != "work_full" || we.Status != 507 {
		t.Fatalf("past the limit: %v", err)
	}
	if n, _ := w.Store.WorkOpenCount(ctx); n != 2 {
		t.Fatalf("open %d", n)
	}
	page, _ := w.Backlog(ctx, "/p", "")
	if len(page.Rows) != 2 || page.Counts[work.ItemPlanned] != 2 {
		t.Fatalf("the Backlog lost something: %+v", page.Counts)
	}
}

// DG-1: a sweep that stops is said out loud. Stalled is false while passes
// come, and true once none has finished for three ticks — the answer
// /v1/health turns red on.
func TestAStoppedSweepIsStalled(t *testing.T) {
	w, _, clock := newBoard(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx, time.Hour); close(done) }()
	for {
		if p, _, _ := w.Pulse(); p.Passes > 0 {
			break
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-done:
			t.Fatal("the sweep ended before its first pass")
		}
	}
	if w.Stalled() {
		t.Fatal("stalled right after a pass")
	}
	clock.at = clock.at.Add(3*time.Hour - time.Second)
	if w.Stalled() {
		t.Fatal("stalled inside three ticks")
	}
	// Control: past three ticks with no pass, it is stalled.
	clock.at = clock.at.Add(2 * time.Second)
	if !w.Stalled() {
		t.Fatal("three ticks without a pass and not stalled")
	}
	cancel()
	<-done
	if w.Stalled() {
		t.Fatal("a sweep that was stopped on purpose reads as stalled")
	}
}
