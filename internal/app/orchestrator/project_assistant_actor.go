package orchestrator

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// AssistantCurrent is resolved by the source host from its canonical registry,
// Bot aggregate, binding and dispatch policy on EVERY operation. No value here
// may come from model arguments or an HTTP request. DispatchAllowed is distinct
// from native read-only permissions and from any human approval of Board work.
type AssistantCurrent struct {
	Fence                  pa.Fence
	Source                 pa.SourceStatus
	Feature                pa.FeatureState
	BotName                string
	BotAppearance          pa.Appearance
	ActiveTurnID           string
	BindingRevoked         bool
	Project                pa.ProjectResolution
	DispatchAllowed        bool
	ProjectDispatchAllowed bool
	// ProjectDispatchAskable marks a direct user turn of a Bot that may ask
	// the person, on a permission card, to dispatch into another Project.
	// It grants nothing by itself: only ApprovedProjects admits a target.
	ProjectDispatchAskable bool
	// ApprovedProjects are destination Project IDs the person allowed for
	// this active turn, once on a card or by a saved Bot-to-Project rule.
	ApprovedProjects []string
	AllowedClaims    []string
	// AnyProjectFile admits any exact relative file inside the Project, as
	// CodyNC lets a Bot work anywhere in its workspace. Path, symlink and
	// directory checks still apply to every claim.
	AnyProjectFile bool
	Assistant      string
	PermissionMode string
	TimeoutMinutes int
}

type AssistantAuthorityOptions struct {
	Current        func(context.Context, pa.BotKey) (AssistantCurrent, error)
	Clock          func() time.Time
	ResolveProject func(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error)
}

// AssistantAuthority signs private, process-local capabilities. It holds no
// actor cache, queue or second expiry policy. The host supplies an explicit
// expiry from its registered turn deadline. Restart invalidates credentials;
// accepted tasks remain durable and can be read using a newly issued actor.
type AssistantAuthority struct {
	o   AssistantAuthorityOptions
	key [32]byte
}

func assistantFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

func NewAssistantAuthority(o AssistantAuthorityOptions) (*AssistantAuthority, error) {
	if o.Current == nil || o.Clock == nil {
		return nil, assistantFailure(pa.Invalid, "assistant_authority")
	}
	a := &AssistantAuthority{o: o}
	if _, err := rand.Read(a.key[:]); err != nil {
		return nil, err
	}
	return a, nil
}

// Issue is a host-only API. Never expose it as an operation accepting client
// identities. CapabilityID is a credential, not the public ownership reference.
func (a *AssistantAuthority) Issue(ctx context.Context, fence pa.Fence, operations []pa.ActorOperation, expires time.Time) (pa.AssistantExecutionActor, error) {
	if a == nil || a.o.Current == nil || a.o.Clock == nil {
		return pa.AssistantExecutionActor{}, assistantFailure(pa.ScopeDenied, "assistant_authority")
	}
	actor := pa.AssistantExecutionActor{Fence: fence, Operations: slices.Clone(operations), ExpiresAt: expires, CapabilityID: "issuing"}
	if len(operations) == 0 {
		return pa.AssistantExecutionActor{}, assistantFailure(pa.ScopeDenied, "actor_operation")
	}
	var current AssistantCurrent
	for _, op := range operations {
		var err error
		if current, err = a.checkCurrent(ctx, actor, op); err != nil {
			return pa.AssistantExecutionActor{}, err
		}
	}
	actor.CapabilityID = a.signature(actor, current.ActiveTurnID)
	return actor, nil
}

func (a *AssistantAuthority) signature(actor pa.AssistantExecutionActor, turnID string) string {
	actor.CapabilityID = ""
	if !slices.Contains(actor.Operations, pa.DispatchWork) {
		turnID = ""
	}
	body, _ := json.Marshal(struct {
		Actor  pa.AssistantExecutionActor
		TurnID string
	}{actor, turnID})
	m := hmac.New(sha256.New, a.key[:])
	_, _ = m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (a *AssistantAuthority) verify(ctx context.Context, actor pa.AssistantExecutionActor, op pa.ActorOperation) (AssistantCurrent, error) {
	if a == nil || a.o.Current == nil || a.o.Clock == nil || len(actor.CapabilityID) != sha256.Size*2 {
		return AssistantCurrent{}, assistantFailure(pa.ScopeDenied, "assistant_capability")
	}
	c, err := a.checkCurrent(ctx, actor, op)
	if err != nil {
		return c, err
	}
	if !hmac.Equal([]byte(actor.CapabilityID), []byte(a.signature(actor, c.ActiveTurnID))) {
		return AssistantCurrent{}, assistantFailure(pa.ScopeDenied, "assistant_capability")
	}
	return c, nil
}

func (a *AssistantAuthority) checkCurrent(ctx context.Context, actor pa.AssistantExecutionActor, op pa.ActorOperation) (AssistantCurrent, error) {
	c, err := a.o.Current(ctx, actor.Fence.Key)
	if err != nil {
		return c, err
	}
	if err = pa.CheckActor(actor, c.Fence, op, a.o.Clock()); err != nil {
		return c, err
	}
	// Execution revocation does not revoke an independently issued historical
	// reader. A mixed dispatch/read credential retains execution checks and
	// cannot silently downgrade itself after revocation or feature disable.
	write := op == pa.DispatchWork || slices.Contains(actor.Operations, pa.DispatchWork)
	if c.BindingRevoked && write {
		return c, assistantFailure(pa.Revoked, "binding")
	}
	if c.Source.MachineID != c.Fence.Key.MachineID {
		return c, assistantFailure(pa.ScopeDenied, "source")
	}
	if err = pa.CheckSource(c.Source, write); err != nil {
		return c, err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: c.Fence.Project.MachineID, ProjectID: c.Fence.Project.ProjectID}, c.Project); err != nil {
		return c, err
	}
	if c.Project.Scope != c.Fence.Project {
		return c, assistantFailure(pa.ScopeDenied, "canonical_project")
	}
	if write {
		if err = c.Feature.CheckWrite(); err != nil {
			return c, err
		}
		if !(c.DispatchAllowed && c.Fence.Project.Kind == pa.RepositoryProject) && !c.ProjectDispatchAllowed && !c.ProjectDispatchAskable {
			return c, assistantFailure(pa.ScopeDenied, "dispatch_policy")
		}
		if c.ActiveTurnID == "" {
			return c, assistantFailure(pa.StaleVersion, "active_turn")
		}
	}
	return c, nil
}

// AssistantOwner contains only public provenance. It grants no execution or
// Board authority. The credential is deliberately absent from Record and Brief.
type AssistantOwner struct {
	Fence         pa.Fence        `json:"fence"`
	BotName       string          `json:"bot_name,omitempty"`
	BotAppearance pa.Appearance   `json:"bot_appearance,omitzero"`
	TurnID        string          `json:"turn_id"`
	Command       pa.Command      `json:"command"`
	RequestHash   string          `json:"request_hash"`
	TargetProject pa.ProjectScope `json:"target_project,omitzero"`
}

type assistantAdmission struct {
	record  Record
	actor   pa.AssistantExecutionActor
	request pa.WorkRequest
}

// AssistantTaskID derives an opaque stable UUID from the command identity,
// excluding content hash: reusing a key for different content must conflict.
// Call this only with a server-authenticated command. It grants no access.
func AssistantTaskID(command pa.Command) string {
	body, _ := json.Marshal([]string{command.Key.MachineID, command.Key.BotID, string(command.Operation), command.IdempotencyKey})
	digest := sha256.Sum256(body)
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	s := hex.EncodeToString(digest[:16])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

// AssistantRequestHash covers effect-bearing arguments independently from a
// caller's claimed ContentHash; credentials and expiry are not persisted.
func AssistantRequestHash(q pa.WorkRequest) string {
	body, _ := json.Marshal(struct {
		Command       pa.Command
		Fence         pa.Fence
		TurnID, Brief string
		Claims        []string
		Title         string          `json:",omitempty"`
		TargetProject pa.ProjectScope `json:"target_project,omitzero"`
	}{q.Command, q.Actor.Fence, q.TurnID, q.Brief, q.Claims, q.Title, q.TargetProject})
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func (b *Broker) checkAssistantRequest(ctx context.Context, q pa.WorkRequest) (AssistantCurrent, error) {
	c, err := b.AssistantAuthority.verify(ctx, q.Actor, pa.DispatchWork)
	if err != nil {
		return c, err
	}
	if err = pa.CheckWorkRequest(q, c.Fence, b.AssistantAuthority.o.Clock()); err != nil {
		return c, err
	}
	if q.TurnID != c.ActiveTurnID {
		return c, assistantFailure(pa.StaleVersion, "work_turn")
	}
	// The source fence owns the receipt. Resolve the destination separately
	// for each admission check, including the check before durable creation.
	if target := q.TargetProject; target != (pa.ProjectScope{}) {
		if !c.ProjectDispatchAllowed && !(c.ProjectDispatchAskable && slices.Contains(c.ApprovedProjects, target.ProjectID)) || b.AssistantAuthority.o.ResolveProject == nil {
			return c, assistantFailure(pa.ScopeDenied, "dispatch_project")
		}
		selection := pa.ProjectSelection{MachineID: target.MachineID, ProjectID: target.ProjectID}
		destination, err := b.AssistantAuthority.o.ResolveProject(ctx, selection)
		if err != nil {
			return c, err
		}
		if err = pa.ValidateTarget(selection, destination); err != nil {
			return c, err
		}
		if destination.Scope != target {
			return c, assistantFailure(pa.StaleVersion, "dispatch_project")
		}
		c.Project = destination
	} else if !c.DispatchAllowed || c.Fence.Project.Kind != pa.RepositoryProject {
		return c, assistantFailure(pa.ScopeDenied, "dispatch_policy")
	}
	claims, err := admitClaims(q.Claims)
	if err != nil {
		return c, assistantFailure(pa.ScopeDenied, "claims")
	}
	for _, claim := range claims {
		// Exact relative files only. Ordinary broker callers retain their
		// existing path contract; a Bot cannot request directories or globs.
		if claim != filepath.ToSlash(filepath.Clean(claim)) || claim == "." || strings.ContainsAny(claim, "\\*?[]:") || filepath.IsAbs(claim) || !c.AnyProjectFile && !slices.Contains(c.AllowedClaims, claim) {
			return c, assistantFailure(pa.ScopeDenied, "claims")
		}
		// Reject existing symlink components, including symlinked directories.
		// New files are allowed only beneath a real in-repository ancestor.
		root, e := filepath.EvalSymlinks(c.Project.Directory)
		if e != nil {
			return c, assistantFailure(pa.ScopeDenied, "project_directory")
		}
		info, e := os.Stat(filepath.Join(root, filepath.FromSlash(claim)))
		if e == nil && info.IsDir() || e != nil && !errors.Is(e, os.ErrNotExist) {
			return c, assistantFailure(pa.ScopeDenied, "claims_file")
		}
		for path := filepath.Join(root, filepath.FromSlash(claim)); path != root; path = filepath.Dir(path) {
			resolved, e := filepath.EvalSymlinks(path)
			if e == nil && resolved != path {
				return c, assistantFailure(pa.ScopeDenied, "claims_symlink")
			}
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return c, assistantFailure(pa.ScopeDenied, "claims_symlink")
			}
		}
	}
	return c, nil
}

func assistantTaskTitle(q pa.WorkRequest) string {
	if q.Title != "" {
		return strings.TrimSpace(q.Title)
	}

	// Older callers supplied only a brief. Use its opening line when it can
	// serve as a task title, and keep the full brief in the worker instructions.
	line, _, _ := strings.Cut(strings.TrimSpace(q.Brief), "\n")
	title := strings.TrimSpace(truncate(strings.TrimSpace(line), titleLimit))
	if pa.ValidateWorkTitle(title) == nil {
		return title
	}
	return "Complete the scoped Bot request"
}

// DispatchAssistant is the only Bot dispatch door. It prepares an in-memory
// validated draft, then enters Dispatch's existing claims/capacity/quota/
// worktree/durable-before-spawn pipeline. No Session or user run is fabricated.
func (b *Broker) DispatchAssistant(ctx context.Context, q pa.WorkRequest) (Dispatched, error) {
	b.assistantMu.Lock()
	defer b.assistantMu.Unlock()
	c, err := b.checkAssistantRequest(ctx, q)
	if err != nil {
		return Dispatched{}, err
	}
	if b.AssistantWorkChanged == nil {
		return Dispatched{}, assistantFailure(pa.Disabled, "work_result_sink")
	}
	id := AssistantTaskID(q.Command)
	protocol := Protocol
	d := draft{Protocol: &protocol, TaskID: id, Kind: "implementation", Assistant: c.Assistant, PermissionMode: c.PermissionMode, ProjectDir: c.Project.Directory,
		Title: assistantTaskTitle(q), Instructions: q.Brief, Claims: &q.Claims, Isolation: IsolationWorktree, TimeoutMinutes: &c.TimeoutMinutes}
	if c.PermissionMode == "" {
		return Dispatched{}, assistantFailure(pa.ScopeDenied, "worker_permission")
	}
	r, err := b.admitAssistant(id, d)
	if err != nil {
		return Dispatched{}, err
	}
	r.AssistantOwner = &AssistantOwner{Fence: q.Actor.Fence, BotName: c.BotName, TurnID: q.TurnID, Command: q.Command, RequestHash: AssistantRequestHash(q), TargetProject: q.TargetProject}
	if pa.ValidateAppearance(c.BotAppearance) == nil {
		r.AssistantOwner.BotAppearance = c.BotAppearance
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return Dispatched{}, err
	}
	inv, err := b.ReadInventory(ctx, c.Project.Directory, nil)
	if err != nil {
		return Dispatched{}, err
	}
	return b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: hex.EncodeToString(secret), Generation: inv.Generation, Offered: true, assistant: &assistantAdmission{record: r, actor: q.Actor, request: q}})
}

// ValidateAssistantWork checks the private source authority before the Bridge
// writes its preparation receipt. Dispatch rechecks immediately before create.
func (b *Broker) ValidateAssistantWork(ctx context.Context, q pa.WorkRequest) error {
	_, err := b.checkAssistantRequest(ctx, q)
	if err == nil && b.AssistantWorkChanged == nil {
		return assistantFailure(pa.Disabled, "work_result_sink")
	}
	return err
}

func sameAssistantOwner(a, b *AssistantOwner) bool {
	// The display name and appearance are recorded at first admission and
	// play no part in deciding whether a retry belongs to the same owner.
	return a != nil && b != nil && a.Fence == b.Fence && a.TurnID == b.TurnID &&
		a.Command == b.Command && a.RequestHash == b.RequestHash && a.TargetProject == b.TargetProject
}

// AssistantWorkSnapshot is a sanitized source-owned projection. Task-only work
// has no Board route; TaskID is a read key, never an invented navigation URL.
type AssistantWorkSnapshot struct {
	Owner  AssistantOwner
	Link   pa.WorkLink
	Result string
}

// AssistantWorkSnapshotOf projects a broker-owned record, never client JSON.
// A record without an AssistantOwner has no Bot projection.
func AssistantWorkSnapshotOf(r Record) AssistantWorkSnapshot {
	if r.AssistantOwner == nil {
		return AssistantWorkSnapshot{}
	}
	s := AssistantWorkSnapshot{Owner: *r.AssistantOwner, Link: pa.WorkLink{ID: r.ID, TaskID: r.ID, Fence: r.AssistantOwner.Fence, TurnID: r.AssistantOwner.TurnID, BoardItemID: r.WorkID, State: pa.WorkUnknown}}
	switch r.State {
	case StateQueued, StateSpawning:
		s.Link.State = pa.WorkAccepted
		if r.State == StateSpawning && !r.AwaitingDialogSince.IsZero() {
			// The briefing waits for the person to answer the worker's dialog.
			s.Link.State = pa.WorkWaitingForPerson
		}
	case StateBriefed:
		s.Link.State = pa.WorkExecuting
	case StateSuccess:
		s.Link.State = pa.WorkCompleted
	case StateFailure, StateCancelled, StateSpawnFailed:
		s.Link.State = pa.WorkFailed
	}
	if r.Landing != nil && (r.Landing.State == LandingLanded || r.Landing.State == LandingIncorporated) {
		s.Link.State, s.Link.LandingCommit = pa.WorkLanded, r.Landing.Commit
	}
	if r.State.Terminal() {
		s.Link.ResultMessageID = "work-result-" + r.ID
		s.Result = r.Verdict
		if r.Result != nil {
			s.Result = r.Result.Summary
		}
		if s.Result == "" {
			s.Result = fmt.Sprintf("%s", r.State)
		}
	}
	return s
}

// AssistantStatus reads history for this Bot/Project/conversation, independent
// of the historical execution generation. The supplied actor must itself be
// current. It never creates, resumes, respawns or lands a task.
func (b *Broker) AssistantStatus(ctx context.Context, actor pa.AssistantExecutionActor, taskID string) (AssistantWorkSnapshot, error) {
	if _, err := b.AssistantAuthority.verify(ctx, actor, pa.ReadWork); err != nil {
		return AssistantWorkSnapshot{}, err
	}
	if !IsTaskID(taskID) {
		return AssistantWorkSnapshot{}, assistantFailure(pa.Invalid, "task_id")
	}
	r, _, err := b.Record(ctx, taskID)
	if err != nil {
		return AssistantWorkSnapshot{}, err
	}
	if r.AssistantOwner == nil || r.AssistantOwner.Fence.Key != actor.Fence.Key || r.AssistantOwner.Fence.Project != actor.Fence.Project || r.AssistantOwner.Fence.ConversationID != actor.Fence.ConversationID {
		return AssistantWorkSnapshot{}, assistantFailure(pa.ScopeDenied, "work_owner")
	}
	return AssistantWorkSnapshotOf(r), nil
}

func (b *Broker) publishAssistantWork(ctx context.Context, r Record) {
	if r.AssistantOwner != nil && b.AssistantWorkChanged != nil {
		// A failed sink never rewrites broker facts. Status is the durable
		// pull/recovery path; the host must surface sink failures separately.
		if err := b.AssistantWorkChanged(ctx, AssistantWorkSnapshotOf(r)); err != nil {
			log.Printf("broker: Bot work projection pending for task %s; reconcile through Status", r.ID)
		}
	}
}
