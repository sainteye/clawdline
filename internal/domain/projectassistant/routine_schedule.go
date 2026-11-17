package projectassistant

import (
	"maps"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

// RoutineTrigger retains the person's schedule, independently of its next wake.
// At is Unix milliseconds, as in the pinned Codync routine contract.
type RoutineTrigger struct {
	Type       string            `json:"type"`
	Seconds    int64             `json:"seconds,omitempty"`
	At         int64             `json:"at,omitempty"`
	Expression string            `json:"expression,omitempty"`
	TimeZone   string            `json:"timeZone,omitempty"`
	Source     string            `json:"source,omitempty"`
	Event      string            `json:"event,omitempty"`
	Filters    map[string]string `json:"filters,omitempty"`
}

func (t RoutineTrigger) Validate() error {
	bad := func(field string) error { return &Failure{Code: Invalid, Field: field} }
	if t.Type != "event" && (t.Source != "" || t.Event != "" || len(t.Filters) != 0) {
		return bad("routine_event_fields")
	}
	switch t.Type {
	case "webhook", "event":
		if t.Seconds != 0 || t.At != 0 || t.Expression != "" || t.TimeZone != "" {
			return bad("routine_hook_schedule")
		}
		if t.Type == "event" {
			return t.EventSpec().Validate(DefaultRoutineEventLimits())
		}
	case "interval":
		if t.Seconds < 1 || t.Seconds > MaxRoutineIntervalSeconds || t.At != 0 || t.Expression != "" || t.TimeZone != "" {
			return bad("routine_interval")
		}
	case "once":
		if t.At <= 0 || t.Seconds != 0 || t.Expression != "" || t.TimeZone != "" {
			return bad("routine_once")
		}
		if _, err := time.UnixMilli(t.At).MarshalJSON(); err != nil {
			return bad("routine_once")
		}
	case "cron":
		if t.At != 0 || t.Seconds != 0 {
			return bad("routine_cron")
		}
		if _, err := t.cron(); err != nil {
			return err
		}
	default:
		return &Failure{Code: Unsupported, Field: "routine_trigger"}
	}
	return nil
}

func isCronDescriptor(expr string) bool {
	switch strings.ToLower(strings.TrimSpace(expr)) {
	case "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly":
		return true
	default:
		return false
	}
}

func (t RoutineTrigger) cron() (cron.Schedule, error) {
	expr := strings.TrimSpace(t.Expression)
	fields := strings.Fields(expr)
	isDesc := isCronDescriptor(expr)
	if (!isDesc && len(fields) != 5) || strings.Contains(expr, "=") || strings.HasPrefix(strings.ToLower(expr), "@every") {
		return nil, &Failure{Code: Invalid, Field: "routine_cron"}
	}
	if t.TimeZone == "" || t.TimeZone == "Local" || len(strings.Fields(t.TimeZone)) != 1 {
		return nil, &Failure{Code: Invalid, Field: "routine_time_zone"}
	}
	if _, err := time.LoadLocation(t.TimeZone); err != nil {
		return nil, &Failure{Code: Invalid, Field: "routine_time_zone"}
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	s, err := parser.Parse("CRON_TZ=" + t.TimeZone + " " + expr)
	if err != nil {
		return nil, &Failure{Code: Invalid, Field: "routine_cron"}
	}
	return s, nil
}

func (t RoutineTrigger) Next(after time.Time) (time.Time, error) {
	if err := t.Validate(); err != nil {
		return time.Time{}, err
	}
	switch t.Type {
	case "webhook", "event":
		return time.Time{}, nil
	case "interval":
		return after.Add(time.Duration(t.Seconds) * time.Second), nil
	case "once":
		at := time.UnixMilli(t.At)
		if at.After(after) {
			return at, nil
		}
		return time.Time{}, nil
	default:
		s, err := t.cron()
		if err != nil {
			return time.Time{}, err
		}
		next := s.Next(after)
		if next.IsZero() {
			return time.Time{}, &Failure{Code: Invalid, Field: "routine_cron_future"}
		}
		return next, nil
	}
}

func earliestRoutineRun(runs []time.Time) time.Time {
	var next time.Time
	for _, at := range runs {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	return next
}

func (r Routine) ScheduleTriggers() []RoutineTrigger {
	if len(r.Triggers) != 0 {
		return cloneRoutineTriggers(r.Triggers)
	}
	return []RoutineTrigger{{Type: "interval", Seconds: r.IntervalSeconds}}
}

// SetRoutineSchedule validates future intent at admission, but permits a retained
// past once trigger on edits. Unchanged schedules retain their durable deadlines.
func SetRoutineSchedule(r, old Routine, triggers []RoutineTrigger, now time.Time) (Routine, error) {
	if len(triggers) == 0 || len(triggers) > MaxRoutineTriggers {
		return r, &Failure{Code: Invalid, Field: "routine_triggers"}
	}
	for _, trigger := range triggers {
		if err := trigger.Validate(); err != nil {
			return r, err
		}
		if trigger.Type == "once" && !time.UnixMilli(trigger.At).After(now) && !slices.ContainsFunc(old.ScheduleTriggers(), func(v RoutineTrigger) bool { return routineTriggerEqual(v, trigger) }) {
			return r, &Failure{Code: Invalid, Field: "routine_once_future"}
		}
	}
	r.Triggers = cloneRoutineTriggers(triggers)
	r.IntervalSeconds = 0
	if old.ID != "" && slices.EqualFunc(old.ScheduleTriggers(), triggers, routineTriggerEqual) && (!r.Enabled || old.Enabled) {
		r.NextRuns = slices.Clone(old.NextRuns)
		if len(r.NextRuns) == 0 {
			r.NextRuns = []time.Time{old.NextDue}
		}
	} else {
		r.NextRuns = make([]time.Time, len(triggers))
		for i, trigger := range triggers {
			at, err := trigger.Next(now)
			if err != nil {
				return r, err
			}
			r.NextRuns[i] = at
		}
	}
	r.NextDue = earliestRoutineRun(r.NextRuns)
	return r, nil
}

func routineTriggerEqual(a, b RoutineTrigger) bool {
	return a.Type == b.Type && a.Seconds == b.Seconds && a.At == b.At && a.Expression == b.Expression && a.TimeZone == b.TimeZone && a.Source == b.Source && a.Event == b.Event && maps.Equal(a.Filters, b.Filters)
}

func DefaultRoutineEventLimits() RoutineEventLimits {
	return RoutineEventLimits{MaxRoutineEventNameBytes, MaxRoutineEventFilters, MaxRoutineEventFilterKeyBytes, MaxRoutineEventFilterValueBytes}
}

func (t RoutineTrigger) EventSpec() RoutineEventSpec {
	return RoutineEventSpec{Source: t.Source, Event: t.Event, Filters: t.Filters}
}

func (r Routine) HasHook() bool {
	for _, t := range r.Triggers {
		if t.Type == "webhook" || t.Type == "event" {
			return true
		}
	}
	return false
}

func (r Routine) MatchesEvent(e RoutineEvent) bool {
	for _, t := range r.Triggers {
		if t.Type == "webhook" || t.Type == "event" && t.EventSpec().Matches(e) {
			return true
		}
	}
	return false
}

func cloneRoutineTriggers(in []RoutineTrigger) []RoutineTrigger {
	out := slices.Clone(in)
	for i := range out {
		out[i].Filters = maps.Clone(out[i].Filters)
	}
	return out
}

// AdvanceRoutineSchedule coalesces simultaneous triggers, keeps interval
// anchors and exhausts each once trigger without disabling its queued run.
func AdvanceRoutineSchedule(r Routine, now time.Time) (Routine, error) {
	if len(r.Triggers) == 0 {
		r.NextDue = AdvanceRoutine(r, now)
		return r, nil
	}
	r.NextRuns = slices.Clone(r.NextRuns)
	for i, due := range r.NextRuns {
		if due.IsZero() || due.After(now) {
			continue
		}
		trigger := r.Triggers[i]
		var next time.Time
		if trigger.Type == "interval" {
			step := time.Duration(trigger.Seconds) * time.Second
			next = due.Add((now.Sub(due)/step + 1) * step)
		} else {
			var err error
			next, err = trigger.Next(now)
			if err != nil {
				return r, err
			}
		}
		r.NextRuns[i] = next
	}
	r.NextDue = earliestRoutineRun(r.NextRuns)
	return r, nil
}
