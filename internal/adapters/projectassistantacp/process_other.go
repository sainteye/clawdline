//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package projectassistantacp

import (
	"os/exec"
	"path/filepath"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func absoluteExecutable(path string) bool { return filepath.IsAbs(path) }
func configureProcess(*exec.Cmd) error    { return refusal(pa.SandboxUnavailable, "process_supervisor") }
func (p *Process) captureOwned()          {}
func (p *Process) signalOwned(bool) error {
	return refusal(pa.SandboxUnavailable, "process_supervisor")
}
