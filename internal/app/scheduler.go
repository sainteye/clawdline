package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/schedule"
)

// Pulse is what one pass of the scheduler says about itself.
//
// It exists because a pass that fired nothing and a scheduler that stopped
// look identical from outside: both are silence. A clock that dispatches work
// has to be able to say it is still running, in the same way a scan has to say
// whether it saw everything.
type Pulse struct {
	At         time.Time
	Considered int
	// Due is how many came due on this pass. It is separate from Fired because
	// "nothing was due" and "something was due and the dispatch was refused"
	// are different states, and a zero Fired alone cannot tell them apart —
	// which also means a zero Fired alone cannot prove the rule that stops a
	// schedule firing on sight is the reason nothing fired.
	Due   int
	Fired int
	// Note says why a pass did nothing, when the reason was not "nothing was
	// due". An empty Note with Considered at zero means there are no schedules;
	// a Note means the pass could not read what it needed.
	Note string
}

// Scheduler is the minute timer: it asks the book what each schedule's latest
// occurrence calls for, and runs, skips or records it.
type Scheduler struct {
	Book *ScheduleBook
	Tick time.Duration
	// Report is called once per pass, whatever the pass did. Nil is allowed so
	// a caller that does not watch the clock need not pretend to.
	Report func(Pulse)
}

// Run watches the clock until the context ends. The first pass is one tick
// after start, never at start: a daemon that has just come up has not been
// asked to do anything yet, and the catch-up rule below already covers an
// occurrence it slept through.
func (s Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick == 0 {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p := s.Book.Beat(ctx)
			if s.Report != nil {
				s.Report(p)
			}
		}
	}
}

// Beat is one pass: `scheduleBeat` and `runScheduledFire` in the Swift app,
// on one lane with manual runs.
//
// Only the latest occurrence of each schedule is ever asked about, so a daemon
// that was down for three days runs at most one catch-up per schedule, inside
// its window, and never a batch. The occurrence it decides is written down
// before anything else happens and survives a restart, so the same occurrence
// is never decided twice.
func (b *ScheduleBook) Beat(ctx context.Context) Pulse {
	now := b.now()
	pulse := Pulse{At: now}
	if !b.dispatchEnabled() {
		pulse.Note = "task dispatch is switched off"
		return pulse
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	inv, err := b.load(ctx)
	if err != nil {
		log.Printf("schedules unreadable: %v", err)
		pulse.Note = "schedules unreadable"
		return pulse
	}
	pulse.Considered = len(inv.valid)
	runs, err := b.runsBySchedule(ctx)
	if err != nil {
		log.Printf("schedule runs unreadable, firing nothing: %v", err)
		pulse.Note = "schedule runs unreadable"
		return pulse
	}

	for _, h := range inv.valid {
		s := h.s
		if !s.Enabled {
			continue
		}
		fire := s.When.LatestFire(now, b.loc())
		if fire.IsZero() {
			continue
		}
		// Whether a run is still working is the broker's answer, read off its
		// row: its beat has already collected the result or run the timeout,
		// so a run that never wrote a result stops holding this schedule back
		// the moment its clock runs out (D07, D53).
		list := runs[s.ID]
		o := schedule.Occurrence{Now: now, Fire: fire, FirstSeen: h.f.FirstSeen, Handled: h.f.LastFire}
		for _, r := range list {
			if r.Created.After(o.LastRun) {
				o.LastRun = r.Created
			}
			if r.State != "" && !finished(r.State) {
				o.Active = true
			}
		}
		switch s.Decide(o) {
		case schedule.Run:
			pulse.Due++
			if b.fireOne(ctx, s, fire, h.f.LastFire) {
				pulse.Fired++
			}
		case schedule.Active:
			_ = b.Store.MarkScheduleFire(ctx, s.ID, fire, false)
			b.audit("orchestrator.schedule.skipped", map[string]string{"schedule": s.ID, "why": "active"})
		case schedule.Missed:
			_ = b.Store.MarkScheduleFire(ctx, s.ID, fire, true)
			b.audit("orchestrator.schedule.skipped", map[string]string{"schedule": s.ID, "why": "missed"})
			if s.NotifyOnFailure && b.Notify != nil {
				b.Notify(ctx, s.Title, "Scheduled run missed its catch-up window.", "schedule-"+s.ID+"-missed")
			}
		}
	}
	return pulse
}

// fireOne dispatches one decided occurrence and reports whether a task was
// accepted.
//
// The row is read again first: the occurrence was decided a moment ago, and a
// delete or a save that moved it can land in between. The occurrence is then
// written down as decided before the dispatch is attempted, so a daemon that
// dies halfway through does not run it again when it comes back. A one-shot is
// spent only by a session that was really accepted; a refusal consumes the
// occurrence and leaves no stamp, except backpressure — `over_capacity`, and
// `terminal_busy`, the Swift app's full terminal queue — which is handed back
// so the next minute retries while the window lasts. That is the broker's
// admission speaking (G30): the lane this book holds only keeps a decision and
// the dispatch it makes together, and the broker is what says "not now".
func (b *ScheduleBook) fireOne(ctx context.Context, decided schedule.Schedule, fire, before time.Time) bool {
	fresh, ok, err := b.named(ctx, decided.ID)
	if err != nil {
		return false
	}
	if !ok || fresh.s.Stale(fire) {
		why := "retimed"
		if !ok {
			why = "removed"
		}
		b.audit("orchestrator.schedule.skipped", map[string]string{"schedule": decided.ID, "why": why})
		return false
	}
	s := fresh.s
	claimed, err := b.Store.ClaimScheduleFire(ctx, s.ID, fire)
	if err != nil || !claimed {
		// Not written down by this pass means not this pass's to run: either
		// the store refused, and running anyway is how a restart would run it
		// a second time, or something else decided it first.
		log.Printf("schedule %s: occurrence %s not claimed, not firing: %v", s.ID, fire.Format(time.RFC3339), err)
		return false
	}
	b.audit("orchestrator.schedule.run", map[string]string{"schedule": s.ID, "how": "timer",
		"fire": fmt.Sprint(fire.Unix())})
	taskID, _, _, warning, err := b.dispatch(ctx, s, fire, "timer")
	if err != nil {
		code := "dispatch_failed"
		var refusal orchestrator.Refusal
		if errors.As(err, &refusal) {
			code = refusal.Code
		}
		if backpressure(code) {
			_ = b.Store.RestoreScheduleFire(ctx, s.ID, fire, before)
		}
		b.audit("orchestrator.schedule.refused", map[string]string{"schedule": s.ID, "code": code, "why": err.Error()})
		if s.NotifyOnFailure && b.Notify != nil {
			b.Notify(ctx, s.Title, "Scheduled run could not start: "+code, "schedule-"+s.ID+"-refused")
		}
		return false
	}
	if s.When.Once() {
		b.markFired(ctx, s.ID, fire)
	}
	if warning != "" {
		b.audit("orchestrator.schedule.brief_undelivered", map[string]string{"schedule": s.ID, "task": taskID})
	}
	log.Printf("schedule %s (%s) fired: task %s for %s", s.Title, s.ID, taskID, fire.Format(time.RFC3339))
	return true
}

// backpressure is a refusal that says "not now" rather than "no": the machine
// is full, and the same occurrence may run a minute later.
func backpressure(code string) bool {
	return code == "over_capacity" || code == "terminal_busy"
}
