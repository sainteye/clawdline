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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	When      When      `json:"when"`
	Assistant string    `json:"assistant"`
	Dir       string    `json:"dir"`
	Brief     string    `json:"brief"`
	Claims    []string  `json:"claims"`
	Enabled   bool      `json:"enabled"`
	LastRun   time.Time `json:"lastRun,omitempty"`
	// LastTask is the task the previous run created. A schedule does not come
	// due again while its own last run is still working: firing on a clock
	// while the previous turn is unfinished is how a queue becomes a pile.
	LastTask string `json:"lastTask,omitempty"`
}

// Due reports whether this schedule should fire now.
//
// A schedule that has never run is due immediately for an interval, because
// the alternative is a first run that happens at an arbitrary time nobody
// chose. A daily one is due once its minute has passed today and it has not
// run today — which is deliberately not "within the same minute": a daemon
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
	if s.LastRun.IsZero() {
		return true
	}
	return now.Sub(s.LastRun) >= s.When.Every
}

func sameDay(a, b time.Time) bool {
	if a.IsZero() {
		return false
	}
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
