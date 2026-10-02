package work

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Work system v2 is deliberately separate from the v1 board projection in
// board.go. V1 derives placement from broker activity. V2 stores a person's
// item and lets only its owning Agent move the execution phase.

type Kind string

const (
	KindFeature  Kind = "feature"
	KindIssue    Kind = "issue"
	KindEpic     Kind = "epic"
	KindRefactor Kind = "refactor"
	KindPlan     Kind = "plan"
)

func (k Kind) Valid() bool {
	switch k {
	case KindFeature, KindIssue, KindEpic, KindRefactor, KindPlan:
		return true
	}
	return false
}

// Executable kinds are assigned to a Session and move through the execution
// phases. An Epic is executable too, behind a plan-and-review gate
// (EpicPlanGate); only a Plan stays in Planning.
func (k Kind) Executable() bool { return k.FeatureLike() || k == KindIssue || k == KindEpic }

// FeatureLike kinds follow a Feature's rules: a Refactor is an internal
// structural change that leaves outward behaviour alone, and it is assigned,
// planned, gated and reviewed exactly as a Feature is.
func (k Kind) FeatureLike() bool { return k == KindFeature || k == KindRefactor }

type Phase string

const (
	PhaseCreated      Phase = "created"
	PhaseAssigning    Phase = "assigning"
	PhaseAssigned     Phase = "assigned"
	PhaseImplementing Phase = "implementing"
	PhaseVerifying    Phase = "verifying"
	PhaseMerging      Phase = "merging"
	PhaseDeploying    Phase = "deploying"
	PhaseDone         Phase = "done"
	PhaseCancelled    Phase = "cancelled"
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseCreated, PhaseAssigning, PhaseAssigned, PhaseImplementing, PhaseVerifying,
		PhaseMerging, PhaseDeploying, PhaseDone, PhaseCancelled:
		return true
	}
	return false
}

func (p Phase) Terminal() bool { return p == PhaseDone || p == PhaseCancelled }

type Condition string

const (
	ConditionBlocked            Condition = "blocked"
	ConditionWaitingUser        Condition = "waiting_user"
	ConditionOwnerRequired      Condition = "owner_required"
	ConditionOwnerOffline       Condition = "owner_offline"
	ConditionEvidenceUnknown    Condition = "evidence_unknown"
	ConditionAssignmentFailed   Condition = "assignment_failed"
	ConditionAssignedUnnotified Condition = "assigned_unnotified"
)

func (c Condition) Valid() bool {
	switch c {
	case "", ConditionBlocked, ConditionWaitingUser, ConditionOwnerRequired, ConditionOwnerOffline,
		ConditionEvidenceUnknown, ConditionAssignmentFailed, ConditionAssignedUnnotified:
		return true
	}
	return false
}

func (c Condition) AgentOwned() bool {
	return c == "" || c == ConditionBlocked || c == ConditionWaitingUser
}

type DeploymentPolicy string

const (
	DeployRequired     DeploymentPolicy = "required"
	DeployNotRequired  DeploymentPolicy = "not_required"
	DeployAgentDecides DeploymentPolicy = "agent_decides"
)

func (p DeploymentPolicy) Valid() bool {
	return p == DeployRequired || p == DeployNotRequired || p == DeployAgentDecides
}

// ItemV2 is the durable identity and execution state of one person-created
// item. ProjectPath is a historical snapshot; presentation is joined from the
// current Project catalog on reads.
type ItemV2 struct {
	ID          string
	ProjectID   string
	ProjectPath string
	Kind        Kind
	Title       string
	Description string
	// AcceptanceCriteria is the exact bounded Markdown contract for this item.
	// The owning Session may fill an empty contract once before verification;
	// later revisions require the person directly or the dedicated run-backed Root route.
	// AcceptanceVersion changes only when those bytes change, and
	// AcceptanceDigest is the SHA-256 of those exact bytes.
	AcceptanceCriteria string
	AcceptanceVersion  int64
	AcceptanceDigest   string
	Phase              Phase
	Condition          Condition
	UserAction         string
	// DecisionID is the open decision an Agent's waiting_user points at: the
	// person answers it with one of its options, and its end clears the
	// condition. Empty for every other condition, and for a waiting_user the
	// daemon itself wrote with a UserAction.
	DecisionID       string
	DeploymentPolicy DeploymentPolicy
	// ReviewRequired is the person's "Needs independent review" switch on a
	// Feature. Only the person sets it, and the planning gate reads it live
	// when the Feature asks to enter implementing; it is never captured in
	// the gate snapshot. It is always false on every other kind.
	ReviewRequired bool
	OwnerSession   string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ClosedAt       time.Time
	Cycle          int64
	// GateSnapshotCycle is zero until the first successful assignment of this
	// cycle. When it equals Cycle, PlanningGate and VerifyGate are immutable
	// for that cycle, including across reassignment.
	GateSnapshotCycle int64
	GateSnapshotAt    time.Time
	PlanningGate      bool
	VerifyGate        bool
	// CycleBaseCommit is the full Project repository HEAD captured before the
	// first successful verify-on assignment of this cycle. Candidate admission
	// requires a strict descendant of it.
	CycleBaseCommit string
	Version         int64
	// CreatedVia is the person's message a Session created this item on
	// (work-system-v2 §2, amended 2026-09-25); nil for an item a person
	// created as themselves.
	CreatedVia *CreatedViaV2
	// ParentID is the Epic this item was broken out of by the Epic's owner
	// Session (EpicChildParent); empty for every other item. It never changes
	// after creation.
	ParentID string
}

// AcceptanceDigest returns the lowercase SHA-256 of the exact Markdown bytes
// stored on an item. It deliberately does no whitespace normalization: a
// checker and the item must name the same contract, byte for byte.
func AcceptanceDigest(criteria string) string {
	sum := sha256.Sum256([]byte(criteria))
	return hex.EncodeToString(sum[:])
}

// SetAcceptance initializes or changes an item's governed acceptance state.
// It returns true only when the exact stored bytes changed.
func SetAcceptance(i *ItemV2, criteria string) bool {
	if i.AcceptanceVersion > 0 && i.AcceptanceCriteria == criteria {
		return false
	}
	i.AcceptanceCriteria = criteria
	if i.AcceptanceVersion == 0 {
		i.AcceptanceVersion = 1
	} else {
		i.AcceptanceVersion++
	}
	i.AcceptanceDigest = AcceptanceDigest(criteria)
	return true
}

// HasGateSnapshot says the item's current execution cycle has captured the
// two global switches. The bools alone cannot say this because false is a
// legitimate captured value.
func (i ItemV2) HasGateSnapshot() bool {
	return i.Cycle > 0 && i.GateSnapshotCycle == i.Cycle
}

// GateNeedsAcceptance says whether this item's captured gates require an
// acceptance contract. Planning exempts Issues; verification does not.
func (i ItemV2) GateNeedsAcceptance() bool {
	return i.VerifyGate || i.PlanningGate && (i.Kind.FeatureLike() || i.Kind == KindEpic)
}

// CreatedViaV2 is the provenance of an item a Session created because a
// person told it to through Clawdline: the run of that message, when it was
// said, and a bounded excerpt of what was said.
//
// An item an Epic's owner Session broke out of its Epic carries no run: its
// authority is the person's assignment of the Epic, so it names the Epic
// instead and Run stays empty.
//
// On an assignment it is the person's message a Session acted on: a claim,
// where Session took the item itself, or, with Assigned, Session handing an
// ordinary item to a new Session opened as Persona (none when empty).
type CreatedViaV2 struct {
	Run      string `json:"run"`
	Session  string `json:"session_id"`
	At       int64  `json:"at"`
	Excerpt  string `json:"excerpt,omitempty"`
	Epic     string `json:"epic_id,omitempty"`
	Assigned bool   `json:"assigned,omitempty"`
	Persona  string `json:"persona,omitempty"`
}

// SessionAssignable is whether a Session may hand an item of this kind to a
// new Session on the person's message: an ordinary Feature, Refactor or Issue.
// An Epic is planned by whoever the person gives it to, and a Plan stays in
// Planning.
func SessionAssignable(k Kind) bool { return k.FeatureLike() || k == KindIssue }

type AssignmentV2 struct {
	ID             string
	WorkID         string
	Mode           string
	SessionID      string
	TerminalID     string
	Assistant      string
	Model          string
	State          string
	HumanActor     string
	RootAssignment string
	Failure        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ReleasedAt     time.Time
	// ClaimedVia is the person's message a Session claimed this item on:
	// the same provenance an item a Session created carries, recorded on
	// the assignment it produced. Nil for an assignment a person made.
	ClaimedVia *CreatedViaV2
	// Persona is the built-in persona a new Session was opened as for this
	// assignment (docs/personas.md); empty for none and for every
	// existing-session assignment.
	Persona string
	// GatePreviewed records the global mode read before a new Session was
	// opened. The item does not gain its snapshot until this assignment becomes
	// active, but a dialog or restart must not make it capture a different mode
	// from the Root Assignment it was shown.
	GatePreviewed bool
	PlanningGate  bool
	VerifyGate    bool
	// CycleBaseCommit is previewed outside the store transaction and becomes
	// authoritative only when this pending assignment is activated.
	CycleBaseCommit string
}

type DocumentV2 struct {
	ID        string
	WorkID    string
	Role      string
	Title     string
	Body      string
	Reference string
	Position  int64
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ImageV2 is the durable metadata for one person-added visual reference. The
// normalized PNG bytes stay in the store and are read through the shared image
// route only when a viewer needs them; Board reads never carry image bodies.
type ImageV2 struct {
	ID        string
	WorkID    string
	Title     string
	MediaType string
	ByteCount int64
	Width     int
	Height    int
	Position  int64
	CreatedBy string
	CreatedAt time.Time
}

type StepV2 struct {
	ID          string
	WorkID      string
	Title       string
	Done        bool
	Position    int64
	CreatedBy   string
	CompletedBy string
	CreatedAt   time.Time
	CompletedAt time.Time
	Version     int64
}

type EventV2 struct {
	Seq             int64
	WorkID          string
	Kind            string
	Actor           string
	PreviousVersion int64
	NextVersion     int64
	Payload         string
	At              time.Time
}

type DirectTodoV2 struct {
	ID          string
	SessionID   string
	Text        string
	CreatedBy   string
	CreatedAt   time.Time
	SentAt      time.Time
	ReadAt      time.Time
	CompletedAt time.Time
	CompletedBy string
	Version     int64
}

func (t DirectTodoV2) Open() bool { return t.CompletedAt.IsZero() }

// DirectTodoImageV2 is one durable visual reference attached to a direct
// Session to-do. Its normalized PNG body stays in the store and travels to the
// Session only when the person presses Send.
type DirectTodoImageV2 struct {
	ID        string
	TodoID    string
	Title     string
	MediaType string
	ByteCount int64
	Width     int
	Height    int
	Position  int64
	CreatedBy string
	CreatedAt time.Time
}

type ProposalV2 struct {
	ID                  string
	ProjectID           string
	ProjectPath         string
	Kind                Kind
	Title               string
	Description         string
	Reason              string
	SuggestedAcceptance string
	SessionID           string
	SourceWorkID        string
	SourceTodoID        string
	State               string
	AcceptedWorkID      string
	CreatedAt           time.Time
	ResolvedAt          time.Time
	Version             int64
}

func (i ItemV2) Planning() bool { return !i.Kind.Executable() }

// Area is a presentation decision, not another stored state.
func (i ItemV2) Area() string {
	switch {
	case i.Planning():
		return "planning"
	case i.Phase.Terminal():
		return "recently_done"
	case i.OwnerSession == "":
		return "unassigned"
	default:
		return string(i.Phase)
	}
}

type RefusalV2 struct {
	Code    string
	Message string
}

func (r RefusalV2) Error() string { return r.Code + ": " + r.Message }

func RefuseV2(code, message string) error { return RefusalV2{Code: code, Message: message} }

// LeaveDecision keeps an item's link to a decision only while the item still
// waits on it: the same decision, condition waiting_user, the same owner,
// and the item open. Otherwise the link is dropped from next — together with
// a waiting_user that had nothing else to say — and the decision it pointed
// at is answered, so its caller can withdraw it if it is still open.
//
// The one exception is ReleasedAtClose: a closing Session that had already
// asked the person to deploy leaves the item, and the question stays.
func LeaveDecision(prev ItemV2, next *ItemV2) string {
	if ReleasedAtClose(prev, *next) {
		return ""
	}
	if next.Condition != ConditionWaitingUser || next.Phase.Terminal() || next.OwnerSession != prev.OwnerSession {
		if next.DecisionID == prev.DecisionID || next.Condition != ConditionWaitingUser {
			next.DecisionID = ""
		}
		if next.Condition == ConditionWaitingUser && next.DecisionID == "" && next.UserAction == "" {
			next.Condition = ""
		}
	}
	if prev.DecisionID == "" || prev.DecisionID == next.DecisionID {
		return ""
	}
	return prev.DecisionID
}

// AwaitsPersonToDeploy says the owning Session has already asked the person
// to deploy this item: it is in deploying, waits on the person, and that wait
// is a decision. Whether the decision is still open is the store's to read.
// Such an item does not keep its Session from closing (docs/work-system.md,
// "Closing a Session that owns Board items").
func (i ItemV2) AwaitsPersonToDeploy() bool {
	return i.Phase == PhaseDeploying && i.Condition == ConditionWaitingUser && i.DecisionID != "" &&
		i.OwnerSession != ""
}

// ReleaseAtClose is the item a closing Session leaves after asking the person
// to deploy it: no owner, and nothing else changed — still waiting_user, still
// linked to the same open decision, which the person answers as before.
func ReleaseAtClose(prev ItemV2, now time.Time) ItemV2 {
	next := prev
	next.OwnerSession, next.UpdatedAt = "", now
	return next
}

// ReleasedAtClose recognises exactly the change ReleaseAtClose makes: an item
// that awaited the person to deploy loses its owner and nothing else that
// LeaveDecision reads. Every other owner change still drops the decision.
func ReleasedAtClose(prev, next ItemV2) bool {
	return prev.AwaitsPersonToDeploy() && next.OwnerSession == "" &&
		next.Phase == prev.Phase && next.Condition == prev.Condition &&
		next.DecisionID == prev.DecisionID && next.UserAction == prev.UserAction
}

// HandedOverWaiting recognises a handoff's owner change on an item that waits
// on the person: a new owner, and nothing LeaveDecision reads changed. The
// question was asked about the work, not about the Session that asked it, so
// it goes with the work to the Session that continues it (docs/handoff.md,
// "Milestone handoffs"); an answer is delivered to the item's owner.
func HandedOverWaiting(prev, next ItemV2) bool {
	return prev.DecisionID != "" && prev.Condition == ConditionWaitingUser && !prev.Phase.Terminal() &&
		next.OwnerSession != "" && next.OwnerSession != prev.OwnerSession &&
		next.Phase == prev.Phase && next.Condition == prev.Condition &&
		next.DecisionID == prev.DecisionID && next.UserAction == prev.UserAction
}

func ValidateNewV2(i ItemV2) error {
	switch {
	case strings.TrimSpace(i.ProjectID) == "":
		return RefuseV2("project_required", "Choose a Project from the catalog.")
	case !i.Kind.Valid():
		return RefuseV2("invalid_kind", "Kind is feature, issue, epic, refactor, or plan.")
	case strings.TrimSpace(i.Title) == "":
		return RefuseV2("title_required", "Title is required.")
	case strings.TrimSpace(i.Description) == "":
		return RefuseV2("description_required", "Description is required.")
	case !i.DeploymentPolicy.Valid():
		return RefuseV2("invalid_deployment_policy", "Deployment policy is required, not_required, or agent_decides.")
	}
	if err := ReviewRequiredApplies(i.Kind, i.ReviewRequired); err != nil {
		return err
	}
	if i.Kind.Executable() && i.Phase != PhaseCreated {
		return RefuseV2("invalid_initial_phase", "Executable work begins in created.")
	}
	if !i.Kind.Executable() && i.Phase != PhaseCreated {
		return RefuseV2("invalid_initial_phase", "Planning work does not enter execution.")
	}
	return nil
}

// ReviewRequiredApplies refuses the person's "Needs independent review"
// switch on anything but a Feature or Refactor: an Epic is always reviewed when planning
// is on and an Issue never is, so the switch would say nothing there.
func ReviewRequiredApplies(k Kind, required bool) error {
	if required && !k.FeatureLike() {
		return RefuseV2("review_required_not_applicable",
			"Needs independent review is a Feature's or Refactor's switch; an Epic is always reviewed with planning on and an Issue is not.")
	}
	return nil
}

// Document roles. Plan and plan_review belong to a Feature, Refactor or Epic: they are
// the record the kind-aware planning gate reads.
const (
	DocumentPlan       = "plan"
	DocumentPlanReview = "plan_review"
)

// DocumentRoleValid says whether role is a document role at all, before it
// is asked whether it fits the item's kind.
func DocumentRoleValid(role string) bool {
	switch role {
	case "spec", "design", "test", "deploy", "completion_report", "other", DocumentPlan, DocumentPlanReview:
		return true
	}
	return false
}

// DocumentRoleApplies refuses a plan or plan_review on anything but a
// Feature, Refactor or Epic.
func DocumentRoleApplies(i ItemV2, role string) error {
	if (role == DocumentPlan || role == DocumentPlanReview) && i.Kind != KindEpic && !i.Kind.FeatureLike() {
		return RefuseV2("document_role_not_applicable",
			"Plan and plan_review documents belong to a Feature, Refactor or Epic; use spec or design for this item.")
	}
	return nil
}

// PlanningGate is the rule a Feature or Epic crosses before implementing; a
// Refactor crosses it as a Feature does.
// It reads only the current cycle snapshot, never the later global setting.
// A Feature takes the reviewed-plan path only when the person checked its
// ReviewRequired switch, read live; otherwise its acceptance criteria are
// enough. A reviewed Feature plan revised later needs an explicit
// unchanged-boundary assessment or a fresh review.
//
// review is what the latest plan_review concluded, read by the caller from
// the referenced task's receipt (ReadPlanReview). A review with a blocking
// finding stops the item even when a review ran; one with only non-blocking
// findings, or a legacy receipt with no severities, lets it through.
func PlanningGate(i ItemV2, next Phase, plans []DocumentV2, review PlanReviewSummary) error {
	if i.Phase != PhaseAssigned || next != PhaseImplementing || i.Kind == KindIssue {
		return nil
	}
	if i.Kind != KindEpic && !i.Kind.FeatureLike() {
		return nil
	}
	if !i.HasGateSnapshot() {
		return RefuseV2("gate_snapshot_required", "This assignment has no gate snapshot; reassign it before implementation.")
	}
	if !i.PlanningGate {
		return nil
	}
	if strings.TrimSpace(i.AcceptanceCriteria) == "" {
		return RefuseV2("acceptance_required", "Write acceptance criteria before this item enters implementation.")
	}
	if i.Kind.FeatureLike() && !i.ReviewRequired {
		return nil
	}
	lastPlan, lastReview, reviews := -1, -1, 0
	for n, d := range plans {
		switch d.Role {
		case DocumentPlan:
			lastPlan = n
		case DocumentPlanReview:
			lastReview = n
			reviews++
		}
	}
	rounds := 1
	prefix := "feature"
	if i.Kind == KindEpic {
		rounds, prefix = EpicPlanReviewRounds, "epic"
	}
	switch {
	case lastPlan < 0:
		return RefuseV2(prefix+"_plan_required",
			"Write the item's plan first: `clawdline item doc "+i.ID+" --role plan --title \"Plan\"` with the plan as its body.")
	case lastReview < lastPlan && ((i.Kind == KindEpic && reviews < rounds) ||
		(i.Kind.FeatureLike() && !UnchangedReviewBoundary(plans, lastReview, lastPlan))):
		return RefuseV2(prefix+"_plan_review_required",
			"The latest plan has no independent review yet. Dispatch one: `clawdline dispatch --kind plan_review --work-id "+i.ID+
				" --title \"Review the plan\" --claims \"\"` with the review brief on stdin, and wait for that child to finish: "+
				"a successful review is recorded on the item by itself. Only if it is not, add it as a plan_review document "+
				"whose reference is that task's id.")
	}
	return planReviewBlocking(i, prefix, lastPlan, lastReview, reviews, rounds, review)
}

// PlanReviewSummary is what the gate needs from the latest plan_review: the
// summaries of its blocking findings. A legacy receipt — none readable, no
// findings array, or a finding without a severity — has Legacy set and is
// judged as before: only that a review ran counts.
type PlanReviewSummary struct {
	Legacy   bool
	Blocking []string
}

// PlanReviewBlockingListLimit is how many blocking findings a refusal names;
// the rest are counted ("and N more"), so a review with dozens of findings
// cannot grow the message without bound.
const PlanReviewBlockingListLimit = 8

// SeverityBlocking is the one finding severity that stops a plan. The others
// a receipt may carry (non_blocking, and the older important and minor) let
// the plan proceed.
const SeverityBlocking = "blocking"

// ReadPlanReview reads a review receipt (result.json `review`) into what the
// planning gate needs. It never fails: what it cannot read is a legacy
// receipt, judged as before.
func ReadPlanReview(raw []byte) PlanReviewSummary {
	var review struct {
		Axes []struct {
			Findings *[]struct {
				ID       string  `json:"id"`
				Severity *string `json:"severity"`
				Summary  string  `json:"summary"`
			} `json:"findings"`
		} `json:"axes"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &review) != nil || len(review.Axes) == 0 {
		return PlanReviewSummary{Legacy: true}
	}
	var out PlanReviewSummary
	for _, a := range review.Axes {
		if a.Findings == nil {
			return PlanReviewSummary{Legacy: true}
		}
		for _, f := range *a.Findings {
			if f.Severity == nil {
				return PlanReviewSummary{Legacy: true}
			}
			if *f.Severity != SeverityBlocking {
				continue
			}
			title := strings.TrimSpace(f.Summary)
			if title == "" {
				title = f.ID
			}
			out.Blocking = append(out.Blocking, title)
		}
	}
	return out
}

// PlanReviewDispatchGate is the planning gate on a dispatch bound to an item
// with --work-id: while a Feature or Epic that must have its plan reviewed is
// still assigned, and its latest review has a blocking finding, no work but a
// new plan review is dispatched for it. Whether a review exists at all stays
// the phase route's question.
func PlanReviewDispatchGate(i ItemV2, kind string, plans []DocumentV2, review PlanReviewSummary) error {
	if kind == DocumentPlanReview || i.Phase != PhaseAssigned || (i.Kind != KindEpic && !i.Kind.FeatureLike()) ||
		!i.HasGateSnapshot() || !i.PlanningGate || (i.Kind.FeatureLike() && !i.ReviewRequired) {
		return nil
	}
	lastPlan, lastReview, reviews := -1, -1, 0
	for n, d := range plans {
		switch d.Role {
		case DocumentPlan:
			lastPlan = n
		case DocumentPlanReview:
			lastReview = n
			reviews++
		}
	}
	rounds, prefix := 1, "feature"
	if i.Kind == KindEpic {
		rounds, prefix = EpicPlanReviewRounds, "epic"
	}
	return planReviewBlocking(i, prefix, lastPlan, lastReview, reviews, rounds, review)
}

// planReviewBlocking refuses when the latest plan review has a blocking
// finding that still stands. An Epic plan revised after that review, with a
// round left, is waiting for its next review instead (the review-required
// rule); otherwise the findings stand until a new review clears them.
func planReviewBlocking(i ItemV2, prefix string, lastPlan, lastReview, reviews, rounds int, review PlanReviewSummary) error {
	if lastReview < 0 || review.Legacy || len(review.Blocking) == 0 {
		return nil
	}
	if i.Kind == KindEpic && lastPlan > lastReview && reviews < rounds {
		return nil
	}
	listed := review.Blocking
	if len(listed) > PlanReviewBlockingListLimit {
		listed = listed[:PlanReviewBlockingListLimit]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The latest plan review has %d blocking finding(s): ", len(review.Blocking))
	for n, t := range listed {
		if n > 0 {
			b.WriteString("; ")
		}
		b.WriteString(strconv.Quote(t))
	}
	if more := len(review.Blocking) - len(listed); more > 0 {
		fmt.Fprintf(&b, "; and %d more", more)
	}
	b.WriteString(". ")
	if i.Kind == KindEpic && reviews >= rounds {
		fmt.Fprintf(&b, "This Epic has used its %d plan reviews, so there are only two ways forward: the person overrides "+
			"the planning gate (%s), or you revise the plan (`clawdline item doc %s --role plan --title \"Plan\" --body-file <file>`) "+
			"and the person raises the review limit for it. Ask the person; do not dispatch a third review on your own.",
			rounds, PlanningGateOverride, i.ID)
	} else {
		fmt.Fprintf(&b, "Revise the plan (`clawdline item doc %s --role plan --title \"Plan\" --body-file <file>`), then have "+
			"it reviewed again (`clawdline dispatch --kind plan_review --work-id %s --claims \"\"`); or the person overrides the "+
			"planning gate (%s).", i.ID, i.ID, PlanningGateOverride)
	}
	return RefuseV2(prefix+"_plan_review_blocking", b.String())
}

// PlanningGateOverride names the override a refusal offers. It is the
// person's, on the Board, and nothing here changes it: a Feature's Needs
// independent review switch, or the captured planning_gate setting, which
// takes effect for an item's next cycle.
const PlanningGateOverride = "only the person can: uncheck the Feature's Needs independent review switch on the Board, " +
	"or `clawdline setting set planning_gate off` before the item's next cycle"

// EpicPlanGate remains as a source-compatible name for callers outside the
// runtime path. New enforcement calls PlanningGate so Features are included.
func EpicPlanGate(i ItemV2, next Phase, plans []DocumentV2, review PlanReviewSummary) error {
	if i.Kind != KindEpic {
		return nil
	}
	if !i.HasGateSnapshot() {
		if i.Cycle == 0 {
			i.Cycle = 1
		}
		i.GateSnapshotCycle, i.PlanningGate = i.Cycle, true
		if strings.TrimSpace(i.AcceptanceCriteria) == "" {
			i.AcceptanceCriteria = "legacy Epic acceptance"
		}
	}
	return PlanningGate(i, next, plans, review)
}

// EpicPlanReviewRounds is how many plan reviews the gate asks of an Epic at
// most: after the second, a revised plan goes into implementing without a
// third.
const EpicPlanReviewRounds = 2

// ActorEpicOwner is the prefix of the actor an Epic's owner Session writes
// under when it creates or assigns the Epic's child items:
// `epic_owner:<session id>`. It is a Session, not a person, and the record
// says so.
const ActorEpicOwner = "epic_owner:"

// EpicOwnerActor is the actor for the owner Session of an Epic.
func EpicOwnerActor(session string) string { return ActorEpicOwner + session }

// EpicChildLimit is how many child items one Epic may hold, open or closed:
// an Epic broken into more pieces than this is two Epics, and a Session
// looping on the create route is stopped here.
const EpicChildLimit = 32

// EpicChildParent is the rule an Epic's owner Session passes before it breaks
// the Epic into a child item or (re)assigns one: the parent is an Epic, still
// open, owned by that Session, and past its plan gate, so the children come
// out of a reviewed plan. The person's assignment of the Epic is the
// authority; no per-message run is needed.
func EpicChildParent(epic ItemV2, session string) error {
	switch {
	case epic.Kind != KindEpic:
		return RefuseV2("parent_not_epic", "Only an Epic's owner may create or assign child items, and this item is not an Epic.")
	case epic.Phase.Terminal():
		return RefuseV2("item_terminal", "That Epic is finished; a person must reopen it before it takes children.")
	case session == "" || epic.OwnerSession != session:
		return RefuseV2("not_epic_owner", "Only the Session the person assigned this Epic to may create or assign its child items.")
	case epic.Phase == PhaseCreated || epic.Phase == PhaseAssigning || epic.Phase == PhaseAssigned:
		return RefuseV2("epic_not_planned",
			"Break an Epic into child items only after its plan is reviewed and it has entered implementing "+
				"(`clawdline guide epic`).")
	}
	return nil
}

// EpicChildKind admits only Feature and Issue as an Epic's children: an Epic
// inside an Epic, or a Planning kind, is not a piece another Session can own
// and finish.
func EpicChildKind(k Kind) error {
	if k != KindFeature && k != KindIssue {
		return RefuseV2("child_kind_not_allowed", "An Epic's child item is a feature or an issue.")
	}
	return nil
}

// EpicDoneGate refuses an Epic's move to done while any of its child items is
// still open: the Epic's owner follows its children to done and integrates
// them, and an Epic closed over open children would say the whole is finished
// when it is not.
func EpicDoneGate(i ItemV2, next Phase, openChildren int64) error {
	if i.Kind != KindEpic || next != PhaseDone || openChildren == 0 {
		return nil
	}
	return RefuseV2("epic_children_open", fmt.Sprintf(
		"%d child item(s) of this Epic are still open; follow them to done or cancelled before closing the Epic.", openChildren))
}

// AgentTransition validates only the Agent-owned execution graph. Assignment,
// cancellation and reopening are person/application operations and never pass
// through this function.
func AgentTransition(i ItemV2, next Phase, hasVerification, hasLanding, hasDeployment, noDeployment bool) error {
	if i.OwnerSession == "" {
		return RefuseV2("item_unassigned", "Only the owning Session can move execution.")
	}
	if i.Phase.Terminal() {
		return RefuseV2("item_terminal", "A person must reopen terminal work.")
	}
	ok := false
	switch i.Phase {
	case PhaseAssigned:
		ok = next == PhaseImplementing
	case PhaseImplementing:
		// An item whose captured verify gate is off has no verification
		// candidate to authorize, so verifying and merging collapse into the
		// one step that carries the landing evidence. Implementing ->
		// verifying stays legal so an item already on the long line, or an
		// Agent that walks it, is not stranded.
		if next == PhaseDeploying && i.VerifyGate {
			return RefuseV2("verification_gate_on", "This item's captured verify gate is on, so it must pass verifying and merging before deploying; see `clawdline guide board`, \"Captured planning and verification gates\".")
		}
		ok = next == PhaseVerifying || next == PhaseDeploying && hasLanding
	case PhaseVerifying:
		ok = next == PhaseImplementing || next == PhaseMerging && hasVerification
	case PhaseMerging:
		ok = next == PhaseImplementing || next == PhaseVerifying || next == PhaseDeploying && hasLanding
	case PhaseDeploying:
		if next == PhaseDone {
			switch i.DeploymentPolicy {
			case DeployRequired:
				ok = hasDeployment
			case DeployNotRequired:
				ok = noDeployment
			case DeployAgentDecides:
				ok = hasDeployment || noDeployment
			}
		}
	}
	if !ok {
		return RefuseV2("invalid_transition", TransitionAdvice(i, next))
	}
	return nil
}

// phaseStep is one move AgentTransition allows out of a phase, and the
// `item phase` flags that move needs; Flags is empty when it needs none.
type phaseStep struct {
	Next  Phase
	Flags string
}

const (
	landingFlags     = "--commit <sha> --target <branch> --remote <remote>"
	landingNeed      = "--commit/--target/--remote or --no-landing-reason"
	verificationFlag = `--verification "<what was run and what it showed>"`
)

// phaseSteps is AgentTransition's switch read the other way: every legal
// next phase from i's phase, with the evidence it needs. A change to one is a
// change to the other; TestTransitionAdviceMatchesAgentTransition holds them
// together.
func phaseSteps(i ItemV2) []phaseStep {
	switch i.Phase {
	case PhaseAssigned:
		return []phaseStep{{Next: PhaseImplementing}}
	case PhaseImplementing:
		out := []phaseStep{{Next: PhaseVerifying}}
		if !i.VerifyGate {
			out = append(out, phaseStep{Next: PhaseDeploying, Flags: landingFlags})
		}
		return out
	case PhaseVerifying:
		return []phaseStep{{Next: PhaseMerging, Flags: verificationFlag}, {Next: PhaseImplementing}}
	case PhaseMerging:
		return []phaseStep{{Next: PhaseDeploying, Flags: landingFlags}, {Next: PhaseVerifying}, {Next: PhaseImplementing}}
	case PhaseDeploying:
		flags := `--deployment "<what was deployed, where>"`
		if i.DeploymentPolicy == DeployNotRequired {
			flags = `--no-deployment-reason "<why nothing deploys>"`
		}
		return []phaseStep{{Next: PhaseDone, Flags: flags}}
	}
	return nil
}

// evidenceNeed names the evidence a step needs in words a caller can match
// against `item phase`'s flags.
func evidenceNeed(i ItemV2, s phaseStep) string {
	switch {
	case s.Flags == landingFlags:
		return landingNeed
	case s.Flags == verificationFlag:
		return "--verification"
	case s.Next == PhaseDone && i.DeploymentPolicy == DeployRequired:
		return "--deployment"
	case s.Next == PhaseDone && i.DeploymentPolicy == DeployNotRequired:
		return "--no-deployment-reason"
	case s.Next == PhaseDone:
		return "--deployment or --no-deployment-reason"
	}
	return ""
}

// TransitionAdvice is the invalid_transition message: why the move was
// refused, the legal next phases from here, and one `item phase` command that
// the daemon would take.
func TransitionAdvice(i ItemV2, next Phase) string {
	steps := phaseSteps(i)
	var legal []string
	pick := -1
	for n, s := range steps {
		word := string(s.Next)
		if need := evidenceNeed(i, s); need != "" {
			word += " (needs " + need + ")"
		}
		legal = append(legal, word)
		if s.Next == next {
			pick = n
		}
	}
	msg := fmt.Sprintf("Cannot move %s to %s", i.Phase, next)
	if pick >= 0 && evidenceNeed(i, steps[pick]) != "" {
		msg += " without " + evidenceNeed(i, steps[pick]) + "."
	} else if pick >= 0 {
		msg += " with the recorded evidence."
	} else {
		msg += fmt.Sprintf(": %s is not a next phase from %s.", next, i.Phase)
	}
	if len(steps) == 0 {
		return msg
	}
	msg += " From " + string(i.Phase) + " the legal next phases are " + strings.Join(legal, ", ") + "."
	if pick < 0 {
		pick = 0
	}
	cmd := "clawdline item phase " + i.ID + " " + string(steps[pick].Next)
	if steps[pick].Flags != "" {
		cmd += " " + steps[pick].Flags
	}
	return msg + " Run: `" + cmd + "`."
}

// NoLandingGate decides whether `--no-landing-reason` may stand in for a
// landing on merging -> deploying: only for work that has no code to land.
// A reason beside a direct landing is two answers to one question. A bound
// task whose landing is still pending is code owed, and one that landed is
// code that is not "none"; each is refused by name. Tasks that settled as
// nothing_to_land or abandoned do not stand in its way.
func NoLandingGate(reason string, directLanding bool, tasks []TaskFacts) error {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	if directLanding {
		return RefuseV2("invalid_landing_evidence",
			"Give either a landing (--commit) or --no-landing-reason, not both.")
	}
	for _, f := range tasks {
		switch f.Landing {
		case "pending":
			return RefuseV2("landing_owed", fmt.Sprintf(
				"Task %s bound to this item still owes its landing; land or abandon it before saying there is no code.", f.Task))
		case "landed", "incorporated":
			return RefuseV2("landing_recorded", fmt.Sprintf(
				"Task %s bound to this item has landed code; --no-landing-reason does not apply.", f.Task))
		}
	}
	return nil
}

func AsRefusalV2(err error) (RefusalV2, bool) {
	var r RefusalV2
	return r, errors.As(err, &r)
}
