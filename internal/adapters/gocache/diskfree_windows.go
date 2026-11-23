//go:build windows

package gocache

import "golang.org/x/sys/windows"

// DiskFree is the space the calling user may still use on the volume that
// holds dir.
func DiskFree(dir string) (int64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free, total, all uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, &total, &all); err != nil {
		return 0, err
	}
	return int64(free), nil
}
