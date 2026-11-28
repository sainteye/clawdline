package resourcefs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

var goCacheEntry = regexp.MustCompile(`^go-build/[0-9a-f]{2}/[0-9a-f]{64}-[ad]$`)
var goModuleVersion = regexp.MustCompile(`^[^@]+@v[0-9]`)

func (r *Root) GoCachePath(resource taskresource.Resource) (string, error) {
	if resource.Owner.Kind != "shared_go" || resource.Class != taskresource.Cache {
		return "", ErrUnsafe
	}
	dir, err := r.verified(resource)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	for _, name := range []string{"go-build", "go-mod"} {
		if err := dir.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		info, err := dir.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnsafe
		}
	}
	return filepath.Join(r.path, resource.ID, "go-build"), nil
}

// TrimGoCache keeps the newest recognized Go entries inside the existing
// resource. The service fences new consumers and proves all old consumers gone
// before calling it. Only this resource's build entries and modules are eligible.
// authorize runs once, before the first candidate could be unlinked.
func (r *Root) TrimGoCache(ctx context.Context, resource taskresource.Resource, expected string, budget int64, authorize func() (bool, error)) (Snapshot, error) {
	if resource.Owner.Kind != "shared_go" || resource.Class != taskresource.Cache || expected == "" || budget < 0 || authorize == nil {
		return Snapshot{}, ErrUnsafe
	}
	before, err := r.Snapshot(ctx, resource)
	if err != nil {
		return Snapshot{}, err
	}
	if before.Digest != expected {
		return Snapshot{}, ErrChanged
	}
	used := before.Logical
	if before.AllocationKnown {
		used = before.Allocated
	}
	if used <= budget {
		return Snapshot{AllocationKnown: true}, nil
	}
	dir, err := r.verified(resource)
	if err != nil {
		return Snapshot{}, err
	}
	defer dir.Close()
	type candidate struct {
		path string
		info os.FileInfo
		size Snapshot
	}
	entries := []candidate{}
	err = fs.WalkDir(dir.FS(), "go-build", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !goCacheEntry.MatchString(path) {
			return nil
		}
		info, err := dir.Lstat(path)
		if err != nil {
			return err
		}
		if e.IsDir() {
			child, err := dir.OpenRoot(path)
			if err != nil {
				return err
			}
			size, err := snapshot(ctx, child, false, true)
			child.Close()
			if err != nil {
				return err
			}
			entries = append(entries, candidate{path, info, size})
			return fs.SkipDir
		}
		a, known := allocatedBytes(info)
		entries = append(entries, candidate{path, info, Snapshot{Logical: info.Size(), Allocated: a, AllocationKnown: known}})
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].info.ModTime().Equal(entries[j].info.ModTime()) {
			return entries[i].path < entries[j].path
		}
		return entries[i].info.ModTime().Before(entries[j].info.ModTime())
	})
	removed := Snapshot{AllocationKnown: true}
	authorizeOnce := func() (bool, error) {
		if authorize == nil {
			return true, nil
		}
		allowed, err := authorize()
		authorize = nil
		return allowed, err
	}
	for _, entry := range entries {
		if used <= budget {
			break
		}
		if allowed, err := authorizeOnce(); err != nil || !allowed {
			return removed, err
		}
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
		now, err := dir.Lstat(entry.path)
		if err != nil || !os.SameFile(entry.info, now) || entry.info.Size() != now.Size() || !entry.info.ModTime().Equal(now.ModTime()) {
			return removed, ErrChanged
		}
		if err := r.current(); err != nil {
			return removed, err
		}
		if err := dir.RemoveAll(entry.path); err != nil {
			return removed, err
		}
		removed.Logical += entry.size.Logical
		removed.Allocated += entry.size.Allocated
		removed.AllocationKnown = removed.AllocationKnown && entry.size.AllocationKnown
		n := entry.size.Logical
		if before.AllocationKnown {
			n = entry.size.Allocated
		}
		used -= n
	}
	if used > budget {
		modules, err := r.trimGoModules(ctx, dir, used-budget, before.AllocationKnown, authorizeOnce)
		removed.Logical += modules.Logical
		removed.Allocated += modules.Allocated
		removed.AllocationKnown = removed.AllocationKnown && modules.AllocationKnown
		return removed, err
	}
	return removed, nil
}

// Module trees are read-only by default. Open directories through the verified
// root before making them removable, and keep names Go does not own.
func (r *Root) trimGoModules(ctx context.Context, dir *os.Root, excess int64, allocated bool, authorize func() (bool, error)) (Snapshot, error) {
	removed := Snapshot{AllocationKnown: true}
	mod, err := dir.OpenRoot("go-mod")
	if errors.Is(err, fs.ErrNotExist) {
		return removed, nil // Resources created before the module cache was added.
	}
	if err != nil {
		return removed, err
	}
	defer mod.Close()
	err = fs.WalkDir(mod.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if excess <= 0 {
			return fs.SkipAll
		}
		if !entry.IsDir() || (path != "cache/download" && path != "cache/vcs" && !goModuleVersion.MatchString(entry.Name())) {
			return nil
		}
		child, err := mod.OpenRoot(path)
		if err != nil {
			return err
		}
		defer child.Close()
		identity, err := child.Stat(".")
		if err != nil {
			return err
		}
		bytes, err := snapshot(ctx, child, false, true)
		if err != nil {
			return err
		}
		if allowed, err := authorize(); err != nil {
			return err
		} else if !allowed {
			return fs.SkipAll
		}
		if err := r.current(); err != nil {
			return err
		}
		named, err := mod.Lstat(path)
		if err != nil || !os.SameFile(identity, named) {
			return ErrChanged
		}
		if err := fs.WalkDir(child.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			d, err := child.Open(path)
			if err != nil {
				return err
			}
			defer d.Close()
			return d.Chmod(0o700)
		}); err != nil {
			return err
		}
		if err := mod.RemoveAll(path); err != nil {
			return err
		}
		removed.Logical += bytes.Logical
		removed.Allocated += bytes.Allocated
		removed.AllocationKnown = removed.AllocationKnown && bytes.AllocationKnown
		if allocated {
			excess -= bytes.Allocated
		} else {
			excess -= bytes.Logical
		}
		return fs.SkipDir
	})
	return removed, err
}
