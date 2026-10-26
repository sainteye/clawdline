//go:build darwin || linux

package process

import (
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// codexWriterPIDs uses the same process-table observation that identified the
// foreground TUI. A managed writer must be a native Codex process with no tty
// below that TUI's exact parent chain. Neither cwd, a recent file nor an
// unrelated shared daemon is evidence of ownership.
func codexWriterPIDs(table string, sessions []session.Session) map[int][]int {
	type process struct {
		pid    int
		tty    string
		codex  bool
		writer bool
	}
	children := map[int][]process{}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		pid, e1 := strconv.Atoi(fields[1])
		parent, e2 := strconv.Atoi(fields[4])
		if e1 != nil || e2 != nil || pid <= 0 || parent <= 0 {
			continue
		}
		children[parent] = append(children[parent], process{pid: pid, tty: fields[0],
			codex:  classify(fields[5]) == session.AssistantCodex,
			writer: len(fields) > 6 && fields[6] == "app-server"})
	}
	roots := map[int]bool{}
	for _, row := range sessions {
		if row.Assistant == session.AssistantCodex {
			roots[row.PID] = true
		}
	}
	out := map[int][]int{}
	for _, row := range sessions {
		if row.Assistant != session.AssistantCodex || row.PID <= 0 {
			continue
		}
		seen := map[int]bool{row.PID: true}
		queue := []int{row.PID}
		for len(queue) > 0 {
			parent := queue[0]
			queue = queue[1:]
			for _, child := range children[parent] {
				if seen[child.pid] || roots[child.pid] {
					continue
				}
				seen[child.pid] = true
				if child.codex && !child.writer {
					continue
				}
				// A nested terminal belongs to its own session, including when
				// ScanTTYs intentionally did not ask to discover that terminal.
				if child.tty != "??" && child.tty != "-" && child.tty != row.TTY {
					continue
				}
				queue = append(queue, child.pid)
				if child.codex && (child.tty == "??" || child.tty == "-") {
					out[row.PID] = append(out[row.PID], child.pid)
				}
			}
		}
	}
	return out
}

func hasCodexRollout(paths []string) bool {
	for _, path := range paths {
		if _, ok := codexRolloutID(path); ok {
			return true
		}
	}
	return false
}

func codexWriterFiles(files map[int][]string, pids []int) []string {
	var out []string
	seen := map[string]bool{}
	for _, pid := range pids {
		for _, path := range files[pid] {
			if !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		}
	}
	return out
}
