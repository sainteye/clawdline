//go:build !darwin && !linux

package antigravityprocess

import "os/exec"

func prepareProcess(*exec.Cmd) error { return failure("unsupported_platform", NotWritten) }
func killProcess(*exec.Cmd) error    { return failure("unsupported_platform", NotWritten) }
func disabledBrowser() string        { return "" }
