package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Lines of work: which one a task is on (design-decisions D36; the rules are
// internal/domain/work/lines.go).
//
// A dispatch is bound to its line when it is admitted, before its record is
// written, so the to-do its root owes is made on that line in the same
// transaction as the task (todos.go) and a work item that follows the line
// finds the task by it (the store's `json_extract(record, '$.work_id')`).
// A task stored before the broker did this is bound once, by BindWork, the
// first time something needs its line: a start that finds its to-do still
// owed (ReconcileTodos), or a proposal about it.

// lineFacts is what a record says about its line.
func lineFacts(r Record) work.LineFacts {
	f := work.LineFacts{Task: r.ID, Named: r.WorkID, Kind: r.Kind}
	if r.Root != nil && !r.Root.PollOnly {
		f.Owner = r.Root.SessionID
	}
	if r.Graph != nil {
		f.Graph = r.Graph.ID
	}
	return f
}

// bindLine decides the line a record being admitted is on. A respawn whose
// copied brief named no line is on the line of the task it retries, if that
// one was bound after its brief was written. An original that cannot be read
// refuses the dispatch: guessing its line would bind the respawn to a line it
// can never leave.
func (b *Broker) bindLine(ctx context.Context, r *Record) error {
	f := lineFacts(*r)
	if r.WorkID == "" && r.RespawnOf != "" {
		origin, _, err := b.Record(ctx, r.RespawnOf)
		switch {
		case err == nil:
			f.RespawnLine = origin.WorkID
		case !isNotFound(err):
			return err
		}
	}
	r.WorkID, r.WorkFrom = work.LineOf(f)
	return nil
}

// lineFor is the line a stored task on no line is to be bound to: the line
// the newest proposal about it named, or else the rules' (work.LineOf). The
// proposals are read in a transaction of the board's own.
func (b *Broker) lineFor(ctx context.Context, r Record) (workID, from string, err error) {
	var prior []work.Proposal
	err = b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		var err error
		prior, err = tx.PriorProposals("", r.ID)
		return err
	})
	if err != nil {
		return "", "", err
	}
	f := lineFacts(r)
	f.Proposed = work.ProposedLine(prior, r.ID)
	workID, from = work.LineOf(f)
	return workID, from, nil
}

// LineOf is the line a stored task is on, or would be bound to by the rules
// if it is on none yet: the second answer says which.
func LineOf(r Record) (workID, from string, bound bool) {
	if r.WorkID != "" {
		from = r.WorkFrom
		if from == "" {
			from = work.WorkNamed
		}
		return r.WorkID, from, true
	}
	workID, from = work.LineOf(lineFacts(r))
	return workID, from, false
}

// BindWork puts a task on a line of work — its record's work_id — and its
// root's to-do with it, in one transaction, with a `task.bound` event that
// says how. A task already on that line is left as it is; one on another line
// is refused (409 work_id_mismatch): a task is bound once, and moving it would
// move every fact it has to a different item.
func (b *Broker) BindWork(ctx context.Context, taskID, workID, from string) error {
	if !IsTaskID(taskID) || !IsTaskID(workID) {
		return refuse(http.StatusUnprocessableEntity, "bad_task", "task_id and work_id are lowercase UUIDs.")
	}
	_, _, err := b.mutateEvent(ctx, taskID, "task.bound", map[string]any{"work_id": workID, "work_from": from},
		func(_ *store.Tx, r *Record) ([]store.Effect, error) {
			switch r.WorkID {
			case workID:
				return nil, errUnchanged
			case "":
				r.WorkID, r.WorkFrom = workID, from
				return nil, nil
			}
			return nil, refuseWith(http.StatusConflict, "work_id_mismatch",
				"That task is already on another line of work; a task is bound once.",
				map[string]any{"task": taskID, "work_id": r.WorkID})
		})
	return err
}

// ——— The work item a dispatch names (D36, BD-4) ———

// A work_id a dispatch names is the one place where a dispatch says which of
// a person's lines it is serving, and until now the broker only checked its
// shape. A name that matched no item bound the task to a line no board row
// follows, and the item it was supposed to be serving stayed in the Backlog
// with nothing on it — which is how 52 dispatches in one day came to say
// nothing about the 14 items they were for.
//
// So a named item is read before anything exists (checkNamedWork), and the
// dispatch is what puts it on the board (commitNamedWork): a root sending
// work out has committed to it, and nothing was ever going to make a person
// press "track" a second time to say so.

// TaskFactsOf is what the board's rules read of one task record. It is here,
// beside Decode, so that the broker and the board read a task the same way:
// app.Facts is this function over the stored rows.
func TaskFactsOf(r Record) work.TaskFacts {
	f := work.TaskFacts{
		Task: r.ID, WorkID: r.WorkID, Title: r.Title, Kind: r.Kind, Project: r.ProjectDir,
		CreatedAt: atSecond(r.CreatedAt), State: string(r.State), Ended: r.State.Terminal(),
		Attempt: r.RespawnGeneration, FinishedAt: atSecond(r.FinishedAt),
	}
	if r.Root != nil && !r.Root.PollOnly {
		f.Owner, f.OwnerAssistant = r.Root.SessionID, r.Root.Assistant
	}
	if r.Landing != nil {
		f.Landing, f.LandedAt = workLandingState(r.Landing.State), atSecond(r.Landing.At)
		f.LandingTarget = r.Landing.Target
	}
	return f
}

// workLandingState maps the richer landing ledger onto the board and to-do
// rules' older outcome vocabulary. Incorporated is a proved settlement, so it
// closes the obligation as landed while the landing record keeps the exact
// distinction for task and console readers.
func workLandingState(state LandingState) string {
	if state == LandingIncorporated {
		return string(LandingLanded)
	}
	return string(state)
}

// atSecond is a time at the store's resolution, so a task's time and an
// item's compare the way the store's own queries compare them.
func atSecond(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return time.Unix(t.Unix(), 0)
}

// checkNamedWork refuses a dispatch that names a work item it cannot be bound
// to, before the task, its tab or its checkout exist. A store that did not
// answer is not an absent item: it refuses too, because admitting the
// dispatch would decide the question by not asking it.
func (b *Broker) checkNamedWork(ctx context.Context, r Record) error {
	if r.WorkID == "" || r.WorkFrom != work.WorkNamed {
		return nil
	}
	if err := b.checkLineWork(ctx, r); err != nil {
		return err
	}
	return b.checkAlsoWork(ctx, r)
}

// checkAlsoWork refuses a dispatch carrying a further item it cannot be
// bound to. Each is a Board (v2) item, because only a Board item finds its
// tasks by every id the task carries (store's WorkV2Tx.Tasks); the legacy
// board reads work_id alone. Each is checked as work_id is: same Project, not
// closed, and a store that did not answer refuses.
func (b *Broker) checkAlsoWork(ctx context.Context, r Record) error {
	for _, id := range r.AlsoWorkIDs {
		v2, err := b.Store.WorkV2Item(ctx, id)
		switch {
		case errors.Is(err, store.ErrNoWorkV2):
			return refuseWith(http.StatusUnprocessableEntity, "also_work_not_found",
				"also_work_ids names "+id+", which is no Board item. Name Board items only, or dispatch it separately.",
				map[string]any{"work_id": id})
		case err != nil:
			return refuse(http.StatusServiceUnavailable, "store_unavailable",
				"The board could not be read, so the work items this dispatch names could not be checked; nothing was started.")
		}
		also := r
		also.WorkID = id
		if err := nameableV2(v2, also); err != nil {
			return err
		}
	}
	return nil
}

// checkLineWork is checkNamedWork for the task's own line, work_id.
func (b *Broker) checkLineWork(ctx context.Context, r Record) error {
	it, err := b.Store.WorkItem(ctx, r.WorkID)
	found := true
	switch {
	case errors.Is(err, store.ErrNoWork):
		// A Board (v2) item is a line too: an Epic's owner dispatches the
		// review of its plan with --work-id <item> (work-system.md).
		if v2, v2err := b.Store.WorkV2Item(ctx, r.WorkID); v2err == nil {
			if err := nameableV2(v2, r); err != nil {
				return err
			}
			return b.checkPlanReviewBlocking(ctx, v2, r)
		} else if !errors.Is(v2err, store.ErrNoWorkV2) {
			return refuse(http.StatusServiceUnavailable, "store_unavailable",
				"The board could not be read, so the work item this dispatch names could not be checked; nothing was started.")
		}
		found = false
	case err != nil:
		return refuse(http.StatusServiceUnavailable, "store_unavailable",
			"The board could not be read, so the work item this dispatch names could not be checked; nothing was started.")
	}
	var ref *work.Refusal
	if err := work.Nameable(it, found, r.ProjectDir); errors.As(err, &ref) {
		return refuseWith(ref.Status, ref.Code, ref.Message, map[string]any{"work_id": r.WorkID})
	}
	return nil
}

// commitNamedWork puts the item a dispatch named onto the board, with the
// move that says which root committed to it. It runs once the task is
// durable, because the commitment rests on the task: an item moved by a
// dispatch that then failed to be recorded would be a commitment to nothing.
//
// Its failure does not fail the dispatch — the work has started, and saying
// otherwise would leave a task running that its caller believes was refused.
// The sweep's dispatch rule makes the same change within a tick, from the
// same facts; the dispatcher is told with a warning that it has not happened
// yet.
func (b *Broker) commitNamedWork(ctx context.Context, r Record) error {
	if r.WorkID == "" || r.WorkFrom != work.WorkNamed {
		return nil
	}
	f := TaskFactsOf(r)
	return b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		it, err := tx.Item(r.WorkID)
		if errors.Is(err, store.ErrNoWork) {
			// A Board (v2) item: its owner and phase are the Session's to
			// move, and the task is found on it by its work_id.
			return nil
		}
		if err != nil {
			return err
		}
		// Decided again here, inside the write, from the row as it is now: a
		// person may have moved the item between the check and this (DG-6).
		c, ok := work.DispatchChange(it, f)
		if !ok {
			return nil
		}
		now := b.now()
		return tx.Put(it, c.Apply(it, now), store.MoveOf(it.ID, c, now))
	})
}

// checkPlanReviewBlocking applies the planning gate's blocking-finding rule
// to a dispatch bound to a Board item (work.PlanReviewDispatchGate): while the
// item's latest plan review has a blocking finding, only a new plan review is
// dispatched for it. A store that did not answer refuses, as above.
func (b *Broker) checkPlanReviewBlocking(ctx context.Context, it work.ItemV2, r Record) error {
	if r.Kind == work.DocumentPlanReview || it.Phase != work.PhaseAssigned {
		return nil
	}
	unavailable := refuse(http.StatusServiceUnavailable, "store_unavailable",
		"The board could not be read, so the plan review this dispatch's item stands on could not be checked; nothing was started.")
	docs, err := b.Store.WorkV2Documents(ctx, it.ID)
	if err != nil {
		return unavailable
	}
	review := work.PlanReviewSummary{Legacy: true}
	ref := ""
	for _, d := range docs {
		if d.Role == work.DocumentPlanReview {
			ref = d.Reference
		}
	}
	if ref != "" {
		row, err := b.Store.BrokerTask(ctx, ref)
		switch {
		case errors.Is(err, store.ErrNoTask):
		case err != nil:
			return unavailable
		default:
			if rec, err := Decode(row.Record); err == nil && rec.Result != nil {
				review = work.ReadPlanReview(rec.Result.Review)
			}
		}
	}
	var refusal work.RefusalV2
	if err := work.PlanReviewDispatchGate(it, r.Kind, docs, review); errors.As(err, &refusal) {
		return refuseWith(http.StatusUnprocessableEntity, refusal.Code, refusal.Message, map[string]any{"work_id": r.WorkID})
	} else if err != nil {
		return err
	}
	return nil
}

// nameableV2 is Nameable for a Board (v2) item: in this dispatch's Project and
// not closed.
func nameableV2(it work.ItemV2, r Record) error {
	switch {
	case it.ProjectPath != r.ProjectDir:
		return refuseWith(http.StatusUnprocessableEntity, work.RefusedWorkOtherProject,
			"That Board item belongs to "+it.ProjectPath+", and this dispatch is in "+r.ProjectDir+
				". An item follows the work of one project.", map[string]any{"work_id": r.WorkID})
	case it.Phase.Terminal():
		return refuseWith(http.StatusUnprocessableEntity, work.RefusedWorkClosed,
			"That Board item is already "+string(it.Phase)+". Name an open item, or leave work_id out.",
			map[string]any{"work_id": r.WorkID})
	}
	return nil
}
