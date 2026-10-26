package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Codex child that may edit must also be able to stage what it edited:
// workspace-write keeps `.git` read-only, so edits and full carry a profile
// that names the repository's metadata and nothing wider.
func TestCodexEditingChildMayWriteItsRepositoryMetadata(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	tasks := t.TempDir()
	metadata := `"` + filepath.Join(repo, ".git") + `"="write"`
	for _, c := range []struct {
		mode, approval string
		network        bool
	}{{"edits", "--ask-for-approval on-request", false}, {"full", "--ask-for-approval never", true}} {
		line := strings.Join(permissionArgs(Record{Assistant: "codex", PermissionMode: c.mode, Repository: repo}, tasks, repo, ""), " ")
		for _, want := range []string{`default_permissions="clawdline-workspace"`,
			`permissions.clawdline-workspace.extends=":workspace"`, metadata, `"` + tasks + `"="write"`, c.approval} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: %q missing from %s", c.mode, want, line)
			}
		}
		if strings.Contains(line, "--sandbox") {
			t.Errorf("%s: a legacy --sandbox would override the profile: %s", c.mode, line)
		}
		if got := strings.Contains(line, "network.enabled=true"); got != c.network {
			t.Errorf("%s: network enabled = %v, want %v: %s", c.mode, got, c.network, line)
		}
		if strings.Contains(line, `":root"`) || strings.Contains(line, "danger") {
			t.Errorf("%s: the profile is wider than the workspace: %s", c.mode, line)
		}
	}
}

// The person's config.toml is theirs, and a Codex refuses to start under
// --strict-config when it carries a key that version does not know: on the
// Ubuntu host, codex-cli 0.161.0 stopped every edits child on
// `use_linux_sandbox_bwrap`, before drawing a composer. So the profile launch
// does not ask for strict parsing, and confinement does not depend on it: a
// Codex that cannot read the profile still finds workspace-write on the
// command line instead of the file's own sandbox_mode.
func TestCodexEditingChildStartsDespiteUnknownConfigKeys(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mode    string
		network bool
	}{{"edits", false}, {"full", true}} {
		line := strings.Join(permissionArgs(Record{Assistant: "codex", PermissionMode: c.mode, Repository: repo}, "", repo, ""), " ")
		if strings.Contains(line, "--strict-config") {
			t.Errorf("%s: --strict-config refuses a config.toml with keys this Codex does not know: %s", c.mode, line)
		}
		if !strings.Contains(line, `sandbox_mode="workspace-write"`) {
			t.Errorf("%s: no workspace-write fallback for a Codex without profiles: %s", c.mode, line)
		}
		if got := strings.Contains(line, "sandbox_workspace_write.network_access=true"); got != c.network {
			t.Errorf("%s: fallback network access = %v, want %v: %s", c.mode, got, c.network, line)
		}
	}
}

// Without metadata it can check, the launch keeps the flags it always had.
func TestCodexChildWithoutMetadataDirectoryKeepsWorkspaceWrite(t *testing.T) {
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"", linked} {
		line := strings.Join(permissionArgs(Record{Assistant: "codex", PermissionMode: "edits", Repository: repo}, "/tmp/tasks", "/tmp/w", ""), " ")
		if line != "--ask-for-approval on-request --sandbox workspace-write" {
			t.Errorf("repository %q: %s", repo, line)
		}
	}
}

// Ask is the person's own provider settings; a repository does not change that.
func TestAskCodexChildGetsNoMetadataProfile(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if args := permissionArgs(Record{Assistant: "codex", PermissionMode: "ask", Repository: repo}, "/tmp/tasks", repo, ""); len(args) != 0 {
		t.Fatalf("ask overrides native settings: %v", args)
	}
}

// A worktree child's index and refs live in its gitdir under the main
// repository's metadata, which Codex protects as well; both are named.
func TestCodexWorktreeChildMayWriteItsGitdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "wt")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base"},
		{"-C", repo, "worktree", "add", "-q", worktree, "-b", "child"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo, _ = filepath.EvalSymlinks(repo)
	worktree, _ = filepath.EvalSymlinks(worktree)
	line := strings.Join(permissionArgs(Record{Assistant: "codex", PermissionMode: "full", Repository: repo}, "", worktree, ""), " ")
	for _, want := range []string{`"` + filepath.Join(repo, ".git") + `"="write"`, `"` + filepath.Join(repo, ".git", "worktrees", "wt") + `"="write"`} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from %s", want, line)
		}
	}
}

// A `.git` file pointing anywhere but this repository's worktrees grants nothing.
func TestCodexChildIgnoresGitdirOutsideTheRepository(t *testing.T) {
	repo, cwd, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{elsewhere, filepath.Join(repo, ".git"), filepath.Join(repo, ".git", "worktrees", "..", "..")} {
		if err := os.WriteFile(filepath.Join(cwd, ".git"), []byte("gitdir: "+target+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := worktreeGitdir(cwd, filepath.Join(repo, ".git")); got != "" {
			t.Errorf("gitdir %s was granted as %s", target, got)
		}
	}
}
