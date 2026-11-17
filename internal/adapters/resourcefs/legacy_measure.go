package resourcefs

import (
	"context"
	"io/fs"
	"math"
	"os"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// LegacySize is read-only accounting, never adoption or a deletion grant.
// Its caller must supply an authoritative legacy boundary. Links are counted
// by their own metadata and never followed to include a different owner.
func LegacySize(ctx context.Context, path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, ErrUnsafe
	}
	if !info.IsDir() {
		return measuredSize(info)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return 0, ErrChanged
	}
	var total int64
	count := 0
	err = fs.WalkDir(root.FS(), ".", func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > taskresource.FilesLimit {
			return ErrInventoryIncomplete
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		n, err := measuredSize(info)
		if err != nil {
			return err
		}
		if total > math.MaxInt64-n {
			return ErrUnsafe
		}
		total += n
		return nil
	})
	return total, err
}
func measuredSize(info os.FileInfo) (int64, error) {
	n := info.Size()
	if allocated, known := allocatedBytes(info); known {
		n = allocated
	}
	if n < 0 {
		return 0, ErrUnsafe
	}
	return n, nil
}

// InspectLegacyScratch is a read-only rooted check after the broker's legacy
// ownership proof. It grants no generic path-based removal authority.
func InspectLegacyScratch(ctx context.Context, path string) (Snapshot, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, ErrUnsafe
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return Snapshot{}, ErrChanged
	}
	return snapshot(ctx, root, false, true)
}
