package resourcefs

import (
	"context"
	"io/fs"
	"math"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// VerifyRoot checks the daemon-owned boundary without walking its children.
func (r *Root) VerifyRoot() error { return r.current() }

// Measure is read-only accounting, not a deletion snapshot. Protected names,
// symlinks and special entries are counted by their own metadata, never followed
// to another owner. Destructive inspection retains its stricter Snapshot rules.
func (r *Root) Measure(ctx context.Context, resource taskresource.Resource) (Snapshot, error) {
	dir, err := r.verified(resource)
	if err != nil {
		return Snapshot{}, err
	}
	defer dir.Close()
	base, err := dir.Stat(".")
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Measured: true, AllocationKnown: true}
	err = fs.WalkDir(dir.FS(), ".", func(path string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		out.Files++
		if out.Files > taskresource.FilesLimit {
			return ErrInventoryIncomplete
		}
		st, err := dir.Lstat(path)
		if err != nil {
			return err
		}
		if !sameFilesystem(base, st) {
			return ErrUnsafe
		}
		if st.IsDir() {
			return nil
		}
		n := st.Size()
		allocated, known := allocatedBytes(st)
		if n < 0 || allocated < 0 || out.Logical > math.MaxInt64-n || out.Allocated > math.MaxInt64-allocated {
			return ErrUnsafe
		}
		out.Logical += n
		out.Allocated += allocated
		out.AllocationKnown = out.AllocationKnown && known
		return nil
	})
	return out, err
}
