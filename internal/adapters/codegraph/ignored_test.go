package codegraph

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return root
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// Observed on a real project: 747 tracked files, but a gitignored local
// release directory held 20,533 entries and every build failed at the
// scan-entry limit before parsing started.
func TestGitIgnoredTreesDoNotCountAgainstTheScanLimit(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "logs/\n*.gen.go\n")
	write(t, root, "main.go", "package main\n")
	write(t, root, "kept.gen.go", "package main\n")
	if out, err := exec.Command("git", "-C", root, "add", "-f", "kept.gen.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	write(t, root, "other.gen.go", "package main\n")
	releases := filepath.Join(root, "logs", "stack", "releases")
	if err := os.MkdirAll(releases, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= MaxScanEntries; i++ {
		if err := os.WriteFile(filepath.Join(releases, strconv.Itoa(i)+".go"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, status, err := scan(t.Context(), root, "")
	if err != nil {
		t.Fatalf("scan refused a project whose only large tree is gitignored: %v (entries %d)", err, status.Entries)
	}
	for _, name := range []string{"main.go", ".gitignore", "kept.gen.go"} {
		if _, ok := sources[name]; !ok {
			t.Errorf("%s is missing; a tracked file stays an input even when it matches an ignore pattern", name)
		}
	}
	for name := range sources {
		if strings.HasPrefix(name, "logs/") || name == "other.gen.go" {
			t.Errorf("ignored path %s was inventoried", name)
		}
	}
}

func TestDirectoriesOutsideGitKeepTheFullScan(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".gitignore", "logs/\n")
	write(t, root, "logs/tool.go", "package logs\n")
	sources, _, err := scan(t.Context(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sources["logs/tool.go"]; !ok {
		t.Fatal("a directory without a Git work tree must not apply ignore patterns by itself")
	}
}

func TestOversizedIgnoreListingIsRefused(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "*.tmp\n")
	name := strings.Repeat("x", 200)
	for i := 0; i*(len(name)+8) <= MaxIgnoredListBytes; i++ {
		write(t, root, name+strconv.Itoa(i)+".tmp", "")
	}
	if _, err := gitIgnored(t.Context(), root); !errors.Is(err, BuildError("source_limit")) {
		t.Fatalf("an ignore listing past its registered bound must be refused, got %v", err)
	}
}
