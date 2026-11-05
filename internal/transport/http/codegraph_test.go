package http

import (
	"context"
	"database/sql"
	"github.com/sainteye/clawdline/internal/adapters/codegraph"
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

func TestCodeGraphProjectReadCrossesAuthenticationAndMachineOptIn(t *testing.T) {
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
	url := "/v1/projects/" + s.place + "/codegraph"
	if w := read(url, ""); w.Code == 200 {
		t.Fatal("anonymous graph read accepted")
	}
	if w := read(url, local); w.Code != 403 || !strings.Contains(w.Body.String(), "codegraph_disabled") {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if _, err := s.server.settingsFile().Set(map[string]any{"codegraph_enabled": true}); err != nil {
		t.Fatal(err)
	}
	if w := read(url, local); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"missing"`) {
		t.Fatalf("missing: %d %s", w.Code, w.Body)
	}
	if err := os.WriteFile(filepath.Join(project.Path, "main.go"), []byte("package demo"), 0600); err != nil {
		t.Fatal(err)
	}
	s.server.codegraphBuilder = codegraph.NewBuilder(t.TempDir(), s.server.codegraphEnabled, httpCodeGraphEngine(t))
	t.Cleanup(func() { _ = s.server.StopCodeGraph(context.Background()) })
	s.server.codegraphBuilder.Start(project.Path, "fixture")
	for s.server.codegraphBuilder.Status(project.Path).State == "running" {
		time.Sleep(time.Millisecond)
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
	if w := read("/v1/projects/missing/codegraph", local); w.Code != 404 {
		t.Fatalf("unknown: %d", w.Code)
	}
	if _, err := s.server.settingsFile().Set(map[string]any{"codegraph_enabled": false}); err != nil {
		t.Fatal(err)
	}
	if w := read(url, local); w.Code != 403 || strings.Contains(w.Body.String(), `"nodes"`) {
		t.Fatalf("off leaked graph: %d %s", w.Code, w.Body)
	}
}

func TestCodeGraphSessionViewUsesOnlyTheSelectedSessionDirectory(t *testing.T) {
	s := projectCloudStandIn(t)
	root := t.TempDir()
	s.server.inventory = app.Inventory{Process: fixedInventory{session.Inventory{Sessions: []session.Session{{ID: "%19", Assistant: session.AssistantCodex, CWD: root}}, Complete: true, Provenance: "ps", Sources: map[string]bool{"ps": true}}}}
	if _, err := s.server.settingsFile().Set(map[string]any{"codegraph_enabled": true}); err != nil {
		t.Fatal(err)
	}
	local, _, _ := s.server.CloudCredentials()
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/codegraph", 200},
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/codegraph?path=/elsewhere", 400},
		{"GET", "/v1/sessions/" + url.PathEscape("%19") + "/codegraph/more", 501},
		{"POST", "/v1/sessions/" + url.PathEscape("%19") + "/codegraph", 400},
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

func TestCodeGraphBuildRequiresStrictExplicitMutationAndPublishesAsync(t *testing.T) {
	s := projectCloudStandIn(t)
	project, _ := s.server.workV2Project(t.Context(), s.place)
	if err := os.WriteFile(filepath.Join(project.Path, "main.go"), []byte("package main\nfunc main(){}"), 0600); err != nil {
		t.Fatal(err)
	}
	token, _, _ := s.server.CloudCredentials()
	path := "/v1/projects/" + s.place + "/codegraph"
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	s.server.codegraphBuilder = codegraph.NewBuilder(t.TempDir(), s.server.codegraphEnabled, func(ctx context.Context, input, output string, p func(string)) error {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		return httpCodeGraphEngine(t)(ctx, input, output, p)
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
	if _, err := s.server.settingsFile().Set(map[string]any{"codegraph_enabled": true}); err != nil {
		t.Fatal(err)
	}
	if w := post(path, "{}", "anonymous", ""); w.Code == 202 {
		t.Fatal("anonymous mutation accepted")
	}
	for _, body := range []string{"", `null`, `[]`, `{"path":"/outside"}`, `{"backend":"openai"}`, `{} {}`, strings.Repeat(" ", codegraph.BuildBodyBytesLimit+1)} {
		if w := post(path, body, "bad", token); w.Code != 400 {
			t.Fatalf("bad body accepted: %d", w.Code)
		}
	}
	if w := post(path+"?path=/outside", "{}", "query", token); w.Code != 400 {
		t.Fatal("query accepted")
	}
	if w := post("/v1/projects/unknown/codegraph", "{}", "unknown", token); w.Code != 404 {
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
	for s.server.codegraphBuilds().Status(project.Path).State == "running" {
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
	_ = s.server.StopCodeGraph(context.Background())
	// The durable acceptance receipt remains usable after the in-memory builder
	// is replaced, without executing a second parser after a daemon restart.
	s.server.codegraphBuilder = codegraph.NewBuilder(t.TempDir(), nil, func(context.Context, string, string, func(string)) error { calls.Add(1); return nil })
	if w = post(path, "{}", "one", token); w.Code != 202 {
		t.Fatalf("receipt lost: %d %s", w.Code, w.Body)
	}
	if calls.Load() != 1 {
		t.Fatal("replayed mutation after restart")
	}
}

func httpCodeGraphEngine(t *testing.T) codegraph.Engine {
	return func(ctx context.Context, input, output string, _ func(string)) error {
		directory := filepath.Join(output, "native")
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		db, err := sql.Open("sqlite", filepath.Join(directory, "codegraph.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.ExecContext(ctx, `CREATE TABLE project_metadata(key TEXT PRIMARY KEY,value TEXT);INSERT INTO project_metadata VALUES('index_state','complete'),('indexed_with_version','1.6.2');CREATE TABLE files(path TEXT,content_hash TEXT);CREATE TABLE nodes(id TEXT,name TEXT,file_path TEXT,start_line INTEGER);CREATE TABLE edges(id TEXT,source TEXT,target TEXT,kind TEXT,provenance TEXT);INSERT INTO nodes VALUES('one','One','main.go',1);`)
		return err
	}
}

func TestCodeGraphQueryIsLocalOnlyAndRefusesUnknownOrOversizedInput(t *testing.T) {
	s := newLocalAuthorityStandIn(t)
	s.server.settingsFile().Set(map[string]any{"codegraph_enabled": true})
	t.Cleanup(func() { _ = s.server.StopCodeGraph(context.Background()) })
	for _, tc := range []struct {
		token, actor, body string
		want               int
	}{
		{s.local, "", `{"directory":"/outside","query":"x"}`, 403},
		{s.lan, "", `{"directory":"/outside","query":"x"}`, 403},
		{s.machine, actorDevice, `{"directory":"/outside","query":"x"}`, 401},
		{s.machine, "", `{"directory":"relative","query":"x"}`, 400},
		{s.machine, "", `{"directory":"/outside","query":"x","model":"remote"}`, 400},
		{s.machine, "", `{"directory":"/outside","query":"x"}` + strings.Repeat(" ", codegraph.QueryBodyBytesLimit), 400},
		{s.machine, "", `{"directory":"/outside","query":"x"}`, 409},
	} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:7757/v1/orchestrator/codegraph/query", strings.NewReader(tc.body))
		if tc.token == s.machine {
			req.Header.Set(machineHeader, tc.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		if tc.actor != "" {
			req.Header.Set(actorHeader, tc.actor)
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("authority/input: got %d want %d: %s", w.Code, tc.want, w.Body)
		}
	}
}
