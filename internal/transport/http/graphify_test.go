package http

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphifyProjectReadCrossesAuthenticationAndMachineOptIn(t *testing.T) {
	s := projectCloudStandIn(t)
	project, _ := s.server.workV2Project(t.Context(), s.place)
	local, _, _ := s.server.CloudCredentials()
	read := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:7757"+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		return w
	}
	url := "/v1/projects/" + s.place + "/graphify"
	if w := read(url, ""); w.Code == 200 {
		t.Fatal("anonymous graph read accepted")
	}
	if w := read(url, local); w.Code != 403 || !strings.Contains(w.Body.String(), "graphify_disabled") {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if _, err := s.server.settingsFile().Set(map[string]any{"graphify_enabled": true}); err != nil {
		t.Fatal(err)
	}
	if w := read(url, local); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"missing"`) {
		t.Fatalf("missing: %d %s", w.Code, w.Body)
	}
	if err := os.Mkdir(filepath.Join(project.Path, "graphify-out"), 0700); err != nil {
		t.Fatal(err)
	}
	text := "package demo"
	sum := md5.Sum([]byte(text))
	manifest, _ := json.Marshal(map[string]any{"source.go": map[string]string{"ast_hash": hex.EncodeToString(sum[:])}})
	for name, body := range map[string]string{"source.go": text, "graphify-out/.graphify_root": project.Path, "graphify-out/manifest.json": string(manifest), "graphify-out/graph.json": `{"graph":{"schema_version":1},"nodes":[{"id":"one","label":"One"}],"links":[]}`} {
		if err := os.WriteFile(filepath.Join(project.Path, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := read(url, local)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"source_matched"`) || !strings.Contains(w.Body.String(), `"label":"One"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("view: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7757"+url, strings.NewReader("chunked body"))
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer "+local)
	rejected := httptest.NewRecorder()
	s.handler.ServeHTTP(rejected, r)
	if rejected.Code != 400 {
		t.Fatalf("chunked body accepted: %d %s", rejected.Code, rejected.Body)
	}
	if w := read(url+"?path=/outside", local); w.Code != 400 {
		t.Fatalf("query: %d %s", w.Code, w.Body)
	}
	if w := read("/v1/projects/missing/graphify", local); w.Code != 404 {
		t.Fatalf("unknown: %d", w.Code)
	}
	if _, err := s.server.settingsFile().Set(map[string]any{"graphify_enabled": false}); err != nil {
		t.Fatal(err)
	}
	if w := read(url, local); w.Code != 403 || strings.Contains(w.Body.String(), `"nodes"`) {
		t.Fatalf("off leaked graph: %d %s", w.Code, w.Body)
	}
}

func TestGraphifySessionViewUsesOnlyTheSelectedSessionDirectory(t *testing.T) {
	s := projectCloudStandIn(t)
	root := t.TempDir()
	s.server.inventory = app.Inventory{Process: fixedInventory{session.Inventory{Sessions: []session.Session{{ID: "%19", Assistant: session.AssistantCodex, CWD: root}}, Complete: true, Provenance: "ps", Sources: map[string]bool{"ps": true}}}}
	if _, err := s.server.settingsFile().Set(map[string]any{"graphify_enabled": true}); err != nil {
		t.Fatal(err)
	}
	local, _, _ := s.server.CloudCredentials()
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/graphify", 200},
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/graphify?path=/elsewhere", 400},
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/graphify/more", 501},
		{"POST", "/v1/sessions/" + url.PathEscape("%19") + "/graphify", 405},
	} {
		r := httptest.NewRequest(test.method, "http://127.0.0.1:7757"+test.path, nil)
		r.Header.Set("Authorization", "Bearer "+local)
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, w.Code, w.Body)
		}
		if test.status == 200 && !strings.Contains(w.Body.String(), `"state":"missing"`) {
			t.Fatalf("wrong directory: %s", w.Body)
		}
	}
}
