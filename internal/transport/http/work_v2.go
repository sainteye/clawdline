package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/work"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var workV2ByServer sync.Map // *Server -> *app.WorkSystemV2

func (s *Server) workV2() *app.WorkSystemV2 {
	if v, ok := workV2ByServer.Load(s); ok {
		return v.(*app.WorkSystemV2)
	}
	v := app.NewWorkSystemV2(s.store)
	v.GateSettings = func(context.Context) (app.WorkV2GateSettings, error) {
		values, err := s.settingsFile().Read()
		if err != nil {
			return app.WorkV2GateSettings{}, err
		}
		gates := values.WorkGates()
		return app.WorkV2GateSettings{Planning: gates.Planning, Verify: gates.Verify}, nil
	}
	got, _ := workV2ByServer.LoadOrStore(s, v)
	return got.(*app.WorkSystemV2)
}

type workV2ProjectWire struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Icon      any    `json:"icon"`
	Available bool   `json:"available"`
}

type workV2ItemWire struct {
	ID                 string            `json:"id"`
	Project            workV2ProjectWire `json:"project"`
	Kind               string            `json:"kind"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	AcceptanceCriteria string            `json:"acceptance_criteria"`
	AcceptanceVersion  int64             `json:"acceptance_version"`
	AcceptanceDigest   string            `json:"acceptance_digest"`
	Phase              string            `json:"phase"`
	Condition          *string           `json:"condition"`
	UserAction         string            `json:"user_action"`
	// DecisionID is the open decision a waiting_user points at; absent when
	// the item waits on nothing, or on a user_action the daemon wrote.
	DecisionID       string `json:"decision_id,omitempty"`
	Area             string `json:"area"`
	DeploymentPolicy string `json:"deployment_policy"`
	// ReviewRequired is the person's "Needs independent review" switch; it
	// is false on every item but a Feature the person checked.
	ReviewRequired bool                  `json:"review_required"`
	OwnerSession   *string               `json:"owner_session"`
	CreatedBy      string                `json:"created_by"`
	CreatedVia     *workV2CreatedViaWire `json:"created_via,omitempty"`
	// ParentID is the Epic this item was broken out of by the Epic's owner
	// Session; absent for every other item.
	ParentID string `json:"parent_id,omitempty"`
	// ClaimedVia is the person's message the owning Session claimed the item
	// on, or a Session assigned it to the owning new Session on (Assigned);
	// absent when the person assigned it, or nobody holds it.
	ClaimedVia         *workV2CreatedViaWire `json:"claimed_via,omitempty"`
	CreatedAt          int64                 `json:"created_at"`
	UpdatedAt          int64                 `json:"updated_at"`
	ClosedAt           *int64                `json:"closed_at"`
	PhaseEnteredAt     *int64                `json:"phase_entered_at"`
	DeploymentEvidence string                `json:"deployment_evidence,omitempty"`
	NoDeploymentReason string                `json:"no_deployment_reason,omitempty"`
	// NoLandingReason is why the latest entry into deploying rested on no
	// landing; absent when it rested on one.
	NoLandingReason string `json:"no_landing_reason,omitempty"`
	// Landings is every landing recorded for the item, the rows
	// GET /v1/orchestrator/landings?work_id= answers; on the one-item read.
	Landings          []contract.RecordedLanding `json:"landings,omitempty"`
	Cycle             int64                      `json:"cycle"`
	GateSnapshotCycle int64                      `json:"gate_snapshot_cycle"`
	GateSnapshotAt    *int64                     `json:"gate_snapshot_at"`
	PlanningGate      bool                       `json:"planning_gate"`
	VerifyGate        bool                       `json:"verify_gate"`
	Verification      any                        `json:"verification,omitempty"`
	Version           int64                      `json:"version"`
	Assignments       []workV2AssignmentWire     `json:"assignments,omitempty"`
	Documents         []workV2DocumentWire       `json:"documents,omitempty"`
	Images            []workV2ImageWire          `json:"images,omitempty"`
	Steps             []workV2StepWire           `json:"steps,omitempty"`
	Events            []workV2EventWire          `json:"events,omitempty"`
}

func optionalPositiveUnix(seconds int64) *int64 {
	if seconds <= 0 {
		return nil
	}
	return &seconds
}

// workV2CreatedViaWire is the person's message a Session created an item on:
// its run, when it was said, and the start of what was said. Absent for an
// item a person created as themselves.
type workV2CreatedViaWire struct {
	Run       string `json:"run"`
	SessionID string `json:"session_id"`
	At        int64  `json:"at"`
	Excerpt   string `json:"excerpt,omitempty"`
	// EpicID names the Epic whose owner Session created the item; such an
	// item carries no run, because the person's assignment of the Epic is its
	// authority.
	EpicID string `json:"epic_id,omitempty"`
	// Assigned says the Session named by SessionID did not take the item
	// itself but, on this message, handed it to a new Session opened as
	// Persona (none when empty). Absent on a claim.
	Assigned bool   `json:"assigned,omitempty"`
	Persona  string `json:"persona,omitempty"`
}

// claimedViaWire is the wire of the person's message an assignment was made
// on, as a claim or as a Session's assignment to a new Session.
func claimedViaWire(via *work.CreatedViaV2) *workV2CreatedViaWire {
	return &workV2CreatedViaWire{Run: via.Run, SessionID: via.Session, At: via.At, Excerpt: via.Excerpt,
		Assigned: via.Assigned, Persona: via.Persona}
}

type workV2ImageWire struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	MediaType string `json:"media_type"`
	ByteCount int64  `json:"byte_count"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Position  int64  `json:"position"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

type workV2AssignmentWire struct {
	ID             string `json:"id"`
	Mode           string `json:"mode"`
	SessionID      string `json:"session_id,omitempty"`
	TerminalID     string `json:"terminal_id,omitempty"`
	Assistant      string `json:"assistant,omitempty"`
	Model          string `json:"model,omitempty"`
	State          string `json:"state"`
	RootAssignment string `json:"root_assignment_id,omitempty"`
	Failure        string `json:"failure,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	// ClaimedVia is the person's message a Session claimed the item on;
	// absent for an assignment a person made.
	ClaimedVia *workV2CreatedViaWire `json:"claimed_via,omitempty"`
	// Persona is the built-in persona the new Session was opened as; absent
	// for none and for an existing Session.
	Persona string `json:"persona,omitempty"`
}

type workV2DocumentWire struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Reference string `json:"reference"`
	Position  int64  `json:"position"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"created_at"`
}

type workV2StepWire struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	CreatedBy   string `json:"created_by"`
	CompletedBy string `json:"completed_by"`
	Done        bool   `json:"done"`
	Position    int64  `json:"position"`
	Version     int64  `json:"version"`
	CompletedAt *int64 `json:"completed_at"`
}

type workV2EventWire struct {
	Seq             int64           `json:"seq"`
	Kind            string          `json:"kind"`
	Actor           string          `json:"actor"`
	PreviousVersion int64           `json:"previous_version"`
	NextVersion     int64           `json:"next_version"`
	Payload         json.RawMessage `json:"payload"`
	At              int64           `json:"at"`
}

type workV2LandingRequest struct {
	Commit string `json:"commit"`
	Target string `json:"target"`
	Remote string `json:"remote"`
	// Project names another catalog Project whose repository holds the
	// commit, when the work landed outside the item's own Project.
	Project string `json:"project"`
}

type workV2CandidateRequest struct {
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
	Commit   string `json:"commit"`
}

func deterministicWorkGateID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	// UUID v5/variant bits keep the id inside the broker's existing closed
	// task-id grammar while the digest keeps replay deterministic.
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func verifyWorkV2Candidate(ctx context.Context, g *gitadapter.Git, item app.WorkV2View, sessionID string,
	req *workV2CandidateRequest, now time.Time) (*contract.WorkGateCandidateReceipt, error) {
	if req == nil || strings.TrimSpace(req.Worktree) == "" || strings.TrimSpace(req.Branch) == "" ||
		strings.TrimSpace(req.Commit) == "" {
		return nil, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "verification_candidate_required",
			Message: "Entering verification requires this Session's worktree, branch, and exact HEAD commit, and the " +
				"request carried none. Run `clawdline item phase " + item.Item.ID + " verifying` from the owning " +
				"Session's worktree: it sends all three itself."}
	}
	i := item.Item
	if i.CycleBaseCommit == "" {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_cycle_base_unknown",
			Message: "This verify-on assignment has no captured Project HEAD; reassign it before verification."}
	}
	worktree, err := filepath.Abs(strings.TrimSpace(req.Worktree))
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "verification_worktree_invalid", Message: err.Error()}
	}
	repository, err := g.Toplevel(ctx, worktree)
	if err != nil || filepath.Clean(repository) != filepath.Clean(i.ProjectPath) {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_project_mismatch",
			Message: "The candidate must be a registered worktree of the item's own Project repository."}
	}
	entries, err := g.Worktrees(ctx, i.ProjectPath)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_worktrees_unreadable",
			Message: "The Project's registered worktrees could not be read."}
	}
	var entry *gitadapter.WorktreeEntry
	for n := range entries {
		if filepath.Clean(entries[n].Path) == filepath.Clean(worktree) {
			entry = &entries[n]
			break
		}
	}
	branch := strings.TrimPrefix(strings.TrimSpace(req.Branch), "refs/heads/")
	if entry == nil || entry.Detached || entry.Branch != "refs/heads/"+branch {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_worktree_unregistered",
			Message: "The candidate path must be a registered, branch-attached worktree matching the submitted branch."}
	}
	commit, err := g.ResolveCommit(ctx, worktree, strings.TrimSpace(req.Commit))
	if err != nil || entry.Head != commit {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_head_moved",
			Message: "The registered worktree branch HEAD no longer equals the requested candidate commit."}
	}
	head, err := g.ResolveCommit(ctx, worktree, "HEAD")
	if err != nil || head != commit {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_head_moved",
			Message: "The worktree HEAD no longer equals the requested candidate commit."}
	}
	status, err := g.Changes(ctx, worktree)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_status_unreadable",
			Message: "The candidate worktree status could not be read."}
	}
	untracked := int64(0)
	for _, file := range status.Files {
		if file.Kind == gitadapter.KindUntracked {
			untracked++
			continue
		}
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_tracked_dirty",
			Message: "Commit every tracked candidate change before requesting independent verification."}
	}
	strict, err := g.IsAncestor(ctx, i.ProjectPath, i.CycleBaseCommit, commit)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_ancestry_unreadable",
			Message: "The candidate's ancestry from the captured cycle base could not be proved."}
	}
	if !strict || commit == i.CycleBaseCommit {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_candidate_not_descendant",
			Message: "The candidate must be a strict descendant of this cycle's captured base commit."}
	}
	tree, err := g.Tree(ctx, i.ProjectPath, commit)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "verification_tree_unreadable",
			Message: "The candidate's Git tree could not be resolved."}
	}
	active := workV2ActiveOwner(item)
	if active.ID == "" || active.SessionID != sessionID {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "not_item_owner",
			Message: app.NotItemOwnerMessage("register a verification candidate for", i.ID)}
	}
	return &contract.WorkGateCandidateReceipt{Repository: filepath.Clean(i.ProjectPath), Worktree: filepath.Clean(worktree),
		Branch: branch, Commit: commit, Tree: tree, AssignmentID: active.ID, OwnerSessionID: sessionID,
		Cycle: i.Cycle, CriteriaVersion: i.AcceptanceVersion, CriteriaDigest: i.AcceptanceDigest,
		UntrackedFiles: untracked, CreatedAt: now.Unix()}, nil
}

type workV2GitReader interface {
	ValidBranchName(context.Context, string) bool
	ResolveCommit(context.Context, string, string) (string, error)
	IsAncestor(context.Context, string, string, string) (bool, error)
}

// workV2PhaseInstruction names the command that moves the phase, in every
// brief an owner receives: a Session told only to "implement, verify, merge
// and deploy" finished the work and left its item in assigned, because nothing
// it read named the route (2026-09-26).
type workV2ProjectCatalog map[string]workV2ProjectWire

// workV2Projects takes the live Project reading once. Callers prepare this
// snapshot before opening a work transaction, then response projection is a
// pure mapping over the committed view and this immutable catalog.
func (s *Server) workV2Projects(ctx context.Context) workV2ProjectCatalog {
	out := workV2ProjectCatalog{}
	for _, p := range s.projectReaders().places.List(s.liveDirectories(ctx), 40) {
		canonical, ok := projects.CanonicalProjectKey(p.Path)
		if !ok {
			continue
		}
		label := p.Label
		if label == "" {
			label = filepath.Base(canonical)
		}
		mark := icon.Generated(canonical)
		if s.icons != nil {
			mark = s.icons.For(canonical)
		}
		out[p.ID] = workV2ProjectWire{ID: p.ID, Label: label, Path: canonical,
			Icon: wireIcon(mark), Available: true}
	}
	return out
}

func (s *Server) workV2Project(ctx context.Context, id string) (workV2ProjectWire, bool) {
	p, ok := s.workV2Projects(ctx)[id]
	return p, ok
}

func (s *Server) workV2ItemProjector(ctx context.Context) func(app.WorkV2View) workV2ItemWire {
	catalog := s.workV2Projects(ctx)
	return func(v app.WorkV2View) workV2ItemWire { return s.workV2ItemOf(catalog, v) }
}

func (s *Server) workV2ItemOf(catalog workV2ProjectCatalog, v app.WorkV2View) workV2ItemWire {
	i := v.Item
	p, ok := catalog[i.ProjectID]
	if !ok {
		label := filepath.Base(i.ProjectPath)
		p = workV2ProjectWire{ID: i.ProjectID, Label: label, Path: i.ProjectPath,
			Icon: wireIcon(icon.Generated(i.ProjectPath))}
	}
	out := workV2ItemWire{ID: i.ID, Project: p, Kind: string(i.Kind), Title: i.Title, Description: i.Description,
		AcceptanceCriteria: i.AcceptanceCriteria, AcceptanceVersion: i.AcceptanceVersion, AcceptanceDigest: i.AcceptanceDigest,
		Phase: string(i.Phase), Condition: optionalString(string(i.Condition)), UserAction: i.UserAction, DecisionID: i.DecisionID, Area: i.Area(),
		DeploymentPolicy: string(i.DeploymentPolicy), ReviewRequired: i.ReviewRequired, OwnerSession: optionalString(i.OwnerSession),
		CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt.Unix(), UpdatedAt: i.UpdatedAt.Unix(),
		ClosedAt: optionalUnix(i.ClosedAt), Cycle: i.Cycle, GateSnapshotCycle: i.GateSnapshotCycle,
		PhaseEnteredAt:     optionalPositiveUnix(v.CardProgress.PhaseEnteredAt),
		DeploymentEvidence: v.CardProgress.DeploymentEvidence, NoDeploymentReason: v.CardProgress.NoDeploymentReason,
		NoLandingReason: v.CardProgress.NoLandingReason, Landings: recordedLandingsWire(v.Landings),
		GateSnapshotAt: optionalUnix(i.GateSnapshotAt), PlanningGate: i.PlanningGate, VerifyGate: i.VerifyGate,
		Version: i.Version, ParentID: i.ParentID}
	if v.Gate != nil {
		out.Verification = v.Gate
	} else if v.GateCompact != nil {
		out.Verification = v.GateCompact
	}
	if via := i.CreatedVia; via != nil && strings.HasPrefix(i.CreatedBy, work.ActorViaSession) {
		out.CreatedVia = &workV2CreatedViaWire{Run: via.Run, SessionID: via.Session, At: via.At, Excerpt: via.Excerpt}
	} else if via != nil && via.Epic != "" && strings.HasPrefix(i.CreatedBy, work.ActorEpicOwner) {
		out.CreatedVia = &workV2CreatedViaWire{SessionID: via.Session, At: via.At, EpicID: via.Epic}
	}
	claim := v.Claim
	for _, a := range v.Assignments {
		if a.State == "active" && a.ClaimedVia != nil {
			claim = a.ClaimedVia
		}
	}
	if claim != nil {
		out.ClaimedVia = claimedViaWire(claim)
	}
	for _, a := range v.Assignments {
		out.Assignments = append(out.Assignments, workV2AssignmentWire{ID: a.ID, Mode: a.Mode, SessionID: a.SessionID,
			TerminalID: a.TerminalID, Assistant: a.Assistant, Model: a.Model, State: a.State,
			RootAssignment: a.RootAssignment, Failure: a.Failure, CreatedAt: a.CreatedAt.Unix(), UpdatedAt: a.UpdatedAt.Unix(),
			Persona: a.Persona})
		if via := a.ClaimedVia; via != nil && strings.HasPrefix(a.HumanActor, work.ActorViaSession) {
			out.Assignments[len(out.Assignments)-1].ClaimedVia = claimedViaWire(via)
		}
	}
	for _, d := range v.Documents {
		out.Documents = append(out.Documents, workV2DocumentWire{ID: d.ID, Role: d.Role, Title: d.Title, Body: d.Body,
			Reference: d.Reference, Position: d.Position, Version: d.Version, CreatedAt: d.CreatedAt.Unix()})
	}
	for _, image := range v.Images {
		out.Images = append(out.Images, workV2ImageWire{ID: image.ID, Title: image.Title, MediaType: image.MediaType,
			ByteCount: image.ByteCount, Width: image.Width, Height: image.Height, Position: image.Position,
			CreatedBy: image.CreatedBy, CreatedAt: image.CreatedAt.Unix()})
	}
	for _, st := range v.Steps {
		out.Steps = append(out.Steps, workV2StepWire{ID: st.ID, Title: st.Title, Done: st.Done, Position: st.Position,
			CreatedBy: st.CreatedBy, CompletedBy: st.CompletedBy, CompletedAt: optionalUnix(st.CompletedAt), Version: st.Version})
	}
	for _, e := range v.Events {
		out.Events = append(out.Events, workV2EventWire{Seq: e.Seq, Kind: e.Kind, Actor: e.Actor,
			PreviousVersion: e.PreviousVersion, NextVersion: e.NextVersion, Payload: json.RawMessage(e.Payload), At: e.At.Unix()})
	}
	return out
}

func (s *Server) writeWorkV2Error(w http.ResponseWriter, err error) {
	var we *app.WorkError
	if errors.As(err, &we) {
		writeRefusal(w, we.Status, we.Code, we.Message)
		return
	}
	writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The work system could not complete that operation.")
}

func requirePersonWorkV2(w http.ResponseWriter, r *http.Request) (string, bool) {
	if machineAuthed(r) {
		writeRefusal(w, http.StatusForbidden, "session_cannot_create_item", "A Session cannot perform a person's work-system action.")
		return "", false
	}
	if !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This needs a device that may send.")
		return "", false
	}
	return personPrincipal(r), true
}

// workV2Route is mounted below /v1/work/v2/ while v1 remains compiled for the
// explicit reset. The Console reads only this surface after cutover.
func (s *Server) workV2Route(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(routePath(r), "/v1/work/v2/"), "/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "items":
		if r.Method == http.MethodGet {
			s.workV2List(w, r)
		} else if r.Method == http.MethodPost {
			s.workV2Create(w, r)
		} else {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Items are read with GET and created with POST.")
		}
	case len(parts) == 2 && parts[0] == "images" && workID(parts[1]):
		s.workV2ReferenceImage(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "items" && workID(parts[1]):
		if r.Method == http.MethodGet {
			s.workV2Item(w, r, parts[1])
		} else if r.Method == http.MethodPatch {
			s.workV2Edit(w, r, parts[1], true)
		} else {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "An item is read with GET and edited with PATCH.")
		}
	case len(parts) == 3 && parts[0] == "items" && workID(parts[1]):
		if parts[2] == "gate-export" {
			s.workV2GateExport(w, r, parts[1])
		} else if parts[2] == "gate-purge" {
			s.workV2GatePurge(w, r, parts[1])
		} else if parts[2] == "gate-decision" {
			s.workV2GateDecision(w, r, parts[1], true)
		} else if parts[2] == "images" {
			s.workV2AddImage(w, r, parts[1])
		} else if parts[2] == "seen" {
			s.workV2Seen(w, r, parts[1])
		} else if parts[2] == "persona-suggestion" {
			s.workV2PersonaSuggestion(w, r, parts[1])
		} else {
			s.workV2PersonAction(w, r, parts[1], parts[2])
		}
	case len(parts) == 4 && parts[0] == "items" && workID(parts[1]) && parts[2] == "images" && workID(parts[3]):
		s.workV2DeleteImage(w, r, parts[1], parts[3])
	case len(parts) >= 2 && parts[0] == "session-todos":
		s.workV2SessionTodos(w, r, parts[1:])
	case len(parts) >= 2 && parts[0] == "human-interventions":
		s.workV2HumanInterventions(w, r, parts[1:])
	case len(parts) >= 2 && parts[0] == "agent":
		s.workV2Agent(w, r, parts[1:])
	case rest == "proposals":
		s.workV2Proposals(w, r)
	case len(parts) == 3 && parts[0] == "proposals":
		s.workV2ResolveProposal(w, r, parts[1], parts[2])
	case rest == "reset":
		s.workV2Reset(w, r)
	default:
		writeNoSuchRoute(w, r)
	}
}

// workV2Seen is the person's receipt for opening an item's detail: what a
// Session card shows as waiting for acceptance stops waiting on every device.
// The phase is the one the person was shown; a view that answers after the
// item moved on marks nothing. It is idempotent by nature, so it takes no
// Idempotency-Key, and a repeat is `marked: false`, not a refusal.
func (s *Server) workV2Seen(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "An item is marked seen with POST.")
		return
	}
	if _, ok := requirePersonWorkV2(w, r); !ok {
		return
	}
	var body struct {
		Phase string `json:"phase"`
	}
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	// Only the phases a Session card can show as awaiting acceptance take a receipt.
	if phase := work.Phase(body.Phase); phase != work.PhaseDeploying && phase != work.PhaseDone {
		writeRefusal(w, http.StatusBadRequest, "invalid_phase", "Only an item shown in deploying or done is marked seen.")
		return
	}
	marked, err := s.store.MarkWorkV2PhaseSeen(r.Context(), id, work.Phase(body.Phase), time.Now())
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "marked": marked})
}

func (s *Server) workV2GateExport(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Verification detail is exported with GET.")
		return
	}
	if _, ok := requirePersonWorkV2(w, r); !ok {
		return
	}
	export, err := s.store.ExportWorkGateDetails(r.Context(), id)
	if err != nil {
		s.writeWorkV2Error(w, workV2GateStoreError(err))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "manifest": map[string]any{"item_id": export.ItemID,
		"item_version": export.ItemVersion, "sha256": export.SHA256, "byte_count": export.ByteCount,
		"round_count": export.RoundCount}, "document": string(export.Document)})
}

func (s *Server) workV2GatePurge(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Exported verification detail is purged with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		SHA256          string `json:"sha256"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	purged, err := s.store.PurgeWorkGateDetails(r.Context(), id, body.ExpectedVersion, body.SHA256,
		time.Now().UTC().Truncate(time.Second))
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, workV2GateStoreError(err))
		return
	}
	answer, _ := json.Marshal(map[string]any{"ok": true, "purged_rounds": purged, "sha256": body.SHA256})
	if err := s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}); err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

func workV2GateStoreError(err error) error {
	switch {
	case errors.Is(err, store.ErrConflict):
		return &app.WorkError{Status: http.StatusConflict, Code: "version_or_manifest_conflict",
			Message: "The item or eligible verification export changed; export it again before purging."}
	case errors.Is(err, store.ErrNoWorkV2):
		return &app.WorkError{Status: http.StatusNotFound, Code: "item_not_found", Message: "No such work item."}
	default:
		return err
	}
}

func (s *Server) workV2GateDecision(w http.ResponseWriter, r *http.Request, id string, person bool) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A verification decision is made with POST.")
		return
	}
	actor := ""
	if person {
		var ok bool
		actor, ok = requirePersonWorkV2(w, r)
		if !ok {
			return
		}
	}
	var body contract.WorkGateDecisionRequest
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if !person {
		actor = body.SessionID
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	view, err := s.workV2().DecideWorkGate(r.Context(), id, body, person)
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	answer := workV2Answer(s.workV2ItemProjector(r.Context())(view))
	if err := s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}); err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// referenceImageFileRefusal names a reference image whose row is there and
// whose file is not, or is not the bytes the row recorded. It is 410: the
// picture was stored and cannot be served again, and asking again will not
// bring it back — not a 500, and never an empty picture.
func proposalV2Of(p work.ProposalV2) map[string]any {
	return map[string]any{"id": p.ID, "project_id": p.ProjectID, "project_path": p.ProjectPath, "kind": p.Kind,
		"title": p.Title, "description": p.Description, "reason": p.Reason, "suggested_acceptance": p.SuggestedAcceptance,
		"session_id": p.SessionID, "source_work_id": p.SourceWorkID, "source_todo_id": p.SourceTodoID, "state": p.State,
		"accepted_work_id": p.AcceptedWorkID, "created_at": p.CreatedAt.Unix(), "resolved_at": optionalUnix(p.ResolvedAt), "version": p.Version}
}

func (s *Server) workV2Proposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Proposals are read with GET.")
		return
	}
	rows, truncated, err := s.workV2().Proposals(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, proposalV2Of(p))
	}
	writeJSON(w, map[string]any{"ok": true, "rows": out, "truncated": truncated})
}

func (s *Server) workV2ResolveProposal(w http.ResponseWriter, r *http.Request, id, decision string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A proposal is resolved with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		Title               string `json:"title"`
		Description         string `json:"description"`
		SuggestedAcceptance string `json:"suggested_acceptance"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	v, err := s.workV2().ResolveProposal(r.Context(), id, actor, app.ProposalDecisionV2{Decision: decision,
		Title: body.Title, Description: body.Description, SuggestedAcceptance: body.SuggestedAcceptance}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(itemOf(v))
		if decision == "reject" {
			answer, _ = json.Marshal(map[string]any{"ok": true, "rejected": id})
		}
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	})
	_ = v
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	if decision == "reject" && len(answer) == 0 {
		answer, _ = json.Marshal(map[string]any{"ok": true, "rejected": id})
		_ = s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

func (s *Server) workV2List(w http.ResponseWriter, r *http.Request) {
	q, ok := workQuery(w, r, "project", "owner", "terminal", "status", "q", "cursor")
	if !ok {
		return
	}
	status := q["status"]
	if status == "" {
		status = "open"
		if q["terminal"] == "true" {
			status = "all"
		}
	}
	// Owner/terminal reads are Session projections and keep the complete internal
	// page. The Console's Board read is the cursor-bounded public projection.
	if q["owner"] != "" || q["terminal"] == "true" {
		s.workV2ListProjection(w, r, q, status)
		return
	}
	page, err := s.workV2().ListPage(r.Context(), q["project"], status, q["q"], q["cursor"])
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	out := struct {
		OK        bool             `json:"ok"`
		Rows      []workV2ItemWire `json:"rows"`
		Counts    map[string]int   `json:"counts"`
		Next      *string          `json:"next_cursor"`
		PageSize  int              `json:"page_size"`
		Truncated bool             `json:"truncated"`
	}{OK: true, Rows: []workV2ItemWire{}, Counts: map[string]int{}, Next: optionalString(page.Next),
		PageSize: app.WorkV2ListPageLimit, Truncated: page.Next != ""}
	var itemOf func(app.WorkV2View) workV2ItemWire
	if len(page.Rows) > 0 {
		itemOf = s.workV2ItemProjector(r.Context())
	}
	for _, row := range page.Rows {
		wire := itemOf(row)
		out.Rows = append(out.Rows, wire)
		out.Counts[wire.Area]++
	}
	writeJSON(w, out)
}

func (s *Server) workV2ListProjection(w http.ResponseWriter, r *http.Request, q map[string]string, status string) {
	rows, truncated, err := s.workV2().List(r.Context(), q["project"], q["owner"], status, q["q"])
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	out := struct {
		OK        bool             `json:"ok"`
		Rows      []workV2ItemWire `json:"rows"`
		Counts    map[string]int   `json:"counts"`
		Truncated bool             `json:"truncated"`
	}{OK: true, Rows: []workV2ItemWire{}, Counts: map[string]int{}, Truncated: truncated}
	var itemOf func(app.WorkV2View) workV2ItemWire
	if len(rows) > 0 {
		itemOf = s.workV2ItemProjector(r.Context())
	}
	for _, row := range rows {
		wire := itemOf(row)
		out.Rows = append(out.Rows, wire)
		out.Counts[wire.Area]++
	}
	writeJSON(w, out)
}

func (s *Server) workV2Item(w http.ResponseWriter, r *http.Request, id string) {
	v, err := s.workV2().Item(r.Context(), id)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "item": s.workV2ItemProjector(r.Context())(v)})
}

const (
	workV2BodyLimit      = 96 << 10
	workV2ImageBodyLimit = 18 << 20
	workV2ImageByteLimit = 5 << 20
)

func readWorkV2Body(w http.ResponseWriter, r *http.Request, into any) ([]byte, bool) {
	return readWorkV2BodyAtMost(w, r, into, workV2BodyLimit, "A work-system request is at most 96 KiB.")
}

func readWorkV2BodyAtMost(w http.ResponseWriter, r *http.Request, into any, limit int64, message string) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", message)
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		message := "The body is not a valid work-system request."
		// encoding/json spells a field DisallowUnknownFields refused as
		// `json: unknown field "x"`; naming it tells the caller what to drop
		// instead of leaving it to guess at the whole body.
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			message = "The body is not a valid work-system request: this route does not take the field " + field + "."
		}
		writeRefusal(w, http.StatusBadRequest, "invalid_request", message)
		return nil, false
	}
	// Every version the store issues is positive. A negative one is refused
	// here, so no body can name app.AnyVersion and skip the comparison an
	// omitted version on an Agent route deliberately skips.
	var version struct {
		ExpectedVersion *int64 `json:"expected_version"`
	}
	if json.Unmarshal(raw, &version) == nil && version.ExpectedVersion != nil && *version.ExpectedVersion < 0 {
		writeRefusal(w, http.StatusBadRequest, "invalid_request",
			"The body is not a valid work-system request: expected_version is a positive version, or omitted on an Agent route.")
		return nil, false
	}
	return raw, true
}

func writeReceiptReplay(w http.ResponseWriter, a *store.ReceiptAnswer) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(a.Status)
	_, _ = w.Write(a.Body)
}

func (s *Server) beginWorkV2Write(w http.ResponseWriter, r *http.Request, actor string, raw []byte) (store.ReceiptKey, bool) {
	k, ok := workKey(w, r, actor)
	if !ok {
		return store.ReceiptKey{}, false
	}
	replay, proceed := s.claimOnce(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		store.ReceiptPolicy{}, "idempotency_key_reused")
	if replay != nil {
		writeReceiptReplay(w, replay)
		return store.ReceiptKey{}, false
	}
	return k, proceed
}

func workV2Answer(v workV2ItemWire) []byte {
	b, _ := json.Marshal(map[string]any{"ok": true, "item": v})
	return b
}

func workV2CompletionEffect(v app.WorkV2View, session, language string) store.Effect {
	terminal := ""
	for _, assignment := range v.Assignments {
		if assignment.State == "active" && assignment.SessionID == session {
			terminal = assignment.TerminalID
			break
		}
	}
	title, body := "Board item completed", v.Item.Title+" is complete."
	if strings.HasPrefix(strings.ToLower(language), "zh") {
		title, body = "看板項目已完成", "「"+v.Item.Title+"」已完成"
	}
	payload, _ := json.Marshal(orchestrator.WorkItemCompletedPush{WorkID: v.Item.ID, Terminal: terminal,
		Title: title, Body: body, Tag: "work-item-" + v.Item.ID})
	return store.Effect{Kind: orchestrator.EffectWorkItemCompletedPush, Subject: v.Item.ID, Payload: payload}
}

func (s *Server) workV2Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ProjectID          string `json:"project_id"`
		Kind               string `json:"kind"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		DeploymentPolicy   string `json:"deployment_policy"`
		ReviewRequired     bool   `json:"review_required"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	catalog := s.workV2Projects(r.Context())
	p, ok := catalog[body.ProjectID]
	if !ok {
		writeRefusal(w, http.StatusUnprocessableEntity, "project_not_found", "Choose a Project from the current Project catalog.")
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	var answer []byte
	v, err := s.workV2().Create(r.Context(), app.NewWorkV2{ProjectID: p.ID, ProjectPath: p.Path, Kind: work.Kind(body.Kind),
		Title: body.Title, Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		DeploymentPolicy: work.DeploymentPolicy(body.DeploymentPolicy), ReviewRequired: body.ReviewRequired, Actor: actor},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(catalog, v))
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	_ = v
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

func (s *Server) workV2Edit(w http.ResponseWriter, r *http.Request, id string, person bool) {
	actor := ""
	if person {
		var ok bool
		actor, ok = requirePersonWorkV2(w, r)
		if !ok {
			return
		}
	} else if !machineAuthed(r) {
		writeRefusal(w, http.StatusUnauthorized, "machine_required", "Only an authenticated Session may use this route.")
		return
	}
	var body struct {
		ExpectedVersion    int64   `json:"expected_version"`
		Title              *string `json:"title"`
		Description        *string `json:"description"`
		AcceptanceCriteria *string `json:"acceptance_criteria"`
		Condition          *string `json:"condition"`
		UserAction         *string `json:"user_action"`
		DecisionID         *string `json:"decision_id"`
		// ReviewRequired is the person's alone; the app refuses it from a
		// Session by name rather than as an unknown field.
		ReviewRequired *bool  `json:"review_required"`
		SessionID      string `json:"session_id"`
		// Phase is read only to refuse it by name: a Session that reaches
		// for the edit route to close its item is told where the phase moves.
		Phase json.RawMessage `json:"phase"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if body.Phase != nil {
		message := "An item's phase is not edited; it moves through the item's own actions."
		if !person {
			message = "An item's phase is not edited; the owning Session moves it with POST /v1/work/v2/agent/items/" +
				id + "/phase (clawdline item phase)."
		}
		writeRefusal(w, http.StatusBadRequest, "phase_not_editable", message)
		return
	}
	if !person {
		actor = body.SessionID
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var condition *work.Condition
	if body.Condition != nil {
		v := work.Condition(*body.Condition)
		condition = &v
	}
	expected := body.ExpectedVersion
	if !person {
		expected = app.AgentExpectedVersion(expected)
	}
	var answer []byte
	_, err := s.workV2().Edit(r.Context(), id, app.EditWorkV2{ExpectedVersion: expected,
		Title: body.Title, Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		Condition: condition, UserAction: body.UserAction, DecisionID: body.DecisionID, ReviewRequired: body.ReviewRequired, Actor: actor,
		OwnerSession: body.SessionID, Person: person}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(itemOf(v))
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// agentReviseAcceptance relays one explicit, recent instruction to the live
// owning Root. The app rechecks ownership, freshness against the current
// contract, item version, and instruction intent inside the item transaction.
func (s *Server) agentReviseAcceptance(w http.ResponseWriter, r *http.Request, id string) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusUnauthorized, "machine_required", "Only an authenticated Session may use this route.")
		return
	}
	var body struct {
		ExpectedVersion    int64  `json:"expected_version"`
		SessionID          string `json:"session_id"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		Via                struct {
			Run string `json:"run"`
		} `json:"via"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := s.relayRefuser(w, r, k)
	run, sess, err := s.relaySession(r.Context(), body.Via.Run, body.SessionID,
		"A revision needs the run of the person's explicit message to this Session.", "nothing was revised")
	if err != nil {
		refuse(err)
		return
	}
	criteria := body.AcceptanceCriteria
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	_, err = s.workV2().Edit(r.Context(), id, app.EditWorkV2{ExpectedVersion: body.ExpectedVersion,
		AcceptanceCriteria: &criteria, OwnerSession: sess.ConversationID, Actor: sess.ConversationID,
		RevisionRun: run}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer, _ = json.Marshal(map[string]any{"ok": true, "item": itemOf(v),
			"acceptance_source": map[string]any{"run": run.ID, "session_id": run.Session,
				"at": run.At.Unix(), "excerpt": run.Excerpt}})
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	})
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

func newWorkV2UUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (s *Server) workV2PersonAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "This action is made with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64     `json:"expected_version"`
		Mode            string    `json:"mode"`
		TerminalID      string    `json:"terminal_id"`
		Assistant       string    `json:"assistant"`
		Model           string    `json:"model"`
		Persona         string    `json:"persona"`
		Kind            work.Kind `json:"kind"`
		Reason          string    `json:"reason"`
		Note            string    `json:"note"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	file := func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(itemOf(v))
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	}
	var out app.WorkV2View
	var err error
	var convertingOwner work.AssignmentV2
	if action == "convert" {
		before, readErr := s.workV2().Item(r.Context(), id)
		if readErr != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, readErr)
			return
		}
		convertingOwner = workV2ActiveOwner(before)
	}
	switch action {
	case "assign":
		out, err = s.assignWorkV2By(r.Context(), id, actor, "", body.ExpectedVersion, body.Mode, body.TerminalID,
			body.Assistant, body.Model, body.Persona, file)
	case "remind":
		out, err = s.remindWorkV2(r.Context(), id, body.ExpectedVersion)
		if err == nil {
			answer = workV2Answer(itemOf(out))
			err = s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer})
		}
	case "unassign":
		out, err = s.workV2().Unassign(r.Context(), id, body.ExpectedVersion, actor, file)
	case "convert":
		out, err = s.workV2().ConvertKind(r.Context(), id, app.ConvertKindV2{
			ExpectedVersion: body.ExpectedVersion, Kind: body.Kind, Actor: actor,
		}, file)
	case "cancel":
		out, err = s.workV2().Cancel(r.Context(), id, body.ExpectedVersion, actor, body.Reason, file)
	case "complete":
		out, err = s.workV2().Complete(r.Context(), id, body.ExpectedVersion, actor, body.Note, file)
	case "reopen":
		out, err = s.workV2().Reopen(r.Context(), id, body.ExpectedVersion, actor, file)
	default:
		err = &app.WorkError{Status: http.StatusNotFound, Code: "action_not_found", Message: "No such person-owned item action."}
	}
	_ = out
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	if action == "convert" && convertingOwner.ID != "" {
		s.tellFormerOwner(r.Context(), convertingOwner, fmt.Sprintf("Clawdline converted Board item %s to another kind and removed its assignment from this Session. It is no longer yours: stop work on it and do not change its phase, steps or documents.", id))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// remindWorkV2 retypes one assigned item to its current owning Session. Unlike
// the assignment courtesy brief, this is an explicit person action, so it may
// interrupt a working Session. The assignment's terminal and conversation
// must still agree before any bytes are written: a recycled terminal must not
// receive work that belonged to the Session that used to occupy it.
func (s *Server) remindWorkV2(ctx context.Context, id string, expected int64) (app.WorkV2View, error) {
	item, err := s.workV2().Item(ctx, id)
	if err != nil {
		return app.WorkV2View{}, err
	}
	if item.Item.Version != expected {
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "version_conflict", Message: "The item changed; reread it before writing."}
	}
	if item.Item.Phase.Terminal() {
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "item_terminal", Message: "A completed or cancelled item has no active Session to remind."}
	}
	var active *work.AssignmentV2
	for n := range item.Assignments {
		assignment := &item.Assignments[n]
		if assignment.State == "active" && assignment.SessionID == item.Item.OwnerSession {
			active = assignment
			break
		}
	}
	if item.Item.OwnerSession == "" || active == nil || active.TerminalID == "" {
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "item_unassigned", Message: "Assign this item to a Session before sending a reminder."}
	}
	sess, findErr := s.actions().Find(ctx, active.TerminalID)
	if findErr != nil {
		return app.WorkV2View{}, workV2ReminderError(findErr)
	}
	if sess.ConversationID != active.SessionID {
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "assignment_session_changed", Message: "The assigned terminal now belongs to another Session; reassign the item before reminding it."}
	}
	brief := fmt.Sprintf("Clawdline reminder for Board item %s: %s. This item is still assigned to this Session. Read its latest description, reference images, documents, and steps, then continue it through implementation, verification, Merge, and its deployment policy.", item.Item.ID, item.Item.Title)
	if _, sendErr := s.actions().Send(ctx, active.TerminalID, brief); sendErr != nil {
		return app.WorkV2View{}, workV2ReminderError(sendErr)
	}
	return item, nil
}

func workV2ReminderError(err error) error {
	var refusal app.Refusal
	if !errors.As(err, &refusal) {
		return &app.WorkError{Status: http.StatusBadGateway, Code: "reminder_failed", Message: err.Error()}
	}
	status := http.StatusBadGateway
	switch refusal.Code {
	case "session_not_found":
		status = http.StatusConflict
	case "session_unknown":
		status = http.StatusServiceUnavailable
	case "busy":
		status = http.StatusTooManyRequests
	case "backend_unsupported":
		status = http.StatusNotImplemented
	}
	return &app.WorkError{Status: status, Code: refusal.Code, Message: refusal.Detail}
}

func (s *Server) assignWorkV2(ctx context.Context, id, actor string, expected int64, mode, terminalID, assistant, model string,
	file app.WorkV2Filer) (app.WorkV2View, error) {
	return s.assignWorkV2By(ctx, id, actor, "", expected, mode, terminalID, assistant, model, "", file)
}

// checkWorkV2Persona refuses a persona an assignment cannot carry: one on an
// existing Session, whose system prompt was fixed when it opened, and a name
// the catalog does not have. An empty persona is none, which every mode
// accepts. A mode that is neither choice is left to the refusal that names
// the two choices.
func checkWorkV2Persona(mode, id string) error {
	if id == "" {
		return nil
	}
	if mode == "existing_session" {
		return &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "persona_not_applicable",
			Message: "A persona is chosen when a new Session opens; an existing Session keeps the one it was opened with. Nothing was assigned."}
	}
	_, builtin := persona.Known(id)
	if mode == "new_session" && !builtin && !squad.ValidCustomID(id) {
		return &app.WorkError{Status: http.StatusBadRequest, Code: "unknown_persona",
			Message: "persona must be one of: " + strings.Join(persona.IDs(), ", ") + ". Nothing was assigned."}
	}
	return nil
}

// workV2ParentOf is the Epic an item was broken out of, when it has one and
// it can be read.
func (s *Server) workV2ParentOf(ctx context.Context, item work.ItemV2) (work.ItemV2, bool) {
	if item.ParentID == "" {
		return work.ItemV2{}, false
	}
	parent, err := s.workV2().Item(ctx, item.ParentID)
	return parent.Item, err == nil
}

// assignWorkV2By is assignWorkV2 made by the owner Session of the item's
// parent Epic when epicOwner names it (the owner check is inside the
// assignment's transaction), and by a person when it is empty.
func (s *Server) assignWorkV2By(ctx context.Context, id, actor, epicOwner string, expected int64,
	mode, terminalID, assistant, model, personaID string, file app.WorkV2Filer) (app.WorkV2View, error) {
	return s.assignWorkV2Via(ctx, id, actor, epicOwner, expected, mode, terminalID, assistant, model, personaID, nil, file)
}

// assignWorkV2Via is assignWorkV2By on the person's message when claim names
// it (app.RunAssignment): a Session handing an ordinary item to a new
// Session. Its checks run inside the assignment's transaction.
func (s *Server) assignWorkV2Via(ctx context.Context, id, actor, epicOwner string, expected int64,
	mode, terminalID, assistant, model, personaID string, claim *work.CreatedViaV2, file app.WorkV2Filer) (app.WorkV2View, error) {
	if err := checkWorkV2Persona(mode, personaID); err != nil {
		return app.WorkV2View{}, err
	}
	if mode == "existing_session" {
		sess, err := s.actions().Find(ctx, terminalID)
		if err != nil {
			return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "session_unavailable", Message: err.Error()}
		}
		item, err := s.workV2().Item(ctx, id)
		if err != nil {
			return app.WorkV2View{}, err
		}
		if !sessionInProject(sess, item.Item) {
			return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "project_mismatch", Message: "The chosen Session is not working in this item's Project."}
		}
		briefItem, err := s.workV2().PreviewAssignment(ctx, item.Item)
		if err != nil {
			return app.WorkV2View{}, err
		}
		cycleBase := briefItem.CycleBaseCommit
		if briefItem.VerifyGate && cycleBase == "" {
			cycleBase, err = gitadapter.New().ResolveCommit(ctx, item.Item.ProjectPath, "HEAD")
			if err != nil {
				return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "verification_repository_unreadable",
					Message: "The item's Project repository HEAD could not be captured before assignment."}
			}
		}
		previous := workV2ActiveOwner(item)
		if previous.SessionID == sess.ConversationID {
			return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "assignment_unchanged",
				Message: "That Session already owns this item; choose another Session, or remind it instead."}
		}
		assigned, err := s.workV2().Assign(ctx, id, app.AssignWorkV2{ExpectedVersion: expected, Mode: mode,
			SessionID: sess.ConversationID, TerminalID: sess.ID, Assistant: string(sess.Assistant), Actor: actor,
			EpicOwner: epicOwner, Claim: claim, CycleBaseCommit: cycleBase}, false, nil)
		if err != nil {
			return app.WorkV2View{}, err
		}
		// The assignment itself is the durable delivery: it appears in the
		// Session's assigned-item projection immediately. Typing is only a
		// courtesy when a fresh reading still says idle. Working, waiting and
		// unknown rows pull their to-do list after their present turn.
		brief := workV2AssignmentBriefForItem(assigned.Item)
		if previous.ID != "" {
			brief = workV2ReassignmentBriefForItem(assigned.Item)
			s.tellReleasedOwner(ctx, previous, id, item.Item.Title)
		}
		if parent, ok := s.workV2ParentOf(ctx, item.Item); ok {
			brief += " " + workV2ParentNote(parent)
		}
		if _, sendErr := s.actions().SendIfIdle(ctx, sess.ID, brief); sendErr != nil {
			refusal, deferred := sendErr.(app.Refusal)
			if !deferred || refusal.Code != "session_not_idle" {
				condition := work.ConditionAssignedUnnotified
				if changed, editErr := s.workV2().Edit(ctx, id, app.EditWorkV2{ExpectedVersion: assigned.Item.Version,
					Condition: &condition, Actor: actor, Person: true}, nil); editErr == nil {
					assigned = changed
				}
			}
		}
		if file != nil {
			if k, a, ok := file(assigned); ok {
				if err := s.store.CompleteReceipt(ctx, k, a); err != nil {
					return assigned, err
				}
			}
		}
		return assigned, nil
	}
	if mode != "new_session" || s.broker == nil {
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_assignment", Message: "Choose an existing Session or a new Session."}
	}
	// The person picks the assistant; an older console that sends none still
	// gets Codex. The default is applied before the record is written so the
	// assignment names the assistant that was actually opened.
	switch assistant {
	case "":
		assistant = "codex"
	case "codex", "claude":
	default:
		return app.WorkV2View{}, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_assistant",
			Message: "A new Session is opened with codex or claude."}
	}
	if model == "" {
		model = "default"
	}
	item, err := s.workV2().Item(ctx, id)
	if err != nil {
		return app.WorkV2View{}, err
	}
	if personaID != "" {
		snapshot, err := s.ResolveSquadLaunch(ctx, personaID, item.Item.ProjectPath)
		if err != nil {
			if errors.Is(err, ErrSquadLaunchDefinition) {
				return app.WorkV2View{}, &app.WorkError{Status: http.StatusBadRequest, Code: "unknown_persona", Message: "No installed role has that ID."}
			}
			return app.WorkV2View{}, err
		}
		if (epicOwner != "" || claim != nil) && !snapshot.AutoAssignEnabled {
			return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "persona_disabled_for_auto_assignment", Message: "This role is disabled for automatic assignment in the target Project."}
		}
	}
	previous := workV2ActiveOwner(item)
	briefItem, err := s.workV2().PreviewAssignment(ctx, item.Item)
	if err != nil {
		return app.WorkV2View{}, err
	}
	cycleBase := briefItem.CycleBaseCommit
	if briefItem.VerifyGate && cycleBase == "" {
		cycleBase, err = gitadapter.New().ResolveCommit(ctx, item.Item.ProjectPath, "HEAD")
		if err != nil {
			return app.WorkV2View{}, &app.WorkError{Status: http.StatusConflict, Code: "verification_repository_unreadable",
				Message: "The item's Project repository HEAD could not be captured before assignment."}
		}
	}
	scope := item.Item.Description
	if previous.ID != "" {
		scope = workV2TakeoverNote(item.Item.Phase) + "\n\n" + scope
	}
	references := "Board item: " + item.Item.ID
	if parent, ok := s.workV2ParentOf(ctx, item.Item); ok {
		scope = workV2ParentNote(parent) + "\n\n" + scope
		references += "; parent Epic: " + parent.ID
	}
	if len(scope) > orchestrator.AssignmentFieldLimit() {
		continuation := "\n\nThe description continues on Board item " + id + "; read it in full before planning."
		cut := orchestrator.AssignmentFieldLimit() - len(continuation)
		for !utf8.RuneStart(scope[cut]) {
			cut--
		}
		scope = scope[:cut] + continuation
	}
	assignmentID, requestID := newWorkV2UUID(), newWorkV2UUID()
	pending, err := s.workV2().Assign(ctx, id, app.AssignWorkV2{ExpectedVersion: expected, Mode: mode,
		Assistant: assistant, Model: model, Actor: actor, AssignmentID: assignmentID, EpicOwner: epicOwner,
		Claim: claim, Persona: personaID, GatePreview: &app.WorkV2GateSettings{
			Planning: briefItem.PlanningGate, Verify: briefItem.VerifyGate,
		}, CycleBaseCommit: cycleBase}, true, nil)
	if err != nil {
		return app.WorkV2View{}, err
	}
	ra, _, openErr := s.broker.OpenRootAssignment(ctx, requestID, orchestrator.RootAssignmentRequest{
		RequestID: requestID, Assistant: assistant, Model: model, Persona: personaID, ProjectDir: item.Item.ProjectPath,
		Label: item.Item.Title, Assignment: orchestrator.Assignment{Objective: item.Item.Title,
			Scope: scope, Constraints: workV2RootConstraints(item.Item.ID, item.Item.Kind),
			RelevantReferences: references,
			Acceptance:         workV2RootAssignmentAcceptanceForItem(briefItem)},
		Handoff: s.workV2HandoffInput(ctx, item, previous)})
	// Its first screen was a question only the person may answer: the
	// assignment stays pending and the item asks them to answer it, and the
	// broker's beat finishes it either way (settleAwaitedAssignments).
	if openErr == nil && ra.State == orchestrator.AssignmentAwaitingDialog && ra.Executor != nil {
		waiting, err := s.workV2().AwaitAssignmentDialog(ctx, id, app.AwaitDialogV2{AssignmentID: assignmentID,
			RootAssignment: ra.ID, TerminalID: ra.Executor.TerminalID, Actor: actor})
		if err != nil {
			return pending, err
		}
		return waiting, s.fileWorkV2Answer(ctx, waiting, file)
	}
	finished, failure, err := s.finishRootAssignment(ctx, id, assignmentID, actor, ra, openErr, previous, item.Item.Title)
	if err != nil {
		return pending, err
	}
	if failure != "" {
		return finished, &app.WorkError{Status: http.StatusBadGateway, Code: "assignment_failed", Message: failure}
	}
	// File the answer after the external root was opened and the local owner is durable.
	return finished, s.fileWorkV2Answer(ctx, finished, file)
}

// fileWorkV2Answer completes the request's receipt with the item as it now is.
func (s *Server) fileWorkV2Answer(ctx context.Context, view app.WorkV2View, file app.WorkV2Filer) error {
	if file == nil {
		return nil
	}
	k, a, ok := file(view)
	if !ok {
		return nil
	}
	if len(a.Body) == 0 {
		a = store.ReceiptAnswer{Status: http.StatusOK, Body: workV2Answer(s.workV2ItemProjector(ctx)(view))}
	}
	return s.store.CompleteReceipt(ctx, k, a)
}

// finishRootAssignment settles a pending new-Session assignment from the
// Feature Root opened for it: active on the Session it opened, or failed
// saying why. The failure is also returned, empty when it became active.
func (s *Server) finishRootAssignment(ctx context.Context, workID, assignmentID, actor string,
	ra orchestrator.RootAssignment, openErr error, previous work.AssignmentV2, title string) (app.WorkV2View, string, error) {
	failure, resolvedTerminal, resolvedSession := "", "", ""
	if openErr != nil {
		failure = openErr.Error()
	} else if ra.Failure != "" || ra.Executor == nil {
		failure = ra.Failure
		if failure == "" {
			failure = "root assignment opened no Session"
		}
	} else {
		resolvedTerminal = ra.Executor.TerminalID
		for n := 0; n < 20; n++ {
			if sess, findErr := s.actions().Find(ctx, resolvedTerminal); findErr == nil && sess.ConversationID != "" {
				resolvedSession = sess.ConversationID
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if resolvedSession == "" {
			failure = "opened Session has no conversation id yet"
		}
	}
	finished, err := s.workV2().FinishAssignment(ctx, workID, app.FinishAssignmentV2{AssignmentID: assignmentID,
		SessionID: resolvedSession, TerminalID: resolvedTerminal, RootAssignment: ra.ID, Failure: failure, Actor: actor})
	if err != nil {
		return app.WorkV2View{}, "", err
	}
	if failure == "" && previous.ID != "" && previous.SessionID != resolvedSession {
		s.tellReleasedOwner(ctx, previous, workID, title)
	}
	return finished, failure, nil
}

// settleAwaitedAssignments finishes every Board assignment whose Feature Root
// was left at a dialog and has since been briefed or failed. The broker's beat
// asks for it when it settles such a Root, and the daemon once when it starts,
// for a Root settled while nothing was listening. An assignment another sweep
// finished first is refused by FinishAssignment and left alone.
func (s *Server) settleAwaitedAssignments(ctx context.Context) {
	if s.broker == nil {
		return
	}
	awaited, err := s.workV2().AwaitedAssignments(ctx)
	if err != nil {
		log.Printf("work v2: the assignments waiting on a dialog could not be read: %v", err)
		return
	}
	for _, a := range awaited {
		ra, err := s.broker.RootAssignmentByID(ctx, a.RootAssignment)
		if err != nil || ra.State == orchestrator.AssignmentAwaitingDialog {
			continue
		}
		item, err := s.workV2().Item(ctx, a.WorkID)
		if err != nil {
			continue
		}
		_, failure, err := s.finishRootAssignment(ctx, a.WorkID, a.ID, a.HumanActor, ra, nil,
			workV2ActiveOwner(item), item.Item.Title)
		switch {
		case err != nil:
			log.Printf("work v2: assignment %s waiting on Root Assignment %s was not settled: %v", a.ID, ra.ID, err)
		case failure != "":
			log.Printf("work v2: assignment %s failed after its dialog: %s", a.ID, failure)
		}
	}
}

func resetConfirmation(counts map[string]int64) string {
	b, _ := json.Marshal(counts)
	sum := sha256.Sum256(append([]byte("clear-work-v1:"), b...))
	return "clear-v1-" + fmt.Sprintf("%d", counts["work"]+counts["board_items"]+counts["backlog"]) + "-" + hex.EncodeToString(sum[:6])
}

func (s *Server) workV2Reset(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	counts, err := s.store.WorkV1Counts(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", err.Error())
		return
	}
	confirmation := resetConfirmation(counts)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"ok": true, "dry_run": true, "counts": counts, "confirmation": confirmation,
			"preserves": []string{"broker tasks", "landings", "root assignments", "session direct to-dos", "work-system v2", "settings", "Git"}})
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Preview with GET and clear with POST.")
		return
	}
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	if body.Confirmation != confirmation {
		writeRefusal(w, http.StatusConflict, "confirmation_mismatch", "The v1 counts changed or the exact dry-run confirmation was not supplied.")
		return
	}
	deleted, err := s.store.ResetWorkV1(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "reset_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": deleted})
}
