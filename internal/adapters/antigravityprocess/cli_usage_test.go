//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestModernACPUsageNeverWritesModelPrompt(t *testing.T) {
	c, dir := fake(t, "usage-modern", nil)
	ready(t, c)
	requireError(t, func() error { _, err := c.Usage(context.Background()); return err }(), "usage_unsupported", NotWritten)
	if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/new"}) {
		t.Fatal("quota wrote a model prompt", got)
	}
}

func TestCLIUsageIsOneBoundedNativeReport(t *testing.T) {
	for _, mode := range []string{"report", "overflow", "stall", "failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			settings(t, dir, `{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`)
			exe := filepath.Join(dir, "cli")
			body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GEMINI_HOME/usage-argv\"\n"
			switch mode {
			case "report":
				body += "printf 'Gemini Models\\tWeekly Limit Remaining\\t73%%\\t2031-01-02T03:04:05Z\\n'\n"
			case "overflow":
				body += "printf '%01000d' 0\n"
			case "stall":
				body += "sleep 60\n"
			case "failed":
				body += "echo 'SECRET provider error' >&2\nexit 2\n"
			}
			if err := os.WriteFile(exe, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			opts := Options{Executable: exe, AccountDir: dir, WorkingDir: dir, Args: []string{"acp"}, MaxFrameBytes: 512, CallTimeout: 2 * time.Second}
			text, err := ReadCLIUsage(context.Background(), opts)
			if mode == "report" {
				if err != nil || !strings.Contains(text, "73%") {
					t.Fatal(text, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("unbounded or leaked report", text, err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "usage-argv"))
			if err != nil || string(raw) != "--settings\n{\"useG1Credits\":false}\n--print\n/usage\n" {
				t.Fatal("not the fixed CLI command", string(raw), err)
			}
			os.Remove(filepath.Join(dir, "usage-argv"))
			opts.Args = []string{"acp", "--app-data-dir", "another-profile"}
			requireError(t, func() error { _, err := ReadCLIUsage(context.Background(), opts); return err }(), "usage_unsupported", NotWritten)
			if _, err := os.Stat(filepath.Join(dir, "usage-argv")); !os.IsNotExist(err) {
				t.Fatal("custom profile started default-account reader")
			}
		})
	}
}

func TestPublicACPReceivesExplicitToolApprovalPolicy(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "review", true: "full"}[full], func(t *testing.T) {
			dir := t.TempDir()
			body := `{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`
			if full {
				body = `{"useG1Credits":false,"auth":{"type":"oauth-personal"},"toolPermission":"always-proceed"}`
			}
			settings(t, dir, body)
			exe := filepath.Join(dir, "peer")
			if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GEMINI_HOME/argv\"\ncat >/dev/null\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c, err := Start(context.Background(), Options{Executable: exe, Args: []string{"acp", "--app-data-dir", "antigravity-acp"}, AccountDir: dir, WorkingDir: dir, MaxFrameBytes: 4096, CallTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			deadline := time.Now().Add(time.Second)
			var raw []byte
			for time.Now().Before(deadline) {
				raw, _ = os.ReadFile(filepath.Join(dir, "argv"))
				if strings.Contains(string(raw), "antigravity-acp\n") {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if got := strings.Contains(string(raw), "--dangerously-skip-permissions\n"); got != full {
				t.Fatalf("native approval flag=%v, want %v; argv=%q", got, full, raw)
			}
		})
	}
}
