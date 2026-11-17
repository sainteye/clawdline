package resourcefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func ownedResource(t *testing.T) (*Root, taskresource.Resource) {
	t.Helper()
	root, err := Open(filepath.Join(t.TempDir(), "managed"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	r := taskresource.Resource{ID: "10000000-0000-4000-8000-000000000001", Generation: "20000000-0000-4000-8000-000000000001", Class: taskresource.Scratch}
	r.RelativePath = r.ID
	r.Identity, err = root.Allocate(r.ID, r.Generation)
	if err != nil {
		t.Fatal(err)
	}
	return root, r
}

func TestManagedResourceRemovalRejectsChangedSnapshotAndKeepsOtherRoots(t *testing.T) {
	root, r := ownedResource(t)
	ctx := context.Background()
	file := filepath.Join(root.Path(), r.ID, "cache-entry")
	if err := os.WriteFile(file, []byte("rebuildable"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := root.Snapshot(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed rebuildable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(ctx, r, before.Digest); !errors.Is(err, ErrChanged) {
		t.Fatal("stale plan removed changed bytes", err)
	}
	after, err := root.Snapshot(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(t.TempDir(), "deliverable")
	if err := os.WriteFile(protected, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(ctx, r, after.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.Path(), r.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("resource remains", err)
	}
	if b, err := os.ReadFile(protected); err != nil || string(b) != "retained" {
		t.Fatal("protected bytes changed", err)
	}
}

func TestManagedRootNeverAdoptsUnmarkedOrLinkedData(t *testing.T) {
	path := t.TempDir()
	if _, err := Open(path); !errors.Is(err, ErrUnsafe) {
		t.Fatal("unmarked directory adopted", err)
	}
	root, r := ownedResource(t)
	if _, err := root.Allocate("../foreign", r.Generation); !errors.Is(err, ErrUnsafe) {
		t.Fatal("foreign path allocated", err)
	}
	if err := os.WriteFile(filepath.Join(root.Path(), r.ID, ".owner"), []byte("wrong-generation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Snapshot(context.Background(), r); !errors.Is(err, ErrChanged) {
		t.Fatal("changed generation accepted", err)
	}
}

func TestManagedResourceRejectsRepositoriesAndProtectedOutputBoundaries(t *testing.T) {
	for _, name := range []string{".git", "artifacts", "result.json"} {
		t.Run(name, func(t *testing.T) {
			root, r := ownedResource(t)
			if err := os.WriteFile(filepath.Join(root.Path(), r.ID, name), []byte("must be preserved"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := root.Snapshot(context.Background(), r); !errors.Is(err, ErrUnsafe) {
				t.Fatal("protected boundary treated as scratch", err)
			}
		})
	}
}

func TestOutputManifestDetectsChangesAndCancellation(t *testing.T) {
	path := t.TempDir()
	file := filepath.Join(path, "result.txt")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Manifest(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Manifest(context.Background(), path)
	if err != nil || a.Digest == b.Digest {
		t.Fatal("changed output not detected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Manifest(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled scan continued", err)
	}
}
