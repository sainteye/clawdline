//go:build !windows

package main

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

func tryBrowserProfileLock(path string) (io.Closer, bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, false, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = errors.New(cliCopy("core", "browser_host.profile_lock_not_regular", "browser profile lock is not a regular file"))
		}
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}
