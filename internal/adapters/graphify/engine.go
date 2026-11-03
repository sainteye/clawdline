package graphify

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

//go:embed parser-requirements.txt
var parserRequirements string

//go:embed local-parser.py
var localParser string

// The environment is an allowlist, never a filtered copy of the daemon's keys.
// Package preparation can download fixed wheels; project parsing has the audit
// hook and receives no credentials, proxies, project config or user home.
func parserEnvironment(home string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + home, "TMP=" + home, "TEMP=" + home,
		"UV_PYTHON_DOWNLOADS=never", "UV_NO_CONFIG=1", "GRAPHIFY_BACKEND=none", "PYTHONDONTWRITEBYTECODE=1", "LC_ALL=C",
		"OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1", "MKL_NUM_THREADS=1"}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return env
}

type cappedOutput struct {
	mu    sync.Mutex
	bytes int
	data  []byte
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := min(len(p), max(0, BuildLogBytesLimit-w.bytes))
	w.data = append(w.data, p[:n]...)
	w.bytes += n
	return len(p), nil
}

// Disk checks bound retained runtime and operation output. They are polled while
// a child runs, so a process can overshoot between checks; completion is checked
// synchronously too. No claim of an OS filesystem quota is made.
func parserDiskSize(ctx context.Context, dir string) error {
	return parserDiskSizeFS(ctx, os.DirFS(dir))
}
func parserDiskSizeFS(ctx context.Context, dir fs.FS) error {
	var size int64
	entries := 0
	err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			if path != "." && os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > MaxScanEntries {
			return BuildError("runtime_limit")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			// AST caches publish temporary files with rename while this
			// read-only budget scan runs. A vanished entry holds no bytes.
			if os.IsNotExist(e) {
				return nil
			}
			return e
		}
		size += st.Size()
		if size > RuntimeBytesLimit {
			return BuildError("runtime_limit")
		}
		return nil
	})
	return err
}

func (b *Builder) runParserCommand(ctx context.Context, home, command string, args ...string) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(child, command, args...)
	readParent, writeParent, err := os.Pipe()
	if err != nil {
		return err
	}
	defer readParent.Close()
	defer writeParent.Close()
	cmd.Stdin = readParent
	cmd.Dir = home
	cmd.Env = parserEnvironment(home)
	var output cappedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	// The parser refuses child processes; uv accepts only wheels and disables
	// source builds/keyring runners. Cancel kills this exact command PID.
	// The parser also watches stdin EOF if its daemon parent disappears.
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(time.Duration(BuildPollMillisecondsLimit) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return fmt.Errorf("%w: %s", err, output.data)
			}
			return parserDiskSize(ctx, b.cache)
		case <-ticker.C:
			if err := parserDiskSize(child, b.cache); err != nil {
				cancel()
				<-done
				return err
			}
		}
	}
}

func privatePython(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, "bin", "python")
}

func (b *Builder) prepareParser(ctx context.Context, home string) (string, error) {
	sum := sha256.Sum256([]byte(parserRequirements))
	version := hex.EncodeToString(sum[:])
	installed := filepath.Join(b.cache, "runtime-"+version)
	marker := filepath.Join(installed, "clawdline-parser-lock.txt")
	if data, err := os.ReadFile(marker); err == nil && string(data) == parserRequirements {
		if err = b.runParserCommand(ctx, home, privatePython(installed), "-I", "-B", "-c", `import importlib.metadata as m,sys
for line in sys.argv[1].splitlines():
 if not line or line.startswith('#'): continue
 name,version=line.split()[0].split('==')
 assert m.version(name)==version
`, parserRequirements); err == nil {
			return privatePython(installed), nil
		}
		return "", BuildError("runtime_unverified")
	}
	if _, err := os.Lstat(installed); !os.IsNotExist(err) {
		return "", BuildError("runtime_unverified")
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		return "", BuildError("prerequisite_missing")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		return "", BuildError("prerequisite_missing")
	}
	if err = b.runParserCommand(ctx, home, python, "-I", "-B", "-c", "import sys; assert sys.version_info >= (3,12)"); err != nil {
		return "", BuildError("prerequisite_missing")
	}
	// The runtime is created at its final path because venv scripts contain
	// absolute paths. It is task-owned only after Mkdir succeeds; partial setup
	// is removed on failure and is never used without its completion marker.
	if err = os.Mkdir(installed, 0700); err != nil {
		return "", BuildError("runtime_unverified")
	}
	completed := false
	defer func() {
		if !completed {
			os.RemoveAll(installed)
		}
	}()
	if err = b.runParserCommand(ctx, home, uv, "--no-config", "venv", "--no-project", "--no-python-downloads", "--python", python, installed); err != nil {
		return "", fmt.Errorf("%w: %v", BuildError("preparation_failed"), err)
	}
	requirements := filepath.Join(home, "parser-requirements.txt")
	if err = os.WriteFile(requirements, []byte(parserRequirements), 0600); err != nil {
		return "", err
	}
	if err = b.runParserCommand(ctx, home, uv, "--no-config", "pip", "sync", "--python", privatePython(installed), "--no-python-downloads", "--no-cache", "--only-binary", ":all:", "--require-hashes", "--strict", "--keyring-provider", "disabled", "--index-url", "https://pypi.org/simple", requirements); err != nil {
		return "", fmt.Errorf("%w: %v", BuildError("preparation_failed"), err)
	}
	if err = os.WriteFile(marker, []byte(parserRequirements), 0600); err != nil {
		return "", err
	}
	completed = true
	return privatePython(installed), nil
}

func (b *Builder) extract(ctx context.Context, input, output string, phase func(string)) error {
	phase("preparing")
	home := filepath.Join(filepath.Dir(input), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		return err
	}
	python, err := b.prepareParser(ctx, home)
	if err != nil {
		return err
	}
	entry := filepath.Join(home, "local-parser.py")
	if err = os.WriteFile(entry, []byte(localParser), 0600); err != nil {
		return err
	}
	phase("parsing")
	if err = b.runParserCommand(ctx, home, python, "-I", "-B", entry, input, output, strconv.Itoa(BuildWorkersLimit)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var typed BuildError
		if errors.As(err, &typed) {
			return err
		}
		return fmt.Errorf("%w: %v", BuildError("parser_failed"), err)
	}
	return os.Rename(filepath.Join(output, "graphify-out", "graph.json"), filepath.Join(output, "graph.json"))
}

// Avoid retaining graph output in callers that only need acceptance and status.
var _ io.Writer = (*cappedOutput)(nil)
