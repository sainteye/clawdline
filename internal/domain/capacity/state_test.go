package capacity

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)

// fill is one row's adapter in miniature: it holds up to its limit and, on
// the write past it, does what the row says it does at the limit — the same
// thing the real adapter does, so the tracker is tested against each row's
// declared behaviour rather than against a behaviour of the test's choosing.
type fill struct {
	e        Entry
	limit    int64
	used     int64
	counters Counters
}

func (f *fill) write() {
	if f.used < f.limit {
		f.used++
		return
	}
	switch f.e.AtLimit {
	case Rotate:
		f.used = 1
		f.counters.Rotated++
	case EvictOldest:
		if f.e.Class == Buffer {
			f.counters.Dropped++
		} else {
			f.counters.Evicted++
		}
	case Refuse:
		f.counters.Refused++
	case Expire:
		f.counters.Expired++
	case Disconnect:
		// The reader that fell behind is ended; what it had waiting goes
		// with it, and it reads afresh when it comes back.
		f.used = 1
		f.counters.Disconnected++
	case Nothing:
		f.used++
	}
}

func (f *fill) reading() Reading { return Reading{Known: true, Used: f.used, Counters: f.counters} }

func kinds(events []Event) string {
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return strings.Join(out, ",")
}

// limits.md §4.7, layer 1: every row with its limit injected at 20, written to
// the 16th (80%), 19th (95%), 20th (100%) and 21st item.
//
// A row whose own limit is at or below twenty is walked at its own instead
// (theSmallRow below). An override may only lower a limit, so twenty cannot be
// injected into a row that holds two, and injecting it into a row that already
// holds twenty is no override at all — and a row that holds two is a real
// bound, not a missing one: how many terminal captures this daemon may have in
// flight is the bound that keeps a queue from forming in front of a person's
// keystroke, and it is small on purpose.
func TestEachRowGoesOkWarnCriticalFullAndActsAtTheLimit(t *testing.T) {
	for _, e := range Register() {
		if e.Limit <= 20 {
			t.Run(e.Name, func(t *testing.T) { theSmallRow(t, e) })
			continue
		}
		t.Run(e.Name, func(t *testing.T) {
			res, problems := Resolve([]Entry{e}, e.Name+"=20")
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			tr := NewTracker()
			f := &fill{e: e, limit: 20}
			now := t0
			st, _ := tr.Observe(res[0], f.reading(), now)
			if st.State != OK || !st.Overridden {
				t.Fatalf("empty: %+v", st)
			}
			var notices, transitions []string
			for i := 1; i <= 21; i++ {
				f.write()
				now = now.Add(time.Minute)
				var events []Event
				st, events = tr.Observe(res[0], f.reading(), now)
				for _, ev := range events {
					switch ev.Kind {
					case EventState:
						transitions = append(transitions, ev.Payload["from"].(string)+"→"+ev.Payload["to"].(string)+"@"+itoa(int64(i)))
					case EventNotify:
						notices = append(notices, ev.Payload["state"].(string)+"@"+itoa(int64(i)))
					}
				}
				switch i {
				case 15:
					if st.State != OK {
						t.Fatalf("15/20 is %s", st.State)
					}
				case 16, 18:
					if st.State != Warn {
						t.Fatalf("%d/20 is %s", i, st.State)
					}
				case 19:
					if st.State != Critical {
						t.Fatalf("19/20 is %s", st.State)
					}
				case 20:
					if st.State != Full {
						t.Fatalf("20/20 is %s", st.State)
					}
				case 21:
					acted := f.counters.Rotated + f.counters.Evicted + f.counters.Dropped + f.counters.Refused + f.counters.Expired +
						f.counters.Disconnected
					if e.AtLimit != Nothing && acted != 1 {
						t.Fatalf("the 21st write did not %s once: %+v", e.AtLimit, f.counters)
					}
					if e.AtLimit == Nothing && acted != 0 {
						t.Fatalf("a row that does nothing at its limit did something: %+v", f.counters)
					}
					if kind := kinds(events); e.AtLimit != Nothing && !strings.Contains(kind, "capacity.") {
						t.Fatalf("the action at the limit left no event: %q", kind)
					}
				}
			}
			want := "ok→warn@16,warn→critical@19,critical→full@20"
			if e.AtLimit == Rotate || e.AtLimit == Disconnect {
				// The 21st line opened a new segment, or the 21st frame ended
				// the stream that had twenty waiting: back to ok in one step.
				want += ",full→ok@21"
			}
			if got := strings.Join(transitions, ","); got != want {
				t.Errorf("transitions %s, want %s", got, want)
			}
			// One notice for the whole climb: critical, with full and any
			// recovery inside the same day counted as suppressed.
			if got := strings.Join(notices, ","); got != "critical@19" {
				t.Errorf("notices %s, want critical@19", got)
			}
			wantQuiet := int64(1)
			if e.AtLimit == Rotate || e.AtLimit == Disconnect {
				wantQuiet = 2
			}
			if st.Notices != 1 || st.Suppressed != wantQuiet {
				t.Errorf("notices %d suppressed %d", st.Notices, st.Suppressed)
			}
			// Exhausted is the health route's one fact, and only the evidence
			// row has it at full: the security audit made room.
			if st.Exhausted != (e.Class == Evidence) {
				t.Errorf("exhausted = %v at the end", st.Exhausted)
			}
		})
	}
}

// theSmallRow is the same walk for a row that holds fewer than twenty things:
// empty is ok, full is full, and the write past the limit does what the row
// says it does, once, and leaves an event saying so.
//
// The four ratios the states are defined at cannot all be crossed by a row of
// two — 1/2 is fifty per cent, which is ok — so what is checked here is what a
// row of any size must do, and the transitions in between are the larger
// walk's business.
func theSmallRow(t *testing.T, e Entry) {
	t.Helper()
	res := Resolved{Entry: e, Limit: e.Limit}
	tr := NewTracker()
	f := &fill{e: e, limit: e.Limit}
	now := t0
	st, _ := tr.Observe(res, f.reading(), now)
	if st.State != OK {
		t.Fatalf("empty: %+v", st)
	}
	var events []Event
	for i := int64(1); i <= e.Limit; i++ {
		f.write()
		now = now.Add(time.Minute)
		st, events = tr.Observe(res, f.reading(), now)
	}
	if st.State != Full {
		t.Fatalf("%d/%d is %s", e.Limit, e.Limit, st.State)
	}
	f.write()
	now = now.Add(time.Minute)
	st, events = tr.Observe(res, f.reading(), now)
	acted := f.counters.Rotated + f.counters.Evicted + f.counters.Dropped + f.counters.Refused +
		f.counters.Expired + f.counters.Disconnected
	if e.AtLimit != Nothing && acted != 1 {
		t.Fatalf("the write past the limit did not %s once: %+v", e.AtLimit, f.counters)
	}
	if kind := kinds(events); e.AtLimit != Nothing && !strings.Contains(kind, "capacity.") {
		t.Fatalf("the action at the limit left no event: %q", kind)
	}
	if st.Exhausted != (e.Class == Evidence) {
		t.Errorf("exhausted = %v at the end", st.Exhausted)
	}
}

// Moving between 18 and 19 of 20 — across 95% and back — does not announce
// the row again, and neither does dropping to warn.
func TestHysteresisKeepsARowFromRepeatingItself(t *testing.T) {
	e := Entry{Name: "x.y", Class: Idempotency, Unit: Rows, Limit: 20, AtLimit: Refuse}
	res := Resolved{Entry: e, Limit: 20}
	tr := NewTracker()
	now := t0
	notify := 0
	var states []State
	for _, used := range []int64{10, 19, 18, 19, 18, 19, 18, 17, 16, 15, 14} {
		now = now.Add(time.Minute)
		st, events := tr.Observe(res, Reading{Known: true, Used: used}, now)
		states = append(states, st.State)
		for _, ev := range events {
			if ev.Kind == EventNotify {
				notify++
			}
		}
	}
	// 18/20 is 90%: critical holds until below 90%, then warn until below 75%
	// — and 15/20 is 75%, not below it.
	want := []State{OK, Critical, Critical, Critical, Critical, Critical, Critical, Warn, Warn, Warn, OK}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("step %d: %v, want %v", i, states, want)
		}
	}
	if notify != 1 {
		t.Fatalf("%d notices, want the one on entering critical", notify)
	}
}

// A day later a new climb is announced again, and a recovery after a day is
// announced as well.
func TestNoticesAreAtMostOneADayPerRow(t *testing.T) {
	e := Entry{Name: "x.y", Class: Evidence, Unit: Bytes, Limit: 100, AtLimit: Refuse}
	res := Resolved{Entry: e, Limit: 100}
	tr := NewTracker()
	count := func(events []Event) (n int) {
		for _, ev := range events {
			if ev.Kind == EventNotify {
				n++
			}
		}
		return n
	}
	_, ev := tr.Observe(res, Reading{Known: true, Used: 100}, t0)
	if count(ev) != 1 {
		t.Fatalf("a row first seen full: %v", ev)
	}
	_, ev = tr.Observe(res, Reading{Known: true, Used: 10}, t0.Add(time.Hour))
	if count(ev) != 0 {
		t.Fatalf("recovered within the day: %v", ev)
	}
	_, ev = tr.Observe(res, Reading{Known: true, Used: 96}, t0.Add(25*time.Hour))
	if count(ev) != 1 {
		t.Fatalf("critical a day later: %v", ev)
	}
	st, ev := tr.Observe(res, Reading{Known: true, Used: 10}, t0.Add(50*time.Hour))
	if count(ev) != 1 || st.Notices != 3 || st.Suppressed != 1 {
		t.Fatalf("recovered a day after that: %v %+v", ev, st)
	}
}

// Unknown is not ok and not zero: the state says unknown, the reason is kept,
// and a row that comes back is measured from ok, not from what it was.
func TestAnUnmeasuredRowIsUnknown(t *testing.T) {
	e := Register()[1] // store.db
	res := Resolved{Entry: e, Limit: 100}
	tr := NewTracker()
	st, events := tr.Observe(res, Unmeasured("stat: permission denied"), t0)
	if st.State != Unknown || st.Exhausted || st.Reading.Err == "" || kinds(events) != EventState {
		t.Fatalf("%+v %v", st, events)
	}
	st, _ = tr.Observe(res, Reading{Known: true, Used: 50}, t0.Add(time.Minute))
	if st.State != OK {
		t.Fatalf("after it could be read again: %s", st.State)
	}
}

func TestExhaustedIsEvidenceAndAuditOnly(t *testing.T) {
	for _, c := range []struct {
		class  Class
		action Action
		state  State
		fail   bool
		want   bool
	}{
		{Evidence, Refuse, Full, false, true},
		{Evidence, Nothing, Full, false, true},
		{Evidence, Refuse, Critical, false, false},
		{Evidence, Refuse, OK, true, true},
		{SecurityAudit, Rotate, Full, false, false},
		{SecurityAudit, Rotate, Full, true, true},
		{SecurityAudit, Rotate, OK, true, true},
		{Idempotency, Refuse, Full, false, false},
		{Idempotency, EvictOldest, Full, true, false},
		{Buffer, EvictOldest, Full, false, false},
	} {
		got := Exhausted(Entry{Class: c.class, AtLimit: c.action}, c.state, Reading{Known: true, Failing: c.fail})
		if got != c.want {
			t.Errorf("%s %s %s failing=%v: %v", c.class, c.action, c.state, c.fail, got)
		}
	}
}

// Two weeks before a projected row is full it is warn, whatever its ratio;
// with under an hour of samples there is no projection at all.
func TestAProjectionWarnsTwoWeeksAhead(t *testing.T) {
	e := Register()[1] // store.db projects
	res := Resolved{Entry: e, Limit: 1_000_000}
	tr := NewTracker()
	st, _ := tr.Observe(res, Reading{Known: true, Used: 0}, t0)
	st, _ = tr.Observe(res, Reading{Known: true, Used: 5_000}, t0.Add(30*time.Minute))
	if st.HasGrowth || st.State != OK {
		t.Fatalf("half an hour of samples: %+v", st)
	}
	// 10,000 in two hours is 120,000 a day: full in about eight days.
	st, _ = tr.Observe(res, Reading{Known: true, Used: 10_000}, t0.Add(2*time.Hour))
	if !st.HasGrowth || st.GrowthPerDay != 120_000 || st.State != Warn {
		t.Fatalf("two hours: %+v", st)
	}
	if d := st.ProjectedFullAt.Sub(t0.Add(2 * time.Hour)); d < 8*24*time.Hour || d > 8*24*time.Hour+6*time.Hour {
		t.Fatalf("projected %s ahead", d)
	}
	// A row that does not project is never warned by one.
	q := Register()[3] // cloud.relay_queue
	qt := NewTracker()
	qt.Observe(Resolved{Entry: q, Limit: 64}, Reading{Known: true, Used: 0}, t0)
	st, _ = qt.Observe(Resolved{Entry: q, Limit: 64}, Reading{Known: true, Used: 10}, t0.Add(2*time.Hour))
	if st.HasGrowth || st.State != OK {
		t.Fatalf("a queue projected: %+v", st)
	}
}

// A counter is reported by how much it moved, once, and a counter that went
// backwards is a new baseline rather than negative work.
func TestCountersAreReportedOnceABatch(t *testing.T) {
	e := Register()[2] // board.receipts
	res := Resolved{Entry: e, Limit: 10}
	tr := NewTracker()
	_, ev := tr.Observe(res, Reading{Known: true, Used: 10, Counters: Counters{Evicted: 7}}, t0)
	for _, x := range ev {
		if x.Kind == "capacity.evicted" {
			t.Fatalf("history reported as news on the first reading: %v", ev)
		}
	}
	_, ev = tr.Observe(res, Reading{Known: true, Used: 10, Counters: Counters{Evicted: 12}}, t0.Add(time.Minute))
	if len(ev) != 1 || ev[0].Kind != "capacity.evicted" || ev[0].Payload["count"] != int64(5) || ev[0].Payload["total"] != int64(12) {
		t.Fatalf("five evictions: %v", ev)
	}
	_, ev = tr.Observe(res, Reading{Known: true, Used: 10, Counters: Counters{Evicted: 2}}, t0.Add(2*time.Minute))
	if len(ev) != 0 {
		t.Fatalf("a reset counter: %v", ev)
	}
}

func TestABeatIsStalledAfterThreeTicksWithoutAPass(t *testing.T) {
	tick := time.Minute
	for _, c := range []struct {
		started, last time.Time
		now           time.Time
		want          bool
	}{
		{time.Time{}, time.Time{}, t0, false},
		{t0, time.Time{}, t0.Add(3 * time.Minute), false},
		{t0, time.Time{}, t0.Add(3*time.Minute + time.Second), true},
		{t0, t0.Add(10 * time.Minute), t0.Add(12 * time.Minute), false},
		{t0, t0.Add(10 * time.Minute), t0.Add(14 * time.Minute), true},
	} {
		if got := BeatStalled(c.started, c.last, tick, c.now); got != c.want {
			t.Errorf("started %v last %v now %v: %v", c.started, c.last, c.now, got)
		}
	}
}

func TestOverridesOnlyLower(t *testing.T) {
	entries := Register()
	res, problems := Resolve(entries, " audit.security=4KiB, store.db=200KiB ,board.receipts=4,cloud.relay_queue=8")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	for name, want := range map[string]int64{AuditSecurity: 4096, StoreDB: 200 << 10, BoardReceipts: 4, CloudRelayQueue: 8} {
		if got := Limit(res, name); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	for _, spec := range []string{
		"audit.security=16MiB",    // raises
		"store.db=2GiB",           // raises
		"board.receipts=4KiB",     // a size on a row that counts rows
		"cloud.relay_queue=0",     // not positive
		"cloud.relay_queue=-3",    // not a count
		"nothing.here=4",          // no such row
		"audit.security",          // no value
		"audit.security=4 KiB",    // not a size
		"store.db=99999999999GiB", // overflows
	} {
		res, problems := Resolve(entries, spec)
		if len(problems) != 1 {
			t.Errorf("%q: %v", spec, problems)
		}
		for i, r := range res {
			if r.Limit != entries[i].Limit || r.Overridden {
				t.Errorf("%q changed %s to %d", spec, r.Entry.Name, r.Limit)
			}
		}
	}
	res, problems = Resolve(entries, "board.receipts=4,board.receipts=2")
	if len(problems) != 1 || Limit(res, BoardReceipts) != 4 {
		t.Errorf("twice: %v %d", problems, Limit(res, BoardReceipts))
	}
	res, _ = Resolve(entries, "board.receipts=4096")
	if res[2].Overridden {
		t.Errorf("an override equal to the default is not an override")
	}
}
