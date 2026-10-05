package main

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The command writes one file and prints the address a browser opens it at as
// its last line.
func TestReportPrintsTheFileAddressLast(t *testing.T) {
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
	sha := git("rev-parse", "HEAD")
	status := filepath.Join(t.TempDir(), "status.md")
	if err := os.WriteFile(status, []byte("# Done\n\n## ✅ Rules\nWritten.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "報告 dir")
	var stdout, stderr bytes.Buffer
	code := runReport(&stdout, &stderr, []string{"--repo", dir, "--status", status, "--lang", "zh-TW", "--out", outDir, sha},
		time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), func(string) string { return "" })
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	last := lines[len(lines)-1]
	u, err := url.Parse(last)
	if err != nil || u.Scheme != "file" {
		t.Fatalf("the last line is not a file address: %q", last)
	}
	want := filepath.Join(outDir, "2026-10-05-"+filepath.Base(dir)+".html")
	if filepath.FromSlash(u.Path) != want {
		t.Errorf("address names %s, want %s", u.Path, want)
	}
	page, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte(`"lang":"zh-TW"`)) || !bytes.Contains(page, []byte(`"pins":["CLAUDE.md"]`)) {
		t.Error("the page does not carry the asked language and the pinned CLAUDE.md")
	}
	if strings.Contains(last, " ") {
		t.Errorf("the address carries a raw space: %q", last)
	}
}

// With no --out the report goes to the state directory, outside every
// repository, and a second report the same day does not overwrite the first.
func TestReportPathDefaultsToTheStateDirectory(t *testing.T) {
	state := t.TempDir()
	first, err := reportPath("", state, "2026-10-05", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if first != filepath.Join(state, "reports", "2026-10-05-demo.html") {
		t.Errorf("default path = %s", first)
	}
	if err := os.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := reportPath("", state, "2026-10-05", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if second != filepath.Join(state, "reports", "2026-10-05-demo-2.html") {
		t.Errorf("second path = %s", second)
	}
	named, err := reportPath(filepath.Join(state, "x.html"), state, "2026-10-05", "demo")
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
