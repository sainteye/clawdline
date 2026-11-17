//go:build darwin || linux

package resourcefs

import (
	"fmt"
	"os"
	"syscall"
)

func privateDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
func privateRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}
func fileIdentity(_ string, info os.FileInfo) (string, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrUnsafe
	}
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino), nil
}
func allocatedBytes(info os.FileInfo) (int64, bool) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int64(s.Blocks) * 512, true
}

func sameFilesystem(root, info os.FileInfo) bool {
	a, ok := root.Sys().(*syscall.Stat_t)
	b, other := info.Sys().(*syscall.Stat_t)
	return ok && other && a.Dev == b.Dev
}
