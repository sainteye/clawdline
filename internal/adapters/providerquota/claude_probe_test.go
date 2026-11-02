package providerquota

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/limits"
)

// The shape Claude Code answered with on a Max plan, with synthetic numbers.
const claudeUsageLine = `{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"subscription_type":"max","rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":2,"resets_at":"2026-11-02T15:49:59.418949+00:00"},"seven_day":{"utilization":66,"resets_at":"2026-11-05T19:59:59.418968+00:00"},"seven_day_opus":null,"model_scoped":[{"display_name":"Fable","utilization":0,"resets_at":"2026-11-05T20:00:00+00:00"}]},"session":{"total_cost_usd":0},"behaviors":null}}}`

func TestClaudeUsageReplyKeepsOnlyLivePlanWindows(t *testing.T) {
	now := time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC)
	windows, problem, ours := claudeUsageReply([]byte(claudeUsageLine), now)
	if !ours || problem != "" || len(windows) != 2 || windows[0].Name != "5h" || windows[0].UsedPercent != 2 ||
		windows[1].Name != "7d" || windows[1].UsedPercent != 66 || windows[1].ResetsAt == nil {
		t.Fatalf("windows=%+v problem=%q ours=%v", windows, problem, ours)
	}
	// After the five-hour reset, only the weekly window is still a reading.
	windows, _, _ = claudeUsageReply([]byte(claudeUsageLine), time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC))
	if len(windows) != 1 || windows[0].Name != "7d" {
		t.Fatalf("reset window kept: %+v", windows)
	}
	for line, want := range map[string]string{
		`{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"rate_limits_available":false,"rate_limits":null}}}`:                              "no_reading",
		`{"type":"control_response","response":{"subtype":"error","request_id":"clawdline-usage","error":"Unsupported control request subtype: get_usage"}}`:                                     "unavailable",
		`{"type":"control_response","response":{"subtype":"success","request_id":"clawdline-usage","response":{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":null}}}}}`: "no_reading",
	} {
		if w, p, ours := claudeUsageReply([]byte(line), now); !ours || p != want || len(w) != 0 {
			t.Errorf("%s: windows=%+v problem=%q ours=%v", line, w, p, ours)
		}
	}
	for _, line := range []string{`{"type":"system","subtype":"init"}`, `{"type":"control_response","response":{"request_id":"other"}}`, `not json`} {
		if _, _, ours := claudeUsageReply([]byte(line), now); ours {
			t.Errorf("%s was taken as this probe's answer", line)
		}
	}
}

func claudeService(t *testing.T, statusLine string) (*Service, *atomic.Int32, chan struct{}) {
	t.Helper()
	home := t.TempDir()
	if statusLine != "" {
		dir := filepath.Join(home, ".claude", "statusline-cache")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rate-limits.json"), []byte(statusLine), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := New(home, limits.NewReader(home, func() limits.Settings { return limits.Settings{} }))
	var calls atomic.Int32
	release := make(chan struct{})
	s.WithClaude(func(ctx context.Context) ([]Window, string) {
		calls.Add(1)
		<-release
		reset := time.Now().Add(time.Hour).Unix()
		return []Window{{Name: "5h", UsedPercent: 3, ResetsAt: &reset}, {Name: "7d", UsedPercent: 66}}, ""
	})
	return s, &calls, release
}

func claudeRow(t *testing.T, snap Snapshot) Row {
	t.Helper()
	for _, row := range snap.Providers {
		if row.ID == "claude" {
			return row
		}
	}
	t.Fatalf("no claude row in %+v", snap)
	return Row{}
}

func TestClaudeWithoutAStatusLineIsAskedOnceAndOnlyFromTheFooter(t *testing.T) {
	s, calls, release := claudeService(t, "")
	now := time.Now()
	if row := claudeRow(t, s.Inspect(now)); row.Problem != "no_record" || calls.Load() != 0 {
		t.Fatalf("Doctor inspection asked Claude Code: %+v calls=%d", row, calls.Load())
	}
	if !s.Snapshot(now).Updating {
		t.Fatal("the footer read did not start the fallback")
	}
	for i := 0; i < 10; i++ {
		s.Snapshot(now)
	}
	close(release)
	waitFinished(t, s)
	if calls.Load() != 1 {
		t.Fatalf("Claude Code asked %d times while one probe ran", calls.Load())
	}
	row := claudeRow(t, s.Snapshot(now))
	if row.Source != "provider_cli" || row.Problem != "" || row.Stale || len(row.Windows) != 2 || row.Windows[1].UsedPercent != 66 {
		t.Fatalf("row=%+v", row)
	}
	// The 15-minute floor holds until it passes.
	s.Snapshot(now.Add(time.Minute))
	waitFinished(t, s)
	if calls.Load() != 1 {
		t.Fatal("asked again inside the refresh floor")
	}
	s.Snapshot(now.Add(RefreshSecondsLimit * time.Second))
	waitFinished(t, s)
	if calls.Load() != 2 {
		t.Fatalf("not asked again after the floor: %d", calls.Load())
	}
}

func TestAFreshStatusLineIsNeverReplacedOrProbed(t *testing.T) {
	reset := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	s, calls, release := claudeService(t, `{"at":`+strconv.FormatInt(time.Now().Unix(), 10)+`,"rate_limits":{"five_hour":{"used_percentage":2,"resets_at":`+reset+`},"seven_day":{"used_percentage":65,"resets_at":`+reset+`}}}`)
	defer close(release)
	snap := s.Snapshot(time.Now())
	row := claudeRow(t, snap)
	if snap.Updating || calls.Load() != 0 || row.Source != "claude_statusline" || len(row.Windows) != 2 || row.Windows[1].UsedPercent != 65 {
		t.Fatalf("fresh status line replaced: %+v updating=%v calls=%d", row, snap.Updating, calls.Load())
	}
}

func TestAFailedFallbackNamesItsOwnProblem(t *testing.T) {
	home := t.TempDir()
	s := New(home, limits.NewReader(home, func() limits.Settings { return limits.Settings{} }))
	s.WithClaude(func(context.Context) ([]Window, string) { return nil, "unavailable" })
	now := time.Now()
	s.Snapshot(now)
	waitFinished(t, s)
	if row := claudeRow(t, s.Snapshot(now)); row.Problem != "unavailable" || len(row.Windows) != 0 || row.Source != "claude_statusline" {
		t.Fatalf("row=%+v", row)
	}
}
