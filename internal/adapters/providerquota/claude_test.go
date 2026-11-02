package providerquota

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/claudeusage"
	"github.com/sainteye/clawdline/internal/adapters/limits"
)

// The footer's Claude row is the shared limits reading: Doctor's inspection
// does not ask Claude Code, and an answer from it is labelled as the CLI's.
func TestTheFooterReadsTheSharedClaudeReading(t *testing.T) {
	home := t.TempDir()
	var calls atomic.Int32
	local := limits.NewReader(home, nil).WithClaudeCode(func(context.Context) ([]claudeusage.Window, claudeusage.Outcome) {
		calls.Add(1)
		return []claudeusage.Window{{Name: "7d", UsedPercent: 66}}, ""
	})
	s := New(home, local)
	now := time.Now()
	if row := s.Inspect(now.Add(-time.Minute)).Providers[0]; row.ID != "claude" || row.Problem != "no_record" || calls.Load() != 0 {
		t.Fatalf("Doctor asked Claude Code: %+v calls=%d", row, calls.Load())
	}
	s.Snapshot(now)
	deadline := time.Now().Add(2 * time.Second)
	var row Row
	for time.Now().Before(deadline) {
		if row = s.Snapshot(now.Add(10 * time.Second)).Providers[0]; len(row.Windows) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if row.Source != "provider_cli" || row.Problem != "" || len(row.Windows) != 1 || row.Windows[0].UsedPercent != 66 || calls.Load() != 1 {
		t.Fatalf("row=%+v calls=%d", row, calls.Load())
	}
}
