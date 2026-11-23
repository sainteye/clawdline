package capacity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func lowDiskEntry(t *testing.T) Entry {
	for _, e := range Register() {
		if e.Name == MachineDiskLow {
			return e
		}
	}
	t.Fatal("low disk row not registered")
	return Entry{}
}

func TestLowDiskNoticeNamesFreeSpaceAndLargestSourcesInBothLanguages(t *testing.T) {
	sources := []DiskSource{
		{What: "tmp", Bytes: 2 << 30, Known: true, Complete: false},
		{What: "gocache", Bytes: 75 << 30, Known: true, Complete: true},
		{What: "tasks", Known: false},
	}
	for lang, want := range map[string][]string{
		"en":      {"160.0 MiB", "Go cache 75", "/tmp at least 2", "task data unmeasured", "20"},
		"zh-Hant": {"160.0 MiB", "Go 快取 75", "/tmp 至少 2", "task 資料 無法量測"},
	} {
		title, body := LowDiskNoticeText(lang, 160<<20, 20<<30, sources)
		t.Logf("%s: %s / %s", lang, title, body)
		for _, w := range want {
			if !strings.Contains(title+" "+body, w) {
				t.Errorf("%s notice lacks %q: %s / %s", lang, w, title, body)
			}
		}
		if strings.Index(body, "75") > strings.Index(body, "/tmp") {
			t.Errorf("%s: sources not largest first: %s", lang, body)
		}
		if utf8.RuneCountInString(title) > 80 || utf8.RuneCountInString(body) > 500 {
			t.Errorf("%s notice exceeds push bounds", lang)
		}
	}
}

func TestLowDiskNoticesOncePerCooldownAndRecoversQuietly(t *testing.T) {
	e := lowDiskEntry(t)
	res := Resolved{Entry: e, Limit: e.Limit}
	tr := NewTracker()
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	low := Reading{Known: true, Used: e.Limit - (1 << 30), Failing: true, DiskFree: 1 << 30, HasDiskFree: true}
	ok := Reading{Known: true, Used: 0, DiskFree: 40 << 30, HasDiskFree: true}
	notices := 0
	count := func(evs []Event) {
		for _, ev := range evs {
			if ev.Kind == EventNotify {
				notices++
			}
		}
	}
	for i, r := range []Reading{ok, low, low, ok, low, ok} {
		_, evs := tr.Observe(res, r, at.Add(time.Duration(i)*time.Hour))
		count(evs)
	}
	if notices != 1 {
		t.Fatalf("%d notices within a day of flapping, want 1", notices)
	}
	_, evs := tr.Observe(res, low, at.Add(25*time.Hour))
	count(evs)
	if notices != 2 {
		t.Fatalf("no notice after the cooldown: %d", notices)
	}
}
