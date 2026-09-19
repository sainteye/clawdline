package orchestrator

import (
	"context"
	"net/http"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/work"
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
