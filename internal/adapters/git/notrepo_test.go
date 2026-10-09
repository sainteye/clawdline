package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A directory that is gone, or that this process may not enter, is not a
// directory without a repository: git never ran, so nothing was learned about
// one. Those used to read as ErrNotRepository.
func TestAMissingOrRefusedDirectoryIsNotNotARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	ctx := context.Background()
	plain := t.TempDir()
	if _, err := New().Changes(ctx, plain); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("a plain directory: %v, want ErrNotRepository", err)
	}
	if _, err := New().Changes(ctx, filepath.Join(plain, "gone")); !errors.Is(err, ErrNoDirectory) {
		t.Fatalf("a missing directory: %v, want ErrNoDirectory", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root enters every directory")
	}
	shut := filepath.Join(plain, "shut")
	if err := os.Mkdir(shut, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(shut, 0o700) })
	if _, err := New().Changes(ctx, shut); !errors.Is(err, ErrNoPermission) {
		t.Fatalf("a directory this process may not enter: %v, want ErrNoPermission", err)
	}
}

func TestNotReposForgetsAfterItsAgeAndPastItsRows(t *testing.T) {
	clock := time.Unix(1_000, 0)
	n := NewNotRepos(time.Minute, 2)
	n.now = func() time.Time { return clock }
	n.Remember("/a")
	clock = clock.Add(time.Second)
	n.Remember("/b")
	if !n.Known("/a") || !n.Known("/b") {
		t.Fatal("a remembered directory was not known")
	}
	clock = clock.Add(time.Second)
	n.Remember("/c")
	if n.Known("/a") || !n.Known("/b") || !n.Known("/c") {
		t.Fatal("past the row limit the oldest was not the one let go")
	}
	clock = clock.Add(time.Minute)
	if n.Known("/b") || n.Known("/c") {
		t.Fatal("a directory was believed past the age limit")
	}
	n.Remember("/d")
	n.Forget("/d")
	if n.Known("/d") {
		t.Fatal("a forgotten directory was still known")
	}
}
