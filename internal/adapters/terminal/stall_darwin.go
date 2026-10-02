//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// osascriptRun is one iTerm2 osascript run, named so that its failure says
// which script it was, about which session, and how long it waited.
type osascriptRun struct {
	kind    string
	session string
	started time.Time
	limit   time.Duration
}

// startOsascript is called just before the process is started, under the
// context that bounds it.
func startOsascript(ctx context.Context, kind, session string) osascriptRun {
	r := osascriptRun{kind: kind, session: session, started: time.Now()}
	if d, ok := ctx.Deadline(); ok {
		r.limit = time.Until(d).Round(time.Millisecond)
	}
	return r
}

// saidLimit is how much of osascript's stderr one failure line carries; the
// end is kept, because that is where the error number is.
const saidLimit = 512

// failed logs the run's one failure line and gives it to the stall watch.
func (r osascriptRun) failed(ctx context.Context, stderr string, err error) scriptFailure {
	reason, code := classifyScriptFailure(ctx.Err(), stderr, err)
	f := scriptFailure{At: time.Now(), Kind: r.kind, Session: r.session,
		Elapsed: time.Since(r.started), Limit: r.limit, Reason: reason, Code: code}
	if reason == reasonOther && err != nil {
		f.Err = err.Error()
	}
	said := strings.TrimSpace(stderr)
	if len(said) > saidLimit {
		said = said[len(said)-saidLimit:]
	}
	f.Said = said
	itermStall.record(f)
	return f
}

// The steps' own deadlines, each under the registered step limit. The probe
// is short on purpose: a healthy iTerm2 answers it in about 0.1 s, and what
// is being measured is whether it answers at all.
const (
	stallProbeTimeout  = 3 * time.Second
	stallSampleSeconds = 2
	stallQuickTimeout  = 5 * time.Second
)

// itermProbe asks iTerm2 the cheapest question there is. `running()` is
// answered by the system without an Apple Event, so a quit iTerm2 is not
// launched by its own diagnosis.
const itermProbe = `
const it = Application("iTerm2");
it.running() ? "windows: " + it.windows().length : "iTerm2 is not running";
`

// itermStallSteps is what a diagnosis records on this Mac, in order.
func itermStallSteps(dir string) []stallStep {
	return []stallStep{
		{Title: "probe: iTerm2 windows().length", Timeout: stallProbeTimeout, Run: func(ctx context.Context) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", itermProbe)
			return stallRun(cmd)
		}},
		{Title: fmt.Sprintf("sample: iTerm2 for %d s", stallSampleSeconds), Run: func(ctx context.Context) ([]byte, error) {
			return sampleITerm(ctx, dir)
		}},
		{Title: "load average", Timeout: stallQuickTimeout, Run: func(ctx context.Context) ([]byte, error) {
			return stallRun(exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "vm.loadavg"))
		}},
		{Title: "frontmost application", Timeout: stallQuickTimeout, Run: frontmostApp},
		{Title: "this daemon's osascript processes", Timeout: stallQuickTimeout, Run: func(ctx context.Context) ([]byte, error) {
			return ownOsascripts(ctx, os.Getpid())
		}},
	}
}

// stallRun runs one diagnostic command and answers everything it wrote. A
// command killed at its deadline is not waited on for its pipes past a second.
func stallRun(cmd *exec.Cmd) ([]byte, error) {
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

// sampleITerm samples iTerm2's threads, the main thread among them. The pid
// comes from the process table: asking iTerm2 for it would be one more Apple
// Event to an application that is not answering them.
func sampleITerm(ctx context.Context, dir string) ([]byte, error) {
	out, err := stallRun(exec.CommandContext(ctx, "/bin/ps", "-Ao", "pid=,comm="))
	if err != nil {
		return out, fmt.Errorf("the process table could not be read: %w", err)
	}
	pid := itermPID(string(out))
	if pid == 0 {
		return nil, fmt.Errorf("no iTerm2 process in the process table")
	}
	// sample writes its report to a file of its own choosing unless told
	// where; it is told, read back and removed, so nothing is left in /tmp.
	file := filepath.Join(dir, fmt.Sprintf(".%ssample-%d.tmp", stallFilePrefix, pid))
	defer os.Remove(file)
	said, err := stallRun(exec.CommandContext(ctx, "/usr/bin/sample", strconv.Itoa(pid),
		strconv.Itoa(stallSampleSeconds), "-file", file))
	report, readErr := os.ReadFile(file)
	head := fmt.Sprintf("pid: %d\n", pid)
	if err != nil {
		return append([]byte(head), said...), err
	}
	if readErr != nil {
		return append([]byte(head), said...), fmt.Errorf("sample wrote no report: %w", readErr)
	}
	return append([]byte(head), report...), nil
}

// itermPID finds iTerm2's own process in `ps -Ao pid=,comm=`: the one whose
// executable is named exactly iTerm2, not its helpers.
func itermPID(table string) int {
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		comm := strings.Join(fields[1:], " ")
		if filepath.Base(comm) != "iTerm2" {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// frontmostApp says which application is in front, so a diagnosis can tell an
// iTerm2 waiting on a dialog of its own from one that is busy.
func frontmostApp(ctx context.Context) ([]byte, error) {
	front, err := stallRun(exec.CommandContext(ctx, "/usr/bin/lsappinfo", "front"))
	asn := strings.TrimSpace(string(front))
	if err != nil || asn == "" {
		if err == nil {
			err = fmt.Errorf("lsappinfo named no frontmost application")
		}
		return front, err
	}
	info, err := stallRun(exec.CommandContext(ctx, "/usr/bin/lsappinfo", "info", "-only", "bundleid", asn))
	return append([]byte("asn: "+asn+"\n"), info...), err
}

// ownOsascripts lists the osascript processes this daemon started and has
// not yet seen end — the queue it has put in front of iTerm2.
func ownOsascripts(ctx context.Context, self int) ([]byte, error) {
	out, err := stallRun(exec.CommandContext(ctx, "/bin/ps", "-Ao", "pid=,ppid=,etime=,comm="))
	if err != nil {
		return out, fmt.Errorf("the process table could not be read: %w", err)
	}
	return []byte(ownOsascriptRows(string(out), self)), nil
}

// ownOsascriptRows reads `ps -Ao pid=,ppid=,etime=,comm=` for the osascript
// children of self.
func ownOsascriptRows(table string, self int) string {
	var b bytes.Buffer
	n := 0
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid != self || filepath.Base(strings.Join(fields[3:], " ")) != "osascript" {
			continue
		}
		n++
		fmt.Fprintf(&b, "pid %s running for %s\n", fields[0], fields[2])
	}
	return fmt.Sprintf("count: %d\n", n) + b.String()
}
