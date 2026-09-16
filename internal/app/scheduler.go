package app

import (
	"context"
	"log"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/schedule"
	"github.com/sainteye/clawdline-go/internal/domain/task"
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

// Scheduler fires stored templates when they come due.
type Scheduler struct {
	Store      *store.Store
	Dispatcher Dispatcher
	Tick       time.Duration
	NewID      func() string
	// Report is called once per pass, whatever the pass did. Nil is allowed so
	// a caller that does not watch the clock need not pretend to.
	Report func(Pulse)
}

// Run watches the clock until the context ends.
//
// Firing marks the run before dispatching rather than after. A dispatch that
// fails has still consumed its turn: retrying it on the next tick would give a
// broken schedule a tight loop, and the honest record of "it was due, it was
// tried" is what a reader needs either way.
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
		case now := <-t.C:
			s.fireDue(ctx, now)
		}
	}
}

func (s Scheduler) fireDue(ctx context.Context, now time.Time) {
	pulse := Pulse{At: now}
	defer func() {
		if s.Report != nil {
			s.Report(pulse)
		}
	}()

	all, err := s.Store.Schedules(ctx)
	if err != nil {
		log.Printf("schedules unreadable: %v", err)
		pulse.Note = "schedules unreadable"
		return
	}
	pulse.Considered = len(all)
	live, err := s.Store.LiveTasks(ctx)
	if err != nil {
		log.Printf("live tasks unreadable, firing nothing: %v", err)
		pulse.Note = "live tasks unreadable"
		return
	}
	running := map[string]bool{}
	for _, t := range live {
		running[t.ID] = true
	}

	for _, sc := range all {
		if !sc.Due(now, running[sc.LastTask]) {
			continue
		}
		pulse.Due++
		if err := s.Store.MarkScheduleRun(ctx, sc.ID, now); err != nil {
			log.Printf("schedule %s: could not record the run: %v", sc.ID, err)
			continue
		}
		t := task.Task{
			ID:         s.NewID(),
			Assistant:  task.Assistant(sc.Assistant),
			ProjectDir: sc.Dir,
			Brief:      sc.Brief,
			Claims:     sc.Claims,
		}
		if t.Claims == nil {
			t.Claims = []string{}
		}
		if _, _, err := s.Dispatcher.Dispatch(ctx, t, string(t.Assistant)); err != nil {
			// A refusal is the ordinary outcome when the paths are busy, and it
			// is recorded rather than retried into a loop.
			log.Printf("schedule %s (%s): %v", sc.Name, sc.ID, err)
			continue
		}
		if err := s.Store.MarkScheduleTask(ctx, sc.ID, t.ID); err != nil {
			log.Printf("schedule %s: could not record its task: %v", sc.ID, err)
		}
		pulse.Fired++
		log.Printf("schedule %s fired: task %s", sc.Name, t.ID)
	}
}

var _ = schedule.Schedule{}
