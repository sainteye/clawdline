package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

// PinnedExecutable hashes one local executable once. Subsequent reads inspect
// the kernel file identity/change stamp, never the full executable bytes. It
// starts no process and is not a native behavior or effective-mode receipt.
type PinnedExecutable struct {
	path, digest string
	info         os.FileInfo
}

func NewPinnedExecutable(ctx context.Context, path string) (*PinnedExecutable, error) {
	if err := nativeContext(ctx); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, refuse("antigravity_executable_invalid")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&0111 == 0 {
		return nil, refuse("antigravity_executable_invalid")
	}
	digest, err := fileDigest(path)
	if err != nil {
		return nil, refuse("antigravity_executable_unreadable")
	}
	after, err := os.Lstat(path)
	if err != nil || !sameExecutable(before, after) {
		return nil, refuse("antigravity_executable_changed")
	}
	if err := nativeContext(ctx); err != nil {
		return nil, err
	}
	return &PinnedExecutable{path: path, digest: digest, info: after}, nil
}

func (p *PinnedExecutable) current() bool {
	if p == nil {
		return false
	}
	info, err := os.Lstat(p.path)
	return err == nil && info.Mode().IsRegular() && sameExecutable(p.info, info)
}

func (p *PinnedExecutable) Fingerprint(ctx context.Context) (ImageFingerprint, error) {
	if err := nativeContext(ctx); err != nil {
		return ImageFingerprint{}, err
	}
	if !p.current() {
		return ImageFingerprint{}, refuse("antigravity_executable_changed")
	}
	if err := nativeContext(ctx); err != nil {
		return ImageFingerprint{}, err
	}
	return ImageFingerprint{Executable: p.path, Digest: p.digest, Platform: runtime.GOOS, Architecture: runtime.GOARCH}, nil
}
