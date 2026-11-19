//go:build darwin || linux

package resourcefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedResourceCannotFollowSymlinkOrReplacement(t *testing.T) {
	root, r := ownedResource(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root.Path(), r.ID, "link")); err != nil {
		t.Fatal(err)
	}
	accounting, err := root.Measure(context.Background(), r)
	if err != nil || accounting.Files != 3 {
		t.Fatal("accounting followed the foreign target or rejected the link metadata", accounting, err)
	}
	if _, err := root.Snapshot(context.Background(), r); !errors.Is(err, ErrUnsafe) {
		t.Fatal("symlink accepted", err)
	}
	if err := os.Remove(filepath.Join(root.Path(), r.ID, "link")); err != nil {
		t.Fatal(err)
	}
	snap, err := root.Snapshot(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root.Path(), r.ID), filepath.Join(root.Path(), r.ID+"-retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root.Path(), r.ID)); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(context.Background(), r, snap.Digest); err == nil {
		t.Fatal("replacement accepted")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "retained" {
		t.Fatal("outside target changed", err)
	}
}
