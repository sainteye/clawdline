package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

const (
	WorkV2PageSize              = 100
	workV2TitleLimit            = 240
	workV2DescriptionLimit      = 64 << 10
	workV2UserActionLimit       = 8 << 10
	workV2CompletionReasonLimit = 8 << 10
	directTodoTextLimit         = 8 << 10
	// A Session writes its own to-dos in batches of at most this many rows.
	sessionTodoBatchLimit = 20
	// The delivered, unfinished to-dos one turn receipt lists, and the
	// characters of each one's text it repeats. The rest is a truncated flag.
	reportOpenTodoLimit     = 20
	reportOpenTodoTextLimit = 120
)

type WorkSystemV2 struct {
	Store *store.Store
	Now   func() time.Time
}

func NewWorkSystemV2(st *store.Store) *WorkSystemV2 { return &WorkSystemV2{Store: st, Now: time.Now} }

func (w *WorkSystemV2) now() time.Time {
	if w.Now != nil {
		return time.Unix(w.Now().Unix(), 0)
	}
	return time.Now().Truncate(time.Second)
}

type WorkV2View struct {
	Item        work.ItemV2
	Assignments []work.AssignmentV2
	Documents   []work.DocumentV2
	Images      []work.ImageV2
	Steps       []work.StepV2
	Events      []work.EventV2
	// EffectIDs are durable side effects recorded by this write. They are not
	// part of the work-system response; the transport hands them to the shared
	// outbox runner after the transaction commits.
	EffectIDs []int64
}

type WorkV2Filer func(WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool)
type TodoV2Filer func(work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool)

func workV2Error(status int, code, message string) error {
	return &WorkError{Status: status, Code: code, Message: message}
}

func mapWorkV2Error(err error) error {
	var typed *WorkError
	if errors.As(err, &typed) {
		return typed
	}
	var refusal work.RefusalV2
	switch {
	case err == nil:
		return nil
	case errors.As(err, &refusal):
		return workV2Error(http.StatusConflict, refusal.Code, refusal.Message)
	case errors.Is(err, store.ErrNoWorkV2):
		return workV2Error(http.StatusNotFound, "work_not_found", "No work item has that id.")
	case errors.Is(err, store.ErrConflict):
		return workV2Error(http.StatusConflict, "version_conflict", "The item changed; reread it before writing.")
	case errors.Is(err, store.ErrWorkV2Full), errors.Is(err, store.ErrPlanningV2Full):
		return workV2Error(http.StatusInsufficientStorage, "work_full", "The work-system capacity is full; nothing was evicted.")
	case errors.Is(err, store.ErrDirectTodoFull):
		return workV2Error(http.StatusInsufficientStorage, "direct_todos_full", "This Session holds as many direct to-dos as it keeps.")
	case errors.Is(err, store.ErrDirectTodoImagesFull):
		return workV2Error(http.StatusInsufficientStorage, "images_full", "This Session to-do already has six reference images.")
	case errors.Is(err, store.ErrDirectTodoImageBytesFull):
		return workV2Error(http.StatusInsufficientStorage, "image_bytes_full", "Session-to-do reference-image storage is full; nothing was evicted.")
	case errors.Is(err, store.ErrWorkV2DocsFull):
		return workV2Error(http.StatusInsufficientStorage, "documents_full", "This item holds as many documents as it keeps.")
	case errors.Is(err, store.ErrWorkV2ImagesFull):
		return workV2Error(http.StatusInsufficientStorage, "images_full", "This item already has six reference images.")
	case errors.Is(err, store.ErrWorkV2ImageBytesFull):
		return workV2Error(http.StatusInsufficientStorage, "image_bytes_full", "Reference-image storage is full; nothing was evicted.")
	case errors.Is(err, store.ErrWorkV2StepsFull):
		return workV2Error(http.StatusInsufficientStorage, "steps_full", "This item holds as many subtasks as it keeps.")
	case errors.Is(err, store.ErrWorkV2ProposalsFull):
		return workV2Error(http.StatusTooManyRequests, "proposals_full", "The proposal inbox is full; no proposal was dropped.")
	case errors.Is(err, store.ErrBusy):
		return workV2Error(http.StatusServiceUnavailable, "store_busy", "The store is busy; nothing was changed.")
	}
	return workV2Error(http.StatusServiceUnavailable, "store_unavailable", err.Error())
}

type NewWorkV2 struct {
	ProjectID        string
	ProjectPath      string
	Kind             work.Kind
	Title            string
	Description      string
	DeploymentPolicy work.DeploymentPolicy
	Actor            string
}

func validateWorkV2Text(title, description string) error {
	switch {
	case len(title) > workV2TitleLimit:
		return workV2Error(http.StatusBadRequest, "invalid_title", "Title is at most 240 characters.")
	case len(description) > workV2DescriptionLimit:
		return workV2Error(http.StatusRequestEntityTooLarge, "description_too_large", "Description is at most 64 KiB.")
	}
	return nil
}

func payload(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (w *WorkSystemV2) Create(ctx context.Context, n NewWorkV2, file WorkV2Filer) (WorkV2View, error) {
	n.ProjectID, n.ProjectPath = strings.TrimSpace(n.ProjectID), strings.TrimSpace(n.ProjectPath)
	n.Title, n.Description = strings.TrimSpace(n.Title), strings.TrimSpace(n.Description)
	if n.DeploymentPolicy == "" {
		n.DeploymentPolicy = work.DeployAgentDecides
	}
	if err := validateWorkV2Text(n.Title, n.Description); err != nil {
		return WorkV2View{}, err
	}
	now := w.now()
	i := work.ItemV2{ID: newWorkID(), ProjectID: n.ProjectID, ProjectPath: n.ProjectPath, Kind: n.Kind,
		Title: n.Title, Description: n.Description, Phase: work.PhaseCreated,
		DeploymentPolicy: n.DeploymentPolicy, CreatedBy: n.Actor, CreatedAt: now, UpdatedAt: now,
		Cycle: 1, Version: 1}
	if err := work.ValidateNewV2(i); err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	v := WorkV2View{Item: i, Assignments: []work.AssignmentV2{}, Documents: []work.DocumentV2{}, Images: []work.ImageV2{},
		Steps: []work.StepV2{}, Events: []work.EventV2{}}
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if existing, ok, err := tx.PristineEquivalentItem(i); err != nil {
			return err
		} else if ok {
			v.Item = existing
			if file != nil {
				if k, a, complete := file(v); complete {
					return tx.CompleteReceipt(k, a)
				}
			}
			return nil
		}
		if err := tx.CreateItem(i, n.Actor, payload(map[string]any{"project_id": i.ProjectID, "kind": i.Kind})); err != nil {
			return err
		}
		if file != nil {
			if k, a, ok := file(v); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return v, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Item(ctx context.Context, id string) (WorkV2View, error) {
	i, err := w.Store.WorkV2Item(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	a, err := w.Store.WorkV2Assignments(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	d, err := w.Store.WorkV2Documents(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	images, err := w.Store.WorkV2Images(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	steps, err := w.Store.WorkV2Steps(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	events, err := w.Store.WorkV2Events(ctx, id, 0, WorkV2PageSize)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	return WorkV2View{Item: i, Assignments: a, Documents: d, Images: images, Steps: steps, Events: events}, nil
}

func (w *WorkSystemV2) List(ctx context.Context, project, owner, status, search string) ([]WorkV2View, bool, error) {
	if status != "open" && status != "done" && status != "all" {
		return nil, false, workV2Error(http.StatusBadRequest, "invalid_status", "Status is open, done or all.")
	}
	items, truncated, err := w.Store.WorkV2Items(ctx, project, owner, status, search, WorkV2PageSize)
	if err != nil {
		return nil, false, mapWorkV2Error(err)
	}
	out := make([]WorkV2View, 0, len(items))
	for _, i := range items {
		documents, documentErr := w.Store.WorkV2Documents(ctx, i.ID)
		if documentErr != nil {
			return nil, false, mapWorkV2Error(documentErr)
		}
		images, imageErr := w.Store.WorkV2Images(ctx, i.ID)
		if imageErr != nil {
			return nil, false, mapWorkV2Error(imageErr)
		}
		steps, stepErr := w.Store.WorkV2Steps(ctx, i.ID)
		if stepErr != nil {
			return nil, false, mapWorkV2Error(stepErr)
		}
		out = append(out, WorkV2View{Item: i, Documents: documents, Images: images, Steps: steps})
	}
	return out, truncated, nil
}

func (w *WorkSystemV2) RecentlyCompleted(ctx context.Context, session string) ([]WorkV2View, bool, error) {
	items, truncated, err := w.Store.CompletedWorkV2ForSession(ctx, session, WorkV2PageSize)
	if err != nil {
		return nil, false, mapWorkV2Error(err)
	}
	out := make([]WorkV2View, 0, len(items))
	for _, i := range items {
		documents, documentErr := w.Store.WorkV2Documents(ctx, i.ID)
		if documentErr != nil {
			return nil, false, mapWorkV2Error(documentErr)
		}
		images, imageErr := w.Store.WorkV2Images(ctx, i.ID)
		if imageErr != nil {
			return nil, false, mapWorkV2Error(imageErr)
		}
		steps, stepErr := w.Store.WorkV2Steps(ctx, i.ID)
		if stepErr != nil {
			return nil, false, mapWorkV2Error(stepErr)
		}
		out = append(out, WorkV2View{Item: i, Documents: documents, Images: images, Steps: steps})
	}
	return out, truncated, nil
}

type AddImageV2 struct {
	ExpectedVersion int64
	Title           string
	Data            []byte
	Width           int
	Height          int
	Position        int64
	Actor           string
}

// AddImage stores a person-supplied reference. Sessions can read references
// with the item, but only a person route can call this command.
func (w *WorkSystemV2) AddImage(ctx context.Context, id string, c AddImageV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.Phase.Terminal() {
			return work.RefuseV2("item_terminal", "Reopen terminal work before changing its reference images.")
		}
		c.Title = strings.TrimSpace(c.Title)
		if c.Title == "" {
			c.Title = "reference image"
		}
		if len(c.Title) > workV2TitleLimit {
			return work.RefuseV2("image_title_too_large", "A reference-image title is at most 240 bytes.")
		}
		if len(c.Data) == 0 || c.Width <= 0 || c.Height <= 0 {
			return work.RefuseV2("invalid_image", "A reference image needs decoded PNG pixels.")
		}
		now := w.now()
		image := work.ImageV2{ID: newWorkID(), WorkID: id, Title: c.Title, MediaType: "image/png",
			ByteCount: int64(len(c.Data)), Width: c.Width, Height: c.Height, Position: c.Position,
			CreatedBy: c.Actor, CreatedAt: now}
		if err := tx.AddImage(image, c.Data); err != nil {
			return err
		}
		next := prev
		next.UpdatedAt = now
		if err := tx.PutItem(prev, next, "image.added", c.Actor, payload(map[string]any{
			"image_id": image.ID, "title": image.Title, "byte_count": image.ByteCount,
			"width": image.Width, "height": image.Height})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Images: []work.ImageV2{image}}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) DeleteImage(ctx context.Context, id, imageID string, expected int64, actor string, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != expected {
			return store.ErrConflict
		}
		if prev.Phase.Terminal() {
			return work.RefuseV2("item_terminal", "Reopen terminal work before changing its reference images.")
		}
		image, err := tx.Image(imageID)
		if err != nil || image.WorkID != id {
			return work.RefuseV2("image_not_found", "No such reference image belongs to this item.")
		}
		if err := tx.DeleteImage(imageID); err != nil {
			return err
		}
		next := prev
		next.UpdatedAt = w.now()
		if err := tx.PutItem(prev, next, "image.deleted", actor, payload(map[string]string{"image_id": imageID})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type EditWorkV2 struct {
	ExpectedVersion int64
	Title           *string
	Description     *string
	Condition       *work.Condition
	UserAction      *string
	Actor           string
	OwnerSession    string
	Person          bool
}

func (w *WorkSystemV2) Edit(ctx context.Context, id string, c EditWorkV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if !c.Person && (prev.OwnerSession == "" || prev.OwnerSession != c.OwnerSession || prev.Phase.Terminal()) {
			return work.RefuseV2("not_item_owner", "Only the owning Session may edit this item.")
		}
		next := prev
		if c.Title != nil {
			next.Title = strings.TrimSpace(*c.Title)
		}
		if c.Description != nil {
			next.Description = strings.TrimSpace(*c.Description)
		}
		if err := validateWorkV2Text(next.Title, next.Description); err != nil {
			return err
		}
		if next.Title == "" || next.Description == "" {
			return work.RefuseV2("content_required", "Title and description cannot be blank.")
		}
		if c.Condition != nil {
			if !c.Condition.Valid() {
				return work.RefuseV2("invalid_condition", "That condition is not part of the work lifecycle.")
			}
			if !c.Person && !c.Condition.AgentOwned() {
				return work.RefuseV2("condition_not_agent_owned", "The Agent may set only blocked or waiting_user.")
			}
			next.Condition = *c.Condition
			if next.Condition != work.ConditionWaitingUser {
				next.UserAction = ""
			}
		}
		if c.UserAction != nil {
			next.UserAction = strings.TrimSpace(*c.UserAction)
			if len(next.UserAction) > workV2UserActionLimit {
				return work.RefuseV2("user_action_too_large", "The requested user action is at most 8 KiB.")
			}
		}
		if c.Condition != nil || c.UserAction != nil {
			switch {
			case next.Condition == work.ConditionWaitingUser && next.UserAction == "":
				return work.RefuseV2("user_action_required", "Say exactly what the person needs to do while waiting for them.")
			case next.Condition != work.ConditionWaitingUser && next.UserAction != "":
				return work.RefuseV2("user_action_requires_waiting_user", "A requested user action belongs to the waiting_user condition.")
			}
		}
		next.UpdatedAt = w.now()
		if err := tx.PutItem(prev, next, "item.edited", c.Actor, payload(map[string]any{"title": c.Title != nil,
			"description": c.Description != nil, "condition": c.Condition, "user_action": c.UserAction != nil})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type AssignWorkV2 struct {
	ExpectedVersion int64
	Mode            string
	SessionID       string
	TerminalID      string
	Assistant       string
	Model           string
	Actor           string
	AssignmentID    string
}

func descriptionStepTitles(description string) []string {
	titles := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		start := -1
		if len(line) >= 2 && strings.ContainsRune("-*+", rune(line[0])) && (line[1] == ' ' || line[1] == '\t') {
			start = 2
		} else {
			i := 0
			for i < len(line) && line[i] >= '0' && line[i] <= '9' {
				i++
			}
			if i > 0 && i+1 < len(line) && (line[i] == '.' || line[i] == ')') && (line[i+1] == ' ' || line[i+1] == '\t') {
				start = i + 2
			}
		}
		if start < 0 {
			continue
		}
		title := strings.TrimSpace(line[start:])
		if title == "" {
			continue
		}
		if len(title) > workV2TitleLimit {
			end := workV2TitleLimit - len("…")
			for end > 0 && !utf8.ValidString(title[:end]) {
				end--
			}
			title = strings.TrimSpace(title[:end]) + "…"
		}
		titles = append(titles, title)
	}
	if len(titles) < 2 {
		return nil
	}
	return titles
}

func seedDescriptionSteps(tx *store.WorkV2Tx, item work.ItemV2, session string, at time.Time) ([]work.StepV2, error) {
	existing, err := tx.Steps(item.ID)
	if err != nil || len(existing) > 0 {
		return nil, err
	}
	titles := descriptionStepTitles(item.Description)
	steps := make([]work.StepV2, 0, len(titles))
	for position, title := range titles {
		step := work.StepV2{ID: newWorkID(), WorkID: item.ID, Title: title, Position: int64(position),
			CreatedBy: session, CreatedAt: at, Version: 1}
		if err := tx.AddStep(step); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (w *WorkSystemV2) Assign(ctx context.Context, id string, c AssignWorkV2, pending bool, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.Planning() {
			return work.RefuseV2("planning_not_assignable", "Epic, Refactor, and Plan stay in Planning.")
		}
		if prev.Phase.Terminal() {
			return work.RefuseV2("item_terminal", "Reopen terminal work before assigning it.")
		}
		if c.AssignmentID == "" {
			c.AssignmentID = newWorkID()
		}
		now := w.now()
		state := "active"
		if pending {
			state = "assigning"
		}
		a := work.AssignmentV2{ID: c.AssignmentID, WorkID: id, Mode: c.Mode, SessionID: c.SessionID,
			TerminalID: c.TerminalID, Assistant: c.Assistant, Model: c.Model, State: state,
			HumanActor: c.Actor, CreatedAt: now, UpdatedAt: now}
		if !pending && strings.TrimSpace(a.SessionID) == "" {
			return work.RefuseV2("session_required", "An active assignment needs a Session conversation id.")
		}
		if pending && c.Mode != "new_session" {
			return work.RefuseV2("invalid_assignment", "Only a new Session has an assigning state.")
		}
		old, err := tx.ActiveAssignment(id)
		if err != nil {
			return err
		}
		if !pending && old.ID != "" {
			old.State, old.ReleasedAt, old.UpdatedAt = "released", now, now
			if err := tx.UpdateAssignment(old); err != nil {
				return err
			}
		}
		if err := tx.CreateAssignment(a); err != nil {
			return err
		}
		seeded := []work.StepV2{}
		if !pending {
			seeded, err = seedDescriptionSteps(tx, prev, a.SessionID, now)
			if err != nil {
				return err
			}
		}
		next := prev
		if pending {
			if old.ID == "" && prev.Phase == work.PhaseCreated {
				next.Phase = work.PhaseAssigning
			}
		} else {
			next.OwnerSession = a.SessionID
			next.Condition = ""
			next.UserAction = ""
			if prev.Phase == work.PhaseCreated || prev.Phase == work.PhaseAssigning {
				next.Phase = work.PhaseAssigned
			}
		}
		next.UpdatedAt = now
		if err := tx.PutItem(prev, next, "item.assigned", c.Actor, payload(map[string]any{
			"assignment_id": a.ID, "mode": a.Mode, "pending": pending, "session_id": a.SessionID,
			"seeded_steps": len(seeded)})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Assignments: []work.AssignmentV2{a}, Steps: seeded}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type FinishAssignmentV2 struct {
	AssignmentID   string
	SessionID      string
	TerminalID     string
	RootAssignment string
	Failure        string
	Actor          string
}

func (w *WorkSystemV2) FinishAssignment(ctx context.Context, workID string, c FinishAssignmentV2) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(workID)
		if err != nil {
			return err
		}
		pending, err := tx.Assignment(c.AssignmentID)
		if err != nil || pending.WorkID != workID || pending.State != "assigning" {
			return work.RefuseV2("assignment_not_pending", "No pending assignment has that id.")
		}
		now := w.now()
		next := prev
		pending.UpdatedAt, pending.RootAssignment = now, c.RootAssignment
		if c.Failure != "" {
			pending.State, pending.Failure = "failed", c.Failure
			if prev.OwnerSession == "" {
				next.Phase, next.Condition = work.PhaseCreated, work.ConditionAssignmentFailed
				next.UserAction = ""
			}
		} else {
			if strings.TrimSpace(c.SessionID) == "" {
				return work.RefuseV2("session_required", "A successful assignment resolved no conversation id.")
			}
			old, err := tx.ActiveAssignment(workID)
			if err != nil {
				return err
			}
			if old.ID != "" {
				old.State, old.ReleasedAt, old.UpdatedAt = "released", now, now
				if err := tx.UpdateAssignment(old); err != nil {
					return err
				}
			}
			pending.State, pending.SessionID, pending.TerminalID = "active", c.SessionID, c.TerminalID
			next.OwnerSession, next.Condition, next.UserAction = c.SessionID, "", ""
			if prev.Phase == work.PhaseCreated || prev.Phase == work.PhaseAssigning {
				next.Phase = work.PhaseAssigned
			}
		}
		seeded := []work.StepV2{}
		if c.Failure == "" {
			seeded, err = seedDescriptionSteps(tx, prev, c.SessionID, now)
			if err != nil {
				return err
			}
		}
		if err := tx.UpdateAssignment(pending); err != nil {
			return err
		}
		next.UpdatedAt = now
		kind := "assignment.activated"
		if c.Failure != "" {
			kind = "assignment.failed"
		}
		if err := tx.PutItem(prev, next, kind, c.Actor, payload(map[string]any{
			"assignment_id": pending.ID, "root_assignment_id": c.RootAssignment, "failure": c.Failure,
			"seeded_steps": len(seeded)})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Assignments: []work.AssignmentV2{pending}, Steps: seeded}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Unassign(ctx context.Context, id string, expected int64, actor string, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != expected {
			return store.ErrConflict
		}
		a, err := tx.ActiveAssignment(id)
		if err != nil {
			return err
		}
		if a.ID == "" {
			return work.RefuseV2("item_unassigned", "This item has no active owner.")
		}
		now := w.now()
		a.State, a.ReleasedAt, a.UpdatedAt = "released", now, now
		if err := tx.UpdateAssignment(a); err != nil {
			return err
		}
		next := prev
		next.OwnerSession, next.Condition, next.UserAction, next.UpdatedAt = "", work.ConditionOwnerRequired, "", now
		if err := tx.PutItem(prev, next, "item.unassigned", actor, payload(map[string]any{"assignment_id": a.ID})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type AdvanceWorkV2 struct {
	ExpectedVersion    int64
	SessionID          string
	Next               work.Phase
	Verification       string
	Landing            *VerifiedLandingV2
	Deployment         string
	NoDeploymentReason string
	Actor              string
	Effects            []store.Effect
}

// VerifiedLandingV2 is a Git reading made by the HTTP adapter, never the
// owning Agent's unverified spelling. Direct Session ownership has no broker
// task, so this is its equivalent durable receipt: one resolved commit is on
// both the local target and the repository's remote-tracking target.
type VerifiedLandingV2 struct {
	Commit       string `json:"commit"`
	Target       string `json:"target"`
	TargetCommit string `json:"target_commit"`
	Remote       string `json:"remote"`
	RemoteCommit string `json:"remote_commit"`
	// Repository is the other catalog Project's path when the work landed
	// outside the item's own Project — a backend item whose change was a
	// frontend commit. Empty means the item's Project.
	Repository string `json:"repository,omitempty"`
}

func (l *VerifiedLandingV2) complete() bool {
	return l != nil && l.Commit != "" && l.Target != "" && l.TargetCommit != "" &&
		l.Remote != "" && l.RemoteCommit != ""
}

func (w *WorkSystemV2) Advance(ctx context.Context, id string, c AdvanceWorkV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.OwnerSession != c.SessionID || c.SessionID == "" {
			return work.RefuseV2("not_item_owner", "Only the owning Session may advance this item.")
		}
		rows, err := tx.Tasks(id)
		if err != nil {
			return err
		}
		facts, unknown := Facts(rows)
		if unknown > 0 {
			return work.RefuseV2("evidence_unknown", "A broker task bound to this item is unreadable.")
		}
		hasLanding := c.Landing.complete()
		for _, f := range facts {
			if work.OutcomeOf(f) == work.OutcomeLanded {
				hasLanding = true
				break
			}
		}
		if err := work.AgentTransition(prev, c.Next, strings.TrimSpace(c.Verification) != "", hasLanding,
			strings.TrimSpace(c.Deployment) != "", strings.TrimSpace(c.NoDeploymentReason) != ""); err != nil {
			return err
		}
		if c.Next == work.PhaseDone {
			steps, err := tx.Steps(id)
			if err != nil {
				return err
			}
			incomplete := 0
			for _, step := range steps {
				if !step.Done {
					incomplete++
				}
			}
			if incomplete > 0 {
				return work.RefuseV2("steps_incomplete", fmt.Sprintf("Complete all item TODOs before closing this work; %d remain.", incomplete))
			}
		}
		now := w.now()
		next := prev
		next.Phase, next.Condition, next.UserAction, next.UpdatedAt = c.Next, "", "", now
		if c.Next == work.PhaseDone {
			next.ClosedAt, next.OwnerSession = now, ""
			a, err := tx.ActiveAssignment(id)
			if err != nil {
				return err
			}
			if a.ID != "" {
				a.State, a.ReleasedAt, a.UpdatedAt = "released", now, now
				if err := tx.UpdateAssignment(a); err != nil {
					return err
				}
			}
		}
		if err := tx.PutItem(prev, next, "item.phase_changed", c.Actor, payload(map[string]any{
			"from": prev.Phase, "to": c.Next, "verification": c.Verification,
			"landing": c.Landing, "deployment": c.Deployment, "no_deployment_reason": c.NoDeploymentReason})); err != nil {
			return err
		}
		next.Version++
		var effectIDs []int64
		if c.Next == work.PhaseDone {
			for _, effect := range c.Effects {
				id, err := tx.AddEffect(effect)
				if err != nil {
					return err
				}
				effectIDs = append(effectIDs, id)
			}
		}
		out = WorkV2View{Item: next, EffectIDs: effectIDs}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Cancel(ctx context.Context, id string, expected int64, actor, reason string, file WorkV2Filer) (WorkV2View, error) {
	if strings.TrimSpace(reason) == "" {
		return WorkV2View{}, workV2Error(http.StatusBadRequest, "reason_required", "Cancellation needs a reason.")
	}
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != expected {
			return store.ErrConflict
		}
		if prev.Phase.Terminal() {
			return work.RefuseV2("item_terminal", "This item is already terminal.")
		}
		now := w.now()
		if a, err := tx.ActiveAssignment(id); err != nil {
			return err
		} else if a.ID != "" {
			a.State, a.ReleasedAt, a.UpdatedAt = "released", now, now
			if err := tx.UpdateAssignment(a); err != nil {
				return err
			}
		}
		next := prev
		next.Phase, next.OwnerSession, next.Condition, next.UserAction = work.PhaseCancelled, "", "", ""
		next.ClosedAt, next.UpdatedAt = now, now
		if err := tx.PutItem(prev, next, "item.cancelled", actor, payload(map[string]string{"reason": reason})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Reopen(ctx context.Context, id string, expected int64, actor string, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != expected {
			return store.ErrConflict
		}
		if !prev.Phase.Terminal() {
			return work.RefuseV2("item_not_terminal", "Only completed or cancelled work is reopened.")
		}
		next := prev
		next.Phase, next.Condition, next.UserAction, next.OwnerSession = work.PhaseCreated, work.ConditionOwnerRequired, "", ""
		next.ClosedAt, next.UpdatedAt, next.Cycle = time.Time{}, w.now(), prev.Cycle+1
		if err := tx.PutItem(prev, next, "item.reopened", actor, payload(map[string]int64{"cycle": next.Cycle})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

// AgentReopenWorkV2 is the narrow correction path for a Session that has just
// declared its own item done and then learns from the person that the result is
// still incomplete. It is deliberately separate from person-owned reopening:
// an Agent cannot reverse a cancellation or take somebody else's completion.
type AgentReopenWorkV2 struct {
	ExpectedVersion int64
	SessionID       string
	Reason          string
}

func (w *WorkSystemV2) ReopenIncomplete(ctx context.Context, id string, c AgentReopenWorkV2,
	file WorkV2Filer) (WorkV2View, error) {
	c.SessionID, c.Reason = strings.TrimSpace(c.SessionID), strings.TrimSpace(c.Reason)
	if c.Reason == "" {
		return WorkV2View{}, workV2Error(http.StatusBadRequest, "reason_required",
			"Retracting a completion needs the concrete fact that remains unfinished.")
	}
	if len(c.Reason) > workV2CompletionReasonLimit {
		return WorkV2View{}, workV2Error(http.StatusRequestEntityTooLarge, "reason_too_large",
			"A completion-correction reason is at most 8 KiB.")
	}
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.Phase != work.PhaseDone {
			return work.RefuseV2("item_not_done", "Only completed work can have its completion retracted by an Agent.")
		}
		completed, err := tx.LatestAssignment(id)
		if err != nil {
			return err
		}
		if c.SessionID == "" || completed.SessionID != c.SessionID || completed.State != "released" ||
			completed.ReleasedAt.IsZero() || !completed.ReleasedAt.Equal(prev.ClosedAt) {
			return work.RefuseV2("not_completing_session",
				"Only the Session that recorded this completion may retract it after user feedback.")
		}
		active, err := tx.ActiveAssignment(id)
		if err != nil {
			return err
		}
		if active.ID != "" {
			return work.RefuseV2("item_already_assigned", "Completed work already has an active assignment.")
		}
		now := w.now()
		assignment := work.AssignmentV2{ID: newWorkID(), WorkID: id, Mode: "existing_session",
			SessionID: c.SessionID, TerminalID: completed.TerminalID, Assistant: completed.Assistant,
			Model: completed.Model, State: "active", HumanActor: c.SessionID,
			RootAssignment: completed.RootAssignment, CreatedAt: now, UpdatedAt: now}
		if err := tx.CreateAssignment(assignment); err != nil {
			return err
		}
		next := prev
		next.Phase, next.Condition, next.UserAction = work.PhaseImplementing, "", ""
		next.OwnerSession, next.ClosedAt, next.UpdatedAt = c.SessionID, time.Time{}, now
		next.Cycle = prev.Cycle + 1
		if err := tx.PutItem(prev, next, "item.completion_retracted", c.SessionID, payload(map[string]any{
			"assignment_id": assignment.ID, "previous_assignment_id": completed.ID,
			"cycle": next.Cycle, "reason": c.Reason,
		})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Assignments: []work.AssignmentV2{assignment}}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type AddDocumentV2 struct {
	ExpectedVersion int64
	SessionID       string
	Role            string
	Title           string
	Body            string
	Reference       string
	Position        int64
}

func (w *WorkSystemV2) AddDocument(ctx context.Context, id string, c AddDocumentV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.OwnerSession == "" || prev.OwnerSession != c.SessionID || prev.Phase.Terminal() {
			return work.RefuseV2("not_item_owner", "Only the owning Session may add item documents.")
		}
		c.Title, c.Body, c.Reference = strings.TrimSpace(c.Title), strings.TrimSpace(c.Body), strings.TrimSpace(c.Reference)
		if c.Title == "" || (c.Body == "" && c.Reference == "") {
			return work.RefuseV2("document_content_required", "A document needs a title and either body or reference.")
		}
		if len(c.Title) > workV2TitleLimit || len(c.Body) > workV2DescriptionLimit {
			return work.RefuseV2("document_too_large", "A document title is at most 240 bytes and its body at most 64 KiB.")
		}
		if c.Reference != "" && !strings.Contains(c.Reference, "://") {
			clean := filepath.Clean(c.Reference)
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return work.RefuseV2("document_reference_outside_project", "A repository document reference must stay inside the Project.")
			}
		}
		switch c.Role {
		case "spec", "design", "test", "deploy", "completion_report", "other":
		default:
			return work.RefuseV2("invalid_document_role", "That document role is not supported.")
		}
		now := w.now()
		doc := work.DocumentV2{ID: newWorkID(), WorkID: id, Role: c.Role, Title: c.Title, Body: c.Body,
			Reference: c.Reference, Position: c.Position, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.AddDocument(doc); err != nil {
			return err
		}
		next := prev
		next.UpdatedAt = now
		if err := tx.PutItem(prev, next, "document.added", c.SessionID, payload(map[string]string{"document_id": doc.ID, "role": doc.Role})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Documents: []work.DocumentV2{doc}}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type AddStepV2 struct {
	ExpectedVersion  int64
	SessionID, Title string
	Position         int64
}

func (w *WorkSystemV2) AddStep(ctx context.Context, id string, c AddStepV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.OwnerSession == "" || prev.OwnerSession != c.SessionID || prev.Phase.Terminal() {
			return work.RefuseV2("not_item_owner", "Only the owning Session may add item steps.")
		}
		c.Title = strings.TrimSpace(c.Title)
		if c.Title == "" {
			return work.RefuseV2("step_title_required", "A subtask needs a title.")
		}
		if len(c.Title) > workV2TitleLimit {
			return work.RefuseV2("step_title_too_large", "A subtask title is at most 240 bytes.")
		}
		now := w.now()
		step := work.StepV2{ID: newWorkID(), WorkID: id, Title: c.Title, Position: c.Position, CreatedBy: c.SessionID, CreatedAt: now, Version: 1}
		if err := tx.AddStep(step); err != nil {
			return err
		}
		next := prev
		next.UpdatedAt = now
		if err := tx.PutItem(prev, next, "step.added", c.SessionID, payload(map[string]string{"step_id": step.ID})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Steps: []work.StepV2{step}}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) CompleteStep(ctx context.Context, id, stepID, session string, expected int64, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != expected {
			return store.ErrConflict
		}
		if prev.OwnerSession == "" || prev.OwnerSession != session || prev.Phase.Terminal() {
			return work.RefuseV2("not_item_owner", "Only the owning Session may complete item steps.")
		}
		step, err := tx.Step(stepID)
		if err != nil || step.WorkID != id {
			return work.RefuseV2("step_not_found", "No such subtask belongs to this item.")
		}
		if !step.Done {
			stepNext := step
			stepNext.Done, stepNext.CompletedAt, stepNext.CompletedBy = true, w.now(), session
			if err := tx.PutStep(step, stepNext); err != nil {
				return err
			}
			stepNext.Version++
			step = stepNext
		}
		next := prev
		next.UpdatedAt = w.now()
		if err := tx.PutItem(prev, next, "step.completed", session, payload(map[string]string{"step_id": step.ID})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Steps: []work.StepV2{step}}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

type NewDirectTodoV2 struct {
	SessionID string
	Text      string
	Actor     string
}

func (w *WorkSystemV2) CreateDirectTodo(ctx context.Context, n NewDirectTodoV2, file TodoV2Filer) (work.DirectTodoV2, error) {
	n.Text = strings.TrimSpace(n.Text)
	if n.SessionID == "" || n.Text == "" {
		return work.DirectTodoV2{}, workV2Error(http.StatusBadRequest, "todo_text_required", "Choose a Session and enter what it should do.")
	}
	if len(n.Text) > directTodoTextLimit {
		return work.DirectTodoV2{}, workV2Error(http.StatusRequestEntityTooLarge, "todo_too_large", "A direct to-do is at most 8 KiB.")
	}
	td := work.DirectTodoV2{ID: newWorkID(), SessionID: n.SessionID, Text: n.Text, CreatedBy: n.Actor,
		CreatedAt: w.now(), Version: 1}
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateDirectTodo(td); err != nil {
			return err
		}
		if file != nil {
			if k, ans, ok := file(td); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return td, mapWorkV2Error(err)
}

// NewSessionTodosV2 is a Session writing its own to-dos, which it does only
// when the person asked it to. The conversation id is both the owner and the
// provenance: a row whose CreatedBy is its own SessionID was written by that
// Session, not sent by the person.
type NewSessionTodosV2 struct {
	SessionID string
	Texts     []string
}

type TodosV2Filer func([]work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool)

// CreateSessionTodos writes the whole batch in one transaction or nothing.
// The rows count as already read: the Session wrote them, so there is no
// delivery for it to observe.
func (w *WorkSystemV2) CreateSessionTodos(ctx context.Context, n NewSessionTodosV2, file TodosV2Filer) ([]work.DirectTodoV2, error) {
	if n.SessionID == "" {
		return nil, workV2Error(http.StatusBadRequest, "session_required", "Name the Session the to-dos belong to.")
	}
	switch {
	case len(n.Texts) == 0:
		return nil, workV2Error(http.StatusBadRequest, "todos_required", "Send at least one to-do.")
	case len(n.Texts) > sessionTodoBatchLimit:
		return nil, workV2Error(http.StatusRequestEntityTooLarge, "too_many_todos",
			fmt.Sprintf("One call adds at most %d to-dos; nothing was added.", sessionTodoBatchLimit))
	}
	now := w.now()
	rows := make([]work.DirectTodoV2, 0, len(n.Texts))
	for i, text := range n.Texts {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, workV2Error(http.StatusBadRequest, "todo_text_required",
				fmt.Sprintf("To-do %d is empty; nothing was added.", i+1))
		}
		if len(text) > directTodoTextLimit {
			return nil, workV2Error(http.StatusRequestEntityTooLarge, "todo_too_large",
				fmt.Sprintf("To-do %d is over 8 KiB; nothing was added.", i+1))
		}
		rows = append(rows, work.DirectTodoV2{ID: newWorkID(), SessionID: n.SessionID, Text: text,
			CreatedBy: n.SessionID, CreatedAt: now, ReadAt: now, Version: 1})
	}
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.CreateDirectTodos(rows); err != nil {
			return err
		}
		if file != nil {
			if k, ans, ok := file(rows); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapWorkV2Error(err)
	}
	return rows, nil
}

func (w *WorkSystemV2) DirectTodos(ctx context.Context, session string, includeCompleted, markRead bool) ([]work.DirectTodoV2, bool, error) {
	rows, truncated, err := w.Store.DirectTodosV2(ctx, session, includeCompleted, markRead, WorkV2PageSize)
	return rows, truncated, mapWorkV2Error(err)
}

type AddDirectTodoImageV2 struct {
	ExpectedVersion int64
	SessionID       string
	Title           string
	Data            []byte
	Width           int
	Height          int
	Position        int64
	Actor           string
}

type TodoImageV2Filer func(work.DirectTodoV2, work.DirectTodoImageV2) (store.ReceiptKey, store.ReceiptAnswer, bool)

func (w *WorkSystemV2) AddDirectTodoImage(ctx context.Context, id string, c AddDirectTodoImageV2,
	file TodoImageV2Filer) (work.DirectTodoV2, work.DirectTodoImageV2, error) {
	var out work.DirectTodoV2
	var image work.DirectTodoImageV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != c.SessionID {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if !prev.Open() || !prev.SentAt.IsZero() {
			return work.RefuseV2("todo_already_delivered", "Reference images can be added only before a to-do is sent or completed.")
		}
		c.Title = strings.TrimSpace(c.Title)
		if c.Title == "" {
			c.Title = "reference image"
		}
		if len(c.Title) > workV2TitleLimit {
			return work.RefuseV2("image_title_too_large", "A reference-image title is at most 240 bytes.")
		}
		if len(c.Data) == 0 || c.Width <= 0 || c.Height <= 0 {
			return work.RefuseV2("invalid_image", "A reference image needs decoded PNG pixels.")
		}
		image = work.DirectTodoImageV2{ID: newWorkID(), TodoID: id, Title: c.Title, MediaType: "image/png",
			ByteCount: int64(len(c.Data)), Width: c.Width, Height: c.Height, Position: c.Position,
			CreatedBy: c.Actor, CreatedAt: w.now()}
		if err := tx.AddDirectTodoImage(image, c.Data); err != nil {
			return err
		}
		if err := tx.PutDirectTodo(prev, prev); err != nil {
			return err
		}
		out = prev
		out.Version++
		if file != nil {
			if k, a, ok := file(out, image); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, image, mapWorkV2Error(err)
}

func (w *WorkSystemV2) DirectTodoImages(ctx context.Context, id string) ([]work.DirectTodoImageV2, error) {
	images, err := w.Store.DirectTodoV2Images(ctx, id)
	return images, mapWorkV2Error(err)
}

// DirectTodoResendAfter is how long an unread delivery is protected from a
// second one. Inside it a second Send is a double tap, or the same press from
// a second screen, and would type the words twice. Past it the Session has
// had its chance: a Session that never reads its queue — one that was busy,
// or does not follow the guide — used to leave the row unsendable for good.
const DirectTodoResendAfter = 2 * time.Minute

// CheckDirectTodoSend distinguishes a first delivery from a reminder. A
// delivery that has not been observed cannot be doubled until
// DirectTodoResendAfter has passed; once the Session has read its queue, the
// person may remind it again while the row is still open.
func CheckDirectTodoSend(td work.DirectTodoV2, session string, now time.Time) error {
	if td.SessionID != session {
		return workV2Error(http.StatusConflict, "todo_session_mismatch", "That to-do belongs to another Session.")
	}
	if !td.Open() {
		return workV2Error(http.StatusConflict, "todo_completed", "A completed to-do is not sent again.")
	}
	if !td.SentAt.IsZero() && td.ReadAt.IsZero() && now.Sub(td.SentAt) < DirectTodoResendAfter {
		return workV2Error(http.StatusConflict, "todo_awaiting_read", "This delivery was sent moments ago and the Session has not read it yet.")
	}
	return nil
}

// DirectTodoSendText is what Send types into a Session for a direct to-do:
// the person's words unchanged, then one line naming the row and the command
// that checks it off. Without that line the Session reads an ordinary message
// and, once the work is done, has no way to tell which row it finished
// (measured 2026-09-25: fixed, deployed and reported, with the to-do left
// open). Only trailing line breaks are dropped, so exactly one blank line
// separates the words from the line.
func DirectTodoSendText(id, text string) string {
	line := "(Clawdline to-do " + id + ". When it is done: clawdline todo done " + id + ")"
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return line
	}
	return text + "\n\n" + line
}

// OpenDeliveredTodos are this Session's direct to-dos that were sent or read
// and are not completed, oldest first: what a turn receipt reminds the
// Session to check off. It reads without marking anything read — a receipt
// is not the Session reading its queue. The answer holds at most
// reportOpenTodoLimit rows; truncated says there were more, or that the
// bounded read of open rows stopped before it saw them all.
func (w *WorkSystemV2) OpenDeliveredTodos(ctx context.Context, session string) ([]work.DirectTodoV2, bool, error) {
	rows, truncated, err := w.DirectTodos(ctx, session, false, false)
	if err != nil {
		return nil, false, err
	}
	out := make([]work.DirectTodoV2, 0, len(rows))
	for _, td := range rows {
		if !td.Open() || (td.SentAt.IsZero() && td.ReadAt.IsZero()) {
			continue
		}
		if len(out) == reportOpenTodoLimit {
			return out, true, nil
		}
		out = append(out, td)
	}
	return out, truncated, nil
}

// TodoPreview is a to-do's text cut to reportOpenTodoTextLimit characters,
// never inside one, with its line breaks folded so it prints as one line.
func TodoPreview(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= reportOpenTodoTextLimit {
		return text
	}
	r := []rune(text)
	return string(r[:reportOpenTodoTextLimit-1]) + "…"
}

func (w *WorkSystemV2) MarkDirectTodoSent(ctx context.Context, id, session string, at time.Time) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if err := CheckDirectTodoSend(prev, session, at); err != nil {
			return err
		}
		next := prev
		next.SentAt, next.ReadAt = time.Unix(at.Unix(), 0), time.Time{}
		if err := tx.PutDirectTodo(prev, next); err != nil {
			return err
		}
		next.Version++
		out = next
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) CompleteDirectTodo(ctx context.Context, id, session, actor string, person bool, file TodoV2Filer) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != session {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if !person && actor != session {
			return work.RefuseV2("not_todo_owner", "An Agent may complete only its own Session to-do.")
		}
		if !prev.CompletedAt.IsZero() {
			out = prev
			return nil
		}
		next := prev
		next.CompletedAt, next.CompletedBy = w.now(), actor
		if err := tx.PutDirectTodo(prev, next); err != nil {
			return err
		}
		next.Version++
		out = next
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

func (w *WorkSystemV2) DeleteDirectTodo(ctx context.Context, id, session string, file func() (store.ReceiptKey, store.ReceiptAnswer, bool)) error {
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		deleted, err := tx.DeleteDirectTodo(id, session)
		if err != nil {
			return err
		}
		if !deleted {
			return work.RefuseV2("todo_not_found", "No direct to-do with that id belongs to this Session.")
		}
		if file != nil {
			if k, ans, ok := file(); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return mapWorkV2Error(err)
}

func (w *WorkSystemV2) Propose(ctx context.Context, p work.ProposalV2) (work.ProposalV2, error) {
	p.ID, p.Title, p.Description, p.Reason = newWorkID(), strings.TrimSpace(p.Title), strings.TrimSpace(p.Description), strings.TrimSpace(p.Reason)
	p.ProjectID, p.ProjectPath, p.SessionID = strings.TrimSpace(p.ProjectID), strings.TrimSpace(p.ProjectPath), strings.TrimSpace(p.SessionID)
	if !p.Kind.Valid() || p.ProjectID == "" || p.ProjectPath == "" || p.SessionID == "" || p.Title == "" || p.Description == "" || p.Reason == "" {
		return p, workV2Error(http.StatusUnprocessableEntity, "invalid_proposal", "A proposal needs Project, kind, title, description, reason, and Session.")
	}
	if err := validateWorkV2Text(p.Title, p.Description); err != nil {
		return p, err
	}
	if len(p.Reason) > directTodoTextLimit || len(p.SuggestedAcceptance) > workV2DescriptionLimit {
		return p, workV2Error(http.StatusRequestEntityTooLarge, "proposal_too_large", "Proposal reason is at most 8 KiB and suggested acceptance at most 64 KiB.")
	}
	if p.SourceWorkID == "" && p.SourceTodoID == "" {
		return p, workV2Error(http.StatusUnprocessableEntity, "proposal_source_required", "A proposal must name the item or direct to-do that revealed it.")
	}
	p.State, p.CreatedAt, p.Version = "pending", w.now(), 1
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if p.SourceWorkID != "" {
			item, err := tx.Item(p.SourceWorkID)
			if err != nil || item.OwnerSession != p.SessionID {
				return work.RefuseV2("proposal_source_invalid", "The source item is not owned by this Session.")
			}
		}
		if p.SourceTodoID != "" {
			td, err := tx.DirectTodo(p.SourceTodoID)
			if err != nil || td.SessionID != p.SessionID {
				return work.RefuseV2("proposal_source_invalid", "The source to-do does not belong to this Session.")
			}
		}
		return tx.CreateProposal(p)
	})
	return p, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Proposals(ctx context.Context, state string) ([]work.ProposalV2, bool, error) {
	rows, truncated, err := w.Store.WorkV2Proposals(ctx, state, WorkV2PageSize)
	return rows, truncated, mapWorkV2Error(err)
}

type ProposalDecisionV2 struct {
	Decision            string
	Title               string
	Description         string
	SuggestedAcceptance string
}

func (w *WorkSystemV2) ResolveProposal(ctx context.Context, id, actor string, c ProposalDecisionV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		p, err := tx.Proposal(id)
		if err != nil {
			return work.RefuseV2("proposal_not_found", "No proposal has that id.")
		}
		if p.State != "pending" {
			return work.RefuseV2("proposal_resolved", "This proposal was already resolved.")
		}
		now := w.now()
		if c.Decision == "reject" {
			if err := tx.ResolveProposal(p, "rejected", "", now); err != nil {
				return err
			}
			return nil
		}
		if c.Decision != "accept" {
			return work.RefuseV2("invalid_decision", "Choose accept or reject.")
		}
		if strings.TrimSpace(c.Title) != "" {
			p.Title = strings.TrimSpace(c.Title)
		}
		if strings.TrimSpace(c.Description) != "" {
			p.Description = strings.TrimSpace(c.Description)
		}
		if c.SuggestedAcceptance != "" {
			p.SuggestedAcceptance = strings.TrimSpace(c.SuggestedAcceptance)
		}
		if err := validateWorkV2Text(p.Title, p.Description); err != nil {
			return err
		}
		description := p.Description
		if p.SuggestedAcceptance != "" {
			description += "\n\nAcceptance:\n" + p.SuggestedAcceptance
		}
		i := work.ItemV2{ID: newWorkID(), ProjectID: p.ProjectID, ProjectPath: p.ProjectPath, Kind: p.Kind, Title: p.Title,
			Description: description, Phase: work.PhaseCreated, DeploymentPolicy: work.DeployAgentDecides,
			CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		if err := work.ValidateNewV2(i); err != nil {
			return err
		}
		if err := tx.CreateItem(i, actor, payload(map[string]string{"proposal_id": p.ID})); err != nil {
			return err
		}
		if err := tx.ResolveProposal(p, "accepted", i.ID, now); err != nil {
			return err
		}
		out = WorkV2View{Item: i}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}
