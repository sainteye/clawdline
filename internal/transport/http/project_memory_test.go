package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/contract"
)

// A session's orchestrator credential adds, lists, shows, updates and
// forgets; each refusal has its own code; a retry of the same add is
// `unchanged`; nothing reaches the store without a credential.
func TestProjectMemoryRoutesKeepOneStorePerProject(t *testing.T) {
	s := iconCloudStandIn(t)
	request := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:7757"+path, strings.NewReader(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		return out
	}
	session := map[string]string{"X-Clawdline-Orchestrator": s.machine}
	base := "/v1/projects/" + s.place + "/memory"
	entry := `{"name":"build-through-heavy","description":"compiles go through tools/heavy.sh","type":"feedback","body":"Invented body.\n"}`

	if out := request("GET", base, "", nil); out.Code == 200 {
		t.Fatal("a read without a credential was answered")
	}
	if out := request("POST", base, entry, nil); out.Code < 400 {
		t.Fatalf("a write without a credential was answered %d", out.Code)
	}
	empty := request("GET", base, "", session)
	var list contract.ProjectMemoryList
	if empty.Code != 200 || json.Unmarshal(empty.Body.Bytes(), &list) != nil || len(list.Entries) != 0 || list.Index != "" {
		t.Fatalf("empty list: %d %s", empty.Code, empty.Body.String())
	}
	if out := request("POST", base, entry, session); out.Code != 201 || !strings.Contains(out.Body.String(), `"created"`) {
		t.Fatalf("add: %d %s", out.Code, out.Body.String())
	}
	if out := request("POST", base, entry, session); out.Code != 200 || !strings.Contains(out.Body.String(), `"unchanged"`) {
		t.Fatalf("the same add again: %d %s", out.Code, out.Body.String())
	}
	changed := strings.Replace(entry, "Invented body.", "Another body.", 1)
	if out := request("POST", base, changed, session); out.Code != 409 || !strings.Contains(out.Body.String(), "memory_entry_exists") {
		t.Fatalf("a different add under the same name: %d %s", out.Code, out.Body.String())
	}
	if out := request("POST", base, strings.Replace(entry, "build-through-heavy", "Bad Name", 1), session); out.Code != 400 ||
		!strings.Contains(out.Body.String(), "memory_entry_invalid") {
		t.Fatalf("a bad name: %d %s", out.Code, out.Body.String())
	}
	if out := request("POST", base, `{"name":"x","extra":1}`, session); out.Code != 400 {
		t.Fatalf("an unknown field: %d", out.Code)
	}
	if out := request("POST", base, strings.Repeat("x", memoryRequestBodyLimit+1), session); out.Code != 413 {
		t.Fatalf("an oversize body: %d", out.Code)
	}
	listed := request("GET", base, "", session)
	if json.Unmarshal(listed.Body.Bytes(), &list) != nil || len(list.Entries) != 1 ||
		!strings.Contains(list.Index, "- build-through-heavy — compiles go through tools/heavy.sh") || list.IndexCut {
		t.Fatalf("list: %s", listed.Body.String())
	}
	project, _ := s.server.workV2Project(t.Context(), s.place)
	if key, _ := memory.KeyFor(project.Path); list.ProjectKey != key {
		t.Fatalf("project key %q, want %q", list.ProjectKey, key)
	}
	one := base + "/build-through-heavy"
	var shown contract.ProjectMemoryEntry
	if out := request("GET", one, "", session); out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &shown) != nil ||
		shown.Body != "Invented body.\n" {
		t.Fatalf("show: %d %s", out.Code, out.Body.String())
	}
	if out := request("PUT", one, changed, session); out.Code != 200 || !strings.Contains(out.Body.String(), `"updated"`) {
		t.Fatalf("update: %d %s", out.Code, out.Body.String())
	}
	if out := request("PUT", base+"/another-name", changed, session); out.Code != 400 {
		t.Fatalf("a path and body that disagree: %d", out.Code)
	}
	if out := request("GET", base+"/never-written", "", session); out.Code != 404 ||
		!strings.Contains(out.Body.String(), "memory_entry_not_found") {
		t.Fatalf("show unknown: %d %s", out.Code, out.Body.String())
	}
	if out := request("DELETE", one, "", session); out.Code != 200 || !strings.Contains(out.Body.String(), `"forgotten"`) {
		t.Fatalf("forget: %d %s", out.Code, out.Body.String())
	}
	if out := request("DELETE", one, "", session); out.Code != 404 {
		t.Fatalf("forget twice: %d", out.Code)
	}
	if out := request("GET", "/v1/projects/no-such-place/memory", "", session); out.Code != 404 ||
		!strings.Contains(out.Body.String(), "project_not_found") {
		t.Fatalf("unknown place: %d %s", out.Code, out.Body.String())
	}
}

// A store that cannot be read is 500 memory_unreadable, never an empty list:
// unknown is not zero.
func TestProjectMemoryThatCannotBeReadIsNotAnEmptyList(t *testing.T) {
	s := iconCloudStandIn(t)
	project, _ := s.server.workV2Project(t.Context(), s.place)
	key, _ := memory.KeyFor(project.Path)
	dir := filepath.Join(s.server.cfg.Dir, "memory", key)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	// A file where the directory should be: reading it fails, as a directory
	// this daemon may not open does.
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:7757/v1/projects/"+s.place+"/memory", nil)
	req.Header.Set("X-Clawdline-Orchestrator", s.machine)
	out := httptest.NewRecorder()
	s.handler.ServeHTTP(out, req)
	if out.Code != 500 || !strings.Contains(out.Body.String(), "memory_unreadable") {
		t.Fatalf("unreadable store: %d %s", out.Code, out.Body.String())
	}
	if got := s.server.memoryLaunchText(project.Path); got != "" {
		t.Fatalf("a launch was handed %q from a store that could not be read", got)
	}
}
