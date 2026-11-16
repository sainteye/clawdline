package projectassistant

import (
	"reflect"
	"testing"
	"time"
)

func routineTime(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRoutineCalendarPreservesWallClockAndDST(t *testing.T) {
	for _, tc := range []struct{ expression, zone, after, want string }{
		{"20 14 * * 0", "Asia/Taipei", "2026-09-26T12:00:00Z", "2026-09-27T06:20:00Z"},
		{"*/15 * * * *", "Asia/Taipei", "2026-09-27T01:07:00Z", "2026-09-27T01:15:00Z"},
		{"0 9 * * 1,3,5", "Asia/Taipei", "2026-09-27T01:07:00Z", "2026-09-28T01:00:00Z"},
		{"0 9 31 * *", "Asia/Taipei", "2026-09-27T01:07:00Z", "2026-10-31T01:00:00Z"},
		{"30 2 * * *", "America/New_York", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00Z"},
		{"30 1 * * *", "America/New_York", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"},
		{"0 9 * * *", "America/New_York", "2026-03-07T14:00:00Z", "2026-03-08T13:00:00Z"},
		{"0 0 29 2 *", "UTC", "2026-03-01T00:00:00Z", "2028-02-29T00:00:00Z"},
	} {
		t.Run(tc.zone+"/"+tc.expression+"/"+tc.after, func(t *testing.T) {
			trigger := RoutineTrigger{Type: "cron", Expression: tc.expression, TimeZone: tc.zone}
			got, err := trigger.Next(routineTime(t, tc.after))
			if err != nil || !got.Equal(routineTime(t, tc.want)) {
				t.Fatalf("next=%s want=%s err=%v", got, tc.want, err)
			}
		})
	}
}

func TestRoutineScheduleRetainsIntentAndExhaustsOnce(t *testing.T) {
	now := routineTime(t, "2026-11-16T01:00:00Z")
	triggers := []RoutineTrigger{{Type: "once", At: now.Add(time.Minute).UnixMilli()}, {Type: "interval", Seconds: 60}, {Type: "cron", Expression: "* * * * *", TimeZone: "Asia/Taipei"}}
	r, err := SetRoutineSchedule(Routine{ID: "r", Enabled: true}, Routine{}, triggers, now)
	if err != nil || !r.NextDue.Equal(now.Add(time.Minute)) {
		t.Fatal(r, err)
	}
	advanced, err := AdvanceRoutineSchedule(r, now.Add(3*time.Minute+15*time.Second))
	if err != nil || !advanced.NextRuns[0].IsZero() || !advanced.NextDue.Equal(now.Add(4*time.Minute)) || !reflect.DeepEqual(advanced.Triggers, triggers) {
		t.Fatal(advanced, err)
	}
	// An edit retains an already spent one-shot and the anchored next wakes.
	edited, err := SetRoutineSchedule(advanced, advanced, triggers, now.Add(5*time.Minute))
	if err != nil || !edited.NextRuns[0].IsZero() || !edited.NextDue.Equal(advanced.NextDue) {
		t.Fatal("edit moved intent", edited, err)
	}
	if _, err = SetRoutineSchedule(Routine{}, Routine{}, triggers, now.Add(5*time.Minute)); Code(err) != Invalid {
		t.Fatal("accepted new past once", err)
	}
	// A one-shot has no timer deadline after admission, but remains enabled
	// so its queued execution can still claim it.
	one, err := SetRoutineSchedule(Routine{ID: "once", Enabled: true}, Routine{}, triggers[:1], now)
	if err != nil {
		t.Fatal(err)
	}
	one, err = AdvanceRoutineSchedule(one, now.Add(time.Minute))
	if err != nil || !one.NextDue.IsZero() || !one.Enabled {
		t.Fatal(one, err)
	}
}

func TestRoutineLegacyCadenceAndScheduleValidation(t *testing.T) {
	now := time.Unix(1800000000, 0)
	old := Routine{ID: "legacy", Enabled: true, IntervalSeconds: 60, NextDue: now.Add(-time.Minute)}
	converted, err := SetRoutineSchedule(old, old, old.ScheduleTriggers(), now)
	if err != nil || !converted.NextDue.Equal(old.NextDue) || converted.IntervalSeconds != 0 {
		t.Fatal(converted, err)
	}
	for _, trigger := range []RoutineTrigger{
		{Type: "interval", Seconds: 0}, {Type: "interval", Seconds: 60, At: 1},
		{Type: "once", At: -1}, {Type: "once", At: now.UnixMilli(), TimeZone: "UTC"},
		{Type: "cron", Expression: "* * * * *"}, {Type: "cron", Expression: "* * * * *", TimeZone: "Local"},
		{Type: "cron", Expression: "* * * * *", TimeZone: "not-a-zone"},
		{Type: "cron", Expression: "CRON_TZ=UTC * * * * *", TimeZone: "Asia/Taipei"},
		{Type: "cron", Expression: "* * * * * *", TimeZone: "UTC"},
		{Type: "cron", Expression: "@every 1h", TimeZone: "UTC"}, {Type: "webhook", Seconds: 1},
	} {
		if trigger.Validate() == nil {
			t.Fatal("accepted invalid trigger", trigger)
		}
	}
	impossible := RoutineTrigger{Type: "cron", Expression: "0 0 31 2 *", TimeZone: "UTC"}
	if _, err := impossible.Next(now); Code(err) != Invalid {
		t.Fatal("accepted impossible calendar", err)
	}
	triggers := make([]RoutineTrigger, MaxRoutineTriggers)
	for i := range triggers {
		triggers[i] = RoutineTrigger{Type: "interval", Seconds: int64(i + 1)}
	}
	if _, err := SetRoutineSchedule(Routine{}, Routine{}, triggers, now); err != nil {
		t.Fatal("at-limit refused", err)
	}
	if _, err := SetRoutineSchedule(Routine{}, Routine{}, append(triggers, triggers[0]), now); Code(err) != Invalid {
		t.Fatal("over-limit accepted", err)
	}
}
