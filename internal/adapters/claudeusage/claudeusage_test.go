package claudeusage

import (
	"testing"
	"time"
)

// The shape Claude Code answered with on a Max plan, with synthetic numbers.
const claudeUsageLine = `{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"subscription_type":"max","rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":2,"resets_at":"2026-11-02T15:49:59.418949+00:00"},"seven_day":{"utilization":66,"resets_at":"2026-11-05T19:59:59.418968+00:00"},"seven_day_opus":null,"model_scoped":[{"display_name":"Fable","utilization":0,"resets_at":"2026-11-05T20:00:00+00:00"}]},"session":{"total_cost_usd":0},"behaviors":null}}}`

func TestReplyKeepsOnlyLivePlanWindows(t *testing.T) {
	now := time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC)
	windows, outcome, ours := reply([]byte(claudeUsageLine), now)
	if !ours || outcome != "" || len(windows) != 2 || windows[0].Name != "5h" || windows[0].UsedPercent != 2 ||
		windows[1].Name != "7d" || windows[1].UsedPercent != 66 || windows[1].ResetsAt == nil {
		t.Fatalf("windows=%+v outcome=%q ours=%v", windows, outcome, ours)
	}
	// After the five-hour reset, only the weekly window is still a reading.
	windows, _, _ = reply([]byte(claudeUsageLine), time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC))
	if len(windows) != 1 || windows[0].Name != "7d" {
		t.Fatalf("reset window kept: %+v", windows)
	}
	for line, want := range map[string]Outcome{
		`{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"rate_limits_available":false,"rate_limits":null}}}`:                              NoPlan,
		`{"type":"control_response","response":{"subtype":"error","request_id":"clawdline-usage","error":"Unsupported control request subtype: get_usage"}}`:                                     Failed,
		`{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":null}}}}}`: NoWindow,
	} {
		if w, p, ours := reply([]byte(line), now); !ours || p != want || len(w) != 0 {
			t.Errorf("%s: windows=%+v outcome=%q ours=%v", line, w, p, ours)
		}
	}
	for _, line := range []string{`{"type":"system","subtype":"init"}`, `{"type":"control_response","response":{"request_id":"other"}}`, `not json`} {
		if _, _, ours := reply([]byte(line), now); ours {
			t.Errorf("%s was taken as this probe's answer", line)
		}
	}
}
