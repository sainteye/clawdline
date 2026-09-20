//go:build darwin

package process

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// systemOpenFiles asks `lsof` which files a set of pids holds open.
//
// One call for every pid that needs one, not one call each: lsof takes a comma
// separated list, and the cost of this reading is the process it starts rather
// than the number of rows it answers for. It is asked only about Codex
// sessions whose id is not already on their command line, which on a machine
// with a dozen sessions is usually none of them.
//
// `-F pn` is the machine-readable form — a `p<pid>` line, then an `n<name>`
// line per file — which is parsed instead of columns for the same reason Scan
// does not count `ps` columns: a human-readable table changes width.
func systemOpenFiles(ctx context.Context, pids []int) (map[int][]string, bool) {
	if len(pids) == 0 {
		return map[int][]string{}, true
	}
	list := make([]string, 0, len(pids))
	for _, pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}
	// `-w` drops the warnings a partial answer prints, `-n` and `-P` skip the
	// name lookups this has no use for, and `-d` keeps the text and mapped
	// entries out: a session's transcript is an open descriptor, never a
	// mapped one.
	//
	// `-a` is what makes `-p` a restriction rather than one more thing to
	// report. lsof ORs its selectors by default, so without it the pid list
	// selects nothing away and the whole machine is listed: measured here on
	// 2026-09-20, asking about three pids answered for 664 processes in 22,166
	// lines and 198 ms, against 301 lines and 26 ms with the flag. The answer
	// was right either way — the parse is keyed by pid — which is exactly why
	// nothing had noticed on a reading this takes every couple of seconds.
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-w", "-n", "-P", "-a", "-F", "pn", "-d", "^txt,^cwd,^rtd", "-p", strings.Join(list, ","))
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	// lsof exits non-zero when it found nothing to report, which is an answer
	// and not a failure. What is not an answer is exiting without a word: a
	// missing lsof, a denial, a killed context.
	if err != nil && len(out) == 0 {
		return nil, false
	}
	return parseLsof(string(out)), true
}

// parseLsof reads lsof's `-F pn` output: a `p<pid>` line opens a process's
// block and every `n<name>` line after it belongs to that process.
func parseLsof(out string) map[int][]string {
	files := map[int][]string{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			n, err := strconv.Atoi(line[1:])
			if err != nil {
				pid = 0
				continue
			}
			pid = n
		case 'n':
			if pid == 0 {
				continue
			}
			files[pid] = append(files[pid], line[1:])
		}
	}
	return files
}
