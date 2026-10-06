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

func TestProjectUnifyRoutePlansAndAppliesOnce(t *testing.T) {
	s := iconCloudStandIn(t)
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("fixture place")
	}
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(project.Path, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("AGENTS.md", "shared\n")
	write("CLAUDE.md", "claude\n")
	write(".agents/skills/s/SKILL.md", "skill")
	local, _, err := s.server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, url, body, key, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:7757"+url, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		s.handler.ServeHTTP(out, req)
		return out
	}
	base := "/v1/projects/" + s.place + "/unify"
	if out := request("GET", base, "", "", ""); out.Code == 200 {
		t.Fatal("unauthorized plan read accepted")
	}
	read := request("GET", base, "", "", local)
	var plan contract.ProjectUnifyPlan
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &plan) != nil || plan.Status != "drifting" || len(plan.Actions) != 2 {
		t.Fatalf("plan: %d %s", read.Code, read.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(project.Path, "CLAUDE.md")); string(got) != "claude\n" {
		t.Fatalf("reading the plan wrote CLAUDE.md: %q", got)
	}
	body := `{"version":"` + plan.Version + `"}`
	if out := request("POST", base, body, "", local); out.Code != 400 {
		t.Fatalf("no key: %d", out.Code)
	}
	if out := request("POST", base, body, "k1", ""); out.Code == 200 {
		t.Fatal("unauthorized apply accepted")
	}
	stale := request("POST", base, `{"version":"old"}`, "k0", local)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "plan_changed") {
		t.Fatalf("stale version: %d %s", stale.Code, stale.Body.String())
	}
	if out := request("POST", base, `{"version":"x","extra":1}`, "k2", local); out.Code != 400 {
		t.Fatalf("unknown field: %d", out.Code)
	}
	applied := request("POST", base, body, "k1", local)
	var out contract.ProjectUnifyApplied
	if applied.Code != 200 || json.Unmarshal(applied.Body.Bytes(), &out) != nil || out.Outcome != "applied" ||
		len(out.Ran) != 2 || out.Plan.Status != "unified" {
		t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
	}
	if target, err := os.Readlink(filepath.Join(project.Path, ".claude/skills/s")); err != nil || target != filepath.FromSlash("../../.agents/skills/s") {
		t.Fatalf("link %q %v", target, err)
	}
	replay := request("POST", base, body, "k1", local)
	if replay.Code != 200 || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %v", replay.Code, replay.Header())
	}
	again := request("POST", base, body, "k3", local)
	if again.Code != 409 || !strings.Contains(again.Body.String(), "plan_changed") {
		t.Fatalf("the old version under a new key: %d %s", again.Code, again.Body.String())
	}
	if out := request("GET", "/v1/projects/not-a-place/unify", "", "", local); out.Code != 404 {
		t.Fatalf("unknown project: %d", out.Code)
	}
	if out := request("PUT", base, body, "k4", local); out.Code != 405 {
		t.Fatalf("PUT: %d", out.Code)
	}
}
