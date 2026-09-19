package work

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The board and the Backlog: step 3 of board-redesign §9, T3 of
// docs/design-decisions.md §6 (D30, D31, D33).
//
// Two of the three structures, each for one reader:
//
//   - the board holds what a person needs to know now, and nothing gets there
//     without a commitment a fact can show: a dispatch that named it, a person
//     who started or assigned it, a start date inside the short window, or a
//     delivery waiting to be closed;
//   - the Backlog holds what is planned and not committed to. Only a person
//     removes anything from it (§5.3).
//
// A work item lives in exactly one of them, or in neither when a person said
// it does not need following (it is then only its sessions' to-dos, T2).
// Every change of place — and every change of an item's life on the board — is
// a Change, recorded as one row of `moves` with who made it and the fact it
// rests on. Nothing here is stored or read from disk: these are the rules, and
// the store applies them inside the transaction that holds the rows they were
// decided from.
//
// Two kinds of change and one rule between them (D31):
//
//   - automatic changes follow broker facts and the clock, by the ordered
//     rules in sweepRules — a landing closes the item, a delivery puts it in
//     the closure queue, three quiet days send it back to the Backlog;
//   - a person's commands (Decide) start, schedule, defer, accept, rework,
//     drop, hand over or stop following an item;
//   - and a person's command never says what only the broker may say. There
//     is no command that lands anything: "accept" closes an item as accepted,
//     and an item is landed only when every delivery bound to it is landed in
//     the broker's own record (D01).
//
// The display state is derived on every read and never written back (D04):
// Derive answers what the facts say now, the stored state is what the last
// recorded change said, and the sweep writes a change only when the two
// differ, once.

// Place is the structure a work item is in.
type Place string

const (
	PlaceBoard   Place = "board"
	PlaceBacklog Place = "backlog"
	// PlaceTodo is an item a person said needs no following: only its
	// sessions' to-dos track it, and nothing of it is on a person's page.
	PlaceTodo Place = "todo"
	// PlaceNone is before the item existed: the from of its first move.
	PlaceNone Place = "none"
)

// ItemState is an item's life inside its structure. The board has four, the
// Backlog two (board-redesign §3.2).
type ItemState string

const (
	ItemActive          ItemState = "active"
	ItemAwaitingClosure ItemState = "awaiting_closure"
	ItemDone            ItemState = "done"
	ItemDropped         ItemState = "dropped"
	ItemPlanned         ItemState = "planned"
)

// Closed says nothing more happens to the item unless a new fact reopens it.
func (s ItemState) Closed() bool { return s == ItemDone || s == ItemDropped }

// Commitment is why an item is on the board rather than in the Backlog: the
// boundary is a fact, never an adjective (board-redesign §3.2).
type Commitment string

const (
	CommitDispatch  Commitment = "dispatch"  // a dispatch named it
	CommitAssigned  Commitment = "assigned"  // a person started it, or handed it to someone
	CommitScheduled Commitment = "scheduled" // its start date is inside the short window
	CommitDecision  Commitment = "decision"  // a person owes it an answer (T4)
	CommitDelivered Commitment = "delivered" // it delivered and waits to be closed
)

// Why a closed item is closed.
const (
	ClosedLanded      = "landed"      // the broker's landing record, for every delivery
	ClosedAccepted    = "accepted"    // a person accepted what was delivered
	ClosedUnconfirmed = "unconfirmed" // asked once, nobody answered, the wait passed
	ClosedDropped     = "dropped"     // a person dropped it
)

// OwnerUser is an item a person holds themselves: on the board and not yet
// handed to any session.
const OwnerUser = "user"

// Actors of the automatic changes. A person's command carries its own.
const (
	ActorBroker = "broker" // a broker fact: a delivery, a landing
	ActorRule   = "rule"   // the clock or the calendar
)

// Policy is the clocks of U3, all of them settings (board-redesign §10).
type Policy struct {
	// Stall is how long an active item may go without a fact before it goes
	// back to the Backlog (3 days).
	Stall time.Duration
	// Short is the window in which a start date is a commitment (7 days).
	Short time.Duration
	// ClosureWait is how long a delivery goes unanswered after a person was
	// asked about it before it closes as unconfirmed (7 days).
	ClosureWait time.Duration
	// Location is the calendar start dates are read in.
	Location *time.Location
}

// DefaultPolicy is board-redesign §10 #2, #3 and #5: 3 days, 7 days, 7 days.
func DefaultPolicy() Policy {
	return Policy{Stall: DefaultStall, Short: 7 * 24 * time.Hour, ClosureWait: 7 * 24 * time.Hour,
		Location: time.Local}
}

func (p Policy) loc() *time.Location {
	if p.Location == nil {
		return time.UTC
	}
	return p.Location
}

// Item is one work item and where it is, as stored.
type Item struct {
	ID         string
	Project    string
	Title      string
	Acceptance string
	CreatedAt  time.Time
	CreatedBy  string

	Place Place
	State ItemState

	// Owner is who the board says holds it: a root's conversation id, or
	// OwnerUser. Empty off the board.
	Owner      string
	Commitment Commitment
	// Since opens the current stay on the board: bound tasks that ended
	// before it belong to an earlier stay and do not decide this one (a
	// rework, a reopening).
	Since time.Time
	// EvidenceAt is the last fact recorded on the item itself — it entered
	// the board, a person started, reworked or handed it over. Bound tasks
	// bring their own times.
	EvidenceAt time.Time
	// AskedAt is when a person was asked about a delivery waiting to be
	// closed (T4's digest). Zero until somebody asks.
	AskedAt time.Time
	// DecisionSince is when the oldest decision still waiting for a person
	// on this item was asked; zero when none waits. While one waits, the
	// quiet clock does not run: an item waiting on a person has not stalled
	// (§5.1, "no decision waiting on a person"). Read with the item; never
	// stored on it.
	DecisionSince time.Time
	ClosedReason  string
	ClosedAt      time.Time

	// StartOn is the planned start date, YYYY-MM-DD, or empty.
	StartOn string
	// Rank orders the Backlog, lowest first; zero is unranked, after them.
	Rank       int64
	ReviewedAt time.Time

	// PlacedAt is when the item entered its current place.
	PlacedAt time.Time
	// Version is the compare-and-set counter a write must still find.
	Version int64
}

// Outcome is what one bound task amounts to for the item.
type Outcome string

const (
	OutcomeLive      Outcome = "live"      // still running
	OutcomeLanded    Outcome = "landed"    // the landing record says landed or nothing_to_land
	OutcomeDelivered Outcome = "delivered" // ended with a result, not landed
	OutcomeFailed    Outcome = "failed"    // ended without a result
)

// OutcomeOf reads one task's facts: the broker's state and its landing
// record, nothing else.
func OutcomeOf(f TaskFacts) Outcome {
	if !f.Ended {
		return OutcomeLive
	}
	switch f.Landing {
	case "landed", "nothing_to_land":
		return OutcomeLanded
	}
	if f.State == "success" {
		return OutcomeDelivered
	}
	return OutcomeFailed
}

// TaskCounts are an item's bound tasks by outcome, over every stay.
type TaskCounts struct {
	Total     int `json:"total"`
	Live      int `json:"live"`
	Landed    int `json:"landed"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
}

// Derived is what the facts say about an item now. It is computed on every
// read and never stored.
type Derived struct {
	State  ItemState
	Reason string
	// Landed says the broker has landed every delivery of this stay. Only a
	// landing record makes it true; no command does.
	Landed bool
	Tasks  TaskCounts
	// LastEvidenceAt is the newest fact about the item: its own, or a bound
	// task's creation, end or landing.
	LastEvidenceAt time.Time
	// StallAt is when an active item with nothing running goes back to the
	// Backlog; zero otherwise.
	StallAt time.Time
	// ClosureDueAt is when a delivery a person was asked about closes as
	// unconfirmed; zero until somebody asks.
	ClosureDueAt time.Time
}

// Reasons Derive gives.
const (
	ReasonTaskRunning       = "task_running"
	ReasonNoDispatch        = "no_dispatch"
	ReasonAttemptsFailed    = "attempts_failed"
	ReasonDeliveredUnlanded = "delivered_unlanded"
	ReasonRedispatched      = "redispatched"
	ReasonPlanned           = "planned"
	ReasonNotFollowed       = "not_followed"
)

// cycle is the tasks that decide the current stay: every task still running,
// and every task made at or after the stay began.
func cycle(tasks []TaskFacts, since time.Time) []TaskFacts {
	out := []TaskFacts{}
	for _, t := range tasks {
		if !t.Ended || !t.CreatedAt.Before(since) {
			out = append(out, t)
		}
	}
	return out
}

func lastEvidence(it Item, tasks []TaskFacts) time.Time {
	last := it.EvidenceAt
	for _, at := range []time.Time{it.PlacedAt} {
		if at.After(last) {
			last = at
		}
	}
	for _, t := range tasks {
		for _, at := range []time.Time{t.CreatedAt, t.FinishedAt, t.LandedAt} {
			if at.After(last) {
				last = at
			}
		}
	}
	return last
}

// Derive answers what an item is now, from its stored row, the facts of every
// task bound to it (by work_id, D36) and the clock.
func Derive(it Item, tasks []TaskFacts, p Policy, now time.Time) Derived {
	d := Derived{LastEvidenceAt: lastEvidence(it, tasks)}
	for _, t := range tasks {
		d.Tasks.Total++
		switch OutcomeOf(t) {
		case OutcomeLive:
			d.Tasks.Live++
		case OutcomeLanded:
			d.Tasks.Landed++
		case OutcomeDelivered:
			d.Tasks.Delivered++
		case OutcomeFailed:
			d.Tasks.Failed++
		}
	}
	switch it.Place {
	case PlaceBacklog:
		d.State, d.Reason = it.State, ReasonPlanned
		return d
	case PlaceTodo, PlaceNone:
		d.State, d.Reason = "", ReasonNotFollowed
		return d
	}
	if it.State == ItemDropped {
		d.State, d.Reason = ItemDropped, ClosedDropped
		return d
	}
	since := it.Since
	if it.State == ItemDone {
		// A closed item stays closed until a task made after it closed says
		// otherwise: a new dispatch is new work on the same item.
		newer := cycle(tasks, it.ClosedAt)
		reopened := false
		for _, t := range newer {
			if !t.CreatedAt.Before(it.ClosedAt) {
				reopened = true
			}
		}
		if !reopened {
			d.State, d.Reason = ItemDone, it.ClosedReason
			d.Landed = it.ClosedReason == ClosedLanded
			return d
		}
		since = it.ClosedAt
	}
	live, landed, delivered, total := 0, 0, 0, 0
	for _, t := range cycle(tasks, since) {
		total++
		switch OutcomeOf(t) {
		case OutcomeLive:
			live++
		case OutcomeLanded:
			landed++
		case OutcomeDelivered:
			delivered++
		}
	}
	reopenedReason := func(r string) string {
		if it.State == ItemDone {
			return ReasonRedispatched
		}
		return r
	}
	switch {
	case live > 0:
		d.State, d.Reason = ItemActive, reopenedReason(ReasonTaskRunning)
	case delivered > 0:
		d.State, d.Reason = ItemAwaitingClosure, ReasonDeliveredUnlanded
		if it.State == ItemAwaitingClosure && !it.AskedAt.IsZero() {
			d.ClosureDueAt = it.AskedAt.Add(p.ClosureWait)
		}
	case landed > 0:
		d.State, d.Reason, d.Landed = ItemDone, ClosedLanded, true
	case total > 0:
		d.State, d.Reason = ItemActive, reopenedReason(ReasonAttemptsFailed)
	default:
		d.State, d.Reason = ItemActive, ReasonNoDispatch
	}
	// The three-day clock runs on an active item with nothing running — but
	// not on one a start date put on the board and nothing has been
	// dispatched for yet: until that date has passed, its commitment is the
	// date, and schedule_missed is the rule that answers for it.
	waitingForDate := it.Commitment == CommitScheduled && it.StartOn != "" && total == 0
	if d.State == ItemActive && live == 0 && !waitingForDate && it.DecisionSince.IsZero() {
		d.StallAt = d.LastEvidenceAt.Add(p.Stall)
	}
	return d
}

// Change is one recorded change to an item: where it goes, what it becomes
// there, and the move that says who made it and on what fact.
type Change struct {
	From  Place
	To    Place
	State ItemState
	// Trigger is the rule's or the command's code; Actor who made it.
	Trigger string
	Actor   string
	// Evidence is the fact it rests on, stored as the move's JSON.
	Evidence map[string]any

	// The item's fields after the change. Only the fields a change sets are
	// read by the store; the rest are the item's own.
	Owner        string
	Commitment   Commitment
	Since        time.Time
	EvidenceAt   time.Time
	ClosedReason string
	StartOn      *string
	Rank         *int64
	Reviewed     bool
	// Asked records that a person was asked about a delivery waiting to be
	// closed (T4's digest): the closure wait runs from here.
	Asked bool
}

// Apply is the item after a change, at time at.
func (c Change) Apply(it Item, at time.Time) Item {
	next := it
	next.State = c.State
	if c.To != it.Place {
		next.Place, next.PlacedAt = c.To, at
		if c.To != PlaceBoard {
			next.Owner, next.Commitment, next.AskedAt = "", "", time.Time{}
		}
	}
	if c.Owner != "" {
		next.Owner = c.Owner
	}
	if c.Commitment != "" {
		next.Commitment = c.Commitment
	}
	if !c.Since.IsZero() {
		next.Since = c.Since
	}
	if !c.EvidenceAt.IsZero() {
		next.EvidenceAt = c.EvidenceAt
	}
	if c.StartOn != nil {
		next.StartOn = *c.StartOn
	} else if it.Place == PlaceBoard && c.To == PlaceBacklog {
		// A date that brought an item onto the board is spent when it
		// leaves: kept, the window rule would bring it straight back.
		next.StartOn = ""
	}
	if c.Rank != nil {
		next.Rank = *c.Rank
	}
	if c.Reviewed {
		next.ReviewedAt = at
	}
	if c.Asked {
		next.AskedAt = at
	}
	if c.State.Closed() {
		next.ClosedReason, next.ClosedAt = c.ClosedReason, at
	} else {
		next.ClosedReason, next.ClosedAt = "", time.Time{}
	}
	return next
}

// SweepRule is one automatic rule. The first rule that answers a change
// decides; the list is data so that a test can take one rule out and watch
// the failure it exists to prevent come back (DG-8).
type SweepRule struct {
	Code  string
	Place Place
	// Decide answers the change the rule makes, or false.
	Decide func(it Item, d Derived, tasks []TaskFacts, p Policy, now time.Time) (Change, bool)
}

// Triggers of the automatic rules. Each is a moves.trigger value.
const (
	TriggerLanded      = "landed"
	TriggerDelivered   = "delivered"
	TriggerRedispatch  = "redispatched"
	TriggerUnconfirmed = "delivery_unconfirmed"
	TriggerStalled     = "stalled_3d"
	TriggerMissed      = "schedule_missed"
	TriggerDispatched  = "dispatched"
	TriggerStartSoon   = "start_on_within_7d"
)

// SweepRules is board-redesign §5.1 and §6, in order.
func SweepRules() []SweepRule {
	return []SweepRule{
		{TriggerLanded, PlaceBoard, ruleLanded},
		{TriggerUnconfirmed, PlaceBoard, ruleUnconfirmed},
		{TriggerDelivered, PlaceBoard, ruleDelivered},
		{TriggerRedispatch, PlaceBoard, ruleRedispatched},
		{TriggerMissed, PlaceBoard, ruleScheduleMissed},
		{TriggerStalled, PlaceBoard, ruleStalled},
		{TriggerDispatched, PlaceBacklog, ruleDispatched},
		{TriggerStartSoon, PlaceBacklog, ruleStartSoon},
	}
}

// Next is the change the rules make to an item now, if any. It reads its
// arguments and nothing else.
func Next(rules []SweepRule, it Item, tasks []TaskFacts, p Policy, now time.Time) (Change, bool) {
	d := Derive(it, tasks, p, now)
	for _, r := range rules {
		if r.Place != it.Place {
			continue
		}
		if c, ok := r.Decide(it, d, tasks, p, now); ok {
			c.From = it.Place
			if c.To == "" {
				c.To = it.Place
			}
			return c, true
		}
	}
	return Change{}, false
}

// taskIDs is the ids of the tasks that satisfy keep, sorted: the evidence a
// change names.
func taskIDs(tasks []TaskFacts, keep func(TaskFacts) bool) []string {
	out := []string{}
	for _, t := range tasks {
		if keep(t) {
			out = append(out, t.Task)
		}
	}
	sort.Strings(out)
	return out
}

func open(it Item) bool { return it.State == ItemActive || it.State == ItemAwaitingClosure }

// ruleLanded: every delivery of the stay is landed in the broker's record and
// nothing runs — the item is done, by the fact alone (#3c).
func ruleLanded(it Item, d Derived, tasks []TaskFacts, _ Policy, _ time.Time) (Change, bool) {
	if !open(it) || d.State != ItemDone || !d.Landed {
		return Change{}, false
	}
	return Change{State: ItemDone, ClosedReason: ClosedLanded, Trigger: TriggerLanded, Actor: ActorBroker,
		Evidence: map[string]any{"landed_tasks": taskIDs(cycle(tasks, it.Since), func(t TaskFacts) bool {
			return OutcomeOf(t) == OutcomeLanded
		})}}, true
}

// ruleUnconfirmed: a delivery a person was asked about, unanswered for the
// wait — the board lets it go, marked as not confirmed, and claims nothing
// about landing (§5.1).
func ruleUnconfirmed(it Item, d Derived, _ []TaskFacts, _ Policy, now time.Time) (Change, bool) {
	if it.State != ItemAwaitingClosure || d.State != ItemAwaitingClosure || d.ClosureDueAt.IsZero() ||
		now.Before(d.ClosureDueAt) {
		return Change{}, false
	}
	return Change{State: ItemDone, ClosedReason: ClosedUnconfirmed, Trigger: TriggerUnconfirmed, Actor: ActorRule,
		Evidence: map[string]any{"asked_at": it.AskedAt.Unix(), "due_at": d.ClosureDueAt.Unix()}}, true
}

// ruleDelivered: the stay's tasks ended and at least one delivered without a
// landing — the item waits for a person to close it.
func ruleDelivered(it Item, d Derived, tasks []TaskFacts, _ Policy, _ time.Time) (Change, bool) {
	if it.State != ItemActive || d.State != ItemAwaitingClosure {
		return Change{}, false
	}
	return Change{State: ItemAwaitingClosure, Trigger: TriggerDelivered, Actor: ActorBroker,
		Evidence: map[string]any{"delivered_tasks": taskIDs(cycle(tasks, it.Since), func(t TaskFacts) bool {
			return OutcomeOf(t) == OutcomeDelivered
		})}}, true
}

// ruleRedispatched: new work arrived on an item that was waiting or closed.
func ruleRedispatched(it Item, d Derived, tasks []TaskFacts, _ Policy, _ time.Time) (Change, bool) {
	if !(it.State == ItemAwaitingClosure || it.State == ItemDone) || d.State != ItemActive {
		return Change{}, false
	}
	c := Change{State: ItemActive, Trigger: TriggerRedispatch, Actor: ActorBroker,
		Evidence: map[string]any{"live_tasks": taskIDs(tasks, func(t TaskFacts) bool { return !t.Ended })}}
	if it.State == ItemDone {
		c.Since = it.ClosedAt
		if root := newestRoot(tasks, it.ClosedAt); root != "" {
			c.Actor = "root:" + root
		}
	}
	return c, true
}

// ruleScheduleMissed: a start date came and went a day ago and nothing was
// dispatched — the commitment was not kept, and the board says so by handing
// the item back to the Backlog.
func ruleScheduleMissed(it Item, d Derived, tasks []TaskFacts, p Policy, now time.Time) (Change, bool) {
	if it.State != ItemActive || it.Commitment != CommitScheduled || it.StartOn == "" ||
		len(cycle(tasks, it.Since)) > 0 {
		return Change{}, false
	}
	start, err := time.ParseInLocation("2006-01-02", it.StartOn, p.loc())
	if err != nil || now.Before(start.AddDate(0, 0, 2)) {
		return Change{}, false
	}
	return Change{To: PlaceBacklog, State: ItemPlanned, Trigger: TriggerMissed, Actor: ActorRule,
		Evidence: map[string]any{"start_on": it.StartOn}}, true
}

// ruleStalled: active, nothing running, no delivery, and no fact for the
// stall window — the item goes back to the Backlog (#3b), and says so.
func ruleStalled(it Item, d Derived, _ []TaskFacts, _ Policy, now time.Time) (Change, bool) {
	if it.State != ItemActive || d.State != ItemActive || d.StallAt.IsZero() || now.Before(d.StallAt) {
		return Change{}, false
	}
	return Change{To: PlaceBacklog, State: ItemPlanned, Trigger: TriggerStalled, Actor: ActorRule,
		Evidence: map[string]any{"last_evidence_at": d.LastEvidenceAt.Unix(), "stall_at": d.StallAt.Unix()}}, true
}

// ruleDispatched: a dispatch named a planned item after it was placed in the
// Backlog — the root committed to it. Tasks from before it was placed there
// do not count: they are why it came back, not a reason to leave.
func ruleDispatched(it Item, _ Derived, tasks []TaskFacts, _ Policy, _ time.Time) (Change, bool) {
	if it.State != ItemPlanned {
		return Change{}, false
	}
	var first *TaskFacts
	for i := range tasks {
		t := tasks[i]
		if t.CreatedAt.Before(it.PlacedAt) {
			continue
		}
		if first == nil || t.CreatedAt.Before(first.CreatedAt) {
			first = &tasks[i]
		}
	}
	if first == nil {
		return Change{}, false
	}
	owner, actor := OwnerUser, ActorBroker
	if first.Owner != "" {
		owner, actor = first.Owner, "root:"+first.Owner
	}
	return Change{To: PlaceBoard, State: ItemActive, Owner: owner, Commitment: CommitDispatch,
		Since: first.CreatedAt, EvidenceAt: first.CreatedAt, Trigger: TriggerDispatched, Actor: actor,
		Evidence: map[string]any{"task": first.Task, "task_created_at": first.CreatedAt.Unix()}}, true
}

// ruleStartSoon: a planned start date entered the short window — planning
// became a commitment.
func ruleStartSoon(it Item, _ Derived, _ []TaskFacts, p Policy, now time.Time) (Change, bool) {
	if it.State != ItemPlanned || it.StartOn == "" {
		return Change{}, false
	}
	start, err := time.ParseInLocation("2006-01-02", it.StartOn, p.loc())
	if err != nil || start.Sub(now) > p.Short {
		return Change{}, false
	}
	// A date already past is not a commitment coming up: it is a plan
	// nobody kept, and it waits for a person rather than bouncing.
	y, m, d := now.In(p.loc()).Date()
	if start.Before(time.Date(y, m, d, 0, 0, 0, 0, p.loc())) {
		return Change{}, false
	}
	return Change{To: PlaceBoard, State: ItemActive, Owner: OwnerUser, Commitment: CommitScheduled,
		Since: now, EvidenceAt: now, Trigger: TriggerStartSoon, Actor: ActorRule,
		Evidence: map[string]any{"start_on": it.StartOn}}, true
}

func newestRoot(tasks []TaskFacts, after time.Time) string {
	var best *TaskFacts
	for i := range tasks {
		t := tasks[i]
		if t.CreatedAt.Before(after) || t.Owner == "" {
			continue
		}
		if best == nil || t.CreatedAt.After(best.CreatedAt) {
			best = &tasks[i]
		}
	}
	if best == nil {
		return ""
	}
	return best.Owner
}

// ——— A person's commands (D31) ———

// Op is a person's command. The list is closed: board-redesign §8's
// "instructions a person can give on the board", less the two that belong
// to T4 (answering a decision, and following a proposal).
type Op string

const (
	OpStart    Op = "start"    // Start: Backlog → board, handed to an owner
	OpTrack    Op = "track"    // Track this: an item nobody follows → board
	OpSchedule Op = "schedule" // Schedule: a start date; inside the short window it is on the board
	OpDefer    Op = "defer"    // Back to Backlog
	OpAccept   Op = "accept"   // Accept: close a delivery as accepted — never as landed
	OpRework   Op = "rework"   // Needs changes: a delivery goes back to work
	OpDrop     Op = "drop"     // Drop
	OpHandover Op = "handover" // Hand over
	OpUntrack  Op = "untrack"  // Don't track: off the board, left to the to-dos
	OpRank     Op = "rank"     // the Backlog's order
)

// Ops is every command, in the order they are documented.
var Ops = []Op{OpStart, OpTrack, OpSchedule, OpDefer, OpAccept, OpRework, OpDrop, OpHandover, OpUntrack, OpRank}

// landingWords are commands a person might reach for that would say what only
// the broker may say. Each is refused by name rather than as unknown, so the
// reason is on the screen: an item is landed by its landing record (D31).
var landingWords = map[string]bool{"land": true, "landed": true, "mark_landed": true, "set_landed": true,
	"complete": true, "close": true, "set_state": true, "transition": true}

// Command is one command as the rules read it.
type Command struct {
	Op      Op
	Actor   string
	Owner   string
	StartOn string
	Rank    *int64
}

// Refusal is a command the rules will not carry out, and why. Nothing was
// written.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func refuse(status int, code, format string, args ...any) *Refusal {
	return &Refusal{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// ParseOp reads a command's name.
func ParseOp(s string) (Op, error) {
	s = strings.TrimSpace(s)
	for _, op := range Ops {
		if string(op) == s {
			return op, nil
		}
	}
	if landingWords[strings.ToLower(s)] {
		return "", refuse(422, "landing_is_broker_fact",
			"Nothing on the board is marked landed by a command: an item is landed when the broker's landing record says every delivery landed. To close a delivery that has not landed, accept it; it stays not landed.")
	}
	return "", refuse(400, "unknown_operation", "%q is not a board command.", s)
}

// ValidStartOn reads a start date.
func ValidStartOn(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Decide is what a person's command does to an item, as the facts stand, or
// the refusal. It is decided again inside the write, from the rows as they
// are then (DG-6).
func Decide(it Item, tasks []TaskFacts, cmd Command, p Policy, now time.Time) (Change, error) {
	d := Derive(it, tasks, p, now)
	evidence := map[string]any{"op": string(cmd.Op)}
	c := Change{From: it.Place, To: it.Place, Trigger: string(cmd.Op), Actor: cmd.Actor, Evidence: evidence}
	onBoardOpen := it.Place == PlaceBoard && open(it)
	switch cmd.Op {
	case OpStart, OpTrack:
		from := PlaceBacklog
		if cmd.Op == OpTrack {
			from = PlaceTodo
		}
		if it.Place == PlaceBoard {
			return Change{}, refuse(409, "already_on_board", "This item is already on the board.")
		}
		if it.Place != from || (from == PlaceBacklog && it.State != ItemPlanned) {
			return Change{}, refuse(409, "wrong_place", "%s takes an item from the %s; this one is in the %s (%s).",
				cmd.Op, from, it.Place, it.State)
		}
		owner := cmd.Owner
		if owner == "" {
			owner = OwnerUser
		}
		c.To, c.State, c.Owner, c.Commitment = PlaceBoard, ItemActive, owner, CommitAssigned
		c.Since, c.EvidenceAt = now, now
		evidence["owner"] = owner
	case OpSchedule:
		if !ValidStartOn(cmd.StartOn) {
			return Change{}, refuse(400, "invalid_start_on", "start_on is a date, YYYY-MM-DD.")
		}
		if it.Place != PlaceBacklog || it.State != ItemPlanned {
			return Change{}, refuse(409, "wrong_place", "Only a planned Backlog item is scheduled; this one is in the %s (%s).",
				it.Place, it.State)
		}
		start := cmd.StartOn
		c.StartOn, c.State, c.Reviewed = &start, ItemPlanned, true
		evidence["start_on"] = start
		if at, _ := time.ParseInLocation("2006-01-02", start, p.loc()); at.Sub(now) <= p.Short {
			owner := cmd.Owner
			if owner == "" {
				owner = OwnerUser
			}
			c.To, c.State, c.Owner, c.Commitment = PlaceBoard, ItemActive, owner, CommitScheduled
			c.Since, c.EvidenceAt = now, now
		}
	case OpDefer:
		if !onBoardOpen {
			return Change{}, refuse(409, "wrong_place", "Only an open board item goes back to the Backlog; this one is in the %s (%s).",
				it.Place, it.State)
		}
		// Deferring does not stop anything that runs; the move says what was
		// running when the person decided.
		evidence["live_tasks"] = d.Tasks.Live
		c.To, c.State = PlaceBacklog, ItemPlanned
	case OpAccept:
		if it.Place != PlaceBoard {
			return Change{}, refuse(409, "wrong_place", "Only a board item is accepted; this one is in the %s.", it.Place)
		}
		if d.State != ItemAwaitingClosure {
			if d.State.Closed() {
				return Change{}, refuse(409, "already_closed", "This item is already closed (%s).", d.Reason)
			}
			return Change{}, refuse(409, "not_awaiting_closure",
				"There is nothing to accept yet: this item is %s (%s). A delivery is accepted once every task of it has ended.",
				d.State, d.Reason)
		}
		c.State, c.ClosedReason = ItemDone, ClosedAccepted
		evidence["delivered"] = d.Tasks.Delivered
		evidence["landed"] = false
	case OpRework:
		if it.Place != PlaceBoard || d.State != ItemAwaitingClosure {
			return Change{}, refuse(409, "not_awaiting_closure", "Only a delivery waiting to be closed goes back to work.")
		}
		c.State, c.Since, c.EvidenceAt = ItemActive, now, now
	case OpDrop:
		switch {
		case onBoardOpen:
			c.State, c.ClosedReason = ItemDropped, ClosedDropped
		case it.Place == PlaceBacklog && it.State == ItemPlanned:
			c.State = ItemDropped
		case it.State.Closed():
			return Change{}, refuse(409, "already_closed", "This item is already closed.")
		default:
			return Change{}, refuse(409, "wrong_place", "Only a board or Backlog item is dropped; this one is in the %s.", it.Place)
		}
	case OpHandover:
		if !onBoardOpen {
			return Change{}, refuse(409, "wrong_place", "Only an open board item is handed over.")
		}
		if cmd.Owner == "" {
			return Change{}, refuse(400, "owner_required", "A handover names the new owner.")
		}
		if cmd.Owner == it.Owner {
			return Change{}, refuse(409, "same_owner", "This item is already held by that owner.")
		}
		c.State, c.Owner, c.EvidenceAt = it.State, cmd.Owner, now
		evidence["from_owner"], evidence["owner"] = it.Owner, cmd.Owner
	case OpUntrack:
		if !onBoardOpen {
			return Change{}, refuse(409, "wrong_place", "Only an open board item stops being followed.")
		}
		c.To, c.State = PlaceTodo, ""
	case OpRank:
		if it.Place != PlaceBacklog || it.State != ItemPlanned {
			return Change{}, refuse(409, "wrong_place", "Only a planned Backlog item is ranked.")
		}
		if cmd.Rank == nil || *cmd.Rank < 0 {
			return Change{}, refuse(400, "invalid_rank", "rank is a whole number, zero for unranked.")
		}
		rank := *cmd.Rank
		c.State, c.Rank, c.Reviewed = ItemPlanned, &rank, true
		evidence["rank"] = rank
	default:
		return Change{}, refuse(400, "unknown_operation", "%q is not a board command.", cmd.Op)
	}
	return c, nil
}

// ——— Reading the board ———

// Section is one of the board's regions (board-redesign §3.3), top to bottom.
type Section string

const (
	SectionDecide    Section = "decide"    // Waiting on you: deliveries waiting to be closed
	SectionActive    Section = "active"    // In progress
	SectionScheduled Section = "scheduled" // Scheduled this week: on the board by date, nothing dispatched yet
	SectionDone      Section = "done"      // Recently done: closed inside the short window
)

// Sections is their order.
var Sections = []Section{SectionDecide, SectionActive, SectionScheduled, SectionDone}

// SectionOf places a board item by what the facts say now. The second answer
// is false for a closed item older than the short window: it is no longer on
// the board's page, and is read by its id.
func SectionOf(it Item, d Derived, p Policy, now time.Time) (Section, bool) {
	switch d.State {
	case ItemAwaitingClosure:
		return SectionDecide, true
	case ItemActive:
		if it.Commitment == CommitScheduled && d.Tasks.Total == 0 {
			return SectionScheduled, true
		}
		return SectionActive, true
	}
	closed := it.ClosedAt
	if closed.IsZero() {
		// Derived closed and not recorded yet: it closed now, as far as the
		// page is concerned.
		closed = now
	}
	return SectionDone, now.Sub(closed) <= p.Short
}
