package limits

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
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
	fresh := int64(1_000_000 - 30)
	cases := []struct {
		name      string
		q         quota
		want      Availability
		stale     bool
		lastKnown Availability
		reason    Reason
		detail    string
	}{
		{"low, old", quota{availability: Low, observedAt: &old,
			windows: []Window{{Name: "7d", UsedPercent: pct(90), ResetsAt: &ahead}}},
			Low, true, "", "", "7d 90%; read 7h ago, past the 6h it was good for"},
		{"ok, old, a window still open", quota{availability: OK, observedAt: &old,
			windows: []Window{{Name: "7d", UsedPercent: pct(40), ResetsAt: &ahead}}},
			Unknown, true, OK, TooOld, "7d 40%; read 7h ago, past the 6h it was good for; unknown now, a stale lower bound"},
		{"ok, old, nothing open", quota{availability: OK, observedAt: &old,
			windows: []Window{{Name: "5h", UsedPercent: pct(40), ResetsAt: &gone}}},
			Unknown, false, OK, TooOld, "unknown: the last reading was taken 7h ago, past the 15m it was good for; last known ok"},
		{"exhausted, reset since", quota{availability: Exhausted, observedAt: &old, resetsAt: &gone,
			windows: []Window{{Name: "5h", UsedPercent: pct(100), ResetsAt: &gone, Hit: true}}},
			Unknown, false, "", TooOld, "unknown: the window that was exhausted has since reset"},
		{"exhausted, still, and old", quota{availability: Exhausted, observedAt: &old, resetsAt: &ahead,
			windows: []Window{{Name: "7d", UsedPercent: pct(100), ResetsAt: &ahead, Hit: true}}},
			Exhausted, true, "", "", "7d 100%; resets in 2d3h; read 7h ago, past the 6h it was good for"},
		{"exhausted, still, and fresh", quota{availability: Exhausted, observedAt: &fresh, resetsAt: &ahead,
			windows: []Window{{Name: "7d", UsedPercent: pct(100), ResetsAt: &ahead, Hit: true}}},
			Exhausted, false, "", "", "7d 100%; resets in 2d3h; read 30s ago"},
		{"nothing seen", quota{availability: Unknown, windows: []Window{},
			reason: NoRecord, note: "~/.codex/sessions is not there"},
			Unknown, false, "", NoRecord,
			"unknown: nothing has been written here to read; ~/.codex/sessions is not there"},
	}
	for _, c := range cases {
		got := finished(c.q, false, now)
		if got.availability != c.want || got.stale != c.stale || got.lastKnown != c.lastKnown ||
			got.reason != c.reason || got.detail != c.detail {
			t.Errorf("%s: %s stale=%v last=%q reason=%q %q",
				c.name, got.availability, got.stale, got.lastKnown, got.reason, got.detail)
		}
	}
	credits := quota{availability: Exhausted, windows: []Window{}}
	if d := detailOf(credits, true, now); d != "premium credits exhausted; no windows reported" {
		t.Errorf("credits with no windows: %q", d)
	}
	for secs, want := range map[float64]string{30: "1m", 3600: "1h", 5400: "1h30m", 86_400: "1d", 90_000: "1d1h"} {
		if got := formatDuration(secs); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", secs, got, want)
		}
	}
	// A reading nine seconds old is nine seconds old: formatDuration rounds
	// anything under a minute up to one, and an age is where that would read
	// as a lie.
	if got := ageText(9); got != "9s" {
		t.Errorf("ageText(9) = %q", got)
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
	if codex.Installed || codex.Availability != Unknown || codex.Reason != NoRecord || len(codex.Windows) != 0 {
		t.Fatalf("codex with no home: %+v", codex)
	}
	// The sentence names the directory that is not there, with the home
	// written `~`: what a person checks next, not somebody's home path.
	if codex.Detail != "unknown: nothing has been written here to read; ~/no-codex/sessions is not there; "+
		"Codex writes a rollout there the first time it runs" {
		t.Fatalf("codex with no home: %q", codex.Detail)
	}
}

// stamp is a rate-limits.json the status line could have written.
func statusCache(t *testing.T, dir string, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rate-limits.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// rollout writes one Codex rollout into the day folder of when it happened.
func rollout(t *testing.T, root string, day time.Time, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-"+name+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// `unknown` is four different facts, and each of them has to arrive as itself:
// nothing written here, a record naming no window, a record that could not be
// read, and a reading that has aged out. A root reading "unknown" and not
// knowing which one cannot tell "dispatch once and it will have a signal" from
// "go and look at that file".
func TestEachKindOfUnknownArrivesAsItself(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	ahead := now.Add(3 * time.Hour).Unix()

	cases := []struct {
		name      string
		assistant string
		// build returns the settings to read under home.
		build  func(t *testing.T, home string) Settings
		reason Reason
		says   string
	}{
		{
			name: "claude has never written a cache", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				return Settings{StatusDir: filepath.Join(home, "statusline-cache")}
			},
			reason: NoRecord, says: "is not there",
		},
		{
			name: "claude's cache cannot be read", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				dir := filepath.Join(home, "statusline-cache")
				path := statusCache(t, dir, `{"at":1,"rate_limits":{}}`)
				if err := os.Chmod(path, 0o000); err != nil {
					t.Fatal(err)
				}
				return Settings{StatusDir: dir}
			},
			reason: Unreadable, says: "could not be read: permission denied",
		},
		{
			name: "claude's cache is half written", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				dir := filepath.Join(home, "statusline-cache")
				statusCache(t, dir, `{"at":1,"rate_limi`)
				return Settings{StatusDir: dir}
			},
			reason: Unreadable, says: "is not a JSON object",
		},
		{
			name: "claude's cache names no window", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				dir := filepath.Join(home, "statusline-cache")
				statusCache(t, dir, fmt.Sprintf(
					`{"at":%d,"rate_limits":{"five_hour":{"resets_at":%d}}}`,
					now.Add(-2*time.Minute).Unix(), ahead))
				return Settings{StatusDir: dir}
			},
			reason: NoReading, says: "names no window with a used percentage",
		},
		{
			name: "every window claude named has reset", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				dir := filepath.Join(home, "statusline-cache")
				statusCache(t, dir, fmt.Sprintf(
					`{"at":%d,"rate_limits":{"five_hour":{"used_percentage":40,"resets_at":%d}}}`,
					now.Add(-2*time.Hour).Unix(), now.Add(-time.Minute).Unix()))
				return Settings{StatusDir: dir}
			},
			reason: NoReading, says: "has reset since it was written",
		},
		{
			name: "claude's last reading has aged out", assistant: "claude",
			build: func(t *testing.T, home string) Settings {
				dir := filepath.Join(home, "statusline-cache")
				statusCache(t, dir, fmt.Sprintf(
					`{"at":%d,"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":%d}}}`,
					now.Add(-4*time.Hour).Unix(), ahead))
				return Settings{StatusDir: dir}
			},
			reason: TooOld, says: "past the 15m it was good for",
		},
		{
			name: "codex has never run here", assistant: "codex",
			build: func(t *testing.T, home string) Settings {
				return Settings{CodexHome: filepath.Join(home, ".codex")}
			},
			reason: NoRecord, says: "the first time it runs",
		},
		{
			// The day folders are Codex's, not this daemon's: it may leave one
			// behind holding anything but a rollout. Looking at the newest two
			// is not the same as looking at the last two days — a Codex that
			// last ran in June still has its June folder read — so "there is
			// no rollout here" is its own sentence.
			name: "codex's newest day folders hold no rollout", assistant: "codex",
			build: func(t *testing.T, home string) Settings {
				codex := filepath.Join(home, ".codex")
				root := filepath.Join(codex, "sessions")
				for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
					dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				return Settings{CodexHome: codex}
			},
			reason: NoRecord, says: "no Codex rollout in the newest 2 day folders",
		},
		{
			name: "codex's rollouts cannot be read", assistant: "codex",
			build: func(t *testing.T, home string) Settings {
				codex := filepath.Join(home, ".codex")
				path := rollout(t, filepath.Join(codex, "sessions"), now, "a", "{}\n")
				if err := os.Chmod(path, 0o000); err != nil {
					t.Fatal(err)
				}
				return Settings{CodexHome: codex}
			},
			reason: Unreadable, says: "could not be read: permission denied",
		},
		{
			name: "codex ran and no turn was answered", assistant: "codex",
			build: func(t *testing.T, home string) Settings {
				codex := filepath.Join(home, ".codex")
				rollout(t, filepath.Join(codex, "sessions"), now, "a",
					`{"timestamp":"2026-09-20T11:00:00Z","type":"event_msg","payload":{"type":"user_message"}}`+"\n")
				return Settings{CodexHome: codex}
			},
			reason: NoReading, says: "no rate_limits in the newest 1 rollout under",
		},
		{
			name: "codex's last reading has aged out", assistant: "codex",
			build: func(t *testing.T, home string) Settings {
				codex := filepath.Join(home, ".codex")
				rollout(t, filepath.Join(codex, "sessions"), now, "a", fmt.Sprintf(
					`{"timestamp":"2026-09-19T12:00:00Z","type":"event_msg","payload":{"type":"token_count",`+
						`"rate_limits":{"primary":{"used_percent":8,"window_minutes":10080,"resets_at":%d}}}}`+"\n",
					now.AddDate(0, 0, 5).Unix()))
				return Settings{CodexHome: codex}
			},
			reason: TooOld, says: "past the 6h it was good for",
		},
	}

	said := map[string]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			settings := c.build(t, home)
			q := NewReader(home, func() Settings { return settings }).Quota(c.assistant, now)
			if q.Availability != Unknown {
				t.Fatalf("availability %s: %+v", q.Availability, q)
			}
			if q.Reason != c.reason {
				t.Errorf("reason %q, want %q (%q)", q.Reason, c.reason, q.Detail)
			}
			if !strings.Contains(q.Detail, c.says) {
				t.Errorf("detail %q does not say %q", q.Detail, c.says)
			}
			if before, seen := said[q.Detail]; seen {
				t.Errorf("says the same as %q: %q", before, q.Detail)
			}
			said[q.Detail] = c.name
		})
	}
	if len(said) != len(cases) {
		t.Errorf("%d sentences for %d kinds of nothing", len(said), len(cases))
	}
}

// A percentage without its age reads as a percentage from now. Every reading
// carries how old it is and the line that age is judged against, and the line
// is the shortest window's, not the widest one's: an account read four hours
// ago is stale for its five-hour window however fresh that is for a week.
func TestAReadingCarriesItsAgeAndTheLineIsTheShortestWindows(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	week, hours := now.AddDate(0, 0, 4).Unix(), now.Add(3*time.Hour).Unix()

	// A weekly window on its own is good for six hours.
	if got := freshFor([]Window{{Name: "7d", UsedPercent: pct(62), ResetsAt: &week}}); got != 6*3600 {
		t.Errorf("a weekly window alone is good for %ds", got)
	}
	// Beside a five-hour window it is good for fifteen minutes, although the
	// weekly one is the tighter of the two by usage: 62% of a week is the
	// window that will bite first, and 19% of five hours is the number that
	// goes wrong first.
	both := []Window{
		{Name: "5h", UsedPercent: pct(19), ResetsAt: &hours},
		{Name: "7d", UsedPercent: pct(62), ResetsAt: &week},
	}
	if got := freshFor(both); got != 15*60 {
		t.Errorf("with a five-hour window the reading is good for %ds", got)
	}
	if w := tightest(both); w == nil || w.Name != "7d" {
		t.Errorf("the window that resets_at follows is still the tightest by usage: %+v", w)
	}

	// Forty minutes old: past the five-hour window's line, so the reading is
	// marked and its age is on it, and `ok` is taken away because "fine" is
	// the one verdict an old reading turns into a lie.
	old := now.Add(-40 * time.Minute).Unix()
	q := finished(quota{availability: OK, observedAt: &old, windows: both}, false, now)
	if !q.stale || q.availability != Unknown || q.lastKnown != OK || q.reason != TooOld {
		t.Fatalf("forty minutes old: %+v", q)
	}
	if want := "5h 19%, 7d 62%; read 40m ago, past the 15m it was good for; unknown now, a stale lower bound"; q.detail != want {
		t.Errorf("detail %q, want %q", q.detail, want)
	}

	// Ten minutes old: inside the line, and the age is still on it.
	fresh := now.Add(-10 * time.Minute).Unix()
	q = finished(quota{availability: OK, observedAt: &fresh, windows: both}, false, now)
	if q.stale || q.availability != OK || q.freshFor != 15*60 {
		t.Fatalf("ten minutes old: %+v", q)
	}
	if want := "5h 19%, 7d 62%; read 10m ago"; q.detail != want {
		t.Errorf("detail %q, want %q", q.detail, want)
	}
}

// Nothing here wakes up to take a reading: one is taken when somebody asks,
// and held only long enough that two callers asking at once are one read of
// the files. What is held is bounded and let go oldest first, which is the
// capacity register's cache.assistant_quota.
func TestAReadingIsTakenOnlyWhenAskedForAndTheCacheIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	asked := 0
	r := NewReader(home, func() Settings {
		asked++
		return Settings{StatusDir: filepath.Join(home, "statusline-cache"),
			CodexHome: filepath.Join(home, ".codex")}
	})
	if asked != 0 {
		t.Fatalf("building a reader read something: %d", asked)
	}
	if got := r.Reading(); !got.Known || got.Used != 0 {
		t.Fatalf("a reader that has been asked nothing holds %+v", got)
	}
	for i := 0; i < 5; i++ {
		r.Quota("claude", now)
	}
	if asked != 1 {
		t.Errorf("five asks inside the hold read the files %d times", asked)
	}
	// Past the hold the files are read again — because somebody asked, not
	// because a clock went off.
	r.Quota("claude", now.Add(cacheFor+time.Second))
	if asked != 2 {
		t.Errorf("an ask past the hold read the files %d times", asked)
	}
	r.Quota("codex", now)
	if got := r.Reading(); got.Used != 2 || got.WindowSeconds != int64(cacheFor/time.Second) {
		t.Fatalf("two assistants read: %+v", got)
	}

	// The bound is the register's, and what it lets go of is the reading
	// nobody has asked for in longest.
	if quotaCacheLimit != capacity.Default(capacity.CacheAssistantQuota) || quotaCacheLimit <= 0 {
		t.Fatalf("the cache limit is not the registered row's: %d", quotaCacheLimit)
	}
	small := NewReader(home, func() Settings { return Settings{} })
	small.hold("a", cached{until: now.Add(cacheFor), used: now})
	small.hold("b", cached{until: now.Add(cacheFor), used: now.Add(time.Second)})
	prior := quotaCacheLimit
	quotaCacheLimit = 2
	t.Cleanup(func() { quotaCacheLimit = prior })
	small.hold("c", cached{until: now.Add(cacheFor), used: now.Add(2 * time.Second)})
	if _, kept := small.cache["a"]; kept || len(small.cache) != 2 {
		t.Errorf("the reading asked for longest ago was kept: %v", small.cache)
	}
	if got := small.Reading(); got.Counters.Evicted != 1 || got.Counters.LastActionAt.IsZero() {
		t.Errorf("letting one go was not counted: %+v", got)
	}
}

// What "Codex is unknown" was on this machine on 2026-09-20: not an assistant
// that had never run — five rollouts from the day before said 98% of a weekly
// window — but a weekly window that reset at 16:09 the previous afternoon with
// nothing run since, so the newest record describes a week that has ended.
//
// The old answer to that was "no signal yet", which reads as "Codex is not set
// up here" and sent somebody looking for a missing install. `no_reading` with
// the record's age on it says the true thing: there is a record, it is twenty
// hours old, and the period it is about is over.
func TestAWindowThatResetSinceTheLastRunIsNotAMissingCodex(t *testing.T) {
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	ran := now.Add(-20 * time.Hour)
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	rollout(t, filepath.Join(codex, "sessions"), ran, "a", fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count",`+
			`"rate_limits":{"limit_id":"codex","primary":{"used_percent":98,"window_minutes":10080,"resets_at":%d}}}}`+"\n",
		ran.Format(time.RFC3339), now.Add(-17*time.Hour).Unix()))

	q := NewReader(home, func() Settings { return Settings{CodexHome: codex} }).Quota("codex", now)
	if q.Availability != Unknown || q.Reason != NoReading {
		t.Fatalf("%s/%s: %+v", q.Availability, q.Reason, q)
	}
	if !q.Installed {
		t.Error("an assistant with a sessions tree is installed")
	}
	// The record's own age is on the answer, and it is past the line, so the
	// answer says so rather than leaving the silence undated.
	if q.ObservedAt == nil || *q.ObservedAt != ran.Unix() || !q.Stale || q.FreshFor != 6*3600 {
		t.Errorf("observedAt=%v stale=%v freshFor=%d", q.ObservedAt, q.Stale, q.FreshFor)
	}
	want := "unknown: a record is there and names no window; every window the newest Codex rollout " +
		"names has reset since it was written; read 20h ago, past the 6h it was good for"
	if q.Detail != want {
		t.Errorf("detail\n got %q\nwant %q", q.Detail, want)
	}
}
