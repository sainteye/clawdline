// Package orchestrator is this daemon's broker: the loop from a root asking
// for work to a child reporting that the work is done.
//
// It is the one part of the Swift app that is not a screen. Every agent session
// on this machine is dispatched, briefed, watched, collected and acknowledged
// through `/v1/orchestrator/*`, and until that lives here the Swift app cannot
// be retired however complete the console is.
//
// Two rules shape everything below, and both are the Swift app's own hard-won
// ones:
//
//   - **A task this daemon dispatches exists entirely in this daemon's store.**
//     The read-only window onto `~/.config/clawdline` (plan.md §4) is for
//     drawing the Swift app's tasks, never for writing one of ours. Two brokers
//     over one record is the failure that window exists to avoid.
//   - **Recording precedes effect.** The intent is durable before a terminal is
//     opened, so a crash in between leaves a task that is known and unstarted,
//     rather than one that is running and unrecorded.
package orchestrator

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
)

// Protocol is the version written into every task.json and read back out of
// every result.json.
const Protocol = 1

// State is where a dispatched task is. Eight values, five terminal, spelled as
// the Swift app spells them because a child, a console and a person all read
// this word (OrchestratorTaskShape.swift).
type State string

const (
	StateQueued      State = "queued"
	StateSpawning    State = "spawning"
	StateBriefed     State = "briefed"
	StateSuccess     State = "success"
	StateFailure     State = "failure"
	StateTimeout     State = "timeout"
	StateCancelled   State = "cancelled"
	StateSpawnFailed State = "spawn_failed"
)

// Terminal reports whether a state is one nothing moves out of.
func (s State) Terminal() bool {
	switch s {
	case StateSuccess, StateFailure, StateTimeout, StateCancelled, StateSpawnFailed:
		return true
	}
	return false
}

// LandingState is the rung a finished task's work has reached. Delivered is not
// reviewed and reviewed is not landed: this is the second machine, deliberately
// apart from State, because a terminal child state is not the completion state
// of a code-producing graph.
type LandingState string

const (
	LandingPending       LandingState = "pending"
	LandingLanded        LandingState = "landed"
	LandingAbandoned     LandingState = "abandoned"
	LandingNothingToLand LandingState = "nothing_to_land"
)

// LandingSettlement is what a task's delivery branch held the moment the task
// ended, as git answered then.
//
// The broker already asked that question at that moment — it reads the branch
// head before every settling write (landing.go, D08) — and until now it used
// the answer to update Worktree.Head and threw the rest away. What it threw
// away is the only evidence that is cheap while it is still actionable: the
// checkout is on disk, the changes are in it, and the person who could commit
// them is reading the line this settlement writes. Measured on 2026-09-20:
// sixteen deliveries that had been merged into master were each refused
// `unverified_landing / nothing_delivered` four hours later, because every
// brief that day had told the child not to commit and by then the evidence
// lived only in directories that had been removed.
//
// Unreadable is a third answer and not a kind of empty: a branch git could not
// count has not said it carries nothing (DG-7).
type LandingSettlement string

const (
	SettlementEmpty      LandingSettlement = "branch_empty"
	SettlementCarried    LandingSettlement = "branch_carries_commits"
	SettlementUnreadable LandingSettlement = "branch_unreadable"
)

// Isolation is where the child's checkout is.
const (
	IsolationNone     = "none"
	IsolationWorktree = "worktree"
)

// Lease scopes (docs/design-decisions.md D21). A task declares one list of
// paths it will write, once, and that list is never cleared. Whether the list
// also reserves those paths against other roots is a separate fact, the scope:
// a task writing the shared checkout holds them as a lease; an isolated one
// writes its own checkout at a different spelling and reserves nothing here.
// The first version of this broker answered both questions by emptying the
// list, so a delivery that wrote twenty-six files read exactly like a review
// that wrote none.
const (
	LeaseShared   = "shared"
	LeaseWorktree = "worktree"
)

// Worktree is the checkout a task with `isolation: "worktree"` was given.
//
// `base` is recorded at creation and never recomputed. It is what the branch
// started from, and a reader asking "did this branch produce anything" compares
// head against it; recomputing base later would answer that question with the
// wrong past.
//
// `head` is the delivery: the branch's own commit, read from git when the task
// settles and again when its landing is proved (D17). At creation it is the
// base, because that is where the branch was made. The first version of this
// broker wrote it once, there, and never again, so every reader of it — the
// inventory, the landing — saw the base and called it the delivery (G17).
// Empty means the branch could not be read when it was last asked: unknown,
// which is not the base and not "nothing delivered".
type Worktree struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
	Head       string `json:"head,omitempty"`
}

// RootRef is the session this task was dispatched by.
type RootRef struct {
	SessionID  string `json:"session_id"`
	Assistant  string `json:"assistant"`
	ProjectDir string `json:"project_dir"`
	Label      string `json:"label"`
	PollOnly   bool   `json:"poll_only"`
}

// Landing is what became of the delivery.
//
// It is the one record of whether a task's work reached its target (D01).
// Anything else that says "landed" reads it rather than keeping its own copy:
// the inventory does (W3); a session's count of open obligations and the board
// are to (W4, T3).
type Landing struct {
	State LandingState `json:"state"`
	// Target is the branch the root named, the first time it named one
	// (D19). Until then it is empty, and empty is "not decided", which no
	// reader may fill in with the repository's HEAD.
	Target string    `json:"target,omitempty"`
	Commit string    `json:"commit,omitempty"`
	Repo   string    `json:"repo,omitempty"`
	At     time.Time `json:"at,omitempty"`
	Note   string    `json:"note,omitempty"`

	// What a `landed` was proved against (D17), kept because none of it can
	// be read back later: the target moves on, and the branch may go.
	// TargetCommit is what the target branch named when the proof ran;
	// DeliveryHead is the isolated branch's head the proof showed the commit
	// carries; Base is the commit the dispatch started from, which the landed
	// commit was shown not to be under.
	TargetCommit string `json:"target_commit,omitempty"`
	DeliveryHead string `json:"delivery_head,omitempty"`
	Base         string `json:"base,omitempty"`

	// Settlement is what the delivery branch held at the moment the task
	// ended, asked of git then (dispatch.go) because it cannot be asked
	// later. Empty means nobody asked: a task with no branch of its own, or
	// a record written before this was kept.
	Settlement LandingSettlement `json:"settlement,omitempty"`

	// CorrectedFrom is the settled landing this one replaced (D18): a
	// resend that disagrees with a settled record is a write, it passes the
	// gate the first one passed, and what it replaced is kept here rather
	// than overwritten. One level: the one before that is in the
	// `landing.corrected` event that recorded it, not nested here without end.
	CorrectedFrom *Landing `json:"corrected_from,omitempty"`
}

// sameAs is whether two landings say the same thing, ignoring when each was
// said and what each replaced. It is what tells a resend from a correction.
func (l Landing) sameAs(o Landing) bool {
	return l.State == o.State && l.Target == o.Target && l.Commit == o.Commit && l.Note == o.Note
}

// replaced is the landing as a correction keeps it: itself, without the one
// it had replaced in turn.
func (l Landing) replaced() *Landing {
	l.CorrectedFrom = nil
	return &l
}

// Obligation is what a `pending` landing means now, derived from whether its
// owner is still here (broker-design #35, O1). It is never stored: it is a
// reading of the machine, and the next reading may say otherwise (D04).
//
// `pending` alone used to be the word for both "somebody is working on it"
// and "the session that owed it is gone", and a line sat fourteen hours behind
// one because the two looked alike. Unknown is the third answer and not a
// kind of either: a machine this broker cannot fully read has not said the
// owner is gone (DG-7).
type Obligation string

const (
	ObligationLive     Obligation = "pending_live"
	ObligationOrphaned Obligation = "pending_orphaned"
	ObligationUnknown  Obligation = "pending_unknown"
)

// NoticeState is where the completion notice to the root has got to.
type NoticeState string

const (
	NoticePending      NoticeState = "pending"
	NoticeDelivered    NoticeState = "delivered"
	NoticeAcknowledged NoticeState = "acknowledged"
	NoticeDeadLetter   NoticeState = "dead_letter"
)

// NoticeError is why one delivery attempt did not happen, or why the last pass
// held the notice back instead of typing it. The Swift app's vocabulary is
// root_missing, identity_stale, conversation_ambiguous, root_choosing,
// terminal_timeout, transport_failed and acknowledgement_timeout; this broker
// adds terminal_busy and composer_occupied, the two holds (notice.go) it keeps
// on the record rather than in memory alone, so "is there anything I have not
// been told" can be answered from `GET /v1/orchestrator/completions`.
type NoticeError struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Notice is the durable envelope for "tell the root this finished".
//
// It is a ledger rather than a flag because the thing it records is a promise
// to somebody who is not here: a line typed into a terminal is not evidence
// anybody read it, so a successful transport reschedules rather than stops, and
// only an ACK ends the sequence. Eight attempts, then dead letter — a notice
// that keeps retrying forever is a notice nobody ever has to answer.
type Notice struct {
	ID             string       `json:"notice_id"`
	State          NoticeState  `json:"state"`
	Attempts       int          `json:"attempts"`
	CreatedAt      time.Time    `json:"created_at"`
	LastAttemptAt  time.Time    `json:"last_attempt_at,omitempty"`
	NextRetryAt    time.Time    `json:"next_retry_at,omitempty"`
	DeliveredAt    time.Time    `json:"transport_delivered_at,omitempty"`
	ObservedAt     time.Time    `json:"observed_at,omitempty"`
	AcknowledgedAt time.Time    `json:"acknowledged_at,omitempty"`
	DeadLetterAt   time.Time    `json:"dead_letter_at,omitempty"`
	LastError      *NoticeError `json:"last_error,omitempty"`
	Recipient      string       `json:"recipient,omitempty"`
}

// AttemptLimit and the two ladders one notice climbs.
//
// **A notice has two waits in it, and they are not the same wait.** One is
// "nothing was typed, try again" — a root that is not on this machine yet, a
// terminal that refused the bytes. The other is "the line is on the root's
// screen, and nobody has said they read it". The Swift app's 5, 10, 20, 40, 80,
// 160, 300-second ladder was measured for the first and used for both, so a
// root that was merely busy integrating its child was told the same thing again
// five seconds later, and again ten seconds after that: the person watching saw
// one "task finished" four and five times inside a minute and asked whether the
// work had been dispatched twice.
//
// So the delivered case climbs its own ladder, with a first rung longer than a
// turn: 2, 4, 8, 16, 30, 30 … minutes. What it is waiting for is a person or an
// agent finishing what it is doing and answering, and nothing about that is
// helped by asking again inside a minute. Eight attempts in all either way, and
// then dead letter — the whole budget is about two and a half hours of
// unanswered delivery instead of ten minutes of shouting.
const (
	AttemptLimit = 8
	retryBase    = 5 * time.Second
	retryCeiling = 300 * time.Second
	ackWaitBase  = 2 * time.Minute
	// maxAckWait is the longest this broker will wait between two typings of
	// one notice, and so the top rung of the delivered ladder.
	maxAckWait     = 30 * time.Minute
	NoticeProtocol = "clawdline.notice"
	NoticeVersion  = 2
)

// RetryDelay is the wait before attempt n+1 when attempt n put nothing on the
// root's screen, given n attempts already made.
func RetryDelay(attempts int) time.Duration {
	return ladder(retryBase, retryCeiling, attempts)
}

// AckWaitDelay is the wait before the same line is typed at the same root
// again, given n attempts already made and the last one delivered.
func AckWaitDelay(attempts int) time.Duration {
	return ladder(ackWaitBase, maxAckWait, attempts)
}

// ladder doubles from base to ceiling. The shift is capped before it is taken:
// a notice re-armed by hand carries its old attempt count, and shifting a
// duration by sixty is zero rather than a long wait.
func ladder(base, ceiling time.Duration, attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	shift := attempts - 1
	if shift > 10 {
		shift = 10
	}
	d := base << shift
	if d > ceiling || d <= 0 {
		return ceiling
	}
	return d
}

// Record is one task as this broker holds it: the brief it was given, plus
// everything that has happened to it since.
//
// The brief half is spelled exactly as task.json spells it, because that file
// is the protocol and a second spelling of the same field is how two halves of
// one system stop agreeing.
type Record struct {
	Protocol       int    `json:"clawdline_protocol"`
	ID             string `json:"task_id"`
	Kind           string `json:"kind"`
	Assistant      string `json:"assistant"`
	PermissionMode string `json:"permission_mode"`
	// Claims is the declared write set, as task.json spells it — D21's
	// `declared_writes`. It is written once at dispatch and never changed;
	// Lease is what it reserves, and that depends on LeaseScope.
	Claims         []string `json:"claims"`
	Isolation      string   `json:"isolation"`
	ProjectDir     string   `json:"project_dir"`
	Title          string   `json:"title"`
	Instructions   string   `json:"instructions"`
	Deliverables   []string `json:"deliverables,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes"`
	Model          string   `json:"model,omitempty"`
	// ReasoningEffort is Codex's `model_reasoning_effort` for this task's
	// session, `high` or `xhigh`, empty for the model's own default. Recorded
	// as well as used: what a session was started with is not readable off the
	// session afterwards, and a task list that cannot say it cannot answer why
	// two runs of one brief cost differently.
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	// AssistantQuota is the evidence available when this task was dispatched,
	// fixed at that moment. It is deliberately not refreshed: its job is to
	// answer why this dispatch spent this assistant's quota, not what either
	// account says now.
	AssistantQuota *AssistantQuotaDecision `json:"assistant_quota,omitempty"`
	Root           *RootRef                `json:"root,omitempty"`
	// WorkID is the line of work this task is on (D36): the key a task, its
	// root's to-do and a board item are bound by, in place of guessing from
	// titles. The dispatch names it, or the broker binds one by its rules
	// (lines.go in internal/domain/work) when it admits the task. Empty for a
	// task with no root, and for a step of other work whose line nobody named.
	WorkID string `json:"work_id,omitempty"`
	// WorkFrom is how WorkID was decided: named, respawn, graph or dispatch.
	WorkFrom string `json:"work_from,omitempty"`
	// Graph is the task graph this task is a node of, `current_node` naming
	// which (graphs.go). Nil for a task that is no graph's.
	Graph *Graph `json:"graph,omitempty"`

	// LeaseScope is `shared` or `worktree`, fixed at dispatch. Empty on a
	// record written before it existed; Scope reads those.
	LeaseScope string `json:"lease_scope,omitempty"`

	// What has happened since.
	State      State  `json:"state"`
	Dir        string `json:"dir"`
	Repository string `json:"repository"`
	// DispatchBase is the repository's HEAD when a task that writes the
	// shared checkout was admitted (D17): the line between what was there
	// before it and what it could have made. An isolated task's is its
	// Worktree.Base. Empty when it could not be read, or on a record stored
	// before W3 — and a landing cannot be proved against an unknown line.
	DispatchBase    string    `json:"dispatch_base,omitempty"`
	Worktree        *Worktree `json:"worktree,omitempty"`
	ChildTerminalID string    `json:"child_terminal_id,omitempty"`
	ChildBackend    string    `json:"child_backend,omitempty"`
	RootTerminalID  string    `json:"root_terminal_id,omitempty"`
	SpawnedAt       time.Time `json:"spawned_at,omitempty"`
	// AcceptedAt is when the child signed for its briefing (D10): the first
	// rung of the receipt chain, and the only thing that proves the briefing
	// was read rather than typed.
	AcceptedAt time.Time       `json:"accepted_at,omitempty"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`
	Result     *taskdir.Result `json:"result,omitempty"`
	// Verdict is the broker's own sentence when it ended a task the child did
	// not — a timeout, a tab that never opened. It is never dressed up as a
	// Result: a result is what the child wrote, and one this broker wrote in
	// its name is the second source of completion D15 removed.
	Verdict string   `json:"verdict,omitempty"`
	Landing *Landing `json:"landing,omitempty"`
	// Notice is the completion envelope, read from its own ledger
	// (store/broker_notices.go) and never written with the record. It is here
	// so that a reader holds one task as one value; the only way it changes
	// is through the ledger's compare-and-set, which is what stops an attempt
	// that read the task a moment ago from writing over an ACK.
	Notice *Notice `json:"-"`
	// SpawnError is the terminal's own sentence when the tab did not open,
	// or the briefing's when the tab opened and could not be briefed.
	SpawnError string `json:"spawn_error,omitempty"`
	// Unbriefed is the broker's own knowledge that it gave up on the briefing
	// without ever typing it. The secret never left this process and is not
	// kept, so nothing can brief the child afterwards: the fact decides the
	// task by itself, with no reading of the machine (dispatch.go, watch.go).
	// False on a briefing that was typed and errored — those keystrokes may
	// have landed, and only a receipt can say.
	Unbriefed bool `json:"unbriefed,omitempty"`
	// RespawnOf is the spawn_failed task this one retried, and
	// RespawnGeneration how far down that chain it is: 0 for an original.
	// The limit is counted over the family, not read from this number — see
	// respawn.go for why a depth cannot enforce it.
	RespawnOf         string `json:"respawn_of,omitempty"`
	RespawnGeneration int    `json:"respawn_generation,omitempty"`
	// ScheduleID is the schedule whose occurrence this task is, and
	// ScheduleTitle that schedule's title — the Swift app's `schedule_id` and
	// the root label it gives a scheduled task. A scheduled task has no root:
	// nobody dispatched it, and nobody is waiting on its tab (D07). Empty on
	// every task a session asked for.
	ScheduleID    string `json:"schedule_id,omitempty"`
	ScheduleTitle string `json:"schedule_title,omitempty"`
	// ScheduleCloseTab is that schedule's close_tab when this run was
	// dispatched — the Swift app's `schedule_close_tab` — and what the run's
	// end does to its tab (tabPolicy). Empty on a record written before it
	// existed, which reads as the schedule default, on_success.
	ScheduleCloseTab string `json:"schedule_close_tab,omitempty"`
	// Dispatcher is the store handle that admitted this task — its process
	// and a nonce (store.Owner). Only that process ever holds the plaintext
	// secret, so a task still `queued` after it has provably gone can never be
	// briefed, and is settled rather than left to its timeout.
	Dispatcher string `json:"dispatcher,omitempty"`
}

// AssistantQuotaDecision is the account evidence one dispatch was made with.
// ReadAt is the broker's clock; each row's ObservedAt is the provider record's
// own clock, and AgeSeconds is their difference at ReadAt. A whole-reader
// failure is kept as ReadError instead of an empty list that looks like there
// were no assistants to consider.
type AssistantQuotaDecision struct {
	ReadAt     time.Time                `json:"read_at"`
	Assistants []AssistantQuotaSnapshot `json:"assistants"`
	ReadError  string                   `json:"read_error,omitempty"`
}

// AssistantQuotaSnapshot is one immutable row of a dispatch's quota evidence.
// It mirrors the assistants route's facts without depending on its wire type.
type AssistantQuotaSnapshot struct {
	ID              string                 `json:"id"`
	Label           string                 `json:"label"`
	Installed       bool                   `json:"installed"`
	Availability    string                 `json:"availability"`
	ObservedAt      *int64                 `json:"observed_at"`
	AgeSeconds      *int64                 `json:"age_seconds"`
	Stale           bool                   `json:"stale"`
	FreshForSeconds *int64                 `json:"fresh_for_seconds"`
	ResetsAt        *int64                 `json:"resets_at"`
	Detail          string                 `json:"detail"`
	Windows         []AssistantQuotaWindow `json:"windows"`
	LastKnown       string                 `json:"last_known,omitempty"`
	UnknownReason   string                 `json:"unknown_reason,omitempty"`
}

// AssistantQuotaWindow is one provider window as it stood at dispatch.
type AssistantQuotaWindow struct {
	Name        string   `json:"name"`
	UsedPercent *float64 `json:"used_percent,omitempty"`
	ResetsAt    *int64   `json:"resets_at,omitempty"`
	Hit         bool     `json:"hit"`
}

// stored is the record as the store keeps it: the record without its long
// prose, and the prose apart (D25). The record half is what every beat and
// every list decodes, so it carries a title and never an essay.
func (r Record) stored() (json.RawMessage, map[string]string) {
	texts := map[string]string{store.TextInstructions: r.Instructions, store.TextSummary: ""}
	r.Instructions = ""
	if r.Result != nil {
		result := *r.Result
		texts[store.TextSummary] = result.Summary
		result.Summary = ""
		r.Result = &result
	}
	return r.Encode(), texts
}

// applyTexts puts a task's long prose back into a record read without it.
// A record from before the prose had its own table still carries it inline,
// and a body that is not in the table leaves that inline copy alone.
func (r *Record) applyTexts(texts map[string]string) {
	if v, ok := texts[store.TextInstructions]; ok {
		r.Instructions = v
	}
	if v, ok := texts[store.TextSummary]; ok && r.Result != nil {
		r.Result.Summary = v
	}
}

// Brief is the task.json this record would write.
func (r Record) Brief() taskdir.Brief {
	claims := r.Claims
	if claims == nil {
		claims = []string{}
	}
	b := taskdir.Brief{
		Protocol:       Protocol,
		TaskID:         r.ID,
		Kind:           r.Kind,
		Assistant:      r.Assistant,
		PermissionMode: r.PermissionMode,
		Claims:         claims,
		Isolation:      r.Isolation,
		ProjectDir:     r.ProjectDir,
		Title:          r.Title,
		Instructions:   r.Instructions,
		Deliverables:   r.Deliverables,
		TimeoutMinutes: r.TimeoutMinutes,
		CreatedAt:      r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		WorkID:         r.WorkID,
	}
	if r.Graph != nil {
		b.Graph, _ = json.Marshal(r.Graph)
	}
	if r.Root != nil {
		b.Root = &taskdir.RootRef{
			SessionID:  r.Root.SessionID,
			Assistant:  r.Root.Assistant,
			ProjectDir: r.Root.ProjectDir,
			Label:      r.Root.Label,
			PollOnly:   r.Root.PollOnly,
		}
	}
	return b
}

// Scope is the record's lease scope, reading a record written before the
// field existed by the only thing that decided it then: whether it had a
// checkout of its own.
func (r Record) Scope() string {
	if r.LeaseScope != "" {
		return r.LeaseScope
	}
	if r.Worktree != nil || r.Isolation == IsolationWorktree {
		return LeaseWorktree
	}
	return LeaseShared
}

// Lease is the paths this task reserves in the shared tree against other
// roots: its declared writes when it writes there, nothing when it has a
// checkout of its own. Never nil, because an empty lease is an answer.
func (r Record) Lease() []string {
	if r.Scope() == LeaseWorktree || r.Claims == nil {
		return []string{}
	}
	return r.Claims
}

// DeclaredWrites is the landing write set: what the task said it would write,
// whatever its lease became. The second answer is false when that cannot be
// known — a task that declared nothing at all, or an isolated one stored
// before W1 by the broker that erased its list on the way in. Unknown is not
// "writes nothing", which is what an empty list would say.
func (r Record) DeclaredWrites() ([]string, bool) {
	if r.Claims == nil {
		return nil, false
	}
	if r.LeaseScope == "" && r.Scope() == LeaseWorktree {
		return nil, false
	}
	return r.Claims, true
}

// Age is how long this task has existed, never negative.
func (r Record) Age(now time.Time) int {
	age := int(now.Sub(r.CreatedAt).Seconds())
	if age < 0 {
		return 0
	}
	return age
}

// Deadline is when this task is considered timed out: a wall clock started
// when the task was admitted (`created_at`), with no grace of any kind
// (docs/design-decisions.md D12).
//
// Not from the moment the child was briefed, and not paused while this daemon
// is down: the budget is an upper bound on the whole task, including a tab
// that was slow to open, and a restart is not time the child did not have. The
// Swift app's twenty-second restart grace belongs to closing a finished tab
// (linger), never to this clock.
func (r Record) Deadline() time.Time {
	if r.TimeoutMinutes <= 0 {
		return time.Time{}
	}
	return r.CreatedAt.Add(time.Duration(r.TimeoutMinutes) * time.Minute)
}

// Encode is the record as it is stored.
func (r Record) Encode() json.RawMessage {
	body, _ := json.Marshal(r)
	return body
}

// Decode reads a stored record.
func Decode(raw json.RawMessage) (Record, error) {
	var r Record
	err := json.Unmarshal(raw, &r)
	return r, err
}

// SettleState maps a child's own word to the state this broker records.
//
// An unrecognised word is a failure rather than a success: a result nobody can
// read is not a delivery, and guessing in the direction of success is how a
// silent failure becomes a landed one.
func SettleState(status string) State {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "ok", "done":
		return StateSuccess
	default:
		return StateFailure
	}
}
