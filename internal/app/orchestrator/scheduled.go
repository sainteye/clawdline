package orchestrator

import (
	"context"
	"net/http"
	"os"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

// A schedule's run is a task like any other (docs/design-decisions.md D07).
//
// There used to be two ways to start one. A session asked the broker, and a
// schedule asked `app.Dispatcher`, an older skeleton that kept its tasks in a
// table of its own. The two tables never met, so a scheduled run and a
// dispatched child could claim the same paths and neither was refused, and a
// scheduled run had no secret, no briefing and no clock: one that never wrote
// result.json counted as "still running" for ever and held back every later
// occurrence of its schedule. Now a schedule hands its occurrence here, and the
// broker runs it through Dispatch — the same arbitration, record, CHILD.md,
// secret, timeout and collection — with the two differences the Swift app has
// for a scheduled task and no others: it has no root, and its template may
// leave its claims undeclared (draft.go, admit).

// ScheduledRun is one occurrence of a stored schedule, handed to the broker.
type ScheduledRun struct {
	// TaskID is minted by the schedule, so that the schedule can write down
	// that it made this task before the task exists: a process that dies
	// between the two still leaves the schedule knowing its run.
	TaskID     string
	ScheduleID string
	// Title is the schedule's title: the label a scheduled task is known by
	// where a dispatched one shows its root.
	Title string
	// CloseTab is the schedule's close_tab: what the run's end does to the
	// tab it opens (tabPolicy). Empty is the schedule default, on_success.
	CloseTab string
	// Template is the schedule's `task` object as it is stored. It becomes
	// the task.json the task is admitted from, with the three keys the broker
	// owns — protocol, id and root — set over whatever the template says.
	Template            map[string]any
	Investigation       *schedulewebhook.InvestigationRef
	InvestigationBundle []byte
}

// ScheduledDispatch is what DispatchScheduled answers.
type ScheduledDispatch struct {
	Dispatched
	// Absent is the broker's positive answer that it holds no task with this
	// id: the run was refused before anything was recorded. False does not
	// mean the task exists — a store that could not be asked has not said —
	// and so false is never a reason to forget the run.
	Absent bool
}

// DispatchScheduled writes the task.json a schedule's template makes, mints the
// task's secret, and admits it through Dispatch.
//
// The secret is minted here and travels exactly as a caller's does: into the
// child's composer, once, and never back out. The schedule that asked holds
// nothing that authenticates the child; only the child does.
func (b *Broker) DispatchScheduled(ctx context.Context, run ScheduledRun) (ScheduledDispatch, error) {
	if !IsTaskID(run.TaskID) {
		return ScheduledDispatch{Absent: true}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"task_id must be a lowercase UUID.")
	}
	if run.ScheduleID == "" {
		return ScheduledDispatch{Absent: true}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"A scheduled dispatch names the schedule it runs.")
	}
	if held, _, err := b.Record(ctx, run.TaskID); err == nil {
		if held.ScheduleID != run.ScheduleID || !sameInvestigationOrigin(held.Investigation, run.Investigation) {
			return ScheduledDispatch{}, refuse(409, "delivery_conflict", "The task belongs to another schedule.")
		}
		return ScheduledDispatch{Dispatched: Dispatched{Record: held, Replayed: true}}, nil
	} else if !isNotFound(err) {
		return ScheduledDispatch{}, err
	}
	brief := make(map[string]any, len(run.Template)+3)
	for k, v := range run.Template {
		brief[k] = v
	}
	if run.Investigation != nil {
		if run.Template["kind"] != schedulewebhook.InvestigationKind || run.Investigation.Validate() != nil {
			return ScheduledDispatch{Absent: true}, refuse(422, "schedule_invalid", "The schedule is not investigation-bound.")
		}
		path, err := b.Tasks.WriteInvestigationEvidence(run.TaskID, run.InvestigationBundle)
		if err != nil {
			return ScheduledDispatch{Absent: true}, refuse(500, "investigation_evidence_unavailable", "The investigation evidence could not be saved.")
		}
		instructions, _ := brief["instructions"].(string)
		brief["instructions"] = instructions + schedulewebhook.InvestigationInstructions(run.TaskID, run.ScheduleID, path, *run.Investigation)
	}
	brief["clawdline_protocol"] = Protocol
	brief["task_id"] = run.TaskID
	brief["root"] = map[string]any{"session_id": nil, "label": run.Title}
	if err := b.writeBriefOnce(run.TaskID, brief); err != nil {
		// A capacity refusal or crash may leave the brief before admission.
		// Only the identical bounded file can be retried under this task id.
		if !os.IsExist(err) || !b.Tasks.MatchesScheduledBrief(run.TaskID, brief) {
			return ScheduledDispatch{Absent: true}, refuse(http.StatusInternalServerError, "internal",
				"Could not write the scheduled task file.")
		}
	}
	out, err := b.Dispatch(ctx, DispatchRequest{
		TaskID:   run.TaskID,
		Secret:   NewSecret(),
		Schedule: &ScheduleOrigin{ID: run.ScheduleID, Title: run.Title, CloseTab: run.CloseTab, Investigation: run.Investigation},
	})
	if err == nil {
		return ScheduledDispatch{Dispatched: out}, nil
	}
	_, _, held := b.Record(ctx, run.TaskID)
	return ScheduledDispatch{Dispatched: out, Absent: isNotFound(held)}, err
}

// scheduleOf is the schedule a task belongs to, for a dispatch that repeats
// it (respawn): a scheduled task retried is still that schedule's, and still
// has no root to resolve.
func scheduleOf(r Record) *ScheduleOrigin {
	if r.ScheduleID == "" {
		return nil
	}
	return &ScheduleOrigin{ID: r.ScheduleID, Title: r.ScheduleTitle, CloseTab: r.ScheduleCloseTab, Investigation: r.Investigation}
}

func sameInvestigationOrigin(a, b *schedulewebhook.InvestigationRef) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
