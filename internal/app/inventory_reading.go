package app

import (
	"context"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// One reading of this machine, for everybody who asks at about the same time.
//
// Three loops each rebuilt the whole reading on a clock of its own — the event
// stream every two seconds per open page, the Cloud publisher every five, the
// broker's beat every five — and each rebuild scans the process table, asks
// tmux, asks iTerm2 and then captures a screen per qualifying row. Nothing
// shared anything, so the cost was the number of loops times the number of
// rows, and a machine with twenty-five iTerm2 tabs spent more of every second
// scanning itself than not.
//
// **The consumers do not all want the same freshness, and that is the whole of
// this file's design.** A console redrawing every two seconds is content with a
// reading taken a moment ago; the broker deciding whether a child's tab is
// still there is not, because that decision closes sessions and settles tasks,
// and a snapshot taken before the tab opened would be evidence of something
// that was never observed (docs/design-decisions.md D05 ③ — an expired
// snapshot may not stand in for evidence). So there are two words for it, every
// caller says which it needs, and Counts records how each one was answered.
//
//   - Recent may be answered from the held reading while it is younger than
//     the TTL.
//   - Fresh is never answered from a completed reading. It joins a scan that is
//     still running, or starts one; either way what comes back was taken for
//     this call.
//
// Both are singleflight: readers arriving during a scan share it rather than
// starting another.

// InventoryTTL is how old the held reading may be before a Recent reader is
// given a new one.
//
// Two seconds because that is the fastest consumer's own clock — the event
// stream's tick (transport/http streamTick) — so the slower two ride along on
// a reading that was going to be taken anyway, and a page never shows anything
// older than the tick it asked on. It is far inside the forty-five seconds past
// which a row's closeability calls the reading stale, which is the guard that
// stops an old snapshot from authorising a close.
const InventoryTTL = 2 * time.Second

// InventoryScanBudget bounds one scan of the machine.
//
// It is the scan's own clock and not the caller's on purpose: a scan started
// for a reader that has since given up still finishes and still fills the held
// reading, so the next reader is answered at once instead of starting the same
// scan again.
const InventoryScanBudget = 30 * time.Second

// InventoryCounts is how the readers were answered, for /v1/diagnostics. It is
// the evidence for "one producer": Scans is how often this daemon actually
// looked at the machine, and the other three are the readers that cost nothing.
type InventoryCounts struct {
	// Scans is how many readings were taken from the machine.
	Scans int64
	// Held is readers answered from the held reading.
	Held int64
	// Joined is readers that shared a scan already running.
	Joined int64
	// Late is readers whose own clock ran out before the scan they were
	// waiting on finished. A Recent reader is then given the held reading,
	// however old; a Fresh one is given an unread reading, which is not an
	// empty machine and says so.
	Late int64
}

// InventoryReading is the single producer in front of an Inventory.
type InventoryReading struct {
	scan func(context.Context) session.Inventory
	ttl  time.Duration
	now  func() time.Time

	mu     sync.Mutex
	held   session.Inventory
	holds  bool
	flight *inventoryFlight
	counts InventoryCounts
}

// inventoryFlight is one scan several readers are waiting on.
type inventoryFlight struct {
	done chan struct{}
	inv  session.Inventory
}

// NewInventoryReading wraps a reader. ttl of zero is InventoryTTL.
func NewInventoryReading(scan func(context.Context) session.Inventory, ttl time.Duration) *InventoryReading {
	if ttl <= 0 {
		ttl = InventoryTTL
	}
	return &InventoryReading{scan: scan, ttl: ttl, now: time.Now}
}

// Recent is a reading no older than the TTL: the console, the event stream and
// the Cloud publisher, all of which redraw on a clock of their own and none of
// which decides anything irreversible from it.
func (r *InventoryReading) Recent(ctx context.Context) session.Inventory {
	r.mu.Lock()
	if r.holds && r.now().Sub(r.held.ObservedAt) < r.ttl {
		inv := r.held
		r.counts.Held++
		r.mu.Unlock()
		return inv
	}
	r.mu.Unlock()
	return r.take(ctx, false)
}

// Within is Recent for a reader whose own clock is slower than the TTL: it is
// answered from the held reading while that is younger than age. The waiting
// watcher asks this way — its rule is measured in minutes, so a reading the
// broker's beat took a few seconds ago is as good as a new one, and taking a
// new one for it would be a scan nobody else asked for.
func (r *InventoryReading) Within(ctx context.Context, age time.Duration) session.Inventory {
	r.mu.Lock()
	if r.holds && r.now().Sub(r.held.ObservedAt) < max(age, r.ttl) {
		inv := r.held
		r.counts.Held++
		r.mu.Unlock()
		return inv
	}
	r.mu.Unlock()
	return r.take(ctx, false)
}

// Fresh is a reading taken for this call: the broker's beat, which decides from
// it whether a child's tab is still there.
//
// It shares a scan that is already running, because such a scan is still being
// taken now and its answer is the one this caller would have got by scanning
// itself. It never accepts a reading that had already finished.
func (r *InventoryReading) Fresh(ctx context.Context) session.Inventory {
	return r.take(ctx, true)
}

// Counts is how the readers have been answered so far.
func (r *InventoryReading) Counts() InventoryCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

// Held is the last completed reading and whether there is one. For a caller
// that wants to say how old what it drew was without causing a scan.
func (r *InventoryReading) Held() (session.Inventory, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held, r.holds
}

func (r *InventoryReading) take(ctx context.Context, fresh bool) session.Inventory {
	r.mu.Lock()
	flight := r.flight
	if flight != nil {
		r.counts.Joined++
	} else {
		flight = &inventoryFlight{done: make(chan struct{})}
		r.flight = flight
		r.counts.Scans++
		// The scan runs on a goroutine with a clock of its own. Were it run on
		// the leader's, a leader that hung up would take every reader waiting
		// behind it down too, and the reading it had nearly finished would be
		// thrown away.
		go r.run(ctx, flight)
	}
	r.mu.Unlock()

	select {
	case <-flight.done:
		return flight.inv
	case <-ctx.Done():
		r.mu.Lock()
		r.counts.Late++
		held, holds := r.held, r.holds
		r.mu.Unlock()
		if fresh || !holds {
			return unreadInventory()
		}
		return held
	}
}

// run takes the reading and hands it to everybody waiting.
func (r *InventoryReading) run(ctx context.Context, flight *inventoryFlight) {
	scan, cancel := context.WithTimeout(context.WithoutCancel(ctx), InventoryScanBudget)
	defer cancel()
	inv := r.scan(scan)
	r.mu.Lock()
	r.held = inv
	r.holds = true
	r.flight = nil
	r.mu.Unlock()
	flight.inv = inv
	close(flight.done)
}

// unreadInventory is the answer when the caller's clock ran out before any
// reading was taken for it.
//
// It is not an empty machine and must never be read as one: Complete is false,
// so nothing downstream treats it as authoritative, `EmptyAuthoritative` is
// false, and the note says which it is. Handing back an older reading instead
// would be the defect this whole file is about — an expired snapshot standing
// in for evidence.
func unreadInventory() session.Inventory {
	return session.Inventory{
		Provenance: "unread",
		Complete:   false,
		Sources:    map[string]bool{},
		Notes:      []string{"this reading was not taken: the caller's own clock ran out before the scan finished"},
	}
}
