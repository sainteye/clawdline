package projectassistant

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRoutineHookSchedulesHaveNoClockWake(t *testing.T) {
	for _, trigger := range []RoutineTrigger{{Type: "webhook"}, {Type: "event", Source: "github", Event: "issues", Filters: map[string]string{"action": "opened"}}} {
		r, err := SetRoutineSchedule(Routine{}, Routine{}, []RoutineTrigger{trigger}, time.Now())
		if err != nil || !r.NextDue.IsZero() || len(r.NextRuns) != 1 || !r.NextRuns[0].IsZero() {
			t.Fatalf("hook became a clock schedule: %+v %v", r, err)
		}
	}
}

func TestRoutineHookEmptyFiltersRetainOtherClockAnchors(t *testing.T) {
	now := time.Now().UTC()
	triggers := []RoutineTrigger{{Type: "interval", Seconds: 60}, {Type: "event", Source: "github", Event: "*", Filters: map[string]string{}}}
	r, err := SetRoutineSchedule(Routine{ID: "retained", Enabled: true}, Routine{}, triggers, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	var persisted Routine
	if err = json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	edited, err := SetRoutineSchedule(persisted, persisted, triggers, now.Add(30*time.Second))
	if err != nil || !edited.NextDue.Equal(r.NextDue) {
		t.Fatal("empty filter representation reset the interval", edited.NextDue, r.NextDue, err)
	}
}
