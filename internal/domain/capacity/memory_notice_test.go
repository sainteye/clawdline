package capacity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func memoryEntriesRow(t *testing.T) Entry {
	t.Helper()
	for _, e := range Register() {
		if e.Name == MemoryEntries {
			return e
		}
	}
	t.Fatal("memory.entries is not registered")
	return Entry{}
}

func notices(events []Event) (n int) {
	for _, e := range events {
		if e.Kind == EventNotify {
			n++
		}
	}
	return n
}

// A Project's shared memory refuses new entries when it is full and the
// daemon never removes one on its own, so the person hears about it while
// there is still room to decide: at 85% of the live bound, not at 95%.
func TestMemoryEntriesTellsThePersonAtEightyFivePercent(t *testing.T) {
	e := memoryEntriesRow(t)
	if !Pushes(e) {
		t.Fatalf("memory.entries tells %v; a person is never pushed", e.Told)
	}
	res := Resolved{Entry: e, Limit: e.Limit}
	tr := NewTracker()
	now := t0
	early := (e.Limit*85 + 99) / 100
	st, events := tr.Observe(res, Reading{Known: true, Used: early - 1}, now)
	if st.State != OK || notices(events) != 0 {
		t.Fatalf("%d of %d: %s, %d notices", early-1, e.Limit, st.State, notices(events))
	}
	now = now.Add(time.Hour)
	st, events = tr.Observe(res, Reading{Known: true, Used: early}, now)
	if st.State != Warn || notices(events) != 1 {
		t.Fatalf("%d of %d: %s, %d notices; want one notice on entering warn", early, e.Limit, st.State, notices(events))
	}
	// The same day it goes on to critical: the one-a-day budget holds.
	now = now.Add(time.Hour)
	_, events = tr.Observe(res, Reading{Known: true, Used: e.Limit - 1}, now)
	if notices(events) != 0 {
		t.Fatalf("a second notice within a day: %+v", events)
	}
}

// A row that does not opt in is still told only at critical and full.
func TestWarnStaysQuietForOrdinaryRows(t *testing.T) {
	e := Entry{Name: "test.rows", Class: Evidence, Unit: Rows, Limit: 100, AtLimit: Refuse, Told: []Channel{Notice}, EvictedBy: Person}
	tr := NewTracker()
	_, events := tr.Observe(Resolved{Entry: e, Limit: 100}, Reading{Known: true, Used: 85}, t0)
	if notices(events) != 0 {
		t.Fatalf("an ordinary row was announced at warn: %+v", events)
	}
}

// The notice says how many entries the Project's own maintenance already
// suggests releasing, so the person knows there is something to review.
func TestMemoryNoticeNamesTheSuggestions(t *testing.T) {
	e := memoryEntriesRow(t)
	for _, language := range []string{"en", "zh-Hant"} {
		_, body := NoticeTextReading(language, e, Warn, Reading{Known: true, Used: 990, Reclaimable: 142, HasReclaimable: true}, e.Limit, time.Time{}, time.UTC)
		if !strings.Contains(body, "142") || !strings.Contains(body, "memory dream") {
			t.Errorf("%s: %q does not name the 142 suggestions or how to read them", language, body)
		}
		if utf8.RuneCountInString(body) > 500 {
			t.Errorf("%s: %d characters", language, utf8.RuneCountInString(body))
		}
		_, plain := NoticeTextReading(language, e, Warn, Reading{Known: true, Used: 990}, e.Limit, time.Time{}, time.UTC)
		if strings.Contains(plain, "memory dream") {
			t.Errorf("%s: a reading without suggestions still mentions them: %q", language, plain)
		}
	}
}
