//go:build darwin || linux

package main

import (
	"os/exec"
	"syscall"
)

// detach starts the browser in its own session, so it outlives the shell
// that ran `clawdline browser launch`.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
