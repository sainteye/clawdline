package updater

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestAHealthyUpdateSwitchesCurrentAndPrunesOldReleases(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	// Older releases, and a source deploy that must never be pruned.
	for _, v := range []string{"v0.8.0", "v0.9.0", "v0.9.1"} {
		f.installRelease(v, commitOf('9'))
	}
	f.installRelease(commitOf('d'), commitOf('d'))
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)

	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateRestarting || got.From != "v0.10.0" || got.To != "v0.11.0" {
		t.Fatalf("after the handoff: %+v", got)
	}
	if f.current() != "v0.10.0" {
		t.Fatalf("the running daemon switched current itself: %s", f.current())
	}
	if !f.ran("systemd-run --user --unit clawdline-next-update --collect -p Restart=on-failure") ||
		!f.ran(filepath.Join(f.env.Layout.ReleaseDir("v0.10.0"), "clawdline")+" update finish") {
		t.Fatalf("the supervisor was not started from the old release: %v", f.calls)
	}
	p, ok, _ := f.env.ReadPending()
	if !ok || p.To != "v0.11.0" || p.ToCommit != commitOf('b') || p.FromCommit != commitOf('a') {
		t.Fatalf("pending: %+v %v", p, ok)
	}

	f.healthy = commitOf('b')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.current() != "v0.11.0" || !f.ran("systemctl --user restart clawdline-next.service") {
		t.Fatalf("current %s, calls %v", f.current(), f.calls)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateHealthy || got.Error != nil {
		t.Fatalf("after finish: %+v", got)
	}
	if _, ok, _ := f.env.ReadPending(); ok {
		t.Fatal("pending.json outlived a healthy update")
	}
	for name, want := range map[string]bool{"v0.11.0": true, "v0.10.0": true, "v0.9.1": true, "v0.9.0": false, "v0.8.0": false, commitOf('d'): true} {
		_, err := os.Stat(f.env.Layout.ReleaseDir(name))
		if (err == nil) != want {
			t.Errorf("releases/%s kept=%v, want %v", name, err == nil, want)
		}
	}
}

func TestAReleaseThatDoesNotComeUpIsRolledBackAndNotRetriedByItself(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	// A store, so there is a snapshot that an additive release must not
	// ask to have restored.
	conn := openWAL(t, filepath.Join(f.env.StateDir, StoreFile))
	execSQL(t, conn, "CREATE TABLE t (v TEXT)")
	conn.Close()
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	// The new release never answers; the old one does once it is back.
	f.healthy = commitOf('a')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.current() != "v0.10.0" {
		t.Fatalf("current is %s after a rollback", f.current())
	}
	got := f.apply()
	if got.State != contract.UpdateApplyStateRolledBack || got.Error == nil || got.Error.Code != CodeHealthTimeout {
		t.Fatalf("after a failed health check: %+v %+v", got, got.Error)
	}
	if failed, _ := f.env.HasFailed("v0.11.0"); !failed {
		t.Fatal("the version that rolled back is not remembered")
	}
	if p := filepath.Join(f.env.BackupsDir(), "v0.10.0-v0.11.0.sqlite3"); !exists(p) {
		t.Fatal("no snapshot was taken before the update")
	}
	if exists(f.env.restorePath()) {
		t.Fatal("an additive release asked for the snapshot to be restored")
	}
	// Auto-apply does not try it again; a person asking does.
	if _, err := f.env.Begin(context.Background(), f.exe, Request{Auto: true}); errCode(err) != CodeVersionFailedBefore {
		t.Fatalf("auto-apply of a failed version: %v", err)
	}
	p, err := f.env.Begin(context.Background(), f.exe, Request{})
	if err != nil {
		t.Fatalf("a person asking again: %v", err)
	}
	p.Abandon()
}

func TestTheHealthWaitIsBounded(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := f.env.waitHealthy(ctx, install.ServiceFile{Port: 1}, Served{Commit: commitOf('b')})
	if errCode(err) != CodeHealthTimeout || time.Since(start) > 5*time.Second {
		t.Fatalf("waitHealthy: %v after %s", err, time.Since(start))
	}
}

// The real check, as the daemon serves it: BUILD.json answers only the
// local token as a bearer (the drill on a Mac rolled a healthy release back
// when it sent the orchestrator header). A release candidate and its final
// release share a commit, so only the version tells the restarted daemon
// from the one it replaced.
func TestHealthNeedsTheNewVersionNotOnlyItsCommit(t *testing.T) {
	var build string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/BUILD.json" {
			if r.Header.Get("Authorization") != "Bearer local-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, build)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	e := Env{StateDir: t.TempDir(), Poll: time.Millisecond, HealthWait: 100 * time.Millisecond}
	if err := os.WriteFile(filepath.Join(e.StateDir, "local-token"), []byte("local-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.StateDir, "orchestrator-token"), []byte("machine-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := install.ServiceFile{Port: port}
	want := Served{Commit: commitOf('b'), Version: "v1.0.0"}
	build = `{"stamp":"` + commitOf('b') + `","version":"v1.0.0-rc.1"}`
	if err := e.waitHealthy(context.Background(), svc, want); errCode(err) != CodeHealthTimeout {
		t.Fatalf("the release candidate still running passed as the final release: %v", err)
	}
	build = `{"stamp":"` + commitOf('b') + `","version":"v1.0.0"}`
	if err := e.waitHealthy(context.Background(), svc, want); err != nil {
		t.Fatalf("the final release: %v", err)
	}
	// A BUILD.json from before it carried a version is known by its commit.
	build = `{"stamp":"` + commitOf('b') + `"}`
	if err := e.waitHealthy(context.Background(), svc, want); err != nil {
		t.Fatalf("a versionless BUILD.json: %v", err)
	}
}

func TestANonAdditiveReleaseRestoresTheSnapshotOnRollback(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	db := filepath.Join(f.env.StateDir, StoreFile)
	conn := openWAL(t, db)
	execSQL(t, conn, "CREATE TABLE t (v TEXT)", "INSERT INTO t VALUES ('before')")
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, func(m *release.Manifest) {
		m.NonAdditiveMigration = true
	})
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	// The new release changes the store, then fails.
	execSQL(t, conn, "INSERT INTO t VALUES ('written by the new release')")
	conn.Close()
	p, _, _ := f.env.ReadPending()
	p.Phase, p.Reason = phaseRollback, &contract.UpdateApplyError{Code: CodeHealthTimeout, Detail: "test"}
	if err := f.env.writePending(p); err != nil {
		t.Fatal(err)
	}
	f.healthy = commitOf('a')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The old release's next start puts the snapshot back before the store
	// is opened.
	exit, note, err := f.env.BootGuard(f.exe)
	if err != nil || exit || !strings.Contains(note, "restored") {
		t.Fatalf("boot guard of the old release: exit=%v note=%q err=%v", exit, note, err)
	}
	conn = openWAL(t, db)
	defer conn.Close()
	if n := count(t, conn, "SELECT COUNT(*) FROM t"); n != 1 {
		t.Fatalf("after the restore the store has %d rows, want the 1 from before the update", n)
	}
}

func TestASupervisorKilledMidUpdateResumesFromPending(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	// The first supervisor switched current and was killed before the
	// health check settled: current is the new release, pending remains.
	if err := f.env.Layout.SwitchCurrent("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	p, _, _ := f.env.ReadPending()
	p.SupervisorRuns = 1
	f.env.writePending(p)
	f.healthy = commitOf('b')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateHealthy || f.current() != "v0.11.0" {
		t.Fatalf("resumed: %+v current %s", got, f.current())
	}

	// A supervisor started too often without settling gives up and rolls back.
	g := newFixture(t, "v0.10.0")
	g.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := g.update(Request{}); err != nil {
		t.Fatal(err)
	}
	q, _, _ := g.env.ReadPending()
	q.SupervisorRuns = SupervisorRunsLimit
	g.env.writePending(q)
	g.healthy = commitOf('a')
	if err := g.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := g.apply(); got.State != contract.UpdateApplyStateRolledBack || got.Error.Code != CodeSupervisorGaveUp || g.current() != "v0.10.0" {
		t.Fatalf("gave up: %+v current %s", got, g.current())
	}
	// With nothing pending, a supervisor started once more just exits.
	if err := g.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTheBootGuardRollsBackANewReleaseThatKeepsFailing(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	// The supervisor switched and died; the new binary starts, and starts.
	if err := f.env.Layout.SwitchCurrent("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	newExe := filepath.Join(f.env.Layout.ReleaseDir("v0.11.0"), "clawdline")
	// The old release starting is none of the guard's business.
	if exit, _, err := f.env.BootGuard(filepath.Join(f.env.Layout.ReleaseDir("v0.10.0"), "clawdline")); exit || err != nil {
		t.Fatalf("old release: %v %v", exit, err)
	}
	for i := 0; i < BootAttemptsLimit; i++ {
		if exit, _, err := f.env.BootGuard(newExe); exit || err != nil {
			t.Fatalf("start %d: exit=%v %v", i+1, exit, err)
		}
	}
	exit, note, err := f.env.BootGuard(newExe)
	if !exit || err != nil || !strings.Contains(note, "rolled back to v0.10.0") {
		t.Fatalf("start %d: exit=%v note=%q err=%v", BootAttemptsLimit+1, exit, note, err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateRolledBack || got.Error.Code != CodeBootGuard || f.current() != "v0.10.0" {
		t.Fatalf("boot guard: %+v current %s", got, f.current())
	}

	// Past the deadline, the first start already gives up.
	g := newFixture(t, "v0.10.0")
	g.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := g.update(Request{}); err != nil {
		t.Fatal(err)
	}
	g.env.Layout.SwitchCurrent("v0.11.0")
	g.clock = g.clock.Add(PendingDeadlineSecondsLimit*time.Second + time.Second)
	if exit, _, _ := g.env.BootGuard(filepath.Join(g.env.Layout.ReleaseDir("v0.11.0"), "clawdline")); !exit || g.current() != "v0.10.0" {
		t.Fatalf("past the deadline: exit=%v current %s", exit, g.current())
	}
}

func TestEveryRefusalBeforeTheDownloadHasACode(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	ctx := context.Background()

	// Nothing published yet.
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != CodeNoReleasePublished {
		t.Fatalf("404: %v", err)
	}
	f.publish("v0.10.0", commitOf('a'), daemonArchive(t, commitOf('a')), true, nil)
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != CodeAlreadyCurrent {
		t.Fatalf("same version: %v", err)
	}
	f.publish("v0.9.0", commitOf('9'), daemonArchive(t, commitOf('9')), false, nil)
	if _, err := f.env.Begin(ctx, f.exe, Request{Version: "v0.9.0"}); errCode(err) != CodeNotNewer {
		t.Fatalf("older without force: %v", err)
	}
	if p, err := f.env.Begin(ctx, f.exe, Request{Version: "v0.9.0", Force: true}); err != nil {
		t.Fatalf("older with force: %v", err)
	} else {
		p.Abandon()
	}
	f.publish("v0.12.0", commitOf('c'), daemonArchive(t, commitOf('c')), true, func(m *release.Manifest) { m.MinVersion = "v0.11.0" })
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != CodeVersionBelowMin {
		t.Fatalf("below min_version: %v", err)
	}

	// A bad signature.
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	f.mu.Lock()
	f.files["/latest/manifest.json"] = []byte(strings.Replace(string(f.files["/latest/manifest.json"]), "v0.11.0", "v0.11.1", 1))
	f.mu.Unlock()
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != release.CodeSignatureInvalid {
		t.Fatalf("tampered manifest: %v", err)
	}
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)

	// Lock contention.
	p, err := f.env.Begin(ctx, f.exe, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != CodeUpdateInProgress {
		t.Fatalf("a second update while the first holds the lock: %v", err)
	}
	p.Abandon()
	// A lock left by a process that died long ago is taken over.
	p, _ = f.env.Begin(ctx, f.exe, Request{})
	f.clock = f.clock.Add(LockStaleSecondsLimit*time.Second + time.Second)
	if q, err := f.env.Begin(ctx, f.exe, Request{}); err != nil {
		t.Fatalf("a stale lock: %v", err)
	} else {
		q.Abandon()
	}

	// A source checkout is not updated this way.
	if _, err := f.env.Begin(ctx, "/somewhere/else/clawdline", Request{}); errCode(err) != CodeNotAReleaseInstall {
		t.Fatalf("source checkout: %v", err)
	}
	// Nor a release install without a service to restart.
	os.Remove(filepath.Join(f.env.StateDir, install.ServiceFileName))
	if _, err := f.env.Begin(ctx, f.exe, Request{}); errCode(err) != CodeNoService {
		t.Fatalf("no service: %v", err)
	}
}

func TestADownloadThatIsNotTheReleaseChangesNothing(t *testing.T) {
	good := func(t *testing.T) []byte { return daemonArchive(t, commitOf('b')) }
	cases := []struct {
		name, want string
		serve      func(t *testing.T, f *fixture, m release.Manifest)
		edit       func(*release.Manifest)
	}{
		{name: "sha mismatch", want: release.CodeArtifactSHA256, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			b := good(t)
			b[len(b)-1] ^= 0xff
			f.files["/files/"+m.Artifacts[0].Name] = b
		}},
		{name: "truncated", want: release.CodeArtifactSize, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			f.files["/files/"+m.Artifacts[0].Name] = good(t)[:20]
		}},
		{name: "gone", want: CodeDownloadFailed, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			delete(f.files, "/files/"+m.Artifacts[0].Name)
		}},
		{name: "smoke mismatch", want: CodeSmokeFailed, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			f.smoke[f.env.Layout.ReleaseDir("v0.11.0")] = `{"version":"v0.11.0","commit":"` + commitOf('e') + `"}`
		}},
		{name: "binary does not run", want: CodeSmokeFailed, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			delete(f.smoke, f.env.Layout.ReleaseDir("v0.11.0"))
		}},
		{name: "supervisor does not start", want: CodeSupervisorFailed, serve: func(t *testing.T, f *fixture, m release.Manifest) {
			f.failCmd["systemd-run"] = true
		}},
		{name: "too large", want: CodeDownloadFailed, edit: func(m *release.Manifest) { m.Artifacts[0].Size = maxArtifactBytes + 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, "v0.10.0")
			m := f.publish("v0.11.0", commitOf('b'), good(t), true, c.edit)
			if c.serve != nil {
				f.mu.Lock()
				c.serve(t, f, m)
				f.mu.Unlock()
			}
			err := f.update(Request{})
			if errCode(err) != c.want {
				t.Fatalf("got %v, want %s", err, c.want)
			}
			if got := f.apply(); got.State != contract.UpdateApplyStateFailed || got.Error == nil || got.Error.Code != c.want {
				t.Fatalf("recorded %+v", got)
			}
			if f.current() != "v0.10.0" {
				t.Fatalf("current moved to %s", f.current())
			}
			if _, ok, _ := f.env.ReadPending(); ok {
				t.Fatal("a failed update left pending.json")
			}
			if _, err := os.Stat(f.env.lockPath()); err == nil {
				t.Fatal("a failed update kept the lock")
			}
			if ents, _ := os.ReadDir(f.env.Layout.Staging); len(ents) != 0 {
				t.Fatalf("staging kept %d files", len(ents))
			}
		})
	}
}

func TestAnArchiveCannotWriteOutsideItsRelease(t *testing.T) {
	cases := map[string][]entry{
		"climbs out":            {{name: "../evil", body: "x"}},
		"climbs out deeper":     {{name: "dist/../../evil", body: "x"}},
		"absolute":              {{name: "/tmp/evil", body: "x"}},
		"symlink out":           {{name: "dist", typ: tar.TypeSymlink, link: "../../outside"}},
		"absolute symlink":      {{name: "link", typ: tar.TypeSymlink, link: "/etc"}},
		"through a symlink":     {{name: "d", typ: tar.TypeSymlink, link: "."}, {name: "d/x", body: "x"}},
		"hard link":             {{name: "h", typ: tar.TypeLink, link: "clawdline"}},
		"device":                {{name: "dev", typ: tar.TypeChar}},
		"no binary at the root": {{name: "dist/BUILD.json", body: "{}"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "v0.10.0")
			// A binary at the root, so the only thing wrong is the entry.
			if name != "no binary at the root" {
				entries = append([]entry{{name: "clawdline", body: "x"}}, entries...)
			}
			f.publish("v0.11.0", commitOf('b'), tarGz(t, entries...), true, nil)
			if err := f.update(Request{}); errCode(err) != CodeArchiveUnsafe {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat(f.env.Layout.ReleaseDir("v0.11.0")); err == nil {
				t.Fatal("a refused archive left releases/v0.11.0")
			}
			for _, at := range []string{f.env.Layout.Releases, f.env.Layout.Root, filepath.Dir(f.env.Layout.Root)} {
				if _, err := os.Lstat(filepath.Join(at, "evil")); err == nil {
					t.Fatalf("an entry escaped to %s", at)
				}
			}
		})
	}
	// Symlinks inside the release are kept, as an app bundle's are.
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), tarGz(t, entry{name: "clawdline", body: "x"},
		entry{name: "lib/", typ: tar.TypeDir}, entry{name: "lib/a", body: "a"},
		entry{name: "lib/current", typ: tar.TypeSymlink, link: "a"}), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(f.env.Layout.ReleaseDir("v0.11.0"), "lib", "current")); err != nil || target != "a" {
		t.Fatalf("inside symlink: %q %v", target, err)
	}
}

func TestASnapshotTakenWhileTheWALHoldsPagesIsComplete(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	db := filepath.Join(f.env.StateDir, StoreFile)
	conn := openWAL(t, db)
	defer conn.Close()
	// No checkpoint: every committed page stays in the WAL, and the writer
	// keeps its connection open as the daemon does.
	execSQL(t, conn, "PRAGMA wal_autocheckpoint=0", "CREATE TABLE t (v TEXT)")
	for i := 0; i < 500; i++ {
		execSQL(t, conn, "INSERT INTO t VALUES ('row')")
	}
	if info, err := os.Stat(db + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("the WAL is not holding pages: %v", err)
	}
	// And a transaction still open on another connection is not in it.
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO t VALUES ('uncommitted')"); err != nil {
		t.Fatal(err)
	}
	snap, err := f.env.Snapshot("v0.10.0", "v0.11.0")
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	s, err := sql.Open("sqlite", snap+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var check string
	if err := s.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity: %q %v", check, err)
	}
	if n := count(t, s, "SELECT COUNT(*) FROM t"); n != 500 {
		t.Fatalf("the snapshot has %d rows, want the 500 committed", n)
	}
	// Only the newest BackupsKeptLimit are kept.
	for _, pair := range [][2]string{{"v0.11.0", "v0.12.0"}, {"v0.12.0", "v0.13.0"}} {
		if _, err := f.env.Snapshot(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ents, _ := os.ReadDir(f.env.BackupsDir())
	if len(ents) != BackupsKeptLimit {
		t.Fatalf("%d snapshots kept, want %d", len(ents), BackupsKeptLimit)
	}
}

func TestTheStatusOfAReleaseInstall(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	d := NewDaemon(f.env, f.exe)
	// Nothing read yet, then nothing published: unknown, with the reason a
	// caller branches on, and never an error loop.
	if st := d.Status(); st.State != contract.UpdateStateUnknown || st.InstallKind != contract.UpdateInstallKindRelease {
		t.Fatalf("before a check: %+v", st)
	}
	d.Checker.Refresh(context.Background())
	st := d.Status()
	if st.State != contract.UpdateStateUnknown || st.Reason != CodeNoReleasePublished {
		t.Fatalf("404: %+v", st)
	}
	if st.Running.Version != "v0.10.0" || st.Running.Stamp != commitOf('a') || st.Apply == nil || st.Apply.State != contract.UpdateApplyStateIdle {
		t.Fatalf("running: %+v apply %+v", st.Running, st.Apply)
	}
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	d.Checker.Refresh(context.Background())
	st = d.Status()
	if st.State != contract.UpdateStateUpdateAvailable || st.Latest.Version != "v0.11.0" || st.Latest.NotesURL == "" ||
		st.Channel != ChannelStable || !strings.HasSuffix(st.SourceURL, "/latest/manifest.json") {
		t.Fatalf("newer: %+v", st)
	}
	// A failed check keeps the last good answer.
	f.mu.Lock()
	f.files["/latest/manifest.sig.json"] = []byte("[]")
	f.mu.Unlock()
	d.Checker.Refresh(context.Background())
	if st := d.Status(); st.State != contract.UpdateStateUpdateAvailable || st.Error == "" {
		t.Fatalf("after a failed check: %+v", st)
	}
	// An unreadable apply state is not idle.
	os.MkdirAll(f.env.UpdateDir(), 0o700)
	os.WriteFile(f.env.statusPath(), []byte("{"), 0o600)
	if st := d.Status(); st.Apply == nil || st.Apply.State != contract.UpdateApplyStateFailed || st.Apply.Error.Code != CodeStateUnreadable {
		t.Fatalf("unreadable status: %+v", st.Apply)
	}
}

func TestBetaFollowsTheNewestPublishedRelease(t *testing.T) {
	f := newFixture(t, "v0.10.0-beta.1")
	f.env.ReleaseURL = ""
	f.mu.Lock()
	f.files["/api/releases"] = []byte(`[{"tag_name":"v0.11.0-beta.2","draft":true},{"tag_name":"v0.10.0"},{"tag_name":"v0.11.0-beta.1","prerelease":true},{"tag_name":"junk"}]`)
	f.mu.Unlock()
	if ChannelFor("v0.10.0-beta.1") != ChannelBeta || ChannelFor("v0.10.0") != ChannelStable {
		t.Fatal("channel from the running version")
	}
	base, err := f.env.Base(context.Background(), ChannelBeta, "")
	if err != nil || base != DefaultDownloadRoot+"/v0.11.0-beta.1" {
		t.Fatalf("beta base: %q %v", base, err)
	}
	f.mu.Lock()
	f.files["/api/releases"] = []byte(`[]`)
	f.mu.Unlock()
	if _, err := f.env.Base(context.Background(), ChannelBeta, ""); errCode(err) != CodeNoReleasePublished {
		t.Fatalf("no releases: %v", err)
	}
}

func TestAutoApplyWaitsForIdleSessionsAndStableReleases(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	d := NewDaemon(f.env, f.exe)
	on, busy := true, true
	d.AutoApply = func() bool { return on }
	d.Busy = func(context.Context) bool { return busy }
	d.Logf = t.Logf
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	check := d.Checker.Refresh(context.Background())

	d.maybeAutoApply(context.Background(), check)
	d.Wait()
	if f.apply().State != contract.UpdateApplyStateIdle {
		t.Fatal("applied while a session was busy")
	}
	busy, on = false, false
	d.maybeAutoApply(context.Background(), check)
	d.Wait()
	if f.apply().State != contract.UpdateApplyStateIdle {
		t.Fatal("applied with the setting off")
	}
	on = true
	d.maybeAutoApply(context.Background(), check)
	d.Wait()
	if got := f.apply(); got.State != contract.UpdateApplyStateRestarting || got.To != "v0.11.0" {
		t.Fatalf("idle and on: %+v", got)
	}

	// A pre-release is never applied by itself.
	g := newFixture(t, "v0.10.0")
	e := NewDaemon(g.env, g.exe)
	e.AutoApply = func() bool { return true }
	g.publish("v0.11.0-beta.1", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	e.maybeAutoApply(context.Background(), e.Checker.Refresh(context.Background()))
	e.Wait()
	if g.apply().State != contract.UpdateApplyStateIdle {
		t.Fatal("a beta was auto-applied")
	}
}

func TestTheLaunchdSupervisorIsAOneShotAgentFromTheOldRelease(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	svc := install.ServiceFile{Supervisor: "launchd", Name: "com.example.clawdline-next.test", Domain: "gui/501", Port: 17821}
	install.WriteServiceFile(f.env.StateDir, svc)
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(f.env.UpdateDir(), "com.example.clawdline-next.test.update.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<key>SuccessfulExit</key><false/>", filepath.Join(f.env.Layout.ReleaseDir("v0.10.0"), "clawdline"), "<string>finish</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("the plist lacks %q:\n%s", want, plist)
		}
	}
	if !f.ran("launchctl bootstrap gui/501 ") {
		t.Fatalf("calls %v", f.calls)
	}
	f.healthy = commitOf('b')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.ran("launchctl kickstart -k gui/501/com.example.clawdline-next.test") || !f.ran("launchctl bootout gui/501/com.example.clawdline-next.test.update") {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestTheAppBundleIsSwappedOnlyWhenItIsNotRunning(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.env.GOOS, f.env.GOARCH, f.env.AppsDir = "darwin", "arm64", filepath.Join(t.TempDir(), "Applications")
	installed := filepath.Join(f.env.AppsDir, AppBundleName)
	os.MkdirAll(filepath.Join(installed, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(installed, "Contents", "old"), []byte("old"), 0o644)
	app := tarGz(t, entry{name: AppBundleName + "/", typ: tar.TypeDir}, entry{name: AppBundleName + "/Contents/new", body: "new"})
	f.mu.Lock()
	f.files["/files/app.tar.gz"] = app
	f.mu.Unlock()
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, func(m *release.Manifest) {
		m.Artifacts[0].OS, m.Artifacts[0].Arch = "darwin", "arm64"
		a := release.Artifact{OS: "darwin", Arch: "arm64", Kind: release.KindApp, Name: "app.tar.gz", URL: f.srv.URL + "/files/app.tar.gz", Size: int64(len(app))}
		a.SHA256 = sha(app)
		m.Artifacts = append(m.Artifacts, a)
	})
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	p, _, _ := f.env.ReadPending()
	if p.StagedApp == "" {
		t.Fatal("the app was not staged")
	}
	// The app is running: it stays staged.
	runFake := f.env.Run
	f.env.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ps" {
			return []byte("/sbin/launchd\n" + installed + "/Contents/MacOS/Clawdline Next\n"), nil
		}
		return runFake(ctx, name, args...)
	}
	f.healthy = commitOf('b')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateHealthy || got.StagedApp != p.StagedApp {
		t.Fatalf("running app: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(installed, "Contents", "old")); err != nil {
		t.Fatal("a running app's bundle was changed")
	}
	// Still running: the daemon's poll leaves it staged.
	if swapped, err := f.env.SettleApp(context.Background()); err != nil || swapped {
		t.Fatalf("settle while running: %v %v", swapped, err)
	}
	// Quit: the poll swaps it in, keeps the replaced bundle with its
	// release, and the status no longer names a staged app.
	f.env.Run = runFake
	if swapped, err := f.env.SettleApp(context.Background()); err != nil || !swapped {
		t.Fatalf("settle after quit: %v %v", swapped, err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateHealthy || got.StagedApp != "" {
		t.Fatalf("after the swap: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(installed, "Contents", "new")); err != nil {
		t.Fatal("the new bundle is not installed")
	}
	if _, err := os.Stat(filepath.Join(f.env.Layout.ReleaseDir("v0.10.0"), "app", AppBundleName, "Contents", "old")); err != nil {
		t.Fatal("the replaced bundle was not kept")
	}
}

func openWAL(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func execSQL(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// A new release that exits at once is restarted by its service faster than
// the supervisor's health wait ends (systemd restarts it every 3 s), so its
// boot guard gives the update up while the supervisor still waits. What the
// guard recorded is the reason; the supervisor stops waiting and does not
// write the update back as pending or restart the service again.
func TestTheSupervisorKeepsWhatTheBootGuardDecided(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	newExe := filepath.Join(f.env.Layout.ReleaseDir("v0.11.0"), "clawdline")
	f.env.HealthWait = 10 * time.Second
	starts := 0
	f.env.Health = func(_ context.Context, _ int, _ string, want Served) error {
		// Each poll is one more start of the new release, until its guard
		// switches back; the old release then answers, never the new one.
		if starts <= BootAttemptsLimit {
			starts++
			if _, _, err := f.env.BootGuard(newExe); err != nil {
				t.Errorf("boot guard: %v", err)
			}
		}
		if want.Version == "v0.10.0" && f.current() == "v0.10.0" {
			return nil
		}
		return errors.New("not answering")
	}
	start := time.Now()
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the supervisor waited %s after the boot guard had settled the update", took)
	}
	got := f.apply()
	if got.State != contract.UpdateApplyStateRolledBack || got.Error == nil || got.Error.Code != CodeBootGuard {
		t.Fatalf("the supervisor replaced the boot guard's record: %+v %+v", got, got.Error)
	}
	if _, pending, _ := f.env.ReadPending(); pending {
		t.Fatal("the supervisor wrote back an update the boot guard had settled")
	}
	if f.current() != "v0.10.0" {
		t.Fatalf("current is %s", f.current())
	}
}
