package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

func TestScheduleNotificationLanguageIsProductPreferenceOnly(t *testing.T) {
	dir := t.TempDir()
	book := ScheduleBook{Broker: &orchestrator.Broker{Dir: dir}}
	if got := book.notificationLanguage(); got != "en" {
		t.Fatalf("unset product language = %q", got)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"language": "zh-Hant", "voice_language": "ja"}); err != nil {
		t.Fatal(err)
	}
	if got := book.notificationLanguage(); got != "en" {
		t.Fatalf("agent/voice setting changed notification language to %q", got)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"product_language": "de"}); err != nil {
		t.Fatal(err)
	}
	if got := book.notificationLanguage(); got != "de" {
		t.Fatalf("selected notification language = %q", got)
	}
}

func TestScheduleNoticeBaselineAndCoreLanguagesRemainComplete(t *testing.T) {
	kinds := []scheduleNoticeKind{scheduleNoticeInvalidJSON, scheduleNoticeInvalidSchema,
		scheduleNoticeProjectUnavailable, scheduleNoticeMissed, scheduleNoticeRefused}
	for _, language := range nextconfig.ProductLanguages {
		translated, total := scheduleNoticeCoverage(language)
		t.Logf("schedule notice coverage %s: %d/%d", language, translated, total)
		templates, ok := scheduleNotices[language]
		if !ok || !validScheduleNoticeCatalog(language) {
			t.Errorf("%s catalog is invalid", language)
			continue
		}
		if language == "en" || language == "zh-Hant" {
			if translated != total {
				t.Errorf("%s core catalog coverage = %d/%d", language, translated, total)
			}
		}
		for _, kind := range kinds {
			if templates[kind] == "" {
				t.Errorf("%s misses initial baseline key %s", language, kind)
			}
			value := scheduleNotice(language, kind, "sample")
			if value == "" || strings.Contains(value, "%!") {
				t.Errorf("%s %s rendered %q", language, kind, value)
			}
		}
	}
}

func TestInvalidExistingScheduleTranslationFallsBackAsAWhole(t *testing.T) {
	kind := scheduleNoticeRefused
	template := scheduleNotices["fr"][kind]
	scheduleNotices["fr"][kind] = "Impossible de démarrer (%d)."
	t.Cleanup(func() { scheduleNotices["fr"][kind] = template })
	if translated, total := scheduleNoticeCoverage("fr"); translated != 0 || total == 0 {
		t.Fatalf("invalid catalog coverage = %d/%d", translated, total)
	}
	if got := scheduleNotice("fr", scheduleNoticeMissed, ""); got != scheduleNotice("en", scheduleNoticeMissed, "") {
		t.Fatalf("another key from an invalid catalog stayed translated: %q", got)
	}
}

func TestMissingSecondaryScheduleNoticeKeyFallsBackToSameEnglishCode(t *testing.T) {
	kind := scheduleNoticeRefused
	template := scheduleNotices["fr"][kind]
	delete(scheduleNotices["fr"], kind)
	t.Cleanup(func() { scheduleNotices["fr"][kind] = template })
	if translated, total := scheduleNoticeCoverage("fr"); translated != total-1 {
		t.Fatalf("coverage after missing key = %d/%d", translated, total)
	}
	if got := scheduleNotice("fr", kind, "over_capacity"); got != scheduleNotice("en", kind, "over_capacity") {
		t.Fatalf("missing French key = %q", got)
	}
}

func TestScheduleNotificationsUseTheSelectedLanguageWithoutProducerDetail(t *testing.T) {
	const producer = "project_dir must be an absolute path to a directory"
	cases := []struct {
		name     string
		language string
		kind     scheduleNoticeKind
		value    string
		want     string
	}{
		{"Traditional invalid JSON", "zh-Hant", scheduleNoticeInvalidJSON, "broken.json", "無法讀取排程檔 broken.json，已停用。"},
		{"Traditional schema", "zh-TW", scheduleNoticeInvalidSchema, "broken.json", "排程檔 broken.json 的內容不符合格式，已停用。"},
		{"Ambiguous Chinese", "zh", scheduleNoticeProjectUnavailable, "broken.json", "Schedule file broken.json names a project directory that is currently unavailable and was disabled."},
		{"Traditional missed", "zh-Hant", scheduleNoticeMissed, "", "排程執行已超過可補跑的時間，這次沒有啟動。"},
		{"Traditional refused", "zh-Hant", scheduleNoticeRefused, "over_capacity", "排程這次無法啟動（over_capacity）。"},
		{"English invalid JSON", "en", scheduleNoticeInvalidJSON, "broken.json", "Schedule file broken.json could not be read and was disabled."},
		{"English missed", "en-US", scheduleNoticeMissed, "", "The scheduled run missed its catch-up window and did not start."},
		{"English refused", "en", scheduleNoticeRefused, "over_capacity", "The scheduled run could not start (over_capacity)."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scheduleNotice(c.language, c.kind, c.value)
			if got != c.want {
				t.Fatalf("scheduleNotice() = %q, want %q", got, c.want)
			}
			if strings.Contains(got, producer) {
				t.Fatalf("notification exposed producer detail: %q", got)
			}
		})
	}
}

func TestInvalidScheduleNoticeKindUsesTheProducerClassification(t *testing.T) {
	cases := map[string]scheduleNoticeKind{
		"unreadable_json":     scheduleNoticeInvalidJSON,
		"schema":              scheduleNoticeInvalidSchema,
		"project_unavailable": scheduleNoticeProjectUnavailable,
		"future_kind":         scheduleNoticeInvalidSchema,
	}
	for kind, want := range cases {
		if got := invalidScheduleNoticeKind(kind); got != want {
			t.Errorf("invalidScheduleNoticeKind(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestLoadingAnInvalidSchedulePushesOnlyCataloguedCopy(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const id = "5c000005-0000-4000-8000-000000000005"
	if err := st.CreateScheduleFile(context.Background(), id, []byte(`{"unfinished":`), time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	var body string
	book := ScheduleBook{Store: st, Notify: func(_ context.Context, _, notice, _ string) { body = notice }}
	if _, err := book.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body != "Schedule file "+id+".json could not be read and was disabled." {
		t.Fatalf("invalid schedule notice = %q", body)
	}
	if strings.Contains(body, "JSON object") || strings.Contains(body, "unexpected end") {
		t.Fatalf("invalid schedule notice exposed producer detail: %q", body)
	}
}
