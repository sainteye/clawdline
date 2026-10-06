// Package updater installs a newer signed release on a release install and
// takes it back out when it does not come up (docs/updates.md).
//
// An update crosses three processes, and each owns one part:
//
//   - The running (old) daemon checks for releases, downloads one into
//     <root>/staging, checks it against the signed manifest, unpacks it to
//     releases/<v>, smoke-runs it, snapshots the store with VACUUM INTO, writes
//     <state>/update/pending.json and starts the supervisor. Until then nothing
//     the old daemon runs from has changed.
//   - The supervisor is the old release's binary (`update finish`), started
//     outside the service's job (systemd-run, or a one-shot LaunchAgent), so a
//     restart of the service does not take it down and a killed supervisor is
//     started again and resumes from pending.json. It switches `current`,
//     restarts the service, waits for the new daemon to serve its console and
//     name the new commit, and either clears pending.json or switches back.
//   - The new daemon's boot guard (`serve`) counts its own starts in
//     pending.json; past the deadline or the start limit it switches back and
//     exits, so the service manager starts the old binary even when no
//     supervisor is left.
//
// Every step records its state in <state>/update/status.json, which
// GET /v1/update reads; nothing here keeps state only in memory.
package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// Bounds, registered in internal/domain/capacity.
const (
	// CheckIntervalSecondsLimit is how long a release check's answer is
	// held before the next one; CheckJitterSecondsLimit is the most added to
	// it at random, so machines started together do not ask together.
	CheckIntervalSecondsLimit = 6 * 3600
	CheckJitterSecondsLimit   = 1800
	// FetchTimeoutSecondsLimit is one read of a manifest, its signatures or
	// the release list.
	FetchTimeoutSecondsLimit = 30
	// DownloadTimeoutSecondsLimit is one artifact's download.
	DownloadTimeoutSecondsLimit = 900
	// maxArtifactBytes refuses a manifest naming a larger artifact before
	// anything is downloaded.
	maxArtifactBytes = 512 << 20
	// maxReleaseListBytes is one answer of the GitHub Releases API.
	maxReleaseListBytes = 1 << 20
	// maxArchiveEntries and maxUnpackedBytes bound one archive's unpacking.
	maxArchiveEntries = 20000
	maxUnpackedBytes  = 2 << 30
	// SmokeTimeoutSecondsLimit is the new binary's `version --json`.
	SmokeTimeoutSecondsLimit = 15
	// HealthWaitSecondsLimit is how long the supervisor waits for a
	// restarted daemon to serve its console and name its commit.
	HealthWaitSecondsLimit = 60
	// PendingDeadlineSecondsLimit is how long an update may stay pending
	// before the new release's boot guard gives it up.
	PendingDeadlineSecondsLimit = 600
	// BootAttemptsLimit is how many times the new release may start while
	// its update is pending.
	BootAttemptsLimit = 3
	// SupervisorRunsLimit is how many times the supervisor may be started for
	// one update before it stops trying the new release and rolls back.
	SupervisorRunsLimit = 5
	// LockStaleSecondsLimit is the age at which an update.lock left by a
	// process that died is taken over.
	LockStaleSecondsLimit = 1800
	// BackupsKeptLimit is how many store snapshots are kept.
	BackupsKeptLimit = 2
	// PreviousReleasesKeptLimit is how many releases besides `current` stay
	// unpacked for a rollback by hand.
	PreviousReleasesKeptLimit = 2
	// FailedVersionsLimit is how many versions that rolled back are
	// remembered, so auto-apply does not try them again.
	FailedVersionsLimit = 16
	// maxStateFileBytes is one of the updater's own JSON files.
	maxStateFileBytes = 64 << 10
)

// Environment switches.
const (
	// ReleaseURLEnv replaces the release host: <url>/manifest.json is the
	// newest release, and a named version is read from the sibling path
	// whose last segment is the version (tests and local release servers).
	ReleaseURLEnv = "CLAWDLINE_NEXT_RELEASE_URL"
	// ChannelEnv chooses stable or beta; without it a release install
	// follows the channel its running version is on.
	ChannelEnv = "CLAWDLINE_NEXT_RELEASE_CHANNEL"
	// AppsDirEnv replaces ~/Applications, where setup puts the macOS app.
	AppsDirEnv = "CLAWDLINE_NEXT_APPS_DIR"
)

// Where releases are published.
const (
	DefaultStableBase   = "https://github.com/sainteye/clawdline/releases/latest/download"
	DefaultDownloadRoot = "https://github.com/sainteye/clawdline/releases/download"
	DefaultReleasesAPI  = "https://api.github.com/repos/sainteye/clawdline/releases?per_page=30"
)

// AppBundleName is the macOS app's bundle.
const AppBundleName = "Clawdline Next.app"

// Channels.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// Error is a refusal or a failure with a stable code a caller branches on.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// The codes Error carries, besides release.Error's, which pass through.
const (
	CodeUpdateInProgress    = "update_in_progress"
	CodeNotAReleaseInstall  = "not_a_release_install"
	CodeNoService           = "not_installed_as_service"
	CodeAlreadyCurrent      = "already_current"
	CodeNotNewer            = "version_not_newer"
	CodeVersionBelowMin     = "version_below_min"
	CodeVersionFailedBefore = "version_failed_before"
	CodeNoReleasePublished  = "no_release_published"
	CodeReleaseUnreachable  = "release_unreachable"
	CodeDownloadFailed      = "download_failed"
	CodeArchiveUnsafe       = "archive_unsafe"
	CodeSmokeFailed         = "smoke_run_failed"
	CodeSnapshotFailed      = "snapshot_failed"
	CodeSupervisorFailed    = "supervisor_failed"
	CodeSwitchFailed        = "switch_failed"
	CodeHealthTimeout       = "health_timeout"
	CodeBootGuard           = "boot_guard"
	CodeSupervisorGaveUp    = "supervisor_gave_up"
	CodeStateUnreadable     = "update_state_unreadable"
)

func fail(code, format string, a ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// codeOf is the stable code of err: its own, a release refusal's, or a
// generic one.
func codeOf(err error, fallback string) *contract.UpdateApplyError {
	var ue *Error
	if errors.As(err, &ue) {
		return &contract.UpdateApplyError{Code: ue.Code, Detail: ue.Detail}
	}
	var re *release.Error
	if errors.As(err, &re) {
		return &contract.UpdateApplyError{Code: re.Code, Detail: re.Detail}
	}
	return &contract.UpdateApplyError{Code: fallback, Detail: err.Error()}
}

// Runner runs a command and returns what it printed; a test replaces it.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the real command.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Env is everything the updater touches, so a test can replace each part.
type Env struct {
	Layout   install.Layout
	StateDir string
	GOOS     string
	GOARCH   string
	Keys     []ed25519.PublicKey
	Client   *http.Client
	Run      Runner
	Now      func() time.Time
	// ReleaseURL is ReleaseURLEnv's value; ReleasesAPI the GitHub list.
	ReleaseURL  string
	ReleasesAPI string
	// AppsDir holds the installed macOS app, if setup installed one.
	AppsDir string
	// Health asks the daemon on port whether it serves its console and
	// names commit; nil is the real HTTP check.
	Health func(ctx context.Context, port int, token, commit string) error
	// Poll is the pause between health asks; HealthWait replaces
	// HealthWaitSecondsLimit when shorter (tests).
	Poll       time.Duration
	HealthWait time.Duration
	// SupervisorEnv is extra environment the supervisor is started with.
	SupervisorEnv map[string]string
}

// DefaultEnv is the real machine's, for the state directory dir.
func DefaultEnv(stateDir string) (Env, error) {
	layout, err := install.DefaultLayout()
	if err != nil {
		return Env{}, err
	}
	apps := os.Getenv(AppsDirEnv)
	if apps == "" {
		if home, err := os.UserHomeDir(); err == nil {
			apps = filepath.Join(home, "Applications")
		}
	}
	env := Env{
		Layout: layout, StateDir: stateDir, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Keys: release.TrustedKeys(), Client: &http.Client{}, Run: ExecRunner, Now: time.Now,
		ReleaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv(ReleaseURLEnv)), "/"),
		ReleasesAPI: DefaultReleasesAPI, AppsDir: apps, Poll: time.Second,
		SupervisorEnv: map[string]string{},
	}
	// The supervisor and the daemon it restarts must see the same layout and
	// state; a test install names both in the environment.
	for _, k := range []string{"CLAWDLINE_NEXT_INSTALL_ROOT", "CLAWDLINE_NEXT_DIR", ReleaseURLEnv, ChannelEnv, AppsDirEnv, "PATH", "HOME"} {
		if v := os.Getenv(k); v != "" {
			env.SupervisorEnv[k] = v
		}
	}
	return env, nil
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e Env) client() *http.Client {
	if e.Client != nil {
		return e.Client
	}
	return http.DefaultClient
}

// UpdateDir is <state>/update, where the updater keeps its files.
func (e Env) UpdateDir() string { return filepath.Join(e.StateDir, "update") }

// BackupsDir is <state>/backups, where store snapshots go.
func (e Env) BackupsDir() string { return filepath.Join(e.StateDir, "backups") }

func (e Env) statusPath() string  { return filepath.Join(e.UpdateDir(), "status.json") }
func (e Env) pendingPath() string { return filepath.Join(e.UpdateDir(), "pending.json") }
func (e Env) failedPath() string  { return filepath.Join(e.UpdateDir(), "failed.json") }
func (e Env) lockPath() string    { return filepath.Join(e.UpdateDir(), "update.lock") }
func (e Env) restorePath() string { return filepath.Join(e.UpdateDir(), "restore.json") }

// Running is the release this executable runs from: its releases/ entry and
// its kind. A binary outside the layout is a source checkout with no name.
func Running(l install.Layout, exe string) (string, install.Kind) {
	kind := l.KindOf(exe)
	if kind != install.KindRelease && kind != install.KindSourceDeploy {
		return "", kind
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	releases := l.Releases
	if resolved, err := filepath.EvalSymlinks(releases); err == nil {
		releases = resolved
	}
	rel, err := filepath.Rel(releases, exe)
	if err != nil {
		return "", install.KindSourceCheckout
	}
	return strings.SplitN(rel, string(filepath.Separator), 2)[0], kind
}

// buildOf reads releases/<name>/dist/BUILD.json: the commit and its time.
func buildOf(l install.Layout, name string) (commit, committedAt string) {
	f, err := os.Open(filepath.Join(l.ReleaseDir(name), "dist", "BUILD.json"))
	if err != nil {
		return "", ""
	}
	defer f.Close()
	var b struct {
		Stamp       string `json:"stamp"`
		CommittedAt string `json:"committed_at"`
	}
	if json.NewDecoder(io.LimitReader(f, maxStateFileBytes)).Decode(&b) != nil {
		return "", ""
	}
	return strings.TrimSpace(b.Stamp), strings.TrimSpace(b.CommittedAt)
}

// ChannelFor is the channel a release install follows: ChannelEnv when set,
// else beta for a pre-release version and stable otherwise.
func ChannelFor(running string) string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ChannelEnv))) {
	case ChannelBeta:
		return ChannelBeta
	case ChannelStable:
		return ChannelStable
	}
	if v, err := release.ParseVersion(running); err == nil && v.Pre != "" {
		return ChannelBeta
	}
	return ChannelStable
}
