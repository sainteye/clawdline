package http

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// releaseUpdateServer is a daemon running from release v0.10.0 of a test
// layout, with a release host that publishes `published` (signed by a test
// key) and a hosted BUILD.json that counts how often it is asked.
func releaseUpdateServer(t *testing.T, dir, published string) (*Server, *atomic.Int64) {
	t.Helper()
	commit := strings.Repeat("a", 40)
	base := t.TempDir()
	layout := install.NewLayout(filepath.Join(base, "root"), filepath.Join(base, "bin"))
	rel := layout.ReleaseDir("v0.10.0")
	os.MkdirAll(filepath.Join(rel, "dist"), 0o755)
	os.WriteFile(filepath.Join(rel, "clawdline"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(rel, "dist", "BUILD.json"), []byte(`{"stamp":"`+commit+`"}`), 0o644)
	if err := layout.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	files := map[string][]byte{}
	if published != "" {
		sum := sha256.Sum256([]byte("x"))
		m, _ := json.Marshal(release.Manifest{Version: published, Commit: commit, Channel: "stable",
			Artifacts: []release.Artifact{{OS: "linux", Arch: "amd64", Kind: release.KindDaemon, Name: "a.tar.gz",
				URL: "http://127.0.0.1:1/a.tar.gz", Size: 1, SHA256: hex.EncodeToString(sum[:])}}})
		sig, _ := json.Marshal([]release.Signature{release.Sign(m, priv)})
		files["/latest/manifest.json"], files["/latest/manifest.sig.json"] = m, sig
	}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(host.Close)
	var hostedHits atomic.Int64
	hosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostedHits.Add(1)
		w.Write([]byte(`{"stamp":"` + strings.Repeat("f", 40) + `","committed_at":"2030-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(hosted.Close)
	env := updater.Env{Layout: layout, StateDir: dir, GOOS: "linux", GOARCH: "amd64",
		Keys: []ed25519.PublicKey{pub}, Client: host.Client(), Now: time.Now, ReleaseURL: host.URL + "/latest"}
	s := &Server{cfg: config.Config{Dir: dir, Port: 7757},
		update:        &updatecheck.Checker{URL: hosted.URL, Running: func() contract.BuildStamp { return contract.BuildStamp{Stamp: commit} }},
		releaseUpdate: updater.NewDaemon(env, filepath.Join(layout.Current, "clawdline"))}
	return s, &hostedHits
}

// A release install compares release versions from its signed manifest and
// never asks the hosted console's BUILD.json, which on 2026-10-06 named a
// commit no release carries: the same version is current, whatever the
// hosted stamp says.
func TestAReleaseInstallNeverReadsTheHostedBuild(t *testing.T) {
	s, hostedHits := releaseUpdateServer(t, t.TempDir(), "v0.10.0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.runUpdateCheck(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for s.releaseUpdate.Checker.Last().Latest == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	rec := httptest.NewRecorder()
	s.updateRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/update", nil))
	var st contract.UpdateStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.State != contract.UpdateStateCurrent || st.InstallKind != contract.UpdateInstallKindRelease ||
		st.Running.Version != "v0.10.0" || st.Latest.Version != "v0.10.0" {
		t.Fatalf("a release install: %s", rec.Body)
	}
	if n := hostedHits.Load(); n != 0 {
		t.Fatalf("the hosted BUILD.json was asked %d times", n)
	}

	// With nothing published the answer is unknown, with the reason a
	// console branches on, and still no hosted read.
	s, hostedHits = releaseUpdateServer(t, t.TempDir(), "")
	s.releaseUpdate.Checker.Refresh(context.Background())
	rec = httptest.NewRecorder()
	s.updateRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/update", nil))
	json.Unmarshal(rec.Body.Bytes(), &st)
	if st.State != contract.UpdateStateUnknown || st.Reason != updater.CodeNoReleasePublished || hostedHits.Load() != 0 {
		t.Fatalf("nothing published: %s (hosted asked %d times)", rec.Body, hostedHits.Load())
	}
}

// POST /v1/update/apply restarts this daemon. Paired devices reach validation,
// and every refusal after that carries a code.
func TestApplyingAnUpdateNeedsPairingAndAReleaseInstall(t *testing.T) {
	f, _ := newGateFixture(t)
	s, _ := releaseUpdateServer(t, f.g.dir, "v0.11.0")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/update/apply", s.updateApplyRoute)
	mux.HandleFunc("/v1/update", s.updateRoute)
	h := f.g.wrap(mux)
	bearer := func(tok string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"}
	}

	if rec := (call{path: "/v1/update/apply", headers: bearer("unpaired")}).do(h); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unpaired device: %d %s", rec.Code, rec.Body)
	}
	if rec := (call{method: http.MethodGet, path: "/v1/update/apply", headers: bearer(f.send)}).do(h); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	if rec := (call{path: "/v1/update/apply", body: `{"force":true}`, headers: bearer(f.send)}).do(h); rec.Code != http.StatusBadRequest {
		t.Fatalf("force without a version: %d %s", rec.Code, rec.Body)
	} else {
		var refused contract.Refusal
		if err := json.Unmarshal(rec.Body.Bytes(), &refused); err != nil || refused.Error != "bad_request" ||
			refused.Detail != "force needs the version it installs" || refused.DetailKey != "http.53e1be922753ea67" {
			t.Fatalf("fixed update refusal lost wire detail or explicit catalog key: %s (%v)", rec.Body, err)
		}
	}
	if rec := (call{path: "/v1/update/apply", body: `{"nonsense":1}`, headers: bearer(f.send)}).do(h); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d %s", rec.Code, rec.Body)
	} else {
		var refused contract.Refusal
		if err := json.Unmarshal(rec.Body.Bytes(), &refused); err != nil || refused.Error != "bad_request" ||
			!strings.Contains(refused.Detail, "nonsense") || refused.DetailKey != "" {
			t.Fatalf("parser detail was mistaken for fixed copy: %s (%v)", rec.Body, err)
		}
	}
	// Installed without a service, a release install cannot restart.
	rec := (call{path: "/v1/update/apply", headers: bearer(f.send)}).do(h)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), updater.CodeNoService) {
		t.Fatalf("no service: %d %s", rec.Code, rec.Body)
	}
	// A pending update refuses a second one.
	install.WriteServiceFile(f.g.dir, install.ServiceFile{Supervisor: "systemd", Name: "x.service", Port: 1})
	os.MkdirAll(filepath.Join(f.g.dir, "update"), 0o700)
	os.WriteFile(filepath.Join(f.g.dir, "update", "pending.json"), []byte(`{"from":"v0.10.0","to":"v0.11.0"}`), 0o600)
	rec = (call{path: "/v1/update/apply", headers: map[string]string{"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}).do(h)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), updater.CodeUpdateInProgress) {
		t.Fatalf("in progress: %d %s", rec.Code, rec.Body)
	}
	// The CLI on this machine follows the update it started with the same
	// token it started it with.
	rec = (call{method: http.MethodGet, path: "/v1/update", headers: map[string]string{"X-Clawdline-Orchestrator": f.machine}}).do(h)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"install_kind":"release"`) {
		t.Fatalf("the machine reading its update: %d %s", rec.Code, rec.Body)
	}
	// And a daemon that is not a release install says so.
	plain := &Server{cfg: config.Config{Dir: t.TempDir()}, update: &updatecheck.Checker{Disabled: true}}
	plain.updateOnce.Do(func() {})
	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/update/apply", nil)
	r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{machine: true}))
	plain.updateApplyRoute(rec, r)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), updater.CodeNotAReleaseInstall) {
		t.Fatalf("not a release install: %d %s", rec.Code, rec.Body)
	}
}
