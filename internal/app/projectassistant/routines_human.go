package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strings"
	"time"
)

// HumanRoutineCommand carries a human command, never a native tool identity.
type HumanRoutineCommand struct {
	Command          pa.Command
	Fence            pa.Fence
	ExpectedRevision int64
	Request          RoutineRequest
}

type HumanRoutineCapabilities interface {
	CheckHumanRoutine(context.Context, pa.Bot, pa.Fence) error
}

// Capability inspection starts no native session and allocates no actor slot.
func (r *Engine) CheckHumanRoutine(ctx context.Context, bot pa.Bot, f pa.Fence) error {
	if err := r.authorized(ctx, bot.Key, bot.Project); err != nil {
		return err
	}
	caps, err := r.o.Provider.Capabilities(ctx, bot)
	if err != nil {
		return err
	}
	if err = pa.CheckExecutionCapabilities(caps, f); err != nil {
		return err
	}
	if !caps.Resume {
		return runtimeFailure(pa.ResumeUnsupported, "routine_main_session")
	}
	return nil
}

// Both native and human admission use this pure mutation builder. Each caller
// retains its own authority, replay and compare-and-swap boundaries.
func prepareRoutineMutation(f pa.Fence, q RoutineRequest, cmd pa.Command, definitions []pa.Routine, runs []pa.RoutineRun, now time.Time, id func() string, newRun func(pa.Routine) pa.RoutineRun) (Mutation, error) {
	var err error
	var d pa.Routine
	found := false
	for _, v := range definitions {
		if v.ID == q.ID && !v.Deleted {
			d = v
			found = true
			break
		}
	}
	if q.ID != "" && !found {
		return Mutation{}, runtimeFailure(pa.NotFound, "routine")
	}
	m := Mutation{Command: cmd, Receipt: pa.Receipt{State: pa.EffectDelivered}}
	switch q.Action {
	case "save":
		if !found {
			d = pa.Routine{ID: id(), Key: f.Key, Project: f.Project, ConversationID: f.ConversationID, Enabled: true}
		}
		old := d
		d.Name = q.Name
		d.Instruction = q.Instruction
		if q.TimeoutSeconds != 0 {
			d.TimeoutSeconds = q.TimeoutSeconds
		}
		if d.TimeoutSeconds == 0 {
			d.TimeoutSeconds = 3600
		}
		if q.Enabled != nil {
			d.Enabled = *q.Enabled
		}
		triggers := q.Triggers
		if triggers != nil && q.IntervalSeconds != 0 {
			return Mutation{}, runtimeFailure(pa.Invalid, "routine_schedule_conflict")
		}
		if triggers == nil {
			triggers = []pa.RoutineTrigger{{Type: "interval", Seconds: q.IntervalSeconds}}
		}
		if !found {
			old = pa.Routine{}
		}
		d, err = pa.SetRoutineSchedule(d, old, triggers, now)
		if err != nil {
			return Mutation{}, err
		}
		d.Version++
		if err = pa.ValidateRoutine(d); err != nil {
			return Mutation{}, err
		}
		m.Routine = &d
		m.Receipt.ResourceID = d.ID
	case "save_progress":
		if !found {
			return Mutation{}, runtimeFailure(pa.NotFound, "routine")
		}
		content := strings.TrimSpace(q.Content)
		if err := pa.ValidateRoutineProgress(content); err != nil {
			return Mutation{}, err
		}
		d.Progress = content
		d.Version++
		m.Routine = &d
		m.Receipt.ResourceID = d.ID
	case "enable", "delete":
		if !found {
			return Mutation{}, runtimeFailure(pa.NotFound, "routine")
		}
		if q.Action == "delete" {
			d.Deleted = true
			d.Enabled = false
		} else {
			if q.Enabled == nil {
				return Mutation{}, runtimeFailure(pa.Invalid, "routine_enabled")
			}
			old := d
			d.Enabled = *q.Enabled
			if d.Enabled && !old.Enabled {
				d, err = pa.SetRoutineSchedule(d, old, d.ScheduleTriggers(), now)
				if err != nil {
					return Mutation{}, err
				}
			}
		}
		d.Version++
		m.Routine = &d
		m.Receipt.ResourceID = d.ID
	case "test":
		if !found {
			return Mutation{}, runtimeFailure(pa.Revoked, "routine_paused")
		}
		for _, run := range runs {
			if run.RoutineID == d.ID && run.State == pa.TurnQueued {
				m.Receipt.ResourceID = run.ID
				break
			}
		}
		if m.Receipt.ResourceID != "" {
			break
		}
		run := newRun(d)
		run.Source = "test"
		m.RoutineRun = &run
		m.Receipt.ResourceID = run.ID
	default:
		return Mutation{}, runtimeFailure(pa.Unsupported, "routine_action")
	}
	return m, nil
}
