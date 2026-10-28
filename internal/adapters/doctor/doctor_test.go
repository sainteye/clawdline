package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/providerquota"
)

func sample(at int64, issue string) providerquota.Snapshot {
	return providerquota.Snapshot{At: at, Providers: []providerquota.Row{{ID: "claude", Source: "private-path", Label: "private-account", Problem: issue}}}
}

func TestProjectionRetainsRealZeroAndExcludesUntrustedStrings(t *testing.T) {
	p, ok := Project(providerquota.Row{ID: "claude", Label: "private-account", Source: "secret-path", Problem: "Bearer secret", Windows: []providerquota.Window{{Name: "secret-model", UsedPercent: 0}}})
	b, _ := json.Marshal(p)
	if !ok || p.UsedPercent == nil || *p.UsedPercent != 0 || p.Problem != "unavailable" || strings.Contains(string(b), "secret") || strings.Contains(string(b), "private") {
		t.Fatalf("unsafe or lost reading: %s", b)
	}
	if _, ok := Project(providerquota.Row{ID: "private-provider"}); ok {
		t.Fatal("unknown provider entered the report")
	}
}

func TestHistorySurvivesRestartDeduplicatesAndExplainsRecovery(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Unix()
	r := New(dir)
	r.Record(sample(now, "no_record"))
	r.Record(sample(now+1, "no_record"))
	s := sample(now+2, "")
	s.Providers[0].Windows = []providerquota.Window{{UsedPercent: 0}}
	r.Record(s)
	h, c := New(dir).Evidence(time.Unix(now+3, 0))
	if len(h) != 2 || h[0].Provider.Problem != "no_record" || h[1].Provider.UsedPercent == nil || c.State != "ok" || c.Through == nil || *c.Through != now+2 {
		t.Fatalf("history=%+v coverage=%+v", h, c)
	}
	assertHistoryPermissions(t, filepath.Join(dir, "doctor", "history.json"))
}

func TestHistoryBoundsAndExpiryCountMissingEvidence(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)
	now := time.Now().Unix()
	for i := 0; i < HistoryRowsLimit+5; i++ {
		issue := "no_record"
		if i%2 == 0 {
			issue = "unreadable"
		}
		r.Record(sample(now+int64(i), issue))
	}
	h, c := New(dir).Evidence(time.Unix(now+100, 0))
	if len(h) != HistoryRowsLimit || c.Dropped != 5 {
		t.Fatalf("len=%d coverage=%+v", len(h), c)
	}
	h, c = r.Evidence(time.Unix(now+HistorySecondsLimit+100, 0))
	if len(h) != 0 || c.Dropped != HistoryRowsLimit+5 {
		t.Fatalf("expiry: %d %+v", len(h), c)
	}
}

func TestCorruptAndUnwritableHistoryAreNotSuccessfulEmptyEvidence(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "doctor"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doctor", "history.json"), []byte(`{"events":[{"at":1,"provider":{"id":"secret"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	h, c := New(dir).Evidence(time.Now())
	if len(h) != 0 || c.State != "unreadable" {
		t.Fatalf("corruption disappeared: %+v %+v", h, c)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r := New(blocked)
	r.Record(sample(time.Now().Unix(), "no_record"))
	h, c = r.Evidence(time.Now())
	if len(h) != 1 || c.State != "write_failed" {
		t.Fatalf("write failure disappeared: %+v %+v", h, c)
	}
}

func TestPersistedHistoryCannotSmuggleExtraFieldsIntoReports(t *testing.T) {
	dir := t.TempDir()
	r := New(dir)
	now := time.Now()
	r.Record(sample(now.Unix(), "no_record"))
	path := filepath.Join(dir, "doctor", "history.json")
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), `"problem":"no_record"`, `"problem":"no_record","token":"secret"`, 1))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	h, c := New(dir).Evidence(now)
	if len(h) != 0 || c.State != "unreadable" {
		t.Fatalf("unknown fields accepted: %+v %+v", h, c)
	}
}
