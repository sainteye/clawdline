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
	ID               string                 `json:"id"`
	Project          workV2ProjectWire      `json:"project"`
	Kind             string                 `json:"kind"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description"`
	Phase            string                 `json:"phase"`
	Condition        *string                `json:"condition"`
	UserAction       string                 `json:"user_action"`
	Area             string                 `json:"area"`
	DeploymentPolicy string                 `json:"deployment_policy"`
	OwnerSession     *string                `json:"owner_session"`
	CreatedBy        string                 `json:"created_by"`
	CreatedAt        int64                  `json:"created_at"`
	UpdatedAt        int64                  `json:"updated_at"`
	ClosedAt         *int64                 `json:"closed_at"`
	Cycle            int64                  `json:"cycle"`
	Version          int64                  `json:"version"`
	Assignments      []workV2AssignmentWire `json:"assignments,omitempty"`
	Documents        []workV2DocumentWire   `json:"documents,omitempty"`
	Images           []workV2ImageWire      `json:"images,omitempty"`
	Steps            []workV2StepWire       `json:"steps,omitempty"`
	Events           []workV2EventWire      `json:"events,omitempty"`
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
}

type workV2DocumentWire struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Reference string `json:"reference"`
	Position  int64  `json:"position"`
	Version   int64  `json:"version"`
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
}

type workV2GitReader interface {
	ValidBranchName(context.Context, string) bool
	ResolveCommit(context.Context, string, string) (string, error)
	IsAncestor(context.Context, string, string, string) (bool, error)
}

func verifyWorkV2DirectLanding(ctx context.Context, g workV2GitReader, item app.WorkV2View,
	session string, ask *workV2LandingRequest) (*app.VerifiedLandingV2, *app.WorkError) {
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
	repo := item.Item.ProjectPath
	resolved, err := g.ResolveCommit(ctx, repo, commit)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_commit_unresolved",
			Message: "The landing commit does not resolve in the item's Project."}
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
	return &app.VerifiedLandingV2{Commit: resolved, Target: target, TargetCommit: localHead,
		Remote: remote, RemoteCommit: remoteHead}, nil
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
	for _, a := range v.Assignments {
		out.Assignments = append(out.Assignments, workV2AssignmentWire{ID: a.ID, Mode: a.Mode, SessionID: a.SessionID,
			TerminalID: a.TerminalID, Assistant: a.Assistant, Model: a.Model, State: a.State,
			RootAssignment: a.RootAssignment, Failure: a.Failure, CreatedAt: a.CreatedAt.Unix(), UpdatedAt: a.UpdatedAt.Unix()})
	}
	for _, d := range v.Documents {
		out.Documents = append(out.Documents, workV2DocumentWire{ID: d.ID, Role: d.Role, Title: d.Title, Body: d.Body,
			Reference: d.Reference, Position: d.Position, Version: d.Version})
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

func (s *Server) workV2ReferenceImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is read with GET.")
		return
	}
	data, ok, err := s.store.WorkV2ImageBytes(r.Context(), id)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The reference image could not be read.")
		return
	}
	if !ok {
		writeRefusal(w, http.StatusNotFound, "image_not_found", "No reference image has that id.")
		return
	}
	w.Header().Set("Content-Type", "image/png")
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
	q, ok := workQuery(w, r, "project", "owner", "terminal")
	if !ok {
		return
	}
	terminal := q["terminal"] == "true"
	rows, truncated, err := s.workV2().List(r.Context(), q["project"], q["owner"], terminal)
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
		writeRefusal(w, http.StatusBadRequest, "invalid_request", "The body is not a valid work-system request.")
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
		Title: body.Title, Data: normalized.PNG, Width: normalized.Width, Height: normalized.Height,
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
	}
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
	case "unassign":
		out, err = s.workV2().Unassign(r.Context(), id, body.ExpectedVersion, actor, file)
	case "cancel":
		out, err = s.workV2().Cancel(r.Context(), id, body.ExpectedVersion, actor, body.Reason, file)
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
		canonical, ok := projects.CanonicalProjectKey(sess.CWD)
		if !ok || canonical != item.Item.ProjectPath {
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
		brief := fmt.Sprintf("Clawdline assigned you Board item %s: %s. Read its description and reference images, then own it through implementation, verification, Merge, and deployment. Use the work-system v2 Agent API to update it; do not create Board items.", id, item.Item.Title)
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
	if assistant == "" {
		assistant = "codex"
	}
	if model == "" {
		model = "default"
	}
	ra, _, openErr := s.broker.OpenRootAssignment(ctx, requestID, orchestrator.RootAssignmentRequest{
		RequestID: requestID, Assistant: assistant, Model: model, ProjectDir: item.Item.ProjectPath,
		Label: item.Item.Title, Assignment: orchestrator.Assignment{Objective: item.Item.Title,
			Scope: item.Item.Description, Constraints: "Own only this Board item. Do not create Board items.",
			RelevantReferences: "Board item: " + item.Item.ID,
			Acceptance:         "Implement, verify, merge, and deploy according to the item's deployment policy."}})
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
	ID          string            `json:"id"`
	Text        string            `json:"text"`
	CreatedAt   int64             `json:"created_at"`
	SentAt      *int64            `json:"sent_at"`
	ReadAt      *int64            `json:"read_at"`
	CompletedAt *int64            `json:"completed_at"`
	CompletedBy string            `json:"completed_by,omitempty"`
	Version     int64             `json:"version"`
	Images      []workV2ImageWire `json:"images,omitempty"`
}

func directTodoWire(td work.DirectTodoV2, images []work.DirectTodoImageV2) directTodoV2Wire {
	out := directTodoV2Wire{ID: td.ID, Text: td.Text, CreatedAt: td.CreatedAt.Unix(), SentAt: optionalUnix(td.SentAt),
		ReadAt: optionalUnix(td.ReadAt), CompletedAt: optionalUnix(td.CompletedAt), CompletedBy: td.CompletedBy, Version: td.Version}
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
	terminalID := parts[0]
	sess, err := s.actions().Find(r.Context(), terminalID)
	if err != nil || sess.ConversationID == "" {
		writeRefusal(w, http.StatusConflict, "session_unavailable", "The Session is unavailable or has no conversation id.")
		return
	}
	conversation := sess.ConversationID
	if len(parts) == 1 && r.Method == http.MethodGet {
		rows, truncated, err := s.workV2().DirectTodos(r.Context(), conversation, false, false)
		if err != nil {
			s.writeWorkV2Error(w, err)
			return
		}
		items, itemTruncated, err := s.workV2().List(r.Context(), "", conversation, false)
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
			Data: normalized.PNG, Width: normalized.Width, Height: normalized.Height,
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
		pictures, pictureErr := s.store.DirectTodoV2ImagePayloads(r.Context(), found.ID)
		if pictureErr != nil {
			err = &app.WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable", Message: pictureErr.Error()}
			break
		}
		dataURLs := make([]string, 0, len(pictures))
		for _, picture := range pictures {
			dataURLs = append(dataURLs, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(picture.Data))
		}
		if _, sendErr := s.actions().SendWithPictures(r.Context(), terminalID, found.Text, dataURLs); sendErr != nil {
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
	if len(parts) == 3 && parts[0] == "items" && workID(parts[1]) && parts[2] == "edit" && r.Method == http.MethodPatch {
		s.workV2Edit(w, r, parts[1], false)
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
		landing, landingErr := verifyWorkV2DirectLanding(r.Context(), gitadapter.New(), item, body.SessionID, body.Landing)
		if landingErr != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, landingErr)
			return
		}
		var answer []byte
		_, err = s.workV2().Advance(r.Context(), parts[1], app.AdvanceWorkV2{ExpectedVersion: body.ExpectedVersion,
			SessionID: body.SessionID, Next: work.Phase(body.Next), Verification: body.Verification,
			Landing: landing, Deployment: body.Deployment, NoDeploymentReason: body.NoDeploymentReason, Actor: body.SessionID},
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
			items, itemTruncated, err := s.workV2().List(r.Context(), "", sessionID, false)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			assigned := make([]workV2ItemWire, 0, len(items))
			for _, item := range items {
				assigned = append(assigned, s.workV2ItemOf(r.Context(), item))
			}
			writeJSON(w, map[string]any{"ok": true, "assigned_items": assigned, "direct_todos": out,
				"truncated": truncated || itemTruncated})
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
