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
// (tools/release/build.sh `tar_into`). The current install.sh verifies the
// signed archive before unpacking or running it with local Python and OpenSSL.
// setup independently proves that every unpacked file is the file the signed
// manifest's archive holds, so what is activated is what was signed. Older
// bootstraps did not provide the pre-execution check.

// entryName is a tar entry's path, cleaned, or "" for the root itself. An
// absolute path or one that climbs out is refused.
func entryName(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == "" {
		return "", nil
	}
	if strings.Contains(name, `\`) || !filepath.IsLocal(filepath.FromSlash(clean)) || path.IsAbs(clean) {
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		return &Refusal{Code: CodeTreeMismatch, Detail: "cannot open " + dir}
	}
	defer root.Close()
	var links []string
	err = eachEntry(archive, func(h *tar.Header, name string, r io.Reader) error {
		if err := installNoSymlinkParents(root, name); err != nil {
			return &Refusal{Code: CodeTreeMismatch, Detail: name + " has an invalid parent"}
		}
		switch h.Typeflag {
		case tar.TypeDir:
			info, err := root.Lstat(name)
			if err != nil || !info.IsDir() {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is missing from " + dir}
			}
		case tar.TypeSymlink:
			target, err := root.Readlink(name)
			if err != nil || target != h.Linkname {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is not the link the archive holds"}
			}
			links = append(links, name)
		case tar.TypeReg:
			info, err := root.Lstat(name)
			if err != nil || !info.Mode().IsRegular() || info.Size() != h.Size {
				return &Refusal{Code: CodeTreeMismatch, Detail: name + " is not the regular file the archive holds"}
			}
			want := sha256.New()
			if _, err := io.Copy(want, r); err != nil {
				return err
			}
			f, err := root.Open(name)
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
		default:
			return &Refusal{Code: CodeTreeMismatch, Detail: name + " has an unsupported archive type"}
		}
		return nil
	})
	if err == nil {
		if err = installCheckLinks(root, links); err != nil {
			return &Refusal{Code: CodeTreeMismatch, Detail: err.Error()}
		}
	}
	return err
}

// Extract unpacks archive into dir, which must not exist yet. Only
// directories, regular files and symlinks that stay inside dir are written;
// anything else is refused.
func Extract(archive, dir string) (err error) {
	// Create the leaf exclusively: a pre-existing directory or link is never
	// adopted as the extraction root. Its parent is owned by the installer.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	var links []string
	err = eachEntry(archive, func(h *tar.Header, name string, r io.Reader) error {
		if err := installNoSymlinkParents(root, name); err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if info, err := root.Lstat(name); err == nil && !info.IsDir() {
				return fmt.Errorf("archive directory %q is not a directory", h.Name)
			}
			return root.MkdirAll(name, mode|0o700)
		case tar.TypeSymlink:
			target := h.Linkname
			if strings.Contains(target, `\`) || filepath.IsAbs(target) || !filepath.IsLocal(path.Join(path.Dir(name), target)) {
				return fmt.Errorf("archive link %q points outside the archive", h.Name)
			}
			if err := root.Symlink(target, name); err != nil {
				return err
			}
			links = append(links, name)
			return nil
		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0o600)
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
	if err == nil {
		err = installCheckLinks(root, links)
	}
	return err
}

// Reject archive writes and verification reads through symlink parents, even
// internal aliases. Root operations additionally contain actual path resolution.
func installNoSymlinkParents(root *os.Root, name string) error {
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[:i], "/")
		info, err := root.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("archive entry %q has a non-directory parent", name)
		}
	}
	return nil
}

// Check after all entries exist so forward links cannot hide an escape.
// Root.Stat permits internal chains and reports escapes even when dangling.
func installCheckLinks(root *os.Root, links []string) error {
	for _, name := range links {
		if _, err := root.Stat(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("archive link %q cannot resolve inside the archive: %w", name, err)
		}
	}
	return nil
}
