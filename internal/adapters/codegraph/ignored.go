package codegraph

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// ignoredSet is what Git itself declares ignored under one working directory:
// untracked files and whole directories, collapsed by `--directory`, so a
// gitignored release tree of thousands of copies costs one line rather than
// one scan entry per file. Tracked files are never in it, even when they match
// an ignore pattern, because Git keeps tracking them.
type ignoredSet struct {
	files map[string]bool
	dirs  map[string]bool
}

func (s ignoredSet) dir(name string) bool  { return s.dirs[name] }
func (s ignoredSet) file(name string) bool { return s.files[name] }

// The bridge reproduces these arguments exactly (bridge.cjs ignoredPaths), so
// the Go check and the service compute the same inventory. Only the
// repository's own .gitignore files and .git/info/exclude decide: the
// person's global and system configuration are left out because the service
// runs with a private HOME and could not see them. Settings that make Git run
// a program are emptied on the command line, where they outrank the
// repository's .git/config.
var ignoredArgs = []string{"-c", "core.fsmonitor=", "-c", "core.excludesFile=", "-c", "core.hooksPath=" + os.DevNull,
	"ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory"}

func ignoredEnvironment() []string {
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	for _, name := range []string{"PATH", "HOME", "USERPROFILE", "SYSTEMROOT"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// gitIgnored answers an empty set for a directory outside any Git work tree
// or a machine without Git, which keeps the earlier full scan. Any other Git
// failure is an error: guessing either way would change the inventory and
// mark a matched graph stale or a stale one matched.
func gitIgnored(ctx context.Context, root string) (ignoredSet, error) {
	out := ignoredSet{files: map[string]bool{}, dirs: map[string]bool{}}
	command := exec.CommandContext(ctx, "git", ignoredArgs...)
	command.Dir = root
	command.Env = ignoredEnvironment()
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = MaxIgnoredListBytes, 4096
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return out, nil
	}
	if stdout.over {
		return out, BuildError("source_limit")
	}
	if err != nil {
		if strings.Contains(stderr.String(), "not a git repository") {
			return out, nil
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, BuildError("ignore_unavailable")
	}
	for _, name := range bytes.Split(stdout.Bytes(), []byte{0}) {
		if len(name) == 0 {
			continue
		}
		n := string(name)
		if dir, ok := strings.CutSuffix(n, "/"); ok {
			if fs.ValidPath(dir) && dir != "." {
				out.dirs[dir] = true
			}
		} else if fs.ValidPath(n) {
			out.files[n] = true
		}
	}
	return out, nil
}

// limitedBuffer is deliberately not an embedded bytes.Buffer: its promoted
// ReadFrom would let io.Copy bypass Write and the bound with it.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		b.over = true
		return 0, errors.New("output limit")
	}
	return b.buf.Write(p)
}
func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }
