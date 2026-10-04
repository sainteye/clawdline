package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
	"github.com/sainteye/clawdline/internal/contract"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// Exit codes of `clawdline update` (docs/updates.md).
const (
	updateExitCurrent = 0
	updateExitBehind  = 10
	updateExitUnknown = 3
)

// updateExit is the exit code for a state: 0 when nothing newer is known, 10
// when the cloud has something this machine does not, 3 when it cannot say.
func updateExit(s contract.UpdateState) int {
	switch s {
	case contract.UpdateStateCurrent, contract.UpdateStateAhead:
		return updateExitCurrent
	case contract.UpdateStateUpdateAvailable, contract.UpdateStateDiffers:
		return updateExitBehind
	}
	return updateExitUnknown
}

// updateCommand is `clawdline update [--json] [--apply [--force]]`: whether
// this machine trails the cloud's latest build, and on Linux inside a source
// checkout, deploying it.
func updateCommand(args []string) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	asJSON := fs.Bool("json", false, "print GET /v1/update's body")
	apply := fs.Bool("apply", false, "deploy the latest build (Linux, inside a source checkout)")
	force := fs.Bool("force", false, "with --apply: deploy even when this machine is current or ahead")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: clawdline update [--json] [--apply [--force]] [--port n]")
		os.Exit(2)
	}
	st := updateStatus(*port)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(st)
	} else {
		printUpdate(os.Stdout, st)
	}
	if !*apply {
		os.Exit(updateExit(st.State))
	}
	os.Exit(applyUpdate(os.Stdout, os.Stderr, st, *force, runtime.GOOS, ".", execRunner{}))
}

// updateStatus asks the daemon; when no daemon answers the route, it computes
// the same answer here, with one fetch.
func updateStatus(port int) contract.UpdateStatus {
	if b, err := openBroker(port); err == nil {
		b.client.Timeout = 5 * time.Second
		if a, err := b.request(http.MethodGet, "/v1/update", nil, nil, ""); err == nil && a.Status == http.StatusOK {
			var st contract.UpdateStatus
			if json.Unmarshal(a.Body, &st) == nil && st.State != "" {
				return st
			}
		}
	}
	return directUpdateStatus(context.Background(), httptransport.WebRoot())
}

func directUpdateStatus(ctx context.Context, webRoot string) contract.UpdateStatus {
	c := updatecheck.New(webRoot)
	c.Refresh(ctx)
	return c.Status()
}

func printUpdate(w io.Writer, st contract.UpdateStatus) {
	line := func(name string, b contract.BuildStamp) {
		stamp, at := b.Stamp, b.CommittedAt
		if stamp == "" {
			stamp = "(unknown)"
		}
		if at == "" {
			at = "time unknown"
		}
		fmt.Fprintf(w, "%-8s %s  (%s)\n", name, stamp, at)
	}
	line("running", st.Running)
	line("latest", st.Latest)
	fmt.Fprintf(w, "state    %s", st.State)
	if st.Reason != "" {
		fmt.Fprintf(w, " — %s", st.Reason)
	}
	fmt.Fprintln(w)
	if st.Error != "" {
		fmt.Fprintf(w, "last check failed: %s\n", st.Error)
	}
	fmt.Fprintf(w, "source   %s\n", st.SourceURL)
}

// runner runs a command in dir; a test replaces it.
type runner interface {
	run(dir string, stdout, stderr io.Writer, name string, args ...string) error
}

type execRunner struct{}

func (execRunner) run(dir string, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stdout, stderr
	return cmd.Run()
}

// applyUpdate deploys st.Latest on Linux from the source checkout around dir.
// On macOS it prints the commands, because a bundle is replaced whole and never
// while it runs (tools/package-macos.sh), and exits non-zero.
func applyUpdate(stdout, stderr io.Writer, st contract.UpdateStatus, force bool, goos, dir string, r runner) int {
	stamp := st.Latest.Stamp
	if stamp == "" {
		fmt.Fprintln(stderr, "clawdline: the cloud's latest build is not known, so there is nothing to apply")
		return updateExitUnknown
	}
	if (st.State == contract.UpdateStateCurrent || st.State == contract.UpdateStateAhead) && !force {
		fmt.Fprintf(stderr, "clawdline: this machine is %s; nothing to apply (--force deploys %s anyway)\n", st.State, stamp)
		return 1
	}
	switch goos {
	case "linux":
	case "darwin":
		fmt.Fprintln(stderr, "clawdline: --apply does not replace an app bundle; run these from a source checkout:")
		fmt.Fprintf(stdout, "git fetch origin main\n")
		fmt.Fprintf(stdout, "git worktree add -f \"$TMPDIR/clawdline-%s\" %s\n", short(stamp), stamp)
		fmt.Fprintf(stdout, "cd \"$TMPDIR/clawdline-%s\" && tools/package-macos.sh\n", short(stamp))
		return 1
	default:
		fmt.Fprintf(stderr, "clawdline: --apply is built for Linux only, not %s (docs/updates.md)\n", goos)
		return 1
	}
	var top strings.Builder
	if err := r.run(dir, &top, io.Discard, "git", "rev-parse", "--show-toplevel"); err != nil {
		fmt.Fprintln(stderr, "clawdline: --apply runs inside a Clawdline source checkout, and this directory is not one")
		return 1
	}
	root := strings.TrimSpace(top.String())
	deploy := filepath.Join(root, "tools", "deploy-linux-user.sh")
	if _, err := os.Stat(deploy); err != nil {
		fmt.Fprintf(stderr, "clawdline: --apply runs inside a Clawdline source checkout; %s has no tools/deploy-linux-user.sh\n", root)
		return 1
	}
	if err := r.run(root, stdout, stderr, "git", "fetch", "origin", "main"); err != nil {
		fmt.Fprintf(stderr, "clawdline: git fetch origin main failed: %v\n", err)
		return 1
	}
	if err := r.run(root, io.Discard, io.Discard, "git", "merge-base", "--is-ancestor", stamp, "origin/main"); err != nil {
		fmt.Fprintf(stderr, "clawdline: %s is not on origin/main; refusing to deploy it\n", stamp)
		return 1
	}
	if err := r.run(root, stdout, stderr, deploy, "--rev", stamp); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "clawdline: %s: %v\n", deploy, err)
		return 1
	}
	return 0
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
