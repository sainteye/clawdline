package install

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// ServiceRelease identifies the installed executable by its inode (older
// daemons) or its signed executable bytes. current alone can point at a
// different release while the updater is switching or rolling back.
func (l Layout) ServiceRelease(exe string) (string, Kind) {
	return l.serviceRelease(exe, true)
}

func (l Layout) serviceRelease(exe string, matchCopies bool) (string, Kind) {
	binary := "clawdline"
	switch filepath.Clean(exe) {
	case filepath.Clean(l.ServiceBin):
		binary = DaemonBinary
	case filepath.Clean(l.LegacyServiceBin()):
	default:
		return "", KindNone
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", KindNone
	}
	defer f.Close()
	service, err := f.Stat()
	if err != nil || !service.Mode().IsRegular() {
		return "", KindNone
	}
	digest := sync.OnceValues(func() ([]byte, error) { return executableDigest(f) })
	matches := func(name string) bool {
		candidate, err := os.Open(filepath.Join(l.ReleaseDir(name), binary))
		if err != nil {
			return false
		}
		defer candidate.Close()
		info, err := candidate.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		if os.SameFile(service, info) {
			return true
		}
		if !matchCopies || binary != DaemonBinary || service.Size() != info.Size() {
			return false
		}
		want, err := digest()
		if err != nil {
			return false
		}
		got, err := executableDigest(candidate)
		return err == nil && bytes.Equal(want, got)
	}
	current, kind := l.CurrentRelease()
	if kind != KindNone && matches(current) {
		return current, kind
	}
	entries, err := os.ReadDir(l.Releases)
	if err != nil {
		return "", KindNone
	}
	for _, entry := range entries {
		name := entry.Name()
		if kind := kindOfRelease(name); kind != KindNone && name != current && matches(name) {
			return name, kind
		}
	}
	return "", KindNone
}

func executableDigest(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// DetachServiceBinary replaces a managed hard link with a byte-for-byte copy.
// This supports the first update performed by a supervisor that still installs
// hard links. Re-exec updates the process image, but launch attribution can
// keep its original path until the next start.
func (l Layout) DetachServiceBinary() (bool, error) {
	src, err := os.Open(l.ServiceBin)
	if err != nil {
		return false, err
	}
	defer src.Close()
	old, err := src.Stat()
	if err != nil {
		return false, err
	}
	name, _ := l.serviceRelease(l.ServiceBin, false)
	if name == "" {
		return false, nil
	}

	if err := copyServiceBinary(src, l.ServiceBin, old); err != nil {
		return false, err
	}
	return true, nil
}

func copyServiceBinary(src *os.File, destination string, previous os.FileInfo) error {
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("the named background service is not an executable regular file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".daemon-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, src); err != nil {
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if previous != nil {
		selected, err := os.Stat(destination)
		if err != nil {
			return err
		}
		if !os.SameFile(previous, selected) {
			return fmt.Errorf("the selected background service changed while it was being copied")
		}
	}
	return os.Rename(tmp.Name(), destination)
}
