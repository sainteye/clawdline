package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

var workV2ByServer sync.Map // *Server -> *app.WorkSystemV2

func (s *Server) workV2() *app.WorkSystemV2 {
	if v, ok := workV2ByServer.Load(s); ok {
		return v.(*app.WorkSystemV2)
	}
	v := app.NewWorkSystemV2(s.store)
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
	ID               string                `json:"id"`
	Project          workV2ProjectWire     `json:"project"`
	Kind             string                `json:"kind"`
	Title            string                `json:"title"`
	Description      string                `json:"description"`
	Phase            string                `json:"phase"`
	Condition        *string               `json:"condition"`
	UserAction       string                `json:"user_action"`
	Area             string                `json:"area"`
	DeploymentPolicy string                `json:"deployment_policy"`
	OwnerSession     *string               `json:"owner_session"`
	CreatedBy        string                `json:"created_by"`
	CreatedVia       *workV2CreatedViaWire `json:"created_via,omitempty"`
	// ClaimedVia is the person's message the owning Session claimed the item
	// on; absent when the person assigned it, or nobody holds it.
	ClaimedVia  *workV2CreatedViaWire  `json:"claimed_via,omitempty"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
	ClosedAt    *int64                 `json:"closed_at"`
	Cycle       int64                  `json:"cycle"`
	Version     int64                  `json:"version"`
	Assignments []workV2AssignmentWire `json:"assignments,omitempty"`
	Documents   []workV2DocumentWire   `json:"documents,omitempty"`
	Images      []workV2ImageWire      `json:"images,omitempty"`
	Steps       []workV2StepWire       `json:"steps,omitempty"`
	Events      []workV2EventWire      `json:"events,omitempty"`
}

// workV2CreatedViaWire is the person's message a Session created an item on:
// its run, when it was said, and the start of what was said. Absent for an
// item a person created as themselves.
type workV2CreatedViaWire struct {
	Run       string `json:"run"`
	SessionID string `json:"session_id"`
	At        int64  `json:"at"`
	Excerpt   string `json:"excerpt,omitempty"`
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

type workV2GitReader interface {
	ValidBranchName(context.Context, string) bool
	ResolveCommit(context.Context, string, string) (string, error)
	IsAncestor(context.Context, string, string, string) (bool, error)
}

// workV2PhaseInstruction names the command that moves the phase, in every
// brief an owner receives: a Session told only to "implement, verify, merge
// and deploy" finished the work and left its item in assigned, because nothing
// it read named the route (2026-09-26).
func workV2PhaseInstruction(id string) string {
	return "Move the item through its phases yourself as the work happens: `clawdline item phase " + id +
		" implementing`, then verifying, merging (--verification), deploying (--commit --target --remote) and done " +
		"(--deployment or --no-deployment-reason); `clawdline guide board` says what each one needs."
}

// workV2StepsInstruction says when an owner breaks its item into steps, in
// every brief an owner receives: the daemon already took steps from their
// owner, yet no brief or command named that, so complex work was never broken
// down unless the person wrote the list themselves; and a simple change must
// not grow a ceremony of steps (2026-09-26).
func workV2StepsInstruction(id string) string {
	return "If this item has no steps and the work is multi-stage — several changes verified separately, or more than " +
		"one part of the system — first break it into its ordered steps with `clawdline item step-add " + id +
		" \"…\" \"…\"` and complete each with `clawdline item step-done` once it is verified; a single straightforward " +
		"change takes no steps."
}

// workV2AssignmentBrief is what an existing Session is sent when it is
// assigned an item.
func workV2AssignmentBrief(id, title string) string {
	return fmt.Sprintf("Clawdline assigned you Board item %s: %s. Read its description and reference images, then own it through implementation, verification, Merge, and deployment. Use the work-system v2 Agent API to update it; do not create Board items. %s %s %s", id, title,
		workV2StepsInstruction(id), workV2PhaseInstruction(id), workV2CompletionReportInstruction)
}

// workV2RootAssignmentAcceptance is the acceptance of the Root Assignment a
// new Session is opened with for an item.
func workV2RootAssignmentAcceptance(id string) string {
	return "Implement, verify, merge, and deploy according to the item's deployment policy. " +
		workV2StepsInstruction(id) + " " + workV2PhaseInstruction(id) + " " + workV2CompletionReportInstruction
}

const workV2CompletionReportInstruction = "When substantial investigation was needed to find a non-obvious cause, add a user-readable completion_report before done; a straightforward fix does not require one."

// repo is the repository the commit is looked for in: the item's Project, or
// the catalog Project the request named, resolved by the caller.
func verifyWorkV2DirectLanding(ctx context.Context, g workV2GitReader, item app.WorkV2View,
	session, repo string, ask *workV2LandingRequest) (*app.VerifiedLandingV2, *app.WorkError) {
	if ask == nil {
		return nil, nil
	}
	ownedDirectly := false
	for _, a := range item.Assignments {
		ownsSession := a.State == "active" && a.SessionID == session
		directSession := a.Mode == "existing_session"
		resolvedRoot := a.Mode == "new_session" && strings.TrimSpace(a.RootAssignment) != ""
		if ownsSession && (directSession || resolvedRoot) {
			ownedDirectly = true
			break
		}
	}
	if !ownedDirectly {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "direct_landing_not_applicable",
			Message: "Direct landing evidence belongs to an active existing-Session assignment or a resolved Root Assignment."}
	}
	commit, target, remote := strings.TrimSpace(ask.Commit), strings.TrimSpace(ask.Target), strings.TrimSpace(ask.Remote)
	if commit == "" || !g.ValidBranchName(ctx, target) || remote == "" || strings.Contains(remote, "/") ||
		!g.ValidBranchName(ctx, remote) {
		return nil, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_landing_evidence",
			Message: "Landing evidence needs a commit, a valid local target branch, and one remote name."}
	}
	resolved, err := g.ResolveCommit(ctx, repo, commit)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_commit_unresolved",
			Message: "The landing commit does not resolve in the item's Project. Work that landed in another Project names it with landing.project."}
	}
	localRef := "refs/heads/" + target
	localHead, err := g.ResolveCommit(ctx, repo, localRef)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_target_unresolved",
			Message: "The local target branch does not resolve in the item's Project."}
	}
	onLocal, err := g.IsAncestor(ctx, repo, resolved, localHead)
	if err != nil || !onLocal {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_not_on_target",
			Message: "The landing commit is not contained by the local target branch."}
	}
	remoteRef := "refs/remotes/" + remote + "/" + target
	remoteHead, err := g.ResolveCommit(ctx, repo, remoteRef)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_remote_unresolved",
			Message: "The remote-tracking target does not resolve; fetch or push it before recording landing."}
	}
	onRemote, err := g.IsAncestor(ctx, repo, resolved, remoteHead)
	if err != nil || !onRemote {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_not_published",
			Message: "The landing commit is not contained by the remote-tracking target."}
	}
	landing := &app.VerifiedLandingV2{Commit: resolved, Target: target, TargetCommit: localHead,
		Remote: remote, RemoteCommit: remoteHead}
	if repo != item.Item.ProjectPath {
		landing.Repository = repo
	}
	return landing, nil
}

func (s *Server) workV2Project(ctx context.Context, id string) (workV2ProjectWire, bool) {
	for _, p := range s.projectReaders().places.List(s.liveDirectories(ctx), 40) {
		if p.ID != id {
			continue
		}
		canonical, ok := projects.CanonicalProjectKey(p.Path)
		if !ok {
			return workV2ProjectWire{}, false
		}
		label := p.Label
		if label == "" {
			label = filepath.Base(canonical)
		}
		return workV2ProjectWire{ID: p.ID, Label: label, Path: canonical, Icon: wireIcon(s.icons.For(canonical)), Available: true}, true
	}
	return workV2ProjectWire{}, false
}

func (s *Server) workV2ItemOf(ctx context.Context, v app.WorkV2View) workV2ItemWire {
	i := v.Item
	p, ok := s.workV2Project(ctx, i.ProjectID)
	if !ok {
		label := filepath.Base(i.ProjectPath)
		p = workV2ProjectWire{ID: i.ProjectID, Label: label, Path: i.ProjectPath, Icon: wireIcon(s.icons.For(i.ProjectPath))}
	}
	out := workV2ItemWire{ID: i.ID, Project: p, Kind: string(i.Kind), Title: i.Title, Description: i.Description,
		Phase: string(i.Phase), Condition: optionalString(string(i.Condition)), UserAction: i.UserAction, Area: i.Area(),
		DeploymentPolicy: string(i.DeploymentPolicy), OwnerSession: optionalString(i.OwnerSession),
		CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt.Unix(), UpdatedAt: i.UpdatedAt.Unix(),
		ClosedAt: optionalUnix(i.ClosedAt), Cycle: i.Cycle, Version: i.Version}
	if via := i.CreatedVia; via != nil && strings.HasPrefix(i.CreatedBy, work.ActorViaSession) {
		out.CreatedVia = &workV2CreatedViaWire{Run: via.Run, SessionID: via.Session, At: via.At, Excerpt: via.Excerpt}
	}
	claim := v.Claim
	for _, a := range v.Assignments {
		if a.State == "active" && a.ClaimedVia != nil {
			claim = a.ClaimedVia
		}
	}
	if claim != nil {
		out.ClaimedVia = &workV2CreatedViaWire{Run: claim.Run, SessionID: claim.Session, At: claim.At, Excerpt: claim.Excerpt}
	}
	for _, a := range v.Assignments {
		out.Assignments = append(out.Assignments, workV2AssignmentWire{ID: a.ID, Mode: a.Mode, SessionID: a.SessionID,
			TerminalID: a.TerminalID, Assistant: a.Assistant, Model: a.Model, State: a.State,
			RootAssignment: a.RootAssignment, Failure: a.Failure, CreatedAt: a.CreatedAt.Unix(), UpdatedAt: a.UpdatedAt.Unix()})
		if via := a.ClaimedVia; via != nil && strings.HasPrefix(a.HumanActor, work.ActorViaSession) {
			out.Assignments[len(out.Assignments)-1].ClaimedVia = &workV2CreatedViaWire{Run: via.Run,
				SessionID: via.Session, At: via.At, Excerpt: via.Excerpt}
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
		if parts[2] == "images" {
			s.workV2AddImage(w, r, parts[1])
		} else {
			s.workV2PersonAction(w, r, parts[1], parts[2])
		}
	case len(parts) == 4 && parts[0] == "items" && workID(parts[1]) && parts[2] == "images" && workID(parts[3]):
		s.workV2DeleteImage(w, r, parts[1], parts[3])
	case len(parts) >= 2 && parts[0] == "session-todos":
		s.workV2SessionTodos(w, r, parts[1:])
	case len(parts) >= 2 && parts[0] == "agent":
		s.workV2Agent(w, r, parts[1:])
	case rest == "proposals":
		s.workV2Proposals(w, r)
	case len(parts) == 3 && parts[0] == "proposals":
		s.workV2ResolveProposal(w, r, parts[1], parts[2])
	case rest == "reset":
		s.workV2Reset(w, r)
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such work-system v2 route.")
	}
}

// referenceImageFileRefusal names a reference image whose row is there and
// whose file is not, or is not the bytes the row recorded. It is 410: the
// picture was stored and cannot be served again, and asking again will not
// bring it back — not a 500, and never an empty picture.
func referenceImageFileRefusal(err error) (*app.WorkError, bool) {
	switch {
	case errors.Is(err, store.ErrReferenceImageMissing):
		return &app.WorkError{Status: http.StatusGone, Code: "image_file_missing",
			Message: "That reference image's file is gone from this machine."}, true
	case errors.Is(err, store.ErrReferenceImageMismatch):
		return &app.WorkError{Status: http.StatusGone, Code: "image_file_mismatch",
			Message: "That reference image's file is not the picture that was stored."}, true
	}
	return nil, false
}

func (s *Server) workV2ReferenceImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is read with GET.")
		return
	}
	// `?size=thumb` is the picture a card or a to-do row draws: long edge at
	// most artifacts.MaxThumbnailEdge, as JPEG. No query is the full stored
	// PNG, which is what the full-size viewer asks for. Anything else is a
	// spelling this route does not know, refused rather than read as "full".
	query := r.URL.Query()
	thumb := false
	for key, values := range query {
		if key != "size" || len(values) != 1 || values[0] != "thumb" {
			writeRefusal(w, http.StatusBadRequest, "bad_query", "A reference image is read whole, or with size=thumb.")
			return
		}
		thumb = true
	}
	data, mediaType, ok, err := s.store.WorkV2ImageBytes(r.Context(), id)
	if refusal, named := referenceImageFileRefusal(err); named {
		writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The reference image could not be read.")
		return
	}
	if !ok {
		writeRefusal(w, http.StatusNotFound, "image_not_found", "No reference image has that id.")
		return
	}
	if thumb {
		// The stored image is read first either way, so a deleted image is a
		// 404 and never a thumbnail the cache still holds.
		t, err := s.thumbs.Get(id, data)
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The reference image could not be drawn small.")
			return
		}
		data, mediaType = t.JPEG, "image/jpeg"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

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
	var answer []byte
	v, err := s.workV2().ResolveProposal(r.Context(), id, actor, app.ProposalDecisionV2{Decision: decision,
		Title: body.Title, Description: body.Description, SuggestedAcceptance: body.SuggestedAcceptance}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
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
	q, ok := workQuery(w, r, "project", "owner", "terminal", "status", "q")
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
	for _, row := range rows {
		wire := s.workV2ItemOf(r.Context(), row)
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
	writeJSON(w, map[string]any{"ok": true, "item": s.workV2ItemOf(r.Context(), v)})
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
	return raw, true
}

func (s *Server) workV2AddImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is added with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		Title           string `json:"title"`
		DataURL         string `json:"data_url"`
		Position        int64  `json:"position"`
	}
	raw, ok := readWorkV2BodyAtMost(w, r, &body, workV2ImageBodyLimit, "A reference-image request is at most 18 MiB.")
	if !ok {
		return
	}
	bytes, ok := artifacts.DecodeDataURL(body.DataURL)
	if !ok {
		writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
		return
	}
	policy := artifacts.ProductionPolicy
	policy.MaxEncodedBytes = workV2ImageByteLimit
	normalized, err := artifacts.Normalize(r.Context(), bytes, policy)
	if err != nil {
		var refusal artifacts.Refusal
		if errors.As(err, &refusal) {
			writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		} else {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
		}
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	var answer []byte
	_, err = s.workV2().AddImage(r.Context(), id, app.AddImageV2{ExpectedVersion: body.ExpectedVersion,
		Title: body.Title, Data: normalized.Data, MediaType: normalized.MediaType, Width: normalized.Width, Height: normalized.Height,
		Position: body.Position, Actor: actor}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
		return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
	})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

func (s *Server) workV2DeleteImage(w http.ResponseWriter, r *http.Request, id, imageID string) {
	if r.Method != http.MethodDelete {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is removed with DELETE.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	var answer []byte
	_, err := s.workV2().DeleteImage(r.Context(), id, imageID, body.ExpectedVersion, actor,
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
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
		ProjectID        string `json:"project_id"`
		Kind             string `json:"kind"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		DeploymentPolicy string `json:"deployment_policy"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	p, ok := s.workV2Project(r.Context(), body.ProjectID)
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
		Title: body.Title, Description: body.Description, DeploymentPolicy: work.DeploymentPolicy(body.DeploymentPolicy), Actor: actor},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
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
		ExpectedVersion int64   `json:"expected_version"`
		Title           *string `json:"title"`
		Description     *string `json:"description"`
		Condition       *string `json:"condition"`
		UserAction      *string `json:"user_action"`
		SessionID       string  `json:"session_id"`
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
	var condition *work.Condition
	if body.Condition != nil {
		v := work.Condition(*body.Condition)
		condition = &v
	}
	var answer []byte
	_, err := s.workV2().Edit(r.Context(), id, app.EditWorkV2{ExpectedVersion: body.ExpectedVersion,
		Title: body.Title, Description: body.Description, Condition: condition, UserAction: body.UserAction, Actor: actor,
		OwnerSession: body.SessionID, Person: person}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
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
		ExpectedVersion int64  `json:"expected_version"`
		Mode            string `json:"mode"`
		TerminalID      string `json:"terminal_id"`
		Assistant       string `json:"assistant"`
		Model           string `json:"model"`
		Reason          string `json:"reason"`
		Note            string `json:"note"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	var answer []byte
	file := func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
		return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
	}
	var out app.WorkV2View
	var err error
	switch action {
	case "assign":
		out, err = s.assignWorkV2(r.Context(), id, actor, body.ExpectedVersion, body.Mode, body.TerminalID, body.Assistant, body.Model, file)
	case "remind":
		out, err = s.remindWorkV2(r.Context(), id, body.ExpectedVersion)
		if err == nil {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), out))
			err = s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer})
		}
	case "unassign":
		out, err = s.workV2().Unassign(r.Context(), id, body.ExpectedVersion, actor, file)
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
		assigned, err := s.workV2().Assign(ctx, id, app.AssignWorkV2{ExpectedVersion: expected, Mode: mode,
			SessionID: sess.ConversationID, TerminalID: sess.ID, Assistant: string(sess.Assistant), Actor: actor}, false, nil)
		if err != nil {
			return app.WorkV2View{}, err
		}
		// The assignment itself is the durable delivery: it appears in the
		// Session's assigned-item projection immediately. Typing is only a
		// courtesy when a fresh reading still says idle. Working, waiting and
		// unknown rows pull their to-do list after their present turn.
		brief := workV2AssignmentBrief(id, item.Item.Title)
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
	assignmentID, requestID := newWorkV2UUID(), newWorkV2UUID()
	pending, err := s.workV2().Assign(ctx, id, app.AssignWorkV2{ExpectedVersion: expected, Mode: mode,
		Assistant: assistant, Model: model, Actor: actor, AssignmentID: assignmentID}, true, nil)
	if err != nil {
		return app.WorkV2View{}, err
	}
	ra, _, openErr := s.broker.OpenRootAssignment(ctx, requestID, orchestrator.RootAssignmentRequest{
		RequestID: requestID, Assistant: assistant, Model: model, ProjectDir: item.Item.ProjectPath,
		Label: item.Item.Title, Assignment: orchestrator.Assignment{Objective: item.Item.Title,
			Scope: item.Item.Description, Constraints: "Own only this Board item. Do not create Board items.",
			RelevantReferences: "Board item: " + item.Item.ID,
			Acceptance:         workV2RootAssignmentAcceptance(item.Item.ID)}})
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
	finished, finishErr := s.workV2().FinishAssignment(ctx, id, app.FinishAssignmentV2{AssignmentID: assignmentID,
		SessionID: resolvedSession, TerminalID: resolvedTerminal, RootAssignment: ra.ID, Failure: failure, Actor: actor})
	if finishErr != nil {
		return pending, finishErr
	}
	if failure != "" {
		return finished, &app.WorkError{Status: http.StatusBadGateway, Code: "assignment_failed", Message: failure}
	}
	// File the answer after the external root was opened and the local owner is durable.
	answer := workV2Answer(s.workV2ItemOf(ctx, finished))
	if file != nil {
		if k, a, ok := file(finished); ok {
			if len(a.Body) == 0 {
				a = store.ReceiptAnswer{Status: http.StatusOK, Body: answer}
			}
			if err := s.store.CompleteReceipt(ctx, k, a); err != nil {
				return finished, err
			}
		}
	}
	return finished, nil
}

type directTodoV2Wire struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	CreatedAt   int64  `json:"created_at"`
	SentAt      *int64 `json:"sent_at"`
	ReadAt      *int64 `json:"read_at"`
	CompletedAt *int64 `json:"completed_at"`
	CompletedBy string `json:"completed_by,omitempty"`
	// CreatedBy is who wrote the row: the person's actor, or the Session's own
	// conversation id when the Session added it on the person's request.
	CreatedBy string            `json:"created_by"`
	Version   int64             `json:"version"`
	Images    []workV2ImageWire `json:"images,omitempty"`
}

func directTodoWire(td work.DirectTodoV2, images []work.DirectTodoImageV2) directTodoV2Wire {
	out := directTodoV2Wire{ID: td.ID, Text: td.Text, CreatedAt: td.CreatedAt.Unix(), SentAt: optionalUnix(td.SentAt),
		ReadAt: optionalUnix(td.ReadAt), CompletedAt: optionalUnix(td.CompletedAt), CompletedBy: td.CompletedBy,
		CreatedBy: td.CreatedBy, Version: td.Version}
	for _, image := range images {
		out.Images = append(out.Images, workV2ImageWire{ID: image.ID, Title: image.Title, MediaType: image.MediaType,
			ByteCount: image.ByteCount, Width: image.Width, Height: image.Height, Position: image.Position,
			CreatedBy: image.CreatedBy, CreatedAt: image.CreatedAt.Unix()})
	}
	return out
}

func directTodoAnswer(td work.DirectTodoV2, images []work.DirectTodoImageV2) []byte {
	b, _ := json.Marshal(map[string]any{"ok": true, "todo": directTodoWire(td, images)})
	return b
}

func (s *Server) workV2SessionTodos(w http.ResponseWriter, r *http.Request, parts []string) {
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	terminalID := decodeSegment(parts[0])
	sess, err := s.actions().Find(r.Context(), terminalID)
	if err != nil || sess.ConversationID == "" {
		writeRefusal(w, http.StatusConflict, "session_unavailable", "The Session is unavailable or has no conversation id.")
		return
	}
	conversation := sess.ConversationID
	if len(parts) == 1 && r.Method == http.MethodGet {
		rows, truncated, err := s.workV2().DirectTodos(r.Context(), conversation, true, false)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		items, itemTruncated, err := s.workV2().List(r.Context(), "", conversation, "open", "")
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		recent, recentTruncated, err := s.workV2().RecentlyCompleted(r.Context(), conversation)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		todos := make([]directTodoV2Wire, 0, len(rows))
		for _, td := range rows {
			images, imageErr := s.workV2().DirectTodoImages(r.Context(), td.ID)
			if imageErr != nil {
				s.writeWorkV2Error(w, imageErr)
				return
			}
			todos = append(todos, directTodoWire(td, images))
		}
		assigned := make([]workV2ItemWire, 0, len(items))
		for _, item := range items {
			assigned = append(assigned, s.workV2ItemOf(r.Context(), item))
		}
		completed := make([]workV2ItemWire, 0, len(recent))
		for _, item := range recent {
			completed = append(completed, s.workV2ItemOf(r.Context(), item))
		}
		writeJSON(w, map[string]any{"ok": true, "assigned_items": assigned, "recent_items": completed,
			"direct_todos": todos, "truncated": truncated || itemTruncated || recentTruncated})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPost {
		var body struct {
			Text string `json:"text"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, actor, raw)
		if !ok {
			return
		}
		var answer []byte
		_, err := s.workV2().CreateDirectTodo(r.Context(), app.NewDirectTodoV2{SessionID: conversation, Text: body.Text, Actor: actor},
			func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = directTodoAnswer(td, nil)
				return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[2] == "images" {
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A Session to-do reference image is added with POST.")
			return
		}
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			Title           string `json:"title"`
			DataURL         string `json:"data_url"`
			Position        int64  `json:"position"`
		}
		raw, ok := readWorkV2BodyAtMost(w, r, &body, workV2ImageBodyLimit, "A reference-image request is at most 18 MiB.")
		if !ok {
			return
		}
		bytes, ok := artifacts.DecodeDataURL(body.DataURL)
		if !ok {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
			return
		}
		policy := artifacts.ProductionPolicy
		policy.MaxEncodedBytes = workV2ImageByteLimit
		normalized, normalizeErr := artifacts.Normalize(r.Context(), bytes, policy)
		if normalizeErr != nil {
			var refusal artifacts.Refusal
			if errors.As(normalizeErr, &refusal) {
				writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
			} else {
				writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
			}
			return
		}
		k, ok := s.beginWorkV2Write(w, r, actor, raw)
		if !ok {
			return
		}
		var answer []byte
		_, _, err := s.workV2().AddDirectTodoImage(r.Context(), parts[1], app.AddDirectTodoImageV2{
			ExpectedVersion: body.ExpectedVersion, SessionID: conversation, Title: body.Title,
			Data: normalized.Data, MediaType: normalized.MediaType, Width: normalized.Width, Height: normalized.Height,
			Position: body.Position, Actor: actor,
		}, func(td work.DirectTodoV2, image work.DirectTodoImageV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = directTodoAnswer(td, []work.DirectTodoImageV2{image})
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) != 3 || r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET, POST, or a to-do action.")
		return
	}
	var empty struct{}
	raw, ok := readWorkV2Body(w, r, &empty)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	id, action := parts[1], parts[2]
	var answer []byte
	switch action {
	case "send":
		rows, _, readErr := s.workV2().DirectTodos(r.Context(), conversation, true, false)
		if readErr != nil {
			err = readErr
			break
		}
		var found *work.DirectTodoV2
		for n := range rows {
			if rows[n].ID == id {
				found = &rows[n]
				break
			}
		}
		if found == nil {
			err = &app.WorkError{Status: http.StatusNotFound, Code: "todo_not_found", Message: "No such direct to-do belongs to this Session."}
			break
		}
		if err = app.CheckDirectTodoSend(*found, conversation, time.Now()); err != nil {
			break
		}
		pictures, pictureErr := s.store.DirectTodoV2ImagePayloads(r.Context(), found.ID)
		if refusal, named := referenceImageFileRefusal(pictureErr); named {
			err = refusal
			break
		}
		if pictureErr != nil {
			err = &app.WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable", Message: pictureErr.Error()}
			break
		}
		dataURLs := make([]string, 0, len(pictures))
		for _, picture := range pictures {
			dataURLs = append(dataURLs, "data:"+picture.Image.MediaType+";base64,"+base64.StdEncoding.EncodeToString(picture.Data))
		}
		if _, sendErr := s.actions().SendWithPictures(r.Context(), terminalID,
			app.DirectTodoSendText(found.ID, found.Text), dataURLs); sendErr != nil {
			err = &app.WorkError{Status: http.StatusBadGateway, Code: "send_failed", Message: sendErr.Error()}
			break
		}
		var td work.DirectTodoV2
		td, err = s.workV2().MarkDirectTodoSent(r.Context(), id, conversation, time.Now())
		if err == nil {
			answer = directTodoAnswer(td, nil)
			_ = s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer})
		}
	case "complete":
		_, err = s.workV2().CompleteDirectTodo(r.Context(), id, conversation, actor, true,
			func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = directTodoAnswer(td, nil)
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
	case "delete":
		answer, _ = json.Marshal(map[string]any{"ok": true, "deleted": id})
		err = s.workV2().DeleteDirectTodo(r.Context(), id, conversation, func() (store.ReceiptKey, store.ReceiptAnswer, bool) {
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	default:
		err = &app.WorkError{Status: http.StatusNotFound, Code: "action_not_found", Message: "No such to-do action."}
	}
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

func (s *Server) workV2Agent(w http.ResponseWriter, r *http.Request, parts []string) {
	if !machineAuthed(r) {
		writeRefusal(w, http.StatusUnauthorized, "machine_required", "Only an authenticated Session may use this route.")
		return
	}
	if len(parts) == 1 && parts[0] == "items" && r.Method == http.MethodPost {
		s.agentCreateItem(w, r)
		return
	}
	if len(parts) == 1 && parts[0] == "proposals" && r.Method == http.MethodPost {
		var body struct {
			ProjectID           string `json:"project_id"`
			Kind                string `json:"kind"`
			Title               string `json:"title"`
			Description         string `json:"description"`
			Reason              string `json:"reason"`
			SuggestedAcceptance string `json:"suggested_acceptance"`
			SessionID           string `json:"session_id"`
			SourceWorkID        string `json:"source_work_id"`
			SourceTodoID        string `json:"source_todo_id"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		project, ok := s.workV2Project(r.Context(), body.ProjectID)
		if !ok {
			writeRefusal(w, http.StatusUnprocessableEntity, "project_not_found", "Choose a Project from the current Project catalog.")
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		p, err := s.workV2().Propose(r.Context(), work.ProposalV2{ProjectID: project.ID, ProjectPath: project.Path,
			Kind: work.Kind(body.Kind), Title: body.Title, Description: body.Description, Reason: body.Reason,
			SuggestedAcceptance: body.SuggestedAcceptance, SessionID: body.SessionID,
			SourceWorkID: body.SourceWorkID, SourceTodoID: body.SourceTodoID})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		answer, _ := json.Marshal(map[string]any{"ok": true, "proposal": proposalV2Of(p)})
		if err := s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}); err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "claim" && r.Method == http.MethodPost {
		s.agentClaimItem(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "edit" && r.Method == http.MethodPatch {
		s.workV2Edit(w, r, parts[1], false)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "reopen" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
			Reason          string `json:"reason"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		var answer []byte
		_, err := s.workV2().ReopenIncomplete(r.Context(), parts[1], app.AgentReopenWorkV2{
			ExpectedVersion: body.ExpectedVersion, SessionID: body.SessionID, Reason: body.Reason,
		}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "phase" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion    int64                 `json:"expected_version"`
			SessionID          string                `json:"session_id"`
			Next               string                `json:"next"`
			Verification       string                `json:"verification"`
			Landing            *workV2LandingRequest `json:"landing"`
			Deployment         string                `json:"deployment"`
			NoDeploymentReason string                `json:"no_deployment_reason"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		item, err := s.workV2().Item(r.Context(), parts[1])
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		repo := item.Item.ProjectPath
		if body.Landing != nil && strings.TrimSpace(body.Landing.Project) != "" {
			other, ok := s.workV2Project(r.Context(), strings.TrimSpace(body.Landing.Project))
			if !ok {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				writeRefusal(w, http.StatusUnprocessableEntity, "landing_project_not_found",
					"landing.project names no Project in the current catalog.")
				return
			}
			repo = other.Path
		}
		landing, landingErr := verifyWorkV2DirectLanding(r.Context(), gitadapter.New(), item, body.SessionID, repo, body.Landing)
		if landingErr != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, landingErr)
			return
		}
		var effects []store.Effect
		if work.Phase(body.Next) == work.PhaseDone {
			effects = []store.Effect{workV2CompletionEffect(item, body.SessionID, brokerLanguage(s))}
		}
		var answer []byte
		changed, err := s.workV2().Advance(r.Context(), parts[1], app.AdvanceWorkV2{ExpectedVersion: body.ExpectedVersion,
			SessionID: body.SessionID, Next: work.Phase(body.Next), Verification: body.Verification,
			Landing: landing, Deployment: body.Deployment, NoDeploymentReason: body.NoDeploymentReason,
			Actor: body.SessionID, Effects: effects},
			func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		s.broker.RunEffects(r.Context(), changed.EffectIDs)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) &&
		(parts[2] == "documents" || parts[2] == "steps") && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
			Role            string `json:"role"`
			Title           string `json:"title"`
			Body            string `json:"body"`
			Reference       string `json:"reference"`
			Position        int64  `json:"position"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		var answer []byte
		file := func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		}
		var err error
		if parts[2] == "documents" {
			_, err = s.workV2().AddDocument(r.Context(), parts[1], app.AddDocumentV2{ExpectedVersion: body.ExpectedVersion,
				SessionID: body.SessionID, Role: body.Role, Title: body.Title, Body: body.Body,
				Reference: body.Reference, Position: body.Position}, file)
		} else {
			_, err = s.workV2().AddStep(r.Context(), parts[1], app.AddStepV2{ExpectedVersion: body.ExpectedVersion,
				SessionID: body.SessionID, Title: body.Title, Position: body.Position}, file)
		}
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 5 && parts[0] == "items" && workID(parts[1]) && parts[2] == "steps" &&
		parts[4] == "complete" && r.Method == http.MethodPost {
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			SessionID       string `json:"session_id"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
		if !ok {
			return
		}
		var answer []byte
		_, err := s.workV2().CompleteStep(r.Context(), parts[1], parts[3], body.SessionID, body.ExpectedVersion,
			func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(answer)
		return
	}
	if len(parts) >= 2 && parts[0] == "session-todos" {
		sessionID := parts[1]
		if len(parts) == 2 && r.Method == http.MethodGet {
			rows, truncated, err := s.workV2().DirectTodos(r.Context(), sessionID, false, true)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			out := make([]directTodoV2Wire, 0, len(rows))
			for _, td := range rows {
				images, imageErr := s.workV2().DirectTodoImages(r.Context(), td.ID)
				if imageErr != nil {
					s.writeWorkV2Error(w, imageErr)
					return
				}
				out = append(out, directTodoWire(td, images))
			}
			items, itemTruncated, err := s.workV2().List(r.Context(), "", sessionID, "open", "")
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			assigned := make([]workV2ItemWire, 0, len(items))
			for _, item := range items {
				assigned = append(assigned, s.workV2ItemOf(r.Context(), item))
			}
			recent, recentTruncated, err := s.workV2().RecentlyCompleted(r.Context(), sessionID)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			completed := make([]workV2ItemWire, 0, len(recent))
			for _, item := range recent {
				completed = append(completed, s.workV2ItemOf(r.Context(), item))
			}
			answer := map[string]any{"ok": true, "assigned_items": assigned, "recent_items": completed,
				"direct_todos": out, "truncated": truncated || itemTruncated || recentTruncated}
			s.addUnacknowledgedCompletions(r.Context(), answer, sessionID)
			writeJSON(w, answer)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPost {
			s.agentAddSessionTodos(w, r, sessionID)
			return
		}
		if len(parts) == 4 && parts[3] == "complete" && r.Method == http.MethodPost {
			var body struct{}
			raw, ok := readWorkV2Body(w, r, &body)
			if !ok {
				return
			}
			k, ok := s.beginWorkV2Write(w, r, sessionID, raw)
			if !ok {
				return
			}
			var answer []byte
			_, err := s.workV2().CompleteDirectTodo(r.Context(), parts[2], sessionID, sessionID, false,
				func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
					answer = directTodoAnswer(td, nil)
					return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
				})
			if err != nil {
				_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
				s.writeWorkV2Error(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(answer)
			return
		}
	}
	writeRefusal(w, http.StatusNotFound, "not_found", "No such Agent work-system route.")
}

// unacknowledgedCompletionWire is one completion notice a root has not
// acknowledged, on the list that root reads at every turn boundary.
type unacknowledgedCompletionWire struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	// Kind is the notice's own kind: task_finished, or task_stalled for a
	// child that sat idle after its briefing and was ended spawn_failed.
	Kind        string                    `json:"kind"`
	ResultPath  string                    `json:"result_path"`
	NoticeID    string                    `json:"notice_id"`
	NoticeState string                    `json:"notice_state"`
	LastError   *orchestrator.NoticeError `json:"last_error,omitempty"`
	AckPath     string                    `json:"ack_path"`
}

// addUnacknowledgedCompletions puts on a root's own to-do answer every child
// completion it has not acknowledged — pending or dead-lettered — so a root
// that never saw the typed line learns of it at its next turn boundary
// (orchestrator.RootCompletions). A ledger that cannot be read says so with
// `unacknowledged_completions_unknown` rather than failing the to-dos, which
// are the other half of the answer, or answering an empty list, which would
// read as "nothing of yours finished".
func (s *Server) addUnacknowledgedCompletions(ctx context.Context, answer map[string]any, conversation string) {
	list := []unacknowledgedCompletionWire{}
	if s.broker == nil || s.broker.Store == nil {
		answer["unacknowledged_completions"] = list
		answer["unacknowledged_completions_unknown"] = true
		return
	}
	rows, err := s.broker.RootCompletions(ctx, conversation)
	if err != nil {
		answer["unacknowledged_completions"] = list
		answer["unacknowledged_completions_unknown"] = true
		return
	}
	for _, c := range rows {
		list = append(list, unacknowledgedCompletionWire{
			TaskID: c.Record.ID, Title: c.Record.Title, State: string(c.Record.State),
			Kind:       orchestrator.NoticeKind(c.Record),
			ResultPath: s.broker.ResultPath(c.Record.ID), NoticeID: c.Notice.ID,
			NoticeState: string(c.Notice.State), LastError: c.Notice.LastError,
			AckPath: "/v1/orchestrator/tasks/" + c.Record.ID + "/completion/ack",
		})
	}
	answer["unacknowledged_completions"] = list
}

// agentAddSessionTodos is POST /v1/work/v2/agent/session-todos/<conversation>:
// a Session writing its own to-dos because the person asked it to. The
// conversation must be a live, non-child Session; the batch is written whole
// or not at all, and a replay with the same key answers what was stored.
func (s *Server) agentAddSessionTodos(w http.ResponseWriter, r *http.Request, conversation string) {
	var body struct {
		Todos []struct {
			Text string `json:"text"`
		} `json:"todos"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, conversation, raw)
	if !ok {
		return
	}
	release := func() { _ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k) }
	if s.broker == nil {
		release()
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved",
			"This daemon has no broker to say which Session owns that conversation; nothing was added.")
		return
	}
	if _, err := s.broker.LiveRootSession(r.Context(), conversation); err != nil {
		release()
		if ref, typed := err.(orchestrator.Refusal); typed {
			writeRefusal(w, ref.Status, ref.Code, ref.Message)
			return
		}
		writeRefusal(w, http.StatusServiceUnavailable, "session_unresolved",
			"Which Session owns that conversation could not be read; nothing was added.")
		return
	}
	texts := make([]string, 0, len(body.Todos))
	for _, td := range body.Todos {
		texts = append(texts, td.Text)
	}
	var answer []byte
	_, err := s.workV2().CreateSessionTodos(r.Context(), app.NewSessionTodosV2{SessionID: conversation, Texts: texts},
		func(rows []work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			wires := make([]directTodoV2Wire, 0, len(rows))
			for _, td := range rows {
				wires = append(wires, directTodoWire(td, nil))
			}
			answer, _ = json.Marshal(map[string]any{"ok": true, "todos": wires})
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
	if err != nil {
		release()
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

// agentCreateItem is POST /v1/work/v2/agent/items: a Session creating a Board
// item because its person told it to, in a message sent through Clawdline
// (work-system-v2 §2, amended 2026-09-25). The body names that message's run;
// the run must exist, be recent and have been said to this Session, the
// Session must be a live non-child one, and one run backs at most
// runItemLimit items. An executable item arrives assigned to the Session with
// its steps; nothing is typed into the terminal, because the Session asked
// for it. Every refusal is typed and writes nothing, and a replay with the
// same key answers what was stored.
func (s *Server) agentCreateItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Via       struct {
			Run string `json:"run"`
		} `json:"via"`
		ProjectID        string   `json:"project_id"`
		Kind             string   `json:"kind"`
		Title            string   `json:"title"`
		Description      string   `json:"description"`
		DeploymentPolicy string   `json:"deployment_policy"`
		Steps            []string `json:"steps"`
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
		"A Session creates a Board item only on a person's message sent through Clawdline; name its run as "+
			"{\"via\":{\"run\":\"…\"}}. Without one, file a proposal (POST /v1/work/v2/agent/proposals) for the person to accept.",
		"nothing was created")
	if err != nil {
		refuse(err)
		return
	}
	project, ok := s.workV2Project(r.Context(), body.ProjectID)
	if !ok {
		refuse(&app.WorkError{Status: http.StatusUnprocessableEntity, Code: "project_not_found",
			Message: "Choose a Project from the current Project catalog."})
		return
	}
	sessionProject, _ := projects.CanonicalProjectKey(sess.CWD)
	var answer []byte
	_, err = s.workV2().CreateFromSession(r.Context(), app.NewSessionItemV2{Run: *run, SessionID: sess.ConversationID,
		TerminalID: sess.ID, Assistant: string(sess.Assistant), SessionProject: sessionProject,
		ProjectID: project.ID, ProjectPath: project.Path, Kind: work.Kind(body.Kind), Title: body.Title,
		Description: body.Description, DeploymentPolicy: work.DeploymentPolicy(body.DeploymentPolicy), Steps: body.Steps},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

// relayRefuser answers a relay route's refusal after releasing its receipt,
// so the same key may be tried again once the cause is gone.
func (s *Server) relayRefuser(w http.ResponseWriter, r *http.Request, k store.ReceiptKey) func(error) {
	return func(err error) {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		if ref, typed := err.(orchestrator.Refusal); typed {
			writeRefusal(w, ref.Status, ref.Code, ref.Message)
			return
		}
		s.writeWorkV2Error(w, err)
	}
}

// relaySession is the person's message a Session relays and the live,
// non-child Session relaying it: the run found and checked as a relay's run,
// the Session resolved through the broker. Whether the run was said to that
// Session is checked where the write happens (work.RelayTo). noRun is the
// refusal for a body that names no run; nothing ends each other refusal.
func (s *Server) relaySession(ctx context.Context, runID, sessionID, noRun, nothing string) (*work.Run, session.Session, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, session.Session{}, &app.WorkError{Status: http.StatusForbidden, Code: "run_unknown", Message: noRun}
	}
	run, err := s.relayRun(ctx, runID)
	if err != nil {
		return nil, session.Session{}, err
	}
	if s.broker == nil {
		return nil, session.Session{}, &app.WorkError{Status: http.StatusServiceUnavailable, Code: "session_unresolved",
			Message: "This daemon has no broker to say which Session owns that conversation; " + nothing + "."}
	}
	sess, err := s.broker.LiveRootSession(ctx, sessionID)
	if err != nil {
		if _, typed := err.(orchestrator.Refusal); !typed {
			err = &app.WorkError{Status: http.StatusServiceUnavailable, Code: "session_unresolved",
				Message: "Which Session owns that conversation could not be read; " + nothing + "."}
		}
		return nil, session.Session{}, err
	}
	return run, sess, nil
}

// agentClaimItem is POST /v1/work/v2/agent/items/<id>/claim: a Session taking
// a Board item because its person told it to, in a message sent through
// Clawdline. The item goes to the Session that message was said to — the body
// names no other Session or terminal — through the same Assign a person's
// "existing Session" choice uses, so it reads as the person's assignment
// would, plus the message it was claimed on. The run is checked as
// agentCreateItem checks it; a child, another Project, an item already held
// or finished, a planning kind and a spent run are refused by name, and
// nothing is written. Nothing is typed into the terminal: the Session asked.
func (s *Server) agentClaimItem(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		SessionID       string `json:"session_id"`
		Via             struct {
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
		"A Session claims a Board item only on a person's message sent through Clawdline that tells it to; name "+
			"its run as {\"via\":{\"run\":\"…\"}}. Without one, leave the item for the person to assign.",
		"nothing was claimed")
	if err != nil {
		refuse(err)
		return
	}
	item, err := s.workV2().Item(r.Context(), id)
	if err != nil {
		refuse(err)
		return
	}
	if !sessionInProject(sess, item.Item) {
		refuse(&app.WorkError{Status: http.StatusConflict, Code: "project_mismatch",
			Message: "This Session is not working in that item's Project; nothing was claimed."})
		return
	}
	var answer []byte
	_, err = s.workV2().ClaimFromSession(r.Context(), id, app.ClaimWorkV2{Run: *run, ExpectedVersion: body.ExpectedVersion,
		SessionID: sess.ConversationID, TerminalID: sess.ID, Assistant: string(sess.Assistant)},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(r.Context(), v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	if err != nil {
		refuse(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// sessionInProject is whether a Session works in an item's Project, the check
// a person's "existing Session" assignment and a Session's claim both make.
func sessionInProject(sess session.Session, item work.ItemV2) bool {
	canonical, ok := projects.CanonicalProjectKey(sess.CWD)
	return ok && canonical == item.ProjectPath
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
