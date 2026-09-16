//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"time"
)

// Process is a running tree. On Windows the equivalent of a process group is a
// Job Object, and this does not create one yet.
type Process struct {
	Cmd  *exec.Cmd
	PGID int
}

// Start refuses rather than starting something it could not stop.
//
// Starting a tree this package cannot take away again is worse than not
// starting it: the caller would believe cancellation works. The Windows
// implementation needs a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, and until it exists this says so.
func Start(name string, args []string, dir string, env []string) (*Process, error) {
	return nil, fmt.Errorf(
		"supervisor_unsupported: starting a supervised tree on Windows needs a Job Object, which is not implemented yet")
}

func (p *Process) Stop(grace time.Duration) error {
	return fmt.Errorf("supervisor_unsupported: no Job Object to end")
}

func (p *Process) Detach() error {
	return fmt.Errorf("supervisor_unsupported: no Job Object to release")
}
