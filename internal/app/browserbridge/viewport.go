package browserbridge

import "encoding/json"

// Device presets are layout choices. Callers cannot allocate an arbitrary
// viewport or turn this operation into a general CDP command.
func validViewport(args json.RawMessage) bool {
	var value struct {
		Mode string `json:"mode"`
	}
	return json.Unmarshal(args, &value) == nil && (value.Mode == "mobile" || value.Mode == "desktop")
}
