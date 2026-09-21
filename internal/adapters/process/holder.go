package process

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Holder is the process the system names as listening on a TCP port.
//
// It exists for one sentence: a daemon that cannot bind its port says who has
// it. On 2026-09-21 a daemon the macOS shell started could not bind 7727,
// exited with the reason on a stderr nobody reads, and left the port to an
// orphan that served no console for seven and a half hours. "Address already
// in use" names nobody; this names the pid, what it is and since when.
type Holder struct {
	PID int
	// Command is the command line as the process table prints it. Empty when
	// the table would not say.
	Command string
	// Started is when the process began. Zero when the system did not say.
	Started time.Time
}

// ErrNoHolder is the system answering that nothing listens on the port. It is
// an answer, and a different one from a lookup that could not be made.
var ErrNoHolder = errors.New("the system lists no process listening on that port")

// parseLsofPIDs reads `lsof -F p` output: one `p<pid>` line per process.
func parseLsofPIDs(out string) []int {
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "p") {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(line[1:])); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// parsePSStart reads one line of `ps -o lstart=,command=` under LC_ALL=C:
// five fields of start time ("Mon Sep 21 03:15:22 2026") and then the command.
//
// The fields are split rather than counted by column, because the day of the
// month is space-padded and a column count is exactly what ps_unix.go warns
// against.
func parsePSStart(line string, loc *time.Location) (time.Time, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return time.Time{}, "", false
	}
	at, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(fields[:5], " "), loc)
	if err != nil {
		return time.Time{}, "", false
	}
	return at, strings.Join(fields[5:], " "), true
}

// parseNetstatListener reads `netstat -ano -p TCP` for the pid listening on
// port. The local address is the second column and ends in `:<port>`.
func parseNetstatListener(out string, port int) (int, bool) {
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.HasSuffix(fields[1], suffix) || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if pid, err := strconv.Atoi(fields[4]); err == nil && pid > 0 {
			return pid, true
		}
	}
	return 0, false
}

// parseSSListener reads `ss -Hltnp` for the pid in its `users:((…,pid=N,…))`
// column. ss prints the pid only for processes this user may inspect.
func parseSSListener(out string) (int, bool) {
	i := strings.Index(out, "pid=")
	if i < 0 {
		return 0, false
	}
	rest := out[i+len("pid="):]
	end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(rest)
	}
	pid, err := strconv.Atoi(rest[:end])
	return pid, err == nil && pid > 0
}
