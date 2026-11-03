package http

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/graphify"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		{"POST", "/v1/sessions/" + url.PathEscape("%19") + "/graphify", 400},
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

func TestGraphifyBuildRequiresStrictExplicitMutationAndPublishesAsync(t *testing.T) {
	s := projectCloudStandIn(t)
	project, _ := s.server.workV2Project(t.Context(), s.place)
	if err := os.WriteFile(filepath.Join(project.Path, "main.go"), []byte("package main\nfunc main(){}"), 0600); err != nil {
		t.Fatal(err)
	}
	token, _, _ := s.server.CloudCredentials()
	path := "/v1/projects/" + s.place + "/graphify"
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	s.server.graphifyBuilder = graphify.NewBuilder(t.TempDir(), s.server.graphifyEnabled, func(ctx context.Context, input, output string, p func(string)) error {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		return os.WriteFile(filepath.Join(output, "graph.json"), []byte(`{"graph":{"schema_version":1},"nodes":[{"id":"main","source_file":"main.go"}],"links":[]}`), 0600)
	})
	post := func(path, body, key, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:7757"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		return w
	}
	if w := post(path, "{}", "off", token); w.Code != 403 {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if _, err := s.server.settingsFile().Set(map[string]any{"graphify_enabled": true}); err != nil {
		t.Fatal(err)
	}
	if w := post(path, "{}", "anonymous", ""); w.Code == 202 {
		t.Fatal("anonymous mutation accepted")
	}
	for _, body := range []string{"", `null`, `[]`, `{"path":"/outside"}`, `{"backend":"openai"}`, `{} {}`, strings.Repeat(" ", graphify.BuildBodyBytesLimit+1)} {
		if w := post(path, body, "bad", token); w.Code != 400 {
			t.Fatalf("bad body accepted: %d", w.Code)
		}
	}
	if w := post(path+"?path=/outside", "{}", "query", token); w.Code != 400 {
		t.Fatal("query accepted")
	}
	if w := post("/v1/projects/unknown/graphify", "{}", "unknown", token); w.Code != 404 {
		t.Fatal("unknown Project accepted")
	}
	if w := post(path, "{}", "", token); w.Code != 400 {
		t.Fatal("missing request key accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("validation executed parser")
	}
	w := post(path, "{}", "one", token)
	if w.Code != 202 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	<-started
	for _, key := range []string{"one", "two"} {
		if w = post(path, "{}", key, token); w.Code != 202 {
			t.Fatalf("dedup: %d %s", w.Code, w.Body)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate parser")
	}
	close(release)
	deadline := time.After(5 * time.Second)
	for s.server.graphifyBuilds().Status(project.Path).State == "running" {
		select {
		case <-deadline:
			t.Fatal("not completed")
		case <-time.After(time.Millisecond):
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7757"+path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"source_matched"`) || !strings.Contains(w.Body.String(), `"state":"succeeded"`) {
		t.Fatalf("completion: %d %s", w.Code, w.Body)
	}
	// The durable acceptance receipt remains usable after the in-memory builder
	// is replaced, without executing a second parser after a daemon restart.
	s.server.graphifyBuilder = graphify.NewBuilder(t.TempDir(), nil, func(context.Context, string, string, func(string)) error { calls.Add(1); return nil })
	if w = post(path, "{}", "one", token); w.Code != 202 {
		t.Fatalf("receipt lost: %d %s", w.Code, w.Body)
	}
	if calls.Load() != 1 {
		t.Fatal("replayed mutation after restart")
	}
}
