// Package work is the board's second design: three structures, each with one
// reader (docs/board-redesign.md, adopted by docs/design-decisions.md D30).
//
//   - the board (看板): what a person needs to know now;
//   - the session to-do list (Session 待辦): what a session must not forget,
//     made, tracked and closed by facts alone;
//   - the Backlog: planned, with no commitment to start.
//
// This file is step 1 of board-redesign §9 and nothing more: a read-only
// projection of the old app's cards onto those three tracks, by the ordered
// rules of §7.1, so that a person can check the rules against their own data
// before anything is written. It stores nothing, and no function here can: the
// package imports no I/O. The legacy cards come in as [Card] values that an
// adapter filled from files it only read.
package work

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RulesVersion names the rule set a projection was computed with. A migration
// (board-redesign §7.4) records it next to every move, so a later change of
// rules can tell which rows it may move again.
const RulesVersion = "legacy-tracks/1"

// DefaultStall is how long a started card may go without a fact before it
// stops counting as happening now: board-redesign §5.5 and §10 #2, which
// design-decisions U3 keeps as the default until the person says otherwise.
const DefaultStall = 72 * time.Hour

// Track is one of the three structures.
type Track string

const (
	TrackTodo    Track = "todo"
	TrackBoard   Track = "board"
	TrackBacklog Track = "backlog"
)

// TrackOrder is their fixed order: the order counts and rows are listed in.
var TrackOrder = []Track{TrackTodo, TrackBoard, TrackBacklog}

// Bucket is where inside a track a card lands. The names are board-redesign
// §7.2's rows (S.done … K.stalled), spelt with the tracks' API names.
type Bucket string

const (
	TodoDone            Bucket = "todo.done"             // S.done
	TodoAutoclose       Bucket = "todo.autoclose"        // S.autoclose
	TodoLive            Bucket = "todo.live"             // S.live
	BoardNow            Bucket = "board.now"             // B.now
	BoardClosure        Bucket = "board.closure"         // B.closure
	BoardDone           Bucket = "board.done"            // B.done
	BacklogNeverStarted Bucket = "backlog.never_started" // K.never_started
	BacklogStalled      Bucket = "backlog.stalled"       // K.stalled
)

// BucketOrder is their fixed order, the order of the §7.2 table.
var BucketOrder = []Bucket{TodoDone, TodoAutoclose, TodoLive, BoardNow, BoardClosure, BoardDone,
	BacklogNeverStarted, BacklogStalled}

// Track is the structure a bucket belongs to.
func (b Bucket) Track() Track {
	track, _, _ := strings.Cut(string(b), ".")
	return Track(track)
}

// Card is what the projection knows about one card of the old app's board.
//
// Every field is a recorded fact copied out of that app's store. Nothing here
// is a conclusion: the rules below draw every conclusion, so that each one has
// a fixture of its own.
type Card struct {
	ID        string
	Key       string
	ProjectID string
	Title     string
	Type      string
	// Audience is who the old app says the card is for: its catalog
	// disposition, or, without one, what the inferred source key implies.
	// "human" is a person; anything else is a session's or an archive's.
	Audience string
	// Progress is the old card view's own projection of the evidence
	// (internal/adapters/board ProgressOf), taken as given.
	Progress    Progress
	Obligations []Obligation
	// Attempts are the card's task links: which source recorded each and the
	// attempt state that source last reported.
	Attempts []Attempt
	Spans    []Span
	// Deliveries and Evidence count a session's delivery reports and the
	// recorded evidence rows.
	Deliveries int
	Evidence   int
	// Checklist is the status of each acceptance row.
	Checklist []string
	// CreatedAt and UpdatedAt are Unix seconds, as the old store writes them.
	// UpdatedAt moves whenever that app's reconciliation touches the card, so
	// it is the idle clock only when there is no history to read.
	CreatedAt float64
	UpdatedAt float64
	// History is the card's event log. Nil means the old app kept none for it.
	History *History
}

// Progress is the part of the old progress projection the rules read.
type Progress struct {
	State  string
	Group  string
	Active bool
}

// Obligation is something the card says is still owed, and to whom.
type Obligation struct {
	Resolved  bool
	ActorKind string
}

// Attempt is one task link.
type Attempt struct {
	Source string
	State  string
}

// Span is a declared interval of work.
type Span struct {
	SessionID string
	Open      bool
}

// History is a card's event log as far as it could be read.
type History struct {
	// Readable is false when the log exists and could not be read whole. The
	// idle clock then falls back to UpdatedAt and the row says so.
	Readable bool
	Events   []Event
}

// Event is one line of the log.
type Event struct {
	Kind string
	At   float64
}

// Liveness is what a reading of the machine says about one session.
type Liveness string

const (
	Live    Liveness = "live"
	Gone    Liveness = "gone"
	Unknown Liveness = "unknown"
)

// Presence is one reading of which sessions are running.
//
// A session missing from the reading is gone only when the reading is
// complete. Otherwise it is unknown, and unknown never proves that a session
// is dead (docs/design-guidelines.md DG-7).
type Presence struct {
	Complete bool
	// Sessions holds every id the reading saw, terminal and conversation ids
	// alike, lower-cased.
	Sessions map[string]bool
}

// Of answers for one session id.
func (p Presence) Of(id string) Liveness {
	if p.Sessions[strings.ToLower(id)] {
		return Live
	}
	if p.Complete {
		return Gone
	}
	return Unknown
}

// The vocabularies the rules read. Each is the old app's own.
var (
	// terminalGroups are the progress groups that mean the work is over.
	terminalGroups = map[string]bool{"completed": true, "canceled": true}
	// activeAttemptStates are a broker attempt that is still running.
	activeAttemptStates = map[string]bool{"queued": true, "spawning": true, "briefed": true}
	// checkedStatuses are a checklist row that somebody ticked.
	checkedStatuses = map[string]bool{"completed": true, "done": true, "passed": true}
	// closureStates are a started, quiet card that is waiting for somebody to
	// close it rather than for work to begin again (§7.1 B.closure).
	closureStates = map[string]bool{"delivered": true, "verified": true, "blocked": true,
		"correction": true, "review_testing": true}
	// machineEventKinds are the old store recomputing its own projection:
	// none of them is a new fact, so none of them moves the idle clock
	// (board-redesign §1.1, "非機器的更新").
	machineEventKinds = map[string]bool{"item_created": true, "automatic_state_reconciled": true,
		"catalog_reconciled": true, "task_reattributed": true, "session_relation_confirmed": true}
)

// Idle-clock bases, as they appear in Facts.IdleFrom.
const (
	IdleFromHistory    = "history"            // the last fact in the card's log
	IdleFromCreated    = "created"            // the log holds no fact after the card was made
	IdleFromUpdatedAt  = "updated_at"         // the old app kept no log for this card
	IdleFromUnreadable = "unreadable_history" // there is a log and it could not be read
)

// Facts are the conclusions the rules are applied to, published with every
// row so a person can see why it landed where it did.
type Facts struct {
	Audience      string `json:"audience"`
	Type          string `json:"type"`
	Progress      string `json:"progress"`
	ProgressGroup string `json:"progressGroup"`
	// Active is the progress projection's word; Ghost says the only thing
	// holding it up is a declared span of a session that is gone (§1.4).
	Active bool `json:"active"`
	Ghost  bool `json:"ghost"`
	// Presence is what the reading said about the sessions of an active
	// card's open spans: live, gone or unknown. Empty when nothing was asked.
	Presence      Liveness `json:"presence,omitempty"`
	Started       bool     `json:"started"`
	UserDecisions int      `json:"userDecisions"`
	IdleSeconds   float64  `json:"idleSeconds"`
	IdleFrom      string   `json:"idleFrom"`
}

// ForPerson is §7.1's first test: a card for a person, and not a record of
// sessions coordinating with each other.
func (f Facts) ForPerson() bool { return f.Audience == "human" && f.Type != "coordination" }

// Terminal says the work is over.
func (f Facts) Terminal() bool { return terminalGroups[f.ProgressGroup] }

// ActiveNow says the work is being done, and not only declared.
func (f Facts) ActiveNow() bool { return f.Active && !f.Ghost }

// FactsOf draws the conclusions for one card.
func FactsOf(c Card, presence Presence, now float64) Facts {
	f := Facts{Audience: c.Audience, Type: c.Type, Progress: c.Progress.State,
		ProgressGroup: c.Progress.Group, Active: c.Progress.Active}
	for _, o := range c.Obligations {
		if !o.Resolved && o.ActorKind == "user" {
			f.UserDecisions++
		}
	}
	f.Ghost, f.Presence = ghost(c, presence)
	f.Started = started(c)
	last, from := lastFact(c)
	f.IdleSeconds, f.IdleFrom = now-last, from
	return f
}

// ghost is §1.4's "進行中是假的": the card is active only because a session
// declared a span, and every session that declared an open one is gone. A
// running broker attempt is work whatever the spans say. A session that may
// or may not be running is not a dead one.
func ghost(c Card, presence Presence) (bool, Liveness) {
	if !c.Progress.Active {
		return false, ""
	}
	for _, a := range c.Attempts {
		if a.Source == "broker" && activeAttemptStates[a.State] {
			return false, ""
		}
	}
	seen := map[Liveness]bool{}
	for _, s := range c.Spans {
		if s.Open {
			seen[presence.Of(s.SessionID)] = true
		}
	}
	switch {
	case len(seen) == 0:
		return false, ""
	case seen[Live]:
		return false, Live
	case seen[Unknown]:
		return false, Unknown
	}
	return true, Gone
}

// started is §1.1's "開始過": recorded work, not a declaration. A declared
// span does not count, because the old workflow's begin template declares the
// output phase the moment an item is registered (§1.7-3).
func started(c Card) bool {
	for _, a := range c.Attempts {
		if a.Source == "broker" {
			return true
		}
	}
	if c.Deliveries > 0 || c.Evidence > 0 {
		return true
	}
	for _, status := range c.Checklist {
		if checkedStatuses[status] {
			return true
		}
	}
	return false
}

// lastFact is when the card last received a fact that was not the old store
// recomputing itself, and what that answer rests on.
func lastFact(c Card) (float64, string) {
	switch {
	case c.History == nil, c.History.Readable && len(c.History.Events) == 0:
		// An empty log says as little as a missing one.
		return c.UpdatedAt, IdleFromUpdatedAt
	case !c.History.Readable:
		return c.UpdatedAt, IdleFromUnreadable
	}
	created, haveCreated := 0.0, false
	last, haveLast := 0.0, false
	for _, e := range c.History.Events {
		if e.Kind == "item_created" && (!haveCreated || e.At < created) {
			created, haveCreated = e.At, true
		}
		if !machineEventKinds[e.Kind] && (!haveLast || e.At > last) {
			last, haveLast = e.At, true
		}
	}
	switch {
	case haveLast:
		return last, IdleFromHistory
	case haveCreated:
		return created, IdleFromCreated
	}
	return c.CreatedAt, IdleFromCreated
}

// rule is one line of §7.1. The first rule that matches decides.
type rule struct {
	code   string
	bucket Bucket
	match  func(f Facts, stall float64) bool
}

// rules is §7.1 in its order. A card for no person is the session's; one that
// is over is done; one that is waiting on a person or visibly being done is
// now; one that never started is Backlog; one that started and moved within
// the stall window is now; one that stopped after delivering waits to be
// closed; the rest stalled and go back to the Backlog.
var rules = []rule{
	{"not_for_person_terminal", TodoDone, func(f Facts, _ float64) bool { return !f.ForPerson() && f.Terminal() }},
	{"not_for_person_active", TodoLive, func(f Facts, _ float64) bool { return !f.ForPerson() && f.ActiveNow() }},
	{"not_for_person", TodoAutoclose, func(f Facts, _ float64) bool { return !f.ForPerson() }},
	{"terminal", BoardDone, func(f Facts, _ float64) bool { return f.Terminal() }},
	{"user_decision_open", BoardNow, func(f Facts, _ float64) bool { return f.UserDecisions > 0 }},
	{"active", BoardNow, func(f Facts, _ float64) bool { return f.ActiveNow() }},
	{"never_started", BacklogNeverStarted, func(f Facts, _ float64) bool { return !f.Started }},
	{"idle_within_stall", BoardNow, func(f Facts, stall float64) bool { return f.IdleSeconds <= stall }},
	{"idle_past_stall_delivered", BoardClosure, func(f Facts, _ float64) bool { return closureStates[f.Progress] }},
	{"idle_past_stall", BacklogStalled, func(Facts, float64) bool { return true }},
}

// classify applies a rule list; the empty bucket means none matched.
func classify(list []rule, f Facts, stall float64) (Bucket, string) {
	for _, r := range list {
		if r.match(f, stall) {
			return r.bucket, r.code
		}
	}
	return "", ""
}

// Row is one card's place.
type Row struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Track     Track  `json:"track"`
	Bucket    Bucket `json:"bucket"`
	// Rule is the code of the §7.1 line that decided.
	Rule  string `json:"rule"`
	Facts Facts  `json:"facts"`

	createdAt float64
}

// Counts are the three tracks' sizes.
type Counts struct {
	Cards   int `json:"cards"`
	Todo    int `json:"todo"`
	Board   int `json:"board"`
	Backlog int `json:"backlog"`
}

// BucketCount is one line of the §7.2 table: how many, and what the progress
// projection says about them.
type BucketCount struct {
	Bucket   Bucket         `json:"bucket"`
	Track    Track          `json:"track"`
	Count    int            `json:"count"`
	Progress map[string]int `json:"progress"`
}

// Projection is the whole answer for a set of cards.
type Projection struct {
	Rules        string        `json:"rules"`
	EvaluatedAt  float64       `json:"evaluatedAt"`
	StallSeconds float64       `json:"stallSeconds"`
	Counts       Counts        `json:"counts"`
	Buckets      []BucketCount `json:"buckets"`
	// Rows are in bucket order, newest card first within a bucket.
	Rows []Row `json:"rows"`
}

// Project places every card. It reads its arguments and nothing else.
func Project(cards []Card, presence Presence, now time.Time, stall time.Duration) Projection {
	at, window := unixSeconds(now), stall.Seconds()
	p := Projection{Rules: RulesVersion, EvaluatedAt: at, StallSeconds: window,
		Rows: make([]Row, 0, len(cards))}
	counts := map[Bucket]*BucketCount{}
	for _, b := range BucketOrder {
		counts[b] = &BucketCount{Bucket: b, Track: b.Track(), Progress: map[string]int{}}
	}
	for _, c := range cards {
		f := FactsOf(c, presence, at)
		bucket, code := classify(rules, f, window)
		p.Rows = append(p.Rows, Row{ID: c.ID, Key: c.Key, ProjectID: c.ProjectID, Title: c.Title,
			Track: bucket.Track(), Bucket: bucket, Rule: code, Facts: f, createdAt: c.CreatedAt})
		counts[bucket].Count++
		counts[bucket].Progress[f.Progress]++
		p.Counts.Cards++
		switch bucket.Track() {
		case TrackTodo:
			p.Counts.Todo++
		case TrackBoard:
			p.Counts.Board++
		case TrackBacklog:
			p.Counts.Backlog++
		}
	}
	for _, b := range BucketOrder {
		p.Buckets = append(p.Buckets, *counts[b])
	}
	sort.SliceStable(p.Rows, func(i, j int) bool { return p.Rows[i].before(p.Rows[j]) })
	return p
}

func unixSeconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

// bucketIndex is a bucket's place in BucketOrder.
func bucketIndex(b Bucket) int {
	for i, x := range BucketOrder {
		if x == b {
			return i
		}
	}
	return len(BucketOrder)
}

// before is the row order: bucket, then newest first, then id.
func (r Row) before(o Row) bool {
	if a, b := bucketIndex(r.Bucket), bucketIndex(o.Bucket); a != b {
		return a < b
	}
	if r.createdAt != o.createdAt {
		return r.createdAt > o.createdAt
	}
	return r.ID < o.ID
}

// cursor is the position just after a row, in the row order. It is made of
// characters a query string carries as they are.
func (r Row) cursor() string {
	return fmt.Sprintf("%d:%s:%s", bucketIndex(r.Bucket),
		strconv.FormatFloat(r.createdAt, 'f', -1, 64), r.ID)
}

// ErrCursor is a cursor this projection did not write.
var ErrCursor = fmt.Errorf("not a tracks cursor")

func parseCursor(s string) (Row, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return Row{}, ErrCursor
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil || index < 0 || index >= len(BucketOrder) {
		return Row{}, ErrCursor
	}
	created, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || math.IsNaN(created) || math.IsInf(created, 0) {
		return Row{}, ErrCursor
	}
	return Row{Bucket: BucketOrder[index], createdAt: created, ID: parts[2]}, nil
}

// Page is at most size rows of one track (or of every track when track is
// empty) that come after the cursor, and the cursor for the next page, empty
// when there is none. A cursor is a position, not an offset: a card that
// moved between two reads is listed where it now stands.
func (p Projection) Page(track Track, cursor string, size int) ([]Row, string, error) {
	var after *Row
	if cursor != "" {
		row, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		after = &row
	}
	out := []Row{}
	for _, r := range p.Rows {
		if track != "" && r.Track != track {
			continue
		}
		if after != nil && !after.before(r) {
			continue
		}
		if len(out) == size {
			return out, out[len(out)-1].cursor(), nil
		}
		out = append(out, r)
	}
	return out, "", nil
}

// ForPeople is the §7.2 view of the cards a person reads: every card on the
// board and in the Backlog.
func (p Projection) ForPeople() int { return p.Counts.Board + p.Counts.Backlog }

// Sources say what the projection was computed from, so that a partial or
// stale reading is never mistaken for a board with fewer cards.
type Sources struct {
	Board    BoardSource    `json:"board"`
	History  HistorySource  `json:"history"`
	Presence PresenceSource `json:"presence"`
}

// BoardSource is the old app's board file.
type BoardSource struct {
	// Status is ok, stale (an earlier good reading, because the file cannot
	// be read now) or absent (there is no such file on this machine).
	Status   string  `json:"status"`
	Revision int64   `json:"revision"`
	Cards    int     `json:"cards"`
	ReadAt   float64 `json:"readAt"`
}

// HistorySource is the old app's per-card event logs.
type HistorySource struct {
	// Status is ok or absent (no log directory at all).
	Status     string `json:"status"`
	Files      int    `json:"files"`
	Unreadable int    `json:"unreadable"`
}

// PresenceSource is the reading of running sessions.
type PresenceSource struct {
	// From is inventory (this daemon's reading of the machine) or file (a
	// saved session list given on the command line).
	From     string `json:"from"`
	Complete bool   `json:"complete"`
	Sessions int    `json:"sessions"`
	// Assumed is true when a person declared an incomplete reading complete.
	Assumed bool `json:"assumed,omitempty"`
}

// Tracks is the answer to `GET /v1/board/tracks` and `clawdline board tracks
// --json`: a projection, what it was computed from, and one page of its rows.
type Tracks struct {
	Projection
	Project    *string `json:"project"`
	Sources    Sources `json:"sources"`
	NextCursor *string `json:"nextCursor"`
	PageSize   int     `json:"pageSize"`
}
