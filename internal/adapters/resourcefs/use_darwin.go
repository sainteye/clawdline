//go:build darwin

package resourcefs

import (
	"context"
	"errors"
	"os/exec"
)

type presenceWriter struct{ seen bool }

func (w *presenceWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.seen = true
	}
	return len(p), nil
}

// UsePresence reads only whether a process holds an entry under the exact
// managed directory. Discarding pid text keeps the subprocess output bounded
// without collecting private paths or a machine-wide open-file table.
func UsePresence(ctx context.Context, path string) string {
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-n", "-P", "-t", "+D", path)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var output, warnings presenceWriter
	cmd.Stdout = &output
	cmd.Stderr = &warnings
	err := cmd.Run()
	if output.seen {
		return "present"
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && !warnings.seen && ctx.Err() == nil {
		return "absent"
	}
	return "unknown"
}
