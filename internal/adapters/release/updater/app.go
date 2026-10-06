package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

// installedApp is the macOS app setup installed, or "" when there is none.
func (e Env) installedApp() string {
	if e.GOOS != "darwin" || e.AppsDir == "" {
		return ""
	}
	app := filepath.Join(e.AppsDir, AppBundleName)
	if info, err := os.Stat(app); err == nil && info.IsDir() {
		return app
	}
	return ""
}

// stageApp downloads and unpacks the release's app bundle into
// releases/<v>/app/ when this machine has the app installed and the release
// carries one. It is swapped in only after the new daemon is healthy, and
// only while the app is not running (swapApp).
func (e Env) stageApp(ctx context.Context, m release.Manifest, dir string) (string, error) {
	if e.installedApp() == "" {
		return "", nil
	}
	a, err := m.Artifact(e.GOOS, e.GOARCH, release.KindApp)
	if err != nil {
		// A release without an app for this machine leaves the app as it is.
		return "", nil
	}
	archive, err := e.download(ctx, a)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err := e.unpackRelease(archive, dir, "app"); err != nil {
		return "", err
	}
	staged := filepath.Join(dir, "app", AppBundleName)
	if info, err := os.Stat(staged); err != nil || !info.IsDir() {
		return "", fail(CodeArchiveUnsafe, "%s has no %s at its root", a.Name, AppBundleName)
	}
	return staged, nil
}

// appRunning says whether any process runs from inside bundle.
func (e Env) appRunning(ctx context.Context, bundle string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := e.Run(ctx, "ps", "-axo", "command=")
	if err != nil {
		return false, err
	}
	prefix := bundle + "/Contents/"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true, nil
		}
	}
	return false, nil
}

// swapApp puts the staged bundle in place of the installed one when nothing
// runs from it, keeping the replaced bundle in releases/<from>/app. It
// answers the path still staged, "" once swapped. A running app keeps its
// bundle: it swaps on quit.
func (e Env) swapApp(ctx context.Context, staged, from string) (string, error) {
	app := e.installedApp()
	if staged == "" || app == "" {
		return "", nil
	}
	if _, err := os.Stat(staged); err != nil {
		return "", nil
	}
	running, err := e.appRunning(ctx, app)
	if err != nil || running {
		return staged, err
	}
	keep := filepath.Join(e.Layout.ReleaseDir(from), "app", AppBundleName)
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		return staged, err
	}
	os.RemoveAll(keep)
	if err := os.Rename(app, keep); err != nil {
		return staged, err
	}
	if err := os.Rename(staged, app); err != nil {
		// Put the old one back rather than leave no app at all.
		if rerr := os.Rename(keep, app); rerr != nil {
			return staged, errors.Join(err, rerr)
		}
		return staged, err
	}
	return "", nil
}
