//go:build darwin || linux

package process

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

type workflowParent struct {
	parent             int
	tty, group, marker string
}

var workflowMarker = regexp.MustCompile(`^clawdline-workflow:([A-Za-z0-9_-]{22})$`)

func workflowParentTable(table string) map[int]workflowParent {
	out := map[int]workflowParent{}
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		pid, _ := strconv.Atoi(f[1])
		parent, _ := strconv.Atoi(f[4])
		row := workflowParent{parent: parent, tty: f[0], group: f[2]}
		// Only the dedicated sh wrapper's final argv identifies the launch. Text
		// in a provider prompt or an unrelated command is not a wrapper receipt.
		if path.Base(f[5]) == "sh" && len(f) > 8 && f[6] == "-c" {
			if m := workflowMarker.FindStringSubmatch(f[len(f)-1]); len(m) == 2 {
				row.marker = m[1]
			}
		}
		out[pid] = row
	}
	return out
}
func workflowAncestor(rows map[int]workflowParent, pid int) string {
	original, ok := rows[pid]
	if !ok {
		return ""
	}
	seen := map[int]bool{pid: true}
	for parent := original.parent; parent > 0; {
		if seen[parent] {
			return ""
		}
		seen[parent] = true
		row, ok := rows[parent]
		if !ok || row.tty != original.tty || row.group != original.group {
			return ""
		}
		if row.marker != "" {
			return row.marker
		}
		parent = row.parent
	}
	return ""
}
