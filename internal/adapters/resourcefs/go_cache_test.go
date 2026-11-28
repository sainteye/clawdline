package resourcefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestSharedGoTrimKeepsRecentEntriesUnknownDataAndOwnership(t *testing.T) {
	root, r := ownedResource(t)
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	path, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 1))
	recent := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 2))
	unknown := filepath.Join(path, "keep-unknown")
	for _, p := range []string{old, recent, unknown} {
		if err := os.WriteFile(p, []byte("cache bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	before, err := root.Snapshot(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	a, known := allocatedBytes(info)
	budget := before.Logical - info.Size()
	if known {
		budget = before.Allocated - a
	}
	authorize := func() (bool, error) { return true, nil }
	removed, err := root.TrimGoCache(context.Background(), r, before.Digest, budget, authorize)
	if err != nil || removed.Logical != info.Size() {
		t.Fatal(removed, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old entry retained", err)
	}
	for _, p := range []string{recent, unknown, filepath.Join(root.Path(), r.ID, ".owner")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("protected or recent bytes removed", p, err)
		}
	}
	if _, err := root.TrimGoCache(context.Background(), r, before.Digest, 0, authorize); !errors.Is(err, ErrChanged) {
		t.Fatal("changed plan accepted", err)
	}
}

func TestGoTrimRefusesTaskCachesAndEscapedEntries(t *testing.T) {
	root, r := ownedResource(t)
	if _, err := root.GoCachePath(r); !errors.Is(err, ErrUnsafe) {
		t.Fatal(err)
	}
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	path, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	if err := os.Symlink(foreign, filepath.Join(path, "ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Snapshot(context.Background(), r); !errors.Is(err, ErrUnsafe) {
		t.Fatal("linked cache accepted", err)
	}
}

func TestSharedGoTrimCanReclaimReadOnlyModulesUnderPressure(t *testing.T) {
	root, r := ownedResource(t)
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	build, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(filepath.Dir(build), "go-mod")
	version := filepath.Join(mod, "example.invalid", "module@v1.0.0")
	if err := os.MkdirAll(version, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(mod, "keep-unknown")
	for _, path := range []string{filepath.Join(version, "go.mod"), unknown} {
		if err := os.WriteFile(path, []byte("module fixture"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(version, 0o500); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before, err := root.Snapshot(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	authorizations := 0
	removed, err := root.TrimGoCache(ctx, r, before.Digest, 0, func() (bool, error) {
		authorizations++
		return true, nil
	})
	if err != nil || removed.Logical != int64(len("module fixture")) {
		t.Fatal(removed, err)
	}
	if authorizations != 1 {
		t.Fatalf("module-only trim requested %d authorizations, want 1", authorizations)
	}
	if _, err := os.Stat(version); !os.IsNotExist(err) {
		t.Fatal("read-only module was retained", err)
	}
	if raw, err := os.ReadFile(unknown); err != nil || string(raw) != "module fixture" {
		t.Fatal("unknown file was changed", err)
	}
	if _, err := root.GoCachePath(r); err != nil {
		t.Fatal("shared cache could not be reused", err)
	}
}

func TestSharedGoTrimLeavesReplacedModuleReadOnly(t *testing.T) {
	root, r := ownedResource(t)
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	build, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(filepath.Dir(build), "go-mod")
	version := filepath.Join(mod, "example.invalid", "module@v1.0.0")
	if err := os.MkdirAll(filepath.Join(version, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "nested", "go.mod"), []byte("module fixture"), 0o400); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(version, "nested"), version} {
		if err := os.Chmod(path, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	before, err := root.Snapshot(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	movedParent := filepath.Join(mod, "moved")
	moved := filepath.Join(movedParent, filepath.Base(version))
	t.Cleanup(func() {
		for _, path := range []string{version, moved} {
			_ = os.Chmod(path, 0o700)
			_ = os.Chmod(filepath.Join(path, "nested"), 0o700)
		}
	})
	// Replace the reviewed entry after it has been opened and inspected.
	removed, err := root.TrimGoCache(ctx, r, before.Digest, 0, func() (bool, error) {
		if err := os.Rename(filepath.Dir(version), movedParent); err != nil {
			return false, err
		}
		if err := os.Mkdir(filepath.Dir(version), 0o700); err != nil {
			return false, err
		}
		return true, os.Mkdir(version, 0o500)
	})
	if !errors.Is(err, ErrChanged) || removed.Logical != 0 {
		t.Fatalf("trim after replacement = (%+v, %v), want ErrChanged and no deletion", removed, err)
	}
	for _, path := range []string{moved, filepath.Join(moved, "nested"), version} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o500 {
			t.Errorf("%s mode = %o, want 500", filepath.Base(path), mode)
		}
	}
}
