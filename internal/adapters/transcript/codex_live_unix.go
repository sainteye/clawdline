//go:build darwin

package transcript

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// heldCodexThreadIDs names only lock files another open description holds.
// Files left behind after a crash are ignored: a successful non-blocking lock
// proves nobody is using that record now, and is released immediately.
func heldCodexThreadIDs(dir string) ([]string, bool) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, true
	}
	if err != nil {
		return nil, false
	}
	ids := make([]string, 0, len(entries))
	complete := true
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".lock")
		if !codexUUID(id) {
			continue
		}
		f, err := os.OpenFile(filepath.Join(dir, entry.Name()), os.O_RDWR, 0)
		if err != nil {
			complete = false
			continue
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
			ids = append(ids, id)
		default:
			complete = false
		}
		_ = f.Close()
	}
	sort.Strings(ids)
	return ids, complete
}

func codexUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	for i := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := id[i]
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				if c < 'A' || c > 'F' {
					return false
				}
			}
		}
	}
	return true
}
