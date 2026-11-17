//go:build linux

package resourcefs

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func UsePresence(ctx context.Context, path string) string {
	return useProcessPresence(ctx, path, "/proc")
}

// procRoot is supplied only by the platform and isolated filesystem tests.
func useProcessPresence(ctx context.Context, path, procRoot string) string {
	if strings.ContainsAny(path, "\n\\") {
		return "unknown"
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return "unknown"
	}
	read := 0
	observed := false
	inside := func(p string) bool { return p == path || strings.HasPrefix(p, path+string(filepath.Separator)) }
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return "unknown"
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		read++
		if read > taskresource.FilesLimit {
			return "unknown"
		}
		proc := filepath.Join(procRoot, e.Name())
		info, err := os.Stat(proc)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "unknown"
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return "unknown"
		}
		if st.Uid != uint32(os.Geteuid()) {
			continue
		}
		observed = true
		cwd, err := os.Readlink(filepath.Join(proc, "cwd"))
		if err != nil {
			if _, gone := os.Stat(proc); os.IsNotExist(gone) {
				continue
			}
			return "unknown"
		}
		if inside(cwd) {
			return "present"
		}
		// Executables and mappings may stay in use after their data FD closes.
		executable, eerr := os.Readlink(filepath.Join(proc, "exe"))
		if eerr == nil && inside(executable) {
			return "present"
		}
		if eerr != nil && !os.IsNotExist(eerr) {
			return "unknown"
		}
		fds, err := os.ReadDir(filepath.Join(proc, "fd"))
		if os.IsNotExist(err) {
			if _, gone := os.Stat(proc); os.IsNotExist(gone) {
				continue
			}
			return "unknown"
		}
		if err != nil {
			return "unknown"
		}
		for _, fd := range fds {
			read++
			if read > taskresource.FilesLimit {
				return "unknown"
			}
			target, err := os.Readlink(filepath.Join(proc, "fd", fd.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "unknown"
			}
			if inside(target) {
				return "present"
			}
		}
		maps, err := os.Open(filepath.Join(proc, "maps"))
		if os.IsNotExist(err) {
			if _, gone := os.Stat(proc); os.IsNotExist(gone) {
				continue
			}
			return "unknown"
		}
		if err != nil {
			return "unknown"
		}
		scanner := bufio.NewScanner(maps)
		for scanner.Scan() {
			if ctx.Err() != nil {
				maps.Close()
				return "unknown"
			}
			read++
			if read > taskresource.FilesLimit {
				maps.Close()
				return "unknown"
			}
			if inside(mappedPath(scanner.Text())) {
				maps.Close()
				return "present"
			}
		}
		err = scanner.Err()
		maps.Close()
		if err != nil {
			return "unknown"
		}

	}
	if !observed {
		return "unknown"
	}
	return "absent"
}

// The first five maps columns are whitespace-delimited; a pathname can itself
// contain repeated spaces and must not be reconstructed with strings.Fields.
func mappedPath(line string) string {
	for field := 0; field < 5; field++ {
		line = strings.TrimLeft(line, " \t")
		end := strings.IndexAny(line, " \t")
		if end < 0 {
			return ""
		}
		line = line[end:]
	}
	return strings.TrimLeft(line, " \t")
}
