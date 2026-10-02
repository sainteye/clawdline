package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// UpstreamRemote against repositories it made: a clone's branch tracks its
// origin, a branch set to track another local branch tracks no remote, and a
// fresh branch tracks nothing.
func TestUpstreamRemoteReadsWhatABranchTracks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "nonexistent-gitconfig"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(root, "nonexistent-gitconfig"),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin")
	if err := os.Mkdir(origin, 0o700); err != nil {
		t.Fatal(err)
	}
	run(origin, "init", "-q", "-b", "trunk")
	run(origin, "commit", "-q", "--allow-empty", "-m", "first")
	run(root, "clone", "-q", "--origin", "upstream", origin, "clone")
	clone := filepath.Join(root, "clone")
	run(clone, "branch", "fresh")
	run(clone, "branch", "--track", "local-tracker", "trunk")

	g := New()
	for branch, want := range map[string]string{"trunk": "upstream", "fresh": "", "local-tracker": ""} {
		got, err := g.UpstreamRemote(context.Background(), clone, branch)
		if err != nil || got != want {
			t.Fatalf("%s tracks %q (%v), want %q", branch, got, err, want)
		}
	}
	if _, err := g.UpstreamRemote(context.Background(), filepath.Join(root, "nowhere"), "trunk"); err == nil {
		t.Fatal("a directory with no repository answered as one tracking nothing")
	}
}
