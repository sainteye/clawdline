//go:build darwin || linux

package antigravitylaunch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"

	"golang.org/x/sys/unix"
)

var errMissing = errors.New("private_file_missing")

type privateDir struct {
	path string
	file *os.File
	info os.FileInfo
}

func ownedFile(info os.FileInfo, dir bool) bool {
	if info == nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return false
	}
	if dir {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 && st.Nlink == 1
}

func openPrivate(path string, create bool) (*privateDir, error) {
	if !absolute(path) {
		return nil, fail("directory_invalid")
	}
	// Walk from / using no-follow directory opens, including public ancestors.
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail("directory_unavailable")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		if !component(part) {
			unix.Close(fd)
			return nil, fail("directory_invalid")
		}
		if create && i == len(parts)-1 {
			if e := unix.Mkdirat(fd, part, 0700); e != nil && !errors.Is(e, unix.EEXIST) {
				unix.Close(fd)
				return nil, fail("directory_write_failed")
			}
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, fail("directory_invalid")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !ownedFile(info, true) {
		f.Close()
		return nil, fail("directory_invalid")
	}
	d := &privateDir{path: path, file: f, info: info}
	if err := d.current(); err != nil {
		d.close()
		return nil, err
	}
	return d, nil
}

func (d *privateDir) close() { _ = d.file.Close() }
func (d *privateDir) current() error {
	canonical, e := filepath.EvalSymlinks(d.path)
	info, err := os.Lstat(d.path)
	if e != nil || canonical != d.path || err != nil || !ownedFile(info, true) || !os.SameFile(d.info, info) {
		return fail("directory_changed")
	}
	return nil
}
func (d *privateDir) child(name string, create bool) (*privateDir, error) {
	if !component(name) {
		return nil, fail("directory_invalid")
	}
	if err := d.current(); err != nil {
		return nil, err
	}
	if create {
		if err := unix.Mkdirat(int(d.file.Fd()), name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, fail("directory_write_failed")
		}
		if err := d.file.Sync(); err != nil {
			return nil, fail("directory_sync_failed")
		}
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail("directory_invalid")
	}
	f := os.NewFile(uintptr(fd), filepath.Join(d.path, name))
	info, e := f.Stat()
	if e != nil || !ownedFile(info, true) {
		f.Close()
		return nil, fail("directory_invalid")
	}
	next := &privateDir{path: filepath.Join(d.path, name), file: f, info: info}
	if err := next.current(); err != nil {
		next.close()
		return nil, err
	}
	if err := d.current(); err != nil {
		next.close()
		return nil, err
	}
	return next, nil
}
func (d *privateDir) read(name string, ceiling int) ([]byte, error) {
	if !component(name) {
		return nil, fail("file_invalid")
	}
	if err := d.current(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, errMissing
	}
	if err != nil {
		return nil, fail("file_unavailable")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !ownedFile(info, false) || info.Size() > int64(ceiling) {
		return nil, fail("file_invalid")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(ceiling)+1))
	after, e := f.Stat()
	named, n := os.Lstat(filepath.Join(d.path, name))
	if err != nil || e != nil || n != nil || len(b) > ceiling || !ownedFile(named, false) || !os.SameFile(info, after) || !os.SameFile(info, named) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, fail("file_changed")
	}
	if err := d.current(); err != nil {
		return nil, err
	}
	return b, nil
}

// replace has one fixed staging name per file, never a retry-generated name.
// Only an owned regular single-link staging file may be recovered after crash.
func (d *privateDir) replace(name string, body []byte) error {
	if !component(name) {
		return fail("file_invalid")
	}
	if err := d.current(); err != nil {
		return err
	}
	if _, err := d.read(name, antigravity.MaxObservationBytes); err != nil && !missing(err) {
		return err
	}
	stage := "." + name + ".next"
	if _, err := d.read(stage, antigravity.MaxObservationBytes); err == nil {
		if err := unix.Unlinkat(int(d.file.Fd()), stage, 0); err != nil {
			return fail("file_write_failed")
		}
	} else if !missing(err) {
		return err
	}
	fd, err := unix.Openat(int(d.file.Fd()), stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fail("file_write_failed")
	}
	f := os.NewFile(uintptr(fd), stage)
	defer func() { f.Close(); _ = unix.Unlinkat(int(d.file.Fd()), stage, 0) }()
	if _, err := f.Write(body); err != nil {
		return fail("file_write_failed")
	}
	if err := f.Sync(); err != nil {
		return fail("file_sync_failed")
	}
	if err := f.Close(); err != nil {
		return fail("file_write_failed")
	}
	if err := d.current(); err != nil {
		return err
	}
	if err := unix.Renameat(int(d.file.Fd()), stage, int(d.file.Fd()), name); err != nil {
		return fail("file_write_failed")
	}
	if err := d.file.Sync(); err != nil {
		return fail("file_sync_failed")
	}
	return d.current()
}

func (d *privateDir) exclusive(name string, body []byte) error {
	if !component(name) {
		return fail("file_invalid")
	}
	if err := d.current(); err != nil {
		return err
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fail("file_exists")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return fail("file_write_failed")
	}
	if err := f.Sync(); err != nil {
		return fail("file_sync_failed")
	}
	if err := d.file.Sync(); err != nil {
		return fail("file_sync_failed")
	}
	return d.current()
}
func (d *privateDir) lock() (*os.File, error) {
	fd, err := unix.Openat(int(d.file.Fd()), "context-store.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fail("lock_unavailable")
	}
	f := os.NewFile(uintptr(fd), "context-store.lock")
	info, e := f.Stat()
	if e != nil || !ownedFile(info, false) {
		f.Close()
		return nil, fail("lock_invalid")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fail("busy")
	}
	return f, nil
}

func (d *privateDir) checkLock(f *os.File) error {
	if f == nil {
		return fail("lock_invalid")
	}
	opened, err := f.Stat()
	named, e := os.Lstat(filepath.Join(d.path, "context-store.lock"))
	if err != nil || e != nil || !ownedFile(named, false) || !os.SameFile(opened, named) {
		return fail("lock_changed")
	}
	return nil
}

func (d *privateDir) removePending(name string) error {
	if _, err := d.read(name, antigravity.MaxObservationBytes); err != nil {
		if missing(err) {
			return nil
		}
		return err
	}
	if err := unix.Unlinkat(int(d.file.Fd()), name, 0); err != nil {
		return fail("file_write_failed")
	}
	if err := d.file.Sync(); err != nil {
		return fail("file_sync_failed")
	}
	return d.current()
}
