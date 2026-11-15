//go:build !windows

package projectassistantstore

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

func openOutput(root *os.Root, path string) (*os.File, error) {
	if !filepath.IsLocal(path) {
		return nil, os.ErrPermission
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(path, string(filepath.Separator))
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i+1 < len(parts) {
			flags |= unix.O_DIRECTORY
		}
		fd, e := unix.Openat(int(dir.Fd()), part, flags, 0)
		dir.Close()
		if e != nil {
			return nil, e
		}
		dir = os.NewFile(uintptr(fd), part)
	}
	return dir, nil
}

func syncOutputDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
