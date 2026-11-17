package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
)

func TestACallJournalIsAppendedOnTheMachineADayAFile(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	body := `{"call":"call-0001","entries":[{"at":1,"kind":"heard","text":"TimeTree 那個怎樣"},{"at":2,"kind":"tool","text":"get_session_summary","tool":{"outcome":"done","ms":800}},{"at":3,"kind":"said","text":"` + strings.Repeat("字", maxCopilotJournalTextRunes+10) + `"}]}`
	rec := copilotCall(t, s, s.copilotJournalRoute, http.MethodPost, "/v1/voice/copilot-journal", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	raw, err := os.ReadFile(filepath.Join(s.copilotJournalDir(), time.Now().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"call":"call-0001"`) || !strings.Contains(lines[0], "TimeTree") || !strings.Contains(lines[1], `"outcome":"done"`) {
		t.Fatalf("lines: %v", lines)
	}
	if n := len([]rune(lines[2])); n > maxCopilotJournalTextRunes+100 {
		t.Fatalf("a long line was kept whole: %d runes", n)
	}
	if info, _ := os.Stat(filepath.Join(s.copilotJournalDir(), time.Now().Format("2006-01-02")+".jsonl")); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestACallJournalKeepsOnlyItsDaysAndRefusesWhatIsNotALine(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	dir := s.copilotJournalDir()
	_ = os.MkdirAll(dir, 0o700)
	old := time.Now().Add(-maxCopilotJournalAge-24*time.Hour).Format("2006-01-02") + ".jsonl"
	_ = os.WriteFile(filepath.Join(dir, old), []byte("{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600)
	for body, code := range map[string]string{
		`{"call":"x","entries":[]}`: "invalid_body",
		`{"call":"call-0001","entries":[{"at":1,"kind":"shout","text":""}]}`: "journal_bad_kind",
	} {
		if rec := copilotCall(t, s, s.copilotJournalRoute, http.MethodPost, "/v1/voice/copilot-journal", body); !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: %s", body, rec.Body)
		}
	}
	copilotCall(t, s, s.copilotJournalRoute, http.MethodPost, "/v1/voice/copilot-journal", `{"call":"call-0001","entries":[{"at":1,"kind":"start","text":"call"}]}`)
	if _, err := os.Stat(filepath.Join(dir, old)); !os.IsNotExist(err) {
		t.Fatal("a journal older than the kept days survived")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("a file that is no journal was removed")
	}
}

func TestDailyVoiceCostsCountOneLatestSnapshotAndKeepMissingCallsUnknown(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	now := time.Now().UTC()
	start := now.UnixMilli()
	entry := func(call, kind, text string, at int64, u *copilotUsageSnapshot) []byte {
		raw, _ := json.Marshal(u)
		if u == nil {
			raw = nil
		}
		b, _ := json.Marshal(struct {
			Call string `json:"call"`
			copilotJournalEntry
		}{call, copilotJournalEntry{At: at, Kind: kind, Text: text, Tool: raw}})
		return append(b, '\n')
	}
	u := copilotUsageSnapshot{Started: start, Engine: "live", Model: "gpt-live-1", USD: 0.05, Parts: map[string]float64{"voice": 0.05}, Seconds: 60, Priced: true}
	var lines []byte
	lines = append(lines, entry("call-current", "start", "private conversation", start, nil)...)
	lines = append(lines, entry("call-current", "notice", "usage_snapshot", start+1, &u)...)
	u.Seq = 1
	u.USD = 0.1
	u.Parts["voice"] = 0.1
	u.Seconds = 120
	lines = append(lines, entry("call-current", "notice", "usage_snapshot", start+3, &u)...)
	lines = append(lines, entry("call-current", "notice", "usage_snapshot", start+3, &u)...)
	u.Seq = 0
	u.USD = 0.075
	u.Parts["voice"] = 0.075
	lines = append(lines, entry("call-current", "notice", "usage_snapshot", start+200000, &u)...)
	lines = append(lines, entry("call-legacy", "start", "no usage", start, nil)...)
	if err := s.appendCopilotJournal(now, lines); err != nil {
		t.Fatal(err)
	}
	got := s.copilotUsageHistory(now)
	if got.Error != "" || len(got.Days) != 1 {
		t.Fatalf("%+v", got)
	}
	day := got.Days[0]
	if day.Calls != 2 || day.Missing != 1 || day.Partial != 1 || day.USD != 0.1 || day.Seconds != 120 {
		t.Fatalf("%+v", day)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "private conversation") {
		t.Fatal("usage exposed conversation")
	}
}

func TestVoiceAnalysisUsageIsCountedSeparatelyFromCallDuration(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	u := copilotChatUsage{Prompt: 10000, Completion: 1000}
	u.Details.Cached = 2000
	s.recordCopilotChatUsage("gpt-4.1-mini", u)
	got := s.copilotUsageHistory(time.Now())
	if len(got.Days) != 1 {
		t.Fatalf("%+v", got)
	}
	day := got.Days[0]
	if day.Calls != 0 || day.USD != 0.005 || day.Parts["analysis"] != day.USD || day.Input != 10000 || day.Output != 1000 || day.Cached != 2000 {
		t.Fatalf("%+v", day)
	}
}

func TestBrokenCostRecordsAreVisibleAndNotAnEmptySuccessfulHistory(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	if err := s.appendCopilotJournal(time.Now(), []byte("{broken\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.copilotUsageHistory(time.Now()); got.Error != "voice_usage_incomplete" {
		t.Fatalf("%+v", got)
	}
}
