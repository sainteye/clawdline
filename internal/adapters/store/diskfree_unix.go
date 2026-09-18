//go:build darwin || linux

package store

import sysunix "golang.org/x/sys/unix"

// diskFree is the space an unprivileged writer may still use on the file
// system that holds dir.
func diskFree(dir string) (int64, error) {
	var st sysunix.Statfs_t
	if err := sysunix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
