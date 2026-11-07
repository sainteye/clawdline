package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// The guide sent agents to GET /v1/devstacks, which the CLI's credential could
// not call, so a Gemini Session reversed the adapter's source (2026-11-07).
// `project devstack` prints the Project's own stack and each Link address.
func TestProjectDevStackPrintsThisProjectsServersAndTheirLinks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stacks, _ := json.Marshal(map[string]any{"observedAt": 1, "stacks": []any{
		map[string]any{"name": "other", "root": "/elsewhere", "state": "running", "commands": []any{},
			"processes": []any{map[string]any{"name": "x", "port": 1, "state": "running"}}},
		map[string]any{"name": "myapp", "root": dir, "state": "partial", "commands": []any{},
			"processes": []any{
				map[string]any{"name": "api", "port": 8002, "state": "running"},
				map[string]any{"name": "tunnel", "port": 20241, "state": "stopped", "url": "https://myapp.example.com"},
			}},
	}})
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, string(stacks) })
	var out, errs bytes.Buffer
	if code := projectDevStack(&out, &errs, b, filepath.Join(dir, "sub", ".."), false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || seen[0].Method != http.MethodGet || seen[0].EscapedPath != "/v1/devstacks" {
		t.Fatalf("requests = %+v", seen)
	}
	got := out.String()
	for _, want := range []string{"myapp  partial  " + dir, "http://127.0.0.1:8002", "https://myapp.example.com", "stopped"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "other") {
		t.Fatalf("another Project's stack was printed:\n%s", got)
	}

	// No stack for the directory is a refusal that says where Clawdline looks.
	out.Reset()
	errs.Reset()
	if code := projectDevStack(&out, &errs, b, t.TempDir(), false); code != 1 || !strings.Contains(errs.String(), ".devstack.json") {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
}

// `item propose` files the proposal the no_run refusal asks for, with its
// source, and defaults the Project to the command's directory.
func TestItemProposeFilesAProposalWithItsSource(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 201, `{"ok":true,"proposal":{"id":"p-1","title":"Ship it"}}`
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "propose", itemFlags{kind: "issue", title: "Ship it", description: "d",
		reason: "worth it", acceptance: "visible", sourceTodo: "todo-1"}, nil, thinConversation, "item-propose", envOf(nil))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].EscapedPath != "/v1/work/v2/agent/proposals" {
		t.Fatalf("requests = %+v", seen)
	}
	var body map[string]any
	if err := json.Unmarshal(seen[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["source_todo_id"] != "todo-1" || body["reason"] != "worth it" || body["suggested_acceptance"] != "visible" ||
		body["session_id"] != thinConversation || !filepath.IsAbs(body["project_id"].(string)) {
		t.Fatalf("body = %s", seen[0].Body)
	}
	if !strings.Contains(out.String(), "p-1") {
		t.Fatalf("stdout = %s", out.String())
	}
}
