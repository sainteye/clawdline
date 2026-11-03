package opsdocs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "main")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, main, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "README.md"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, main, "add", "README.md")
	run(t, main, "commit", "-q", "-m", "first")
	return main
}

// A linked worktree's info/exclude is its common directory's, so the line
// lands where every worktree of the repository reads it.
func TestExcludingTheOpsFolderFromALinkedWorktreeWritesTheCommonExclude(t *testing.T) {
	main := repo(t)
	linked := filepath.Join(filepath.Dir(main), "linked")
	run(t, main, "worktree", "add", "-q", linked)
	if err := os.MkdirAll(filepath.Join(main, "clawdline-ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "clawdline-ops", "Runbook.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run(t, main, "status", "--porcelain"), "clawdline-ops/") {
		t.Fatal("fixture: the folder is not visible to git before the change")
	}
	before := run(t, main, "ls-files", "-s")
	if err := SetExcluded(context.Background(), linked, true); err != nil {
		t.Fatal(err)
	}
	if err := SetExcluded(context.Background(), linked, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(main, ".git", "info", "exclude"))
	if err != nil || strings.Count(string(data), ExcludeLine+"\n") != 1 {
		t.Fatalf("common exclude: %q %v", data, err)
	}
	if status := run(t, main, "status", "--porcelain"); strings.Contains(status, "clawdline-ops") {
		t.Fatalf("git still lists the folder: %q", status)
	}
	if after := run(t, main, "ls-files", "-s"); after != before {
		t.Fatalf("tracked files changed:\n%s\n%s", before, after)
	}
	setup := Setup(context.Background(), main)
	if setup.Git != contract.OpsGitStateRepo || !setup.Excluded || !setup.FolderExists || setup.Visibility != contract.OpsVisibilityUnchecked {
		t.Fatalf("setup: %+v", setup)
	}
	if err := SetExcluded(context.Background(), main, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(main, ".git", "info", "exclude")); strings.Contains(string(data), ExcludeLine) {
		t.Fatalf("line left: %q", data)
	}
	if !strings.Contains(run(t, main, "status", "--porcelain"), "clawdline-ops/") {
		t.Fatal("including the folder again did not show it to git")
	}
}

func TestOutsideGitThereIsNothingToExclude(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if setup := Setup(context.Background(), dir); setup.Git != contract.OpsGitStateNone {
		t.Fatalf("setup: %+v", setup)
	}
	if err := SetExcluded(context.Background(), dir, true); err != ErrNoRepository {
		t.Fatalf("exclude outside git: %v", err)
	}
}

func TestGitHubIsAskedOncePerRepositoryAndOnlyForAGitHubRemote(t *testing.T) {
	main := repo(t)
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/repos/example-owner/example-repo" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"private":false}`))
	}))
	defer srv.Close()
	p := &Prober{Base: srv.URL}
	setup := Setup(context.Background(), main)
	p.Probe(context.Background(), main, &setup)
	if setup.Visibility != contract.OpsVisibilityUnknown || setup.VisibilityDetail != "not a GitHub remote" || asked.Load() != 0 {
		t.Fatalf("no remote: %+v asked %d", setup, asked.Load())
	}
	run(t, main, "remote", "add", "origin", "git@github.com:example-owner/example-repo.git")
	for range 2 {
		setup = Setup(context.Background(), main)
		p.Probe(context.Background(), main, &setup)
		if setup.Visibility != contract.OpsVisibilityPublic {
			t.Fatalf("public: %+v", setup)
		}
	}
	if asked.Load() != 1 {
		t.Fatalf("GitHub asked %d times", asked.Load())
	}
}
