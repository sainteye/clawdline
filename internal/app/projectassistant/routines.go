package projectassistant

import (
	"context"
	"errors"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Routine changes enter only through an execution-bound host capability. This
// method rechecks its full fence under the same actor lock as chat admission.
func (r *Engine) Routine(ctx context.Context, f pa.Fence, turnID string, q RoutineRequest) (RoutineResult, error) {
	a, err := r.acquire(f.Key)
	if err != nil {
		return RoutineResult{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err := r.o.Repository.Bot(ctx, f.Key)
	if err != nil {
		return RoutineResult{}, err
	}
	if err = r.authorized(ctx, f.Key, bot.Project); err != nil {
		return RoutineResult{}, err
	}
	x, err := r.o.Repository.Export(ctx, f.Key)
	if err != nil {
		return RoutineResult{}, err
	}
	if err = pa.CheckFence(f, currentFence(bot, x.Conversation)); err != nil {
		return RoutineResult{}, err
	}
	if x.Conversation.ActiveTurnID != turnID || a.turnID != turnID {
		return RoutineResult{}, runtimeFailure(pa.StaleGeneration, "routine_tool_turn")
	}
	if q.Action == "list" {
		out := []pa.Routine{}
		for _, d := range x.Routines {
			if !d.Deleted {
				out = append(out, d)
			}
		}
		return RoutineResult{Definitions: out}, nil
	}
	if q.IdempotencyKey == "" || len(q.IdempotencyKey) > pa.MaxNameRunes {
		return RoutineResult{}, runtimeFailure(pa.Invalid, "routine_command")
	}
	payload := struct {
		Action  string
		Request RoutineRequest
	}{q.Action, q}
	cmd := r.command(f.Key, pa.EditOperation, "routine-"+turnID+"-"+q.IdempotencyKey, payload)
	if prior, e := r.replay(ctx, cmd, pa.EditOperation); e != nil {
		return RoutineResult{}, e
	} else if prior != nil {
		return RoutineResult{ID: prior.ResourceID}, nil
	}
	// Do not accept a schedule whose isolated runs would replace the main
	// native history on an adapter that cannot resume it.
	if q.Action == "save" || q.Action == "test" || q.Action == "enable" && q.Enabled != nil && *q.Enabled {
		caps, e := r.o.Provider.Capabilities(ctx, bot)
		if e != nil {
			return RoutineResult{}, e
		}
		if e = pa.CheckExecutionCapabilities(caps, f); e != nil {
			return RoutineResult{}, e
		}
		if !caps.Resume {
			return RoutineResult{}, runtimeFailure(pa.ResumeUnsupported, "routine_main_session")
		}
	}
	var d pa.Routine
	found := false
	for _, v := range x.Routines {
		if v.ID == q.ID && !v.Deleted {
			d = v
			found = true
			break
		}
	}
	if q.ID != "" && !found {
		return RoutineResult{}, runtimeFailure(pa.NotFound, "routine")
	}
	m := Mutation{Command: cmd, Receipt: pa.Receipt{State: pa.EffectDelivered}}
	switch q.Action {
	case "save":
		if !found {
			d = pa.Routine{ID: r.o.ID(), Key: f.Key, Project: f.Project, ConversationID: f.ConversationID, Enabled: true}
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
			return RoutineResult{}, runtimeFailure(pa.Invalid, "routine_schedule_conflict")
		}
		if triggers == nil {
			triggers = []pa.RoutineTrigger{{Type: "interval", Seconds: q.IntervalSeconds}}
		}
		if !found {
			old = pa.Routine{}
		}
		d, err = pa.SetRoutineSchedule(d, old, triggers, r.o.Clock())
		if err != nil {
			return RoutineResult{}, err
		}
		d.Version++
		if err = pa.ValidateRoutine(d); err != nil {
			return RoutineResult{}, err
		}
		m.Routine = &d
		m.Receipt.ResourceID = d.ID
	case "enable", "delete":
		if !found {
			return RoutineResult{}, runtimeFailure(pa.NotFound, "routine")
		}
		if q.Action == "delete" {
			d.Deleted = true
			d.Enabled = false
		} else {
			if q.Enabled == nil {
				return RoutineResult{}, runtimeFailure(pa.Invalid, "routine_enabled")
			}
			old := d
			d.Enabled = *q.Enabled
			if d.Enabled && !old.Enabled {
				d, err = pa.SetRoutineSchedule(d, old, d.ScheduleTriggers(), r.o.Clock())
				if err != nil {
					return RoutineResult{}, err
				}
			}
		}
		d.Version++
		m.Routine = &d
		m.Receipt.ResourceID = d.ID
	case "test":
		if !found {
			return RoutineResult{}, runtimeFailure(pa.Revoked, "routine_paused")
		}
		for _, run := range x.RoutineRuns {
			if run.RoutineID == d.ID && run.State == pa.TurnQueued {
				m.Receipt.ResourceID = run.ID
				break
			}
		}
		if m.Receipt.ResourceID != "" {
			break
		}
		run := r.newRoutineRun(d)
		run.Source = "test"
		m.RoutineRun = &run
		m.Receipt.ResourceID = run.ID
	default:
		return RoutineResult{}, runtimeFailure(pa.Unsupported, "routine_action")
	}
	// Revalidate after the export and before the effect, including source revocation.
	if err = r.authorized(ctx, f.Key, bot.Project); err != nil {
		return RoutineResult{}, err
	}
	rec, err := r.mutate(ctx, bot, m)
	return RoutineResult{ID: rec.ResourceID}, err
}
func (r *Engine) newRoutineRun(d pa.Routine) pa.RoutineRun {
	return pa.RoutineRun{Source: "schedule", ID: r.o.ID(), RoutineID: d.ID, Key: d.Key, Project: d.Project, ConversationID: d.ConversationID, State: pa.TurnQueued, CreatedAt: r.o.Clock(), TimeoutSeconds: d.TimeoutSeconds}
}

// TickRoutines advances one Bot under its existing actor; there is no new
// provider or goroutine per definition. Unknown execution is a durable barrier.
func (r *Engine) TickRoutines(ctx context.Context, key pa.BotKey) error {
	a, err := r.acquire(key)
	if err != nil {
		return err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return err
	}
	if err = r.authorized(ctx, key, bot.Project); err != nil {
		return err
	}
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return err
	}
	now := r.o.Clock()
	for _, d := range x.Routines {
		if d.Deleted || !d.Enabled || d.NextDue.IsZero() || now.Before(d.NextDue) {
			continue
		}
		exists := false
		for _, run := range x.RoutineRuns {
			if run.RoutineID == d.ID && run.State == pa.TurnQueued {
				exists = true
			}
		}
		cmd := r.command(key, pa.EditOperation, "routine-due-"+d.ID+"-"+d.NextDue.UTC().Format(time.RFC3339Nano), d.NextDue)
		advanced, advanceErr := pa.AdvanceRoutineSchedule(d, now)
		m := Mutation{Command: cmd, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: d.ID}}
		if advanceErr != nil {
			var failure *pa.Failure
			if !errors.As(advanceErr, &failure) || failure.Code != pa.Invalid || failure.Field != "routine_cron_future" {
				return advanceErr
			}
			// Pause only this definition, retaining every original deadline and
			// trigger (Advance may have partially advanced an OR schedule).
			// Existing terminal run/turn events report a scheduling failure
			// without claiming a native prompt ever started.
			d.Enabled = false
			run := r.newRoutineRun(d)
			run.State, run.FinishedAt, run.Published = pa.TurnFailed, &now, true
			f := currentFence(bot, x.Conversation)
			turn := pa.Turn{ID: "routine-turn-" + run.ID, RoutineRunID: run.ID, Fence: f, MessageID: "routine-notice-" + run.ID, State: pa.TurnFailed, IdempotencyKey: "routine-" + run.ID, Failure: failure}
			run.TurnID = turn.ID
			msg := pa.Message{ID: turn.MessageID, Fence: f, TurnID: turn.ID, Kind: pa.NoticeMessage, Text: d.Name, At: now}
			m.RoutineRun, m.Turn, m.Messages = &run, &turn, []pa.Message{msg}
			m.Receipt.State = pa.EffectFailed
			m.Events = []pa.Event{{Fence: f, Kind: pa.MessageCreated, Message: &msg}, {Fence: f, Kind: pa.TurnUpdated, Turn: &turn}}
		} else if !exists {
			d = advanced
			run := r.newRoutineRun(d)
			m.RoutineRun = &run
		} else {
			d = advanced
		}
		d.Version++
		m.Routine = &d
		if _, err = r.mutate(ctx, bot, m); err != nil {
			return err
		}
		x, err = r.o.Repository.Export(ctx, key)
		if err != nil {
			return err
		}
		bot = x.Bot
	}
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
	for _, t := range pending {
		if t.State == pa.TurnUnknown {
			return nil
		}
		// Retain one coalesced wake, but never queue another routine turn
		// behind an active one. A paused definition can cancel that wake.
		if t.RoutineRunID != "" {
			if t.State == pa.TurnQueued && !a.working {
				r.setProject(a, bot.Project)
				a.working = true
				r.mu.Lock()
				a.users++
				r.mu.Unlock()
				go r.work(a)
			}
			return nil
		}
	}
	if len(pending) >= pa.MaxPendingTurns {
		return nil
	}
	for _, run := range x.RoutineRuns {
		if run.State != pa.TurnQueued || run.TurnID != "" {
			continue
		}
		var d pa.Routine
		for _, def := range x.Routines {
			if def.ID == run.RoutineID {
				d = def
				break
			}
		}
		if d.Deleted || !d.Enabled && run.Source != "test" {
			run.State = pa.TurnCancelled
			at := now
			run.FinishedAt = &at
			run.Published = true
			_, err = r.mutate(ctx, bot, Mutation{Command: r.command(key, pa.EditOperation, "routine-cancel-"+run.ID, run.ID), RoutineRun: &run, Receipt: pa.Receipt{State: pa.EffectFailed, ResourceID: run.ID}})
			return err
		}
		c := x.Conversation
		if c.BindingID == "" {
			return runtimeFailure(pa.ResumeUnsupported, "routine_main_session")
		}
		f := currentFence(bot, c)
		turn := pa.Turn{ID: "routine-turn-" + run.ID, RoutineRunID: run.ID, Fence: f, MessageID: "routine-notice-" + run.ID, State: pa.TurnQueued, IdempotencyKey: "routine-" + run.ID}
		// The notice is provenance, never the hidden instruction presented as a person.
		msg := pa.Message{ID: turn.MessageID, Fence: f, TurnID: turn.ID, Kind: pa.NoticeMessage, Text: d.Name, At: now}
		run.TurnID = turn.ID
		_, err = r.mutate(ctx, bot, Mutation{Command: r.command(key, pa.SendOperation, "routine-admit-"+run.ID, run.ID), RoutineRun: &run, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: turn.ID}, Events: []pa.Event{{Fence: f, Kind: pa.MessageCreated, Message: &msg}, {Fence: f, Kind: pa.TurnUpdated, Turn: &turn}}})
		if err != nil {
			return err
		}
		r.setProject(a, bot.Project)
		if !a.working {
			a.working = true
			r.mu.Lock()
			a.users++
			r.mu.Unlock()
			go r.work(a)
		}
		return nil
	}
	return nil
}
func routineForTurn(x Export, t pa.Turn) (pa.Routine, pa.RoutineRun, error) {
	var run pa.RoutineRun
	for _, v := range x.RoutineRuns {
		if v.ID == t.RoutineRunID {
			run = v
			break
		}
	}
	for _, d := range x.Routines {
		if d.ID == run.RoutineID && d.Key == t.Fence.Key && d.Project == t.Fence.Project && d.ConversationID == t.Fence.ConversationID && !d.Deleted && (d.Enabled || run.Source == "test") {
			return d, run, nil
		}
	}
	return pa.Routine{}, run, runtimeFailure(pa.Revoked, "routine_paused")
}
