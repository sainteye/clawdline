package browserbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	// MinViewportSide and the maxima bound an agent's emulated viewport, in
	// CSS pixels. A caller can choose a size but cannot allocate an arbitrary
	// surface or turn this operation into a general CDP command.
	MinViewportSide   = 200
	MaxViewportWidth  = 3840
	MaxViewportHeight = 2160
	// MaxClickCoordinate bounds one click coordinate in CSS pixels; it is
	// Chrome's own largest window side, so no real viewport point is refused.
	MaxClickCoordinate = 16384
)

// NotFound is a click label the page does not show.
const NotFound Refusal = "browser_element_not_found"

// ArgError is a refused argument: its code, the field and what is allowed.
// It unwraps to its Refusal, so IsRefusal still reports the typed code.
type ArgError struct {
	Code    Refusal
	Field   string
	Message string
}

func (e *ArgError) Error() string { return string(e.Code) + ": " + e.Field + ": " + e.Message }
func (e *ArgError) Unwrap() error { return e.Code }

func badArg(field, format string, v ...any) *ArgError {
	return &ArgError{Code: BadRequest, Field: field, Message: fmt.Sprintf(format, v...)}
}

// validViewport is the person's route: only the two layout presets.
func validViewport(args json.RawMessage) bool {
	var value struct {
		Mode string `json:"mode"`
	}
	return json.Unmarshal(args, &value) == nil && (value.Mode == "mobile" || value.Mode == "desktop")
}

// agentViewport checks an agent's viewport and returns the arguments the
// extension receives. It keeps {"mode":"mobile"|"desktop"}, reads
// {"preset":...} as the same, and adds {"width","height"} in CSS pixels with
// optional booleans mobile and touch (both default to false).
func agentViewport(args json.RawMessage) (json.RawMessage, error) {
	fields, err := objectFields(args)
	if err != nil {
		return nil, badArg("args", "use {\"mode\":\"mobile\"|\"desktop\"} or {\"width\":W,\"height\":H}")
	}
	if p, ok := fields["preset"]; ok {
		if _, both := fields["mode"]; both {
			return nil, badArg("preset", "use mode or preset, not both")
		}
		fields["mode"] = p
		delete(fields, "preset")
	}
	_, hasW := fields["width"]
	_, hasH := fields["height"]
	if raw, ok := fields["mode"]; ok || (!hasW && !hasH) {
		var mode string
		if !ok || json.Unmarshal(raw, &mode) != nil || (mode != "mobile" && mode != "desktop") {
			return nil, badArg("mode", "mode must be \"mobile\" or \"desktop\", or give width and height instead")
		}
		for name := range fields {
			if name != "mode" && name != "tab" {
				return nil, badArg("mode", "mode cannot be combined with %s; use either mode or width and height", name)
			}
		}
		return json.Marshal(fields)
	}
	for name := range fields {
		switch name {
		case "width", "height", "mobile", "touch", "tab":
		default:
			return nil, badArg(name, "unknown field; a size takes width, height, mobile and touch")
		}
	}
	for _, side := range []struct {
		name string
		max  int
	}{{"width", MaxViewportWidth}, {"height", MaxViewportHeight}} {
		raw, ok := fields[side.name]
		var n float64
		if !ok || json.Unmarshal(raw, &n) != nil || n != math.Trunc(n) || n < MinViewportSide || n > float64(side.max) {
			return nil, badArg(side.name, "%s must be a whole number of CSS pixels from %d to %d", side.name, MinViewportSide, side.max)
		}
	}
	for _, name := range []string{"mobile", "touch"} {
		if raw, ok := fields[name]; ok {
			var b bool
			if json.Unmarshal(raw, &b) != nil {
				return nil, badArg(name, "%s must be true or false", name)
			}
		}
	}
	return json.Marshal(fields)
}

// agentClick checks an agent's click and returns the arguments the extension
// receives: a visible label (args.label, or the CLI's args.target) or a point
// x, y in CSS pixels of the viewport. Nothing without one reaches the page,
// so the extension never sends a mouse event without coordinates.
func agentClick(args json.RawMessage) (json.RawMessage, error) {
	fields, err := objectFields(args)
	if err != nil {
		return nil, badArg("args", "use {\"label\":\"visible text\"} or {\"x\":X,\"y\":Y} in CSS pixels")
	}
	label, hasLabel := fields["label"]
	if target, ok := fields["target"]; ok {
		if hasLabel {
			return nil, badArg("label", "use label or target, not both")
		}
		label, hasLabel = target, true
		delete(fields, "target")
	}
	_, hasX := fields["x"]
	_, hasY := fields["y"]
	if hasLabel {
		var text string
		if json.Unmarshal(label, &text) != nil || strings.TrimSpace(text) == "" {
			return nil, badArg("label", "label must be the element's visible text")
		}
		if hasX || hasY {
			return nil, badArg("label", "use label or x and y, not both")
		}
		delete(fields, "label")
		fields["target"] = label
		return json.Marshal(fields)
	}
	if !hasX && !hasY {
		return nil, badArg("label", "give label (the element's visible text) or x and y in CSS pixels")
	}
	for _, name := range []string{"x", "y"} {
		raw, ok := fields[name]
		var n float64
		if !ok || json.Unmarshal(raw, &n) != nil || math.IsNaN(n) || n < 0 || n > MaxClickCoordinate {
			return nil, badArg(name, "%s must be a number of CSS pixels from 0 to %d, measured from the viewport's top-left; a screenshot pixel divided by its scale is one CSS pixel", name, MaxClickCoordinate)
		}
	}
	return json.Marshal(fields)
}

// objectFields reads one JSON object without losing its values' encoding.
func objectFields(args json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	return fields, nil
}

// notFoundPrefix is how the extension reports a label it cannot see.
const notFoundPrefix = "no visible element matches"

func clickNotFound(message string) error {
	if !strings.HasPrefix(message, notFoundPrefix) {
		return nil
	}
	return &ArgError{Code: NotFound, Field: "label", Message: message}
}
