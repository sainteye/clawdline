package install

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Release archives are gzipped tars with their contents at the root
// (tools/release/build.sh `tar_into`). install.sh unpacks the daemon archive
// before any signature has been checked, because the checking binary is inside
// it; setup then proves that every file it unpacked is the file the signed
// manifest's archive holds, so what is activated is what was signed.

// entryName is a tar entry's path, cleaned, or "" for the root itself. An
// absolute path or one that climbs out is refused.
func entryName(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == "" {
		return "", nil
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive entry %q leaves the archive", name)
	}
	return clean, nil
}

func eachEntry(archive string, fn func(h *tar.Header, name string, r io.Reader) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a gzip archive: %w", filepath.Base(archive), err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(archive), err)
		}
		name, err := entryName(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if err := fn(h, name, tr); err != nil {
			return err
		}
	}
}

// VerifyTree proves that dir holds every regular file and symlink of archive
// with the same contents. Files in dir the archive does not have (a
// manifest copy setup wrote) are not its concern.
func VerifyTree(archive, dir string) error {
	return eachEntry(archive, func(h *tar.Header, name string, r io.Reader) error {
		local := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			info, err := os.Lstat(local)
			if err != nil || !info.IsDir() {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is missing from " + dir}
			}
		case tar.TypeSymlink:
			target, err := os.Readlink(local)
			if err != nil || target != h.Linkname {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is not the link the archive holds"}
			}
		case tar.TypeReg:
			want := sha256.New()
			if _, err := io.Copy(want, r); err != nil {
				return err
			}
			f, err := os.Open(local)
			if err != nil {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is missing from " + dir}
			}
			got := sha256.New()
			_, err = io.Copy(got, f)
			f.Close()
			if err != nil {
				return err
			}
			if string(got.Sum(nil)) != string(want.Sum(nil)) {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " in " + dir + " differs from the signed archive"}
			}
		}
		return nil
	})
}

// Extract unpacks archive into dir, which must not exist yet. Only
// directories, regular files and symlinks that stay inside dir are written;
// anything else is refused.
func Extract(archive, dir string) error {
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return eachEntry(archive, func(h *tar.Header, name string, r io.Reader) error {
		local := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(local, mode|0o700)
		case tar.TypeSymlink:
			target := h.Linkname
			resolved := target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(local), resolved)
			}
			if filepath.IsAbs(target) || !within(dir, resolved) {
				return fmt.Errorf("archive link %q points outside the archive", h.Name)
			}
			return os.Symlink(target, local)
		case tar.TypeReg:
			f, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, r)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			return err
		default:
			return fmt.Errorf("archive entry %q is neither a file, a directory nor a link", h.Name)
		}
	})
}

func within(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
