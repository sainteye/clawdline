package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The session to-do list, kept by the broker (design-decisions T2, D36;
// board-redesign §5.2).
//
// Every dispatch leaves its root one to-do — collect it, land it — and the
// broker's facts are all that move it:
//
//   - it is made in the transaction that admits the task (create);
//   - it follows every later fact about that task in the transaction that
//     records the fact (mutateEvent) — a landing and the to-do it closes are
//     one write, and a to-do that cannot be written takes the fact with it;
//   - it is handed off, returned or dropped by the beat, on a reading that
//     positively says where its owner is (tendTodos), decided again inside
//     the write from the rows as they are then;
//   - and when the daemon starts (Run, never an ordinary pass) it makes the
//     to-dos a fact was stored without, and follows the ones still owed, so
//     nothing recorded while it was not keeping them is missed, and nothing
//     is made twice (the id is the fact's).
//
// Every task with a root gets its row the first time the broker meets it,
// even one already over — written done, with its reason — so each task is
// examined once in its life, and a start costs what is still owed, not the
// length of the history (G33).
//
// The rules themselves are pure and live in internal/domain/work. Nothing here
// asks a person, pushes a notification or writes anything a board reads.

// todoPageLimit is the most to-dos one read of a session's list answers; the
// rest are a cursor away.
const todoPageLimit = 100

// taskFacts is what the to-do rules read of a record: facts only.
func taskFacts(r Record) work.TaskFacts {
	f := work.TaskFacts{
		Task: r.ID, WorkID: r.WorkID, Title: r.Title, Kind: r.Kind, Project: r.ProjectDir,
		CreatedAt: r.CreatedAt, State: string(r.State), Ended: r.State.Terminal(),
		Attempt: r.RespawnGeneration,
	}
	if r.Root != nil && !r.Root.PollOnly {
		f.Owner, f.OwnerAssistant = r.Root.SessionID, r.Root.Assistant
	}
	if r.Landing != nil {
		f.Landing = string(r.Landing.State)
	}
	return f
}

// followTodo applies a record's facts to its to-do inside the transaction
// that holds the record, and — when owner is not nil — the owner's presence
// after them. A task with a root and no to-do yet is given one as the facts
// now stand: open, or already done — recorded either way, so it is never
// examined as new again.
func followTodo(tx *store.Tx, r Record, at time.Time, owner *work.Liveness) error {
	f := taskFacts(r)
	id := work.TodoID(work.OriginDispatch, r.ID)
	prev, err := tx.Todo(id)
	if errors.Is(err, store.ErrNoTodo) {
		t, owes := work.DispatchTodo(f, at)
		if !owes {
			return nil
		}
		kind := "todo.opened"
		if !t.State.Outstanding() {
			kind = "todo." + string(t.State)
		}
		return tx.PutTodo(t, nil, todoEvent(kind, t, nil))
	}
	if err != nil {
		return err
	}
	next, first := work.Follow(prev.Todo, f, at)
	var second *work.Transition
	if owner != nil {
		next, second = work.Tend(next, *owner, at)
	}
	events := []store.Event{}
	for _, tr := range []*work.Transition{first, second} {
		if tr != nil {
			events = append(events, todoEvent("todo."+string(tr.To), next, tr))
		}
	}
	// The to-do is on the line its task is on (lines.go): a task bound after
	// its to-do was made takes the to-do with it.
	if bound, ok := work.Bound(next, f, at); ok {
		next = bound
		events = append(events, todoEvent("todo.bound", next, nil))
	}
	if len(events) == 0 {
		return nil
	}
	return tx.PutTodo(next, &prev, events...)
}

// todoEvent is the journal line for a to-do's change. The subject is the
// to-do's id, so a to-do's history is one query.
func todoEvent(kind string, t work.Todo, tr *work.Transition) store.Event {
	body := map[string]any{"todo": t.ID, "task": t.Task, "owner": t.Owner, "state": t.State, "reason": t.Reason}
	if t.WorkID != "" {
		body["work_id"] = t.WorkID
	}
	if tr != nil {
		body["from"] = tr.From
	}
	payload, _ := json.Marshal(body)
	return store.Event{Kind: kind, Subject: t.ID, Payload: payload}
}

// applyTodo re-decides one task's to-do inside a write transaction, from the
// record and the to-do as they are then, and writes it if it moved. The task
// row itself is not rewritten. False when nothing was written — including a
// record nobody can decode, whose facts are unknown and so move nothing.
func (b *Broker) applyTodo(ctx context.Context, taskID string, owner *work.Liveness) bool {
	at := b.now()
	moved := false
	_, _, err := b.Store.UpdateBrokerTask(ctx, taskID, func(tx *store.Tx, row store.BrokerRow) (*store.BrokerWrite, error) {
		r, err := Decode(row.Record)
		if err != nil {
			return nil, nil
		}
		id := work.TodoID(work.OriginDispatch, taskID)
		before, missing := tx.Todo(id)
		if err := followTodo(tx, r, at, owner); err != nil {
			return nil, err
		}
		after, err := tx.Todo(id)
		moved = err == nil && (missing != nil || after.Version != before.Version)
		return nil, nil
	})
	return err == nil && moved
}

// ReconcileTodos makes the to-dos a fact was stored without, and follows the
// ones still owed against the facts as they are now. Run owes it once when
// the daemon starts; running it again changes nothing.
//
// A task whose to-do is still owed and that is on no line of work — stored
// before the broker bound lines at admission — is bound here: to the line a
// proposal about it named, if one did, or else by the rule an admission would
// have used (lines.go), so every owed to-do a session reads names the work it
// is part of. A binding that fails leaves the reconcile owed, and the next
// beat asks again. A task whose to-do is over is left as it is:
// nothing is owed on it, and rewriting a finished record buys nobody anything.
//
// Its cost is the tasks with a root and no row — every one of them once in
// its life, because it leaves each with a row — plus the to-dos still owed;
// never the whole history. Each is decided from a read first and written only
// when that read says something is due, then decided again inside the write.
func (b *Broker) ReconcileTodos(ctx context.Context) (int, error) {
	missing, err := b.Store.TasksWithoutTodo(ctx, work.OriginDispatch)
	if err != nil {
		return 0, storeError(err)
	}
	owed, err := b.Store.OutstandingTodos(ctx)
	if err != nil {
		return 0, storeError(err)
	}
	held := map[string]store.TodoRow{}
	ids := append([]string{}, missing...)
	for _, t := range owed {
		held[t.Task] = t
		ids = append(ids, t.Task)
	}
	rows, err := b.Store.BrokerTaskRecords(ctx, ids)
	if err != nil {
		return 0, storeError(err)
	}
	now := b.now()
	moved, unbound := 0, 0
	for _, id := range ids {
		row, ok := rows[id]
		if !ok {
			continue
		}
		r, err := Decode(row.Record)
		if err != nil {
			continue
		}
		f := taskFacts(r)
		owing := false
		if t, ok := held[id]; ok {
			next, tr := work.Follow(t.Todo, f, now)
			_, rebound := work.Bound(next, f, now)
			if (tr != nil || rebound) && b.applyTodo(ctx, id, nil) {
				moved++
			}
			owing = next.State.Outstanding()
		} else if t, owes := work.DispatchTodo(f, now); owes {
			if b.applyTodo(ctx, id, nil) {
				moved++
			}
			owing = t.State.Outstanding()
		}
		if r.WorkID != "" || !owing {
			continue
		}
		line, from, err := b.lineFor(ctx, r)
		if err == nil && line != "" {
			err = b.BindWork(ctx, id, line, from)
		}
		if err != nil {
			// Kept owed: the next beat reconciles again rather than leave the
			// task off its line until another start.
			log.Printf("todos: task %s was not put on its line: %v", id, err)
			unbound++
			continue
		}
		if line != "" {
			moved++
		}
	}
	if unbound > 0 {
		return moved, fmt.Errorf("%d owed task(s) were not put on their line", unbound)
	}
	return moved, nil
}

// tendTodos is the beat's part: the reconcile a start owes, until it has
// succeeded once, then hand off, return or drop each owed to-do whose owner a
// fresh reading positively places. It writes only when a rule says something
// moved; a pass in which every owner is where they were writes nothing.
func (b *Broker) tendTodos(ctx context.Context) int {
	moved := 0
	if b.todosOwed.Load() {
		if n, err := b.ReconcileTodos(ctx); err == nil {
			b.todosOwed.Store(false)
			moved += n
		}
	}
	owed, err := b.Store.OutstandingTodos(ctx)
	if err != nil {
		return moved
	}
	now := b.now()
	for _, t := range owed {
		owner := b.ownerLiveness(t.Owner, t.OwnerAssistant)
		if _, tr := work.Tend(t.Todo, owner, now); tr == nil {
			continue
		}
		if b.applyTodo(ctx, t.Task, &owner) {
			moved++
		}
	}
	return moved
}

// TodoFilter is which of a session's to-dos a read answers.
type TodoFilter string

const (
	TodosOutstanding TodoFilter = "outstanding"
	TodosClosed      TodoFilter = "closed"
	TodosAll         TodoFilter = "all"
)

func (f TodoFilter) states() ([]work.TodoState, bool) {
	switch f {
	case TodosOutstanding, "":
		return []work.TodoState{work.TodoStateOpen, work.TodoStateHandedOff}, true
	case TodosClosed:
		return []work.TodoState{work.TodoStateDone, work.TodoStateDropped}, true
	case TodosAll:
		return nil, true
	}
	return nil, false
}

// TodoView is one to-do as a session reads it: the row, and the signals that
// make it worth asking a person about — which this broker only reports. T4
// decides whether anybody is asked.
type TodoView struct {
	work.Todo
	Escalation []work.Signal `json:"escalation"`
}

// TodoPage is one read of a session's to-do list.
type TodoPage struct {
	Session string
	Filter  TodoFilter
	// Counts are every state's count for this session, whatever the filter.
	Counts map[work.TodoState]int
	Todos  []TodoView
	// Next is the cursor for the page after this one, empty when there is none.
	Next     string
	PageSize int
}

// SessionTodos reads one page of what a session owes, newest first.
//
// The session is named by its conversation id — the value a dispatch sends
// as `root.session_id` — never by a terminal id. A terminal id would find no
// to-dos, and an empty list for the wrong spelling reads exactly like a
// session that owes nothing, so it is refused by name instead (DG-5, DG-7).
func (b *Broker) SessionTodos(ctx context.Context, session string, filter TodoFilter, cursor string) (TodoPage, error) {
	session = strings.TrimSpace(session)
	if session == "" || strings.Contains(session, "/") {
		return TodoPage{}, refuse(http.StatusBadRequest, "bad_request", "The route must name one session id.")
	}
	states, ok := filter.states()
	if !ok {
		return TodoPage{}, refuse(http.StatusBadRequest, "bad_request",
			"state must be outstanding, closed or all.")
	}
	if filter == "" {
		filter = TodosOutstanding
	}
	q := store.TodoQuery{Owner: session, States: states, Limit: todoPageLimit + 1}
	if cursor != "" {
		created, id, ok := strings.Cut(cursor, ":")
		at, err := strconv.ParseInt(created, 10, 64)
		if !ok || err != nil || id == "" {
			return TodoPage{}, refuse(http.StatusBadRequest, "bad_request", "cursor is not one this route wrote.")
		}
		q.AfterCreated, q.AfterID = at, id
	}
	counts, err := b.Store.TodoCounts(ctx, session)
	if err != nil {
		return TodoPage{}, storeError(err)
	}
	if len(counts) == 0 {
		if s, ok := b.sessionByTerminal(ctx, session); ok && s.ConversationID != session {
			return TodoPage{}, refuseWith(http.StatusConflict, "session_id_is_terminal",
				"That is a terminal id. To-dos belong to a conversation — the root.session_id a dispatch sends — "+
					"because the conversation outlives the tab it is drawn in. Resolve it with "+
					"GET /v1/orchestrator/whoami and ask again.",
				map[string]any{"terminal": session, "session_id": nullable(s.ConversationID)})
		}
	}
	rows, err := b.Store.Todos(ctx, q)
	if err != nil {
		return TodoPage{}, storeError(err)
	}
	page := TodoPage{Session: session, Filter: filter, Counts: counts, Todos: []TodoView{}, PageSize: todoPageLimit}
	if len(rows) > todoPageLimit {
		rows = rows[:todoPageLimit]
		last := rows[len(rows)-1]
		page.Next = strconv.FormatInt(last.CreatedAt.Unix(), 10) + ":" + last.ID
	}
	facts := map[string]work.TaskFacts{}
	ids := []string{}
	for _, t := range rows {
		if t.State.Outstanding() {
			ids = append(ids, t.Task)
		}
	}
	if len(ids) > 0 {
		records, err := b.Store.BrokerTaskRecords(ctx, ids)
		if err != nil {
			return TodoPage{}, storeError(err)
		}
		for id, row := range records {
			if r, err := Decode(row.Record); err == nil {
				facts[id] = taskFacts(r)
			}
		}
	}
	now := b.now()
	held := map[string]bool{}
	for _, t := range rows {
		if _, asked := held[t.WorkID]; !t.State.Outstanding() || t.WorkID == "" || asked {
			continue
		}
		it, err := b.Store.WorkItem(ctx, t.WorkID)
		switch {
		case err == nil:
			held[t.WorkID] = it.Place == work.PlaceBoard || it.Place == work.PlaceBacklog
		case errors.Is(err, store.ErrNoWork):
			held[t.WorkID] = false
		default:
			return TodoPage{}, storeError(err)
		}
	}
	for _, t := range rows {
		signals := []work.Signal{}
		if f, ok := facts[t.Task]; ok {
			signals = work.Escalation(t.Todo, f, held[t.WorkID], now)
		}
		page.Todos = append(page.Todos, TodoView{Todo: t.Todo, Escalation: signals})
	}
	return page, nil
}
