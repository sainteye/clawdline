package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// WorkBoard keeps the board and the Backlog (design-decisions T3; the rules
// are internal/domain/work/board.go).
//
// Three things move an item, and each is one transaction that reads the rows
// it decides from, decides, and writes the change with the move that records
// it:
//
//   - a person's command (Command, Create), filed under its request receipt
//     in the same transaction (D03);
//   - the sweep (Sweep, run on its own clock by Run), which applies the
//     automatic rules to the facts: a landing closes an item, a delivery puts
//     it in the closure queue, a dispatch that names a planned item brings it
//     onto the board, three quiet days send it back;
//   - nothing else. A read never writes.
//
// The sweep is how the board follows the broker without the broker calling
// it: the broker's package belongs to another line of work (W5) while this one
// is built, and a board that could stop the broker's beat by failing inside it
// is the wrong way round. A read shows what the facts say at once, by the
// same pure function the sweep applies (work.Derive), so a landing is on the
// page before the sweep has recorded it; the sweep records it within one tick,
// once. When the broker records a fact, one line in its transaction can make
// the two writes one (report.md).
type WorkBoard struct {
	Store  *store.Store
	Policy work.Policy
	// Rules are the automatic rules; nil is work.SweepRules().
	Rules []work.SweepRule
	Now   func() time.Time
	// OpenLimit is the register's `work.open`; zero is store.WorkOpenLimit.
	OpenLimit int64
	// Also runs at the end of every pass, on the same clock: the parts of
	// the board a person takes part in (T4 — a proposal's wait, a decision's
	// due, the digest). Its error is the pass's.
	Also func(ctx context.Context) error

	mu    sync.Mutex
	pulse WorkPulse
	tick  time.Duration
	run   bool
}

// NewWorkBoard is the board with the default clocks.
func NewWorkBoard(st *store.Store) *WorkBoard {
	return &WorkBoard{Store: st, Policy: work.DefaultPolicy(), Now: time.Now}
}

// Page sizes and field bounds. The page is fixed (D34): no size parameter,
// no byte budget, no prefix selector.
const (
	WorkPageSize = 100
	// workScanLimit is the most board rows one read derives: the open items
	// are at most OpenLimit, and the recently closed ride on top.
	workScanLimit     = 5_000
	workTitleLimit    = 200
	workProjectLimit  = 1_024
	workAcceptLimit   = 2_000
	workOwnerLimit    = 256
	workActorLimit    = 256
	workStalledFactor = 3
)

func (w *WorkBoard) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WorkBoard) rules() []work.SweepRule {
	if w.Rules != nil {
		return w.Rules
	}
	return work.SweepRules()
}

func (w *WorkBoard) openLimit() int64 {
	if w.OpenLimit > 0 && w.OpenLimit < store.WorkOpenLimit {
		return w.OpenLimit
	}
	return store.WorkOpenLimit
}

// Facts reads what the board's rules need of a set of task rows. A row that
// cannot be decoded is counted, not guessed at: its task might be the one
// still running, and unknown moves nothing (DG-7).
func Facts(rows []store.BrokerRow) ([]work.TaskFacts, int) {
	out := []work.TaskFacts{}
	unknown := 0
	for _, row := range rows {
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			unknown++
			continue
		}
		f := work.TaskFacts{
			Task: r.ID, WorkID: r.WorkID, Title: r.Title, Kind: r.Kind, Project: r.ProjectDir,
			CreatedAt: seconds(r.CreatedAt), State: string(r.State), Ended: r.State.Terminal(),
			Attempt: r.RespawnGeneration, FinishedAt: seconds(r.FinishedAt),
		}
		if r.Root != nil && !r.Root.PollOnly {
			f.Owner, f.OwnerAssistant = r.Root.SessionID, r.Root.Assistant
		}
		if r.Landing != nil {
			f.Landing, f.LandedAt = string(r.Landing.State), seconds(r.Landing.At)
			f.LandingTarget = r.Landing.Target
		}
		out = append(out, f)
	}
	return out, unknown
}

// seconds is a time at the store's resolution, so a task's time and an
// item's compare the way the store's own queries compare them.
func seconds(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return time.Unix(t.Unix(), 0)
}

// ——— The sweep ———

// WorkPulse is the sweep's account of its last pass.
type WorkPulse struct {
	At         time.Time
	Passes     int64
	Considered int
	Moved      int
	// Unknown is items left alone because a task bound to them could not be
	// read.
	Unknown int
	Err     string
}

// Sweep applies the automatic rules once to every item one of them might
// move. Each change is decided again inside its write, from the rows as they
// are then; a pass in which nothing changed writes nothing.
func (w *WorkBoard) Sweep(ctx context.Context) WorkPulse {
	p := WorkPulse{At: w.now()}
	ids, err := w.Store.WorkSweepCandidates(ctx)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	rows, err := w.Store.WorkTasks(ctx, ids)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	for _, id := range ids {
		p.Considered++
		it, err := w.Store.WorkItem(ctx, id)
		if err != nil {
			continue
		}
		facts, unknown := Facts(rows[id])
		if unknown > 0 {
			p.Unknown++
			continue
		}
		if _, ok := work.Next(w.rules(), it, facts, w.Policy, w.now()); !ok {
			continue
		}
		moved, err := w.applyRules(ctx, id)
		if err != nil {
			p.Err = err.Error()
			continue
		}
		if moved {
			p.Moved++
		}
	}
	if w.Also != nil {
		if err := w.Also(ctx); err != nil && p.Err == "" {
			p.Err = err.Error()
		}
	}
	return p
}

// applyRules decides one item again inside a write and records the change.
func (w *WorkBoard) applyRules(ctx context.Context, id string) (bool, error) {
	moved := false
	err := w.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		it, err := tx.Item(id)
		if err != nil {
			return err
		}
		rows, err := tx.Tasks(id)
		if err != nil {
			return err
		}
		facts, unknown := Facts(rows)
		if unknown > 0 {
			return nil
		}
		now := w.now()
		c, ok := work.Next(w.rules(), it, facts, w.Policy, now)
		if !ok {
			return nil
		}
		moved = true
		return tx.Put(it, c.Apply(it, now), store.MoveOf(id, c, now))
	})
	if errors.Is(err, store.ErrConflict) {
		// Somebody changed it between the read and the write; the next pass
		// decides from what they left.
		return false, nil
	}
	return moved && err == nil, err
}

// Run keeps the sweep on its clock until ctx ends: a pass at once, then one
// per tick. A pass that panics is recorded as an error and the clock goes on.
func (w *WorkBoard) Run(ctx context.Context, tick time.Duration) {
	w.mu.Lock()
	w.tick, w.run = tick, true
	w.mu.Unlock()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		p := w.safeSweep(ctx)
		w.mu.Lock()
		p.Passes = w.pulse.Passes + 1
		w.pulse = p
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.run = false
			w.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

func (w *WorkBoard) safeSweep(ctx context.Context) (p WorkPulse) {
	defer func() {
		if v := recover(); v != nil {
			p = WorkPulse{At: w.now(), Err: fmt.Sprintf("panic: %v", v)}
		}
	}()
	return w.Sweep(ctx)
}

// Pulse is the last pass, and the clock it runs on.
func (w *WorkBoard) Pulse() (WorkPulse, time.Duration, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pulse, w.tick, w.run
}

// Stalled says the sweep is supposed to run and has not finished a pass for
// three ticks (DG-1): the board is then not following its facts, and
// /v1/health says so.
func (w *WorkBoard) Stalled() bool {
	p, tick, running := w.Pulse()
	if !running || tick <= 0 || p.At.IsZero() {
		return false
	}
	return w.now().Sub(p.At) > workStalledFactor*tick
}

// ——— A person's commands ———

// WorkError is a refusal with the status and code its route answers.
type WorkError struct {
	Status  int
	Code    string
	Message string
	// Current is the item as it is, for a version conflict.
	Current *WorkView
}

func (e *WorkError) Error() string { return e.Code + ": " + e.Message }

func workRefusal(status int, code, msg string) *WorkError {
	return &WorkError{Status: status, Code: code, Message: msg}
}

// storeRefusal turns a store failure into the refusal its route answers.
func storeRefusal(err error) error {
	var we *WorkError
	var r *work.Refusal
	switch {
	case err == nil:
		return nil
	case errors.As(err, &we):
		return we
	case errors.As(err, &r):
		return workRefusal(r.Status, r.Code, r.Message)
	case errors.Is(err, store.ErrNoWork):
		return workRefusal(404, "work_not_found", "No work item has that id.")
	case errors.Is(err, store.ErrWorkFull):
		return workRefusal(507, "work_full",
			"The board and the Backlog hold as many open items as this machine keeps; nothing was added. Close or drop some first.")
	case errors.Is(err, store.ErrBusy):
		return workRefusal(503, "store_busy", "The store was held by another writer; nothing was done. Retry.")
	case errors.Is(err, store.ErrConflict):
		return workRefusal(409, "version_conflict", "The item changed while this was being decided; nothing was done. Read it and decide again.")
	case strings.Contains(err.Error(), "work_in_two_places"):
		return workRefusal(409, "work_in_two_places", "A work item is on the board or in the Backlog, never both; nothing was done.")
	}
	return workRefusal(503, "store_unavailable", "The board could not be read or written; nothing was done.")
}

// WorkCommand is one command as its route decoded it.
type WorkCommand struct {
	work.Command
	// Principal is the credential that carried it (local, device:<id>,
	// machine), recorded beside the actor in the move's evidence.
	Principal string
	// ExpectedVersion, when set, is the version the person decided from: a
	// different one refuses the command rather than applying it to an item
	// they have not seen.
	ExpectedVersion *int64
}

// Filer is how a route files its answer inside the change's own
// transaction: the answer for the item as it now is.
type Filer func(WorkView) (store.ReceiptKey, store.ReceiptAnswer, bool)

// Command carries out a person's command on one item, or refuses it and
// writes nothing.
func (w *WorkBoard) Command(ctx context.Context, id string, cmd WorkCommand, file Filer) (WorkView, error) {
	if err := checkActor(cmd.Actor); err != nil {
		return WorkView{}, err
	}
	if n := utf8.RuneCountInString(cmd.Owner); n > workOwnerLimit {
		return WorkView{}, workRefusal(400, "invalid_owner", "owner is at most "+strconv.Itoa(workOwnerLimit)+" characters.")
	}
	var out WorkView
	err := w.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		it, err := tx.Item(id)
		if err != nil {
			return err
		}
		rows, err := tx.Tasks(id)
		if err != nil {
			return err
		}
		facts, unknown := Facts(rows)
		if cmd.ExpectedVersion != nil && *cmd.ExpectedVersion != it.Version {
			v := w.view(it, facts, unknown)
			e := workRefusal(409, "version_conflict", "The item has changed since you read it; nothing was done.")
			e.Current = &v
			return e
		}
		if unknown > 0 {
			return workRefusal(409, "facts_unknown",
				"A task bound to this item could not be read, so what it is now is unknown; nothing was done.")
		}
		now := w.now()
		c, err := work.Decide(it, facts, cmd.Command, w.Policy, now)
		if err != nil {
			return err
		}
		if cmd.Principal != "" {
			c.Evidence["principal"] = cmd.Principal
		}
		next := c.Apply(it, now)
		if err := tx.Put(it, next, store.MoveOf(id, c, now)); err != nil {
			return err
		}
		next.Version = it.Version + 1
		out = w.view(next, facts, 0)
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, storeRefusal(err)
}

func checkActor(actor string) error {
	if actor == "" || utf8.RuneCountInString(actor) > workActorLimit {
		return workRefusal(400, "invalid_actor", "A command names who gave it.")
	}
	return nil
}

// NewWork is a person's new item.
type NewWork struct {
	Title      string
	Project    string
	Acceptance string
	// Place is the board or the Backlog. An item nobody follows is not made:
	// it is a to-do, and a to-do is the broker's to make (T2).
	Place   work.Place
	Owner   string
	StartOn string
	Rank    int64
	Actor   string
	// Principal is the credential that carried it.
	Principal string
}

// Create makes a new item where the person put it, with its first move.
func (w *WorkBoard) Create(ctx context.Context, n NewWork, file Filer) (WorkView, error) {
	n.Title, n.Project, n.Acceptance = strings.TrimSpace(n.Title), strings.TrimSpace(n.Project), strings.TrimSpace(n.Acceptance)
	switch {
	case checkActor(n.Actor) != nil:
		return WorkView{}, checkActor(n.Actor)
	case n.Title == "" || utf8.RuneCountInString(n.Title) > workTitleLimit:
		return WorkView{}, workRefusal(400, "invalid_title", "title is 1 to "+strconv.Itoa(workTitleLimit)+" characters.")
	case n.Project == "" || utf8.RuneCountInString(n.Project) > workProjectLimit:
		// The work's project is said, never taken from a session's directory
		// (board-redesign §1.7-2, §10 #9).
		return WorkView{}, workRefusal(400, "project_required", "project names the work's project.")
	case utf8.RuneCountInString(n.Acceptance) > workAcceptLimit:
		return WorkView{}, workRefusal(400, "invalid_acceptance", "acceptance is at most "+strconv.Itoa(workAcceptLimit)+" characters.")
	case utf8.RuneCountInString(n.Owner) > workOwnerLimit:
		return WorkView{}, workRefusal(400, "invalid_owner", "owner is at most "+strconv.Itoa(workOwnerLimit)+" characters.")
	case n.StartOn != "" && !work.ValidStartOn(n.StartOn):
		return WorkView{}, workRefusal(400, "invalid_start_on", "start_on is a date, YYYY-MM-DD.")
	case n.Rank < 0:
		return WorkView{}, workRefusal(400, "invalid_rank", "rank is a whole number, zero for unranked.")
	}
	now := seconds(w.now())
	it := work.Item{ID: newWorkID(), Project: n.Project, Title: n.Title, Acceptance: n.Acceptance,
		CreatedAt: now, CreatedBy: n.Actor, Place: n.Place, PlacedAt: now}
	c := work.Change{From: work.PlaceNone, To: n.Place, Trigger: "created", Actor: n.Actor,
		Evidence: map[string]any{"op": "create"}}
	if n.Principal != "" {
		c.Evidence["principal"] = n.Principal
	}
	switch n.Place {
	case work.PlaceBoard:
		owner := n.Owner
		if owner == "" {
			owner = work.OwnerUser
		}
		it.State, it.Owner, it.Commitment, it.Since, it.EvidenceAt = work.ItemActive, owner, work.CommitAssigned, now, now
		it.StartOn = n.StartOn
		c.Evidence["owner"] = owner
	case work.PlaceBacklog:
		if n.Owner != "" {
			return WorkView{}, workRefusal(400, "owner_on_board_only", "An owner is given to an item on the board; the Backlog has none.")
		}
		it.State, it.StartOn, it.Rank, it.ReviewedAt = work.ItemPlanned, n.StartOn, n.Rank, now
	default:
		return WorkView{}, workRefusal(400, "invalid_place", "place is board or backlog.")
	}
	c.State = it.State
	var out WorkView
	err := w.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		if err := tx.Create(it, store.MoveOf(it.ID, c, now), w.openLimit()); err != nil {
			return err
		}
		out = w.view(it, nil, 0)
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, storeRefusal(err)
}

// newWorkID mints a work id: a lowercase version-4 UUID, the shape a
// dispatch's work_id is checked against (D36).
func newWorkID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ——— Reading ———

// WorkView is one item as a person reads it: the stored row, what the facts
// say now, and where on the board that puts it.
type WorkView struct {
	Item    work.Item
	Derived work.Derived
	Section work.Section
	// OnPage is false for a board item closed longer ago than the short
	// window: it is read by its id, not listed.
	OnPage bool
	// Unknown is bound tasks that could not be read. While there are any,
	// no rule moves the item.
	Unknown int
	Tasks   []work.TaskFacts
}

func (w *WorkBoard) view(it work.Item, facts []work.TaskFacts, unknown int) WorkView {
	now := w.now()
	v := WorkView{Item: it, Derived: work.Derive(it, facts, w.Policy, now), Unknown: unknown, Tasks: facts}
	if it.Place == work.PlaceBoard {
		v.Section, v.OnPage = work.SectionOf(it, v.Derived, w.Policy, now)
	}
	return v
}

// Item reads one item, its bound tasks and what they make it now.
func (w *WorkBoard) Item(ctx context.Context, id string) (WorkView, error) {
	it, err := w.Store.WorkItem(ctx, id)
	if err != nil {
		return WorkView{}, storeRefusal(err)
	}
	rows, err := w.Store.WorkTasks(ctx, []string{id})
	if err != nil {
		return WorkView{}, storeRefusal(err)
	}
	facts, unknown := Facts(rows[id])
	return w.view(it, facts, unknown), nil
}

// BoardPage is one read of the board.
type BoardPage struct {
	Counts map[work.Section]int
	Rows   []WorkView
	Next   string
	// Truncated says the board held more rows than one read derives; the
	// counts are then of the rows read, and say so.
	Truncated bool
}

// Board reads one page of the board: every section's count, and the rows of
// one section (or of all of them, in order, when section is empty).
func (w *WorkBoard) Board(ctx context.Context, project string, section work.Section, cursor string) (BoardPage, error) {
	now := w.now()
	items, truncated, err := w.Store.WorkBoard(ctx, project, now.Add(-w.Policy.Short), workScanLimit)
	if err != nil {
		return BoardPage{}, storeRefusal(err)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	rows, err := w.Store.WorkTasks(ctx, ids)
	if err != nil {
		return BoardPage{}, storeRefusal(err)
	}
	page := BoardPage{Counts: map[work.Section]int{}, Rows: []WorkView{}, Truncated: truncated}
	all := []WorkView{}
	for _, it := range items {
		facts, unknown := Facts(rows[it.ID])
		v := w.view(it, facts, unknown)
		if !v.OnPage {
			continue
		}
		page.Counts[v.Section]++
		if section == "" || v.Section == section {
			all = append(all, v)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return boardBefore(all[i], all[j]) })
	var after *WorkView
	if cursor != "" {
		c, err := parseBoardCursor(cursor)
		if err != nil {
			return BoardPage{}, err
		}
		after = &c
	}
	for _, v := range all {
		if after != nil && !boardBefore(*after, v) {
			continue
		}
		if len(page.Rows) == WorkPageSize {
			page.Next = boardCursor(page.Rows[len(page.Rows)-1])
			break
		}
		page.Rows = append(page.Rows, v)
	}
	return page, nil
}

func sectionIndex(s work.Section) int {
	for i, x := range work.Sections {
		if x == s {
			return i
		}
	}
	return len(work.Sections)
}

// boardBefore is the board's order: section, newest placed first, then id.
func boardBefore(a, b WorkView) bool {
	if x, y := sectionIndex(a.Section), sectionIndex(b.Section); x != y {
		return x < y
	}
	if !a.Item.PlacedAt.Equal(b.Item.PlacedAt) {
		return a.Item.PlacedAt.After(b.Item.PlacedAt)
	}
	return a.Item.ID < b.Item.ID
}

func boardCursor(v WorkView) string {
	return fmt.Sprintf("%d:%d:%s", sectionIndex(v.Section), v.Item.PlacedAt.Unix(), v.Item.ID)
}

var errCursor = workRefusal(400, "bad_cursor", "cursor is not one this route wrote.")

func parseBoardCursor(s string) (WorkView, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return WorkView{}, errCursor
	}
	i, err := strconv.Atoi(parts[0])
	if err != nil || i < 0 || i >= len(work.Sections) {
		return WorkView{}, errCursor
	}
	at, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return WorkView{}, errCursor
	}
	return WorkView{Section: work.Sections[i], Item: work.Item{ID: parts[2], PlacedAt: time.Unix(at, 0)}}, nil
}

// BacklogPage is one read of the Backlog.
type BacklogPage struct {
	Counts map[work.ItemState]int
	Rows   []WorkView
	Next   string
}

// Backlog reads one page of the planned Backlog in its order.
func (w *WorkBoard) Backlog(ctx context.Context, project, cursor string) (BacklogPage, error) {
	q := store.BacklogQuery{Project: project, Limit: WorkPageSize + 1}
	if cursor != "" {
		parts := strings.SplitN(cursor, ":", 3)
		if len(parts) != 3 || parts[2] == "" {
			return BacklogPage{}, errCursor
		}
		rank, err1 := strconv.ParseInt(parts[0], 10, 64)
		created, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil {
			return BacklogPage{}, errCursor
		}
		q.AfterRank, q.AfterCreated, q.AfterID = rank, created, parts[2]
	}
	items, err := w.Store.WorkBacklog(ctx, q)
	if err != nil {
		return BacklogPage{}, storeRefusal(err)
	}
	counts, err := w.Store.BacklogCounts(ctx, project)
	if err != nil {
		return BacklogPage{}, storeRefusal(err)
	}
	page := BacklogPage{Counts: counts, Rows: []WorkView{}}
	if len(items) > WorkPageSize {
		items = items[:WorkPageSize]
		last := items[len(items)-1]
		rank := last.Rank
		if rank == 0 {
			rank = 9223372036854775807
		}
		page.Next = fmt.Sprintf("%d:%d:%s", rank, last.CreatedAt.Unix(), last.ID)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	rows, err := w.Store.WorkTasks(ctx, ids)
	if err != nil {
		return BacklogPage{}, storeRefusal(err)
	}
	for _, it := range items {
		facts, unknown := Facts(rows[it.ID])
		page.Rows = append(page.Rows, w.view(it, facts, unknown))
	}
	return page, nil
}

// Moves reads one page of an item's moves, oldest first.
func (w *WorkBoard) Moves(ctx context.Context, id string, after int64) ([]store.WorkMove, int64, error) {
	if _, err := w.Store.WorkItem(ctx, id); err != nil {
		return nil, 0, storeRefusal(err)
	}
	moves, err := w.Store.WorkMoves(ctx, id, after, WorkPageSize+1)
	if err != nil {
		return nil, 0, storeRefusal(err)
	}
	var next int64
	if len(moves) > WorkPageSize {
		moves = moves[:WorkPageSize]
		next = moves[len(moves)-1].Seq
	}
	return moves, next, nil
}
