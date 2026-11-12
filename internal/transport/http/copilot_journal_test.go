package http

import (
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
