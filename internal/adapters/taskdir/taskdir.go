// Package taskdir is the exchange between this daemon and a child: a directory
// holding the brief on the way in and the result on the way out.
//
// A file rather than a socket because the contract has to be readable by a
// person and by any assistant on any platform, and because a file survives both
// ends restarting. The Swift app puts these under /tmp; this uses the daemon's
// own durable state root, which the capability matrix already records as a
// requirement for a non-Mac runtime.
package taskdir

import (
	"path/filepath"
)

type Root struct{ Dir string }

func New(stateDir string) Root { return Root{Dir: filepath.Join(stateDir, "tasks")} }

func (r Root) Path(id string) string { return filepath.Join(r.Dir, id) }
