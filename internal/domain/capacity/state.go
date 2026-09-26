package capacity

import (
	"sync"
	"time"
)

// State is where a row stands against its limit (docs/limits.md §4.6).
type State string

const (
	OK       State = "ok"
	Warn     State = "warn"
	Critical State = "critical"
	Full     State = "full"
	// Unknown is a row that could not be measured. It is not OK: a reading
	// that failed and a reading of zero are different answers (DG-7).
	Unknown State = "unknown"
)

const (
	// criticalAt and fullAt are the two upper thresholds; warn is per row.
	criticalAt = 0.95
	fullAt     = 1.0
	// hysteresis is how far below a threshold a row must fall before it goes
	// back down past it, so a row sitting on 95% does not announce itself on
	// every pass.
	hysteresis = 0.05
	// NoticeEvery is the most often one row may produce a notice.
	NoticeEvery = 24 * time.Hour
	// A projection is taken over the last week of hourly samples, needs at
	// least an hour of them, and turns a row to warn two weeks before it would
	// be full.
	sampleEvery = time.Hour
	sampleSpan  = 7 * 24 * time.Hour
	minSpan     = time.Hour
	warnAhead   = 14 * 24 * time.Hour
	// StallTicks is how many ticks without a finished pass make a beat
	// stalled (DG-1: three).
	StallTicks = 3
)

func rank(s State) int {
	switch s {
	case Warn:
		return 1
	case Critical:
		return 2
	case Full:
		return 3
	}
	return 0
}

func threshold(e Entry, s State) float64 {
	switch s {
	case Warn:
		return e.Warn()
	case Critical:
		return criticalAt
	case Full:
		return fullAt
	}
	return 0
}

func below(s State) State {
	switch s {
	case Full:
		return Critical
	case Critical:
		return Warn
	}
	return OK
}

// atLeast is ratio ≥ t, with the thresholds' own rounding forgiven: 0.8−0.05
// is not exactly 0.75 in floating point, and 15 of 20 must compare equal to it.
func atLeast(ratio, t float64) bool { return ratio >= t-1e-12 }

func rising(e Entry, ratio float64) State {
	switch {
	case atLeast(ratio, fullAt):
		return Full
	case atLeast(ratio, criticalAt):
		return Critical
	case atLeast(ratio, e.Warn()):
		return Warn
	}
	return OK
}

// Level is the state a row is in at ratio, given the state it was in. Going
// up is immediate; going down past a threshold needs the ratio to be
// `hysteresis` under it. A row that was unknown, or never read, starts from
// ok.
func Level(e Entry, prev State, ratio float64) State {
	up := rising(e, ratio)
	if rank(up) >= rank(prev) {
		return up
	}
	s := prev
	for rank(s) > rank(up) && !atLeast(ratio, threshold(e, s)-hysteresis) {
		s = below(s)
	}
	return s
}

// Counters are what a row's adapter has done at the limit. They are the
// adapter's own count, since this process started unless the reading says
// they are durable.
type Counters struct {
	Refused     int64
	Evicted     int64
	Expired     int64
	Rotated     int64
	Dropped     int64
	WriteErrors int64
	// Coalesced is newer values that replaced a waiting one, on a buffer
	// whose readers want only the latest: nothing was lost, and a reader was
	// told once where it would have been told twice.
	Coalesced int64
	// Disconnected is readers a buffer ended because they were not keeping
	// up, each to come back and read afresh.
	Disconnected int64
	// LastActionAt is when the adapter last refused, evicted, rotated or
	// dropped something. Zero is never, or not recorded.
	LastActionAt time.Time
}

// Reading is one measurement of one row. A measurement must be cheap: a stat,
// an indexed count, a length — never a whole file read for its line count.
type Reading struct {
	// Known is false when the row could not be measured, and Err says why.
	Known bool
	Used  int64
	Err   string
	// Note is anything a reader should know about a known reading.
	Note string
	// OldestAt is the oldest thing the row holds, when the adapter knows it.
	OldestAt time.Time
	// DiskFree is the free space where the row lives, when HasDiskFree.
	DiskFree    int64
	HasDiskFree bool
	// WindowSeconds is a time window the row keeps things for, when it has one.
	WindowSeconds int64
	Counters      Counters
	// DurableCounters says the counters survive a restart because the adapter
	// stores them, rather than counting since this process began.
	DurableCounters bool
	// Failing says the last write to this row, or its last at-limit action,
	// failed. For an evidence or security-audit row that is an alarm whatever
	// the ratio: the thing that must not be lost could not be written.
	Failing bool
}

// Unmeasured is the reading of a row that could not be measured.
func Unmeasured(why string) Reading { return Reading{Err: why} }

// Resolved is a register row with the limit this process runs it at.
type Resolved struct {
	Entry      Entry
	Limit      int64
	Overridden bool
}

// Status is what one reading of one row means.
type Status struct {
	Resolved
	Reading    Reading
	MeasuredAt time.Time
	State      State
	// Ratio is Used over Limit; meaningful only when the reading is known.
	Ratio float64
	// Exhausted is the one fact the open health route turns on: an evidence
	// or security-audit row that is full with nothing making room, or whose
	// last write failed.
	Exhausted bool
	// GrowthPerDay is set when HasGrowth: there is at least an hour of this
	// process's samples to take it from.
	GrowthPerDay float64
	HasGrowth    bool
	// ProjectedFullAt is when the row reaches its limit at GrowthPerDay; zero
	// when it is not growing or is not projected.
	ProjectedFullAt time.Time
	Notices         int64
	Suppressed      int64
	LastNoticeAt    time.Time
}

// Exhausted reports whether a row, in state s with reading r, is the kind of
// full that turns /v1/health red (D27, limits §4.5): evidence or a security
// audit, at its limit with an at-limit behaviour that does not make room — or
// failing to write at all. A rotating audit at its segment size is not
// exhausted: the next line opens a new segment. The other classes never are;
// they are seen in diagnostics and heard as notices.
func Exhausted(e Entry, s State, r Reading) bool {
	if e.Class != Evidence && e.Class != SecurityAudit {
		return false
	}
	if r.Failing {
		return true
	}
	return s == Full && e.AtLimit != Rotate
}

// Event is something the tracker saw that belongs in the store's event log.
// One per transition or per batch, never one per item (limits §4.5).
type Event struct {
	Kind    string
	Subject string
	Payload map[string]any
}

// The event kinds.
const (
	EventState  = "capacity.state"
	EventNotify = "capacity.notify"
)

type sample struct {
	at   time.Time
	used int64
}

type track struct {
	seen     bool
	state    State
	alerted  bool
	samples  []sample
	counters Counters
	notices  int64
	quiet    int64
	noticeAt time.Time
}

// Tracker remembers, per row, what the last reading meant: its state, the
// samples a projection is taken from, the counters already reported and when
// it last produced a notice. It does no I/O; the caller records the events it
// answers.
type Tracker struct {
	mu   sync.Mutex
	rows map[string]*track
}

// NewTracker is an empty tracker.
func NewTracker() *Tracker { return &Tracker{rows: map[string]*track{}} }

// Observe folds one reading into the row's history and answers what it means
// and what it caused: a state event per transition, a notice on entering
// critical or full or on recovering unless the row recovers quietly (at most
// one per row per NoticeEvery; the
// rest are counted as suppressed), and one event per counter that moved since
// the last reading.
func (t *Tracker) Observe(res Resolved, r Reading, now time.Time) (Status, []Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := res.Entry
	tr := t.rows[e.Name]
	if tr == nil {
		tr = &track{}
		t.rows[e.Name] = tr
	}
	st := Status{Resolved: res, Reading: r, MeasuredAt: now}

	prev := tr.state
	if !tr.seen || prev == Unknown {
		prev = OK
	}
	next := Unknown
	if r.Known && res.Limit > 0 {
		st.Ratio = float64(r.Used) / float64(res.Limit)
		next = Level(e, prev, st.Ratio)
		if e.Projects {
			tr.samples = keep(tr.samples, sample{at: now, used: r.Used}, now)
			if perDay, full, ok := project(tr.samples, r.Used, res.Limit, now); ok {
				st.HasGrowth, st.GrowthPerDay, st.ProjectedFullAt = true, perDay, full
				if !full.IsZero() && full.Sub(now) < warnAhead && rank(next) < rank(Warn) {
					next = Warn
				}
			}
		}
	}
	st.State = next
	st.Exhausted = Exhausted(e, next, r)

	var events []Event
	was := tr.state
	if !tr.seen {
		was = ""
	}
	if next != was && (tr.seen || next != OK) {
		events = append(events, Event{Kind: EventState, Subject: e.Name, Payload: map[string]any{
			"name": e.Name, "from": string(was), "to": string(next),
			"used": r.Used, "limit": res.Limit,
		}})
	}

	notice := false
	switch {
	case next != Unknown && rank(next) > rank(prev) && (next == Critical || next == Full):
		notice = true
		tr.alerted = true
	case next == OK && tr.alerted:
		notice = !e.QuietRecovery
		tr.alerted = false
	}
	if notice {
		if tr.noticeAt.IsZero() || now.Sub(tr.noticeAt) >= NoticeEvery {
			tr.notices++
			tr.noticeAt = now
			payload := map[string]any{
				"name": e.Name, "state": string(next), "from": string(was),
				"used": r.Used, "limit": res.Limit, "ratio": st.Ratio,
			}
			if !st.ProjectedFullAt.IsZero() {
				payload["projected_full_at"] = st.ProjectedFullAt.Unix()
			}
			events = append(events, Event{Kind: EventNotify, Subject: e.Name, Payload: payload})
		} else {
			tr.quiet++
		}
	}

	if tr.seen {
		events = append(events, moved(e.Name, tr.counters, r.Counters)...)
	}
	tr.counters = r.Counters
	tr.seen = true
	tr.state = next

	st.Notices, st.Suppressed, st.LastNoticeAt = tr.notices, tr.quiet, tr.noticeAt
	return st, events
}

// moved is one event per counter that went up between two readings. A counter
// that went down was reset under us (a replaced file); it is taken as the new
// baseline and not reported as negative work.
func moved(name string, before, after Counters) []Event {
	var out []Event
	for _, c := range []struct {
		kind       string
		was, total int64
	}{
		{"capacity.refused", before.Refused, after.Refused},
		{"capacity.evicted", before.Evicted, after.Evicted},
		{"capacity.expired", before.Expired, after.Expired},
		{"capacity.rotated", before.Rotated, after.Rotated},
		{"capacity.dropped", before.Dropped, after.Dropped},
		{"capacity.write_errors", before.WriteErrors, after.WriteErrors},
		{"capacity.coalesced", before.Coalesced, after.Coalesced},
		{"capacity.disconnected", before.Disconnected, after.Disconnected},
	} {
		if c.total > c.was {
			out = append(out, Event{Kind: c.kind, Subject: name, Payload: map[string]any{
				"name": name, "count": c.total - c.was, "total": c.total,
			}})
		}
	}
	return out
}

// keep adds s when the newest sample is an hour old, and forgets samples older
// than the projection's span. The list is at most a week of hours.
func keep(samples []sample, s sample, now time.Time) []sample {
	if n := len(samples); n == 0 || s.at.Sub(samples[n-1].at) >= sampleEvery {
		samples = append(samples, s)
	}
	cut := 0
	for cut < len(samples)-1 && now.Sub(samples[cut].at) > sampleSpan {
		cut++
	}
	return samples[cut:]
}

// project is growth per day from the oldest kept sample to now, and when the
// row reaches limit at that rate. ok is false with less than minSpan of
// samples; a row that is not growing has a rate and no date.
func project(samples []sample, used, limit int64, now time.Time) (perDay float64, full time.Time, ok bool) {
	if len(samples) == 0 {
		return 0, time.Time{}, false
	}
	first := samples[0]
	span := now.Sub(first.at)
	if span < minSpan {
		return 0, time.Time{}, false
	}
	perDay = float64(used-first.used) * 86400 / span.Seconds()
	if perDay <= 0 {
		return perDay, time.Time{}, true
	}
	if used >= limit {
		return perDay, now, true
	}
	days := float64(limit-used) / perDay
	return perDay, now.Add(time.Duration(days * float64(24*time.Hour))), true
}

// BeatStalled reports whether a beat that started at started and last finished
// a pass at last, on a tick of tick, has stopped: no pass in StallTicks ticks.
// A beat that never finished its first pass is measured from its start. A beat
// that was never started is not stalled; it is absent, and says so elsewhere.
func BeatStalled(started, last time.Time, tick time.Duration, now time.Time) bool {
	ref := last
	if ref.IsZero() {
		ref = started
	}
	if ref.IsZero() || tick <= 0 {
		return false
	}
	return now.Sub(ref) > StallTicks*tick
}
