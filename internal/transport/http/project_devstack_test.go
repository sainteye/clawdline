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
