//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// HolderOf asks the system which process listens on a TCP port.
//
// ErrNoHolder is the system saying nobody does. Any other error is the lookup
// failing, which says nothing about the port either way.
func HolderOf(ctx context.Context, port int) (Holder, error) {
	pid, err := listenerPID(ctx, port)
	if err != nil {
		return Holder{}, err
	}
	h := Holder{PID: pid}
	cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "lstart=,command=", "-p", strconv.Itoa(pid))
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	if out, err := cmd.Output(); err == nil {
		if at, command, ok := parsePSStart(strings.TrimSpace(string(out)), time.Local); ok {
			h.Started, h.Command = at, command
		}
	}
	return h, nil
}

func listenerPID(ctx context.Context, port int) (int, error) {
	lsof := "/usr/sbin/lsof"
	if runtime.GOOS != "darwin" {
		found, err := exec.LookPath("lsof")
		if err != nil {
			// A Linux box without lsof usually has ss.
			return ssListenerPID(ctx, port)
		}
		lsof = found
	}
	cmd := exec.CommandContext(ctx, lsof, "-nP", "-w", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if pids := parseLsofPIDs(string(out)); len(pids) > 0 {
		return pids[0], nil
	}
	// lsof exits 1 with nothing printed when nothing matched, and also when
	// it could not look; only the first is an answer.
	var exit *exec.ExitError
	if err == nil || (errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0) {
		return 0, ErrNoHolder
	}
	return 0, fmt.Errorf("lsof: %w", err)
}

func ssListenerPID(ctx context.Context, port int) (int, error) {
	out, err := exec.CommandContext(ctx, "ss", "-Hltnp", "sport = :"+strconv.Itoa(port)).Output()
	if err != nil {
		return 0, fmt.Errorf("neither lsof nor ss could be asked: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return 0, ErrNoHolder
	}
	if pid, ok := parseSSListener(string(out)); ok {
		return pid, nil
	}
	return 0, errors.New("ss lists a listener on that port and not its pid (it belongs to another user)")
}
