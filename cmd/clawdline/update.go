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
	"github.com/sainteye/clawdline/internal/adapters/release"
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

const updateUsage = `usage: clawdline update [--apply [--version vX.Y.Z] [--force]] [--json] [--port n]

Checks for a newer release of Clawdline, or installs it. If the new release
does not start, Clawdline goes back to the one running now by itself.

  --apply             install the newest release (on Linux inside a source
                      checkout: deploy the newest build)
  --version vX.Y.Z    with --apply: install this release instead
  --force             with --apply --version: install it even when it is not newer
  --json              print the whole status as JSON
  --port <n>          the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)

Exit status: 0 up to date, 10 a newer release is available, 3 unknown (the
check failed), 1 the update was refused, failed or rolled back.
docs/updates.md says more.`

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
	port := fs.Int("port", 0, "")
	asJSON := fs.Bool("json", false, "")
	apply := fs.Bool("apply", false, "")
	want := fs.String("version", "", "")
	force := fs.Bool("force", false, "")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(cliCopy("ops", "release.usage", updateUsage))
		os.Exit(0)
	}
	if err != nil || fs.NArg() != 0 {
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline update:", err)
		}
		fmt.Fprintln(os.Stderr, cliCopy("ops", "release.usage", updateUsage))
		os.Exit(2)
	}
	st := updateStatus(*port)
	// A release install's --apply reads the manifest afresh and prints what
	// it installs and how that ended; the daemon's last check, up to a check
	// interval old, would print "current" just above "updating to".
	if *apply && !*asJSON && st.InstallKind == contract.UpdateInstallKindRelease {
		os.Exit(applyRelease(os.Stdout, os.Stderr, *port, *want, *force))
	}
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
		fmt.Fprintln(os.Stderr, cliCopy("ops", "release.version_requires_release", "clawdline: --version installs a release, and this is not a release install"))
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
		fmt.Fprintf(w, cliCopy("ops", "release.install_kind", "install  %s"), st.InstallKind)
		if st.Channel != "" {
			fmt.Fprintf(w, cliCopy("ops", "release.channel", " (channel %s)"), st.Channel)
		}
		fmt.Fprintln(w)
	}
	line := func(name string, b contract.BuildStamp) {
		stamp, at := b.Stamp, b.CommittedAt
		if stamp == "" {
			stamp = cliCopy("ops", "update.unknown_stamp", "(unknown)")
		}
		if b.Version != "" {
			stamp = b.Version + " " + short(stamp)
		}
		if at == "" {
			at = cliCopy("ops", "update.unknown_time", "time unknown")
		}
		fmt.Fprintf(w, "%-8s %s  (%s)\n", name, stamp, at)
	}
	line(cliCopy("ops", "update.running", "running"), st.Running)
	line(cliCopy("ops", "update.latest", "latest"), st.Latest)
	// A failed check is said once, in words, with its code last: the
	// reason the daemon gives for an unknown state repeats the same error.
	switch {
	case st.Error != "" && st.State == contract.UpdateStateUnknown:
		fmt.Fprintf(w, cliCopy("ops", "update.state", "state    %s")+" — %s\n", st.State, checkFailed(st.Error))
	case st.Reason != "":
		fmt.Fprintf(w, cliCopy("ops", "update.state", "state    %s")+" — %s\n", st.State, st.Reason)
	default:
		fmt.Fprintf(w, cliCopy("ops", "update.state", "state    %s")+"\n", st.State)
	}
	if st.Error != "" && st.State != contract.UpdateStateUnknown {
		fmt.Fprintf(w, cliCopy("ops", "update.last_check_failed", "last check failed: %s\n"), checkFailed(st.Error))
	}
	if st.Latest.NotesURL != "" {
		fmt.Fprintf(w, cliCopy("ops", "release.notes", "notes    %s\n"), st.Latest.NotesURL)
	}
	if a := st.Apply; a != nil {
		fmt.Fprintf(w, cliCopy("ops", "release.apply_state", "apply    %s"), a.State)
		if a.To != "" {
			fmt.Fprintf(w, " %s → %s", a.From, a.To)
		}
		if a.At != "" {
			fmt.Fprintf(w, cliCopy("ops", "release.at", " at %s"), a.At)
		}
		fmt.Fprintln(w)
		if a.Error != nil {
			switch a.State {
			case contract.UpdateApplyStateRolledBack:
				fmt.Fprintf(w, cliCopy("ops", "release.rolled_back", "         %s did not start, so Clawdline went back to %s. (%s)\n"), a.To, a.From, a.Error.Code)
			case contract.UpdateApplyStateFailed:
				fmt.Fprintf(w, cliCopy("ops", "release.failed", "         the update to %s did not finish; nothing changed. (%s)\n"), a.To, a.Error.Code)
			default:
				fmt.Fprintf(w, "         %s: %s\n", a.Error.Code, a.Error.Detail)
			}
		}
		// The updater keeps a list of versions that rolled back here, and
		// auto-apply never installs one of them again (updater.Apply).
		if a.State == contract.UpdateApplyStateRolledBack && a.To != "" && a.To == st.Latest.Version {
			fmt.Fprintf(w, cliCopy("ops", "release.auto_skip", "         automatic updates will not try %s again; a newer release, or update --apply, will.\n"), a.To)
		}
		if a.StagedApp != "" {
			fmt.Fprintf(w, cliCopy("ops", "release.app_staged", "         the app is staged at %s and replaces the installed one when it quits\n"), a.StagedApp)
		}
	}
	if st.InstallKind == contract.UpdateInstallKindRelease {
		auto := cliCopy("ops", "release.auto_off", "off")
		if st.AutoApply {
			auto = cliCopy("ops", "release.auto_on", "on")
		}
		fmt.Fprintf(w, cliCopy("ops", "release.auto_status", "auto     %s (clawdline setting set %s on|off)\n"), auto, httptransport.AutoApplySetting)
	}
	fmt.Fprintf(w, cliCopy("ops", "update.source", "source   %s\n"), st.SourceURL)
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
		fmt.Fprintln(stderr, cliCopy("ops", "update.latest_unknown", "clawdline: the cloud's latest build is not known, so there is nothing to apply"))
		return updateExitUnknown
	}
	if (st.State == contract.UpdateStateCurrent || st.State == contract.UpdateStateAhead) && !force {
		fmt.Fprintf(stderr, cliCopy("ops", "update.nothing_to_apply", "clawdline: this machine is %s; nothing to apply (--force deploys %s anyway)\n"), st.State, stamp)
		return 1
	}
	switch goos {
	case "linux":
	case "darwin":
		fmt.Fprintln(stderr, cliCopy("ops", "update.bundle_instructions", "clawdline: --apply does not replace an app bundle; run these from a source checkout:"))
		fmt.Fprintf(stdout, "git fetch origin main\n")
		fmt.Fprintf(stdout, "git worktree add -f \"$TMPDIR/clawdline-%s\" %s\n", short(stamp), stamp)
		fmt.Fprintf(stdout, "cd \"$TMPDIR/clawdline-%s\" && tools/package-macos.sh\n", short(stamp))
		return 1
	default:
		fmt.Fprintf(stderr, cliCopy("ops", "update.linux_only", "clawdline: --apply is built for Linux only, not %s (docs/updates.md)\n"), goos)
		return 1
	}
	var top strings.Builder
	if err := r.run(dir, &top, io.Discard, "git", "rev-parse", "--show-toplevel"); err != nil {
		fmt.Fprintln(stderr, cliCopy("ops", "update.not_checkout", "clawdline: --apply runs inside a Clawdline source checkout, and this directory is not one"))
		return 1
	}
	root := strings.TrimSpace(top.String())
	deploy := filepath.Join(root, "tools", "deploy-linux-user.sh")
	if _, err := os.Stat(deploy); err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "update.no_deploy_script", "clawdline: --apply runs inside a Clawdline source checkout; %s has no tools/deploy-linux-user.sh\n"), root)
		return 1
	}
	if err := r.run(root, stdout, stderr, "git", "fetch", "origin", "main"); err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "update.fetch_failed", "clawdline: git fetch origin main failed: %v\n"), err)
		return 1
	}
	if err := r.run(root, io.Discard, io.Discard, "git", "merge-base", "--is-ancestor", stamp, "origin/main"); err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "update.refuse_unmerged", "clawdline: %s is not on origin/main; refusing to deploy it\n"), stamp)
		return 1
	}
	if err := r.run(root, stdout, stderr, deploy, "--rev", stamp); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, cliCopy("ops", "update.deploy_failed", "clawdline: %s: %v\n"), deploy, err)
		return 1
	}
	return 0
}

// checkFailed is a failed release check in words: what it means for this
// machine and what to do, then the error's code.
func checkFailed(errText string) string {
	code, _, _ := strings.Cut(errText, ":")
	code = strings.TrimSpace(code)
	var said string
	switch code {
	case release.CodeManifestMalformed, release.CodeManifestUnsigned, release.CodeSignatureInvalid,
		release.CodeNoTrustedKey, release.CodeVersionUnparseable:
		said = cliCopy("ops", "release.verify_failed", "the release could not be verified; nothing was changed. Try again later.")
	case updater.CodeReleaseUnreachable:
		said = cliCopy("ops", "release.server_unreachable", "the update server could not be reached; nothing was changed. Try again later.")
	case updater.CodeNoReleasePublished:
		said = cliCopy("ops", "release.none_published", "no release is published yet; nothing was changed.")
	default:
		said = cliCopy("ops", "release.check_failed", "the check for a newer release failed; nothing was changed. Try again later.")
	}
	if code == "" || strings.ContainsAny(code, " \t") {
		return said
	}
	return said + " (" + code + ")"
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
		fmt.Fprintln(stderr, cliCopy("ops", "release.force_needs_version", "clawdline: --force on a release install needs --version, the release it installs"))
		return 2
	}
	b, err := openBroker(port)
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "release.daemon_unavailable", "clawdline: the daemon is not answering, and an update is started by it: %v\n"), err)
		return updateExitUnknown
	}
	b.client.Timeout = 2*updater.FetchTimeoutSecondsLimit*time.Second + 10*time.Second
	a, err := b.request(http.MethodPost, "/v1/update/apply", nil, contract.UpdateApplyRequest{Version: version, Force: force}, "")
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "release.request_failed", "clawdline: asking the daemon to update: %v\n"), err)
		return updateExitUnknown
	}
	if a.Status != http.StatusAccepted {
		code, msg := a.refusal()
		fmt.Fprintf(stderr, cliCopy("ops", "release.refused", "clawdline: the update was refused: %s: %s\n"), code, msg)
		return 1
	}
	var started contract.UpdateStatus
	_ = json.Unmarshal(a.Body, &started)
	to := version
	if started.Apply != nil && started.Apply.To != "" {
		to = started.Apply.To
	}
	fmt.Fprintf(stdout, cliCopy("ops", "release.updating_to", "updating to %s\n"), to)
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
		ended := st.Apply.State == contract.UpdateApplyStateRolledBack || st.Apply.State == contract.UpdateApplyStateFailed
		if st.Apply.State != last && !ended {
			last = st.Apply.State
			fmt.Fprintf(stdout, "%s\n", last)
		}
		details := "clawdline update --json"
		if port != 0 {
			details += install.PortFlag(port)
		}
		switch st.Apply.State {
		case contract.UpdateApplyStateHealthy:
			fmt.Fprintf(stdout, cliCopy("ops", "release.is_running", "%s is running\n"), st.Apply.To)
			if st.Apply.StagedApp != "" {
				fmt.Fprintf(stdout, cliCopy("ops", "release.app_staged_short", "the app is staged at %s and replaces the installed one when it quits\n"), st.Apply.StagedApp)
			}
			return 0
		case contract.UpdateApplyStateRolledBack:
			fmt.Fprintln(stderr, rolledBackSentence(st.Apply.To, st.Apply.From, details))
			return 1
		case contract.UpdateApplyStateFailed:
			fmt.Fprintf(stderr, cliCopy("ops", "release.failed_outcome", "The update to %s did not finish, so nothing changed: %s is still running. Details: %s\n"),
				st.Apply.To, st.Apply.From, details)
			return 1
		}
	}
	fmt.Fprintln(stderr, cliCopy("ops", "release.outcome_timeout", "clawdline: the update's outcome could not be read in time; `clawdline update` shows it later"))
	return updateExitUnknown
}

// rolledBackSentence is what `update --apply` says when the new release did
// not start and the updater went back: that nothing is left to do, and that
// auto-apply will not install that version again.
func rolledBackSentence(to, from, details string) string {
	return fmt.Sprintf(cliCopy("ops", "release.rollback_sentence", "%s did not start, so Clawdline went back to %s, which is running now. Nothing else to do; automatic updates will skip this version. Details: %s"), to, from, details)
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
		fmt.Fprintln(os.Stderr, cliCopy("ops", "release.finish_usage", "usage: clawdline update finish --state <dir> --root <dir>"))
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
