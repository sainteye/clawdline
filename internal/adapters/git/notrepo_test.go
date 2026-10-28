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

func TestCapacityReadingsForgetExpiredIdleAnswers(t *testing.T) {
	clock := time.Unix(1000, 0)
	n := NewNotRepos(time.Minute, 256)
	n.now = func() time.Time { return clock }
	n.Remember("/expired")
	clock = clock.Add(2529 * time.Second)
	if r := n.AgeReading(); r.Used != 0 || r.Counters.Expired != 1 || !r.OldestAt.IsZero() {
		t.Fatalf("expired idle answer reported as occupied: %+v", r)
	}
	if r := n.RowsReading(); r.Used != 0 {
		t.Fatal(r)
	}
	n.Remember("/older")
	clock = clock.Add(30 * time.Second)
	n.Remember("/fresh")
	clock = clock.Add(30 * time.Second)
	if r := n.RowsReading(); r.Used != 1 {
		t.Fatal("row reading retained expired answers", r)
	}
	if r := n.AgeReading(); r.Used != 30 {
		t.Fatal("fresh answer age", r)
	}
	if n.Known("/older") || !n.Known("/fresh") {
		t.Fatal("validity differs from capacity readings")
	}
}
