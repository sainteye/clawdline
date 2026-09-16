package app

import (
	"context"
	"log"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/schedule"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// Scheduler fires stored templates when they come due.
type Scheduler struct {
	Store      *store.Store
	Dispatcher Dispatcher
	Tick       time.Duration
	NewID      func() string
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
	all, err := s.Store.Schedules(ctx)
	if err != nil {
		log.Printf("schedules unreadable: %v", err)
		return
	}
	live, err := s.Store.LiveTasks(ctx)
	if err != nil {
		log.Printf("live tasks unreadable, firing nothing: %v", err)
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
		log.Printf("schedule %s fired: task %s", sc.Name, t.ID)
	}
}

var _ = schedule.Schedule{}
