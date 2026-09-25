package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const itemRun = "0f0f0f0f-0000-4000-8000-000000000001"

const createdItem = `{"ok":true,"item":{"id":"item-1","title":"Ship it","kind":"feature","phase":"assigned",
 "owner_session":"` + thinConversation + `","version":2,"steps":[
 {"id":"s1","title":"draft","done":false},{"id":"s2","title":"check","done":false},{"id":"s3","title":"publish","done":false}]}}`

// `item add` reads this conversation's latest run, then posts the item with
// its steps in the order given, under a key it prints before asking; the
// created steps are printed with their ids, and nothing is written as a
// Session to-do.
func TestItemAddPostsThreeStepsInOrderUnderTheLatestRun(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"ok":true,"run":{"id":"` + itemRun + `","session_id":"` + thinConversation + `"}}`
		}
		return 201, createdItem
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "feature", title: "Ship it",
		description: "The person asked.", steps: []string{"draft", " ", "check", "publish"}}, nil, "", "",
		envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 {
		t.Fatalf("asked %d times", len(seen))
	}
	if seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/orchestrator/sessions/"+thinConversation+"/run" {
		t.Fatalf("run read = %+v", seen[0])
	}
	r := seen[1]
	if r.Method != "POST" || r.EscapedPath != "/v1/work/v2/agent/items" || !strings.HasPrefix(r.Key, "item-") ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+r.Key) {
		t.Fatalf("create = %+v, stderr %q", r, errs.String())
	}
	for _, other := range seen {
		if strings.Contains(other.EscapedPath, "session-todos") {
			t.Fatalf("item add wrote a Session to-do: %+v", other)
		}
	}
	var body struct {
		SessionID string            `json:"session_id"`
		Via       map[string]string `json:"via"`
		ProjectID string            `json:"project_id"`
		Kind      string            `json:"kind"`
		Steps     []string          `json:"steps"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil || body.SessionID != thinConversation ||
		body.Via["run"] != itemRun || body.ProjectID != "p1" || body.Kind != "feature" ||
		strings.Join(body.Steps, "|") != "draft|check|publish" {
		t.Fatalf("body = %s", r.Body)
	}
	for _, want := range []string{"item-1  Ship it  [feature, assigned, assigned to " + thinConversation + "]",
		"[ ] s1  draft", "[ ] s2  check", "[ ] s3  publish"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, out.String())
		}
	}
}

// --run and --key are used as given, and no run is read.
func TestItemAddUsesTheNamedRunAndKey(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "x",
		description: "d", run: itemRun}, nil, thinConversation, "item-retry", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Key != "item-retry" || !strings.Contains(string(seen[0].Body), itemRun) ||
		strings.Contains(string(seen[0].Body), `"steps"`) {
		t.Fatalf("requests = %+v", seen)
	}
}

// With no run for this conversation — the person typed in the terminal —
// nothing is created and the proposal path is named.
func TestItemAddWithoutARunCreatesNothing(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 404, `{"error":"no_run","detail":"Nobody has sent this session a message through Clawdline."}`
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "x", description: "d"},
		nil, thinConversation, "", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "refused, 404 no_run:") ||
		!strings.Contains(errs.String(), "Agent proposals") {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if len(s.requests()) != 1 || out.Len() != 0 {
		t.Fatalf("asked %d times; stdout %q", len(s.requests()), out.String())
	}
}

// The route's refusal is said with its status and code, and exits 1.
func TestItemAddSaysTheRefusal(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 429, `{"error":"run_items_exhausted","detail":"That message has already created 5 items."}`
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "x", description: "d",
		run: itemRun}, nil, thinConversation, "", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "refused, 429 run_items_exhausted: That message") {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
}

// `step-done` reads the item for its version and completes the step on the
// Agent route; `steps` lists them.
func TestItemStepsAndStepDoneUseTheItemsVersion(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	env := envOf(map[string]string{"CODEX_THREAD_ID": thinConversation})
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "steps", itemFlags{}, []string{"item-1"}, "", "", env); code != 0 {
		t.Fatalf("steps exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "[ ] s2  check") {
		t.Fatalf("steps stdout: %s", out.String())
	}
	if code := sessionItem(&out, &errs, b, "step-done", itemFlags{}, []string{"item-1", "s2"}, "", "", env); code != 0 {
		t.Fatalf("step-done exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 3 || seen[1].Method != "GET" || seen[1].EscapedPath != "/v1/work/v2/items/item-1" {
		t.Fatalf("requests = %+v", seen)
	}
	done := seen[2]
	if done.Method != "POST" || done.EscapedPath != "/v1/work/v2/agent/items/item-1/steps/s2/complete" || done.Key == "" {
		t.Fatalf("complete = %+v", done)
	}
	var body map[string]any
	if err := json.Unmarshal(done.Body, &body); err != nil || body["expected_version"] != float64(2) ||
		body["session_id"] != thinConversation {
		t.Fatalf("complete body = %s", done.Body)
	}
}

// Without a conversation nothing is asked.
func TestItemAddWithoutAConversationAsksNothing(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p", kind: "issue", title: "x"}, nil, "", "",
		envOf(nil)); code != 2 || len(s.requests()) != 0 {
		t.Fatalf("exit %d, asked %d times", code, len(s.requests()))
	}
}
