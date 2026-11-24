//go:build windows

package projectassistantacp

import (
	"context"
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
func (p *Process) captureOwned(context.Context) ([]processIdentity, error) {
	return nil, refusal(pa.SandboxUnavailable, "windows_job_supervisor")
}
func (p *Process) signalOwned(context.Context, []processIdentity, bool) error {
	return refusal(pa.SandboxUnavailable, "windows_job_supervisor")
}
func (p *Process) identities(context.Context) ([]processIdentity, error) {
	return nil, refusal(pa.SandboxUnavailable, "windows_job_supervisor")
}

// No census runs here, so nothing is ever attributed to a reaper.
const orphansGoToInit = false
