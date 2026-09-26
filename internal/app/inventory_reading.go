package app

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
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

// LastGoodInventoryAgeLimit is how long a drawing-only reader may be shown the
// last complete answer from a source that did not answer this pass.
//
// Two minutes spans twelve full iTerm2 listing timeouts, so one busy Apple
// Events queue does not turn every row into "unknown". It is also the longest
// this daemon already leaves a failed sampled screen in backoff
// (ScreenBackoffMax). Past it, continuing to describe a session as working or
// waiting would turn an observation into a claim about the present, so the
// retained answer expires and the source becomes genuinely unknown.
const LastGoodInventoryAgeLimit = 2 * time.Minute

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
	// waiting on finished. A Recent reader is then given the held reading only
	// while it remains inside the retention bound; a Fresh one is always given
	// an unread reading. Neither answer turns old state into current evidence.
	Late int64
}

// InventoryReading is the single producer in front of an Inventory.
type InventoryReading struct {
	scan func(context.Context) session.Inventory
	ttl  time.Duration
	now  func() time.Time
	// retainFor is independently configurable from ttl: ttl avoids duplicate
	// scans measured in seconds; this is the honesty line for a last good row.
	retainFor time.Duration

	mu    sync.Mutex
	held  session.Inventory
	holds bool
	good  map[string]sourceReading
	// forgotten is a terminal backend's positive answer that a session was
	// closed. It prevents an older scan already in flight, or the last-good
	// shelf during a source outage, from putting that session back on screen.
	// Entries live inside the same retainFor window as the shelf and leave
	// sooner when a later complete source reading confirms the absence.
	forgotten map[string]forgottenSession
	flight    *inventoryFlight
	counts    InventoryCounts
	expired   int64
	// observers are shown every scan's raw reading after its readers have
	// been answered (Observe).
	observers []func(context.Context, session.Inventory)
}

// sourceReading is the last complete answer from one terminal source. It is
// deliberately per source: an iTerm2 Apple Event timeout must not age a tmux
// row that tmux read successfully in the same pass.
type sourceReading struct {
	at   time.Time
	rows []session.Session
}

type forgottenSession struct {
	at  time.Time
	row session.Session
}

// inventoryFlight is one scan several readers are waiting on.
type inventoryFlight struct {
	done    chan struct{}
	raw     session.Inventory
	display session.Inventory
}

// NewInventoryReading wraps a reader. ttl of zero is InventoryTTL.
func NewInventoryReading(scan func(context.Context) session.Inventory, ttl time.Duration) *InventoryReading {
	if ttl <= 0 {
		ttl = InventoryTTL
	}
	return &InventoryReading{
		scan: scan, ttl: ttl, now: time.Now,
		retainFor: LastGoodInventoryAgeLimit,
		good:      map[string]sourceReading{}, forgotten: map[string]forgottenSession{},
	}
}

// Forget records the terminal backend's positive answer that row was closed.
//
// This is not a metadata tombstone and it does not guess from a missing row:
// Actions.Close calls it only after TerminalHost.Close returned success. The
// terminal/tab or tmux pane is the existence authority; this only stops an
// observation taken before that answer from being drawn afterwards.
func (r *InventoryReading) Forget(row session.Session) {
	if row.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgotten[row.ID] = forgottenSession{at: r.now(), row: row}
	if r.holds {
		r.held.Sessions = withoutSession(r.held.Sessions, row)
	}
	for source, good := range r.good {
		good.rows = withoutSession(good.rows, row)
		r.good[source] = good
	}
}

// Observe adds fn to what every scan's raw reading is shown, once the readers
// waiting on that scan have their answer.
//
// It is how something that must follow the machine whether or not anybody is
// looking — the record of which sessions were open in this boot
// (SessionRestore) — rides on the scans the broker's beat already takes every
// few seconds, rather than taking scans of its own. fn runs on the scan's own
// goroutine, so it holds up the next scan and nobody's answer.
func (r *InventoryReading) Observe(fn func(context.Context, session.Inventory)) {
	r.mu.Lock()
	r.observers = append(r.observers, fn)
	r.mu.Unlock()
}

// SetRetentionAge applies the capacity register's seconds limit. A nonpositive
// value keeps the shipped bound.
func (r *InventoryReading) SetRetentionAge(seconds int64) {
	if seconds <= 0 {
		return
	}
	r.mu.Lock()
	r.retainFor = time.Duration(seconds) * time.Second
	r.mu.Unlock()
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
		if fresh {
			return flight.raw
		}
		return flight.display
	case <-ctx.Done():
		r.mu.Lock()
		r.counts.Late++
		held, holds, now, retainFor := r.held, r.holds, r.now(), r.retainFor
		r.mu.Unlock()
		if fresh || !holds || held.Observation.ObservedAt.IsZero() ||
			now.Sub(held.Observation.ObservedAt) >= retainFor {
			return unreadInventory()
		}
		return held
	}
}

// run takes the reading and hands it to everybody waiting.
func (r *InventoryReading) run(ctx context.Context, flight *inventoryFlight) {
	scan, cancel := context.WithTimeout(context.WithoutCancel(ctx), InventoryScanBudget)
	defer cancel()
	raw := r.scan(scan)
	r.mu.Lock()
	observed := raw.ObservedAt
	if observed.IsZero() {
		observed = r.now()
	}
	raw = r.withoutForgottenLocked(raw, observed, r.now())
	display := r.retainLocked(raw)
	r.held = display
	r.holds = true
	r.flight = nil
	observers := r.observers
	r.mu.Unlock()
	flight.raw = raw
	flight.display = display
	close(flight.done)
	for _, fn := range observers {
		fn(scan, raw)
	}
}

// retainLocked turns one failed source reading into an earlier, named reading
// instead of six fresh unknown placeholders. It does not change raw, which is
// what Fresh returns to callers that decide or act.
func (r *InventoryReading) retainLocked(raw session.Inventory) session.Inventory {
	now := r.now()
	observed := raw.ObservedAt
	if observed.IsZero() {
		observed = now
	}
	display := raw
	display.Sessions = append([]session.Session(nil), raw.Sessions...)
	display.Observation = session.Observation{
		ObservedAt: observed, Provenance: raw.Provenance, Freshness: session.FreshnessCurrent,
	}
	for n := range display.Sessions {
		source := session.SourceFor(display.Sessions[n].Backend)
		freshness := session.FreshnessCurrent
		complete, known := raw.Sources[source]
		if (source == "" && !raw.Complete) || (source != "" && (!known || !complete)) {
			freshness = session.FreshnessMissing
		}
		display.Sessions[n].Observation = session.Observation{
			ObservedAt: observed, Provenance: source, Freshness: freshness,
		}
	}

	missing := !raw.Complete && len(raw.Sources) == 0
	retained := false
	oldest := observed
	for source, complete := range raw.Sources {
		if complete {
			rows := rowsFromSource(display.Sessions, source)
			for n := range rows {
				rows[n].Observation = session.Observation{
					ObservedAt: observed, Provenance: source, Freshness: session.FreshnessCurrent,
				}
			}
			r.good[source] = sourceReading{at: observed, rows: rows}
			continue
		}

		// The placeholders from an incomplete source are the attempted pass,
		// not its last answer. Remove them before considering the shelf.
		attempted := rowsFromSource(display.Sessions, source)
		display.Sessions = withoutSource(display.Sessions, source)
		good, ok := r.good[source]
		age := now.Sub(good.at)
		if !ok || age < 0 || age >= r.retainFor {
			if ok {
				delete(r.good, source)
				r.expired++
			}
			for _, row := range attempted {
				row.Observation = session.Observation{
					Provenance: source, Freshness: session.FreshnessMissing,
				}
				display.Sessions = append(display.Sessions, row)
			}
			missing = true
			continue
		}
		for _, row := range good.rows {
			row.Observation = session.Observation{
				ObservedAt: good.at, Provenance: source, Freshness: session.FreshnessUnverified,
			}
			display.Sessions = append(display.Sessions, row)
		}
		retained = true
		if oldest.IsZero() || good.at.Before(oldest) {
			oldest = good.at
		}
	}
	sort.SliceStable(display.Sessions, func(i, j int) bool {
		return inventoryRowKey(display.Sessions[i]) < inventoryRowKey(display.Sessions[j])
	})
	switch {
	case missing:
		display.Observation = session.Observation{
			Provenance: raw.Provenance, Freshness: session.FreshnessMissing,
		}
	case retained:
		display.Observation = session.Observation{
			ObservedAt: oldest, Provenance: raw.Provenance, Freshness: session.FreshnessUnverified,
		}
	}
	return display
}

// withoutForgottenLocked applies a fact newer than an in-flight reading: a
// successful terminal close. A later complete reading owns the next answer —
// absence confirms the close, while presence says the terminal exists again.
// An unanswered source says neither, so the close fact remains for the same
// bounded window as the retained readings it outranks.
func (r *InventoryReading) withoutForgottenLocked(raw session.Inventory, observed, now time.Time) session.Inventory {
	for id, gone := range r.forgotten {
		if now.Sub(gone.at) >= r.retainFor {
			delete(r.forgotten, id)
			continue
		}
		later := observed.After(gone.at)
		proves, _ := raw.ProvesAbsence(session.SourceFor(gone.row.Backend))
		present := hasSession(raw.Sessions, gone.row)
		if later && proves {
			// This source has now answered for itself. Whether it confirmed the
			// absence or showed the same terminal id again, its newer complete
			// enumeration is the absolute truth.
			delete(r.forgotten, id)
			if present {
				continue
			}
		}
		raw.Sessions = withoutSession(raw.Sessions, gone.row)
	}
	return raw
}

func hasSession(rows []session.Session, wanted session.Session) bool {
	for _, row := range rows {
		if sameTerminalSession(row, wanted) {
			return true
		}
	}
	return false
}

func withoutSession(rows []session.Session, wanted session.Session) []session.Session {
	out := rows[:0]
	for _, row := range rows {
		if !sameTerminalSession(row, wanted) {
			out = append(out, row)
		}
	}
	return out
}

func sameTerminalSession(a, b session.Session) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	return a.TTY != "" && b.TTY != "" && a.TTY == b.TTY
}

func rowsFromSource(rows []session.Session, source string) []session.Session {
	out := make([]session.Session, 0, len(rows))
	for _, row := range rows {
		if session.SourceFor(row.Backend) == source {
			out = append(out, row)
		}
	}
	return out
}

func withoutSource(rows []session.Session, source string) []session.Session {
	out := rows[:0]
	for _, row := range rows {
		if session.SourceFor(row.Backend) != source {
			out = append(out, row)
		}
	}
	return out
}

func inventoryRowKey(row session.Session) string {
	if row.TTY != "" {
		return row.TTY
	}
	return row.ID
}

// RetentionReading is the capacity row for the one time-bounded shelf. Used
// is the oldest retained source's age; expiry is counted rather than hidden.
func (r *InventoryReading) RetentionReading() capacity.Reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	reading := capacity.Reading{
		Known: true, WindowSeconds: int64(r.retainFor / time.Second),
		Counters: capacity.Counters{Expired: r.expired},
	}
	if len(r.good) == 0 {
		reading.Note = "no complete terminal-source reading is held"
		return reading
	}
	now := r.now()
	oldest := now
	for _, good := range r.good {
		if good.at.Before(oldest) {
			oldest = good.at
		}
	}
	age := now.Sub(oldest)
	if age > 0 {
		reading.Used = int64(age / time.Second)
	}
	reading.OldestAt = oldest
	return reading
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
		Provenance:  "unread",
		Complete:    false,
		Sources:     map[string]bool{},
		Notes:       []string{"this reading was not taken: the caller's own clock ran out before the scan finished"},
		Observation: session.Observation{Provenance: "unread", Freshness: session.FreshnessMissing},
	}
}
