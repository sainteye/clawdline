package schedule

import (
	"testing"
	"time"
)

// This file is the first test in this repository, and it is here rather than
// anywhere else because this is where the code has actually hurt: twice, a
// schedule opened a real assistant session that nobody asked for. Both times
// the fire was the first one.
//
// Everything below states a moment and asks one question of it, so a failure
// names the rule it broke rather than the line it stopped on.

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func hourly(firstSeen, lastRun time.Time) Schedule {
	return Schedule{
		Enabled:   true,
		When:      When{Every: time.Hour},
		FirstSeen: firstSeen,
		LastRun:   lastRun,
	}
}

func TestIntervalNeverFiresOnSight(t *testing.T) {
	now := at("2026-09-16T12:00:00Z")

	cases := []struct {
		name string
		sc   Schedule
		want bool
	}{
		{
			// The incident. A row arrives in the store with no last run, the
			// clock ticks, and an hour-long interval fires immediately.
			name: "seen this instant, never run",
			sc:   hourly(now, time.Time{}),
			want: false,
		},
		{
			// A row nobody has claimed to have seen cannot be fired at all.
			// The read path stamps every row, so reaching here means the row
			// was not finished loading.
			name: "never seen at all",
			sc:   hourly(time.Time{}, time.Time{}),
			want: false,
		},
		{
			name: "seen 59 minutes ago, never run",
			sc:   hourly(now.Add(-59*time.Minute), time.Time{}),
			want: false,
		},
		{
			name: "seen an hour ago, never run",
			sc:   hourly(now.Add(-time.Hour), time.Time{}),
			want: true,
		},
		{
			// A long-standing schedule that ran recently is not due, even
			// though it was first seen days ago: the later of the two wins.
			name: "seen days ago, ran ten minutes ago",
			sc:   hourly(now.Add(-72*time.Hour), now.Add(-10*time.Minute)),
			want: false,
		},
		{
			name: "seen days ago, ran two hours ago",
			sc:   hourly(now.Add(-72*time.Hour), now.Add(-2*time.Hour)),
			want: true,
		},
		{
			name: "disabled, however overdue",
			sc: func() Schedule {
				s := hourly(now.Add(-72*time.Hour), time.Time{})
				s.Enabled = false
				return s
			}(),
			want: false,
		},
	}

	for _, c := range cases {
		if got := c.sc.Due(now, false); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}

// A schedule does not come due while its own last run is still working. Firing
// on a clock while the previous turn is unfinished is how a queue becomes a
// pile, and the pile is what opened ten sessions in eighteen seconds.
func TestOverdueWaitsForItsOwnLastRun(t *testing.T) {
	now := at("2026-09-16T12:00:00Z")
	sc := hourly(now.Add(-72*time.Hour), now.Add(-2*time.Hour))

	if !sc.Due(now, false) {
		t.Fatal("the fixture is wrong: this schedule should be due when nothing is running")
	}
	if sc.Due(now, true) {
		t.Error("Due = true while its own last run is still working")
	}
}

// The floor is checked where a person can still be told about it, rather than
// at fire time where the only available action is to refuse silently.
func TestIntervalFloorIsRefusedAtParseTime(t *testing.T) {
	if _, err := ParseWhen("every 3s"); err == nil {
		t.Error("every 3s was accepted; the floor is what stopped the runaway")
	}
	if _, err := ParseWhen("every 1m"); err != nil {
		t.Errorf("every 1m was refused: %v", err)
	}
	if _, err := ParseWhen("@every 1h"); err == nil {
		t.Error("@every 1h was accepted; it is not a spelling this parser reads")
	}
}

// A daily schedule counts days, not sightings: a daemon asleep at 09:00 runs
// the 09:00 job when it wakes rather than skipping the day.
func TestDailyRunsLateRatherThanSkipping(t *testing.T) {
	nine := 9 * 60
	sc := Schedule{Enabled: true, When: When{Daily: &nine}, FirstSeen: at("2026-09-16T00:00:00Z")}

	morning := time.Date(2026, 9, 16, 8, 59, 0, 0, time.Local)
	if sc.Due(morning, false) {
		t.Error("Due before its minute")
	}
	afternoon := time.Date(2026, 9, 16, 15, 0, 0, 0, time.Local)
	if !sc.Due(afternoon, false) {
		t.Error("not Due six hours late; a missed minute must not skip the day")
	}
	sc.LastRun = time.Date(2026, 9, 16, 15, 0, 1, 0, time.Local)
	if sc.Due(afternoon.Add(time.Hour), false) {
		t.Error("Due twice in one day")
	}
}
