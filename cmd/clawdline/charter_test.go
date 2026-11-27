package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// fakeCharterDaemon answers the workflow route as the daemon does: GET
// answers the charter, PUT validates the graph and saves the next version.
type fakeCharterDaemon struct {
	dir      string
	version  int64
	workflow *contract.ProjectAssistantCharterWorkflow
	put      *contract.ProjectAssistantCharterWorkflowSaveRequest
	query    url.Values
}

func (f *fakeCharterDaemon) request(method, path string, query url.Values, body any, key string) (answer, error) {
	reply := func(status int, v any) (answer, error) {
		data, _ := json.Marshal(v)
		return answer{Status: status, Body: data}, nil
	}
	if path == "/v1/places" {
		return reply(200, contract.StartPlaceList{Places: []contract.StartPlace{{ID: "place-1", Path: f.dir}}})
	}
	if path != charterWorkflowPath {
		return reply(404, contract.Refusal{Error: "not_found"})
	}
	charter := func() contract.ProjectAssistantCharterResponse {
		return contract.ProjectAssistantCharterResponse{MachineID: "this-machine", ProjectID: "place-1", Version: f.version, Workflow: f.workflow}
	}
	if method == http.MethodGet {
		f.query = query
		return reply(200, charter())
	}
	raw, _ := json.Marshal(body)
	var put contract.ProjectAssistantCharterWorkflowSaveRequest
	_ = json.Unmarshal(raw, &put)
	f.put = &put
	if put.IdempotencyKey != key || put.ExpectedVersion != f.version || put.MachineID != "this-machine" {
		return reply(409, contract.ProjectAssistantErrorResponse{Error: contract.ProjectAssistantFailure{Code: contract.ProjectAssistantErrorCode(pa.StaleVersion), Field: "charter_version"}})
	}
	flow := pa.CharterWorkflow{}
	for _, s := range put.Workflow.States {
		flow.States = append(flow.States, pa.WorkflowState{ID: s.ID, Title: s.Title, BotID: s.BotID, Kind: pa.WorkflowStateKind(s.Kind), Note: s.Note})
	}
	for _, t := range put.Workflow.Transitions {
		flow.Transitions = append(flow.Transitions, pa.WorkflowTransition{From: t.From, To: t.To, Label: t.Label, Kind: pa.WorkflowTransitionKind(t.Kind)})
	}
	if err := pa.ValidateCharterWorkflow(flow, nil); err != nil {
		failure := err.(*pa.Failure)
		return reply(400, contract.ProjectAssistantErrorResponse{Error: contract.ProjectAssistantFailure{Code: contract.ProjectAssistantErrorCode(failure.Code), Field: failure.Field}})
	}
	f.version++
	f.workflow = &put.Workflow
	return reply(200, charter())
}

func runCharterFake(t *testing.T, f *fakeCharterDaemon, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCharter(args, strings.NewReader(stdin), &stdout, &stderr, func() (memoryDaemon, error) { return f, nil })
	return code, stdout.String(), stderr.String()
}

// A Session shows the workflow, writes one on the person's words against the
// version it read, round-trips show --json, and is told which state broke a
// rule.
func TestCharterWorkflowCommand(t *testing.T) {
	f := &fakeCharterDaemon{dir: t.TempDir(), version: 3}
	code, out, _ := runCharterFake(t, f, "", "workflow", "show", "--project", "place-1")
	if code != 0 || !strings.Contains(out, "Charter version 3 of this Project has no workflow yet.") || f.query.Get("project_id") != "place-1" || f.query.Has("machine_id") {
		t.Fatalf("show without a workflow: %d %q %v", code, out, f.query)
	}
	flow := `{"states":[{"id":"open","title":"Open","bot_id":"","kind":"start","note":""},{"id":"build","title":"Build","bot_id":"maker","kind":"step","note":""},{"id":"done","title":"Done","bot_id":"","kind":"done","note":""}],"transitions":[{"from":"open","to":"build","label":"","kind":"normal"},{"from":"build","to":"done","label":"built","kind":"pass"}]}`
	file := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(file, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCharterFake(t, f, "", "workflow", "set", "--file", file, "--reason", "The person asked for a build step.", "--key", "k1", "--project", "place-1")
	if code != 0 || !strings.Contains(out, "Saved the workflow as charter version 4.") || f.put == nil || f.put.Reason != "The person asked for a build step." || f.put.ExpectedVersion != 3 || !strings.Contains(errs, "Idempotency-Key: k1") {
		t.Fatalf("set: %d %q %q %+v", code, out, errs, f.put)
	}
	code, out, _ = runCharterFake(t, f, "", "workflow", "show", "--project", "place-1")
	if code != 0 || !strings.Contains(out, "Charter version 4") || !strings.Contains(out, "- Build（maker）：built → Done") {
		t.Fatalf("show renders the workflow: %q", out)
	}
	code, out, _ = runCharterFake(t, f, "", "workflow", "show", "--json", "--project", "place-1")
	var back contract.ProjectAssistantCharterWorkflow
	if code != 0 || json.Unmarshal([]byte(out), &back) != nil || len(back.States) != 3 {
		t.Fatalf("show --json prints the workflow set reads: %q", out)
	}
	// The same JSON read from stdin, with a state that cannot be reached.
	broken := strings.Replace(out, `"from": "open"`, `"from": "build"`, 1)
	code, _, errs = runCharterFake(t, f, broken, "workflow", "set", "--file", "-", "--reason", "Loop.", "--project", "place-1")
	if code != 1 || !strings.Contains(errs, "pa_invalid: workflow_unreachable:build") || !strings.Contains(errs, "State build cannot be reached from the start state.") {
		t.Fatalf("a refusal names the state: %d %q", code, errs)
	}
	// A directory names the Project registered at it.
	code, _, _ = runCharterFake(t, f, "", "workflow", "show", "--project", f.dir)
	if code != 0 || f.query.Get("project_id") != "place-1" {
		t.Fatalf("a directory resolves to its Project: %d %v", code, f.query)
	}
	for _, args := range [][]string{{"workflow", "set", "--file", file}, {"workflow", "show", "--reason", "x"}, {"workflow"}, {"rules", "show"}, {"workflow", "set", "--reason", "x"}} {
		if code, _, _ := runCharterFake(t, f, "", args...); code != 2 {
			t.Errorf("%v is a usage error: %d", args, code)
		}
	}
	if code, _, errs := runCharterFake(t, f, `{"states":[],"transitions":[],"extra":1}`, "workflow", "set", "--file", "-", "--reason", "x", "--project", "place-1"); code != 2 || !strings.Contains(errs, "one workflow JSON object") {
		t.Fatalf("an unknown field is refused before anything is sent: %d %q", code, errs)
	}
}
