package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestBrowserPrepareRequiresAPersonAndOnlyWritesItsOwnPayload(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"browser_mode": "user_chrome"}); err != nil {
		t.Fatal(err)
	}
	device := func(method, body string, caps ...auth.Capability) *http.Request {
		return httptest.NewRequest(method, "/v1/browser/prepare", strings.NewReader(body)).WithContext(context.WithValue(context.Background(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(caps...)}}))
	}
	call := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.browserPrepareRoute(rec, req)
		return rec
	}
	for _, req := range []*http.Request{device("POST", "{}", auth.Read), device("POST", "{}").WithContext(context.WithValue(context.Background(), accessKey{}, access{machine: true})), device("POST", "{}")} {
		if rec := call(req); rec.Code != http.StatusForbidden {
			t.Fatalf("unauthorized: %d %s", rec.Code, rec.Body)
		}
	}
	if _, err := os.Stat(browserlaunch.ExtensionDir(dir)); !os.IsNotExist(err) {
		t.Fatal("refused request wrote payload")
	}
	if rec := call(device("POST", `{"directory":"/tmp/other"}`, auth.Send)); rec.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary path: %d", rec.Code)
	}
	if rec := call(device("GET", "", auth.Send)); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET wrote payload: %d", rec.Code)
	}
	rec := call(device("POST", "{}", auth.Send))
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare: %d %s", rec.Code, rec.Body)
	}
	var got browserlaunch.PreparedExtension
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got.Directory, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"browser-host-user-chrome.sh", "chrome-profile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("preparation touched %s", name)
		}
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"browser_mode": "native"}); err != nil {
		t.Fatal(err)
	}
	if rec := call(device("POST", "{}", auth.Send)); rec.Code != http.StatusConflict {
		t.Fatalf("wrong mode: %d %s", rec.Code, rec.Body)
	}
}
