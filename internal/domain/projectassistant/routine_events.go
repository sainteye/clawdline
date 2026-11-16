package projectassistant

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// RoutineEventSpec is the pinned Codync event trigger's body: an exact source,
// an exact event name or "*", and filters that must all hold. It is the
// "event" variant of the upstream tagged trigger, not a second schedule format,
// and it is not yet accepted by RoutineTrigger.Validate: the schedule, store and
// entry points refuse event triggers until the complete path is wired.
type RoutineEventSpec struct {
	Source  string            `json:"source"`
	Event   string            `json:"event"`
	Filters map[string]string `json:"filters,omitempty"`
}

// RoutineEventMessageFilter matches as a substring of the event's "text"
// instead of as an exact field, as upstream schedule.rs does.
const RoutineEventMessageFilter = "messageContains"

// RoutineEventAnyName matches every event name from the spec's source.
const RoutineEventAnyName = "*"

// RoutineEventSources are the pinned upstream sources. A source is a label the
// sender supplies, never authority: a generic webhook may claim any of them.
func RoutineEventSources() []string { return slices.Clone(routineEventSources) }

var routineEventSources = []string{"slack", "github", "origin", "microsoftTeams", "linear", "sentry", "pagerduty", "email"}

// RoutineEventLimits are caller-owned bounds. This package registers no product
// default; the caller registers and passes the values it wires.
type RoutineEventLimits struct {
	EventNameBytes   int
	Filters          int
	FilterKeyBytes   int
	FilterValueBytes int
}

// Validate refuses a zero or negative bound instead of treating it as unlimited.
func (l RoutineEventLimits) Validate() error {
	if l.EventNameBytes < 1 || l.Filters < 0 || l.FilterKeyBytes < 1 || l.FilterValueBytes < 1 {
		return fail(Invalid, "routine_event_limits")
	}
	return nil
}

// Validate applies the pinned upstream checks with the caller's bounds. Every
// value must also be valid UTF-8, which upstream strings are by construction.
func (s RoutineEventSpec) Validate(l RoutineEventLimits) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if !isRoutineEventSource(s.Source) {
		return fail(Unsupported, "routine_event_source")
	}
	if s.Event == "" || len(s.Event) > l.EventNameBytes || !utf8.ValidString(s.Event) {
		return fail(Invalid, "routine_event_name")
	}
	if len(s.Filters) > l.Filters {
		return fail(Invalid, "routine_event_filters")
	}
	for key, value := range s.Filters {
		if key == "" || len(key) > l.FilterKeyBytes || !utf8.ValidString(key) {
			return fail(Invalid, "routine_event_filter_key")
		}
		if len(value) > l.FilterValueBytes || !utf8.ValidString(value) {
			return fail(Invalid, "routine_event_filter_value")
		}
	}
	return nil
}

func isRoutineEventSource(source string) bool { return slices.Contains(routineEventSources, source) }

// RoutineEvent is normalized, untrusted event data: a decoded JSON object.
// Its values are what the run may see; none of them grants anything.
type RoutineEvent map[string]any

// Matches is upstream Trigger::matches: exact source, exact event unless the
// spec says "*", and every filter. A filter compares one top-level field that
// must be a JSON string equal to the value; messageContains requires a string
// "text" containing the value. A number, boolean, object or null never matches.
func (s RoutineEventSpec) Matches(e RoutineEvent) bool {
	if str, ok := e["source"].(string); !ok || str != s.Source {
		return false
	}
	if s.Event != RoutineEventAnyName {
		if str, ok := e["event"].(string); !ok || str != s.Event {
			return false
		}
	}
	for key, value := range s.Filters {
		if key == RoutineEventMessageFilter {
			text, ok := e["text"].(string)
			if !ok || !strings.Contains(text, value) {
				return false
			}
			continue
		}
		if str, ok := e[key].(string); !ok || str != value {
			return false
		}
	}
	return true
}
