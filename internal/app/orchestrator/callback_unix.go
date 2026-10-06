//go:build darwin || linux

package orchestrator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/supervisor"
)

// callbackSupported: this platform can stop a command's whole tree.
const callbackSupported = true

// callbackProcess is a started callback's wrapper.
type callbackProcess struct {
	cmd  *exec.Cmd
	pid  int
	pgid int
}

// startCallbackProcess starts r's command under the wrapper, as the leader of
// a new process group, with its output going straight to output.log — a file,
// not a pipe, so nothing it writes depends on this daemon staying alive — and
// holding run.lock, which every process it starts inherits.
//
// live is asked once the lock is held, and nothing is started unless it says
// the task is still live: the lock is the fence a settler takes too
// (tendCallback), so a task settled while this waited to start stays
// unstarted.
func startCallbackProcess(r Record, files callbackFiles, live func() error) (*callbackProcess, error) {
	if err := outside(); err != nil {
		return nil, err
	}
	out, err := os.OpenFile(files.log(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	lock, err := os.OpenFile(files.lock(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("its run.lock is already held: " + err.Error())
	}
	if err := live(); err != nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		return nil, err
	}
	// The attempt marker, durable before the command can exist. Without it
	// "no exit status and nobody holds the lock" is both "never started" and
	// "started, and died with the machine" — and a recovery that guessed
	// the first would run a deploy twice.
	if err := writeSynced(files.attempt(), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n")); err != nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		return nil, errors.New("could not record the attempt: " + err.Error())
	}
	args := append([]string{"-c", callbackWrapper, "sh"}, r.Callback.Argv...)
	cmd := exec.Command("/bin/sh", args...)
	cmd.Dir = r.Callback.Dir
	cmd.Env = callbackEnviron(r)
	cmd.Stdin = nil
	cmd.Stdout = out
	cmd.Stderr = out
	// fd 3 in the command: the open lock. It is not close-on-exec there, so
	// every descendant that does not close it on purpose keeps the lock.
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		return nil, err
	}
	// This process's copy of the lock and the log are closed by the defers;
	// the lock stays held by the open file the command inherited.
	return &callbackProcess{cmd: cmd, pid: cmd.Process.Pid, pgid: cmd.Process.Pid}, nil
}

// callbackEnviron is the command's whole environment: what the caller handed
// over, and the callback's own id and directory. Nothing of the daemon's.
func callbackEnviron(r Record) []string {
	env := []string{}
	for k, v := range r.Callback.Env {
		if strings.HasPrefix(k, "CLAWDLINE_") {
			continue
		}
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return append(env, "CLAWDLINE_CALLBACK_TASK_ID="+r.ID, "CLAWDLINE_CALLBACK_DIR="+r.Dir)
}

// wait reaps the wrapper and says how it ended.
func (p *callbackProcess) wait() (*int, string) {
	err := p.cmd.Wait()
	st := p.cmd.ProcessState
	if st == nil {
		_ = err
		return nil, ""
	}
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return nil, ws.Signal().String()
	}
	code := st.ExitCode()
	if code < 0 {
		return nil, ""
	}
	return &code, ""
}

// lockHeld reports whether some process holds path's flock. A missing file is
// a lock nobody ever took.
func lockHeld(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// stopGroup stops the process group led by pgid: TERM, then KILL.
func stopGroup(pgid int, grace time.Duration) error {
	if err := outside(); err != nil {
		return err
	}
	return (&supervisor.Process{PGID: pgid}).Stop(grace)
}

// writeSynced writes path and its bytes to disk before it answers.
func writeSynced(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// And the directory entry, so the marker survives the power loss it is
	// there for.
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// fence takes path's flock without waiting, creating the file if it is not
// there, and answers the file holding it — nil when somebody else holds it.
// Held, it keeps a start from beginning (startCallbackProcess) while the
// holder decides the task.
func fence(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		f.Close()
		return nil, nil
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// unfence gives a fence back.
func unfence(f *os.File) {
	if f != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// sameGroup reports whether pid still leads process group pgid.
func sameGroup(pid, pgid int) bool {
	got, err := syscall.Getpgid(pid)
	return err == nil && got == pgid
}
