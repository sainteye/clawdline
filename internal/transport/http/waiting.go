package http

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The waiting push's wiring (app.Waiting, docs/push.md "有人在等你回答").

var waitingByServer sync.Map // *Server -> *app.Waiting

// waiting is this server's watcher of sessions standing on a question.
func (s *Server) waiting() *app.Waiting {
	if w, ok := waitingByServer.Load(s); ok {
		return w.(*app.Waiting)
	}
	w := &app.Waiting{Store: s.store, After: waitingAfter(), Enabled: s.agentNotifyEnabled}
	if s.broker != nil {
		// The push goes out the way the capacity push does: recorded with its
		// decision in one transaction, then run by the broker's outbox, whose
		// recovery never sends a begun push twice.
		w.Run = s.broker.RunEffects
		w.Role = s.waitingRole
	}
	got, _ := waitingByServer.LoadOrStore(s, w)
	return got.(*app.Waiting)
}

// agentNotifyEnabled is the person's `orchestrator_agent_notify` switch, read
// at every use so turning it off needs no restart. It is the one switch there
// is for pushes about what sessions are doing; absent or unreadable is on.
func (s *Server) agentNotifyEnabled() bool {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return true
	}
	on, ok := values.Bool("orchestrator_agent_notify")
	return !ok || on
}

// waitingRole is the task whose tab a terminal is, the newest when a terminal
// was more than one child's. Asked only when a stop is due.
func (s *Server) waitingRole(ctx context.Context, terminal string) (app.WaitingRole, bool) {
	records, _, err := s.broker.Records(ctx)
	if err != nil || terminal == "" {
		return app.WaitingRole{}, false
	}
	found := false
	var role app.WaitingRole
	var newest time.Time
	for _, r := range records {
		if r.ChildTerminalID != terminal || (found && !r.CreatedAt.After(newest)) {
			continue
		}
		found, newest = true, r.CreatedAt
		role = app.WaitingRole{Child: true, Live: !r.State.Terminal(), Title: r.Title, Deadline: r.Deadline()}
	}
	return role, found
}

// waitingAfter is CLAWDLINE_NEXT_WAITING_AFTER, for a person trying the push
// on a daemon of their own; the watcher only ever takes it lower.
func waitingAfter() time.Duration {
	if v := os.Getenv("CLAWDLINE_NEXT_WAITING_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 0
}

// watchWaiting reads the session list on the board's clock and hands it to
// the watcher. It takes whatever reading is held while that is younger than
// its own tick — the broker's beat takes one every five seconds — so on a
// running daemon it costs no scan of its own; only when nothing else has read
// the machine for a whole tick does it share in a new one.
func (s *Server) watchWaiting(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	said := ""
	for {
		if _, err := s.waiting().Observe(ctx, s.readingWithin(ctx, tick)); err != nil {
			// Said once until it changes: the sweep runs every few seconds,
			// and the same line every few seconds buries the ones that matter.
			if err.Error() != said {
				log.Printf("waiting push: %v", err)
			}
			said = err.Error()
		} else {
			said = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// readingWithin is Server.reading for a reader on a slower clock
// (app.InventoryReading.Within).
func (s *Server) readingWithin(ctx context.Context, age time.Duration) session.Inventory {
	if s.readings == nil {
		return s.inventory.Read(ctx)
	}
	return s.readings.Within(ctx, age)
}
