package app

import (
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// How many sessions one reading of the machine asks an activity time of.
//
// Reading one is a stat of a file the assistant writes anyway, taken inside
// the one producer (inventory_reading.go), so a console redrawing every two
// seconds costs one stat per row every two seconds and the other two loops
// cost nothing. That is cheap — and a cost that grows with the number of rows
// and is bounded by nothing is still the shape that put this daemon's screen
// captures behind a process-wide lock twenty at a time.
//
// So it is bounded, and what happens at the bound is chosen rather than
// inherited: the rows past it are **not read**, and each says `unread`. They
// are not given a stale time, and they are not given a zero — a row ordered by
// a time nobody read would sink to the bottom of the list, which is the
// failure this whole field exists to stop. The order puts an unknown ahead of
// every known time inside its state, so a row this reading could not get to is
// somewhere a reader will see it.

// ActivityReadLimit is that bound: the capacity register's
// `sessions.activity_reads`.
const ActivityReadLimit = 64

// ActivityReads is the bound and what the last reading spent against it.
type ActivityReads struct {
	mu      sync.Mutex
	limit   int64
	last    int64
	refused int64
	lastAt  time.Time
	now     func() time.Time
}

// NewActivityReads is the bound at the register row's default limit.
func NewActivityReads() *ActivityReads {
	return &ActivityReads{limit: ActivityReadLimit, now: time.Now}
}

// SetLimit is the capacity override for `sessions.activity_reads`; zero or
// less keeps the one it has.
func (a *ActivityReads) SetLimit(n int64) {
	if a == nil || n <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.limit = n
}

// budget is how many reads the reading starting now may take.
func (a *ActivityReads) budget() int64 {
	if a == nil {
		return ActivityReadLimit
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limit
}

// spent records what one reading used, and how many rows it left unread.
func (a *ActivityReads) spent(used, refused int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.last = used
	if refused > 0 {
		a.refused += refused
		a.lastAt = a.now()
	}
}

// Reading is the `sessions.activity_reads` row: what the last reading of the
// machine spent, and how many rows this process has left unread since it
// started.
func (a *ActivityReads) Reading() capacity.Reading {
	if a == nil {
		return capacity.Reading{Known: true, Note: "this inventory reads no activity times"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return capacity.Reading{
		Known: true, Used: a.last,
		Counters: capacity.Counters{Refused: a.refused, LastActionAt: a.lastAt},
	}
}
