//go:build darwin || linux

package browserlaunch

import (
	"testing"
)

func TestParseOrphanedTestBrowsers(t *testing.T) {
	sampleOutput := `  PID  PPID COMMAND
  100     1 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/tmp/extproto-123
  101   500 /Users/test/Library/Caches/ms-playwright/chromium-1254/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing --user-data-dir=/tmp/extproto-123
  102     1 /Users/test/Library/Caches/ms-playwright/chromium-1254/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing --user-data-dir=/Users/test/permanent-profile
  103     1 /Users/test/Library/Caches/ms-playwright/chromium-1254/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing --headless=new --user-data-dir=/var/folders/v7/847/T/extproto-FyyMUN/profile about:blank
  104     1 /Users/test/Library/Caches/ms-playwright/chromium_headless_shell-1169/chrome-mac/headless_shell --headless=old --user-data-dir=/tmp/worker-privacy-abc/prof about:blank
`
	orphans := parseOrphanedTestBrowsers([]byte(sampleOutput))
	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphans, got %d: %+v", len(orphans), orphans)
	}

	if orphans[0].PID != 103 || orphans[0].PPID != 1 {
		t.Errorf("orphan 0 mismatch: %+v", orphans[0])
	}
	if orphans[0].TempDir != "/var/folders/v7/847/T/extproto-FyyMUN/profile" {
		t.Errorf("orphan 0 temp dir mismatch: %s", orphans[0].TempDir)
	}

	if orphans[1].PID != 104 || orphans[1].PPID != 1 {
		t.Errorf("orphan 1 mismatch: %+v", orphans[1])
	}
	if orphans[1].TempDir != "/tmp/worker-privacy-abc/prof" {
		t.Errorf("orphan 1 temp dir mismatch: %s", orphans[1].TempDir)
	}
}

func TestExtractUserDataDir(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"chrome --user-data-dir=/tmp/foo about:blank", "/tmp/foo"},
		{"chrome --user-data-dir=/var/folders/foo/prof --headless", "/var/folders/foo/prof"},
		{"chrome without flag", ""},
	}
	for _, c := range cases {
		if got := extractUserDataDir(c.cmd); got != c.want {
			t.Errorf("cmd %q: got %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestIsDisposableTempProfile(t *testing.T) {
	if !isDisposableTempProfile("/tmp/extproto-xyz") {
		t.Error("expected /tmp/extproto-xyz to be disposable")
	}
	if !isDisposableTempProfile("/var/folders/v7/847/T/extproto-FyyMUN/profile") {
		t.Error("expected /var/folders/.../extproto-FyyMUN/profile to be disposable")
	}
	if isDisposableTempProfile("/Users/example/.config/clawdline-next/chrome-profile") {
		t.Error("clawdline permanent profile must NOT be disposable")
	}
	if isDisposableTempProfile("/Users/example/Library/Application Support/Google/Chrome") {
		t.Error("user Chrome profile must NOT be disposable")
	}
}
