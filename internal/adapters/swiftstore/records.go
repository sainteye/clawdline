package swiftstore

import (
	"encoding/json"
	"strings"
	"time"
)

// These are the Swift app's on-disk records, spelled as it writes them
// (Sources/OrchestratorStore.swift). Only fields the session list and the task
// list read are declared: an undeclared key is skipped by the decoder, which is
// how `secret_hash`, `queued_secret` and the transcript paths never reach this
// process's memory as values.

// Orchestrator is ~/.config/clawdline/orchestrator.json.
type Orchestrator struct {
	Version              int                  `json:"version"`
	Tasks                []Task               `json:"tasks"`
	Handoffs             []Handoff            `json:"handoffs"`
	HandoffLabels        []HandoffLabel       `json:"handoff_labels"`
	RootAssignments      []RootAssignment     `json:"root_assignments"`
	CoordinationWaits    []CoordinationWait   `json:"coordination_waits"`
	SessionDeliveries    []SessionDelivery    `json:"session_deliveries"`
	SessionSelfStates    []SessionSelfState   `json:"session_self_states"`
	ClosureAttestations  []ClosureAttestation `json:"closure_attestations"`
	ObligationGeneration int64                `json:"obligation_generation"`
}

// Seconds is a Unix time the Swift app writes as a JSON number, sometimes with
// a fraction. The wire always sends it rounded down to whole seconds, as
// Swift's `Int(date.timeIntervalSince1970)` does.
type Seconds float64

func (s Seconds) Unix() int64     { return int64(s) }
func (s Seconds) Time() time.Time { return time.Unix(0, int64(float64(s)*1e9)) }

// Task is one dispatched task.
type Task struct {
	ID               string   `json:"id"`
	State            string   `json:"state"`
	Kind             string   `json:"kind"`
	Title            string   `json:"title"`
	Assistant        string   `json:"assistant"`
	ProjectDir       string   `json:"project_dir"`
	Created          Seconds  `json:"created"`
	Depth            int64    `json:"depth"`
	Model            *string  `json:"model"`
	WorkItemID       *string  `json:"work_item_id"`
	WorkPhase        *string  `json:"work_phase"`
	ReasoningEffort  *string  `json:"reasoning_effort"`
	ScheduleID       *string  `json:"schedule_id"`
	SessionRoot      bool     `json:"session_root"`
	Permission       string   `json:"permission"`
	SpawnedAt        *Seconds `json:"spawned_at"`
	BriefedAt        *Seconds `json:"briefed_at"`
	FinishedAt       *Seconds `json:"finished_at"`
	ResultVerifiedAt *Seconds `json:"result_verified_at"`

	RootSession   *string `json:"root_session"`
	RootAssistant *string `json:"root_assistant"`
	RootLabel     *string `json:"root_label"`
	ParentTask    *string `json:"parent_task"`

	ChildTerminal    *string  `json:"child_terminal"`
	ChildBackend     *string  `json:"child_backend"`
	ChildSession     *string  `json:"child_session"`
	ChildTTY         *string  `json:"child_tty"`
	ChildPID         *int64   `json:"child_pid"`
	ChildProcStart   *Seconds `json:"child_proc_start"`
	TranscriptProven bool     `json:"transcript_proven"`

	AttachSession *string  `json:"attach_session"`
	Artifacts     []string `json:"artifacts"`
	// Claims is nil when the key is absent: a task that declared no claims and
	// one that declared an empty list are different records.
	Claims             []string            `json:"claims"`
	UntouchedClaims    []string            `json:"untouched_claims"`
	Usage              *TaskUsage          `json:"usage"`
	Landing            *Landing            `json:"landing"`
	Worktree           *Worktree           `json:"worktree"`
	CompletionDelivery *CompletionDelivery `json:"completion_delivery"`
	// Summary is whether the child wrote one, and its words when they are a
	// string: the worktree lifecycle shows them when there is no progress note.
	Summary present `json:"summary"`
	// Plan and Progress are the task's own narrative, read by the Projects
	// page's worktree lifecycle (ProjectWorktreeLifecycle.resolveOwner).
	Plan     *string        `json:"plan"`
	Progress []TaskProgress `json:"progress"`
}

// TaskProgress is one progress note a child sent.
type TaskProgress struct {
	Note string `json:"note"`
}

// present records that a key was there, and keeps its words when they are a
// string.
type present struct {
	set  bool
	Text string
}

func (p *present) UnmarshalJSON(b []byte) error {
	p.set = string(b) != "null"
	_ = json.Unmarshal(b, &p.Text)
	return nil
}

// Set is whether the key carried a value.
func (p present) Set() bool { return p.set }

// CompletionDelivery is how far the root has been told the task finished.
type CompletionDelivery struct {
	State string `json:"state"`
}

// Live is the Swift app's "not yet terminal": an unrecognised state counts as
// live, because it fails open (OrchestratorTaskList.swift).
func (t Task) Live() bool {
	switch t.State {
	case "success", "failure", "timeout", "cancelled", "spawn_failed":
		return false
	}
	return true
}

// TaskUsage is what a task's child spent.
type TaskUsage struct {
	Input      int64    `json:"input"`
	Output     int64    `json:"output"`
	CacheRead  int64    `json:"cache_read"`
	CacheWrite int64    `json:"cache_write"`
	Total      int64    `json:"total"`
	Model      *string  `json:"model"`
	CostUSD    *float64 `json:"cost_usd"`
}

// Landing is where a delivery landed, as the broker verified it.
type Landing struct {
	State                string   `json:"state"`
	Commit               *string  `json:"commit"`
	VerifiedCommit       *string  `json:"verified_commit"`
	VerifiedTargetCommit *string  `json:"verified_target_commit"`
	Target               *string  `json:"target"`
	VerificationOrigin   *string  `json:"verification_origin"`
	RepositoryCommonDir  *string  `json:"repository_common_dir"`
	LandedAt             *Seconds `json:"landed_at"`
}

// Worktree is only read for whether it is there, and for the part the wire
// exposes.
type Worktree struct {
	Path    string  `json:"path"`
	Branch  string  `json:"branch"`
	Base    string  `json:"base"`
	Head    *string `json:"head"`
	Commits *int64  `json:"commits"`
	Dirty   *bool   `json:"dirty"`
}

// Handoff is one handoff envelope; only whether it is still open, and whose it
// is, matter here.
type Handoff struct {
	ID          string  `json:"handoff_id"`
	State       string  `json:"state"`
	FromSession *string `json:"from_session"`
	Created     Seconds `json:"created"`
}

// HandoffLabel names the tab a handoff opened.
type HandoffLabel struct {
	HandoffID string                 `json:"handoff_id"`
	Label     string                 `json:"label"`
	Identity  RootAssignmentIdentity `json:"identity"`
}

// Identity is a recorded process identity: the six facts a live session must
// match before a record is believed to be about it.
type Identity struct {
	TerminalID     string   `json:"terminal_id"`
	TTY            string   `json:"tty"`
	Assistant      *string  `json:"assistant"`
	PID            *int64   `json:"pid"`
	ProcessStart   *Seconds `json:"process_start"`
	ConversationID *string  `json:"conversation_id"`
}

// SessionDelivery is a session's own "this turn is delivered" receipt.
type SessionDelivery struct {
	Identity
	Summary    string   `json:"summary"`
	ReportedAt Seconds  `json:"reported_at"`
	Settled    bool     `json:"settled"`
	Landing    *Landing `json:"landing"`
}

// SessionSelfState is what a session declared about itself.
type SessionSelfState struct {
	Identity
	Claim           *string  `json:"claim"`
	Note            *string  `json:"note"`
	MovedBy         *string  `json:"moved_by"`
	PersonNeeded    *bool    `json:"person_needed"`
	ClaimReportedAt *Seconds `json:"claim_reported_at"`
	ClaimSettled    bool     `json:"claim_settled"`
	Owed            *Owed    `json:"owed"`
}

// Owed is the second axis a session can declare: something it is owed.
type Owed struct {
	Note         string  `json:"note"`
	PersonNeeded bool    `json:"person_needed"`
	Since        Seconds `json:"since"`
	MovedBy      *string `json:"moved_by"`
}

// RootAssignment is an independently owned Feature Root.
type RootAssignment struct {
	ID       string                  `json:"id"`
	Label    string                  `json:"label"`
	State    string                  `json:"state"`
	Identity *RootAssignmentIdentity `json:"identity"`
}

// RootAssignmentIdentity is matched loosely: the optional facts only have to
// agree when they were recorded (Orchestrator.swift rootAssignmentIdentityMatches).
type RootAssignmentIdentity struct {
	TerminalID     string   `json:"terminal_id"`
	Assistant      string   `json:"assistant"`
	TTY            *string  `json:"tty"`
	PID            *int64   `json:"pid"`
	ProcessStart   *Seconds `json:"process_start"`
	ConversationID *string  `json:"conversation_id"`
}

// CoordinationWait is a file-ownership wait between sessions.
type CoordinationWait struct {
	ID               string               `json:"id"`
	Repository       string               `json:"repository"`
	Paths            []string             `json:"paths"`
	OwnerSessionID   string               `json:"owner_session_id"`
	ReleaseCondition string               `json:"release_condition"`
	Created          Seconds              `json:"created"`
	Waiters          []CoordinationWaiter `json:"waiters"`
}

// CoordinationWaiter is one session parked on a wait.
type CoordinationWaiter struct {
	SessionID          string   `json:"session_id"`
	Reason             string   `json:"reason"`
	Created            Seconds  `json:"created"`
	RequestDeliveredAt *Seconds `json:"request_delivered_at"`
	ReleaseDeliveredAt *Seconds `json:"release_delivered_at"`
}

// ClosureAttestation is a session's own "nothing is owed, I may be closed".
type ClosureAttestation struct {
	Identity
	ID                   string  `json:"id"`
	Note                 string  `json:"note"`
	Created              Seconds `json:"created"`
	ActivityGeneration   int64   `json:"activity_generation"`
	ObligationGeneration int64   `json:"obligation_generation"`
}

// Coordinator is ~/.config/clawdline/coordinator.json: which live process is
// Clawdfather. Only the identity is read.
type Coordinator struct {
	Version        int      `json:"version"`
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	SessionID      string   `json:"sessionID"`
	Assistant      string   `json:"assistant"`
	TTY            string   `json:"tty"`
	PID            *int64   `json:"pid"`
	ProcessStart   *Seconds `json:"processStart"`
	ConversationID *string  `json:"conversationID"`
	CWD            string   `json:"cwd"`
}

// Live is one session as this daemon sees it, reduced to what a recorded
// identity is compared with.
type Live struct {
	TerminalID     string
	TTY            string
	Assistant      string
	PID            int64
	ProcessStart   time.Time
	ConversationID string
}

// startTolerance is the Swift app's SessionRegistry.startTolerance.
const startTolerance = 5 * time.Second

// sameTTY compares ttys the way both apps spell them: the Swift app records
// "/dev/ttys012", this daemon's inventory carries "ttys012".
func sameTTY(a, b string) bool {
	return strings.TrimPrefix(a, "/dev/") == strings.TrimPrefix(b, "/dev/")
}

func sameStart(recorded *Seconds, live time.Time) bool {
	if recorded == nil || live.IsZero() {
		return false
	}
	d := recorded.Time().Sub(live)
	if d < 0 {
		d = -d
	}
	return d <= startTolerance
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Matches is `recordedIdentityMatchesCurrentSession`
// (Orchestrator.swift): all six facts, with nothing borrowed. A live session
// whose process facts could not be read matches nothing, because a record that
// cannot be tied to this process is not about it.
func (id Identity) Matches(l Live) bool {
	if l.Assistant == "" || l.PID == 0 || l.ConversationID == "" {
		return false
	}
	return id.TerminalID == l.TerminalID &&
		sameTTY(id.TTY, l.TTY) &&
		deref(id.Assistant) == l.Assistant &&
		id.PID != nil && *id.PID == l.PID &&
		sameStart(id.ProcessStart, l.ProcessStart) &&
		deref(id.ConversationID) == l.ConversationID
}
