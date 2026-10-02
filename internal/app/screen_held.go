package app

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The screens the session list reads, held rather than captured under the
// reader.
//
// **A capture is a subprocess, and on iTerm2 it is a subprocess behind a
// process-wide lock.** The list used to take one per qualifying row, inline,
// on whichever reader happened to be building it: twenty rows was twenty
// AppleScripts in a queue one deep, and every reader behind them waited for
// all of them. Worse, a reader that had already reached its deadline went on
// to take the lock anyway and start a subprocess that was expired at birth —
// which is what five hundred and seventy-one `context deadline exceeded` lines
// in one afternoon's daemon log were.
//
// So the list reads what is held and asks for a refresh behind its answer.
// Three bounds make that safe rather than merely faster:
//
//   - **A capture never runs on the reader's clock.** It gets a budget of its
//     own, started when the capture starts, so nothing expired is ever
//     launched and a reader that gave up does not take the capture with it.
//   - **At most ScreenCaptureLimit captures are in flight.** That, and not the
//     number of rows, is how deep the Apple Events queue behind this daemon
//     can get. Past the limit the refresh is simply not started and the reader
//     is given what is held.
//   - **A terminal that failed is left alone for a while.** Backoff doubles
//     from ScreenBackoffFirst to ScreenBackoffMax, so a tab that cannot be
//     read — no automation permission, a tab that is gone — stops costing a
//     subprocess every tick. One success clears it.
//
// This is not the live-screen feature (screens.go). That one is demand-driven,
// leases a pipe and publishes revisions to a watching page; this one exists so
// that building a list of rows costs no terminal at all.

const (
	// ScreenHeldTmux is how long a tmux screen is good for. One capture is
	// 4.15 ms and there is no lock in front of it, so it may be taken at the
	// console's own tick and the row's working line is never more than a tick
	// behind.
	ScreenHeldTmux = 2 * time.Second

	// ScreenHeldOnDemand is the same for a backend that can only be sampled.
	// One round of iTerm2's capture is about 0.16 s — some forty times a tmux
	// capture — and every one of them queues behind the Apple Events lock this
	// process also opens tabs and types with. Eight seconds holds a machine
	// with a dozen iTerm2 tabs at roughly a fifth of one lock holder, where
	// the console's two-second tick would hold it at more than all of it.
	//
	// What is lost is that a row's line and its drawn menu may be up to this
	// old. What is gained is that they exist at all: under the old shape those
	// captures timed out and the row had no line and no menu.
	ScreenHeldOnDemand = 8 * time.Second

	// ScreenCaptureBudget bounds one capture, counted from when the capture
	// begins. The backends have their own, shorter clocks inside it; this is
	// the outer bound that guarantees a slot is given back.
	ScreenCaptureBudget = 12 * time.Second

	// ScreenBackoffFirst is how long a terminal that failed is left alone, and
	// ScreenBackoffMax is as far as the doubling goes.
	ScreenBackoffFirst = 5 * time.Second
	ScreenBackoffMax   = 2 * time.Minute

	// ScreenHeldIdle is how long a screen nobody has asked about is kept. A
	// row that has left the list stops being refreshed the moment it is no
	// longer asked for; this is only how long its last answer lingers.
	ScreenHeldIdle = 2 * time.Minute
)

// ScreenCaptureLimit is how many screen captures this daemon runs at once for
// the session list — the `screens.capture_slots` row of the capacity register.
//
// Two, because on the backend this exists for they are serialised behind one
// lock anyway: a third would not make the round shorter, it would only make
// the queue in front of a person's keystroke longer.
const ScreenCaptureLimit = 2

// ScreenHeldLimit is how many screens are held at once — the register's
// `cache.terminal_screens`. Past it the screen asked about longest ago is let
// go; a miss is one capture, behind the answer, like any other.
const ScreenHeldLimit = 64

// HeldScreens is the session list's reader of screens.
//
// It satisfies ports.ScreenHost, so Inventory reads it exactly as it read the
// live one — but it answers out of what it holds and never waits for a
// terminal.
type HeldScreens struct {
	host ports.ScreenHost
	now  func() time.Time

	mu       sync.Mutex
	held     map[string]*heldScreen
	inflight int
	slots    int
	rows     int
	counts   ScreenCounts
	// One failed iTerm2 Apple Event means the application may be unable to
	// answer any tab. Per-row backoff alone rotates through every open tab.
	itermFailures int
	itermQuiet    time.Time
}

// sourceScreenHost distinguishes a failed Apple Event transport from one
// missing tab. Only the former may quiet every iTerm2 row; a closed tab must
// not hide healthy neighbors.
type sourceScreenHost interface {
	CaptureWithFailure(context.Context, session.Session) (text string, ok bool, sourceFailed bool)
}

type heldScreen struct {
	backend  session.Backend
	text     string
	readable bool
	at       time.Time
	asked    time.Time
	// capturing is a refresh already on its way for this session.
	capturing bool
	// failures is how many captures in a row failed, and quietUntil is when
	// this session may be asked again.
	failures   int
	quietUntil time.Time
}

// ScreenCounts is what this has done to the machine, for /v1/diagnostics.
type ScreenCounts struct {
	// Served is readers answered from a held screen.
	Served int64
	// Missing is readers answered "not read yet" — a session whose first
	// capture has not come back, or one that is in backoff with nothing held.
	Missing int64
	// Captures is how many captures were actually started.
	Captures int64
	// Refused is refreshes not started because every slot was taken.
	Refused int64
	// Backoff is refreshes not started because that terminal is being left
	// alone after a failure.
	Backoff int64
	// Failed is captures that came back with nothing.
	Failed int64
	// Evicted is held screens let go at the limit or after going idle.
	Evicted int64
}

// NewHeldScreens wraps the live reader. A nil host holds nothing and answers
// nothing, which is what a platform with no readable terminal already does.
func NewHeldScreens(host ports.ScreenHost) *HeldScreens {
	return &HeldScreens{
		host:  host,
		now:   time.Now,
		held:  map[string]*heldScreen{},
		slots: ScreenCaptureLimit,
		rows:  ScreenHeldLimit,
	}
}

// SetLimits takes this row's limits from the capacity register, as the other
// bounded caches do. A value of zero or less leaves the default.
func (h *HeldScreens) SetLimits(slots, rows int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if slots > 0 {
		h.slots = int(slots)
	}
	if rows > 0 {
		h.rows = int(rows)
	}
}

// Counts is what this has done so far.
func (h *HeldScreens) Counts() ScreenCounts {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts
}

// Rows is how many screens are held, for the capacity register's reading.
func (h *HeldScreens) Rows() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.held)
}

// Capture answers out of what is held and asks for a refresh behind the
// answer. It never waits for a terminal, and ctx is read for nothing but
// whether the caller is still there.
func (h *HeldScreens) Capture(ctx context.Context, s session.Session) (string, bool) {
	if h == nil || h.host == nil || s.ID == "" {
		return "", false
	}
	now := h.now()
	h.mu.Lock()
	entry := h.held[s.ID]
	if entry == nil {
		entry = &heldScreen{backend: s.Backend}
		h.held[s.ID] = entry
		h.evictLocked(now)
	}
	entry.backend = s.Backend
	previous := entry.asked
	entry.asked = now
	stale := entry.at.IsZero() || now.Sub(entry.at) >= heldFor(s.Backend)
	sourceQuiet := s.Backend == session.BackendITerm && now.Before(h.itermQuiet)
	// The list asks its rows in one stable order, and a slot goes to whoever
	// asks while it is free — so without this the rows at the front took
	// every slot every tick and a row near the end was not captured again for
	// as long as the daemon ran. A free slot is for the row that has waited
	// longest, and this one waits while another is further overdue.
	owed := stale && h.furtherOverdueLocked(now, s.ID, dueAt(entry), previous)
	start := stale && !entry.capturing && !now.Before(entry.quietUntil) && !sourceQuiet &&
		!owed && h.inflight < h.slots
	switch {
	case start:
		entry.capturing = true
		h.inflight++
		h.counts.Captures++
	case stale && entry.capturing:
		// Somebody is already asking this terminal. Nothing to count: the
		// refresh is on its way.
	case stale && (now.Before(entry.quietUntil) || sourceQuiet):
		h.counts.Backoff++
	case stale && owed:
		// This is priority for another row, not a capture-slot refusal.
	case stale:
		h.counts.Refused++
	}
	text, readable := entry.text, entry.readable
	if readable {
		h.counts.Served++
	} else {
		h.counts.Missing++
	}
	h.mu.Unlock()

	if start {
		go h.refresh(s)
	}
	if !readable {
		return "", false
	}
	return text, true
}

// furtherOverdueLocked is whether another row can use the next free slot and
// has waited longer for it than a row due at `due`: never read, or read
// longer ago past its own freshness window.
//
// **On 2026-10-02 a Claude Code session stopped on an AskUserQuestion menu was
// shown with no buttons for as long as the menu stood.** Twenty-six rows
// shared two slots, a dozen iTerm2 tabs held them a capture at a time, and
// the tmux rows at the front of the order took every slot that came free; a
// probe of the live list counted one capture of that pane in thirty passes
// and eleven of the first. Its one capture was from before the menu was
// drawn, so the registry said waiting, the parser was handed a screen with
// no menu on it, and the card said the choices could not be read.
//
// Only rows the list has asked about since `since` — this row's own previous
// ask, which is the list's last pass — are counted, so a row that has left
// the list holds the others back for one pass at most. A failed tab in
// backoff, or an iTerm source outage, must not hold healthy rows behind an
// impossible capture, and a row already captured since it was last asked
// about is not waiting for anything.
func (h *HeldScreens) furtherOverdueLocked(now time.Time, except string, due, since time.Time) bool {
	for id, entry := range h.held {
		if id == except || entry.capturing || entry.asked.IsZero() || entry.asked.Before(since) ||
			(!entry.at.IsZero() && !entry.asked.After(entry.at)) ||
			now.Before(entry.quietUntil) ||
			(entry.backend == session.BackendITerm && now.Before(h.itermQuiet)) {
			continue
		}
		if other := dueAt(entry); other.Before(due) && !now.Before(other) {
			return true
		}
	}
	return false
}

// dueAt is when a held screen stops being good: the zero time for one never
// read, which is due before every screen that has been.
func dueAt(entry *heldScreen) time.Time {
	if entry.at.IsZero() {
		return time.Time{}
	}
	return entry.at.Add(heldFor(entry.backend))
}

// heldFor is how long this backend's screen is good for.
func heldFor(backend session.Backend) time.Duration {
	if backend == session.BackendTmux {
		return ScreenHeldTmux
	}
	return ScreenHeldOnDemand
}

// refresh takes one capture, on a clock of its own.
func (h *HeldScreens) refresh(s session.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), ScreenCaptureBudget)
	var text string
	var ok, sourceFailed bool
	if source, hasFailure := h.host.(sourceScreenHost); hasFailure {
		text, ok, sourceFailed = source.CaptureWithFailure(ctx, s)
	} else {
		text, ok = h.host.Capture(ctx, s)
	}
	cancel()

	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inflight--
	entry := h.held[s.ID]
	if entry == nil {
		// Let go while this was in flight. The slot is what mattered.
		return
	}
	entry.capturing = false
	if !ok {
		h.counts.Failed++
		entry.failures++
		entry.quietUntil = now.Add(backoffFor(entry.failures))
		if s.Backend == session.BackendITerm && sourceFailed {
			h.itermFailures++
			h.itermQuiet = now.Add(backoffFor(h.itermFailures))
		}
		// What was held stays held: a capture that failed says nothing about
		// whether the last one was right, and dropping it would turn one
		// unanswered AppleScript into a row that lost its line.
		return
	}
	entry.failures = 0
	entry.quietUntil = time.Time{}
	if s.Backend == session.BackendITerm {
		h.itermFailures = 0
		h.itermQuiet = time.Time{}
	}
	entry.text = text
	entry.readable = true
	entry.at = now
}

// backoffFor doubles from ScreenBackoffFirst and stops at ScreenBackoffMax.
func backoffFor(failures int) time.Duration {
	wait := ScreenBackoffFirst
	for i := 1; i < failures; i++ {
		wait *= 2
		if wait >= ScreenBackoffMax {
			return ScreenBackoffMax
		}
	}
	return wait
}

// evictLocked lets go of screens nobody has asked about, and then of the
// oldest asks, until the row is inside its limit.
func (h *HeldScreens) evictLocked(now time.Time) {
	for id, entry := range h.held {
		if entry.capturing || entry.asked.IsZero() {
			continue
		}
		if now.Sub(entry.asked) > ScreenHeldIdle {
			delete(h.held, id)
			h.counts.Evicted++
		}
	}
	if len(h.held) <= h.rows {
		return
	}
	ids := make([]string, 0, len(h.held))
	for id, entry := range h.held {
		if entry.capturing {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		return h.held[ids[a]].asked.Before(h.held[ids[b]].asked)
	})
	for _, id := range ids {
		if len(h.held) <= h.rows {
			return
		}
		delete(h.held, id)
		h.counts.Evicted++
	}
}

// Reading is this cache's own account for the capacity register's two rows:
// how many screens are held, and how many captures are in flight.
func (h *HeldScreens) Reading() (held capacity.Reading, slots capacity.Reading) {
	h.mu.Lock()
	defer h.mu.Unlock()
	held = capacity.Reading{
		Known: true, Used: int64(len(h.held)),
		Counters: capacity.Counters{Evicted: h.counts.Evicted},
	}
	slots = capacity.Reading{
		Known: true, Used: int64(h.inflight),
		Note: "captures in flight now; " + itoa(h.counts.Captures) + " taken, " + itoa(h.counts.Failed) + " with nothing back",
		Counters: capacity.Counters{
			// A refresh not started because every slot was taken is this row
			// refusing, which is what it says it does at its limit.
			Refused: h.counts.Refused,
			// One left alone after a failure is not the limit acting; it is
			// counted apart so the two are never read as one.
			Dropped: h.counts.Backoff,
		},
	}
	return held, slots
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

var _ ports.ScreenHost = (*HeldScreens)(nil)
