package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
	"github.com/sainteye/clawdline/internal/config"
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

// updateCommand is `clawdline update [--json] [--apply [--version vX] [--force]]`:
// whether this machine trails the latest build, and installing it — a signed
// release on a release install, the latest commit on Linux inside a source
// checkout. `clawdline update finish` is the supervisor an update starts.
func updateCommand(args []string) {
	if len(args) > 0 && args[0] == "finish" {
		os.Exit(updateFinishCommand(args[1:]))
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	asJSON := fs.Bool("json", false, "print GET /v1/update's body")
	apply := fs.Bool("apply", false, "install the latest release (a release install), or deploy the latest build (Linux, inside a source checkout)")
	want := fs.String("version", "", "with --apply on a release install: install this release, vX.Y.Z")
	force := fs.Bool("force", false, "with --apply: install even when this machine is current or ahead")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: clawdline update [--json] [--apply [--version vX.Y.Z] [--force]] [--port n]")
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
	if st.InstallKind == contract.UpdateInstallKindRelease {
		os.Exit(applyRelease(os.Stdout, os.Stderr, *port, *want, *force))
	}
	if *want != "" {
		fmt.Fprintln(os.Stderr, "clawdline: --version installs a release, and this is not a release install")
		os.Exit(2)
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

// directReleaseStatus is GET /v1/update for a release install with no daemon
// answering: one manifest read, here.
func directReleaseStatus(ctx context.Context) (contract.UpdateStatus, bool) {
	exe, err := os.Executable()
	if err != nil {
		return contract.UpdateStatus{}, false
	}
	env, err := updater.DefaultEnv(config.Dir())
	if err != nil {
		return contract.UpdateStatus{}, false
	}
	if _, kind := updater.Running(env.Layout, exe); kind != install.KindRelease {
		return contract.UpdateStatus{}, false
	}
	d := updater.NewDaemon(env, exe)
	d.Checker.Refresh(ctx)
	return d.Status(), true
}

func directUpdateStatus(ctx context.Context, webRoot string) contract.UpdateStatus {
	if st, ok := directReleaseStatus(ctx); ok {
		return st
	}
	c := updatecheck.New(webRoot)
	c.Refresh(ctx)
	return c.Status()
}

func printUpdate(w io.Writer, st contract.UpdateStatus) {
	if st.InstallKind != "" {
		fmt.Fprintf(w, "install  %s", st.InstallKind)
		if st.Channel != "" {
			fmt.Fprintf(w, " (channel %s)", st.Channel)
		}
		fmt.Fprintln(w)
	}
	line := func(name string, b contract.BuildStamp) {
		stamp, at := b.Stamp, b.CommittedAt
		if stamp == "" {
			stamp = "(unknown)"
		}
		if b.Version != "" {
			stamp = b.Version + " " + short(stamp)
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
	if st.Latest.NotesURL != "" {
		fmt.Fprintf(w, "notes    %s\n", st.Latest.NotesURL)
	}
	if a := st.Apply; a != nil {
		fmt.Fprintf(w, "apply    %s", a.State)
		if a.To != "" {
			fmt.Fprintf(w, " %s → %s", a.From, a.To)
		}
		if a.At != "" {
			fmt.Fprintf(w, " at %s", a.At)
		}
		fmt.Fprintln(w)
		if a.Error != nil {
			fmt.Fprintf(w, "         %s: %s\n", a.Error.Code, a.Error.Detail)
		}
		if a.StagedApp != "" {
			fmt.Fprintf(w, "         the app is staged at %s and replaces the installed one when it quits\n", a.StagedApp)
		}
	}
	if st.InstallKind == contract.UpdateInstallKindRelease {
		auto := "off"
		if st.AutoApply {
			auto = "on"
		}
		fmt.Fprintf(w, "auto     %s (clawdline setting set %s on|off)\n", auto, httptransport.AutoApplySetting)
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

// Bounds of `update --apply` on a release install: how long it follows the
// update, and how often it asks.
const (
	applyFollowSecondsLimit = updater.DownloadTimeoutSecondsLimit + updater.PendingDeadlineSecondsLimit
	applyPollInterval       = 2 * time.Second
)

// applyRelease asks the daemon to install a release and follows it through
// GET /v1/update until it is healthy, rolled back or failed. Exit 0 when the
// new release is healthy, 1 when it was refused, failed or rolled back, 3
// when the outcome could not be read in time.
func applyRelease(stdout, stderr io.Writer, port int, version string, force bool) int {
	if force && version == "" {
		fmt.Fprintln(stderr, "clawdline: --force on a release install needs --version, the release it installs")
		return 2
	}
	b, err := openBroker(port)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline: the daemon is not answering, and an update is started by it: %v\n", err)
		return updateExitUnknown
	}
	b.client.Timeout = 2*updater.FetchTimeoutSecondsLimit*time.Second + 10*time.Second
	a, err := b.request(http.MethodPost, "/v1/update/apply", nil, contract.UpdateApplyRequest{Version: version, Force: force}, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline: asking the daemon to update: %v\n", err)
		return updateExitUnknown
	}
	if a.Status != http.StatusAccepted {
		code, msg := a.refusal()
		fmt.Fprintf(stderr, "clawdline: the update was refused: %s: %s\n", code, msg)
		return 1
	}
	var started contract.UpdateStatus
	_ = json.Unmarshal(a.Body, &started)
	to := version
	if started.Apply != nil && started.Apply.To != "" {
		to = started.Apply.To
	}
	fmt.Fprintf(stdout, "updating to %s\n", to)
	b.client.Timeout = 5 * time.Second
	deadline := time.Now().Add(applyFollowSecondsLimit * time.Second)
	last := contract.UpdateApplyState("")
	for time.Now().Before(deadline) {
		time.Sleep(applyPollInterval)
		a, err := b.request(http.MethodGet, "/v1/update", nil, nil, "")
		if err != nil || a.Status != http.StatusOK {
			// The daemon is restarting; it answers again shortly.
			continue
		}
		var st contract.UpdateStatus
		if json.Unmarshal(a.Body, &st) != nil || st.Apply == nil || (to != "" && st.Apply.To != to) {
			continue
		}
		if st.Apply.State != last {
			last = st.Apply.State
			fmt.Fprintf(stdout, "%s\n", last)
		}
		switch st.Apply.State {
		case contract.UpdateApplyStateHealthy:
			fmt.Fprintf(stdout, "%s is running\n", st.Apply.To)
			if st.Apply.StagedApp != "" {
				fmt.Fprintf(stdout, "the app is staged at %s and replaces the installed one when it quits\n", st.Apply.StagedApp)
			}
			return 0
		case contract.UpdateApplyStateRolledBack, contract.UpdateApplyStateFailed:
			if e := st.Apply.Error; e != nil {
				fmt.Fprintf(stderr, "clawdline: %s: %s: %s\n", st.Apply.State, e.Code, e.Detail)
			}
			return 1
		}
	}
	fmt.Fprintln(stderr, "clawdline: the update's outcome could not be read in time; `clawdline update` shows it later")
	return updateExitUnknown
}

// updateFinishCommand is `clawdline update finish --state <dir> --root <dir>`,
// the supervisor an update starts from the release it replaces. Exit 0 when
// the update is settled; non-zero asks the service manager to start it
// again, and it resumes from pending.json.
func updateFinishCommand(args []string) int {
	fs := flag.NewFlagSet("update finish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	state := fs.String("state", "", "the daemon's state directory")
	root := fs.String("root", "", "the install layout's root")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *state == "" || *root == "" {
		fmt.Fprintln(os.Stderr, "usage: clawdline update finish --state <dir> --root <dir>")
		return 2
	}
	// The layout the service runs from is the one the update was started
	// in, whatever this process's environment says.
	os.Setenv("CLAWDLINE_NEXT_INSTALL_ROOT", *root)
	env, err := updater.DefaultEnv(*state)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		return 1
	}
	log.SetPrefix("update finish: ")
	if err := env.Finish(context.Background()); err != nil {
		log.Printf("not settled, the service manager starts this again: %v", err)
		return 1
	}
	if a, err := env.ReadApply(); err == nil {
		log.Printf("%s %s → %s", a.State, a.From, a.To)
	}
	return 0
}

// updateBootGuard is serve's first step on a release install (docs/updates.md):
// it restores a store snapshot a rollback asked for, and gives a pending
// update up when the new release keeps failing to settle. It answers false
// when serve must exit non-zero so the service manager starts the release
// `current` names again.
func updateBootGuard(stateDir string) bool {
	exe, err := os.Executable()
	if err != nil {
		return true
	}
	env, err := updater.DefaultEnv(stateDir)
	if err != nil {
		return true
	}
	exit, note, err := env.BootGuard(exe)
	if note != "" {
		log.Printf("update: %s", note)
	}
	if err != nil {
		log.Printf("update: boot guard: %v", err)
	}
	return !exit
}
