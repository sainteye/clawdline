package projects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func consentGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("local Git required:", err)
	}
	templates := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
	defer cancel()
	full := []string{"-c", "core.hooksPath=" + templates, "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false", "-c", "user.name=Synthetic Test", "-c", "user.email=test@example.invalid"}
	cmd := exec.CommandContext(ctx, git, append(full, args...)...)
	cmd.Dir = dir
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_TEMPLATE_DIR="+templates, "LC_ALL=C")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("local synthetic Git %v: %v %s", args, err, data)
	}
}
func consentGitWorkspace(t *testing.T) (string, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	consentGit(t, root, "init")
	consentGit(t, root, "commit", "--allow-empty", "-m", "Synthetic membership fixture")
	linked := filepath.Join(base, "linked")
	consentGit(t, root, "worktree", "add", "--detach", linked, "HEAD")
	raw, err := os.ReadFile(filepath.Join(linked, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	return root, linked, strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir: "))
}
func TestWorkspaceConsentChecksRealGitRegistrationAndNestedDirectories(t *testing.T) {
	root, linked, _ := consentGitWorkspace(t)
	ctx := context.Background()
	for _, owner := range []string{root, linked} {
		nested := filepath.Join(owner, "nested")
		if err := os.Mkdir(nested, 0700); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{owner, nested} {
			if err := VerifyProjectWorkspace(ctx, root, path); err != nil {
				t.Fatal(path, err)
			}
		}
	}
	foreign := filepath.Join(filepath.Dir(root), "foreign")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProjectWorkspace(ctx, root, foreign); err == nil {
		t.Fatal("plain foreign directory borrowed membership")
	}
	nestedRepo := filepath.Join(root, "other-repository")
	if err := os.Mkdir(nestedRepo, 0700); err != nil {
		t.Fatal(err)
	}
	consentGit(t, nestedRepo, "init")
	if err := VerifyProjectWorkspace(ctx, root, nestedRepo); err == nil {
		t.Fatal("nested repository borrowed parent membership")
	}
}
func TestWorkspaceConsentRejectsForgedOrIncompleteRegistration(t *testing.T) {
	root, linked, gitdir := consentGitWorkspace(t)
	ctx := context.Background()
	fake := filepath.Join(filepath.Dir(root), "fake")
	if err := os.Mkdir(fake, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fake, ".git"), []byte("gitdir: "+gitdir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if candidate, _ := CanonicalProjectKey(fake); candidate != root {
		t.Fatal("near-miss fixture did not name target Project")
	}
	if err := VerifyProjectWorkspace(ctx, root, fake); err == nil {
		t.Fatal("forged pointer borrowed another worktree's registration")
	}
	for _, kind := range []string{"backlink", "commondir", "missing-head", "oversized-marker", "symlink-marker"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(gitdir, "gitdir")
			replacement := []byte(filepath.Join(fake, ".git") + "\n")
			switch kind {
			case "commondir":
				path = filepath.Join(gitdir, "commondir")
				replacement = []byte("../../../unrelated\n")
			case "missing-head":
				path = filepath.Join(gitdir, "HEAD")
			case "oversized-marker":
				path = filepath.Join(linked, ".git")
				replacement = []byte(strings.Repeat("x", WorkspaceMetadataBytesLimit+1))
			case "symlink-marker":
				path = filepath.Join(linked, ".git")
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(path)
				if err := os.WriteFile(path, before, 0600); err != nil {
					t.Fatal(err)
				}
			}()
			if kind == "missing-head" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if kind == "symlink-marker" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(fake, ".git"), path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, replacement, 0600); err != nil {
				t.Fatal(err)
			}
			if err := VerifyProjectWorkspace(ctx, root, linked); err == nil {
				t.Fatal("invalid registration admitted", kind)
			}
		})
	}
}
func TestWorkspaceConsentMetadataLimitAndCancellation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "metadata")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", WorkspaceMetadataBytesLimit)), 0600); err != nil {
		t.Fatal(err)
	}
	if text, err := workspaceMetadata(context.Background(), path); err != nil || len(text) != WorkspaceMetadataBytesLimit {
		t.Fatal(len(text), err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", WorkspaceMetadataBytesLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceMetadata(context.Background(), path); err != WorkspaceMetadataTooLarge {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyProjectWorkspace(ctx, root, root); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
