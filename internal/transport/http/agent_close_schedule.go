package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A Session's own close, carried out when its turn ends.
//
// An agent runs `clawdline session close` from inside its own turn, so its
// terminal reads `working`, and `terminal_working` (mover: this session) made
// its closeability blocked every time: the command could never close the
// Session that ran it. When that is the only thing left, the close route
// records the request here and answers 202 `close_scheduled`; the sweep below
// audits the Session again once its terminal reads idle and closes it only if
// the whole audit is then safe.
//
// The requests live in memory. A daemon restart drops them; the agent, or the
// person, can ask again.

const (
	// maxScheduledCloses is how many closes may wait at once
	// (capacity.SessionCloseScheduled).
	maxScheduledCloses = 16
	// maxScheduledClosesPerTerminal is one: a repeat is the same request
	// (capacity.SessionCloseScheduledPerTerminal).
	maxScheduledClosesPerTerminal = 1
	// maxScheduledCloseWait is how long a request waits for its turn to end
	// before it is dropped (capacity.SessionCloseScheduledSeconds).
	maxScheduledCloseWait = 15 * time.Minute
)

// errCloseScheduleFull is a request refused at maxScheduledCloses.
var errCloseScheduleFull = errors.New("close_schedule_full")

// scheduledClose is one waiting request.
type scheduledClose struct {
	Terminal string
	// Caller is the conversation that asked: the Session must still hold it
	// when the close is carried out.
	Caller string
	// Actor is the principal the close's releases are written in the name of,
	// the same one the immediate close would have used.
	Actor string
	At    time.Time
}

// closeSchedule is the waiting requests, shared by the close route and the
// sweep. Its zero value is ready to use.
type closeSchedule struct {
	mu      sync.Mutex
	pending map[string]scheduledClose
	// now is the clock; nil is time.Now.
	now func() time.Time
}

func (c *closeSchedule) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// add records a request, answering the one now waiting for its terminal and
// whether it is new. A repeat for a terminal already waiting changes nothing.
func (c *closeSchedule) add(terminal, caller, actor string) (scheduledClose, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = map[string]scheduledClose{}
	}
	if have, ok := c.pending[terminal]; ok && have.Caller == caller {
		return have, false, nil
	}
	_, replacing := c.pending[terminal]
	if !replacing && len(c.pending) >= maxScheduledCloses {
		return scheduledClose{}, false, errCloseScheduleFull
	}
	sc := scheduledClose{Terminal: terminal, Caller: caller, Actor: actor, At: c.clock()}
	c.pending[terminal] = sc
	return sc, true, nil
}

// waiting is every request, oldest first.
func (c *closeSchedule) waiting() []scheduledClose {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]scheduledClose, 0, len(c.pending))
	for _, sc := range c.pending {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// settle removes a request, unless it was replaced while the sweep held it.
func (c *closeSchedule) settle(sc scheduledClose) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if have, ok := c.pending[sc.Terminal]; ok && have == sc {
		delete(c.pending, sc.Terminal)
	}
}

func (c *closeSchedule) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// onlyThisTurn reports whether the one thing between a Session closing itself
// and a safe close is the turn it is in.
func onlyThisTurn(audit agentCloseAudit) bool {
	if audit.Authority != "self" || audit.State != string(contract.CloseabilityStateBlocked) || len(audit.Reasons) == 0 {
		return false
	}
	for _, r := range audit.Reasons {
		if r.Code != "terminal_working" || r.SubjectID != audit.TerminalID {
			return false
		}
	}
	return true
}

// closeSweep is what one pass over the waiting requests uses; the server
// supplies its own, a test its fakes.
type closeSweep struct {
	audit  func(ctx context.Context, caller, terminal string) (agentCloseAudit, *agentCloseRefusal)
	close  func(ctx context.Context, sc scheduledClose) (session.Session, error)
	record func(ctx context.Context, kind, terminal string, payload map[string]any)
	closed func(ctx context.Context, audit agentCloseAudit, closed session.Session) error
}

// sweep carries out every waiting request whose turn has ended, keeps the
// ones still in their turn, and drops the rest with an event naming why.
func (c *closeSchedule) sweep(ctx context.Context, do closeSweep) {
	for _, sc := range c.waiting() {
		if ctx.Err() != nil {
			return
		}
		expired := c.clock().Sub(sc.At) >= maxScheduledCloseWait
		drop := func(why string, reasons []contract.CloseReason) {
			c.settle(sc)
			payload := map[string]any{"why": why, "conversation": sc.Caller}
			if len(reasons) > 0 {
				payload["reasons"] = reasons
			}
			do.record(ctx, "session.close_schedule_dropped", sc.Terminal, payload)
		}
		audit, refusal := do.audit(ctx, sc.Caller, sc.Terminal)
		switch {
		case refusal != nil && refusal.code == "close_inventory_unavailable":
			// Not read this time: no answer either way.
			if expired {
				drop("expired", nil)
			}
		case refusal != nil && refusal.code == "close_not_yours":
			drop("conversation_changed", nil)
		case refusal != nil:
			drop(refusal.code, nil)
		case audit.Authority != "self":
			drop("conversation_changed", nil)
		case onlyThisTurn(audit):
			if expired {
				drop("expired", audit.Reasons)
			}
		case audit.State == string(contract.CloseabilityStateSafe):
			c.settle(sc)
			closed, err := do.close(ctx, sc)
			if err != nil {
				do.record(ctx, "session.close_schedule_dropped", sc.Terminal,
					map[string]any{"why": "close_failed", "conversation": sc.Caller, "error": err.Error()})
				continue
			}
			if do.closed != nil {
				if err := do.closed(ctx, audit, closed); err != nil {
					do.record(ctx, "session.root_close_receipt_failed", sc.Terminal,
						map[string]any{"conversation": sc.Caller, "error": err.Error()})
				}
			}
			do.record(ctx, "session.closed", sc.Terminal,
				map[string]any{"conversation": sc.Caller, "scheduled_at": sc.At.UTC().Format(time.RFC3339)})
		default:
			drop(audit.State, audit.Reasons)
		}
	}
}

// scheduledCloseSweep is the server's pass: its own audit, close and store.
func (s *Server) scheduledCloseSweep(ctx context.Context) {
	if s.closes.count() == 0 {
		return
	}
	s.closes.sweep(ctx, closeSweep{
		audit: s.agentAuditClose,
		close: func(ctx context.Context, sc scheduledClose) (session.Session, error) {
			ctx, more := context.WithTimeout(ctx, closeBudget)
			defer more()
			return s.closeActionsAs(sc.Actor).Close(ctx, sc.Terminal, false)
		},
		record: s.recordCloseEvent,
		closed: func(ctx context.Context, audit agentCloseAudit, closed session.Session) error {
			return s.recordAuditedRootClosure(ctx, audit, closed, "scheduled")
		},
	})
}

// recordCloseEvent writes one scheduled close's event, and logs it.
func (s *Server) recordCloseEvent(ctx context.Context, kind, terminal string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	log.Printf("session close: %s %s %s", kind, terminal, raw)
	if s.store == nil {
		return
	}
	if err := s.store.Append(ctx, store.Event{Kind: kind, Subject: terminal, Payload: raw}); err != nil {
		log.Printf("session close: %s for %s could not be recorded: %v", kind, terminal, err)
	}
}

// runScheduledCloses is the sweep's beat, for as long as ctx lives.
func (s *Server) runScheduledCloses(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.scheduledCloseSweep(ctx)
		}
	}
}

// scheduledClosesReading is capacity.SessionCloseScheduled.
func (s *Server) scheduledClosesReading() capacity.Reading {
	return capacity.Reading{Known: true, Used: int64(s.closes.count()), Note: "in memory; a restart drops them"}
}
