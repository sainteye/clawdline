// Package schedule is work that repeats: a stored task template and the clock
// that decides when it is due.
package schedule

import (
	"fmt"
	"strings"
	"time"
)

// When says how often a schedule comes due. Two forms, because they answer
// different questions: "every so often" and "at this time of day".
type When struct {
	Every time.Duration `json:"every,omitempty"`
	// Daily is minutes since midnight, local time.
	Daily *int `json:"daily,omitempty"`
}

func ParseWhen(text string) (When, error) {
	text = strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(text, "every "):
		d, err := time.ParseDuration(strings.TrimPrefix(text, "every "))
		if err != nil {
			return When{}, fmt.Errorf("bad_schedule: %q is not a duration", text)
		}
		if d <= 0 {
			return When{}, fmt.Errorf("bad_schedule: an interval must be positive")
		}
		if d < MinInterval {
			return When{}, fmt.Errorf(
				"bad_schedule: %s is shorter than the %s floor; work that cannot finish between two runs will pile up",
				d, MinInterval)
		}
		return When{Every: d}, nil
	case strings.HasPrefix(text, "at "):
		t, err := time.Parse("15:04", strings.TrimPrefix(text, "at "))
		if err != nil {
			return When{}, fmt.Errorf("bad_schedule: %q is not a time of day", text)
		}
		m := t.Hour()*60 + t.Minute()
		return When{Daily: &m}, nil
	}
	return When{}, fmt.Errorf("bad_schedule: say %q or %q", "every 30m", "at 09:00")
}

func (w When) String() string {
	if w.Daily != nil {
		return fmt.Sprintf("at %02d:%02d", *w.Daily/60, *w.Daily%60)
	}
	return "every " + w.Every.String()
}

// MinInterval is the shortest interval a schedule may carry.
//
// A schedule that comes due faster than its own work can finish is a runaway,
// and it was one: a three-second interval opened ten real assistant sessions in
// eighteen seconds before anything noticed. The floor is a value rather than a
// constant so a test can reach it without waiting a minute.
var MinInterval = time.Minute

// Schedule is one stored template.
type Schedule struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	When When   `json:"when"`
	// Spec is the stored spelling, kept verbatim. A schedule whose spelling
	// could not be parsed still has to be shown as what it actually says:
	// rendering its zero When as "every 0s" turns "we could not read this"
	// into a value, and a value is something a person will act on.
	Spec string `json:"spec"`
	// Unreadable is set when Spec did not parse. Such a schedule is switched
	// off rather than guessed at, and that has to be distinguishable from one
	// a person switched off.
	Unreadable bool      `json:"unreadable,omitempty"`
	Assistant  string    `json:"assistant"`
	Dir        string    `json:"dir"`
	Brief      string    `json:"brief"`
	Claims     []string  `json:"claims"`
	Enabled    bool      `json:"enabled"`
	LastRun    time.Time `json:"lastRun,omitempty"`
	// FirstSeen is when this daemon first had this schedule in front of it and
	// could have fired it.
	//
	// It exists because "never run" and "due now" are not the same thing, and
	// treating them as the same opened a real assistant session twice in this
	// project's short life. The first time the interval was three seconds and
	// a floor fixed it. The second time the interval was an hour and perfectly
	// legal: a row that arrived in the store with last_run at zero fired on
	// the next tick. The rate was never the shape of the problem — the first
	// fire was.
	FirstSeen time.Time `json:"firstSeen,omitempty"`
	// LastTask is the task the previous run created. A schedule does not come
	// due again while its own last run is still working: firing on a clock
	// while the previous turn is unfinished is how a queue becomes a pile.
	LastTask string `json:"lastTask,omitempty"`
}

// Due reports whether this schedule should fire now.
//
// An interval schedule is due one interval after the later of its last run and
// the moment this daemon first saw it — never on sight. A daily one is due once
// its minute has passed today and it has not run today — which is deliberately not "within the same minute": a daemon
// that was asleep at 09:00 should still run the 09:00 job when it wakes,
// rather than silently skip a day.
func (s Schedule) Due(now time.Time, lastStillRunning bool) bool {
	if !s.Enabled || lastStillRunning {
		return false
	}
	if s.When.Daily != nil {
		minutes := now.Hour()*60 + now.Minute()
		if minutes < *s.When.Daily {
			return false
		}
		return !sameDay(s.LastRun, now)
	}
	// An interval counts from the later of its last run and the moment this
	// daemon first saw it. A schedule somebody creates now first fires one
	// interval from now, which is the time they chose; the alternative, firing
	// at once, is not "the time nobody chose" made safe, it is every restored
	// or copied schedule in the store firing together.
	base := s.FirstSeen
	if s.LastRun.After(base) {
		base = s.LastRun
	}
	if base.IsZero() {
		// Nothing has claimed to have seen this schedule, so nothing may fire
		// it. A caller that has not recorded a first sighting has not finished
		// loading it, and firing on an unloaded row is how this went wrong.
		return false
	}
	return now.Sub(base) >= s.When.Every
}

func sameDay(a, b time.Time) bool {
	if a.IsZero() {
		return false
	}
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
