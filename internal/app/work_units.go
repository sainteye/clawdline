package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// One unit of work (docs/token-ledger.md "One unit of work").
//
// The ledger's answers are cumulative: a long owner Session's bill is every
// call it ever made. A unit of work — a child task from admission to its end,
// a Board item from entering implementing to done or cancelled, one of its
// steps — leaves a cursor at each edge: every session counted for it, with that
// session's cumulative ledger reading at that moment. A unit's cost is end
// minus start, per session, summed; the cumulative answers are not changed.
//
// Taking a cursor never stands in the way of the change that caused it: the
// broker and the Board post an edge, and a worker reads the ledger and writes
// the rows after the change has committed. A cursor that cannot be taken is
// logged once and leaves a `cursor_missing` marker the report shows.

// The units.
const (
	WorkUnitTask = "task"
	WorkUnitItem = "item"
	WorkUnitStep = "step"
)

// The states a cursor, a session's delta or a unit can be in. None of them is
// ever shown as a zero.
const (
	// WorkNotYetRead is a session the ledger had no row of at a cursor.
	WorkNotYetRead = store.UsageNotYetRead
	// WorkMissing is a session a closed unit counts that has no reading at
	// one of its edges.
	WorkMissing = "missing"
	// WorkLedgerBehind is an end (or release) taken while the ledger had not
	// read the transcript up to its size and modification time.
	WorkLedgerBehind = "ledger_behind"
	// WorkSettled is a later reading that caught up with exactly the
	// transcript the end cursor saw; WorkSettledAfterGrowth one that caught up
	// only after the transcript grew past it, so it may hold work done after
	// the unit ended (an upper bound).
	WorkSettled            = "settled"
	WorkSettledAfterGrowth = "settled_after_growth"
	// WorkSessionHandoff is an item whose owner changed while it was open:
	// the leaving session is measured to its release, the arriving one from
	// its acquisition.
	WorkSessionHandoff = "session_handoff"
	// WorkStartedInside is a session with no starting reading that joined an
	// item after it started — a child its owner dispatched, an assignment
	// opened later — counted from zero.
	WorkStartedInside = "started_inside"
	// WorkMixedModels and WorkUnpriced make the cost unknown; the tokens stay.
	WorkMixedModels = "mixed_models"
	WorkUnpriced    = "unpriced"
	// WorkCursorMissing is an edge whose cursor could not be taken.
	WorkCursorMissing = store.WorkCursorMissing
	// WorkOpen is a unit that has not ended.
	WorkOpen = "open"
	// WorkNoSession is a unit that ended with no session counted for it: a
	// child that never opened, or one whose transcript names nothing yet.
	WorkNoSession = "no_session"
)

const (
	// workCursorQueueLimit is how many edges may wait for the worker
	// (capacity row usage.work_cursor_queue). Past it an edge is not read:
	// its cursor_missing marker waits instead, written before the queue is.
	workCursorQueueLimit = 1024
	// workUnitAnswerLimit is how many units one report reads, the most
	// recent first (capacity row usage.work_units_per_answer).
	workUnitAnswerLimit = 500
	// workSettleLimit is how many behind cursors one settling pass reads
	// again; the rest are the next pass's.
	workSettleBatch = 64
)

// WorkCursorModel is one model a session's reading names, and whether it has
// a price.
type WorkCursorModel struct {
	Model  string `json:"model"`
	Priced bool   `json:"priced"`
}

// WorkCursorFile is one transcript of a session — its own or a subagent's —
// as the ledger had read it and as it was on disk when the cursor was taken.
type WorkCursorFile struct {
	Conversation     string    `json:"conversation"`
	LedgerSize       int64     `json:"ledger_size"`
	LedgerModifiedAt time.Time `json:"ledger_modified_at"`
	FileSize         int64     `json:"file_size"`
	FileModifiedAt   time.Time `json:"file_modified_at"`
	// Seen says the file was found; a file not found is not behind.
	Seen   bool   `json:"seen"`
	More   bool   `json:"more,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// behind says the ledger had not read this file as it is.
func (f WorkCursorFile) behind() bool {
	if f.More || f.Reason == store.UsageTranscriptUnreadable {
		return true
	}
	return f.Seen && (f.FileSize != f.LedgerSize || !f.FileModifiedAt.Equal(f.LedgerModifiedAt))
}

// WorkCursorReading is one session's cumulative ledger reading at a cursor:
// totals only.
type WorkCursorReading struct {
	Assistant string `json:"assistant,omitempty"`
	// Present says the ledger has a row of the session.
	Present bool      `json:"present"`
	Reason  string    `json:"reason,omitempty"`
	ReadAt  time.Time `json:"read_at"`
	More    bool      `json:"more,omitempty"`
	// Tokens is the session's measured count by part, its subagents'
	// included, with the priced part's cost and the unpriced tokens.
	Tokens      transcript.Tokens `json:"tokens"`
	Calls       int64             `json:"calls"`
	Compactions int64             `json:"compactions"`
	// PeakContext is the session's own largest context so far.
	PeakContext int64             `json:"peak_context"`
	Models      []WorkCursorModel `json:"models,omitempty"`
	Files       []WorkCursorFile  `json:"files,omitempty"`
	Behind      bool              `json:"behind,omitempty"`
}

// WorkUnitEvent is one edge of a unit, as the broker or the Board posts it.
type WorkUnitEvent struct {
	Kind  string
	ID    string
	Cycle string
	Edge  string
	// Outcome is the ending as the broker or the Board states it.
	Outcome string
	At      time.Time
	// Session is the one session a release or acquire reads.
	Session string
	// Item is the Board item a step belongs to; a step event with no ID is
	// the item's next open step, resolved when the cursor is taken.
	Item string
}

func (e WorkUnitEvent) key() string {
	return e.Kind + "/" + e.ID + "/" + e.Cycle + "/" + e.Edge + "/" + e.Session
}

// WorkUnitRecorder takes cursors off the caller's path: Post never blocks,
// and Run's worker reads the ledger and writes the rows in the order the
// edges were posted.
type WorkUnitRecorder struct {
	Ledger *UsageLedger
	Log    func(format string, args ...any)

	mu sync.Mutex
	// queue is the edges waiting to be read; missing the ones refused at a
	// full queue, whose cursor_missing markers the worker writes first.
	queue, missing []WorkUnitEvent
	lost           int64
	wake           chan struct{}
	logged         map[string]bool
}

// NewWorkUnitRecorder is a recorder over u.
func NewWorkUnitRecorder(u *UsageLedger) *WorkUnitRecorder {
	return &WorkUnitRecorder{Ledger: u, wake: make(chan struct{}, 1)}
}

func (r *WorkUnitRecorder) sayOnce(key, format string, args ...any) {
	r.mu.Lock()
	if r.logged == nil {
		r.logged = map[string]bool{}
	}
	if r.logged[key] {
		r.mu.Unlock()
		return
	}
	r.logged[key] = true
	r.mu.Unlock()
	if r.Log != nil {
		r.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Post queues an edge and returns at once.
func (r *WorkUnitRecorder) Post(e WorkUnitEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	switch {
	case len(r.queue) < workCursorQueueLimit:
		r.queue = append(r.queue, e)
	case len(r.missing) < workCursorQueueLimit:
		r.missing = append(r.missing, e)
	default:
		r.lost++
	}
	full := len(r.queue) >= workCursorQueueLimit
	lost := r.lost
	r.mu.Unlock()
	if lost > 0 {
		r.sayOnce("lost", "usage: work-cursor edges are being dropped with no marker (%d so far); the reports cannot name them",
			lost)
	}
	if full {
		r.sayOnce("queue", "usage: the work-cursor queue is full (%d edges); further edges are recorded as cursor_missing",
			workCursorQueueLimit)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Waiting is how many edges wait for the worker, the refused ones included.
func (r *WorkUnitRecorder) Waiting() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue) + len(r.missing)
}

// Run takes every posted cursor until ctx ends.
func (r *WorkUnitRecorder) Run(ctx context.Context) {
	for {
		r.Drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		}
	}
}

// Drain takes every cursor posted so far.
func (r *WorkUnitRecorder) Drain(ctx context.Context) {
	for ctx.Err() == nil {
		r.mu.Lock()
		missing := r.missing
		r.missing = nil
		var next *WorkUnitEvent
		if len(missing) == 0 && len(r.queue) > 0 {
			e := r.queue[0]
			r.queue = r.queue[1:]
			next = &e
		}
		r.mu.Unlock()
		for _, e := range missing {
			r.markMissing(ctx, e, errors.New("the work-cursor queue was full"))
		}
		if next == nil {
			if len(missing) == 0 {
				return
			}
			continue
		}
		if err := r.Record(ctx, *next); err != nil && ctx.Err() == nil {
			r.sayOnce("record:"+next.key(), "usage: the %s cursor of %s %s could not be taken: %v",
				next.Edge, next.Kind, next.ID, err)
		}
	}
}

func (r *WorkUnitRecorder) markMissing(ctx context.Context, e WorkUnitEvent, cause error) {
	if e.ID == "" {
		return
	}
	now := r.Ledger.now()
	marker := store.WorkCursor{UnitKind: e.Kind, UnitID: e.ID, Cycle: e.Cycle, Edge: e.Edge, Session: e.Session,
		At: e.At, TakenAt: now, Outcome: e.Outcome, State: store.WorkCursorMissing}
	if _, err := r.Ledger.Store.AddWorkCursors(ctx, nil, []store.WorkCursor{marker}); err != nil {
		r.sayOnce("missing:"+e.key(), "usage: the %s cursor of %s %s is missing (%v), and that could not be recorded: %v",
			e.Edge, e.Kind, e.ID, cause, err)
	}
}

// Record takes one edge's cursor now. A failure leaves a cursor_missing marker
// when the store can still write one.
func (r *WorkUnitRecorder) Record(ctx context.Context, e WorkUnitEvent) error {
	err := r.record(ctx, e)
	if err != nil {
		r.markMissing(ctx, e, err)
	}
	return err
}

func (r *WorkUnitRecorder) record(ctx context.Context, e WorkUnitEvent) error {
	u := r.Ledger
	if e.Kind == WorkUnitStep {
		if err := r.resolveStep(ctx, &e); err != nil || e.ID == "" {
			return err
		}
	}
	if e.ID == "" || e.Edge == "" {
		return errors.New("an edge needs its unit and edge")
	}
	var sessions []string
	var err error
	switch {
	case e.Session != "":
		sessions = []string{e.Session}
	case e.Kind == WorkUnitTask:
		sessions, err = u.taskSessions(ctx, e.ID)
	case e.Kind == WorkUnitItem:
		sessions, err = u.itemSessions(ctx, e.ID)
	case e.Kind == WorkUnitStep:
		sessions, err = u.itemSessions(ctx, e.Item)
	default:
		return fmt.Errorf("no unit kind %q", e.Kind)
	}
	if err != nil {
		return err
	}
	readings, err := u.cursorReadings(ctx, sessions)
	if err != nil {
		return err
	}
	now := u.now()
	if e.At.IsZero() {
		e.At = now
	}
	var rows []store.WorkCursor
	for _, s := range sessions {
		reading := readings[s]
		body, err := json.Marshal(reading)
		if err != nil {
			return err
		}
		rows = append(rows, store.WorkCursor{UnitKind: e.Kind, UnitID: e.ID, Cycle: e.Cycle, Edge: e.Edge,
			Session: s, At: e.At, TakenAt: now, Outcome: e.Outcome, State: readingState(reading), Reading: body})
	}
	var marker *store.WorkCursor
	if e.Session == "" {
		marker = &store.WorkCursor{UnitKind: e.Kind, UnitID: e.ID, Cycle: e.Cycle, Edge: e.Edge,
			At: e.At, TakenAt: now, Outcome: e.Outcome}
	}
	_, err = u.Store.AddWorkCursors(ctx, marker, rows)
	return err
}

// resolveStep fills a step event's cycle from its item, and a "next open
// step" event's step.
func (r *WorkUnitRecorder) resolveStep(ctx context.Context, e *WorkUnitEvent) error {
	st := r.Ledger.Store
	if e.Item == "" {
		return errors.New("a step edge needs its item")
	}
	item, err := st.WorkV2Item(ctx, e.Item)
	if err != nil {
		return err
	}
	if e.Cycle == "" {
		e.Cycle = strconv.FormatInt(item.Cycle, 10)
	}
	if e.ID != "" {
		return nil
	}
	steps, err := st.WorkV2Steps(ctx, e.Item)
	if err != nil {
		return err
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })
	for _, s := range steps {
		if !s.Done {
			e.ID = s.ID
			return nil
		}
	}
	return nil
}

func readingState(r WorkCursorReading) string {
	switch {
	case !r.Present:
		return WorkNotYetRead
	case r.Behind:
		return WorkLedgerBehind
	}
	return r.Reason
}

// taskSessions is the sessions whose first message names the task, as
// ForTask counts them.
func (u *UsageLedger) taskSessions(ctx context.Context, taskID string) ([]string, error) {
	rows, err := u.Store.UsageRowsForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return conversationsOf(ownRows(rows)), nil
}

// itemSessions is the item's owner sessions and its dispatched tasks'
// sessions, as ForItem counts them.
func (u *UsageLedger) itemSessions(ctx context.Context, itemID string) ([]string, error) {
	got, err := u.ForItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range got.Sessions {
		out = append(out, s.Conversation)
	}
	for _, t := range got.Tasks {
		for _, s := range t.Sessions {
			out = append(out, s.Conversation)
		}
	}
	return dedupe(out), nil
}

// cursorReadings is each session's reading now, from its row and its
// subagents' rows, with its files as they are on disk.
func (u *UsageLedger) cursorReadings(ctx context.Context, sessions []string) (map[string]WorkCursorReading, error) {
	out := map[string]WorkCursorReading{}
	if len(sessions) == 0 {
		return out, nil
	}
	rows, err := u.Store.UsageRowsForConversations(ctx, sessions)
	if err != nil {
		return nil, err
	}
	subs, err := u.Store.UsageRowsWithParents(ctx, sessions)
	if err != nil {
		return nil, err
	}
	own := map[string]store.UsageRow{}
	for _, r := range ownRows(rows) {
		own[r.Conversation] = r
	}
	for _, s := range sessions {
		row, ok := own[s]
		if !ok {
			out[s] = WorkCursorReading{}
			continue
		}
		var mine []store.UsageRow
		for _, sub := range subs {
			if sub.Parent == s {
				mine = append(mine, sub)
			}
		}
		out[s] = cursorReading(row, mine)
	}
	return out, nil
}

// cursorReading is one session's reading from its rows.
func cursorReading(row store.UsageRow, subagents []store.UsageRow) WorkCursorReading {
	folded := FoldSession(row.Conversation, []store.UsageRow{row}, subagents)
	out := WorkCursorReading{Assistant: row.Assistant, Present: !row.ReadAt.IsZero(), Reason: row.Reason,
		ReadAt: row.ReadAt, More: row.More, Tokens: folded.Totals.Measured, Calls: folded.Calls,
		Compactions: folded.Compactions, PeakContext: folded.PeakContext}
	models := map[string]bool{}
	for _, r := range append([]store.UsageRow{row}, subagents...) {
		state, _, _ := usageLedgerOf(r)
		if state.Model != "" {
			models[state.Model] = true
		}
		if r.Parent != "" {
			out.Calls += state.Calls
		}
		f := WorkCursorFile{Conversation: r.Conversation, LedgerSize: r.Size, LedgerModifiedAt: r.ModifiedAt,
			More: r.More, Reason: r.Reason}
		if r.Path != "" {
			if st, err := os.Stat(r.Path); err == nil {
				f.Seen, f.FileSize, f.FileModifiedAt = true, st.Size(), st.ModTime()
			}
		}
		out.Behind = out.Behind || f.behind()
		out.Files = append(out.Files, f)
	}
	for m := range models {
		_, _, priced := transcript.Price(m)
		out.Models = append(out.Models, WorkCursorModel{Model: m, Priced: priced})
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Model < out.Models[j].Model })
	return out
}

// ---------- what the Board and the broker post ----------

// activePhase is a phase between entering implementing and closing.
func activePhase(p work.Phase) bool {
	switch p {
	case work.PhaseImplementing, work.PhaseVerifying, work.PhaseMerging, work.PhaseDeploying:
		return true
	}
	return false
}

// ObserveBoard posts the edges a committed Board write made: an item entering
// implementing (and its first open step starting), an item that was in
// progress closing, its owner changing while it is in progress, and a step
// marked done (and the next open one starting).
func (r *WorkUnitRecorder) ObserveBoard(changes []store.WorkV2Change) {
	for _, c := range changes {
		if c.Step {
			if !c.PrevStep.Done && c.NextStep.Done {
				at := c.NextStep.CompletedAt
				r.Post(WorkUnitEvent{Kind: WorkUnitStep, ID: c.NextStep.ID, Item: c.NextStep.WorkID,
					Edge: store.WorkCursorEnd, Outcome: "step-done", At: at})
				r.Post(WorkUnitEvent{Kind: WorkUnitStep, Item: c.NextStep.WorkID, Edge: store.WorkCursorStart, At: at})
			}
			continue
		}
		prev, next := c.Prev, c.Next
		cycle := strconv.FormatInt(next.Cycle, 10)
		at := next.UpdatedAt
		if prev.Phase != work.PhaseImplementing && next.Phase == work.PhaseImplementing {
			r.Post(WorkUnitEvent{Kind: WorkUnitItem, ID: next.ID, Cycle: cycle, Edge: store.WorkCursorStart, At: at})
			r.Post(WorkUnitEvent{Kind: WorkUnitStep, Item: next.ID, Edge: store.WorkCursorStart, At: at})
		}
		if prev.OwnerSession != next.OwnerSession && (activePhase(prev.Phase) || activePhase(next.Phase)) &&
			prev.Cycle == next.Cycle {
			if prev.OwnerSession != "" {
				r.Post(WorkUnitEvent{Kind: WorkUnitItem, ID: next.ID, Cycle: cycle, Edge: store.WorkCursorRelease,
					Session: prev.OwnerSession, At: at})
			}
			if next.OwnerSession != "" && activePhase(next.Phase) {
				r.Post(WorkUnitEvent{Kind: WorkUnitItem, ID: next.ID, Cycle: cycle, Edge: store.WorkCursorAcquire,
					Session: next.OwnerSession, At: at})
			}
		}
		if !prev.Phase.Terminal() && next.Phase.Terminal() && activePhase(prev.Phase) {
			r.Post(WorkUnitEvent{Kind: WorkUnitItem, ID: next.ID, Cycle: cycle, Edge: store.WorkCursorEnd,
				Outcome: string(next.Phase), At: at})
		}
	}
}

// TaskEdge posts a child task's admission (edge start) or its end, with the
// broker's terminal state as the outcome.
func (r *WorkUnitRecorder) TaskEdge(taskID, edge, outcome string, at time.Time) {
	r.Post(WorkUnitEvent{Kind: WorkUnitTask, ID: taskID, Edge: edge, Outcome: outcome, At: at})
}

// ---------- settling a late ledger ----------

// SettleWorkCursors reads again the sessions whose end or release cursor
// found the ledger behind, and records a settled reading for each one the
// ledger has caught up with. The cursor it settles is left as it was.
func (u *UsageLedger) SettleWorkCursors(ctx context.Context) error {
	if err := u.settleLateTasks(ctx); err != nil {
		return err
	}
	waiting, err := u.Store.WorkCursorsAwaitingSettlement(ctx, WorkLedgerBehind, workSettleBatch)
	if err != nil || len(waiting) == 0 {
		return err
	}
	var sessions []string
	for _, c := range waiting {
		sessions = append(sessions, c.Session)
	}
	readings, err := u.cursorReadings(ctx, dedupe(sessions))
	if err != nil {
		return err
	}
	now := u.now()
	var rows []store.WorkCursor
	for _, c := range waiting {
		now := readings[c.Session]
		if !now.Present || now.Behind {
			continue
		}
		var was WorkCursorReading
		_ = json.Unmarshal(c.Reading, &was)
		state := WorkSettled
		if !caughtUpExactly(was, now) {
			state = WorkSettledAfterGrowth
		}
		body, err := json.Marshal(now)
		if err != nil {
			return err
		}
		rows = append(rows, store.WorkCursor{UnitKind: c.UnitKind, UnitID: c.UnitID, Cycle: c.Cycle,
			Edge: store.WorkCursorSettled + ":" + c.Edge, Session: c.Session, At: c.At, Outcome: c.Outcome,
			State: state, Reading: body})
	}
	for i := range rows {
		rows[i].TakenAt = now
	}
	_, err = u.Store.AddWorkCursors(ctx, nil, rows)
	return err
}

// settleLateTasks finds the sessions of child tasks that ended before the
// ledger had read any transcript naming them, and records each one's reading
// as a settled end. The end cursor saw none of them, so the reading is an
// upper bound (settled_after_growth) rather than the task's exact end.
func (u *UsageLedger) settleLateTasks(ctx context.Context) error {
	ends, err := u.Store.WorkCursorEndsWithoutSessions(ctx, WorkUnitTask, u.now().Add(-u.window()), workSettleBatch)
	if err != nil {
		return err
	}
	now := u.now()
	var rows []store.WorkCursor
	for _, c := range ends {
		sessions, err := u.taskSessions(ctx, c.UnitID)
		if err != nil || len(sessions) == 0 {
			continue
		}
		readings, err := u.cursorReadings(ctx, sessions)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			r := readings[s]
			if !r.Present || r.Behind {
				continue
			}
			body, err := json.Marshal(r)
			if err != nil {
				return err
			}
			rows = append(rows, store.WorkCursor{UnitKind: c.UnitKind, UnitID: c.UnitID, Cycle: c.Cycle,
				Edge: store.WorkCursorSettled + ":" + store.WorkCursorEnd, Session: s, At: c.At, TakenAt: now,
				Outcome: c.Outcome, State: WorkSettledAfterGrowth, Reading: body})
		}
	}
	_, err = u.Store.AddWorkCursors(ctx, nil, rows)
	return err
}

// caughtUpExactly says the ledger now has read each file to exactly where it
// stood on disk when the behind cursor was taken, and no further.
func caughtUpExactly(was, now WorkCursorReading) bool {
	files := map[string]WorkCursorFile{}
	for _, f := range now.Files {
		files[f.Conversation] = f
	}
	if len(now.Files) != len(was.Files) {
		return false
	}
	for _, f := range was.Files {
		g, ok := files[f.Conversation]
		if !ok || !f.Seen || g.LedgerSize != f.FileSize || !g.LedgerModifiedAt.Equal(f.FileModifiedAt) {
			return false
		}
	}
	return true
}

// ---------- the report ----------

// WorkUnitSession is one session's part of a unit.
type WorkUnitSession struct {
	Conversation string   `json:"conversation"`
	States       []string `json:"states"`
	// Counted says Delta is in the unit's totals; a session whose delta
	// cannot be known (not read at an edge, missing at one) is listed and
	// not counted.
	Counted     bool              `json:"counted"`
	Delta       transcript.Tokens `json:"delta"`
	Calls       int64             `json:"calls"`
	Compactions int64             `json:"compactions"`
	// PeakStart is nil when the session had no starting reading. PeakRose
	// says its peak context grew during the unit; a per-unit peak is not
	// kept by the ledger and is not made up here.
	PeakStart *int64            `json:"peak_start"`
	PeakEnd   int64             `json:"peak_end"`
	PeakRose  bool              `json:"peak_rose"`
	Models    []WorkCursorModel `json:"models"`
}

// WorkUnitDelta is one unit cycle: its raw cursors and what they add up to.
type WorkUnitDelta struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	Cycle     string    `json:"cycle"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Outcome   string    `json:"outcome"`
	States    []string  `json:"states"`
	// Tokens is the counted sessions' deltas summed, by part. Cost is only
	// set when CostKnown: one priced model and every session counted.
	Tokens      transcript.Tokens  `json:"tokens"`
	CostKnown   bool               `json:"cost_known"`
	Cost        *float64           `json:"cost"`
	Calls       int64              `json:"calls"`
	Compactions int64              `json:"compactions"`
	Models      []WorkCursorModel  `json:"models"`
	Sessions    []WorkUnitSession  `json:"sessions"`
	Cursors     []store.WorkCursor `json:"cursors"`
}

// WorkUnitReport is every unit with a cursor in [Since, Until].
type WorkUnitReport struct {
	Since     time.Time       `json:"since"`
	Until     time.Time       `json:"until"`
	Units     []WorkUnitDelta `json:"units"`
	Truncated bool            `json:"truncated"`
}

// WorkUnits is the raw cursors of every unit with one in [since, until] and
// what each unit added, the most recent unit first. Cursors whose ledger was
// behind are settled first where the ledger has caught up; a failure to settle
// leaves them behind and is not the report's failure.
func (u *UsageLedger) WorkUnits(ctx context.Context, since, until time.Time) (WorkUnitReport, error) {
	if err := u.SettleWorkCursors(ctx); err != nil {
		u.sayOnce("settle", "usage: behind work cursors could not be settled: "+err.Error())
	}
	rows, more, err := u.Store.WorkCursorsBetween(ctx, since, until, workUnitAnswerLimit)
	if err != nil {
		return WorkUnitReport{}, err
	}
	out := WorkUnitReport{Since: since, Until: until, Truncated: more, Units: []WorkUnitDelta{}}
	var order []store.WorkCursorUnit
	byUnit := map[store.WorkCursorUnit][]store.WorkCursor{}
	for _, c := range rows {
		k := store.WorkCursorUnit{Kind: c.UnitKind, ID: c.UnitID, Cycle: c.Cycle}
		if _, ok := byUnit[k]; !ok {
			order = append(order, k)
		}
		byUnit[k] = append(byUnit[k], c)
	}
	for _, k := range order {
		out.Units = append(out.Units, FoldWorkUnit(k, byUnit[k]))
	}
	return out, nil
}

// WorkUnitsSince is WorkUnits from a `since` as a route spells it
// (ParseCompareSince) to now, read against this ledger's clock.
func (u *UsageLedger) WorkUnitsSince(ctx context.Context, raw string) (WorkUnitReport, error) {
	now := u.now()
	since, err := ParseCompareSince(raw, now)
	if err != nil {
		return WorkUnitReport{}, err
	}
	return u.WorkUnits(ctx, since, now)
}

// segment is one stretch of a session inside a unit: from its start or
// acquisition to its release or the unit's end.
type segment struct {
	from, to, settled *store.WorkCursor
	// late is a session the end cursor did not see at all, found by a
	// settling pass: its settled reading is its only end.
	late bool
}

// FoldWorkUnit is one unit cycle's delta from its cursors.
func FoldWorkUnit(k store.WorkCursorUnit, cursors []store.WorkCursor) WorkUnitDelta {
	out := WorkUnitDelta{Kind: k.Kind, ID: k.ID, Cycle: k.Cycle, Cursors: cursors, States: []string{},
		Sessions: []WorkUnitSession{}, Models: []WorkCursorModel{}}
	states := map[string]bool{}
	var startMarker, endMarker *store.WorkCursor
	bySession := map[string][]store.WorkCursor{}
	settled := map[string]*store.WorkCursor{}
	var sessions []string
	for i := range cursors {
		c := &cursors[i]
		if c.Session == "" {
			switch c.Edge {
			case store.WorkCursorStart:
				startMarker = c
			case store.WorkCursorEnd:
				endMarker = c
			}
			continue
		}
		if len(c.Edge) > len(store.WorkCursorSettled) && c.Edge[:len(store.WorkCursorSettled)+1] == store.WorkCursorSettled+":" {
			settled[c.Session+"\x00"+c.Edge[len(store.WorkCursorSettled)+1:]] = c
			continue
		}
		if _, ok := bySession[c.Session]; !ok {
			sessions = append(sessions, c.Session)
		}
		bySession[c.Session] = append(bySession[c.Session], *c)
	}
	// A session only a settling pass found: the end cursor did not see it.
	var late []string
	for key := range settled {
		session, edge, _ := strings.Cut(key, "\x00")
		if _, ok := bySession[session]; !ok && edge == store.WorkCursorEnd {
			late = append(late, session)
		}
	}
	sort.Strings(late)
	sessions = append(sessions, late...)
	if startMarker != nil {
		out.StartedAt = startMarker.At
		if startMarker.State == WorkCursorMissing {
			states[WorkCursorMissing] = true
		}
	} else {
		states[WorkCursorMissing] = true
	}
	ended := endMarker != nil
	if ended {
		out.EndedAt, out.Outcome = endMarker.At, endMarker.Outcome
		if endMarker.State == WorkCursorMissing {
			states[WorkCursorMissing] = true
			ended = false
		}
	} else {
		states[WorkOpen] = true
	}

	costKnown := true
	models := map[string]bool{}
	unpriced := false
	counted := 0
	for _, s := range sessions {
		rows := bySession[s]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
		ws := WorkUnitSession{Conversation: s, States: []string{}, Models: []WorkCursorModel{}}
		sstates := map[string]bool{}
		var segs []segment
		var open *store.WorkCursor
		had := false
		if len(rows) == 0 {
			segs = append(segs, segment{to: settled[s+"\x00"+store.WorkCursorEnd], late: true})
			had = true
		}
		for i := range rows {
			c := &rows[i]
			switch c.Edge {
			case store.WorkCursorStart, store.WorkCursorAcquire:
				if open == nil {
					open = c
				}
				if c.Edge == store.WorkCursorAcquire {
					sstates[WorkSessionHandoff] = true
				}
			case store.WorkCursorRelease, store.WorkCursorEnd:
				if c.Edge == store.WorkCursorRelease {
					sstates[WorkSessionHandoff] = true
				}
				if open == nil && had {
					// Its measure ended at an earlier release.
					continue
				}
				segs = append(segs, segment{from: open, to: c, settled: settled[s+"\x00"+c.Edge]})
				open, had = nil, true
			}
		}
		if open != nil && ended {
			// Started and no end reading of it: the unit closed without it.
			sstates[WorkMissing] = true
		}
		peakSet := false
		measured := len(segs) > 0
		exact := true
		for _, seg := range segs {
			var from, to WorkCursorReading
			if seg.from != nil {
				_ = json.Unmarshal(seg.from.Reading, &from)
				if !peakSet {
					p := from.PeakContext
					ws.PeakStart, peakSet = &p, true
				}
			}
			_ = json.Unmarshal(seg.to.Reading, &to)
			end := to
			switch {
			case seg.late:
				// Its only end is a later reading: an upper bound.
				sstates[WorkLedgerBehind], sstates[WorkSettledAfterGrowth] = true, true
				exact = false
			case seg.to.State == WorkLedgerBehind:
				sstates[WorkLedgerBehind] = true
				exact = false
				if seg.settled != nil {
					sstates[seg.settled.State] = true
					if seg.settled.State == WorkSettled {
						_ = json.Unmarshal(seg.settled.Reading, &end)
						exact = true
					}
				}
			}
			if seg.to.State == WorkCursorMissing || seg.from != nil && seg.from.State == WorkCursorMissing {
				sstates[WorkCursorMissing] = true
				measured = false
			}
			switch {
			case seg.from == nil && k.Kind == WorkUnitTask:
				// A child task's sessions begin after its admission.
			case seg.from == nil:
				sstates[WorkStartedInside] = true
			case !from.Present:
				if k.Kind != WorkUnitTask {
					sstates[WorkNotYetRead] = true
					measured = false
				}
			}
			if !end.Present {
				sstates[WorkNotYetRead] = true
				measured = false
			}
			if from.Reason == store.UsageTranscriptMissing || end.Reason == store.UsageTranscriptMissing {
				sstates[store.UsageTranscriptMissing] = true
			}
			ws.Delta = addTokens(ws.Delta, subTokens(end.Tokens, from.Tokens))
			ws.Calls += end.Calls - from.Calls
			ws.Compactions += end.Compactions - from.Compactions
			ws.PeakEnd = max(ws.PeakEnd, end.PeakContext)
			for _, m := range append(append([]WorkCursorModel{}, from.Models...), end.Models...) {
				if !models[m.Model] {
					models[m.Model] = true
					out.Models = append(out.Models, m)
				}
				if !m.Priced {
					unpriced = true
				}
				known := false
				for _, x := range ws.Models {
					known = known || x.Model == m.Model
				}
				if !known {
					ws.Models = append(ws.Models, m)
				}
			}
		}
		if len(segs) == 0 && ended {
			sstates[WorkMissing] = true
		}
		ws.PeakRose = ws.PeakStart == nil && ws.PeakEnd > 0 || ws.PeakStart != nil && ws.PeakEnd > *ws.PeakStart
		ws.Counted = measured && !sstates[WorkMissing]
		if !exact {
			costKnown = false
		}
		if ws.Counted {
			counted++
			out.Tokens = addTokens(out.Tokens, ws.Delta)
			out.Calls += ws.Calls
			out.Compactions += ws.Compactions
			if ws.Delta.Unpriced > 0 {
				unpriced = true
			}
		} else {
			costKnown = false
		}
		for st := range sstates {
			if st != "" {
				ws.States = append(ws.States, st)
				states[st] = true
			}
		}
		sort.Strings(ws.States)
		out.Sessions = append(out.Sessions, ws)
	}
	if ended && len(sessions) == 0 {
		states[WorkNoSession] = true
	}
	if len(out.Models) > 1 {
		states[WorkMixedModels] = true
	}
	if unpriced {
		states[WorkUnpriced] = true
	}
	out.CostKnown = costKnown && ended && counted > 0 && !states[WorkMixedModels] && !unpriced &&
		!states[WorkCursorMissing]
	if out.CostKnown {
		cost := out.Tokens.Cost
		out.Cost = &cost
	}
	for st := range states {
		out.States = append(out.States, st)
	}
	sort.Strings(out.States)
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Model < out.Models[j].Model })
	return out
}

func subTokens(a, b transcript.Tokens) transcript.Tokens {
	return transcript.Tokens{
		Input: a.Input - b.Input, CacheWrite1h: a.CacheWrite1h - b.CacheWrite1h,
		CacheWrite5m: a.CacheWrite5m - b.CacheWrite5m, CacheRead: a.CacheRead - b.CacheRead,
		Output: a.Output - b.Output, Cost: a.Cost - b.Cost, Unpriced: a.Unpriced - b.Unpriced,
	}
}
