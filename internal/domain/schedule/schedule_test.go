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
// The model changed under it — a wall-clock time and a catch-up window, the
// Swift app's file, instead of an interval — and the incident did not go away
// with the old model: a file with no `created_at` inside its six-hour window is
// the second incident in a new spelling. So the same cells are asked again of
// the new decision.
//
// Everything below states a moment and asks one question of it, so a failure
// names the rule it broke rather than the line it stopped on.

var loc = time.FixedZone("test", 8*3600)

func local(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func daily(h, m int) Schedule {
	return Schedule{Enabled: true, When: When{Hour: h, Minute: m, Daily: true},
		CatchUpHours: DefaultCatchUpHours}
}

func decide(s Schedule, now, firstSeen, handled time.Time) Action {
	fire := s.When.LatestFire(now, loc)
	return s.Decide(Occurrence{Now: now, Fire: fire, FirstSeen: firstSeen, Handled: handled})
}

func TestNeverFiresOnSight(t *testing.T) {
	now := local(2026, 9, 16, 12, 0)
	nine := daily(9, 0) // three hours ago, inside the six-hour window

	cases := []struct {
		name      string
		firstSeen time.Time
		want      Action
	}{
		// The incident. A row arrives with no stamp of its own — imported,
		// restored, copied — and its last occurrence is inside the window.
		{"seen this instant, no created_at", now, BeforeSighting},
		// A row nobody has claimed to have seen cannot be fired at all.
		{"never seen at all", time.Time{}, BeforeSighting},
		// Seen before its minute: it was here at nine, so nine is owed.
		{"seen before its minute", local(2026, 9, 16, 8, 0), Run},
	}
	for _, c := range cases {
		if got := decide(nine, now, c.firstSeen, time.Time{}); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A schedule made at lunchtime for nine does not run this morning's nine, and
// one retimed from 21:00 to 09:00 at two does not invent a nine o'clock either.
func TestStampsGateTheirOwnOccurrences(t *testing.T) {
	now := local(2026, 9, 16, 14, 0)
	seen := local(2026, 9, 1, 0, 0)

	made := daily(9, 0)
	made.CreatedAt = local(2026, 9, 16, 13, 0)
	if got := decide(made, now, seen, time.Time{}); got != BeforeCreation {
		t.Errorf("made after its minute: %s, want %s", got, BeforeCreation)
	}

	retimed := daily(9, 0)
	retimed.WhenChangedAt = local(2026, 9, 16, 13, 59)
	if got := decide(retimed, now, seen, time.Time{}); got != BeforeRetiming {
		t.Errorf("retimed after its minute: %s, want %s", got, BeforeRetiming)
	}
}

// A restart inside the catch-up window must not run the same occurrence twice,
// and a daemon that was down through an occurrence it had seen coming runs it
// once when it is back — never a batch.
func TestRestartNeitherRepeatsNorForgets(t *testing.T) {
	seen := local(2026, 9, 1, 0, 0)
	nine := daily(9, 0)
	fire := local(2026, 9, 16, 9, 0)

	if got := decide(nine, local(2026, 9, 16, 9, 30), seen, fire); got != AlreadyHandled {
		t.Errorf("handled before the restart: %s, want %s", got, AlreadyHandled)
	}
	if got := decide(nine, local(2026, 9, 16, 9, 30), seen, local(2026, 9, 15, 9, 0)); got != Run {
		t.Errorf("down through today's nine: %s, want %s", got, Run)
	}
	// Down for three days: only the latest occurrence is ever asked about.
	if got := nine.When.LatestFire(local(2026, 9, 19, 9, 30), loc); !got.Equal(local(2026, 9, 19, 9, 0)) {
		t.Errorf("latest after three days down: %v", got)
	}
	if got := decide(nine, local(2026, 9, 16, 16, 0), seen, local(2026, 9, 15, 9, 0)); got != Missed {
		t.Errorf("outside the window: %s, want %s", got, Missed)
	}
}

func TestActiveRunSkipsTheOccurrence(t *testing.T) {
	nine := daily(9, 0)
	o := Occurrence{Now: local(2026, 9, 16, 9, 1), Fire: local(2026, 9, 16, 9, 0),
		FirstSeen: local(2026, 9, 1, 0, 0), Active: true}
	if got := nine.Decide(o); got != Active {
		t.Errorf("a run still working: %s, want %s", got, Active)
	}
}

func TestOnceIsSpentByItsStamp(t *testing.T) {
	obj := map[string]any{"at": "09:00", "on": "2026-09-16"}
	w, err := ParseWhen(obj)
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Enabled: true, When: w, CatchUpHours: 6}
	now := local(2026, 9, 16, 9, 30)
	seen := local(2026, 9, 1, 0, 0)
	if got := decide(s, now, seen, time.Time{}); got != Run {
		t.Errorf("a one-shot at its minute: %s, want %s", got, Run)
	}
	s.FiredAt = local(2026, 9, 16, 9, 0)
	if got := decide(s, now, seen, time.Time{}); got != Spent {
		t.Errorf("a one-shot that ran: %s, want %s", got, Spent)
	}
	if next := w.NextFire(now, loc); !next.IsZero() {
		t.Errorf("a one-shot past its minute has a next fire: %v", next)
	}
}

func TestWhenGrammar(t *testing.T) {
	refused := []map[string]any{
		{"at": "09:00"},
		{"at": "09:00", "days": "daily", "on": "2026-09-16"},
		{"at": "9:00", "days": "daily"},
		{"at": "24:00", "days": "daily"},
		{"at": "09:00", "days": []any{}},
		{"at": "09:00", "days": []any{"mon", "mon"}},
		{"at": "09:00", "days": []any{"monday"}},
		{"at": "09:00", "on": "2026-02-30"},
	}
	for _, obj := range refused {
		if _, err := ParseWhen(obj); err == nil {
			t.Errorf("accepted %v", obj)
		}
	}
	w, err := ParseWhen(map[string]any{"at": "07:05", "days": []any{"fri", "mon"}})
	if err != nil {
		t.Fatal(err)
	}
	days, _ := w.Object()["days"].([]string)
	if len(days) != 2 || days[0] != "mon" || days[1] != "fri" {
		t.Errorf("weekdays back out of order: %v", days)
	}
	// Monday 2026-09-14; the next Friday is the 18th.
	if next := w.NextFire(local(2026, 9, 14, 8, 0), loc); !next.Equal(local(2026, 9, 18, 7, 5)) {
		t.Errorf("next after Monday 08:00: %v", next)
	}
}

// A trigger-only schedule has no clock: it is spelled exactly
// `{"trigger_only": true}`, it has no occurrence to run, miss or catch up at
// any moment, and it reads back in its own spelling.
func TestATriggerOnlyScheduleHasNoClock(t *testing.T) {
	w, err := ParseWhen(map[string]any{"trigger_only": true})
	if err != nil || !w.TriggerOnly || w.Once() {
		t.Fatalf("trigger_only: %+v %v", w, err)
	}
	for _, bad := range []map[string]any{
		{"trigger_only": false},
		{"trigger_only": "true"},
		{"trigger_only": true, "at": "09:00"},
		{"trigger_only": true, "days": "daily"},
		{"trigger_only": true, "on": "2026-09-30"},
		{"at": "09:00"},
		{},
	} {
		if _, err := ParseWhen(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	if got := w.Object(); len(got) != 1 || got["trigger_only"] != true {
		t.Fatalf("Object: %v", got)
	}
	timed := When{Hour: 9, Daily: true}
	if w.Same(timed) || timed.Same(w) || !w.Same(When{TriggerOnly: true}) {
		t.Fatal("Same does not tell a trigger-only schedule from a timed one")
	}
	s := Schedule{Enabled: true, When: w, CatchUpHours: MaxCatchUpHours}
	// Across a week of minutes-apart moments, including midnight (Hour and
	// Minute are zero), there is never a latest or a next fire.
	for now := local(2026, 9, 14, 0, 0); now.Before(local(2026, 9, 21, 0, 0)); now = now.Add(97 * time.Minute) {
		if !w.LatestFire(now, loc).IsZero() || !w.NextFire(now, loc).IsZero() {
			t.Fatalf("a trigger-only schedule has an occurrence at %s", now)
		}
	}
	// Even asked directly about a moment, the decision is never run or missed.
	now := local(2026, 9, 16, 12, 0)
	if got := s.Decide(Occurrence{Now: now, Fire: local(2026, 9, 16, 0, 0), FirstSeen: local(2026, 9, 1, 0, 0)}); got != NoClock {
		t.Fatalf("Decide: %s, want %s", got, NoClock)
	}
}
