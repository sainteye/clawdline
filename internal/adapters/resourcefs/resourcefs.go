// Package resourcefs confines disposable resources to daemon-created roots.
package resourcefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

const rootMarker = "clawdline-task-resources-v1\n"

var ErrUnsafe = errors.New("resource_path_unverified")
var ErrChanged = errors.New("resource_identity_changed")
var ErrInventoryIncomplete = errors.New("resource_inventory_limit")

type Root struct {
	path string
	dir  *os.Root
	info os.FileInfo
}

// Open refuses an existing unmarked or linked directory. A fresh private root
// has a durable marker; knowledge of its name alone never adopts old data.
func Open(path string) (*Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnsafe
	}
	created := false
	if err := os.Mkdir(path, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !privateDirectory(info) {
		return nil, ErrUnsafe
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	r := &Root{path: path, dir: dir, info: info}
	if created {
		if err := writeMarker(dir, ".owner", rootMarker); err != nil {
			dir.Close()
			return nil, err
		}
	}
	if err := r.current(); err != nil {
		dir.Close()
		return nil, err
	}
	return r, nil
}
func (r *Root) Close() error { return r.dir.Close() }
func (r *Root) Path() string { return r.path }
func (r *Root) current() error {
	info, err := os.Lstat(r.path)
	if err != nil || !privateDirectory(info) || !os.SameFile(r.info, info) {
		return ErrChanged
	}
	b, err := readMarker(r.dir, ".owner")
	if err != nil || string(b) != rootMarker {
		return ErrUnsafe
	}
	return nil
}
func readMarker(dir *os.Root, name string) ([]byte, error) {
	st, err := dir.Lstat(name)
	if err != nil || !privateRegular(st) {
		return nil, ErrUnsafe
	}
	f, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		return nil, ErrChanged
	}
	// Derived from the fixed marker format, not an arbitrary payload allowance.
	return io.ReadAll(io.LimitReader(f, int64(len(rootMarker)+2*len(uuid.Nil.String())+2)))
}
func writeMarker(dir *os.Root, name, value string) error {
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(value); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func validID(id string) bool { u, err := uuid.Parse(id); return err == nil && u.String() == id }

// Allocate only creates a direct child; it cannot adopt a caller's path.
func (r *Root) Allocate(id, generation string) (string, error) {
	if !validID(id) || !validID(generation) {
		return "", ErrUnsafe
	}
	if err := r.current(); err != nil {
		return "", err
	}
	if err := r.dir.Mkdir(id, 0o700); err != nil {
		return "", err
	}
	child, err := r.dir.OpenRoot(id)
	if err != nil {
		return "", err
	}
	defer child.Close()
	if err := writeMarker(child, ".owner", id+"\n"+generation+"\n"); err != nil {
		return "", err
	}
	info, err := r.dir.Lstat(id)
	if err != nil {
		return "", err
	}
	return fileIdentity(filepath.Join(r.path, id), info)
}
func (r *Root) verified(resource taskresource.Resource) (*os.Root, error) {
	if resource.RelativePath != resource.ID || !validID(resource.ID) || !validID(resource.Generation) {
		return nil, ErrUnsafe
	}
	if err := r.current(); err != nil {
		return nil, err
	}
	info, err := r.dir.Lstat(resource.ID)
	if err != nil {
		return nil, err
	}
	if !privateDirectory(info) {
		return nil, ErrUnsafe
	}
	identity, err := fileIdentity(filepath.Join(r.path, resource.ID), info)
	if err != nil || identity != resource.Identity {
		return nil, ErrChanged
	}
	child, err := r.dir.OpenRoot(resource.ID)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, ErrChanged
	}
	b, err := readMarker(child, ".owner")
	if err != nil || string(b) != resource.ID+"\n"+resource.Generation+"\n" {
		child.Close()
		return nil, ErrChanged
	}
	named, err := r.dir.Lstat(resource.ID)
	if err != nil || !os.SameFile(info, named) {
		child.Close()
		return nil, ErrChanged
	}
	return child, nil
}

type Snapshot struct {
	Measured        bool   `json:"measured"`
	Logical         int64  `json:"logical_bytes"`
	Allocated       int64  `json:"allocated_bytes"`
	AllocationKnown bool   `json:"allocation_known"`
	Files           int    `json:"files"`
	Digest          string `json:"digest"`
}

// Snapshot is bounded and does not follow links. Nested repositories, protected
// output boundaries and unsupported file types are kept for owner review.
func (r *Root) Snapshot(ctx context.Context, resource taskresource.Resource) (Snapshot, error) {
	dir, err := r.verified(resource)
	if err != nil {
		return Snapshot{}, err
	}
	defer dir.Close()
	return snapshot(ctx, dir, false, true)
}

// Manifest hashes retained outputs in the exact root verified by the service.
func Manifest(ctx context.Context, path string) (Snapshot, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, ErrUnsafe
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer dir.Close()
	out, err := snapshot(ctx, dir, true, false)
	if err != nil {
		return out, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) {
		return Snapshot{}, ErrChanged
	}
	return out, nil
}

func snapshot(ctx context.Context, dir *os.Root, content, disposable bool) (Snapshot, error) {
	out := Snapshot{Measured: true, AllocationKnown: true}
	hash := sha256.New()
	rootInfo, err := dir.Stat(".")
	if err != nil {
		return Snapshot{}, err
	}
	err = fs.WalkDir(dir.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
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
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return ErrUnsafe
		}
		if !sameFilesystem(rootInfo, info) {
			return ErrUnsafe
		}
		if disposable && (entry.Name() == ".git" || entry.Name() == "artifacts" || entry.Name() == "result.json") {
			return ErrUnsafe
		}
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d\n", path, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.IsDir() {
			return nil
		}
		if info.Size() < 0 || out.Logical > math.MaxInt64-info.Size() {
			return ErrUnsafe
		}
		out.Logical += info.Size()
		allocated, known := allocatedBytes(info)
		out.AllocationKnown = out.AllocationKnown && known
		if allocated < 0 || out.Allocated > math.MaxInt64-allocated {
			return ErrUnsafe
		}
		out.Allocated += allocated
		if content {
			f, err := dir.Open(path)
			if err != nil {
				return err
			}
			before, err := f.Stat()
			if err != nil || !os.SameFile(info, before) {
				f.Close()
				return ErrChanged
			}
			_, err = io.Copy(hash, manifestReader{ctx: ctx, source: f})
			after, aerr := f.Stat()
			cerr := f.Close()
			named, nerr := dir.Lstat(path)
			if err != nil {
				return err
			}
			if cerr != nil {
				return cerr
			}
			if aerr != nil || nerr != nil || !os.SameFile(before, after) || !os.SameFile(before, named) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				return ErrChanged
			}
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	out.Digest = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}

// Remove requires the reviewed snapshot. The service also refreshes the owner,
// readers and output manifest; this adapter only owns filesystem confinement.
func (r *Root) Remove(ctx context.Context, resource taskresource.Resource, expected string) error {
	if expected == "" {
		return ErrUnsafe
	}
	got, err := r.Snapshot(ctx, resource)
	if err != nil {
		return err
	}
	if got.Digest != expected {
		return ErrChanged
	}
	dir, err := r.verified(resource)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := dir.Open(".")
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".owner" {
			continue
		}
		if err := dir.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	// Recheck the named directory before touching its ownership marker or
	// unlinking the top-level name. A replacement is foreign even if empty.
	named, err := r.dir.Lstat(resource.ID)
	if err != nil {
		return err
	}
	opened, err := dir.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(named, opened) {
		return ErrChanged
	}
	// Keep the generation marker until all disposable contents are gone, so
	// a failed removal remains verifiable and retryable after a restart.
	if err := dir.Remove(".owner"); err != nil {
		return err
	}
	if err := r.dir.Remove(resource.ID); err != nil {
		_ = writeMarker(dir, ".owner", resource.ID+"\n"+resource.Generation+"\n")
		return err
	}
	return nil
}

func (r *Root) Verify(resource taskresource.Resource) error {
	dir, err := r.verified(resource)
	if err != nil {
		return err
	}
	return dir.Close()
}

// Absent checks only the registered directory itself. A missing nested file
// during a walk cannot be mistaken for the entire resource having disappeared.
func (r *Root) Absent(resource taskresource.Resource) (bool, error) {
	if resource.RelativePath != resource.ID || !validID(resource.ID) {
		return false, ErrUnsafe
	}
	if err := r.current(); err != nil {
		return false, err
	}
	_, err := r.dir.Lstat(resource.ID)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// A large preserved artifact must still respect inspection cancellation.
type manifestReader struct {
	ctx    context.Context
	source io.Reader
}

func (r manifestReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
