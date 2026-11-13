package browserbridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost answers every polled command with answer, until ctx ends.
func fakeHost(ctx context.Context, b *Bridge, answer func(Command) Result) {
	go func() {
		for ctx.Err() == nil {
			cmds := b.Poll(ctx, HostInfo{Version: "test"})
			var out []Result
			for _, c := range cmds {
				out = append(out, answer(c))
			}
			b.Deliver(out)
		}
	}()
}

func connected(t *testing.T) (*Bridge, context.Context) {
	t.Helper()
	b := New(nil)
	b.pollWait = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// One poll makes the host count as connected.
	b.Poll(ctx, HostInfo{Version: "test"})
	return b, ctx
}

func TestNoHostIsATypedRefusal(t *testing.T) {
	b := New(nil)
	_, err := b.Do(context.Background(), "c1", "tabs", nil)
	if r, ok := IsRefusal(err); !ok || r != HostMissing {
		t.Fatalf("err = %v, want %s", err, HostMissing)
	}
}

func TestOpenThenUseTheSameTab(t *testing.T) {
	b, ctx := connected(t)
	var mu sync.Mutex
	var seen []Command
	fakeHost(ctx, b, func(c Command) Result {
		mu.Lock()
		seen = append(seen, c)
		mu.Unlock()
		return Result{ID: c.ID, OK: true, Tab: 7, URL: "https://github.com/x", Value: json.RawMessage(`{"ok":1}`)}
	})
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://github.com/x"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Do(ctx, "c1", "click", json.RawMessage(`{"target":"Sign in"}`)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[1].Tab != 7 {
		t.Fatalf("the click went to tab %d, want 7 (commands %+v)", seen[len(seen)-1].Tab, seen)
	}
}

func TestAnotherSessionsTabIsRefused(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 3} })
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := b.Do(ctx, "c2", "click", json.RawMessage(`{"tab":3,"target":"x"}`))
	if r, _ := IsRefusal(err); r != OtherSessionsTab {
		t.Fatalf("err = %v, want %s", err, OtherSessionsTab)
	}
}

func TestPersonControlRefusesTheAgentAndIsRecorded(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 4} })
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`)); err != nil {
		t.Fatal(err)
	}
	b.SetPersonControl(4, true)
	_, err := b.Do(ctx, "c1", "screenshot", nil)
	if r, _ := IsRefusal(err); r != PersonControlling {
		t.Fatalf("err = %v, want %s", err, PersonControlling)
	}
	rows, _ := b.Records("c1", 10)
	if len(rows) == 0 || rows[0].Error != string(PersonControlling) {
		t.Fatalf("newest audit row = %+v, want the refusal", rows)
	}
	b.SetPersonControl(4, false)
	if _, err := b.Do(ctx, "c1", "screenshot", nil); err != nil {
		t.Fatalf("after hand-back: %v", err)
	}
}

func TestStopFailsQueuedAndLaterCommands(t *testing.T) {
	b, _ := connected(t)
	done := make(chan error, 1)
	go func() {
		_, err := b.Do(context.Background(), "c1", "tabs", nil)
		done <- err
	}()
	for b.Status().Pending == 0 {
		time.Sleep(time.Millisecond)
	}
	b.SetStopped(true)
	if r, _ := IsRefusal(<-done); r != Stopped {
		t.Fatal("a queued command survived the stop")
	}
	if _, err := b.Do(context.Background(), "c1", "tabs", nil); err != Stopped {
		t.Fatalf("err = %v, want %s", err, Stopped)
	}
}

func TestQueueIsBounded(t *testing.T) {
	b, _ := connected(t)
	b.timeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < MaxPendingCommands; i++ {
		go b.Do(ctx, "c1", "tabs", nil)
	}
	for b.Status().Pending < MaxPendingCommands {
		time.Sleep(time.Millisecond)
	}
	if _, err := b.Do(ctx, "c1", "tabs", nil); err != QueueFull {
		t.Fatalf("err = %v, want %s", err, QueueFull)
	}
}

func TestTimeoutWithoutAnAnswer(t *testing.T) {
	b, _ := connected(t)
	b.timeout = 30 * time.Millisecond
	if _, err := b.Do(context.Background(), "c1", "tabs", nil); err != TimedOut {
		t.Fatalf("err = %v, want %s", err, TimedOut)
	}
	if b.Status().Pending != 0 {
		t.Fatal("a timed-out command stayed queued for the host")
	}
}

func TestPasswordTextIsNotAudited(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result {
		return Result{ID: c.ID, OK: true, Tab: 2, URL: "https://github.com/login", Password: c.Op == "type"}
	})
	b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://github.com/login"}`))
	if _, err := b.Do(ctx, "c1", "type", json.RawMessage(`{"text":"hunter2"}`)); err != nil {
		t.Fatal(err)
	}
	rows, _ := b.Records("c1", 1)
	if rows[0].Text != "" || rows[0].TextLen != 7 || !rows[0].Password {
		t.Fatalf("password row = %+v", rows[0])
	}
}

func TestEachNewSiteIsAnnouncedOncePerSession(t *testing.T) {
	var got []NewSite
	b := New(func(n NewSite) { got = append(got, n) })
	b.pollWait = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Poll(ctx, HostInfo{})
	url := "https://github.com/a"
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 1, URL: url} })
	b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://github.com/a"}`))
	url = "https://docs.github.com/b"
	b.Do(ctx, "c1", "read", nil)
	url = "https://example.com"
	b.Do(ctx, "c1", "read", nil)
	if len(got) != 2 || got[0].Site != "github.com" || got[1].Site != "example.com" {
		t.Fatalf("announcements = %+v", got)
	}
}

func TestAuditIsBounded(t *testing.T) {
	b := New(nil)
	for i := 0; i < MaxAuditRows+5; i++ {
		b.record("c1", "tabs", 0, "", Result{OK: true})
	}
	rows, pruned := b.Records("", MaxAuditRows+10)
	if len(rows) != MaxAuditRows || pruned != 5 {
		t.Fatalf("rows %d pruned %d", len(rows), pruned)
	}
}

func TestSite(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/x":        "github.com",
		"https://docs.github.com/y":   "github.com",
		"https://www.bbc.co.uk/news":  "bbc.co.uk",
		"https://shop.example.com.tw": "example.com.tw",
		"http://127.0.0.1:7791/anim":  "127.0.0.1",
		"chrome://extensions":         "",
	} {
		if got := Site(in); got != want {
			t.Errorf("Site(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenNeedsAWebAddress(t *testing.T) {
	b, ctx := connected(t)
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"file:///etc/passwd"}`)); err != BadRequest {
		t.Fatalf("err = %v", err)
	}
}

func TestTheAuditSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.record("c1", "type", 3, "王小明", Result{OK: true, Tab: 3, URL: "https://example.com/a"})
	b.record("c1", "type", 3, "hunter2", Result{OK: true, Tab: 3, URL: "https://example.com/a", Password: true})
	again, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := again.Records("c1", 10)
	if len(rows) != 2 || rows[0].Text != "" || rows[0].TextLen != 7 || rows[1].Text != "王小明" {
		t.Fatalf("after reopening: %+v", rows)
	}
	info, err := os.Stat(filepath.Join(dir, JournalFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal file: %v %v", err, info.Mode())
	}
}

func TestTheJournalIsCompactedAndCountsWhatItDropped(t *testing.T) {
	dir := t.TempDir()
	b, _ := Open(dir, nil)
	total := 2*MaxAuditRows + 10
	for i := 0; i < total; i++ {
		b.record("c1", "tabs", 0, "", Result{OK: true})
	}
	again, _ := Open(dir, nil)
	rows, pruned := again.Records("", MaxAuditRows+10)
	if len(rows) != MaxAuditRows || pruned != total-MaxAuditRows {
		t.Fatalf("rows %d pruned %d, want %d and %d", len(rows), pruned, MaxAuditRows, total-MaxAuditRows)
	}
	data, _ := os.ReadFile(filepath.Join(dir, JournalFile))
	if lines := strings.Count(string(data), "\n"); lines > 2*MaxAuditRows {
		t.Fatalf("the journal has %d lines; it was never compacted", lines)
	}
	if b.JournalErr() != nil {
		t.Fatal(b.JournalErr())
	}
}

func TestLongTypedTextIsCutInTheAudit(t *testing.T) {
	b := New(nil)
	long := strings.Repeat("字", MaxAuditTextRunes+5)
	b.record("c1", "type", 1, long, Result{OK: true})
	rows, _ := b.Records("c1", 1)
	if len([]rune(rows[0].Text)) != MaxAuditTextRunes || !rows[0].TextCut || rows[0].TextLen != MaxAuditTextRunes+5 {
		t.Fatalf("row: cut %v len %d", rows[0].TextCut, rows[0].TextLen)
	}
}

func TestThePersonActsOnlyOnATabTheyTookOver(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: c.Tab, Value: json.RawMessage(`{}`)} })
	if _, err := b.PersonDo(ctx, "screenshot", 5, nil); err != nil {
		t.Fatalf("looking needs no takeover: %v", err)
	}
	if _, err := b.PersonDo(ctx, "click", 5, json.RawMessage(`{"x":1,"y":2}`)); err != NotTakenOver {
		t.Fatalf("err = %v, want %s", err, NotTakenOver)
	}
	b.SetPersonControl(5, true)
	if _, err := b.PersonDo(ctx, "type", 5, json.RawMessage(`{"text":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	b.SetPersonControl(5, false)
	rows, _ := b.Records("", 10)
	var ops []string
	for _, r := range rows {
		ops = append(ops, r.Op)
		if r.Text != "" {
			t.Fatalf("the person's typing was recorded: %+v", r)
		}
	}
	if strings.Join(ops, ",") != "handback,takeover" {
		t.Fatalf("audit ops = %v", ops)
	}
}

func TestViewportChangesRespectTheControlOwnerAndOnlyAcceptPresets(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: c.Tab, Value: json.RawMessage(`{}`)} })
	args := json.RawMessage(`{"mode":"mobile"}`)
	if _, err := b.PersonDo(ctx, "viewport", 5, args); err != NotTakenOver {
		t.Fatalf("before takeover: %v", err)
	}
	b.SetPersonControl(5, true)
	if _, err := b.PersonDo(ctx, "viewport", 5, args); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Do(ctx, "c1", "viewport", json.RawMessage(`{"tab":5,"mode":"desktop"}`)); err != PersonControlling {
		t.Fatalf("agent during takeover: %v", err)
	}
	b.SetPersonControl(5, false)
	if _, err := b.Do(ctx, "c1", "viewport", json.RawMessage(`{"tab":5,"mode":"desktop"}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []json.RawMessage{nil, json.RawMessage(`{"mode":"unknown"}`), json.RawMessage(`{"width":999999}`)} {
		if _, err := b.Do(ctx, "c1", "viewport", bad); err != BadRequest {
			t.Fatalf("bad agent preset %s: %v", bad, err)
		}
		if _, err := b.PersonDo(ctx, "viewport", 5, bad); err != BadRequest {
			t.Fatalf("bad person preset %s: %v", bad, err)
		}
	}
}

func TestASecretHandoffIsNeverAudited(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 2, URL: "https://example.com/login"} })
	b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com/login"}`))
	// Typed into an ordinary field, as a one-time code often is.
	if _, err := b.Do(ctx, "c1", "type", json.RawMessage(`{"text":"483920","secret":true}`)); err != nil {
		t.Fatal(err)
	}
	rows, _ := b.Records("c1", 1)
	if rows[0].Text != "" || rows[0].TextLen != 6 || !rows[0].Password {
		t.Fatalf("secret row = %+v", rows[0])
	}
}

func TestAMutedSessionIsNotAnnounced(t *testing.T) {
	var got []NewSite
	b := New(func(n NewSite) { got = append(got, n) })
	b.SetMuted("c1", true)
	b.record("c1", "open", 1, "", Result{OK: true, Tab: 1, URL: "https://github.com/a"})
	b.record("c2", "open", 2, "", Result{OK: true, Tab: 2, URL: "https://github.com/a"})
	if len(got) != 1 || got[0].Conversation != "c2" {
		t.Fatalf("announcements = %+v", got)
	}
	if st := b.Status(); len(st.Muted) != 1 || st.Muted[0] != "c1" {
		t.Fatalf("muted = %v", st.Muted)
	}
}

func TestTabOwnershipTakeoverAndMuteSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.pollWait = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Poll(ctx, HostInfo{})
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 41, URL: "https://example.com"} })
	if _, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`)); err != nil {
		t.Fatal(err)
	}
	b.SetPersonControl(41, true)
	b.SetMuted("c1", true)
	again, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := again.Status()
	if len(st.Tabs) != 1 || st.Tabs[0].Tab != 41 || st.Tabs[0].Conversation != "c1" || !st.Tabs[0].Person || !st.Tabs[0].Current {
		t.Fatalf("tabs after restart = %+v", st.Tabs)
	}
	if len(st.Muted) != 1 {
		t.Fatalf("muted after restart = %v", st.Muted)
	}
	again.Forget(41)
	if len(again.Status().Tabs) != 0 {
		t.Fatal("a forgotten tab is still listed")
	}
}

func TestRecentActivityKeepsBoundedPictures(t *testing.T) {
	b := New(nil)
	for i := 0; i < MaxActivityThumbs+3; i++ {
		b.record("c1", "click", 1, "", Result{OK: true, Tab: 1, URL: "https://example.com", Thumb: "AAAA"})
	}
	b.record("c1", "click", 1, "", Result{OK: true, Tab: 1, Thumb: strings.Repeat("A", MaxThumbBytes+1)})
	if got := b.RecentActivity("c1", 100); len(got) != MaxActivityThumbs || got[0].Thumb != "" || got[1].Thumb != "AAAA" {
		t.Fatalf("activity %d", len(got))
	}
	if len(b.RecentActivity("c2", 10)) != 0 {
		t.Fatal("another Session's actions leaked")
	}
}

func TestRecentActivityIncludesActionsWithoutPictures(t *testing.T) {
	b := New(nil)
	b.record("c1", "type", 1, "secret", Result{OK: true, Password: true})
	b.record("c1", "click", 1, "", Result{Error: string(PersonControlling)})
	got := b.RecentActivity("c1", MaxActivityThumbs)
	if len(got) != 2 || got[0].Error != string(PersonControlling) || got[1].Text != "" || got[1].TextLen != 6 || got[1].Thumb != "" {
		t.Fatalf("missing or unsafe activity: %+v", got)
	}
}

func TestTheAuditNarrowsBySessionSiteAndTime(t *testing.T) {
	b := New(nil)
	at := time.Unix(1_000_000, 0)
	b.now = func() time.Time { return at }
	b.record("c1", "open", 1, "", Result{OK: true, Tab: 1, URL: "https://github.com/a"})
	at = at.Add(time.Hour)
	b.record("c1", "open", 1, "", Result{OK: true, Tab: 1, URL: "https://www.example.com/b"})
	b.record("c2", "open", 2, "", Result{OK: true, Tab: 2, URL: "https://github.com/c"})
	count := func(f AuditFilter) int { rows, _ := b.Find(f, 10); return len(rows) }
	for _, tc := range []struct {
		f    AuditFilter
		want int
	}{
		{AuditFilter{}, 3},
		{AuditFilter{Conversation: "c1"}, 2},
		{AuditFilter{Site: "github.com"}, 2},
		{AuditFilter{Conversation: "c1", Site: "github.com"}, 1},
		{AuditFilter{Since: at.Unix()}, 2},
		{AuditFilter{Since: at.Unix(), Site: "github.com"}, 1},
		{AuditFilter{Since: at.Unix() + 1}, 0},
	} {
		if got := count(tc.f); got != tc.want {
			t.Fatalf("%+v: %d rows, want %d", tc.f, got, tc.want)
		}
	}
}

func TestRefusedSecretInputKeepsOnlyItsLength(t *testing.T) {
	b := New(nil)
	b.host.LastPoll = b.now().Unix()
	b.owner[1] = "c1"
	b.current["c1"] = 1
	b.person[1] = true
	_, err := b.Do(context.Background(), "c1", "type", json.RawMessage(`{"text":"private-value","secret":true}`))
	if err != PersonControlling {
		t.Fatalf("refusal: %v", err)
	}
	rows, _ := b.Records("c1", 1)
	if len(rows) != 1 || rows[0].Text != "" || rows[0].TextLen != 13 {
		t.Fatalf("refused secret was retained: %+v", rows)
	}
}

func TestTakeoverDropsQueuedAgentInput(t *testing.T) {
	b, ctx := connected(t)
	b.owner[1] = "c1"
	b.current["c1"] = 1
	done := make(chan error, 1)
	go func() { _, err := b.Do(ctx, "c1", "click", nil); done <- err }()
	for b.Status().Pending == 0 {
		time.Sleep(time.Millisecond)
	}
	b.SetPersonControl(1, true)
	if commands := b.Poll(ctx, HostInfo{Version: "test"}); len(commands) != 0 {
		t.Fatalf("queued input survived takeover: %+v", commands)
	}
	if err := <-done; err != PersonControlling {
		t.Fatalf("queued refusal: %v", err)
	}
}

func TestFrameInFlightCannotCrossATakeoverAndHandback(t *testing.T) {
	b, ctx := connected(t)
	b.owner[1] = "c1"
	b.current["c1"] = 1
	started, release := make(chan struct{}), make(chan struct{})
	fakeHost(ctx, b, func(c Command) Result {
		close(started)
		<-release
		return Result{ID: c.ID, OK: true, Tab: 1, Value: json.RawMessage(`{"data":"private-frame"}`), Thumb: "AAAA"}
	})
	done := make(chan error, 1)
	go func() {
		value, err := b.Do(ctx, "c1", "screenshot", nil)
		if len(value) != 0 {
			done <- ExtensionErrorForTest{}
			return
		}
		done <- err
	}()
	<-started
	b.SetPersonControl(1, true)
	b.SetPersonControl(1, false)
	close(release)
	if err := <-done; err != PersonControlling {
		t.Fatalf("late frame crossed control change: %v", err)
	}
}

type ExtensionErrorForTest struct{}

func (ExtensionErrorForTest) Error() string { return "late frame was returned" }

func TestEmergencyStopSurvivesRestartUntilThePersonResumes(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.SetStopped(true)
	again, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Status().Stopped {
		t.Fatal("restart forgot emergency stop")
	}
	again.SetStopped(false)
	resumed, err := Open(dir, nil)
	if err != nil || resumed.Status().Stopped {
		t.Fatalf("resume was not retained: %v", err)
	}
}

func TestAFieldReadIsThePersonsOwnAndNeverRecorded(t *testing.T) {
	b, ctx := connected(t)
	fakeHost(ctx, b, func(c Command) Result {
		return Result{ID: c.ID, OK: true, Tab: c.Tab, Password: true, Value: json.RawMessage(`{"tab":5,"kind":"text","value":"synthetic-field-text"}`)}
	})
	if _, err := b.PersonDo(ctx, "field", 5, json.RawMessage(`{}`)); err != NotTakenOver {
		t.Fatalf("before takeover: %v, want %s", err, NotTakenOver)
	}
	b.SetPersonControl(5, true)
	value, err := b.PersonDo(ctx, "field", 5, json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(value), "synthetic-field-text") {
		t.Fatalf("person read = %s, %v", value, err)
	}
	b.SetPersonControl(5, false)
	if _, err := b.Do(ctx, "c1", "field", json.RawMessage(`{"tab":5}`)); err != BadRequest {
		t.Fatalf("an agent read the field: %v", err)
	}
	rows, _ := b.Records("", 10)
	for _, r := range rows {
		if r.Op == "field" || strings.Contains(r.Text+r.Target+r.URL, "synthetic-field-text") {
			t.Fatalf("a field read was recorded: %+v", r)
		}
	}
}
