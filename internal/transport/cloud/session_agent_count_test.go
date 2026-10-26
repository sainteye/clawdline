package cloud

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestStatusCountsOnlyCurrentCompleteProviderWork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reading   map[string]any
		agents    []any
		freshness string
		count     any
		unknown   bool
	}{
		{"mixed", map[string]any{"state": "complete"}, []any{map[string]any{"state": "running"}, map[string]any{"state": "done"}, map[string]any{"state": "failed"}}, "current", float64(1), false},
		{"empty", map[string]any{"state": "complete"}, nil, "current", float64(0), false},
		{"finished", map[string]any{"state": "complete"}, []any{map[string]any{"state": "done"}}, "current", float64(0), false},
		{"truncated", map[string]any{"state": "complete", "truncated": float64(1)}, []any{map[string]any{"state": "running"}}, "current", nil, true},
		{"unknown", map[string]any{"state": "unknown"}, []any{map[string]any{"state": "running"}}, "current", nil, true},
		{"unknown agent", map[string]any{"state": "complete"}, []any{map[string]any{"state": "unknown"}}, "current", nil, true},
		{"unread empty", map[string]any{"state": "unknown"}, nil, "current", nil, false},
		{"old", map[string]any{"state": "complete"}, []any{map[string]any{"state": "running"}}, "unverified", nil, false},
		{"old machine", nil, nil, "current", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := map[string]any{"state": "idle", "agents_reading": tc.reading, "agents": tc.agents, "source": map[string]any{"freshness": tc.freshness}}
			raw, err := json.Marshal(ProjectSessionStatus("machine", "session", row, true, 100))
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if got := wire["running_agent_count"]; got != tc.count {
				t.Fatalf("count = %v, want %v; %s", got, tc.count, raw)
			}
			if got := wire["agent_count_unknown"] == true; got != tc.unknown {
				t.Fatalf("unknown = %v, want %v; %s", got, tc.unknown, raw)
			}
		})
	}
}

func TestProviderCountReusesStatusWindowWithoutRefreshingPresentations(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(5000, 0)
	fleet := []statusSession{{id: "%1", state: "idle", agents: 6}}
	body := statusScan(0, fleet)
	out := &collector{}
	p := drivenPublisher(out, &body, &now)
	reads := 0
	p.Sessions = func(context.Context) ([]byte, bool) { reads++; return body, true }
	p.firstPass(ctx)
	out.reset()
	fleet[0].agents = 0
	body = statusScan(5, fleet)
	now = now.Add(5 * time.Second)
	p.Pass(ctx)
	if n := countPrefix(out.channels(), "ss/mac-01/"); n != 0 {
		t.Fatalf("count bypassed coalescing: %v", out.channels())
	}
	now = now.Add(10 * time.Second)
	body = statusScan(15, fleet)
	p.Pass(ctx)
	if n := countPrefix(out.channels(), "ss/mac-01/%251"); n != 1 {
		t.Fatalf("count did not settle in existing window: %v", out.channels())
	}
	if n := countPrefix(out.channels(), "ss/mac-01/"+InventorySessionID); n != 0 {
		t.Fatalf("count refreshed presentation inventory: %v", out.channels())
	}
	if got := out.payload(t, "ss/mac-01/%251")["running_agent_count"]; got != float64(0) {
		t.Fatalf("ended agents retained count: %v", got)
	}
	if reads != 3 {
		t.Fatalf("count started an extra snapshot read: %d", reads)
	}
}
