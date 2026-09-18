package taskdir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// The broker half of a task directory: the brief on the way in, and the three
// files a child writes on the way out.
//
//	task.json          the brief, written once, 0600
//	CHILD.md           the protocol the child is told to follow, 0600
//	progress.json      a material boundary change, rewritten in place
//	result.json.ready  the child's validator said the bytes below are valid
//	result.json        the completion signal, and the only one
//	artifacts/         what the child wants kept
//
// `result.json` is the completion signal because it is the one the child
// creates by rename: a half-written file is never mistaken for a finished one.
// The `.ready` marker exists for the other half of that race — a child whose
// shell died between validating and renaming — and it binds the exact bytes it
// validated, so age alone never becomes consent.

// ErrNoResult is the ordinary state of work still being done. It is returned
// rather than a zero value so that "nothing yet" cannot be read as "empty
// result".
var ErrNoResult = errors.New("no result yet")

// Brief is what goes into task.json. The field names are the protocol: a child
// reads this file with its own tools, and every assistant on this machine has
// been told these names.
type Brief struct {
	Protocol       int      `json:"clawdline_protocol"`
	TaskID         string   `json:"task_id"`
	Kind           string   `json:"kind"`
	Assistant      string   `json:"assistant"`
	PermissionMode string   `json:"permission_mode"`
	Claims         []string `json:"claims"`
	Isolation      string   `json:"isolation"`
	ProjectDir     string   `json:"project_dir"`
	Title          string   `json:"title"`
	Instructions   string   `json:"instructions"`
	Deliverables   []string `json:"deliverables,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes"`
	CreatedAt      string   `json:"created_at"`
	Root           *RootRef `json:"root,omitempty"`
}

// RootRef is who the task is working for. `poll_only` is carried even when
// false because a reader that has to infer it cannot tell an attended root from
// one that was never recorded.
type RootRef struct {
	SessionID  string `json:"session_id"`
	Assistant  string `json:"assistant"`
	ProjectDir string `json:"project_dir"`
	Label      string `json:"label"`
	PollOnly   bool   `json:"poll_only"`
}

// Result is what a child writes when it is finished, or has failed for good.
//
// Only `status` and `summary` are required of it. The rest is what the child
// chose to say about itself, and it is kept verbatim in `Extra` rather than
// dropped: a broker that silently discards a field the protocol documents is
// how a protocol stops being one.
type Result struct {
	Protocol   int             `json:"clawdline_protocol"`
	TaskID     string          `json:"task_id"`
	Secret     string          `json:"task_secret"`
	Status     string          `json:"status"`
	Summary    string          `json:"summary"`
	Symbols    []string        `json:"symbols,omitempty"`
	Artifacts  []string        `json:"artifacts,omitempty"`
	Review     json.RawMessage `json:"review,omitempty"`
	Verify     *Verification   `json:"verification,omitempty"`
	FinishedAt string          `json:"finished_at,omitempty"`
}

// Verification is the child's own account of what it ran.
type Verification struct {
	Runs    int    `json:"runs"`
	Seconds int    `json:"seconds"`
	Last    string `json:"last"`
	Scope   string `json:"scope"`
}

// Progress is one material boundary change. The child replaces the whole file
// each time; the broker keeps the newest few.
type Progress struct {
	Secret string `json:"task_secret"`
	Note   string `json:"note"`
}

// ready is the finalization marker the child's validator writes.
type ready struct {
	Protocol int    `json:"clawdline_protocol"`
	TaskID   string `json:"task_id"`
	Ready    bool   `json:"finalization_ready"`
	SHA256   string `json:"result_sha256"`
}

// Write creates the task directory and puts the brief and the child's
// protocol in it.
//
// The directory is 0700 and each file 0600, in that order, so nothing is ever
// briefly world-readable. The secret is not written here at all: it travels to
// the child in the one message the broker types, and what this directory holds
// is the part a person may read over somebody's shoulder.
func (r Root) Write(brief Brief, childMD string) (string, error) {
	dir := r.Path(brief.TaskID)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(brief, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeOwned(filepath.Join(dir, "task.json"), append(body, '\n')); err != nil {
		return "", err
	}
	if childMD != "" {
		if err := writeOwned(filepath.Join(dir, "CHILD.md"), []byte(childMD)); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// writeOwned writes a file only this user can read, without a window where it
// is readable by anybody else: the mode is passed to the create, not applied
// after it.
func writeOwned(path string, body []byte) error {
	tmp := path + ".writing"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadResult reads a finished child's own account of itself.
//
// Three answers, not two. A missing file is ErrNoResult — the ordinary state of
// work in progress. A file that will not parse is an error naming that, because
// a child that wrote something unreadable has not delivered and must not be
// recorded as having failed for a reason nobody can read.
func (r Root) ReadResult(id string) (Result, []byte, error) {
	path := filepath.Join(r.Path(id), "result.json")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{}, nil, ErrNoResult
		}
		return Result{}, nil, err
	}
	var out Result
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, body, err
	}
	return out, body, nil
}

// ReadReady answers whether a child validated a result and then stopped before
// renaming it.
//
// The marker is only believed when it names this task and when the bytes still
// on disk hash to what it recorded. Anything else — a stale marker, an edited
// tmp file — is no marker at all: age is not consent, and this is the clause
// that says so.
func (r Root) ReadReady(id string) (Result, []byte, bool) {
	dir := r.Path(id)
	markerBody, err := os.ReadFile(filepath.Join(dir, "result.json.ready"))
	if err != nil {
		return Result{}, nil, false
	}
	var m ready
	if json.Unmarshal(markerBody, &m) != nil || !m.Ready || m.TaskID != id || m.SHA256 == "" {
		return Result{}, nil, false
	}
	body, err := os.ReadFile(filepath.Join(dir, "result.json.tmp"))
	if err != nil {
		return Result{}, nil, false
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return Result{}, nil, false
	}
	var out Result
	if json.Unmarshal(body, &out) != nil {
		return Result{}, nil, false
	}
	return out, body, true
}

// AdoptReady publishes a validated result its child never renamed.
func (r Root) AdoptReady(id string) error {
	dir := r.Path(id)
	if err := os.Rename(filepath.Join(dir, "result.json.tmp"), filepath.Join(dir, "result.json")); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, "result.json.ready"))
}

// ReadProgress reads the file a child writes when it cannot reach the broker
// over loopback. It carries the task secret, which the caller verifies: this
// reader does not, because a file under a directory the child owns proves only
// that somebody who can write there wrote it.
func (r Root) ReadProgress(id string) (Progress, os.FileInfo, bool) {
	path := filepath.Join(r.Path(id), "progress.json")
	info, err := os.Stat(path)
	if err != nil {
		return Progress{}, nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Progress{}, nil, false
	}
	var out Progress
	if json.Unmarshal(body, &out) != nil {
		return Progress{}, nil, false
	}
	return out, info, true
}

// Artifacts is where a child puts what it wants kept.
func (r Root) Artifacts(id string) string { return filepath.Join(r.Path(id), "artifacts") }

// Modified is when the directory last changed, for a reader deciding whether
// anything has happened.
func (r Root) Modified(id string) (time.Time, bool) {
	info, err := os.Stat(r.Path(id))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
