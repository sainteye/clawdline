package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// Pending is an update that has been handed to the supervisor and not yet
// settled: pending.json. Its existence is what says an update is under way,
// to the boot guard, to a resumed supervisor and to a second apply.
type Pending struct {
	From       string `json:"from"`
	To         string `json:"to"`
	FromCommit string `json:"from_commit,omitempty"`
	ToCommit   string `json:"to_commit"`
	// Deadline is when the new release's boot guard gives the update up.
	Deadline time.Time `json:"deadline"`
	// Attempts counts the new release's starts while this is pending.
	Attempts int `json:"attempts"`
	// SupervisorRuns counts the supervisor's starts for this update.
	SupervisorRuns int `json:"supervisor_runs"`
	// NonAdditive is the new manifest's non_additive_migration: only then is
	// Snapshot restored on a rollback.
	NonAdditive bool   `json:"non_additive_migration,omitempty"`
	Snapshot    string `json:"snapshot,omitempty"`
	// Phase is "switch" until the supervisor decides to roll back, then
	// "rollback", so a supervisor started again continues what was decided.
	Phase  string                     `json:"phase"`
	Reason *contract.UpdateApplyError `json:"reason,omitempty"`
	// StagedApp is the unpacked macOS app bundle, when there is one.
	StagedApp string `json:"staged_app,omitempty"`
}

const (
	phaseSwitch   = "switch"
	phaseRollback = "rollback"
)

// writeJSON writes v to path atomically: a reader sees the old file or the
// new one, never half of either.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// readJSON reads path into v. os.ErrNotExist passes through unwrapped so a
// caller can tell "none" from "could not read".
func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateFileBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxStateFileBytes {
		return fmt.Errorf("%s is longer than %d bytes", filepath.Base(path), maxStateFileBytes)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// ReadPending is pending.json; ok false when no update is pending.
func (e Env) ReadPending() (Pending, bool, error) {
	var p Pending
	err := readJSON(e.pendingPath(), &p)
	if errors.Is(err, os.ErrNotExist) {
		return Pending{}, false, nil
	}
	if err != nil {
		return Pending{}, false, err
	}
	return p, true, nil
}

func (e Env) writePending(p Pending) error { return writeJSON(e.pendingPath(), p) }

func (e Env) clearPending() error {
	err := os.Remove(e.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ReadApply is the last recorded apply state; idle when none was recorded.
// An unreadable file is reported, never read as idle.
func (e Env) ReadApply() (contract.UpdateApply, error) {
	var a contract.UpdateApply
	err := readJSON(e.statusPath(), &a)
	if errors.Is(err, os.ErrNotExist) {
		return contract.UpdateApply{State: contract.UpdateApplyStateIdle}, nil
	}
	if err != nil {
		return contract.UpdateApply{}, err
	}
	return a, nil
}

// record writes the apply state, stamped now.
func (e Env) record(state contract.UpdateApplyState, from, to string, why *contract.UpdateApplyError, stagedApp string) error {
	return writeJSON(e.statusPath(), contract.UpdateApply{
		State: state, From: from, To: to, Error: why, At: e.now().Format(time.RFC3339), StagedApp: stagedApp,
	})
}

// Failed is the versions that rolled back, oldest first.
func (e Env) Failed() ([]string, error) {
	var list []string
	err := readJSON(e.failedPath(), &list)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return list, err
}

// HasFailed says whether version rolled back before.
func (e Env) HasFailed(version string) (bool, error) {
	list, err := e.Failed()
	if err != nil {
		return false, err
	}
	for _, v := range list {
		if v == version {
			return true, nil
		}
	}
	return false, nil
}

// markFailed remembers version, keeping the newest FailedVersionsLimit.
func (e Env) markFailed(version string) error {
	list, err := e.Failed()
	if err != nil {
		// An unreadable list is replaced: losing the old entries makes
		// auto-apply try a version again, which is the lesser harm than
		// never recording this one.
		list = nil
	}
	out := make([]string, 0, len(list)+1)
	for _, v := range list {
		if v != version {
			out = append(out, v)
		}
	}
	out = append(out, version)
	if len(out) > FailedVersionsLimit {
		out = out[len(out)-FailedVersionsLimit:]
	}
	return writeJSON(e.failedPath(), out)
}

// lockFile is update.lock's body.
type lockFile struct {
	PID int       `json:"pid"`
	At  time.Time `json:"at"`
}

// lock takes update.lock, or refuses update_in_progress. A lock older than
// LockStaleSecondsLimit was left by a process that died mid-download and is
// taken over. The returned func releases it.
func (e Env) lock() (func(), error) {
	if err := os.MkdirAll(e.UpdateDir(), 0o700); err != nil {
		return nil, err
	}
	for try := 0; try < 2; try++ {
		f, err := os.OpenFile(e.lockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			b, _ := json.Marshal(lockFile{PID: os.Getpid(), At: e.now()})
			_, werr := f.Write(b)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(e.lockPath())
				return nil, fmt.Errorf("writing update.lock: %v %v", werr, cerr)
			}
			path := e.lockPath()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		var held lockFile
		info, serr := os.Stat(e.lockPath())
		if rerr := readJSON(e.lockPath(), &held); rerr != nil && serr == nil {
			held.At = info.ModTime()
		}
		if e.now().Sub(held.At) < LockStaleSecondsLimit*time.Second {
			return nil, fail(CodeUpdateInProgress, "another update holds %s since %s (pid %d)",
				e.lockPath(), held.At.UTC().Format(time.RFC3339), held.PID)
		}
		os.Remove(e.lockPath())
	}
	return nil, fail(CodeUpdateInProgress, "could not take %s", e.lockPath())
}

// restoreMarker asks the next start of release From to put Snapshot back
// before it opens the store: restore.json.
type restoreMarker struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Snapshot string `json:"snapshot"`
}
