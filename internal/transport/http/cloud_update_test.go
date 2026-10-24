package http

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/productcopy"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func cloudDiskSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCloudUpdateRealHTTPHealthAndManagement(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "accepted", false: "wrong_stamp_rollback"}[healthy], func(t *testing.T) {
			f := newTermFixture(t, newFakeTerms())
			defer SetCloudLine(f.dir, nil)
			layout := install.NewLayout(filepath.Join(t.TempDir(), "install"), filepath.Join(t.TempDir(), "bin"))
			oldCommit, newCommit := strings.Repeat("a", 40), strings.Repeat("b", 40)
			write := func(path string, value any) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, commit := range map[string]string{"v2.0.0": oldCommit, "v2.0.1": newCommit} {
				write(filepath.Join(layout.ReleaseDir(name), "clawdline"), "synthetic executable")
				write(filepath.Join(layout.ReleaseDir(name), "dist", "BUILD.json"), map[string]string{"stamp": commit, "version": name})
			}
			if err := layout.SwitchCurrent("v2.0.1"); err != nil {
				t.Fatal(err)
			}
			web := t.TempDir()
			if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>Synthetic console</title>"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLAWDLINE_NEXT_WEB", web)
			write(filepath.Join(web, "BUILD.json"), map[string]string{"stamp": newCommit, "version": "v2.0.1"})
			server := httptest.NewServer(f.s.Handler())
			defer server.Close()
			_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			port, _ := strconv.Atoi(portText)
			if err := install.WriteServiceFile(f.dir, install.ServiceFile{Supervisor: "systemd", Name: "synthetic.service", Root: layout.Root, Port: port}); err != nil {
				t.Fatal(err)
			}
			env := updater.Env{Layout: layout, StateDir: f.dir, GOOS: "linux", Poll: time.Millisecond, HealthWait: 100 * time.Millisecond}
			write(filepath.Join(env.UpdateDir(), "pending.json"), updater.Pending{From: "v2.0.0", To: "v2.0.1", FromCommit: oldCommit, ToCommit: newCommit, Phase: "switch", Deadline: time.Now().Add(time.Hour)})
			write(filepath.Join(env.UpdateDir(), "status.json"), contract.UpdateApply{State: contract.UpdateApplyStateRestarting, From: "v2.0.0", To: "v2.0.1"})
			ready := env.CloudAdmission(filepath.Join(layout.ReleaseDir("v2.0.1"), "clawdline"), updater.Served{Version: "v2.0.1", Commit: newCommit})
			SetCloudUnavailable(f.dir)
			ask := func(path, body string) (int, string) {
				t.Helper()
				method := http.MethodGet
				if body != "" {
					method = http.MethodPost
				}
				req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+f.local)
				if body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				b, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				return resp.StatusCode, string(b)
			}
			restarts := 0
			env.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "systemctl" || !strings.Contains(strings.Join(args, " "), "restart") {
					return nil, nil
				}
				restarts++
				if ready() {
					t.Fatal("Cloud admitted before real HTTP health acceptance")
				}
				baseline := cloudDiskSnapshot(t, f.dir)
				status, body := ask("/v1/cloud/devices/revoke", `{"device":"viewer"}`)
				if status != 503 || !strings.Contains(body, "cloud_runtime_unavailable") {
					t.Fatalf("pending revoke: %d %s", status, body)
				}
				if status, _ := ask("/", ""); status != 200 {
					t.Fatalf("pending console health: %d", status)
				}
				if !reflect.DeepEqual(baseline, cloudDiskSnapshot(t, f.dir)) {
					t.Fatal("pending HTTP management wrote disk")
				}
				current, _ := layout.CurrentRelease()
				stamp, version := newCommit, "v2.0.1"
				if !healthy {
					stamp = strings.Repeat("c", 40)
				}
				if current == "v2.0.0" {
					stamp, version = oldCommit, "v2.0.0"
				}
				write(filepath.Join(web, "BUILD.json"), map[string]string{"stamp": stamp, "version": version})
				return nil, nil
			}
			if err := env.Finish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if ready() != healthy {
				t.Fatalf("after real HTTP acceptance: ready=%v healthy=%v", ready(), healthy)
			}
			a, err := env.ReadApply()
			if err != nil {
				t.Fatal(err)
			}
			want := contract.UpdateApplyStateHealthy
			if !healthy {
				want = contract.UpdateApplyStateRolledBack
			}
			if a.State != want {
				t.Fatalf("receipt: %+v", a)
			}
			t.Logf("real HTTP / and /BUILD.json: healthy=%v restarts=%d receipt=%s candidate Cloud admitted=%v; pending revoke HTTP 503 without mutation", healthy, restarts, a.State, ready())
		})
	}
}

func TestCloudUpdateUnavailableRejectsManagementWithoutDiskWrites(t *testing.T) {
	const message = "The Cloud connection is not available in this daemon."
	key := fixedRefusalKey(message)
	if key == "" || productcopy.HTTPRefusalText("zh-Hant", key, message) == message {
		t.Fatal("Cloud update refusal must have English and Taiwan Traditional Chinese copy")
	}
	f := newTermFixture(t, newFakeTerms())
	// Real durable pin/key stores, all synthetic and confined to this fixture.
	pins := adaptercloud.NewPinnedStore(filepath.Join(f.dir, "synthetic-pins"))
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pins.Pin("synthetic-account", adaptercloud.PinnedDevice{DeviceID: "viewer", Scope: adaptercloud.ScopeReadOnly, PublicKey: base64.StdEncoding.EncodeToString(pub)}); err != nil {
		t.Fatal(err)
	}
	keys, err := cloudkeys.Open(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domaincloud.LoadOrCreateMasterSecret(keys, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.LoadOrCreateContentKey(nil); err != nil {
		t.Fatal(err)
	}
	baseline := cloudDiskSnapshot(t, f.dir)
	t.Cleanup(func() { SetCloudLine(f.dir, nil) })
	for _, blocked := range []bool{false, true} {
		if blocked {
			SetCloudUnavailable(f.dir)
		} else {
			SetCloudLine(f.dir, nil)
		}
		for _, path := range []string{"/v1/cloud/pairing", "/v1/cloud/pairing/offer", "/v1/cloud/devices/revoke", "/v1/cloud/devices/grant", "/v1/cloud/keys/rotate"} {
			got := f.do(http.MethodPost, path, f.local, `{"device":"viewer","scope":"operate","offer":"synthetic","confirm":true}`)
			wantStatus, wantCode := http.StatusConflict, "cloud_off"
			if blocked {
				wantStatus, wantCode = http.StatusServiceUnavailable, "cloud_runtime_unavailable"
			}
			if got.Code != wantStatus || !strings.Contains(got.Body.String(), wantCode) {
				t.Fatalf("blocked=%v %s: %d %s", blocked, path, got.Code, got.Body)
			}
			if !reflect.DeepEqual(baseline, cloudDiskSnapshot(t, f.dir)) {
				t.Fatalf("%s changed durable pins/journal/keys", path)
			}
			t.Logf("blocked=%v %s: HTTP %d code=%s disk unchanged", blocked, path, got.Code, wantCode)
		}
	}
	SetCloudLine(f.dir, pairingOnlyLine{pairing: &cloudtransport.Pairing{Pinned: pins}})
	got := f.do(http.MethodPost, "/v1/cloud/devices/grant", f.local, `{"device":"viewer","scope":"operate"}`)
	if got.Code != http.StatusOK {
		t.Fatalf("resumed grant: %d %s", got.Code, got.Body)
	}
	if scope, err := pins.ViewerScope("viewer"); err != nil || scope != adaptercloud.ScopeOperate {
		t.Fatalf("resumed scope %s %v", scope, err)
	}
	if reflect.DeepEqual(baseline, cloudDiskSnapshot(t, f.dir)) {
		t.Fatal("resumed grant did not exercise durable mutation")
	}
}
