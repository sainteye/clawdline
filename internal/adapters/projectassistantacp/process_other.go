//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package projectassistantacp

import (
	"context"
	"os/exec"
	"path/filepath"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func absoluteExecutable(path string) bool { return filepath.IsAbs(path) }
func configureProcess(*exec.Cmd) error    { return refusal(pa.SandboxUnavailable, "process_supervisor") }
func (p *Process) captureOwned(context.Context) ([]processIdentity, error) {
	return nil, refusal(pa.SandboxUnavailable, "process_supervisor")
}
func (p *Process) signalOwned(context.Context, []processIdentity, bool) error {
	return refusal(pa.SandboxUnavailable, "process_supervisor")
}
