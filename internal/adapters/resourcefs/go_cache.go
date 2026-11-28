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

func (r *Root) GoCachePath(resource taskresource.Resource) (string, error) {
	if resource.Owner.Kind != "shared_go" || resource.Class != taskresource.Cache {
		return "", ErrUnsafe
	}
	dir, err := r.verified(resource)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := dir.Mkdir("go-build", 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	info, err := dir.Lstat("go-build")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrUnsafe
	}
	return filepath.Join(r.path, resource.ID, "go-build"), nil
}

// TrimGoCache keeps the newest recognized Go entries inside the existing
// resource. The service fences new consumers and proves all old consumers gone
// before calling it. It never trims a default, explicit, gate or module cache.
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
	used := before.Logical
	if before.AllocationKnown {
		used = before.Allocated
	}
	removed := Snapshot{AllocationKnown: true}
	for _, entry := range entries {
		if used <= budget {
			break
		}
		if authorize != nil {
			allowed, err := authorize()
			if err != nil || !allowed {
				return removed, err
			}
			authorize = nil
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
	return removed, nil
}
