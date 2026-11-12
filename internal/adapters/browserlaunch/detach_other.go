//go:build !darwin && !linux

package browserlaunch

import "os/exec"

func detach(*exec.Cmd) {}
