package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// Request is one ask to install a release.
type Request struct {
	// Version installs that release instead of the channel's newest.
	Version string
	// Force installs Version even when it is not newer than the running one.
	Force bool
	// Auto is auto-apply's ask: it never installs a version that rolled
	// back before.
	Auto bool
}

// Plan is an update that passed every refusal and holds update.lock. Exactly
// one of Execute or Abandon must follow.
type Plan struct {
	From     string
	Manifest release.Manifest
	Service  install.ServiceFile
	unlock   func()
}

// Abandon gives the lock back without doing anything.
func (p *Plan) Abandon() {
	if p.unlock != nil {
		p.unlock()
		p.unlock = nil
	}
}

// Begin decides whether the update may run, without changing anything the
// running daemon uses: it takes update.lock and reads the manifest. Each
// refusal is an *Error whose code the caller can branch on.
func (e Env) Begin(ctx context.Context, exe string, req Request) (*Plan, error) {
	from, kind := Running(e.Layout, exe)
	if kind != install.KindRelease {
		return nil, fail(CodeNotAReleaseInstall, "this daemon runs from %s, which is a %s and not a release install; %s",
			exe, kind, "a source build is updated from its checkout (docs/updates.md)")
	}
	svc, err := install.ReadServiceFile(e.StateDir)
	if err != nil {
		return nil, fail(CodeNoService, "the daemon is not installed as a service: %s has no %s (%v); the installer writes it when it installs the service", e.StateDir, install.ServiceFileName, err)
	}
	if _, ok, err := e.ReadPending(); err != nil {
		return nil, fail(CodeStateUnreadable, "pending.json: %v", err)
	} else if ok {
		return nil, fail(CodeUpdateInProgress, "an update is already handed to the supervisor (%s)", e.pendingPath())
	}
	unlock, err := e.lock()
	if err != nil {
		return nil, err
	}
	plan, err := e.decide(ctx, from, req)
	if err != nil {
		unlock()
		return nil, err
	}
	plan.Service, plan.unlock = svc, unlock
	return plan, nil
}

func (e Env) decide(ctx context.Context, from string, req Request) (*Plan, error) {
	base, err := e.Base(ctx, ChannelFor(from), req.Version)
	if err != nil {
		return nil, err
	}
	m, err := e.Fetch(ctx, base)
	if err != nil {
		return nil, err
	}
	if req.Version != "" && m.Version != req.Version {
		return nil, &release.Error{Code: release.CodeManifestMalformed,
			Detail: fmt.Sprintf("asked for %s and the manifest at %s is %s", req.Version, base, m.Version)}
	}
	running, err := release.ParseVersion(from)
	if err != nil {
		return nil, err
	}
	next, _ := release.ParseVersion(m.Version)
	switch c := release.Compare(next, running); {
	case c == 0:
		return nil, fail(CodeAlreadyCurrent, "%s is already running", from)
	case c < 0 && !(req.Force && req.Version != ""):
		return nil, fail(CodeNotNewer, "%s is older than the running %s; --version %s --force installs it", m.Version, from, m.Version)
	}
	if m.MinVersion != "" {
		min, _ := release.ParseVersion(m.MinVersion)
		if release.Compare(running, min) < 0 {
			return nil, fail(CodeVersionBelowMin, "%s can only be installed over %s or later, and this machine runs %s",
				m.Version, m.MinVersion, from)
		}
	}
	if req.Auto {
		failed, err := e.HasFailed(m.Version)
		if err != nil {
			return nil, fail(CodeStateUnreadable, "failed.json: %v", err)
		}
		if failed {
			return nil, fail(CodeVersionFailedBefore, "%s rolled back before; it is installed again only when asked", m.Version)
		}
	}
	if _, err := m.Artifact(e.GOOS, e.GOARCH, release.KindDaemon); err != nil {
		return nil, err
	}
	return &Plan{From: from, Manifest: m}, nil
}

// Execute downloads, checks, unpacks and smoke-runs the release, snapshots
// the store, writes pending.json and starts the supervisor. Any failure
// before pending.json is written leaves the running release untouched and
// is recorded as failed. It releases the lock in every case.
func (e Env) Execute(ctx context.Context, p *Plan) error {
	defer p.Abandon()
	m, from, to := p.Manifest, p.From, p.Manifest.Version
	failed := func(err error, fallback string) error {
		_ = e.record(contract.UpdateApplyStateFailed, from, to, codeOf(err, fallback), "")
		return err
	}
	_ = e.record(contract.UpdateApplyStateDownloading, from, to, nil, "")
	daemon, _ := m.Artifact(e.GOOS, e.GOARCH, release.KindDaemon)
	archive, err := e.download(ctx, daemon)
	if err != nil {
		return failed(err, CodeDownloadFailed)
	}
	defer os.Remove(archive)
	_ = e.record(contract.UpdateApplyStateVerifying, from, to, nil, "")
	dir := e.Layout.ReleaseDir(to)
	if err := e.unpackRelease(archive, dir, ""); err != nil {
		return failed(err, CodeArchiveUnsafe)
	}
	if err := e.smoke(ctx, dir, m); err != nil {
		return failed(err, CodeSmokeFailed)
	}
	stagedApp, err := e.stageApp(ctx, m, dir)
	if err != nil {
		return failed(err, CodeDownloadFailed)
	}
	snapshot, err := e.Snapshot(from, to)
	if err != nil {
		return failed(err, CodeSnapshotFailed)
	}
	fromCommit, _ := buildOf(e.Layout, from)
	pending := Pending{
		From: from, To: to, FromCommit: fromCommit, ToCommit: m.Commit,
		Deadline:    e.now().Add(PendingDeadlineSecondsLimit * time.Second),
		NonAdditive: m.NonAdditiveMigration, Snapshot: snapshot, Phase: phaseSwitch, StagedApp: stagedApp,
	}
	if err := e.writePending(pending); err != nil {
		return failed(err, CodeStateUnreadable)
	}
	_ = e.record(contract.UpdateApplyStateStaged, from, to, nil, stagedApp)
	if err := e.startSupervisor(ctx, p.Service, from); err != nil {
		// Nothing was switched: take the pending update back so the next
		// apply is not refused as one in progress.
		_ = e.clearPending()
		return failed(err, CodeSupervisorFailed)
	}
	_ = e.record(contract.UpdateApplyStateRestarting, from, to, nil, stagedApp)
	return nil
}

// download fetches a into staging and checks its size and sha256 on the
// way. The file is removed on any failure.
func (e Env) download(ctx context.Context, a release.Artifact) (string, error) {
	if a.Size > maxArtifactBytes {
		return "", fail(CodeDownloadFailed, "%s is %d bytes, more than the %d an artifact may be", a.Name, a.Size, int64(maxArtifactBytes))
	}
	if err := os.MkdirAll(e.Layout.Staging, 0o700); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeoutSecondsLimit*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fail(CodeDownloadFailed, "%v", err)
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return "", fail(CodeDownloadFailed, "%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fail(CodeDownloadFailed, "%s answered %d", a.URL, resp.StatusCode)
	}
	path := filepath.Join(e.Layout.Staging, a.Name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	cerr := a.Check(resp.Body, f)
	if err := f.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		os.Remove(path)
		var re *release.Error
		if errors.As(cerr, &re) {
			return "", cerr
		}
		return "", fail(CodeDownloadFailed, "%s: %v", a.Name, cerr)
	}
	return path, nil
}

// unpackRelease unpacks archive into dir/sub through a temporary directory
// beside it and one rename, so releases/<v> is whole or absent. A releases/<v>
// left by an earlier attempt that never became current is replaced.
func (e Env) unpackRelease(archive, dir, sub string) error {
	target := dir
	if sub != "" {
		target = filepath.Join(dir, sub)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".tmp-"+strconv.Itoa(os.Getpid()))
	os.RemoveAll(tmp)
	if err := unpack(archive, tmp); err != nil {
		return err
	}
	if sub == "" {
		if info, err := os.Stat(filepath.Join(tmp, "clawdline")); err != nil || !info.Mode().IsRegular() {
			os.RemoveAll(tmp)
			return fail(CodeArchiveUnsafe, "%s has no clawdline binary at its root", filepath.Base(archive))
		}
	}
	if name, _ := e.Layout.CurrentRelease(); sub == "" && name == filepath.Base(dir) {
		os.RemoveAll(tmp)
		return fail(CodeAlreadyCurrent, "%s is current; it is not unpacked over", name)
	}
	if err := os.RemoveAll(target); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// smoke runs the unpacked binary's `version --json` and requires it to be
// the release the manifest names.
func (e Env) smoke(ctx context.Context, dir string, m release.Manifest) error {
	ctx, cancel := context.WithTimeout(ctx, SmokeTimeoutSecondsLimit*time.Second)
	defer cancel()
	out, err := e.Run(ctx, filepath.Join(dir, "clawdline"), "version", "--json")
	if err != nil {
		return fail(CodeSmokeFailed, "the new binary did not run: %v", err)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &got); err != nil {
		return fail(CodeSmokeFailed, "the new binary's version --json is not JSON: %v", err)
	}
	if got.Version != m.Version || got.Commit != m.Commit {
		return fail(CodeSmokeFailed, "the new binary says %s at %s; the manifest says %s at %s",
			got.Version, short(got.Commit), m.Version, short(m.Commit))
	}
	return nil
}
