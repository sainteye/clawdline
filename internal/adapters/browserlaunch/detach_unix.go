//go:build darwin || linux

package browserlaunch

import (
	"os/exec"
	"syscall"
)

// detach starts the browser in its own session, so it outlives the shell
// that ran `clawdline browser launch`, or the daemon that answered
// POST /v1/browser/launch.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
