package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

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
		var err error
		out, err = w.addDocument(tx, id, c, file)
		return err
	})
	return out, mapWorkV2Error(err)
}

// addDocument is AddDocument inside its transaction, shared with the
// plan_review a successful review child adds by itself.
//
// A document with the role and title of one the item already holds revises
// it in place: same id, version one higher, event document.revised. The
// item's completion_report is revised whatever the new title. Plan,
// plan_review and the review boundary are always added
// (work.DocumentRevisable). A revision whose title, body, reference and
// position are already the stored ones is a retry, answered like the
// plan_review one below.
//
// A plan_review naming a task the item already holds a plan_review for is a
// retry, not a second review: it passes every check a first one would, then
// answers the document already there and writes nothing. That is why its
// expected version may be stale — the first attempt, or the automatic one,
// already moved the item on — and why an Epic's review count cannot be
// raised by sending one review twice.
func (w *WorkSystemV2) addDocument(tx *store.WorkV2Tx, id string, c AddDocumentV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := func() error {
		prev, err := tx.Item(id)
		if err != nil {
			return err
		}
		var existing work.DocumentV2
		retry := false
		if c.Role == work.DocumentPlanReview {
			if existing, retry, err = tx.PlanReviewDocument(id, strings.TrimSpace(c.Reference)); err != nil {
				return err
			}
		}
		var revising work.DocumentV2
		revise := false
		if title := strings.TrimSpace(c.Title); work.DocumentRevisable(c.Role, title) {
			if revising, revise, err = tx.RevisableDocument(id, c.Role, title); err != nil {
				return err
			}
			if revise && revising.Title == title && revising.Body == strings.TrimSpace(c.Body) &&
				revising.Reference == strings.TrimSpace(c.Reference) && revising.Position == c.Position {
				existing, retry, revise = revising, true, false
			}
		}
		if !VersionHolds(c.ExpectedVersion, prev.Version) && !retry {
			return store.ErrConflict
		}
		if prev.OwnerSession == "" || prev.OwnerSession != c.SessionID || prev.Phase.Terminal() {
			return notItemOwner("add documents to", id)
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
		if retry {
			out = WorkV2View{Item: prev, Documents: []work.DocumentV2{existing}}
			if file != nil {
				if k, a, ok := file(out); ok {
					return tx.CompleteReceipt(k, a)
				}
			}
			return nil
		}
		now := w.now()
		doc := work.DocumentV2{ID: newWorkID(), WorkID: id, Role: c.Role, Title: c.Title, Body: c.Body,
			Reference: c.Reference, Position: c.Position, Version: 1, CreatedAt: now, UpdatedAt: now}
		kind, event := "document.added", map[string]any{"document_id": doc.ID, "role": doc.Role}
		if revise {
			doc.ID, doc.Version, doc.CreatedAt = revising.ID, revising.Version+1, revising.CreatedAt
			kind, event = "document.revised", map[string]any{"document_id": doc.ID, "role": doc.Role, "version": doc.Version}
			if err := tx.ReviseDocument(doc); err != nil {
				return err
			}
		} else if err := tx.AddDocument(doc); errors.Is(err, store.ErrWorkV2DocsFull) {
			return documentsFull(id, c.Role)
		} else if err != nil {
			return err
		}
		next := prev
		next.UpdatedAt = now
		if err := tx.PutItem(prev, next, kind, c.SessionID, payload(event)); err != nil {
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
	}()
	return out, err
}

// PlanReviewTitle is the title of the plan_review a review child adds by
// itself; the verdict follows it when the receipt names one.
const PlanReviewTitle = "Plan review"

// AddPlanReviewFromTask records a finished plan_review child's review on the
// item it was dispatched for, as the item's owner would with
// `clawdline item doc --role plan_review --reference <task>`, and through the
// same checks (checkPlanReviewTask). The body is the child's closed review
// receipt (result.json `review`); a receipt larger than a document body is
// left out and the reference alone stands, because the task record keeps the
// whole receipt and a cut JSON body would be unreadable.
//
// It is safe to call more than once, and safe beside a manual `item doc` for
// the same task: whichever comes second answers the document the first
// wrote. Any refusal is typed (plan_review_task_*, *_plan_required,
// not_item_owner, ...) and leaves the item unchanged, so the manual command
// stays the way to record the review.
func (w *WorkSystemV2) AddPlanReviewFromTask(ctx context.Context, taskID string) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		row, err := tx.BrokerTask(taskID)
		if errors.Is(err, store.ErrNoTask) {
			return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_unknown",
				"No readable task has that id.")
		}
		if err != nil {
			return err
		}
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_unknown",
				"That task's record is not readable.")
		}
		if r.WorkID == "" {
			return workV2Error(http.StatusUnprocessableEntity, "plan_review_task_no_item",
				"That review was dispatched on no item; record it with `clawdline item doc <item id> --role plan_review --title \"Plan review\" --reference "+taskID+"`.")
		}
		prev, err := tx.Item(r.WorkID)
		if err != nil {
			return err
		}
		// It writes as the item's owner, and checkPlanReviewTask then refuses
		// a task that owner did not dispatch (plan_review_task_not_owned).
		title, body := planReviewDocument(r.Result)
		out, err = w.addDocument(tx, r.WorkID, AddDocumentV2{ExpectedVersion: prev.Version, SessionID: prev.OwnerSession,
			Role: work.DocumentPlanReview, Title: title, Body: body, Reference: taskID}, nil)
		return err
	})
	return out, mapWorkV2Error(err)
}

// planReviewDocument is the title and body a review receipt becomes.
func planReviewDocument(result *taskdir.Result) (string, string) {
	if result == nil || len(result.Review) == 0 {
		return PlanReviewTitle, ""
	}
	var review struct {
		Verdict string `json:"verdict"`
	}
	title := PlanReviewTitle
	if json.Unmarshal(result.Review, &review) == nil && review.Verdict != "" && len(review.Verdict) <= 64 {
		title += ": " + review.Verdict
	}
	var body bytes.Buffer
	if json.Indent(&body, result.Review, "", "  ") != nil {
		body.Write(result.Review)
	}
	if body.Len() <= workV2DescriptionLimit {
		return title, body.String()
	}
	const notice = "\n\n[Review receipt truncated; see the referenced task for the full review.]"
	cut := workV2DescriptionLimit - len(notice)
	for cut > 0 && !utf8.Valid(body.Bytes()[:cut]) {
		cut--
	}
	return title, string(body.Bytes()[:cut]) + notice
}

// latestPlanReview reads what the item's latest plan_review concluded from
// the receipt of the task it references (the document body may be cut or
// free-form; the task record is the whole receipt). A task that is gone,
// unreadable or carries no review is a legacy receipt; a store that did not
// answer is an error, never a review that found nothing.
func latestPlanReview(tx *store.WorkV2Tx, plans []work.DocumentV2) (work.PlanReviewSummary, error) {
	ref := ""
	for _, d := range plans {
		if d.Role == work.DocumentPlanReview {
			ref = d.Reference
		}
	}
	if ref == "" {
		return work.PlanReviewSummary{Legacy: true}, nil
	}
	row, err := tx.BrokerTask(ref)
	if errors.Is(err, store.ErrNoTask) {
		return work.PlanReviewSummary{Legacy: true}, nil
	}
	if err != nil {
		return work.PlanReviewSummary{}, err
	}
	r, err := orchestrator.Decode(row.Record)
	if err != nil || r.Result == nil {
		return work.PlanReviewSummary{Legacy: true}, nil
	}
	return work.ReadPlanReview(r.Result.Review), nil
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
