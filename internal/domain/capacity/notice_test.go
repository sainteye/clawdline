package capacity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Every notice any registered row can say fits a push: eighty characters of
// title and five hundred of body, the bounds every push from this daemon
// keeps. Nothing cuts a notice to fit, so this is the only thing that holds it.
func TestEveryNoticeFitsAPush(t *testing.T) {
	full := t0.Add(9 * 24 * time.Hour)
	for _, e := range Register() {
		for _, state := range []State{Critical, Full, OK} {
			for _, used := range []int64{0, e.Limit - 1, e.Limit, e.Limit * 3} {
				title, body := NoticeText(e, state, used, e.Limit, full, time.Local)
				if title == "" || utf8.RuneCountInString(title) > 80 {
					t.Errorf("%s %s: title %q", e.Name, state, title)
				}
				if !strings.Contains(body, e.Name) || utf8.RuneCountInString(body) > 500 {
					t.Errorf("%s %s: body %q", e.Name, state, body)
				}
			}
		}
	}
}

// A notice says what, how full, when, and what to do — and the what-to-do
// follows the register line, not a guess: a row only a person may empty says
// so, and a row the daemon empties by its own rule does not send the person
// to do it.
func TestANoticeSaysWhatHowFullWhenAndWhatToDo(t *testing.T) {
	db := entryNamed(t, StoreDB)
	title, body := NoticeText(db, Critical, 1_020<<20, 1<<30, t0.Add(3*24*time.Hour), time.UTC)
	for _, want := range []string{"容量快滿了", "store.db", "99%"} {
		if !strings.Contains(title, want) {
			t.Errorf("title %q lacks %q", title, want)
		}
	}
	for _, want := range []string{"1020.0 MiB／1.0 GiB", "09-21 07:00", "由你決定", "capacity_exhausted"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}

	logs := entryNamed(t, LogDaemon)
	_, body = NoticeText(logs, Full, 10<<20, 10<<20, time.Time{}, time.UTC)
	if strings.Contains(body, "由你決定") || !strings.Contains(body, "最舊的分段會被刪掉") {
		t.Errorf("a row the daemon rotates on its own: %q", body)
	}
	if strings.Contains(body, "會滿。") {
		t.Errorf("a full row was told when it will be full: %q", body)
	}

	title, body = NoticeText(db, OK, 1<<20, 1<<30, time.Time{}, time.UTC)
	if !strings.Contains(title, "已恢復") || !strings.Contains(body, "不用做什麼") {
		t.Errorf("recovery: %q / %q", title, body)
	}
}

// A push goes to a person only for the rows whose register line says a person
// is told by notice.
func TestOnlyRowsThatTellAPersonPush(t *testing.T) {
	for _, e := range Register() {
		told := false
		for _, c := range e.Told {
			told = told || c == Notice
		}
		if Pushes(e) != told {
			t.Errorf("%s: pushes %v, told by notice %v", e.Name, Pushes(e), told)
		}
	}
	if !Pushes(entryNamed(t, StoreDB)) || Pushes(entryNamed(t, LeasesQueue)) {
		t.Error("store.db must push and leases.queue, answered to its sender, must not")
	}
}

// A restart keeps the one-notice-a-day promise: a tracker seeded with an
// earlier process's notice holds a second one back, and a row announced
// critical still owes its recovery. The control is the same readings with no
// seed, which notices again — the seed is what holds it.
func TestASeededTrackerKeepsTheDailyBudgetAcrossARestart(t *testing.T) {
	e := entryNamed(t, StoreDB)
	res := Resolved{Entry: e, Limit: 20}
	critical := Reading{Known: true, Used: 19}

	seeded := NewTracker()
	seeded.Seed(e.Name, t0, Critical)
	st, events := seeded.Observe(res, critical, t0.Add(time.Hour))
	if kinds(events) != EventState || st.Suppressed != 1 || !st.LastNoticeAt.Equal(t0) {
		t.Fatalf("seeded, still critical an hour on: %s, %+v", kinds(events), st)
	}

	control := NewTracker()
	_, events = control.Observe(res, critical, t0.Add(time.Hour))
	if !strings.Contains(kinds(events), EventNotify) {
		t.Fatalf("unseeded control: %s — the test cannot tell the seed from nothing", kinds(events))
	}

	owed := NewTracker()
	owed.Seed(e.Name, t0, Full)
	_, events = owed.Observe(res, Reading{Known: true, Used: 2}, t0.Add(25*time.Hour))
	notice := last(events, EventNotify)
	if notice == nil || notice.Payload["state"] != string(OK) {
		t.Fatalf("a row announced full and back to ok a day later owes its recovery: %s", kinds(events))
	}

	quiet := NewTracker()
	quiet.Seed(e.Name, t0, OK)
	if _, events = quiet.Observe(res, Reading{Known: true, Used: 2}, t0.Add(25*time.Hour)); len(events) != 0 {
		t.Fatalf("a row last announced as recovered owes nothing: %s", kinds(events))
	}

	// What the tracker has read itself is newer than any seed.
	live := NewTracker()
	live.Observe(res, critical, t0)
	live.Seed(e.Name, t0.Add(-48*time.Hour), OK)
	if st, _ := live.Observe(res, critical, t0.Add(time.Minute)); !st.LastNoticeAt.Equal(t0) {
		t.Fatalf("a seed overwrote a live reading: %+v", st)
	}
}

func entryNamed(t *testing.T, name string) Entry {
	t.Helper()
	for _, e := range Register() {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no row %s", name)
	return Entry{}
}

func last(events []Event, kind string) *Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}
