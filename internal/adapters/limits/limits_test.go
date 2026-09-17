package limits

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func names(ws []Window) string {
	out := ""
	for _, w := range ws {
		p := "?"
		if w.UsedPercent != nil {
			p = fmt.Sprint(*w.UsedPercent)
		}
		out += fmt.Sprintf("%s=%s,", w.Name, p)
	}
	return out
}

// A window whose reset has passed says nothing about the window now open, so
// it is not shown at all rather than shown with the old number.
func TestAClaudeWindowThatHasResetIsDropped(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	data := []byte(`{"at": 999990, "session_id": "s", "rate_limits": {
		"five_hour": {"used_percentage": 40, "resets_at": 999000},
		"seven_day": {"used_percentage": 94.5, "resets_at": "1100000"}}}`)
	got := ClaudeCache(data, now)
	if names(got.Windows) != "7d=94.5," || got.At == nil || *got.At != 999990 {
		t.Fatalf("got %s at %v", names(got.Windows), got.At)
	}
}

// The night a weekly window ran out, Codex's next records named no window at
// all. Taking the newest record would answer "unknown" for an account at
// 100%; the named reading behind it is the answer, with the unnamed one kept.
func TestACodexCreditsRecordDoesNotHideTheNamedWindow(t *testing.T) {
	rollout := []byte(`{"timestamp":"2026-09-16T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":97,"window_minutes":10080,"resets_at":4000000000},"secondary":null}}}
{"timestamp":"2026-09-16T11:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"premium","primary":null,"secondary":null,"credits":{"has_credits":false}}}}
{"timestamp":"2026-09-16T11:0`)
	got := CodexRollout(rollout)
	if names(got.Windows) != "7d=97," || got.Depleted == nil || got.Depleted.LimitID == nil ||
		*got.Depleted.LimitID != "premium" || got.Depleted.HasCredits == nil || *got.Depleted.HasCredits {
		t.Fatalf("got %s depleted %+v", names(got.Windows), got.Depleted)
	}

	home := t.TempDir()
	day := filepath.Join(home, ".codex", "sessions", "2026", "09", "16")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(day, "rollout-a.jsonl"), rollout, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "")
	r := NewReader(home, nil)
	machine := r.Machine("codex", time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC))
	if names(machine.Windows) != "7d=97," || machine.At == nil {
		t.Fatalf("machine reading %s at %v", names(machine.Windows), machine.At)
	}
	// Spent credits after a nearly full window is exhausted, observed at the
	// newer of the two records.
	if want := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC).Unix(); *machine.At != want {
		t.Fatalf("observed at %d, want %d", *machine.At, want)
	}
}

// An old "fine" promises nothing now: past its staleness line it keeps only
// the windows whose reset is still ahead, and with none it is unknown.
func TestAnOldOkReadingDecays(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	pct := 10.0
	old := int64(1_000_000 - 16*60)
	ahead, gone := int64(1_000_500), int64(999_999)
	q := quota{availability: OK, observedAt: &old, windows: []Window{
		{Name: "5h", UsedPercent: &pct, ResetsAt: &gone},
		{Name: "7d", UsedPercent: &pct, ResetsAt: &ahead},
	}}
	// The tightest is the first of equals, a 5h window: fifteen minutes.
	got := decayed(q, now)
	if got.availability != Unknown || names(got.windows) != "7d=10," || got.observedAt == nil {
		t.Fatalf("got %s %s %v", got.availability, names(got.windows), got.observedAt)
	}
	q.windows = q.windows[:1]
	got = decayed(q, now)
	if len(got.windows) != 0 || got.observedAt != nil {
		t.Fatalf("with nothing live: %s %v", names(got.windows), got.observedAt)
	}
	// A reading inside the line is untouched.
	fresh := int64(1_000_000 - 60)
	q.observedAt = &fresh
	if got = decayed(q, now); got.availability != OK || len(got.windows) != 1 {
		t.Fatalf("fresh reading changed: %s %s", got.availability, names(got.windows))
	}
}
