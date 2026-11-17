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
		if processExited(proc) {
			continue
		}
		cwd, err := os.Readlink(filepath.Join(proc, "cwd"))
		if err != nil {
			if processAbsent(proc) {
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
			if processAbsent(proc) {
				continue
			}
			return "unknown"
		}
		fds, err := os.ReadDir(filepath.Join(proc, "fd"))
		if os.IsNotExist(err) {
			if processAbsent(proc) {
				continue
			}
			return "unknown"
		}
		if err != nil {
			if processAbsent(proc) {
				continue
			}
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
			if processAbsent(proc) {
				continue
			}
			return "unknown"
		}
		if err != nil {
			if processAbsent(proc) {
				continue
			}
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

// A zombie has already released its files, mappings and working directory.
// Its /proc directory can remain until its parent reaps it. Missing access
// alone is not exit evidence; require the kernel's explicit Z/X state.
func processExited(proc string) bool {
	b, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(b[end+1:]))
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func processAbsent(proc string) bool {
	_, err := os.Stat(proc)
	return os.IsNotExist(err) || processExited(proc)
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
