package work

import (
	"errors"
	"fmt"
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
// (EpicPlanGate); Refactor and Plan stay in Planning.
func (k Kind) Executable() bool { return k == KindFeature || k == KindIssue || k == KindEpic }

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
	ID               string
	ProjectID        string
	ProjectPath      string
	Kind             Kind
	Title            string
	Description      string
	Phase            Phase
	Condition        Condition
	UserAction       string
	DeploymentPolicy DeploymentPolicy
	OwnerSession     string
	CreatedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ClosedAt         time.Time
	Cycle            int64
	Version          int64
	// CreatedVia is the person's message a Session created this item on
	// (work-system-v2 §2, amended 2026-09-25); nil for an item a person
	// created as themselves.
	CreatedVia *CreatedViaV2
}

// CreatedViaV2 is the provenance of an item a Session created because a
// person told it to through Clawdline: the run of that message, when it was
// said, and a bounded excerpt of what was said.
type CreatedViaV2 struct {
	Run     string `json:"run"`
	Session string `json:"session_id"`
	At      int64  `json:"at"`
	Excerpt string `json:"excerpt,omitempty"`
}

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
	if i.Kind.Executable() && i.Phase != PhaseCreated {
		return RefuseV2("invalid_initial_phase", "Executable work begins in created.")
	}
	if !i.Kind.Executable() && i.Phase != PhaseCreated {
		return RefuseV2("invalid_initial_phase", "Planning work does not enter execution.")
	}
	return nil
}

// Document roles. Plan and plan_review belong to an Epic only: they are the
// record its gate reads, and on any other kind they would be a ceremony
// nothing checks.
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

// DocumentRoleApplies refuses a plan or plan_review on anything but an Epic.
func DocumentRoleApplies(i ItemV2, role string) error {
	if (role == DocumentPlan || role == DocumentPlanReview) && i.Kind != KindEpic {
		return RefuseV2("document_role_not_applicable",
			"Plan and plan_review documents belong to an Epic; use spec or design for this item.")
	}
	return nil
}

// EpicPlanGate is the rule an Epic crosses before it enters implementing: an
// Epic is large, so its plan is written onto the item and a second reader, a
// child review, checks it before code is written. plans is every plan and
// plan_review document on the item, oldest first; a plan written after the
// last review needs a review of its own. Other kinds and other transitions
// pass untouched.
func EpicPlanGate(i ItemV2, next Phase, plans []DocumentV2) error {
	if i.Kind != KindEpic || i.Phase != PhaseAssigned || next != PhaseImplementing {
		return nil
	}
	lastPlan, lastReview := -1, -1
	for n, d := range plans {
		switch d.Role {
		case DocumentPlan:
			lastPlan = n
		case DocumentPlanReview:
			lastReview = n
		}
	}
	switch {
	case lastPlan < 0:
		return RefuseV2("epic_plan_required",
			"Write the Epic's plan onto the item first: `clawdline item doc "+i.ID+" --role plan --title \"Plan\"` with the plan as its body.")
	case lastReview < lastPlan:
		return RefuseV2("epic_plan_review_required",
			"Have a child review the latest plan (`clawdline dispatch --kind plan_review --work-id "+i.ID+
				"`), then record it with `clawdline item doc "+i.ID+" --role plan_review --reference <task id>`.")
	}
	return nil
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
		ok = next == PhaseVerifying
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
		return RefuseV2("invalid_transition", fmt.Sprintf("Cannot move %s to %s with the recorded evidence.", i.Phase, next))
	}
	return nil
}

func AsRefusalV2(err error) (RefusalV2, bool) {
	var r RefusalV2
	return r, errors.As(err, &r)
}
