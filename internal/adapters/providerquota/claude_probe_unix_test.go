//go:build !windows

package providerquota

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A stand-in claude: records its arguments and directory, answers the
// request it reads with the recorded reply, and never needs the network.
func TestProbeClaudeRunsAnIsolatedControlEndpoint(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	// The five-hour window has already reset; the weekly one has not.
	reply := strings.Replace(claudeUsageLine, `"2026-11-05T19:59:59.418968+00:00"`, `"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)+`"`, 1)
	reply = strings.Replace(reply, `"2026-11-02T15:49:59.418949+00:00"`, `"`+time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)+`"`, 1)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + seen + "\npwd >> " + seen + "\nread request\nprintf '%s\\n' \"$request\" >> " + seen +
		"\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\necho '" + reply + "'\nsleep 30\n"
	exe := filepath.Join(dir, "claude")
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	windows, problem := ProbeClaude(ctx, exe)
	if problem != "" || len(windows) != 1 || windows[0].Name != "7d" || windows[0].UsedPercent != 66 {
		t.Fatalf("windows=%+v problem=%q", windows, problem)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("the probe waited for the stand-in to exit instead of stopping it")
	}
	b, _ := os.ReadFile(seen)
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := append([]string{}, ClaudeUsageArgs...)
	if len(got) != len(want)+2 || strings.Join(got[:len(want)], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("arguments=%q", got)
	}
	if !strings.Contains(got[len(want)], "clawdline-claude-usage-") {
		t.Fatalf("ran in %q, not a scratch directory", got[len(want)])
	}
	if !strings.Contains(got[len(want)+1], `"subtype":"get_usage"`) || strings.Contains(string(b), "initialize") {
		t.Fatalf("request=%q", got[len(want)+1])
	}
	if _, err := os.Stat(got[len(want)]); !os.IsNotExist(err) {
		t.Fatal("scratch directory left behind")
	}
	if _, problem := ProbeClaude(ctx, ""); problem != "no_record" {
		t.Fatalf("no executable: %q", problem)
	}
}
