//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
)

func tryBrowserProfileLock(path string) (io.Closer, bool, error) {
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return nil, false, errors.New(cliCopy("core", "browser_host.profile_lock_not_regular", "browser profile lock is not a regular file"))
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, false, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}
