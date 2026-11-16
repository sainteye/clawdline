package projectassistantacp

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

func routineTriggerSchema() map[string]any {
	variant := func(kind string, properties map[string]any, required ...string) map[string]any {
		properties["type"] = map[string]any{"type": "string", "const": kind}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": append([]string{"type"}, required...)}
	}
	text := map[string]any{"type": "string"}
	return map[string]any{"type": "array", "minItems": 1, "maxItems": pa.MaxRoutineTriggers, "items": map[string]any{"oneOf": []any{
		variant("interval", map[string]any{"seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": pa.MaxRoutineIntervalSeconds}}, "seconds"),
		variant("once", map[string]any{"at": map[string]any{"type": "integer", "minimum": 1}}, "at"),
		variant("cron", map[string]any{"expression": text, "timeZone": text}, "expression", "timeZone"),
		variant("webhook", map[string]any{}),
		variant("event", map[string]any{"source": map[string]any{"type": "string", "enum": pa.RoutineEventSources()}, "event": map[string]any{"type": "string", "minLength": 1, "maxLength": pa.MaxRoutineEventNameBytes}, "filters": map[string]any{"type": "object", "maxProperties": pa.MaxRoutineEventFilters, "propertyNames": map[string]any{"minLength": 1, "maxLength": pa.MaxRoutineEventFilterKeyBytes}, "additionalProperties": map[string]any{"type": "string", "maxLength": pa.MaxRoutineEventFilterValueBytes}}}, "source", "event"),
	}}}
}
