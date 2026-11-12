//go:build windows

package projectassistantacp

import (
	"os/exec"
	"path/filepath"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func absoluteExecutable(path string) bool { return filepath.IsAbs(path) }

// A Windows native mode needs an audited job-object supervisor that assigns
// containment before execution. Never substitute taskkill or PID-name matching.
func configureProcess(*exec.Cmd) error {
	return refusal(pa.SandboxUnavailable, "windows_job_supervisor")
}
func (p *Process) captureOwned() {}
func (p *Process) signalOwned(bool) error {
	return refusal(pa.SandboxUnavailable, "windows_job_supervisor")
}
