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

	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
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

// Isolation is where the child's checkout is.
const (
	IsolationNone     = "none"
	IsolationWorktree = "worktree"
)

// Worktree is the checkout a task with `isolation: "worktree"` was given.
//
// `base` is recorded at creation and never recomputed. It is what the branch
// started from, and a reader asking "did this branch produce anything" compares
// head against it; recomputing base later would answer that question with the
// wrong past.
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
type Landing struct {
	State  LandingState `json:"state"`
	Target string       `json:"target,omitempty"`
	Commit string       `json:"commit,omitempty"`
	Repo   string       `json:"repo,omitempty"`
	At     time.Time    `json:"at,omitempty"`
	Note   string       `json:"note,omitempty"`
}

// NoticeState is where the completion notice to the root has got to.
type NoticeState string

const (
	NoticePending      NoticeState = "pending"
	NoticeDelivered    NoticeState = "delivered"
	NoticeAcknowledged NoticeState = "acknowledged"
	NoticeDeadLetter   NoticeState = "dead_letter"
)

// NoticeError is why one delivery attempt did not happen, in the Swift app's
// vocabulary: root_missing, identity_stale, conversation_ambiguous,
// root_choosing, terminal_timeout, transport_failed, acknowledgement_timeout.
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

// AttemptLimit and the retry ladder are the Swift app's measured values:
// 5, 10, 20, 40, 80, 160, 300, 300 … seconds, and eight attempts in all.
const (
	AttemptLimit   = 8
	retryBase      = 5 * time.Second
	retryCeiling   = 300 * time.Second
	NoticeProtocol = "clawdline.notice"
	NoticeVersion  = 2
)

// RetryDelay is the wait before attempt n+1, given n attempts already made.
func RetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	shift := attempts - 1
	if shift > 10 {
		shift = 10
	}
	d := retryBase << shift
	if d > retryCeiling {
		return retryCeiling
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
	Protocol       int      `json:"clawdline_protocol"`
	ID             string   `json:"task_id"`
	Kind           string   `json:"kind"`
	Assistant      string   `json:"assistant"`
	PermissionMode string   `json:"permission_mode"`
	Claims         []string `json:"claims"`
	Isolation      string   `json:"isolation"`
	ProjectDir     string   `json:"project_dir"`
	Title          string   `json:"title"`
	Instructions   string   `json:"instructions"`
	Deliverables   []string `json:"deliverables,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes"`
	CreatedAt      time.Time `json:"created_at"`
	Root           *RootRef  `json:"root,omitempty"`

	// What has happened since.
	State           State           `json:"state"`
	Dir             string          `json:"dir"`
	Repository      string          `json:"repository"`
	Worktree        *Worktree       `json:"worktree,omitempty"`
	ChildTerminalID string          `json:"child_terminal_id,omitempty"`
	ChildBackend    string          `json:"child_backend,omitempty"`
	RootTerminalID  string          `json:"root_terminal_id,omitempty"`
	SpawnedAt       time.Time       `json:"spawned_at,omitempty"`
	FinishedAt      time.Time       `json:"finished_at,omitempty"`
	Result          *taskdir.Result `json:"result,omitempty"`
	Landing         *Landing        `json:"landing,omitempty"`
	Notice          *Notice         `json:"notice,omitempty"`
	// SpawnError is the terminal's own sentence when the tab did not open.
	SpawnError string `json:"spawn_error,omitempty"`
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

// Age is how long this task has existed, never negative.
func (r Record) Age(now time.Time) int {
	age := int(now.Sub(r.CreatedAt).Seconds())
	if age < 0 {
		return 0
	}
	return age
}

// Deadline is when this task is considered timed out.
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
