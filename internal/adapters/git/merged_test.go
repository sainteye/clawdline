package git

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mergedRepository(t *testing.T) (string, func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(repo, "no-global-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid", "LC_ALL=C")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(bytes.TrimSpace(out))
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("branch", "merged")
	run("checkout", "-q", "-b", "unmerged")
	run("commit", "-q", "--allow-empty", "-m", "delivery")
	run("checkout", "-q", "main")
	return repo, run
}

func TestMergedKeepsUnknownDistinctFromUnmerged(t *testing.T) {
	repo, run := mergedRepository(t)
	tree := run("rev-parse", "HEAD^{tree}")
	for _, tc := range []struct {
		name, branch, target string
		merged, known        bool
	}{
		{"same commit", "main", "refs/heads/main", true, true},
		{"landed branch", "merged", "refs/heads/main", true, true},
		{"unlanded branch", "unmerged", "refs/heads/main", false, true},
		{"missing branch", "missing", "refs/heads/main", false, false},
		{"missing target", "merged", "refs/heads/missing", false, false},
		{"non-commit target", "merged", tree, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged, known := New().Merged(context.Background(), repo, tc.branch, tc.target)
			if merged != tc.merged || known != tc.known {
				t.Fatalf("merged=%v known=%v; want %v %v", merged, known, tc.merged, tc.known)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if merged, known := New().Merged(ctx, repo, "merged", "refs/heads/main"); merged || known {
		t.Fatal("cancelled read became known")
	}
	g := &Git{Binary: filepath.Join(t.TempDir(), "missing-git")}
	if merged, known := g.Merged(context.Background(), repo, "merged", "refs/heads/main"); merged || known {
		t.Fatal("unavailable Git became known")
	}
}

func TestMergedUsesOneGitProcess(t *testing.T) {
	repo, _ := mergedRepository(t)
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	if merged, known := New().Merged(context.Background(), repo, "merged", "refs/heads/main"); !merged || !known {
		t.Fatal("merged branch not recognized")
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct{ Event string }
		if json.Unmarshal(line, &event) == nil && event.Event == "start" {
			commands++
		}
	}
	if commands != 1 {
		t.Fatalf("one ancestry question started %d Git processes, want 1", commands)
	}
}
