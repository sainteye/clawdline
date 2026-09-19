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

// The assistants route prints what decay did, in the Swift app's words: a
// `low` past its line is marked stale, an old `ok` becomes unknown with what it
// last was, and an exhausted window that has reset is unknown, not fine.
func TestDecaySaysWhatItDid(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	old := int64(1_000_000 - 7*3600)
	pct := func(v float64) *float64 { return &v }
	ahead, gone := int64(1_000_000+2*86_400+3*3600), int64(999_000)
	cases := []struct {
		name      string
		q         quota
		want      Availability
		stale     bool
		lastKnown Availability
		detail    string
	}{
		{"low, old", quota{availability: Low, observedAt: &old,
			windows: []Window{{Name: "7d", UsedPercent: pct(90), ResetsAt: &ahead}}},
			Low, true, "", "7d 90%"},
		{"ok, old, a window still open", quota{availability: OK, observedAt: &old,
			windows: []Window{{Name: "7d", UsedPercent: pct(40), ResetsAt: &ahead}}},
			Unknown, true, OK, "7d 40%; stale lower bound"},
		{"ok, old, nothing open", quota{availability: OK, observedAt: &old,
			windows: []Window{{Name: "5h", UsedPercent: pct(40), ResetsAt: &gone}}},
			Unknown, false, OK, "unknown; last known ok"},
		{"exhausted, reset since", quota{availability: Exhausted, observedAt: &old, resetsAt: &gone,
			windows: []Window{{Name: "5h", UsedPercent: pct(100), ResetsAt: &gone, Hit: true}}},
			Unknown, false, "", "unknown; the window that was exhausted has since reset"},
		{"exhausted, still", quota{availability: Exhausted, observedAt: &old, resetsAt: &ahead,
			windows: []Window{{Name: "7d", UsedPercent: pct(100), ResetsAt: &ahead, Hit: true}}},
			Exhausted, false, "", "7d 100%; resets in 2d3h"},
		{"nothing seen", quota{availability: Unknown, windows: []Window{}},
			Unknown, false, "", "no signal yet"},
	}
	for _, c := range cases {
		got := finished(c.q, false, now)
		if got.availability != c.want || got.stale != c.stale || got.lastKnown != c.lastKnown || got.detail != c.detail {
			t.Errorf("%s: %s stale=%v last=%q %q", c.name, got.availability, got.stale, got.lastKnown, got.detail)
		}
	}
	if d := detailOf(nil, Exhausted, true, "", now); d != "premium credits exhausted; no windows reported" {
		t.Errorf("credits with no windows: %q", d)
	}
	for secs, want := range map[float64]string{30: "1m", 3600: "1h", 5400: "1h30m", 86_400: "1d", 90_000: "1d1h"} {
		if got := formatDuration(secs); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", secs, got, want)
		}
	}
}

// One reading per assistant serves both the route and a session's windows,
// and an assistant is installed when its home is a directory — read here from
// a home that is not the person's.
func TestTheQuotaReadingIsTheWindowsReading(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, ".claude", "statusline-cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	body := fmt.Sprintf(`{"at":%d,"rate_limits":{"five_hour":{"used_percentage":91,"resets_at":%d}}}`,
		now.Unix()-60, now.Unix()+3600)
	if err := os.WriteFile(filepath.Join(cache, "rate-limits.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReader(home, func() Settings { return Settings{CodexHome: filepath.Join(home, "no-codex")} })
	claude := r.Quota("claude", now)
	if !claude.Installed || claude.Availability != Low || claude.ObservedAt == nil || names(claude.Windows) != "5h=91," {
		t.Fatalf("claude: %+v", claude)
	}
	if m := r.Machine("claude", now); names(m.Windows) != names(claude.Windows) || m.At == nil || *m.At != *claude.ObservedAt {
		t.Fatalf("the session's windows %s differ from the route's %s", names(m.Windows), names(claude.Windows))
	}
	codex := r.Quota("codex", now)
	if codex.Installed || codex.Availability != Unknown || codex.Detail != "no signal yet" || len(codex.Windows) != 0 {
		t.Fatalf("codex with no home: %+v", codex)
	}
}
