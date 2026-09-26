package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The repository a place clones is what names one project on two machines, so
// every way git finds its config has to answer the same repository.
func TestOriginRepoReadsTheConfigGitWouldRead(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	writeFile(t, filepath.Join(main, ".git", "config"), strings.Join([]string{
		"[core]",
		"\tbare = false",
		`[remote "upstream"]`,
		"\turl = git@github.com:someone-else/fork.git",
		`[Remote "origin"]`,
		"\tfetch = +refs/heads/*:refs/remotes/origin/*",
		"\turl = git@github.com:Owner/Name.git",
	}, "\n"))
	if got := OriginRepo(main); got != "github.com/owner/name" {
		t.Fatalf("a checkout answered %q", got)
	}
	// A place inside the repository is the repository's place.
	sub := filepath.Join(main, "web", "console")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := OriginRepo(sub); got != "github.com/owner/name" {
		t.Fatalf("a subdirectory answered %q", got)
	}
	// A linked worktree names its git dir in a file, and that dir names the
	// shared one in `commondir`; the config is the shared one's.
	linked := filepath.Join(root, "linked")
	gitDir := filepath.Join(main, ".git", "worktrees", "linked")
	writeFile(t, filepath.Join(linked, ".git"), "gitdir: "+gitDir+"\n")
	writeFile(t, filepath.Join(gitDir, "commondir"), "../..\n")
	if got := OriginRepo(linked); got != "github.com/owner/name" {
		t.Fatalf("a linked worktree answered %q", got)
	}
}

func TestOriginRepoIsEmptyWhenNothingPortableIsNamed(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		"no origin":      "[core]\n\tbare = false\n",
		"a local origin": "[remote \"origin\"]\n\turl = /srv/git/name.git\n",
		"a quoted url":   "[remote \"origin\"]\n\turl = \"https://example.com/team/tool.git\"\n",
	}
	want := map[string]string{"no origin": "", "a local origin": "", "a quoted url": "example.com/team/tool"}
	for name, body := range cases {
		dir := filepath.Join(root, strings.ReplaceAll(name, " ", "-"))
		writeFile(t, filepath.Join(dir, ".git", "config"), body)
		if got := OriginRepo(dir); got != want[name] {
			t.Errorf("%s answered %q, want %q", name, got, want[name])
		}
	}
	if got := OriginRepo(filepath.Join(root, "not-a-checkout")); got != "" {
		t.Errorf("a folder outside any repository answered %q", got)
	}
}

// A config past the bound is not parsed: the place names no repository rather
// than whatever the first part of a truncated file happened to say.
func TestOriginRepoDoesNotReadPastTheBound(t *testing.T) {
	dir := t.TempDir()
	body := "[remote \"origin\"]\n\turl = git@github.com:owner/name.git\n# " +
		strings.Repeat("x", MaxGitConfigBytes) + "\n"
	writeFile(t, filepath.Join(dir, ".git", "config"), body)
	if got := OriginRepo(dir); got != "" {
		t.Fatalf("an oversized config answered %q", got)
	}
}
