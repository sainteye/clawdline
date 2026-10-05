package main

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/turnreport"
)

// reportRepo is a git project with one commit touching CLAUDE.md, and a
// status file; it answers the project, the commit and the status file.
func reportRepo(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.invalid")
	git("config", "user.name", "t")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "rules")
	status := filepath.Join(t.TempDir(), "status.md")
	if err := os.WriteFile(status, []byte("# Done\n\n## ✅ Rules\nWritten.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, git("rev-parse", "HEAD"), status
}

var reportDay = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// By default the report is kept in the state directory under an id, and the
// command prints two addresses: the file, which a terminal opens, then the
// daemon's, which the console can make a link of.
func TestReportPrintsTheFileThenTheDaemonAddress(t *testing.T) {
	dir, sha, status := reportRepo(t)
	state := filepath.Join(t.TempDir(), "state 狀態")
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_PORT", "7799")
	var stdout, stderr bytes.Buffer
	if code := runReport(&stdout, &stderr, []string{"--repo", dir, "--status", status, "--lang", "zh-TW", sha},
		reportDay, func(string) string { return "" }); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q, want two addresses", stdout.String())
	}
	file, err := url.Parse(lines[0])
	if err != nil || file.Scheme != "file" || strings.Contains(lines[0], " ") {
		t.Fatalf("the first line is not a file address: %q", lines[0])
	}
	m := regexp.MustCompile(`^http://127\.0\.0\.1:7799/reports/(2026-10-05-[0-9a-f]{32})$`).FindStringSubmatch(lines[1])
	if m == nil {
		t.Fatalf("the second line is not the daemon's address: %q", lines[1])
	}
	want := filepath.Join(state, "reports", m[1], "report.html")
	if filepath.FromSlash(file.Path) != want {
		t.Errorf("the file address names %s, want %s", file.Path, want)
	}
	page, err := turnreport.ReadStored(state, m[1])
	if err != nil {
		t.Fatalf("the daemon would not answer the id it was given: %v", err)
	}
	if !bytes.Contains(page, []byte(`"lang":"zh-TW"`)) || !bytes.Contains(page, []byte(`"pins":["CLAUDE.md"]`)) {
		t.Error("the page does not carry the asked language and the pinned CLAUDE.md")
	}
}

// A report written somewhere else has a file address only, and says why.
func TestReportOutsideTheStateDirectoryHasNoDaemonAddress(t *testing.T) {
	dir, sha, status := reportRepo(t)
	outDir := filepath.Join(t.TempDir(), "報告 dir")
	var stdout, stderr bytes.Buffer
	if code := runReport(&stdout, &stderr, []string{"--repo", dir, "--status", status, "--out", outDir, sha},
		reportDay, func(string) string { return "" }); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "file://") {
		t.Fatalf("stdout = %q, want one file address", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no http address") {
		t.Errorf("stderr does not say why there is no http address: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "2026-10-05-"+filepath.Base(dir)+".html")); err != nil {
		t.Error(err)
	}
}

// --out names a file or a directory; a second report the same day in that
// directory does not overwrite the first.
func TestReportPathOutsideTheStateDirectory(t *testing.T) {
	state := t.TempDir()
	out := filepath.Join(t.TempDir(), "reports")
	first, id, err := reportPath(out, state, "2026-10-05", "demo")
	if err != nil || id != "" || first != filepath.Join(out, "2026-10-05-demo.html") {
		t.Fatalf("first = %s %q %v", first, id, err)
	}
	if err := os.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := reportPath(out, state, "2026-10-05", "demo")
	if err != nil || second != filepath.Join(out, "2026-10-05-demo-2.html") {
		t.Errorf("second = %s, %v", second, err)
	}
	named, _, err := reportPath(filepath.Join(state, "x.html"), state, "2026-10-05", "demo")
	if err != nil || named != filepath.Join(state, "x.html") {
		t.Errorf("--out x.html = %s, %v", named, err)
	}
	if got := safeName("../a/b"); strings.ContainsAny(got, `/\`) {
		t.Errorf("safeName kept a separator: %q", got)
	}
}

func TestReportRefusesAMissingStatusOrCommit(t *testing.T) {
	for _, args := range [][]string{{"HEAD"}, {"--status", "s.md"}, {"--status", "s.md", "--lang", "fr", "HEAD"}} {
		var stdout, stderr bytes.Buffer
		if code := runReport(&stdout, &stderr, args, time.Now(), func(string) string { return "" }); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if got := localeLanguage(func(k string) string { return map[string]string{"LANG": "zh_TW.UTF-8"}[k] }); got != "zh-TW" {
		t.Errorf("zh_TW locale gave %s", got)
	}
	if got := localeLanguage(func(string) string { return "" }); got != "en" {
		t.Errorf("no locale gave %s", got)
	}
}
