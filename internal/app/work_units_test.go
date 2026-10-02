package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// One unit of work (docs/token-ledger.md "One unit of work"). Every
// transcript is synthetic, written under a temporary home.

func newWorkHarness(t *testing.T) (*usageHarness, *WorkUnitRecorder) {
	t.Helper()
	h := newUsageHarness(t)
	rec := NewWorkUnitRecorder(h.u)
	rec.Log = h.u.Log
	return h, rec
}

// modelCall is call with the model named.
func modelCall(id, model string, write, read, output int) string {
	return strings.Replace(call(id, write, read, output), `"model":"claude-opus-5-5"`, `"model":"`+model+`"`, 1)
}

func (h *usageHarness) record(rec *WorkUnitRecorder, e WorkUnitEvent) {
	h.t.Helper()
	if err := rec.Record(context.Background(), e); err != nil {
		h.t.Fatal(err)
	}
}

func (h *usageHarness) units() WorkUnitReport {
	h.t.Helper()
	got, err := h.u.WorkUnits(context.Background(), time.Unix(0, 0), time.Time{})
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

func unitOf(t *testing.T, rep WorkUnitReport, kind, id string) WorkUnitDelta {
	t.Helper()
	for _, u := range rep.Units {
		if u.Kind == kind && u.ID == id {
			return u
		}
	}
	t.Fatalf("no %s %s in %+v", kind, id, rep.Units)
	return WorkUnitDelta{}
}

func sessionOf(t *testing.T, u WorkUnitDelta, conversation string) WorkUnitSession {
	t.Helper()
	for _, s := range u.Sessions {
		if s.Conversation == conversation {
			return s
		}
	}
	t.Fatalf("no session %s in %+v", conversation, u.Sessions)
	return WorkUnitSession{}
}

func has(states []string, want string) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}

func (h *usageHarness) cursors(kind, id, cycle string) []store.WorkCursor {
	h.t.Helper()
	got, err := h.store.WorkCursorsOf(context.Background(), store.WorkCursorUnit{Kind: kind, ID: id, Cycle: cycle})
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

func edges(rows []store.WorkCursor) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Edge+":"+r.Session)
	}
	return strings.Join(out, ",")
}

// tokensOnly is t without its cost: a delta of two readings.
func tokensOnly(t transcript.Tokens) transcript.Tokens {
	t.Cost, t.Unpriced = 0, 0
	return t
}

// A start posted twice, an end posted twice, a phase set twice: one cursor per
// edge, the first. A replayed start must not give a session that joined later
// a starting reading it never had.
func TestADuplicateEdgeTakesNoSecondCursor(t *testing.T) {
	h, rec := newWorkHarness(t)
	h.child("k1", "t-dup")
	h.pass()
	start := WorkUnitEvent{Kind: WorkUnitTask, ID: "t-dup", Edge: store.WorkCursorStart, At: time.Unix(1_790_000_000, 0)}
	h.record(rec, start)
	h.child("k2", "t-dup")
	h.pass()
	h.record(rec, start)
	end := WorkUnitEvent{Kind: WorkUnitTask, ID: "t-dup", Edge: store.WorkCursorEnd, Outcome: "success", At: time.Unix(1_790_000_100, 0)}
	h.record(rec, end)
	end.Outcome = "failure"
	h.record(rec, end)
	if got := edges(h.cursors(WorkUnitTask, "t-dup", "")); got != "start:,start:k1,end:,end:k1,end:k2" {
		t.Fatalf("rows: %s", got)
	}
	u := unitOf(t, h.units(), WorkUnitTask, "t-dup")
	if u.Outcome != "success" {
		t.Fatalf("the first end's outcome did not win: %q", u.Outcome)
	}

	// The Board telling the same change twice, and a phase set again.
	item := work.ItemV2{ID: "item-dup", Phase: work.PhaseImplementing, Cycle: 1, UpdatedAt: time.Unix(1_790_000_200, 0)}
	before := item
	before.Phase = work.PhaseAssigned
	change := store.WorkV2Change{Prev: before, Next: item}
	again := store.WorkV2Change{Prev: item, Next: item}
	rec.ObserveBoard([]store.WorkV2Change{change, change, again})
	if n := rec.Waiting(); n != 4 {
		t.Fatalf("%d edges queued, want two starts and two step starts", n)
	}
}

// The ledger had not read the child's last call when the task ended: the end
// cursor says ledger_behind and the delta is a lower bound with no cost. Once
// a pass has read it, settling records a separate settled reading, and the
// end cursor is still there as it was.
func TestALateLedgerIsBehindAtTheEndAndSettledLater(t *testing.T) {
	h, rec := newWorkHarness(t)
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-late", Edge: store.WorkCursorStart})
	h.child("k", "t-late")
	h.pass()
	read := reference(t, h.session("k"))
	h.write(h.session("k"), call("m-late", 5000, 3000, 70))
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-late", Edge: store.WorkCursorEnd, Outcome: "success"})

	u := unitOf(t, h.units(), WorkUnitTask, "t-late")
	if !has(u.States, WorkLedgerBehind) || u.CostKnown || u.Cost != nil {
		t.Fatalf("behind: states %v, cost %v %v", u.States, u.CostKnown, u.Cost)
	}
	if !sameTokens(tokensOnly(u.Tokens), tokensOnly(read)) {
		t.Fatalf("behind delta %+v, want what was read %+v", u.Tokens, read)
	}

	h.pass()
	if err := h.u.SettleWorkCursors(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := edges(h.cursors(WorkUnitTask, "t-late", "")); got != "start:,end:,end:k,settled:end:k" {
		t.Fatalf("rows: %s", got)
	}
	for _, c := range h.cursors(WorkUnitTask, "t-late", "") {
		if c.Edge == store.WorkCursorEnd && c.Session == "k" && c.State != WorkLedgerBehind {
			t.Fatalf("the end cursor was rewritten: %+v", c)
		}
		if c.Edge == "settled:end" && c.State != WorkSettled {
			t.Fatalf("settled as %q", c.State)
		}
	}
	u = unitOf(t, h.units(), WorkUnitTask, "t-late")
	whole := reference(t, h.session("k"))
	if !has(u.States, WorkLedgerBehind) || !has(u.States, WorkSettled) || !u.CostKnown || u.Cost == nil {
		t.Fatalf("settled: states %v, cost %v", u.States, u.CostKnown)
	}
	if !sameTokens(tokensOnly(u.Tokens), tokensOnly(whole)) || !near(*u.Cost, whole.Cost) {
		t.Fatalf("settled delta %+v cost %v, want %+v", u.Tokens, *u.Cost, whole)
	}
}

// A child that ended before the ledger first read any transcript naming it:
// the end counted nobody, and a later settling finds it as an upper bound.
func TestATaskTheLedgerHadNotFoundAtItsEndIsFoundLater(t *testing.T) {
	h, rec := newWorkHarness(t)
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-unseen", Edge: store.WorkCursorStart})
	h.child("k", "t-unseen")
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-unseen", Edge: store.WorkCursorEnd, Outcome: "success"})
	u := unitOf(t, h.units(), WorkUnitTask, "t-unseen")
	if !has(u.States, WorkNoSession) || u.CostKnown || u.Tokens.Total() != 0 {
		t.Fatalf("before the ledger read it: %+v", u)
	}
	h.pass()
	u = unitOf(t, h.units(), WorkUnitTask, "t-unseen")
	s := sessionOf(t, u, "k")
	if !has(s.States, WorkSettledAfterGrowth) || !s.Counted || u.CostKnown || has(u.States, WorkNoSession) {
		t.Fatalf("after: unit %v session %+v", u.States, s)
	}
	if !sameTokens(tokensOnly(u.Tokens), tokensOnly(reference(t, h.session("k")))) {
		t.Fatalf("late delta %+v", u.Tokens)
	}
}

// putItem changes a stored item and lets the Board's observer hear it.
func putItem(t *testing.T, h *usageHarness, id string, change func(*work.ItemV2)) {
	t.Helper()
	if err := h.store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		next := prev
		change(&next)
		next.UpdatedAt = prev.UpdatedAt.Add(time.Minute)
		return tx.PutItem(prev, next, "test", "local", `{}`)
	}); err != nil {
		t.Fatal(err)
	}
}

// The item's owner changes while it is being implemented: the leaving session
// is measured to its release, and what it does afterwards is not the item's;
// the arriving one is measured from its acquisition, not from its own start.
func TestAnItemHandedOverIsMeasuredPerSessionAcrossTheHandoff(t *testing.T) {
	h, rec := newWorkHarness(t)
	ctx := context.Background()
	h.store.ObserveWorkV2(rec.ObserveBoard)
	t.Cleanup(func() { h.store.ObserveWorkV2(nil) })
	t0 := time.Unix(1_790_000_000, 0)
	item := work.ItemV2{ID: "10000000-0000-4000-8000-0000000000a1", ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Fix it", Description: "Fix it", Phase: work.PhaseAssigned,
		DeploymentPolicy: work.DeployAgentDecides, OwnerSession: "owner-a", CreatedBy: "local",
		CreatedAt: t0, UpdatedAt: t0, Cycle: 1, Version: 1}
	if err := h.store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		return tx.CreateItem(item, "local", `{}`)
	}); err != nil {
		t.Fatal(err)
	}
	h.write(h.session("owner-a"), said("Take the item."), call("a1", 8000, 0, 40))
	h.write(h.session("owner-b"), said("Something earlier."), call("b1", 9000, 0, 50))
	h.pass()
	putItem(t, h, item.ID, func(i *work.ItemV2) { i.Phase = work.PhaseImplementing })
	rec.Drain(ctx)

	aStart := reference(t, h.session("owner-a"))
	h.write(h.session("owner-a"), call("a2", 1000, 8000, 30))
	h.pass()
	aRelease := reference(t, h.session("owner-a"))
	bAcquire := reference(t, h.session("owner-b"))
	putItem(t, h, item.ID, func(i *work.ItemV2) { i.OwnerSession = "owner-b" })
	rec.Drain(ctx)

	// After the handoff: a's work is not the item's any more, b's is.
	h.write(h.session("owner-a"), call("a3", 7000, 9000, 90))
	h.write(h.session("owner-b"), call("b2", 2000, 9000, 60))
	h.pass()
	bEnd := reference(t, h.session("owner-b"))
	putItem(t, h, item.ID, func(i *work.ItemV2) { i.Phase = work.PhaseDone; i.ClosedAt = i.UpdatedAt })
	rec.Drain(ctx)

	u := unitOf(t, h.units(), WorkUnitItem, item.ID)
	if u.Cycle != "1" || u.Outcome != "done" || !has(u.States, WorkSessionHandoff) || has(u.States, WorkOpen) {
		t.Fatalf("unit: %+v", u)
	}
	a, b := sessionOf(t, u, "owner-a"), sessionOf(t, u, "owner-b")
	if !a.Counted || !has(a.States, WorkSessionHandoff) || !sameTokens(tokensOnly(a.Delta), tokensOnly(subTokens(aRelease, aStart))) {
		t.Fatalf("leaving session: %+v, want %+v", a, subTokens(aRelease, aStart))
	}
	if !b.Counted || has(b.States, WorkStartedInside) || !sameTokens(tokensOnly(b.Delta), tokensOnly(subTokens(bEnd, bAcquire))) {
		t.Fatalf("arriving session: %+v, want %+v", b, subTokens(bEnd, bAcquire))
	}
	if !sameTokens(tokensOnly(u.Tokens), tokensOnly(addTokens(a.Delta, b.Delta))) || !u.CostKnown || u.Cost == nil {
		t.Fatalf("unit delta %+v cost %v", u.Tokens, u.Cost)
	}
	if a.PeakStart == nil || b.PeakStart == nil || !b.PeakRose {
		t.Fatalf("peaks: a %+v b %+v", a, b)
	}
	if got := edges(h.cursors(WorkUnitItem, item.ID, "1")); got !=
		"start:,start:owner-a,release:owner-a,acquire:owner-b,end:,end:owner-b" {
		t.Fatalf("rows: %s", got)
	}

	// Reopened after done: a new cycle, not an overwrite.
	putItem(t, h, item.ID, func(i *work.ItemV2) { i.Phase = work.PhaseAssigned; i.Cycle = 2; i.ClosedAt = time.Time{} })
	putItem(t, h, item.ID, func(i *work.ItemV2) { i.Phase = work.PhaseImplementing })
	rec.Drain(ctx)
	if got := edges(h.cursors(WorkUnitItem, item.ID, "2")); got != "start:,start:owner-b" {
		t.Fatalf("second cycle rows: %s", got)
	}
	if got := edges(h.cursors(WorkUnitItem, item.ID, "1")); !strings.HasPrefix(got, "start:,start:owner-a,") {
		t.Fatalf("the first cycle changed: %s", got)
	}
}

// A model with no price — a Codex model — makes the cost unknown and keeps
// the tokens; so does a unit spanning two priced models.
func TestAnUnpricedOrMixedUnitHasTokensAndNoCost(t *testing.T) {
	h, rec := newWorkHarness(t)
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-codex", Edge: store.WorkCursorStart})
	h.write(h.session("kc"), said("You are a Clawdline CHILD agent for task t-codex. Say this line first."),
		modelCall("mc", "gpt-5.5-codex", 3000, 0, 20))
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-mixed", Edge: store.WorkCursorStart})
	h.write(h.session("km1"), said("You are a Clawdline CHILD agent for task t-mixed. Say this line first."),
		modelCall("m1", "claude-opus-5-5", 3000, 0, 20))
	h.write(h.session("km2"), said("You are a Clawdline CHILD agent for task t-mixed. Say this line first."),
		modelCall("m2", "claude-sonnet-5", 3000, 0, 20))
	h.pass()
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-codex", Edge: store.WorkCursorEnd, Outcome: "success"})
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-mixed", Edge: store.WorkCursorEnd, Outcome: "success"})

	rep := h.units()
	codex := unitOf(t, rep, WorkUnitTask, "t-codex")
	if !has(codex.States, WorkUnpriced) || codex.CostKnown || codex.Cost != nil {
		t.Fatalf("unpriced: %+v", codex)
	}
	if codex.Tokens.Total() == 0 || codex.Tokens.Unpriced == 0 || len(codex.Models) != 1 || codex.Models[0].Priced {
		t.Fatalf("unpriced tokens: %+v models %+v", codex.Tokens, codex.Models)
	}
	mixed := unitOf(t, rep, WorkUnitTask, "t-mixed")
	if !has(mixed.States, WorkMixedModels) || has(mixed.States, WorkUnpriced) || mixed.CostKnown || mixed.Cost != nil {
		t.Fatalf("mixed: %+v", mixed)
	}
	if mixed.Tokens.Total() == 0 || len(mixed.Models) != 2 {
		t.Fatalf("mixed tokens: %+v models %+v", mixed.Tokens, mixed.Models)
	}
}

// A task that failed or was cancelled still ends: its end cursor has the
// broker's outcome, and the delta is taken as for any other ending.
func TestAFailedOrCancelledTaskGetsAnEndWithItsOutcome(t *testing.T) {
	h, rec := newWorkHarness(t)
	ctx := context.Background()
	rec.TaskEdge("t-fail", store.WorkCursorStart, "queued", time.Unix(1_790_000_000, 0))
	rec.TaskEdge("t-cancel", store.WorkCursorStart, "queued", time.Unix(1_790_000_000, 0))
	rec.Drain(ctx)
	h.child("kf", "t-fail")
	h.pass()
	rec.TaskEdge("t-fail", store.WorkCursorEnd, "failure", time.Unix(1_790_000_300, 0))
	rec.TaskEdge("t-cancel", store.WorkCursorEnd, "cancelled", time.Unix(1_790_000_300, 0))
	rec.Drain(ctx)

	rep := h.units()
	failed := unitOf(t, rep, WorkUnitTask, "t-fail")
	if failed.Outcome != "failure" || failed.EndedAt.IsZero() || has(failed.States, WorkOpen) || !failed.CostKnown {
		t.Fatalf("failed: %+v", failed)
	}
	if !sameTokens(tokensOnly(failed.Tokens), tokensOnly(reference(t, h.session("kf")))) {
		t.Fatalf("failed delta %+v", failed.Tokens)
	}
	cancelled := unitOf(t, rep, WorkUnitTask, "t-cancel")
	if cancelled.Outcome != "cancelled" || !has(cancelled.States, WorkNoSession) || cancelled.CostKnown {
		t.Fatalf("cancelled: %+v", cancelled)
	}
}

// A cursor that cannot be taken leaves a cursor_missing marker, and so does
// an edge refused at a full queue; neither is a silent zero.
func TestACursorThatCannotBeTakenIsMissingNotZero(t *testing.T) {
	h, rec := newWorkHarness(t)
	ctx := context.Background()
	// A step's cursor reads its item; an item that cannot be read fails it.
	if err := rec.Record(ctx, WorkUnitEvent{Kind: WorkUnitStep, ID: "no-such-step", Item: "no-such-item", Cycle: "1",
		Edge: store.WorkCursorStart}); err == nil {
		t.Fatal("a cursor of a step whose item is not there was taken")
	}
	u := unitOf(t, h.units(), WorkUnitStep, "no-such-step")
	if !has(u.States, WorkCursorMissing) || u.CostKnown {
		t.Fatalf("missing: %+v", u)
	}

	for i := 0; i < workCursorQueueLimit+3; i++ {
		rec.TaskEdge(fmt.Sprintf("t-q-%04d", i), store.WorkCursorEnd, "success", time.Unix(1_790_000_000, 0))
	}
	if n := rec.Waiting(); n != workCursorQueueLimit+3 {
		t.Fatalf("%d edges wait", n)
	}
	rec.Drain(ctx)
	for _, id := range []string{"t-q-1024", "t-q-1026"} {
		rows := h.cursors(WorkUnitTask, id, "")
		if len(rows) != 1 || rows[0].State != WorkCursorMissing || rows[0].Outcome != "success" {
			t.Fatalf("%s past the queue: %+v", id, rows)
		}
	}
	if rows := h.cursors(WorkUnitTask, "t-q-0000", ""); len(rows) != 1 || rows[0].State != "" {
		t.Fatalf("an edge inside the queue: %+v", rows)
	}
}

// The table keeps totals only: a prompt's words, a tool's input and an
// answer's text are nowhere in it.
func TestAWorkCursorHoldsNoTranscriptText(t *testing.T) {
	const marker = "MARKER-do-not-store-8c1f"
	h, rec := newWorkHarness(t)
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-text", Edge: store.WorkCursorStart})
	h.write(h.session("kt"), said("You are a Clawdline CHILD agent for task t-text. "+marker+" in the prompt."),
		strings.Replace(call("mt", 3000, 0, 20), `"text":"ok"`, `"text":"`+marker+` in the answer"`, 1),
		`{"type":"assistant","message":{"id":"mt2","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"echo `+marker+`"}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":10,"cache_read_input_tokens":3000,"output_tokens":5}}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"`+marker+` from the terminal"}]}}`)
	h.write(h.subagent("kt", "agent-t"), said(marker+" for the subagent."), call("st", 1000, 0, 10))
	h.pass()
	h.write(h.session("kt"), said(marker+" arrives late."))
	h.record(rec, WorkUnitEvent{Kind: WorkUnitTask, ID: "t-text", Edge: store.WorkCursorEnd, Outcome: "success"})
	h.pass()
	rep := h.units()
	rows, _, err := h.store.WorkCursorsBetween(context.Background(), time.Unix(0, 0), time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 4 {
		t.Fatalf("only %d rows", len(rows))
	}
	stored, _ := json.Marshal(rows)
	if strings.Contains(string(stored), marker) || strings.Contains(string(stored), "MARKER") {
		t.Fatalf("the cursor table holds transcript text: %s", stored)
	}
	answer, _ := json.Marshal(rep)
	if strings.Contains(string(answer), marker) {
		t.Fatal("the report holds transcript text")
	}
}
