package limits

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/claudeusage"
)

func askingReader(t *testing.T, record string) (*Reader, *atomic.Int32, chan struct{}) {
	t.Helper()
	home := t.TempDir()
	if record != "" {
		dir := filepath.Join(home, ".claude", "statusline-cache")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rate-limits.json"), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	release := make(chan struct{})
	r := NewReader(home, nil).WithClaudeCode(func(context.Context) ([]claudeusage.Window, claudeusage.Outcome) {
		calls.Add(1)
		<-release
		reset := time.Now().Add(time.Hour).Unix()
		return []claudeusage.Window{{Name: "5h", UsedPercent: 3, ResetsAt: &reset}, {Name: "7d", UsedPercent: 66}}, ""
	})
	return r, &calls, release
}

func askSettled(t *testing.T, r *Reader) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		busy := r.asked.busy
		r.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the ask did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func percents(q Quota) []string {
	out := []string{}
	for _, w := range q.Windows {
		out = append(out, w.Name+"="+strconv.FormatFloat(*w.UsedPercent, 'f', 0, 64))
	}
	return out
}

// Every consumer reads Quota or Machine, so the answer Claude Code gave is the
// one dispatch, Session info, the corner and the footer all see.
func TestWithoutAStatusLineEveryReaderSeesClaudeCodesAnswer(t *testing.T) {
	r, calls, release := askingReader(t, "")
	now := time.Now()
	// Observed a minute earlier, so its five-second cache does not answer the reads below.
	if q := r.Observe("claude", now.Add(-time.Minute)); q.Reason != NoRecord || calls.Load() != 0 {
		t.Fatalf("observing asked Claude Code: %+v calls=%d", q, calls.Load())
	}
	first := r.Quota("claude", now)
	if first.Reason != NoRecord || first.FromClaudeCode {
		t.Fatalf("a reading waited for the ask: %+v", first)
	}
	for i := 0; i < 10; i++ {
		r.Quota("claude", now.Add(time.Duration(i)*cacheFor))
	}
	close(release)
	askSettled(t, r)
	if calls.Load() != 1 {
		t.Fatalf("asked %d times while one ask ran", calls.Load())
	}
	q := r.Quota("claude", now)
	m := r.Machine("claude", now)
	if !q.FromClaudeCode || q.Availability != OK || q.Reason != "" || q.Stale || q.ObservedAt == nil ||
		strings.Join(percents(q), ",") != "5h=3,7d=66" || len(m.Windows) != 2 || *m.Windows[1].UsedPercent != 66 ||
		!strings.Contains(q.Detail, "asked Claude Code") {
		t.Fatalf("quota=%+v machine=%+v", q, m)
	}
	r.Quota("claude", now.Add(time.Minute))
	askSettled(t, r)
	if calls.Load() != 1 {
		t.Fatal("asked again inside the floor")
	}
	r.Quota("claude", now.Add(claudeusage.RefreshSecondsLimit*time.Second))
	askSettled(t, r)
	if calls.Load() != 2 {
		t.Fatalf("not asked again after the floor: %d", calls.Load())
	}
}

func TestAFreshStatusLineIsNeitherReplacedNorAsked(t *testing.T) {
	now := time.Now()
	reset := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	r, calls, release := askingReader(t, `{"at":`+strconv.FormatInt(now.Unix(), 10)+
		`,"rate_limits":{"five_hour":{"used_percentage":2,"resets_at":`+reset+`},"seven_day":{"used_percentage":65,"resets_at":`+reset+`}}}`)
	defer close(release)
	q := r.Quota("claude", now)
	if calls.Load() != 0 || q.FromClaudeCode || strings.Join(percents(q), ",") != "5h=2,7d=65" {
		t.Fatalf("fresh status line replaced: %+v calls=%d", q, calls.Load())
	}
}

func TestAnOldStatusLineGivesWayToANewerAnswer(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	reset := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	r, calls, release := askingReader(t, `{"at":`+strconv.FormatInt(old.Unix(), 10)+
		`,"rate_limits":{"five_hour":{"used_percentage":90,"resets_at":`+reset+`},"seven_day":{"used_percentage":10,"resets_at":`+reset+`}}}`)
	q := r.Quota("claude", now)
	r.mu.Lock()
	asking := r.asked.busy
	r.mu.Unlock()
	if !q.Stale || q.FromClaudeCode || !asking {
		t.Fatalf("old record: %+v asking=%v", q, asking)
	}
	close(release)
	askSettled(t, r)
	if q := r.Quota("claude", now); calls.Load() != 1 || !q.FromClaudeCode || q.Stale || strings.Join(percents(q), ",") != "5h=3,7d=66" {
		t.Fatalf("newer answer not taken: %+v", q)
	}
}

func TestAFailedAskKeepsTheReasonAndSaysWhatItFound(t *testing.T) {
	home := t.TempDir()
	r := NewReader(home, nil).WithClaudeCode(func(context.Context) ([]claudeusage.Window, claudeusage.Outcome) {
		return nil, claudeusage.NoPlan
	})
	now := time.Now()
	r.Quota("claude", now)
	askSettled(t, r)
	q := r.Quota("claude", now)
	if q.Reason != NoRecord || q.FromClaudeCode || len(q.Windows) != 0 || !strings.Contains(q.Detail, "no plan windows") {
		t.Fatalf("%+v", q)
	}
}

func TestCodexNeverAsksClaudeCode(t *testing.T) {
	r, calls, release := askingReader(t, "")
	defer close(release)
	r.Quota("codex", time.Now())
	if calls.Load() != 0 {
		t.Fatal("a Codex reading asked Claude Code")
	}
}
