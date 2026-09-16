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
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline-go/internal/domain/task"
)

type Root struct{ Dir string }

func New(stateDir string) Root { return Root{Dir: filepath.Join(stateDir, "tasks")} }

func (r Root) Path(id string) string { return filepath.Join(r.Dir, id) }

// Create makes the task directory and writes the brief.
//
// The directory is 0700 and the file 0600, created in that order, so the brief
// is never briefly world-readable. The secret is deliberately not written
// here: it travels one route only, and a file under a shared root is not it.
func (r Root) Create(t task.Task) (string, error) {
	dir := r.Path(t.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

// Result reads what the child wrote, if it has written anything.
//
// Absence is not failure: a task that has not answered yet and one that never
// will look identical here, and telling them apart is the caller's job with a
// clock, not this reader's with a guess.
func (r Root) Result(id string) (task.Result, bool) {
	body, err := os.ReadFile(filepath.Join(r.Path(id), "result.json"))
	if err != nil {
		return task.Result{}, false
	}
	var out task.Result
	if json.Unmarshal(body, &out) != nil {
		return task.Result{}, false
	}
	return out, true
}
