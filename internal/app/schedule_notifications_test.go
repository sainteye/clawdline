package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

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
		{"Traditional project", "zh", scheduleNoticeProjectUnavailable, "broken.json", "排程檔 broken.json 指定的專案資料夾目前無法使用，已停用。"},
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
	if body != "無法讀取排程檔 "+id+".json，已停用。" {
		t.Fatalf("invalid schedule notice = %q", body)
	}
	if strings.Contains(body, "JSON object") || strings.Contains(body, "unexpected end") {
		t.Fatalf("invalid schedule notice exposed producer detail: %q", body)
	}
}
