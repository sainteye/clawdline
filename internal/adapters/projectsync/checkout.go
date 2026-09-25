// Package projectsync reads and writes the parts of a checkout that project
// settings synchronization touches: its origin remote, its untracked
// project-local files, and a fresh clone. Everything policy-shaped — what may
// travel, what a mirror may overwrite — is decided in internal/domain/projectsync.
package projectsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

// gitTimeout bounds one read-only git process: these read a config file or an
// index, so the ceiling is for a git that stopped answering.
const gitTimeout = 10 * time.Second

// CloneTimeout bounds one clone. Registered in internal/domain/capacity.
const CloneTimeout = 10 * time.Minute

// ErrNoRemote is a checkout without an origin.
var ErrNoRemote = errors.New("no origin remote")

// ErrTracked is a carried path that git tracks in this checkout: the
// repository owns it, and a mirror never writes over it.
var ErrTracked = errors.New("tracked by git here")

// ErrLinked is a carried path that passes through a symbolic link here.
var ErrLinked = errors.New("passes through a symbolic link")

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", args[0], err, lastLine(errOut.String()))
	}
	return out.Bytes(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}

// Origin is the checkout's origin URL.
func Origin(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		// `config --get` exits 1 for a missing key, and that is the common case.
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return "", ErrNoRemote
		}
		return "", err
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", ErrNoRemote
	}
	return url, nil
}

// Untracked lists the carried files git does not track in this checkout —
// both untracked and ignored ones, because an ignored skill is just as local.
func Untracked(ctx context.Context, root string) ([]string, error) {
	args := []string{"ls-files", "-z", "--others", "--"}
	args = append(args, domain.Dirs...)
	args = append(args, domain.Files...)
	out, err := git(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && domain.Allowed(p) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Tracked is whether git tracks rel in this checkout.
func Tracked(ctx context.Context, root, rel string) (bool, error) {
	out, err := git(ctx, root, "ls-files", "-z", "--", rel)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimRight(out, "\x00")) > 0, nil
}

// safe resolves rel under root, refusing any existing component that is a
// symbolic link: a link could carry a write outside the checkout.
func safe(root, rel string) (string, error) {
	if !domain.Allowed(rel) {
		return "", fmt.Errorf("%q is not a carried project file", rel)
	}
	parts := strings.Split(rel, "/")
	at := root
	for _, part := range parts {
		at = filepath.Join(at, part)
		info, err := os.Lstat(at)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", ErrLinked
		}
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

// Read is one carried file's content, or nil with no error when it does not
// exist. Anything that is not a regular file, or is larger than the bound, is
// refused.
func Read(root, rel string) ([]byte, error) {
	full, err := safe(root, rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", rel)
	}
	if info.Size() > domain.MaxFileBytes {
		return nil, fmt.Errorf("%q is larger than %d bytes", rel, domain.MaxFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, domain.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > domain.MaxFileBytes {
		return nil, fmt.Errorf("%q is larger than %d bytes", rel, domain.MaxFileBytes)
	}
	return data, nil
}

// Write replaces one carried file whole: a temporary file beside it, synced,
// then renamed, so a reader never sees half of it.
func Write(root, rel string, content []byte) error {
	full, err := safe(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	// MkdirAll may have created components; check none of them is a link now.
	if _, err := safe(root, rel); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(full), ".clawdline-sync-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o644); err == nil {
		if _, err = f.Write(content); err == nil {
			err = f.Sync()
		}
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), full)
}

// Remove deletes one carried file, and the directories under the carried
// places it leaves empty.
func Remove(root, rel string) error {
	full, err := safe(root, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stop := map[string]bool{}
	for _, d := range domain.Dirs {
		stop[filepath.Join(root, filepath.FromSlash(d))] = true
	}
	for dir := filepath.Dir(full); !stop[dir] && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// Clone makes a fresh checkout. It never prompts: a remote that needs
// credentials this machine does not have fails with git's own last line.
func Clone(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, CloneTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--", url, dest)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	cmd.Env = env
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("git clone did not finish within %s", CloneTimeout)
		}
		return fmt.Errorf("git clone: %s", lastLine(errOut.String()))
	}
	return nil
}
