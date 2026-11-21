package cloud

import (
	"encoding/json"
	"testing"
)

func TestStatusCarriesContextAndShellCountsOnlyWhenKnown(t *testing.T) {
	for _, tc := range []struct {
		name            string
		row             map[string]any
		context, shells any
	}{
		{"both", map[string]any{"context_percent": float64(37), "shells": []any{map[string]any{}, map[string]any{}}, "background_reading": "complete", "background_omitted": float64(1)}, float64(37), float64(3)},
		{"no shells", map[string]any{"shells": nil, "background_reading": "complete"}, nil, float64(0)},
		{"unknown window", map[string]any{"context_percent": nil, "background_reading": "complete"}, nil, float64(0)},
		{"unreadable callbacks", map[string]any{"context_percent": float64(80), "shells": []any{map[string]any{}}, "background_reading": "unavailable"}, float64(80), nil},
		{"out of range", map[string]any{"context_percent": float64(140)}, nil, float64(0)},
		{"not current", map[string]any{"context_percent": float64(50), "shells": []any{map[string]any{}}, "stale": true}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			freshness := "current"
			if tc.row["stale"] == true {
				freshness = "unverified"
			}
			row := map[string]any{"state": "working", "source": map[string]any{"freshness": freshness}}
			for k, v := range tc.row {
				row[k] = v
			}
			raw, err := json.Marshal(ProjectSessionStatus("machine", "session", row, true, 100))
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if got := wire["context_percent"]; got != tc.context {
				t.Fatalf("context = %v, want %v; %s", got, tc.context, raw)
			}
			if got := wire["shell_count"]; got != tc.shells {
				t.Fatalf("shells = %v, want %v; %s", got, tc.shells, raw)
			}
		})
	}
}
