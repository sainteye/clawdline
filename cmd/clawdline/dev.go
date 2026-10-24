package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/config"
)

const devUsage = `usage: clawdline dev install [--rev <commit>] [--port n]

Builds a commit of this checkout and runs it on this machine in place of the
installed release or Linux source deployment, to test it. On a release install,
going back is the ordinary update: the version
panel's button, or clawdline update --apply. If the build does not start,
Clawdline restores the previous installation.

  --rev <commit>   the commit to build (default HEAD of this checkout)
  --port <n>       release-install port; omit for a Linux source service

Run it inside a clawdline checkout, on a release or Linux source installation.
docs/updates.md says more.`

// devCommand is `clawdline dev install`: a dev build, docs/updates.md.
func devCommand(args []string) {
	if len(args) == 0 || args[0] != "install" {
		fmt.Fprintln(os.Stderr, cliCopy("ops", "dev.usage", devUsage))
		os.Exit(2)
	}
	fs := flag.NewFlagSet("dev install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "HEAD", "")
	port := fs.Int("port", 0, "")
	err := fs.Parse(args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(cliCopy("ops", "dev.usage", devUsage))
		os.Exit(0)
	}
	if err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, cliCopy("ops", "dev.usage", devUsage))
		os.Exit(2)
	}
	os.Exit(devInstall(os.Stdout, os.Stderr, *rev, *port, execRunner{}))
}

// devVersion is the version a dev build of commit takes over the running
// release: v1.0.7-dev.4c41b18 over v1.0.7, and over another dev build of
// v1.0.7 the same base.
func devVersion(running, commit string) (string, error) {
	v, err := release.ParseVersion(running)
	if err != nil {
		return "", err
	}
	if v.Pre != "" && !v.IsDev() {
		return "", fmt.Errorf("%s is a pre-release; a dev build goes over a stable release", running)
	}
	if len(commit) < 7 {
		return "", fmt.Errorf("%q is not a commit", commit)
	}
	v.Pre = release.DevPre + commit[:7]
	return v.String(), nil
}

func devInstall(stdout, stderr io.Writer, rev string, port int, r runner) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "clawdline dev install: "+format+"\n", a...)
		return 1
	}
	top, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fail(cliCopy("ops", "dev.not_checkout", "run it inside a clawdline checkout: %v"), err)
	}
	if _, err := os.Stat(filepath.Join(top, "tools", "release", "build.sh")); err != nil {
		return fail(cliCopy("ops", "dev.not_clawdline", "%s is not a clawdline checkout (no tools/release/build.sh)"), top)
	}
	commit, err := gitOutput(top, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return fail(cliCopy("ops", "dev.no_commit", "%s is not a commit here: %v"), rev, err)
	}
	env, err := updater.DefaultEnv(config.Dir())
	if err != nil {
		return fail("%v", err)
	}
	running, kind := env.Layout.CurrentRelease()
	if kind == install.KindSourceDeploy {
		return devSourceInstall(stdout, stderr, top, commit, runtime.GOOS, port, r)
	}
	if kind != install.KindRelease {
		return fail(cliCopy("ops", "dev.not_release", "this installation has no supported release or Linux source deployment (current is %q)"), running)
	}
	version, err := devVersion(running, commit)
	if err != nil {
		return fail("%v", err)
	}
	if version == running {
		return fail(cliCopy("ops", "dev.already", "%s is already running"), version)
	}

	scratch, err := os.MkdirTemp("", "clawdline-dev-")
	if err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(scratch)
	src, out := filepath.Join(scratch, "src"), filepath.Join(scratch, "out")
	// The commit is built in its own disposable worktree: the checkout's HEAD
	// and its uncommitted files are not touched (docs/worktrees.md).
	if err := r.run(top, stdout, stderr, "git", "worktree", "add", "--detach", "-f", src, commit); err != nil {
		return fail("git worktree add: %v", err)
	}
	defer func() { _ = r.run(top, io.Discard, io.Discard, "git", "worktree", "remove", "--force", src) }()

	build := []string{filepath.Join("tools", "release", "build.sh"), version, out,
		"--channel", "dev", "--base-url", "file://" + out, "--skip-tests", "--host-only"}
	if tmux := devTmux(env.Layout.ReleaseDir(running), filepath.Join(scratch, "tmux")); tmux != "" {
		build = append(build, "--tmux-from", tmux, "--tmux-installed")
	}
	if runtime.GOOS != "darwin" {
		build = append(build, "--no-app")
	} else if _, err := exec.LookPath("swiftc"); err != nil {
		build = append(build, "--no-app")
	}
	fmt.Fprintf(stdout, cliCopy("ops", "dev.building", "building %s from %s…\n"), version, commit[:7])
	// It waits in place for the compile slot: a person ran this and is
	// watching it, so it is not handed to a callback.
	os.Setenv("HEAVY_HANDOFF", "0")
	// Sign with this machine's copy of the release certificate when available.
	// A Developer ID identity is needed for WebKit's keychain grant to survive
	// an App replacement (tools/release/codesign.sh).
	if p12, password, ok := devCodesign(config.Dir()); ok {
		os.Setenv("CLAWDLINE_CODESIGN_P12", p12)
		os.Setenv("CLAWDLINE_CODESIGN_PASSWORD", password)
	}
	if err := r.run(src, stdout, stderr, filepath.Join(src, "tools", "heavy.sh"), append([]string{"--compile", "bash"}, build...)...); err != nil {
		return fail(cliCopy("ops", "dev.build_failed", "the build failed, and nothing was changed: %v"), err)
	}

	plan, err := env.BeginLocal(filepath.Join(out, "manifest.json"))
	if err != nil {
		return fail(cliCopy("ops", "dev.refused", "the switch was refused: %v"), err)
	}
	if err := env.Execute(context.Background(), plan); err != nil {
		return fail(cliCopy("ops", "dev.switch_failed", "the switch did not start, and %s still runs: %v"), running, err)
	}
	fmt.Fprintf(stdout, cliCopy("ops", "dev.switching", "switching to %s\n"), version)
	b, err := openBroker(port)
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "dev.follow_unavailable", "clawdline: the switch was handed over; `clawdline update` shows how it ends (%v)\n"), err)
		return updateExitUnknown
	}
	code := followApply(stdout, stderr, b, port, version)
	if code == 0 {
		fmt.Fprint(stdout, cliCopy("ops", "dev.way_back", "to go back: clawdline update --apply, or the version panel's button\n"))
	}
	return code
}

// devSourceInstall keeps the CLI entry point the same for Linux source installs.
// The deployment script owns the service port, startup checks and rollback.
func devSourceInstall(stdout, stderr io.Writer, top, commit, goos string, port int, r runner) int {
	if goos != "linux" || port != 0 {
		fmt.Fprintln(stderr, cliCopy("ops", "dev.source_config", "source dev installation requires Linux and the configured service port; omit --port"))
		return 1
	}
	if err := r.run(top, stdout, stderr, "bash", filepath.Join(top, "tools", "deploy-linux-user.sh"), "--dev", "--rev", commit); err != nil {
		fmt.Fprintf(stderr, cliCopy("ops", "dev.source_failed", "dev deployment failed; inspect the deployment output for build or rollback status: %v\n"), err)
		return 1
	}
	return 0
}

// devTmux lays out the running release's tmux the way build.sh --tmux-from
// takes it, so a dev build carries the tmux already running instead of
// building one. "" when the release carries none; build.sh then builds it.
// That tmux was signed ad hoc after its tmux.json was written, so build.sh is
// told so (--tmux-installed) and checks only that it is the pinned tmux: on
// 2026-10-15 the first dev install stopped on its hash.
func devTmux(release, scratch string) string {
	dir := filepath.Join(scratch, runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(filepath.Join(dir, "licenses"), 0o755); err != nil {
		return ""
	}
	for _, name := range []string{"tmux", "tmux.json"} {
		if err := copyFile(filepath.Join(release, "libexec", name), filepath.Join(dir, name)); err != nil {
			return ""
		}
	}
	licenses, _ := filepath.Glob(filepath.Join(release, "licenses", "*.txt"))
	for _, l := range licenses {
		if err := copyFile(l, filepath.Join(dir, "licenses", filepath.Base(l))); err != nil {
			return ""
		}
	}
	return scratch
}

func copyFile(from, to string) error {
	body, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, body, info.Mode().Perm())
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// devCodesign is this machine's code-signing certificate for dev builds:
// <state dir>/signing/codesign.p12 and the password beside it in
// codesign.password (docs/releasing.md, The code-signing certificate). A
// machine without them builds ad hoc.
func devCodesign(dir string) (p12, password string, ok bool) {
	if runtime.GOOS != "darwin" || dir == "" {
		return "", "", false
	}
	p12 = filepath.Join(dir, "signing", "codesign.p12")
	if st, err := os.Stat(p12); err != nil || !st.Mode().IsRegular() {
		return "", "", false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "signing", "codesign.password"))
	if err != nil {
		return "", "", false
	}
	return p12, strings.TrimSpace(string(raw)), true
}
