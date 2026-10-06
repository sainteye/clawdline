package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// fixture is a release install under a temp root, a release host serving
// signed manifests, and a runner that stands in for systemctl, launchctl,
// ps and the new binary's `version --json`.
type fixture struct {
	t     *testing.T
	env   Env
	exe   string
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	srv   *httptest.Server
	clock time.Time

	mu    sync.Mutex
	files map[string][]byte // served path → body
	calls []string
	// smoke is what `<dir>/clawdline version --json` prints, by release dir.
	smoke map[string]string
	// failCmd makes a command whose joined text contains the key fail.
	failCmd map[string]bool
	// healthy is the commit the fake daemon answers health with; "" never.
	healthy string
}

func commitOf(c byte) string { return strings.Repeat(string(c), 40) }

func newFixture(t *testing.T, running string) *fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, pub: pub, priv: priv, files: map[string][]byte{}, smoke: map[string]string{},
		failCmd: map[string]bool{}, clock: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body, ok := f.files[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	base := t.TempDir()
	layout := install.NewLayout(filepath.Join(base, "root"), filepath.Join(base, "bin"))
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	f.env = Env{
		Layout: layout, StateDir: state, GOOS: "linux", GOARCH: "amd64",
		Keys: []ed25519.PublicKey{pub}, Client: f.srv.Client(), Run: f.run,
		Now: func() time.Time { return f.clock }, ReleaseURL: f.srv.URL + "/latest",
		ReleasesAPI: f.srv.URL + "/api/releases", Poll: time.Millisecond, HealthWait: 200 * time.Millisecond,
		Health: func(_ context.Context, _ int, _ string, want Served) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.healthy != "" && (want.Commit == "" || want.Commit == f.healthy) {
				return nil
			}
			return errors.New("not answering")
		},
	}
	f.installRelease(running, commitOf('a'))
	if err := layout.SwitchCurrent(running); err != nil {
		t.Fatal(err)
	}
	f.exe = filepath.Join(layout.Current, "clawdline")
	if err := install.WriteServiceFile(state, install.ServiceFile{Supervisor: "systemd", Name: "clawdline-next.service", Port: 1, Root: layout.Root}); err != nil {
		t.Fatal(err)
	}
	return f
}

// installRelease unpacks a release by hand, as setup or an earlier update did.
func (f *fixture) installRelease(version, commit string) {
	f.t.Helper()
	dir := f.env.Layout.ReleaseDir(version)
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clawdline"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"stamp": commit, "committed_at": "2026-10-01T00:00:00Z"})
	if err := os.WriteFile(filepath.Join(dir, "dist", "BUILD.json"), b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	fails := false
	for k := range f.failCmd {
		if strings.Contains(line, k) {
			fails = true
		}
	}
	smoke, isSmoke := f.smoke[filepath.Dir(name)]
	f.mu.Unlock()
	if fails {
		return nil, fmt.Errorf("%s: injected failure", line)
	}
	if filepath.Base(name) == "clawdline" && len(args) == 2 && args[0] == "version" {
		if !isSmoke {
			return nil, errors.New("exit status 1")
		}
		return []byte(smoke), nil
	}
	return nil, nil
}

func (f *fixture) ran(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

type entry struct {
	name, body, link string
	typ              byte
}

func tarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: e.typ, Linkname: e.link}
		if e.typ == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// daemonArchive is a well-formed daemon archive for commit.
func daemonArchive(t *testing.T, commit string) []byte {
	b, _ := json.Marshal(map[string]string{"stamp": commit})
	return tarGz(t, entry{name: "clawdline", body: "#!/bin/sh\n"}, entry{name: "dist/", typ: tar.TypeDir},
		entry{name: "dist/BUILD.json", body: string(b)})
}

// publish serves a signed release at /<version>/ and, with latest, at
// /latest/. The archive is served as is; manifest overrides change the
// manifest after the archive's size and sha256 are filled in.
func (f *fixture) publish(version, commit string, archive []byte, latest bool, edit func(*release.Manifest)) release.Manifest {
	f.t.Helper()
	sum := sha256.Sum256(archive)
	name := "clawdline_" + version + "_linux_amd64.tar.gz"
	m := release.Manifest{Version: version, Commit: commit, CommittedAt: "2026-10-05T00:00:00Z", Channel: "stable",
		NotesURL: "https://example.invalid/notes/" + version,
		Artifacts: []release.Artifact{{OS: "linux", Arch: "amd64", Kind: release.KindDaemon, Name: name,
			URL: f.srv.URL + "/files/" + name, Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:])}}}
	if edit != nil {
		edit(&m)
	}
	mb, _ := json.Marshal(m)
	sb, _ := json.Marshal([]release.Signature{release.Sign(mb, f.priv)})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files["/files/"+name] = archive
	for _, at := range []string{"/" + version} {
		f.files[at+"/manifest.json"] = mb
		f.files[at+"/manifest.sig.json"] = sb
	}
	if latest {
		f.files["/latest/manifest.json"] = mb
		f.files["/latest/manifest.sig.json"] = sb
	}
	f.smoke[f.env.Layout.ReleaseDir(version)] = fmt.Sprintf(`{"version":%q,"commit":%q,"os":"linux","arch":"amd64"}`, version, commit)
	return m
}

func (f *fixture) apply() contract.UpdateApply {
	f.t.Helper()
	a, err := f.env.ReadApply()
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func (f *fixture) current() string {
	name, _ := f.env.Layout.CurrentRelease()
	return name
}

func errCode(err error) string {
	var ue *Error
	if errors.As(err, &ue) {
		return ue.Code
	}
	var re *release.Error
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// begin runs Begin and Execute as the route does, synchronously.
func (f *fixture) update(req Request) error {
	f.t.Helper()
	p, err := f.env.Begin(context.Background(), f.exe, req)
	if err != nil {
		return err
	}
	return f.env.Execute(context.Background(), p)
}
