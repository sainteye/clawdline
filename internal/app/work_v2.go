package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

const (
	WorkV2PageSize = 100
	// WorkV2ListPageLimit keeps the Board's first useful paint bounded; rows
	// beyond it remain available through the list cursor.
	WorkV2ListPageLimit         = 24
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
	// GateSettings reads the two global defaults for the first successful
	// assignment in a cycle. It is never consulted by later transitions.
	GateSettings func(context.Context) (WorkV2GateSettings, error)
	// VerificationInvalidator is the transaction-local seam the coordinator
	// slice uses to stale rounds and PASS/override authorization. This core
	// calls it for every acceptance change and completion retraction even when
	// no round store has been installed yet.
	VerificationInvalidator func(*store.WorkV2Tx, work.ItemV2, work.ItemV2, string) error
	// VerificationAuthorizer is the transaction-local seam that a verification
	// coordinator supplies to match a durable PASS to this exact item cycle and
	// acceptance tuple. Free-form Agent evidence never substitutes for it when
	// the cycle's verification gate is on.
	VerificationAuthorizer func(*store.WorkV2Tx, work.ItemV2) error
}

type WorkV2GateSettings struct {
	Planning bool
	Verify   bool
}

func NewWorkSystemV2(st *store.Store) *WorkSystemV2 {
	return &WorkSystemV2{Store: st, Now: time.Now, GateSettings: func(context.Context) (WorkV2GateSettings, error) {
		return WorkV2GateSettings{Planning: true, Verify: false}, nil
	}}
}

func (w *WorkSystemV2) now() time.Time {
	if w.Now != nil {
		return time.Unix(w.Now().Unix(), 0)
	}
	return time.Now().Truncate(time.Second)
}

type WorkV2View struct {
	Item         work.ItemV2
	CardProgress store.WorkV2CardProgress
	Assignments  []work.AssignmentV2
	Documents    []work.DocumentV2
	Images       []work.ImageV2
	Steps        []work.StepV2
	Events       []work.EventV2
	Gate         *contract.WorkGateDetailRead
	GateCompact  *contract.WorkGateCompactRead
	// Claim is the person's message the owning Session claimed the item on,
	// read for a page of items that carries no assignments; a view that
	// carries them answers it from its active assignment instead.
	Claim *work.CreatedViaV2
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
	case errors.Is(err, store.ErrWorkGateRoundsFull):
		return workV2Error(http.StatusInsufficientStorage, "verification_rounds_full",
			"Verification round detail is full; export eligible closed evidence and confirm its manifest before purging it.")
	case errors.Is(err, store.ErrWorkGateActive):
		return workV2Error(http.StatusConflict, "verification_round_active", "This item already has a queued or running verification round.")
	case errors.Is(err, store.ErrWorkGateEscalated):
		return workV2Error(http.StatusConflict, "verification_escalated", "Resolve the current verification escalation before starting another round.")
	case errors.Is(err, store.ErrBusy):
		return workV2Error(http.StatusServiceUnavailable, "store_busy", "The store is busy; nothing was changed.")
	}
	return workV2Error(http.StatusServiceUnavailable, "store_unavailable", err.Error())
}

type NewWorkV2 struct {
	ProjectID          string
	ProjectPath        string
	Kind               work.Kind
	Title              string
	Description        string
	AcceptanceCriteria string
	DeploymentPolicy   work.DeploymentPolicy
	// ReviewRequired is the person's "Needs independent review" switch; only
	// the person's create route carries it, and only a Feature takes true.
	ReviewRequired bool
	Actor          string
}

func validateWorkV2Acceptance(criteria string) error {
	switch {
	case len(criteria) > workV2DescriptionLimit:
		return workV2Error(http.StatusRequestEntityTooLarge, "acceptance_too_large", "Acceptance criteria are at most 64 KiB.")
	case !utf8.ValidString(criteria):
		return workV2Error(http.StatusBadRequest, "invalid_acceptance", "Acceptance criteria must be valid UTF-8 Markdown.")
	}
	return nil
}

func (w *WorkSystemV2) currentGateSettings(ctx context.Context) (WorkV2GateSettings, error) {
	if w.GateSettings == nil {
		return WorkV2GateSettings{Planning: true}, nil
	}
	g, err := w.GateSettings(ctx)
	if err != nil {
		return WorkV2GateSettings{}, workV2Error(http.StatusServiceUnavailable, "settings_unavailable",
			"The planning and verification settings could not be read; nothing was assigned.")
	}
	return g, nil
}

func captureWorkV2Gates(i *work.ItemV2, g WorkV2GateSettings, at time.Time) error {
	if !i.HasGateSnapshot() {
		i.GateSnapshotCycle, i.PlanningGate, i.VerifyGate = i.Cycle, g.Planning, g.Verify
		i.GateSnapshotAt = at
	}
	return nil
}

// InvalidateVerificationAuthorization is the application-side hook every
// acceptance change or completion retraction crosses in the item's own
// transaction. The store seam is always called; the optional coordinator
// callback lets a later slice add its aggregate work without weakening this
// core's atomicity.
func (w *WorkSystemV2) InvalidateVerificationAuthorization(tx *store.WorkV2Tx, prev, next work.ItemV2, reason string) error {
	if err := tx.InvalidateWorkV2VerificationAuthorization(prev, next, reason); err != nil {
		return err
	}
	if w.VerificationInvalidator != nil {
		return w.VerificationInvalidator(tx, prev, next, reason)
	}
	return nil
}

func (w *WorkSystemV2) authorizeVerification(tx *store.WorkV2Tx, item work.ItemV2) (store.WorkGateAuthorization, error) {
	if !item.VerifyGate {
		return store.WorkGateAuthorization{}, nil
	}
	if w.VerificationAuthorizer == nil {
		a, err := tx.WorkGateAuthorization(item)
		if err == nil {
			return a, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return store.WorkGateAuthorization{}, work.RefuseV2("verification_authorization_required",
				"This cycle requires an independent PASS or reasoned override for its exact candidate and acceptance criteria before merging.")
		}
		return store.WorkGateAuthorization{}, err
	}
	if err := w.VerificationAuthorizer(tx, item); err != nil {
		return store.WorkGateAuthorization{}, err
	}
	return tx.WorkGateAuthorization(item)
}

// PreviewAssignment returns the gate facts a presently unsnapshotted item
// would capture. The new-Session transport uses it to compose the Root
// Assignment and to refuse missing criteria before opening any external tab;
// the successful FinishAssignment remains the only durable write.
func (w *WorkSystemV2) PreviewAssignment(ctx context.Context, item work.ItemV2) (work.ItemV2, error) {
	if item.HasGateSnapshot() {
		return item, nil
	}
	gates, err := w.currentGateSettings(ctx)
	if err != nil {
		return work.ItemV2{}, err
	}
	if err := captureWorkV2Gates(&item, gates, w.now()); err != nil {
		return work.ItemV2{}, mapWorkV2Error(err)
	}
	return item, nil
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
	if err := validateWorkV2Acceptance(n.AcceptanceCriteria); err != nil {
		return WorkV2View{}, err
	}
	now := w.now()
	i := work.ItemV2{ID: newWorkID(), ProjectID: n.ProjectID, ProjectPath: n.ProjectPath, Kind: n.Kind,
		Title: n.Title, Description: n.Description, Phase: work.PhaseCreated,
		DeploymentPolicy: n.DeploymentPolicy, ReviewRequired: n.ReviewRequired, CreatedBy: n.Actor, CreatedAt: now,
		UpdatedAt: now, Cycle: 1, Version: 1}
	work.SetAcceptance(&i, n.AcceptanceCriteria)
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
		if err := tx.CreateItem(i, n.Actor, payload(map[string]any{"project_id": i.ProjectID, "kind": i.Kind,
			"review_required": i.ReviewRequired})); err != nil {
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
	progress, err := w.Store.WorkV2CardProgress(ctx, []string{id})
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	gate, err := w.Store.WorkGateDetail(ctx, i)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	return WorkV2View{Item: i, Assignments: a, Documents: d, Images: images, Steps: steps, Events: events,
		CardProgress: progress[id],
		Gate:         &gate}, nil
}

func (w *WorkSystemV2) pageViews(ctx context.Context, items []work.ItemV2, includeClaims bool) ([]WorkV2View, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	relations, err := w.Store.WorkV2Relations(ctx, ids)
	if err != nil {
		return nil, mapWorkV2Error(err)
	}
	gates, err := w.Store.WorkGateCompactDetails(ctx, items)
	if err != nil {
		return nil, mapWorkV2Error(err)
	}
	out := make([]WorkV2View, 0, len(items))
	for _, item := range items {
		view := WorkV2View{Item: item, Documents: relations.Documents[item.ID], Images: relations.Images[item.ID],
			CardProgress: relations.Progress[item.ID],
			Steps:        relations.Steps[item.ID]}
		if includeClaims {
			view.Claim = relations.Claims[item.ID]
		}
		if compact, ok := gates[item.ID]; ok {
			view.GateCompact = &compact
		}
		out = append(out, view)
	}
	return out, nil
}

func (w *WorkSystemV2) List(ctx context.Context, project, owner, status, search string) ([]WorkV2View, bool, error) {
	if status != "open" && status != "done" && status != "all" {
		return nil, false, workV2Error(http.StatusBadRequest, "invalid_status", "Status is open, done or all.")
	}
	items, truncated, err := w.Store.WorkV2Items(ctx, project, owner, status, search, WorkV2PageSize)
	if err != nil {
		return nil, false, mapWorkV2Error(err)
	}
	out, err := w.pageViews(ctx, items, true)
	if err != nil {
		return nil, false, err
	}
	return out, truncated, nil
}

type WorkV2ListPage struct {
	Rows []WorkV2View
	Next string
}

type workV2ListCursor struct {
	UpdatedAt int64  `json:"updated_at"`
	ID        string `json:"id"`
}

func encodeWorkV2ListCursor(i work.ItemV2) string {
	b, _ := json.Marshal(workV2ListCursor{UpdatedAt: i.UpdatedAt.Unix(), ID: i.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeWorkV2ListCursor(raw string) (workV2ListCursor, error) {
	if raw == "" {
		return workV2ListCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return workV2ListCursor{}, workV2Error(http.StatusBadRequest, "bad_cursor", "The Board page cursor is not valid.")
	}
	var cursor workV2ListCursor
	if json.Unmarshal(b, &cursor) != nil || cursor.UpdatedAt <= 0 || len(cursor.ID) != 36 {
		return workV2ListCursor{}, workV2Error(http.StatusBadRequest, "bad_cursor", "The Board page cursor is not valid.")
	}
	return cursor, nil
}

// ListPage is the Console Board's bounded read. The older List method remains
// the complete 100-row projection used by Session ownership and internal
// reconciliation; paging the person's screen must not silently page those.
func (w *WorkSystemV2) ListPage(ctx context.Context, project, status, search, rawCursor string) (WorkV2ListPage, error) {
	if status != "open" && status != "done" && status != "all" {
		return WorkV2ListPage{}, workV2Error(http.StatusBadRequest, "invalid_status", "Status is open, done or all.")
	}
	cursor, err := decodeWorkV2ListCursor(rawCursor)
	if err != nil {
		return WorkV2ListPage{}, err
	}
	items, truncated, err := w.Store.WorkV2ItemsPage(ctx, project, "", status, search,
		cursor.UpdatedAt, cursor.ID, WorkV2ListPageLimit)
	if err != nil {
		return WorkV2ListPage{}, mapWorkV2Error(err)
	}
	rows, err := w.pageViews(ctx, items, true)
	if err != nil {
		return WorkV2ListPage{}, err
	}
	page := WorkV2ListPage{Rows: rows}
	if truncated && len(items) > 0 {
		page.Next = encodeWorkV2ListCursor(items[len(items)-1])
	}
	return page, nil
}

func (w *WorkSystemV2) RecentlyCompleted(ctx context.Context, session string) ([]WorkV2View, bool, error) {
	items, truncated, err := w.Store.CompletedWorkV2ForSession(ctx, session, WorkV2PageSize)
	if err != nil {
		return nil, false, mapWorkV2Error(err)
	}
	out, err := w.pageViews(ctx, items, false)
	if err != nil {
		return nil, false, err
	}
	return out, truncated, nil
}

type AddImageV2 struct {
	ExpectedVersion int64
	Title           string
	Data            []byte
	// MediaType is what Normalize wrote Data as: image/png or image/jpeg.
	MediaType string
	Width     int
	Height    int
	Position  int64
	Actor     string
}

// referenceMediaType is a media type a reference image is stored in: what
// Normalize writes, and nothing else.
func referenceMediaType(t string) bool {
	return t == artifacts.MediaTypePNG || t == artifacts.MediaTypeJPEG
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
		if len(c.Data) == 0 || c.Width <= 0 || c.Height <= 0 || !referenceMediaType(c.MediaType) {
			return work.RefuseV2("invalid_image", "A reference image needs normalized PNG or JPEG pixels.")
		}
		now := w.now()
		image := work.ImageV2{ID: newWorkID(), WorkID: id, Title: c.Title, MediaType: c.MediaType,
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
	ExpectedVersion    int64
	Title              *string
	Description        *string
	AcceptanceCriteria *string
	Condition          *work.Condition
	UserAction         *string
	// ReviewRequired sets the person's "Needs independent review" switch on a
	// Feature. Like the deployment policy, the person may change it and the
	// Agent may not.
	ReviewRequired *bool
	Actor          string
	OwnerSession   string
	Person         bool
	// RevisionRun is a daemon-issued message from the person to the owning
	// Root. Only the dedicated acceptance revision route sets it.
	RevisionRun *work.Run
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
		acceptanceChanged := false
		if c.AcceptanceCriteria != nil {
			if !c.Person && c.RevisionRun == nil && (strings.TrimSpace(prev.AcceptanceCriteria) != "" ||
				(prev.Phase != work.PhaseAssigned && prev.Phase != work.PhaseImplementing)) {
				return work.RefuseV2("acceptance_not_agent_editable",
					"The owning Session may write missing acceptance criteria once, before verification; later revisions belong to the person.")
			}
			switch prev.Phase {
			case work.PhaseMerging, work.PhaseDeploying, work.PhaseDone:
				return work.RefuseV2("acceptance_locked", "Acceptance criteria are locked once work enters merging.")
			}
			if c.RevisionRun != nil {
				if c.Person || strings.TrimSpace(prev.AcceptanceCriteria) == "" {
					return work.RefuseV2("acceptance_revision_not_applicable", "This route revises an existing acceptance contract.")
				}
				if err := work.RelayTo(*c.RevisionRun, prev.OwnerSession, time.Time{}); err != nil {
					return relayRefusal(err)
				}
				acceptanceAt, err := tx.AcceptanceChangedAt(prev)
				if err != nil {
					return workV2Error(http.StatusServiceUnavailable, "acceptance_history_unavailable", "The current acceptance version's source could not be checked; nothing changed.")
				}
				if !c.RevisionRun.At.After(acceptanceAt) {
					return work.RefuseV2("run_before_acceptance", "That message predates the current acceptance state; ask the person for a new instruction.")
				}
				owned, err := tx.ActiveOwnedItemCount(prev.OwnerSession)
				if err != nil {
					return err
				}
				if !work.AcceptanceRevisionInstruction(c.RevisionRun.Excerpt, prev.ID, prev.Title, owned == 1) {
					return work.RefuseV2("run_not_acceptance_instruction", "That message does not explicitly identify this item's acceptance revision.")
				}
			}
			if c.RevisionRun != nil && strings.TrimSpace(*c.AcceptanceCriteria) == "" {
				return work.RefuseV2("acceptance_required", "The replacement acceptance Markdown cannot be blank.")
			}
			if err := validateWorkV2Acceptance(*c.AcceptanceCriteria); err != nil {
				return err
			}
			acceptanceChanged = work.SetAcceptance(&next, *c.AcceptanceCriteria)
			if acceptanceChanged && prev.Phase == work.PhaseVerifying {
				next.Phase = work.PhaseImplementing
				next.Condition, next.UserAction = "", ""
			}
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
		if c.ReviewRequired != nil {
			if !c.Person {
				return workV2Error(http.StatusForbidden, "review_required_person_only",
					"Only the person decides whether a Feature needs independent review; the Agent follows their switch.")
			}
			if err := work.ReviewRequiredApplies(prev.Kind, *c.ReviewRequired); err != nil {
				return err
			}
			next.ReviewRequired = *c.ReviewRequired
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
		if acceptanceChanged {
			if err := w.InvalidateVerificationAuthorization(tx, prev, next, "acceptance_changed"); err != nil {
				return err
			}
		}
		actor := c.Actor
		event := map[string]any{"title": c.Title != nil,
			"description": c.Description != nil, "acceptance": acceptanceChanged,
			"acceptance_version": next.AcceptanceVersion, "acceptance_digest": next.AcceptanceDigest,
			"verification_authorization_invalidated": acceptanceChanged,
			"condition":                              c.Condition, "user_action": c.UserAction != nil}
		if c.ReviewRequired != nil {
			event["review_required"] = next.ReviewRequired
		}
		if c.RevisionRun != nil {
			actor = c.RevisionRun.Actor()
			event["acceptance_source"] = map[string]any{"run": c.RevisionRun.Evidence(), "excerpt": c.RevisionRun.Excerpt}
		}
		if err := tx.PutItem(prev, next, "item.edited", actor, payload(event)); err != nil {
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

type ConvertKindV2 struct {
	ExpectedVersion int64
	Kind            work.Kind
	Actor           string
}

// ConvertKind lets a person move work between planning and execution before
// implementation starts. An assigned owner is released in the same transaction;
// the item keeps its identity, content, attachments and history.
func (w *WorkSystemV2) ConvertKind(ctx context.Context, id string, c ConvertKindV2, file WorkV2Filer) (WorkV2View, error) {
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
			return work.RefuseV2("item_terminal", "A completed or cancelled item cannot be converted.")
		}
		if prev.Kind == work.KindPlan {
			if !c.Kind.Executable() {
				return work.RefuseV2("conversion_kind_not_executable", "Convert a Plan to an epic, feature, or issue.")
			}
		} else if (prev.Kind != work.KindEpic && prev.Kind != work.KindFeature) || c.Kind != work.KindPlan {
			return work.RefuseV2("conversion_kind_invalid", "Convert an epic or feature to a Plan, or a Plan to executable work.")
		}
		if (prev.Phase != work.PhaseCreated && prev.Phase != work.PhaseAssigned) || prev.ParentID != "" {
			return work.RefuseV2("item_not_convertible", "Only a top-level item that has not entered implementation can be converted.")
		}
		if prev.Phase == work.PhaseAssigned && prev.OwnerSession == "" {
			return work.RefuseV2("item_not_convertible", "The assigned item has no current owner; refresh it before converting.")
		}
		if pending, err := tx.PendingAssignment(id); err != nil {
			return err
		} else if pending.ID != "" {
			return work.RefuseV2("assignment_pending", "A new Session is still opening for this item; wait for that assignment to finish before converting.")
		}
		if prev.Kind == work.KindEpic {
			if children, _, err := tx.Children(id); err != nil {
				return err
			} else if children != 0 {
				return work.RefuseV2("epic_has_children", "An Epic with child items cannot become a Plan.")
			}
		}
		next := prev
		if prev.OwnerSession != "" {
			a, err := tx.ActiveAssignment(id)
			if err != nil {
				return err
			}
			if a.ID == "" || a.SessionID != prev.OwnerSession {
				return work.RefuseV2("assignment_changed", "The current assignment changed; refresh the item before converting.")
			}
			a.State, a.ReleasedAt, a.UpdatedAt = "released", w.now(), w.now()
			if err := tx.UpdateAssignment(a); err != nil {
				return err
			}
			next.OwnerSession, next.Phase, next.Condition, next.UserAction = "", work.PhaseCreated, "", ""
		}
		next.Kind = c.Kind
		// The review switch is a Feature's alone; it does not follow the item
		// into another kind.
		if next.Kind != work.KindFeature {
			next.ReviewRequired = false
		}
		next.UpdatedAt = w.now()
		if err := tx.PutItem(prev, next, "item.converted", c.Actor, payload(map[string]string{
			"from": string(prev.Kind), "to": string(next.Kind), "previous_session": prev.OwnerSession,
		})); err != nil {
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
	// Claim is the person's message a Session claims the item on
	// (ClaimFromSession), or, with Claim.Assigned, assigns it to a new
	// Session on (RunAssignment); nil for a person's own assignment.
	Claim *work.CreatedViaV2
	// EpicOwner is the Session assigning a child of the Epic it owns
	// (epic_children.go); empty for a person's own assignment. The owner
	// check is made inside the assignment's transaction.
	EpicOwner string
	// Persona is the built-in persona a new Session is opened as; empty for
	// none. The transport checks it against the catalog before this is built.
	Persona string
	// GatePreview is the mode already composed into a new Root Assignment.
	// Direct callers leave it nil and this application reads settings itself.
	GatePreview *WorkV2GateSettings
	// CycleBaseCommit is read before the transaction from the item's Project
	// repository. It is stored only if this assignment captures verify=true.
	CycleBaseCommit string
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
	var gates WorkV2GateSettings
	probe, err := w.Store.WorkV2Item(ctx, id)
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	if !probe.HasGateSnapshot() {
		if pending && c.GatePreview != nil {
			gates = *c.GatePreview
		} else {
			gates, err = w.currentGateSettings(ctx)
			if err != nil {
				return WorkV2View{}, err
			}
		}
	}
	var out WorkV2View
	err = w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		if prev.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if prev.Planning() {
			return work.RefuseV2("planning_not_assignable", "Refactor and Plan stay in Planning.")
		}
		if prev.Phase.Terminal() {
			return work.RefuseV2("item_terminal", "Reopen terminal work before assigning it.")
		}
		if c.Claim != nil {
			if c.Claim.Assigned {
				if err := runMayAssign(prev, c); err != nil {
					return err
				}
			}
			if err := claimable(tx, prev, c.Actor); err != nil {
				return err
			}
		}
		if c.EpicOwner != "" {
			if err := epicOwnerMayAssign(tx, prev, c.EpicOwner); err != nil {
				return err
			}
		}
		// A new Session still standing on its first screen will be briefed as
		// this item's owner the moment the person answers it; a second
		// assignment meanwhile would give the item two.
		if awaited, err := tx.AwaitedAssignment(id); err != nil {
			return err
		} else if awaited.ID != "" {
			return work.RefuseV2("assignment_awaiting_dialog", "A new Session opened for this item is waiting "+
				"for you to answer its first screen. Answer it, or close its tab, before assigning again.")
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
			HumanActor: c.Actor, CreatedAt: now, UpdatedAt: now, ClaimedVia: c.Claim, Persona: c.Persona}
		if pending && !prev.HasGateSnapshot() {
			preview := prev
			if err := captureWorkV2Gates(&preview, gates, now); err != nil {
				return err
			}
			a.GatePreviewed, a.PlanningGate, a.VerifyGate = true, preview.PlanningGate, preview.VerifyGate
			if preview.VerifyGate {
				a.CycleBaseCommit = c.CycleBaseCommit
			}
		}
		if !pending && strings.TrimSpace(a.SessionID) == "" {
			return work.RefuseV2("session_required", "An active assignment needs a Session conversation id.")
		}
		if pending && c.Mode != "new_session" {
			return work.RefuseV2("invalid_assignment", "Only a new Session has an assigning state.")
		}
		if c.Persona != "" && c.Mode != "new_session" {
			return work.RefuseV2("persona_not_applicable", "A persona is chosen when a new Session opens.")
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
			if err := captureWorkV2Gates(&next, gates, now); err != nil {
				return err
			}
			if next.VerifyGate && next.CycleBaseCommit == "" {
				next.CycleBaseCommit = c.CycleBaseCommit
			}
		}
		next.UpdatedAt = now
		fields := map[string]any{"assignment_id": a.ID, "mode": a.Mode, "pending": pending,
			"session_id": a.SessionID, "seeded_steps": len(seeded)}
		if old.ID != "" {
			fields["previous_session"] = old.SessionID
		}
		if c.Claim != nil {
			fields["via_run"], fields["claimed"], fields["excerpt"] = c.Claim.Run, !c.Claim.Assigned, c.Claim.Excerpt
			if c.Claim.Assigned {
				fields["assigned_by"], fields["persona"] = c.Claim.Session, c.Claim.Persona
			}
		}
		if err := tx.PutItem(prev, next, "item.assigned", c.Actor, payload(fields)); err != nil {
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
	AssignmentID    string
	SessionID       string
	TerminalID      string
	RootAssignment  string
	Failure         string
	Actor           string
	CycleBaseCommit string
}

func (w *WorkSystemV2) FinishAssignment(ctx context.Context, workID string, c FinishAssignmentV2) (WorkV2View, error) {
	var gates WorkV2GateSettings
	if c.Failure == "" {
		probe, err := w.Store.WorkV2Item(ctx, workID)
		if err != nil {
			return WorkV2View{}, mapWorkV2Error(err)
		}
		if !probe.HasGateSnapshot() {
			pending, assignmentErr := w.Store.WorkV2Assignment(ctx, c.AssignmentID)
			if assignmentErr == nil && pending.GatePreviewed {
				gates = WorkV2GateSettings{Planning: pending.PlanningGate, Verify: pending.VerifyGate}
			} else {
				gates, err = w.currentGateSettings(ctx)
				if err != nil {
					return WorkV2View{}, err
				}
			}
		}
	}
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
			if prev.UserAction == AssignmentDialogAction {
				next.Condition, next.UserAction = "", ""
			}
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
			if err := captureWorkV2Gates(&next, gates, now); err != nil {
				return err
			}
			if next.VerifyGate && next.CycleBaseCommit == "" {
				next.CycleBaseCommit = pending.CycleBaseCommit
				if next.CycleBaseCommit == "" {
					next.CycleBaseCommit = c.CycleBaseCommit
				}
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

// AssignmentDialogAction is what a Board item waiting on its new Session's
// first screen asks of the person. It is the daemon's own sentence, so a
// failed assignment can take back exactly this and nothing a Session wrote.
const AssignmentDialogAction = "新開的 Session 一開啟就停在一個需要你回答的畫面（例如 Codex 的更新提示）。" +
	"請到那個 Session 回答；回答後工作說明會自動送出。不想繼續的話，關掉那個分頁即可。"

// AwaitDialogV2 is a pending new-Session assignment whose Feature Root was
// opened and stopped on a dialog (orchestrator root_dialog.go).
type AwaitDialogV2 struct {
	AssignmentID   string
	RootAssignment string
	TerminalID     string
	Actor          string
}

// AwaitAssignmentDialog keeps a new-Session assignment pending while its
// Session waits for the person to answer its first screen: the Root
// Assignment and the tab are recorded on it, so the assignment can be
// finished when that Root is briefed or fails (WorkV2AwaitedAssignments), and
// the item asks the person to answer.
func (w *WorkSystemV2) AwaitAssignmentDialog(ctx context.Context, workID string, c AwaitDialogV2) (WorkV2View, error) {
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
		pending.RootAssignment, pending.TerminalID, pending.UpdatedAt = c.RootAssignment, c.TerminalID, now
		if err := tx.UpdateAssignment(pending); err != nil {
			return err
		}
		next := prev
		next.Condition, next.UserAction, next.UpdatedAt = work.ConditionWaitingUser, AssignmentDialogAction, now
		if err := tx.PutItem(prev, next, "assignment.awaiting_dialog", c.Actor, payload(map[string]any{
			"assignment_id": pending.ID, "root_assignment_id": c.RootAssignment})); err != nil {
			return err
		}
		next.Version++
		out = WorkV2View{Item: next, Assignments: []work.AssignmentV2{pending}}
		return nil
	})
	return out, mapWorkV2Error(err)
}

// AwaitedAssignments is every pending assignment waiting on a Feature Root.
func (w *WorkSystemV2) AwaitedAssignments(ctx context.Context) ([]work.AssignmentV2, error) {
	return w.Store.WorkV2AwaitedAssignments(ctx)
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
	// Candidate is required only for implementing -> verifying on a cycle
	// whose verify snapshot is on. The HTTP adapter constructs it from a
	// registered, exact, tracked-clean same-Project worktree.
	Candidate *contract.WorkGateCandidateReceipt
	RoundID   string
	AttemptID string
	// Finish marks a step taken by Finish, in the item's history.
	Finish bool
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
		next, effectIDs, err := w.advanceTx(tx, id, prev, c)
		if err != nil {
			return err
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

// advanceTx moves prev one phase on inside tx, through every gate the phase
// route has, and answers the item as written. Advance and Finish both use it,
// so a step Finish takes is the step `item phase` would have taken.
func (w *WorkSystemV2) advanceTx(tx *store.WorkV2Tx, id string, prev work.ItemV2, c AdvanceWorkV2) (work.ItemV2, []int64, error) {
	if prev.OwnerSession != c.SessionID || c.SessionID == "" {
		return work.ItemV2{}, nil, work.RefuseV2("not_item_owner", "Only the owning Session may advance this item.")
	}
	rows, err := tx.Tasks(id)
	if err != nil {
		return work.ItemV2{}, nil, err
	}
	facts, unknown := Facts(rows)
	if unknown > 0 {
		return work.ItemV2{}, nil, work.RefuseV2("evidence_unknown", "A broker task bound to this item is unreadable.")
	}
	hasLanding := c.Landing.complete()
	var landedTasks []string
	for _, f := range facts {
		if work.OutcomeOf(f) == work.OutcomeLanded {
			hasLanding = true
			landedTasks = append(landedTasks, f.Task)
		}
	}
	hasVerification := strings.TrimSpace(c.Verification) != ""
	var authorization store.WorkGateAuthorization
	if prev.Phase == work.PhaseVerifying && c.Next == work.PhaseMerging && prev.VerifyGate {
		authorization, err = w.authorizeVerification(tx, prev)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		hasVerification = true
	}
	if prev.Phase == work.PhaseMerging && c.Next == work.PhaseDeploying && prev.VerifyGate {
		authorization, err = w.authorizeVerification(tx, prev)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		if c.Landing == nil || c.Landing.Commit != authorization.CandidateCommit {
			return work.ItemV2{}, nil, work.RefuseV2("verified_candidate_mismatch",
				"The landing commit must be the exact candidate authorized by the current PASS or override.")
		}
	}
	if err := work.AgentTransition(prev, c.Next, hasVerification, hasLanding,
		strings.TrimSpace(c.Deployment) != "", strings.TrimSpace(c.NoDeploymentReason) != ""); err != nil {
		return work.ItemV2{}, nil, err
	}
	if c.Next == work.PhaseVerifying && prev.GateNeedsAcceptance() && strings.TrimSpace(prev.AcceptanceCriteria) == "" {
		return work.ItemV2{}, nil, work.RefuseV2("acceptance_required", "Write acceptance criteria before verification begins.")
	}
	if prev.Kind == work.KindEpic || prev.Kind == work.KindFeature {
		plans, err := tx.PlanDocuments(id)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		if err := work.PlanningGate(prev, c.Next, plans); err != nil {
			return work.ItemV2{}, nil, err
		}
		if prev.Kind == work.KindEpic && c.Next == work.PhaseDone {
			_, open, err := tx.Children(id)
			if err != nil {
				return work.ItemV2{}, nil, err
			}
			if err := work.EpicDoneGate(prev, c.Next, open); err != nil {
				return work.ItemV2{}, nil, err
			}
		}
	}
	if prev.Phase == work.PhaseImplementing && c.Next == work.PhaseVerifying && prev.VerifyGate {
		if prev.Kind == work.KindEpic {
			_, open, err := tx.Children(id)
			if err != nil {
				return work.ItemV2{}, nil, err
			}
			if open != 0 {
				return work.ItemV2{}, nil, work.RefuseV2("epic_children_open",
					fmt.Sprintf("Finish or cancel every Epic child before its final Reality Checker round; %d remain open.", open))
			}
		}
		active, err := tx.ActiveAssignment(id)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		candidate := c.Candidate
		if candidate == nil || c.RoundID == "" || c.AttemptID == "" || active.ID == "" ||
			candidate.AssignmentID != active.ID || candidate.OwnerSessionID != prev.OwnerSession ||
			candidate.Cycle != prev.Cycle || candidate.CriteriaVersion != prev.AcceptanceVersion ||
			candidate.CriteriaDigest != prev.AcceptanceDigest || candidate.Commit == "" || candidate.Tree == "" {
			return work.ItemV2{}, nil, work.RefuseV2("verification_candidate_required",
				"Entering verification needs the active owner's exact candidate receipt for this cycle and acceptance digest.")
		}
		persona, err := tx.WorkGateCheckerPersona(prev)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		now := w.now()
		round := contract.WorkGateRound{ID: c.RoundID, ItemID: id, Cycle: prev.Cycle,
			Acceptance: contract.WorkGateAcceptance{Criteria: prev.AcceptanceCriteria,
				Version: prev.AcceptanceVersion, Digest: prev.AcceptanceDigest},
			Candidate: *candidate, CheckerPersona: persona, State: contract.WorkGateRoundStateQueued,
			CreatedAt: now.Unix(), UpdatedAt: now.Unix(), Attempts: []contract.WorkGateAttempt{{
				ID: c.AttemptID, RoundID: c.RoundID, Attempt: 0, State: contract.WorkGateAttemptStateQueued,
				CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
			}}}
		if err := tx.CreateWorkGateRound(round, prev.CycleBaseCommit); err != nil {
			return work.ItemV2{}, nil, err
		}
	}
	if c.Next == work.PhaseDone {
		steps, err := tx.Steps(id)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		incomplete := 0
		for _, step := range steps {
			if !step.Done {
				incomplete++
			}
		}
		if incomplete > 0 {
			return work.ItemV2{}, nil, work.RefuseV2("steps_incomplete", fmt.Sprintf("Complete all item TODOs before closing this work; %d remain.", incomplete))
		}
	}
	now := w.now()
	next := prev
	next.Phase, next.Condition, next.UserAction, next.UpdatedAt = c.Next, "", "", now
	if c.Next == work.PhaseDone {
		next.ClosedAt, next.OwnerSession = now, ""
		a, err := tx.ActiveAssignment(id)
		if err != nil {
			return work.ItemV2{}, nil, err
		}
		if a.ID != "" {
			a.State, a.ReleasedAt, a.UpdatedAt = "released", now, now
			if err := tx.UpdateAssignment(a); err != nil {
				return work.ItemV2{}, nil, err
			}
		}
	}
	change := map[string]any{
		"from": prev.Phase, "to": c.Next, "verification": c.Verification,
		"landing": c.Landing, "deployment": c.Deployment, "no_deployment_reason": c.NoDeploymentReason}
	if c.Next == work.PhaseDeploying && len(landedTasks) > 0 {
		// The broker's landings this step rested on, so the item's own
		// history says which merge moved it, whoever typed the command.
		change["landed_tasks"] = landedTasks
	}
	if c.Finish {
		change["finish"] = true
	}
	if err := tx.PutItem(prev, next, "item.phase_changed", c.Actor, payload(change)); err != nil {
		return work.ItemV2{}, nil, err
	}
	next.Version++
	var effectIDs []int64
	if c.Next == work.PhaseDone {
		for _, effect := range c.Effects {
			id, err := tx.AddEffect(effect)
			if err != nil {
				return work.ItemV2{}, nil, err
			}
			effectIDs = append(effectIDs, id)
		}
	}
	return next, effectIDs, nil
}

// FinishWorkV2 is one `item finish`: the evidence for every phase still
// ahead of the item, given once. Verification is needed only where no gate's
// PASS stands in for it; Landing only where no bound task's landing does.
type FinishWorkV2 struct {
	ExpectedVersion    int64
	SessionID          string
	Verification       string
	Landing            *VerifiedLandingV2
	Deployment         string
	NoDeploymentReason string
	Actor              string
	Effects            []store.Effect
}

// finishNext is the phase each step of Finish moves to, keyed by the phases
// it may start from: the line `item phase` walks after implementing.
var finishNext = map[work.Phase]work.Phase{
	work.PhaseImplementing: work.PhaseVerifying,
	work.PhaseVerifying:    work.PhaseMerging,
	work.PhaseMerging:      work.PhaseDeploying,
	work.PhaseDeploying:    work.PhaseDone,
}

// Finish walks an item from where it stands to done in one transaction, each
// step through advanceTx — the same owner check, planning gate, verification
// gate, landing rule, step check and deployment rule `item phase` applies —
// and writes one item.phase_changed per step, so the history reads as if the
// four commands had been typed. Any step refused refuses the whole: nothing is
// written, and the refusal names the step that was missing its evidence.
//
// Idempotent at its end: an item already done is answered as it stands and
// nothing is written — no event, no second completion effect — whatever key
// the request carries. A request replayed with its Idempotency-Key is answered
// from its receipt before this runs. A cancelled item is refused.
func (w *WorkSystemV2) Finish(ctx context.Context, id string, c FinishWorkV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		cur, err := tx.Item(id)
		if err != nil {
			return err
		}
		if cur.Phase == work.PhaseDone {
			out = WorkV2View{Item: cur}
			if file != nil {
				if k, ans, ok := file(out); ok {
					return tx.CompleteReceipt(k, ans)
				}
			}
			return nil
		}
		if cur.Version != c.ExpectedVersion {
			return store.ErrConflict
		}
		if _, ok := finishNext[cur.Phase]; !ok {
			if cur.Phase.Terminal() {
				return work.RefuseV2("item_terminal", "A person must reopen terminal work.")
			}
			return work.RefuseV2("finish_not_started",
				"Finish moves an item from implementing onward; this one is "+string(cur.Phase)+". Move it to implementing first.")
		}
		var effectIDs []int64
		for cur.Phase != work.PhaseDone {
			step := AdvanceWorkV2{ExpectedVersion: cur.Version, SessionID: c.SessionID, Next: finishNext[cur.Phase],
				Actor: c.Actor, Finish: true}
			switch step.Next {
			case work.PhaseMerging:
				step.Verification = c.Verification
			case work.PhaseDeploying:
				step.Landing = c.Landing
			case work.PhaseDone:
				step.Deployment, step.NoDeploymentReason, step.Effects = c.Deployment, c.NoDeploymentReason, c.Effects
			}
			next, ids, err := w.advanceTx(tx, id, cur, step)
			if err != nil {
				return finishRefusal(cur.Phase, step.Next, err)
			}
			cur, effectIDs = next, append(effectIDs, ids...)
		}
		out = WorkV2View{Item: cur, EffectIDs: effectIDs}
		if file != nil {
			if k, ans, ok := file(out); ok {
				return tx.CompleteReceipt(k, ans)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}

// finishRefusal names what Finish lacked where advanceTx says only that the
// recorded evidence does not allow the move: which of the three notes, given
// once to Finish, the step needed.
func finishRefusal(from, to work.Phase, err error) error {
	refusal, ok := work.AsRefusalV2(err)
	if !ok || refusal.Code != "invalid_transition" {
		return err
	}
	switch to {
	case work.PhaseMerging:
		return work.RefuseV2("verification_required",
			"Moving "+string(from)+" to merging needs --verification: what was run to verify and what it showed.")
	case work.PhaseDeploying:
		return work.RefuseV2("landing_required",
			"No landing is recorded for this item: no task bound to it has landed, and no --commit was given.")
	case work.PhaseDone:
		return work.RefuseV2("deployment_required",
			"Closing needs --deployment (what was deployed, where, which version) or --no-deployment-reason, as this item's deployment policy says.")
	}
	return err
}

// BoundLandingV2 is one landed broker task bound to an item: what it landed
// and where, as the broker recorded it.
type BoundLandingV2 struct {
	Task       string
	Target     string
	Commit     string
	Repository string
}

// FinishFactsV2 is what the transport reads before Finish to spell a landing
// the root did not type: the gate's authorized candidate, when the item has a
// verification gate, and the landings of the tasks bound to it.
type FinishFactsV2 struct {
	Item work.ItemV2
	// Candidate is the commit the current PASS or override authorized; empty
	// when the item has no verification gate or no current authorization.
	Candidate string
	Landings  []BoundLandingV2
}

// FinishFacts reads FinishFactsV2. It writes nothing. A bound task that cannot
// be decoded refuses with evidence_unknown, because the landing it might hold
// cannot be told from no landing.
func (w *WorkSystemV2) FinishFacts(ctx context.Context, id string) (FinishFactsV2, error) {
	var out FinishFactsV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		item, err := tx.Item(id)
		if err != nil {
			return err
		}
		out.Item = item
		rows, err := tx.Tasks(id)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := orchestrator.Decode(row.Record)
			if err != nil {
				return work.RefuseV2("evidence_unknown", "A broker task bound to this item is unreadable.")
			}
			l := r.Landing
			if l == nil || (l.State != orchestrator.LandingLanded && l.State != orchestrator.LandingIncorporated) {
				continue
			}
			repo := l.Repo
			if repo == "" && r.Worktree != nil {
				repo = r.Worktree.Repository
			}
			out.Landings = append(out.Landings, BoundLandingV2{Task: r.ID, Target: l.Target, Commit: l.Commit, Repository: repo})
		}
		if item.VerifyGate {
			a, err := tx.WorkGateAuthorization(item)
			switch {
			case err == nil:
				out.Candidate = a.CandidateCommit
			case !errors.Is(err, sql.ErrNoRows):
				return err
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

// workV2PersonCompletion is the event a person's manual completion writes.
// ReopenIncomplete reads it to refuse an Agent retracting that completion.
const workV2PersonCompletion = "item.completed"

// Complete is the person's override that closes an item as done. Unlike an
// Agent's Advance it asks for no verification, landing or deployment evidence
// and does not refuse on open steps; it records how many were open instead.
// It works from any non-terminal phase, owned or not.
func (w *WorkSystemV2) Complete(ctx context.Context, id string, expected int64, actor, note string, file WorkV2Filer) (WorkV2View, error) {
	note = strings.TrimSpace(note)
	if len(note) > workV2CompletionReasonLimit {
		return WorkV2View{}, workV2Error(http.StatusRequestEntityTooLarge, "note_too_large",
			"A completion note is at most 8 KiB.")
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
		steps, err := tx.Steps(id)
		if err != nil {
			return err
		}
		open := 0
		for _, step := range steps {
			if !step.Done {
				open++
			}
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
		next.Phase, next.OwnerSession, next.Condition, next.UserAction = work.PhaseDone, "", "", ""
		next.ClosedAt, next.UpdatedAt = now, now
		if err := tx.PutItem(prev, next, workV2PersonCompletion, actor, payload(map[string]any{
			"from": prev.Phase, "open_steps": open, "note": note})); err != nil {
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
		next.GateSnapshotCycle, next.GateSnapshotAt, next.PlanningGate, next.VerifyGate = 0, time.Time{}, false, false
		next.CycleBaseCommit = ""
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
		// A person's completion releases the assignment at ClosedAt too, so
		// the assignment ledger alone cannot tell it from the Session's own;
		// the event that closed the item can.
		if closing, _, err := tx.LatestEventOf(id, "item.phase_changed", workV2PersonCompletion); err != nil {
			return err
		} else if closing == workV2PersonCompletion {
			return work.RefuseV2("not_completing_session",
				"A person completed this item; only a person may reopen it.")
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
		// A completion correction is the same responsibility resumed immediately:
		// keep its gate mode, but bind it to the new cycle and make any earlier
		// verification authorization unusable.
		next.GateSnapshotCycle = next.Cycle
		next.GateSnapshotAt = now
		if err := w.InvalidateVerificationAuthorization(tx, prev, next, "completion_retracted"); err != nil {
			return err
		}
		if err := tx.PutItem(prev, next, "item.completion_retracted", c.SessionID, payload(map[string]any{
			"assignment_id": assignment.ID, "previous_assignment_id": completed.ID,
			"cycle": next.Cycle, "reason": c.Reason, "gate_snapshot_cycle": next.GateSnapshotCycle,
			"planning_gate": next.PlanningGate, "verify_gate": next.VerifyGate,
			"verification_authorization_invalidated": true,
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
		if !work.DocumentRoleValid(c.Role) {
			return work.RefuseV2("invalid_document_role", "That document role is not supported.")
		}
		if err := work.DocumentRoleApplies(prev, c.Role); err != nil {
			return err
		}
		if c.Title == work.ReviewBoundaryTitle {
			if c.Role != "other" || c.Reference != "" {
				return work.RefuseV2("review_assessment_invalid", "Review assessments use role other and a JSON body, without a reference.")
			}
			if _, err := work.ParseReviewBoundary(c.Body); err != nil {
				return work.RefuseV2("review_assessment_invalid", err.Error())
			}
		}
		if c.Role == work.DocumentPlanReview {
			if err := checkPlanReviewTask(tx, prev, c.Reference); err != nil {
				return err
			}
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

// checkPlanReviewTask accepts a plan_review only when its reference is a
// Clawdline child that really reviewed this Feature or Epic's latest plan: a task of
// kind plan_review, dispatched by the item's owner Session, finished with
// success, on this item's line if it names one, and dispatched no earlier
// than the latest plan was written. The task is read inside the same
// transaction as the owner it is compared with.
func checkPlanReviewTask(tx *store.WorkV2Tx, item work.ItemV2, taskID string) error {
	plans, err := tx.PlanDocuments(item.ID)
	if err != nil {
		return err
	}
	var latestPlan time.Time
	for _, d := range plans {
		if d.Role == work.DocumentPlan {
			latestPlan = d.CreatedAt
		}
	}
	if latestPlan.IsZero() {
		return work.RefuseV2(string(item.Kind)+"_plan_required", "Write the plan document first; a review reviews a plan.")
	}
	unknown := workV2Error(http.StatusUnprocessableEntity, "plan_review_task_unknown",
		"reference must be the task id of the plan_review child you dispatched; no readable task has that id.")
	if !orchestrator.IsTaskID(taskID) {
		return unknown
	}
	row, err := tx.BrokerTask(taskID)
	if errors.Is(err, store.ErrNoTask) {
		return unknown
	}
	if err != nil {
		return err
	}
	r, err := orchestrator.Decode(row.Record)
	if err != nil {
		return unknown
	}
	switch {
	case r.Root == nil || r.Root.SessionID != item.OwnerSession:
		return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_not_owned",
			"That task was not dispatched by this item's owning Session; dispatch the review yourself.")
	case r.WorkID != "" && r.WorkID != item.ID:
		return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_other_item",
			"That task is on another item's line; dispatch the review with --work-id "+item.ID+".")
	case r.Kind != work.DocumentPlanReview:
		return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_wrong_kind",
			"That task is not a plan review; dispatch one with --kind plan_review.")
	case r.State != orchestrator.StateSuccess:
		return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_unfinished",
			"That review has not finished with success; wait for its result, or dispatch another.")
	case r.CreatedAt.Unix() < latestPlan.Unix():
		return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_stale",
			"That review was dispatched before the latest plan was written; have the current plan reviewed.")
	}
	return nil
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
	// MediaType is what Normalize wrote Data as: image/png or image/jpeg.
	MediaType string
	Width     int
	Height    int
	Position  int64
	Actor     string
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
		if len(c.Data) == 0 || c.Width <= 0 || c.Height <= 0 || !referenceMediaType(c.MediaType) {
			return work.RefuseV2("invalid_image", "A reference image needs normalized PNG or JPEG pixels.")
		}
		image = work.DirectTodoImageV2{ID: newWorkID(), TodoID: id, Title: c.Title, MediaType: c.MediaType,
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

func (w *WorkSystemV2) ReopenDirectTodo(ctx context.Context, id, session string, file TodoV2Filer) (work.DirectTodoV2, error) {
	var out work.DirectTodoV2
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		prev, err := tx.DirectTodo(id)
		if err != nil {
			return err
		}
		if prev.SessionID != session {
			return work.RefuseV2("todo_session_mismatch", "That to-do belongs to another Session.")
		}
		if prev.CompletedAt.IsZero() {
			out = prev
			if file != nil {
				if k, ans, ok := file(out); ok {
					return tx.CompleteReceipt(k, ans)
				}
			}
			return nil
		}
		next := prev
		next.CompletedAt, next.CompletedBy = time.Time{}, ""
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
	if !p.Kind.Valid() || p.ProjectID == "" || p.ProjectPath == "" || p.SessionID == "" || p.Title == "" || p.Description == "" || p.Reason == "" || strings.TrimSpace(p.SuggestedAcceptance) == "" {
		return p, workV2Error(http.StatusUnprocessableEntity, "invalid_proposal", "A proposal needs Project, kind, title, description, reason, suggested acceptance, and Session; explain the work for a person in plain language.")
	}
	if err := validateWorkV2Text(p.Title, p.Description); err != nil {
		return p, err
	}
	if len(p.Reason) > directTodoTextLimit {
		return p, workV2Error(http.StatusRequestEntityTooLarge, "proposal_too_large", "Proposal reason is at most 8 KiB.")
	}
	if err := validateWorkV2Acceptance(p.SuggestedAcceptance); err != nil {
		return p, err
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
			p.SuggestedAcceptance = c.SuggestedAcceptance
		}
		if err := validateWorkV2Text(p.Title, p.Description); err != nil {
			return err
		}
		if err := validateWorkV2Acceptance(p.SuggestedAcceptance); err != nil {
			return err
		}
		i := work.ItemV2{ID: newWorkID(), ProjectID: p.ProjectID, ProjectPath: p.ProjectPath, Kind: p.Kind, Title: p.Title,
			Description: p.Description, Phase: work.PhaseCreated, DeploymentPolicy: work.DeployAgentDecides,
			CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		work.SetAcceptance(&i, p.SuggestedAcceptance)
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
