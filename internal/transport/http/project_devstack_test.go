package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestProjectDevStackRoute(t *testing.T) {
	s := projectCloudStandIn(t)
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("fixture place")
	}
	local, _, err := s.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, url, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:7757"+url, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		return out
	}

	base := "/v1/projects/" + s.place + "/devstack"

	// Missing .devstack.json
	res := request("GET", base, "", local)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var reply ProjectDevStackReply
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.DevStack != contract.ProjectServerSetupMissing {
		t.Errorf("expected missing devstack, got %q", reply.DevStack)
	}
	if reply.ServerCount != 0 {
		t.Errorf("expected 0 servers, got %d", reply.ServerCount)
	}
	if reply.Raw != "" {
		t.Errorf("expected empty raw, got %q", reply.Raw)
	}

	// Create valid .devstack.json
	devstackJSON := `{"version":1,"name":"fixture-app","processes":[{"name":"web","port":3000,"url":"http://127.0.0.1:3000/"},{"name":"api","port":8080}]}`
	devstackFile := filepath.Join(project.Path, ".devstack.json")
	if err := os.WriteFile(devstackFile, []byte(devstackJSON), 0644); err != nil {
		t.Fatal(err)
	}

	res = request("GET", base, "", local)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	reply = ProjectDevStackReply{}
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.DevStack != contract.ProjectServerSetupReady {
		t.Errorf("expected ready devstack, got %q", reply.DevStack)
	}
	if reply.ServerCount != 2 {
		t.Errorf("expected 2 servers, got %d", reply.ServerCount)
	}
	if reply.Raw != devstackJSON {
		t.Errorf("expected raw json matching, got %q", reply.Raw)
	}
	if reply.Stack == nil || reply.Stack.Name != "fixture-app" {
		t.Errorf("expected stack name fixture-app, got %+v", reply.Stack)
	}
	if len(reply.Links) < 2 {
		t.Errorf("expected at least 2 links from devstack, got %d", len(reply.Links))
	}

	// Broken / unreadable .devstack.json
	brokenJSON := `{broken json`
	if err := os.WriteFile(devstackFile, []byte(brokenJSON), 0644); err != nil {
		t.Fatal(err)
	}
	res = request("GET", base, "", local)
	if res.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	reply = ProjectDevStackReply{}
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.DevStack != contract.ProjectServerSetupAttention {
		t.Errorf("expected attention devstack, got %q", reply.DevStack)
	}
	if reply.Raw != brokenJSON {
		t.Errorf("expected broken json in raw, got %q", reply.Raw)
	}

	// Method not allowed
	if bad := request("POST", base, "{}", local); bad.Code != 405 {
		t.Errorf("expected 405, got %d", bad.Code)
	}

	// Unknown project
	if unknown := request("GET", "/v1/projects/not-a-real-place/devstack", "", local); unknown.Code != 404 {
		t.Errorf("expected 404, got %d", unknown.Code)
	}
}

// The hosted console asks the bridge by Project ID, including on read-only
// pairings. Exercise the real HTTP route rather than a canned bridge reply.
func TestProjectDevStackCloudRead(t *testing.T) {
	s := projectCloudStandIn(t)
	s.bridge.AllowCommands = func() bool { return false }
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("fixture place")
	}
	spec := `{"version":1,"name":"fixture-app","processes":[{"name":"web","port":4173}],"up":"touch must-not-run"}`
	if err := os.WriteFile(filepath.Join(project.Path, ".devstack.json"), []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"type": "project-devstack", "project": s.place, "request": "devstack-1", "session": "__clawdline_machine__"}
	answer, payload := s.ask(t, 1, input)
	if answer.Status != 200 {
		t.Fatalf("Cloud devstack: %+v %v", answer, payload)
	}
	body := bodyOf(t, payload)
	if body["devStack"] != "ready" || body["serverCount"] != float64(1) || body["raw"] != spec || body["root"] != project.Path {
		t.Fatalf("devstack reply: %v", body)
	}
	if stack, ok := body["stack"].(map[string]any); !ok || stack["name"] != "fixture-app" {
		t.Fatalf("missing stack: %v", body)
	}
	if links, ok := body["links"].([]any); !ok || len(links) == 0 {
		t.Fatalf("missing links: %v", body)
	}
	if _, err := os.Stat(filepath.Join(project.Path, "must-not-run")); !os.IsNotExist(err) {
		t.Fatalf("read executed the up command: %v", err)
	}
	input["project"] = "not-a-real-place"
	if answer, _ := s.ask(t, 2, input); answer.Status != 404 || answer.Code != "project_not_found" {
		t.Fatalf("unknown Project: %+v", answer)
	}
	input["path"] = "/outside"
	if answer, _ := s.ask(t, 3, input); answer.Code != "malformed_read" {
		t.Fatalf("browser-supplied path: %+v", answer)
	}
}
