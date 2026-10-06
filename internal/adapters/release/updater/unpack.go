package updater

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// unpack extracts the tar.gz at archive into dir, which must not exist yet.
// It refuses, as archive_unsafe, any entry that would land outside dir: an
// absolute name, a name climbing out with "..", a symlink whose target
// resolves outside dir, a hard link, a device or a fifo. Modes keep their
// permission bits only. On any refusal dir is removed.
func unpack(archive, dir string) (err error) {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fail(CodeArchiveUnsafe, "%s is not gzip: %v", filepath.Base(archive), err)
	}
	defer gz.Close()
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	tr := tar.NewReader(gz)
	var entries int
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fail(CodeArchiveUnsafe, "reading %s: %v", filepath.Base(archive), err)
		}
		entries++
		if entries > maxArchiveEntries {
			return fail(CodeArchiveUnsafe, "%s has more than %d entries", filepath.Base(archive), maxArchiveEntries)
		}
		name, ok := insideName(h.Name)
		if !ok {
			return fail(CodeArchiveUnsafe, "entry %q would land outside the release directory", h.Name)
		}
		if name == "" {
			continue // "./"
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := noSymlinkParents(dir, name); err != nil {
			return err
		}
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if h.Size < 0 || total > maxUnpackedBytes {
				return fail(CodeArchiveUnsafe, "%s unpacks to more than %d bytes", filepath.Base(archive), int64(maxUnpackedBytes))
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0o600)
			if err != nil {
				return fail(CodeArchiveUnsafe, "entry %q: %v", h.Name, err)
			}
			n, cerr := io.Copy(out, io.LimitReader(tr, h.Size))
			if err := out.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
			if n != h.Size {
				return fail(CodeArchiveUnsafe, "entry %q is truncated", h.Name)
			}
		case tar.TypeSymlink:
			link := h.Linkname
			if path.IsAbs(link) || filepath.IsAbs(link) {
				return fail(CodeArchiveUnsafe, "symlink %q points at the absolute path %q", h.Name, link)
			}
			if _, ok := insideName(path.Join(path.Dir(name), link)); !ok {
				return fail(CodeArchiveUnsafe, "symlink %q points outside the release directory (%q)", h.Name, link)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(link, target); err != nil {
				return fail(CodeArchiveUnsafe, "entry %q: %v", h.Name, err)
			}
		default:
			return fail(CodeArchiveUnsafe, "entry %q is a %q, which a release does not carry", h.Name, string(h.Typeflag))
		}
	}
}

// insideName cleans an archive name and says whether it stays inside the
// directory it is unpacked into.
func insideName(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || filepath.IsAbs(name) {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", true
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// noSymlinkParents refuses an entry written through a symlink the archive
// made earlier: a link that is inside dir by name can still be followed
// somewhere else by a later entry under it.
func noSymlinkParents(dir, name string) error {
	parts := strings.Split(name, "/")
	cur := dir
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fail(CodeArchiveUnsafe, "entry %q is written through a symlink", name)
		}
	}
	return nil
}
