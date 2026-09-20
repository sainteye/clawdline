//go:build linux

package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
)

// systemOpenFiles reads `/proc/<pid>/fd`, which is the same table lsof reads
// and needs no subprocess to read it.
//
// A pid that has exited between the scan and this read is absent, not a
// failure: it holds nothing open because it is gone. A directory that exists
// and cannot be entered is a failure, because that is this machine refusing to
// answer rather than the process having nothing to say.
func systemOpenFiles(ctx context.Context, pids []int) (map[int][]string, bool) {
	files := map[int][]string{}
	read := true
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return files, false
		}
		dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				read = false
			}
			continue
		}
		for _, e := range entries {
			target, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err != nil || target == "" || target[0] != '/' {
				continue
			}
			files[pid] = append(files[pid], target)
		}
	}
	return files, read
}
