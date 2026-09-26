package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
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

// `phase` reads the item for its version and posts the next phase with only
// the evidence it was given, the landing as one object.
func TestItemPhasePostsTheNextPhaseWithItsEvidence(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})
	var out, errs bytes.Buffer
	f := itemFlags{phase: phaseEvidence{commit: "abc123", target: "main", remote: "origin", landingProject: "p2"}}
	if code := sessionItem(&out, &errs, b, "phase", f, []string{"item-1", "deploying"}, "", "", env); code != 0 {
		t.Fatalf("phase exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/work/v2/items/item-1" {
		t.Fatalf("requests = %+v", seen)
	}
	p := seen[1]
	if p.Method != "POST" || p.EscapedPath != "/v1/work/v2/agent/items/item-1/phase" || p.Key == "" ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+p.Key) {
		t.Fatalf("phase = %+v, stderr %q", p, errs.String())
	}
	var body map[string]any
	if err := json.Unmarshal(p.Body, &body); err != nil {
		t.Fatal(err)
	}
	landing, _ := body["landing"].(map[string]any)
	if body["expected_version"] != float64(2) || body["session_id"] != thinConversation || body["next"] != "deploying" ||
		landing["commit"] != "abc123" || landing["target"] != "main" || landing["remote"] != "origin" ||
		landing["project"] != "p2" || len(body) != 4 {
		t.Fatalf("phase body = %s", p.Body)
	}
}

// Half a landing is refused before anything is asked.
func TestItemPhaseRefusesHalfALanding(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "phase", itemFlags{phase: phaseEvidence{commit: "abc123"}},
		[]string{"item-1", "deploying"}, thinConversation, "", envOf(nil))
	if code != 2 || len(s.requests()) != 0 || !strings.Contains(errs.String(), "go together") {
		t.Fatalf("exit %d, asked %d times, stderr %q", code, len(s.requests()), errs.String())
	}
}

// Flags parse wherever they stand, as the guide writes them after the item
// id and phase; after "--" everything is positional.
func TestItemFlagsParseAfterThePositionalArguments(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		positional []string
		verify     string
	}{
		{[]string{"item-1", "merging", "--verification", "ran it"}, []string{"item-1", "merging"}, "ran it"},
		{[]string{"--verification", "ran it", "item-1", "merging"}, []string{"item-1", "merging"}, "ran it"},
		{[]string{"item-1", "--verification=ran it", "merging"}, []string{"item-1", "merging"}, "ran it"},
		{[]string{"item-1", "--", "--verification"}, []string{"item-1", "--verification"}, ""},
	} {
		fs := flag.NewFlagSet("item phase", flag.ContinueOnError)
		verify := fs.String("verification", "", "")
		got, err := parseInterspersed(fs, tc.args)
		if err != nil || strings.Join(got, "|") != strings.Join(tc.positional, "|") || *verify != tc.verify {
			t.Errorf("%q: positional %q, verification %q, err %v", tc.args, got, *verify, err)
		}
	}
}

// `step-add` posts one step per title, in the order given, each after a
// fresh read of the item: the version it sends is the one it just read, and
// each position lands after every step already there, so the daemon's
// position-then-id order keeps the given order. Each key is printed before
// its write, and the item is printed with all its steps afterwards.
func TestItemStepAddPostsEachTitleInOrderAfterTheExistingSteps(t *testing.T) {
	version, steps := 2, []string{`{"id":"s1","title":"draft","done":true,"position":0}`,
		`{"id":"s2","title":"check","done":false,"position":4}`}
	var posted []map[string]any
	var s *standIn
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost {
			seen := s.requests()
			var body map[string]any
			_ = json.Unmarshal(seen[len(seen)-1].Body, &body)
			posted = append(posted, body)
			version++
			n := len(steps) + 1
			steps = append(steps, fmt.Sprintf(`{"id":"s%d","title":%q,"done":false,"position":%d}`,
				n, body["title"], int64(body["position"].(float64))))
		}
		return 200, fmt.Sprintf(`{"ok":true,"item":{"id":"item-1","title":"Ship it","kind":"feature","phase":"implementing",`+
			`"owner_session":"%s","version":%d,"steps":[%s]}}`, thinConversation, version, strings.Join(steps, ","))
	})
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "step-add", itemFlags{}, []string{"item-1", "Wire the route", " ", "Guide it"},
		"", "", env)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	var methods []string
	for _, r := range seen {
		methods = append(methods, r.Method+" "+r.EscapedPath)
	}
	want := []string{"GET /v1/work/v2/items/item-1", "POST /v1/work/v2/agent/items/item-1/steps",
		"GET /v1/work/v2/items/item-1", "POST /v1/work/v2/agent/items/item-1/steps", "GET /v1/work/v2/items/item-1"}
	if strings.Join(methods, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s", strings.Join(methods, "\n"))
	}
	if len(posted) != 2 || posted[0]["title"] != "Wire the route" || posted[1]["title"] != "Guide it" ||
		posted[0]["expected_version"] != float64(2) || posted[1]["expected_version"] != float64(3) ||
		posted[0]["position"] != float64(5) || posted[1]["position"] != float64(6) ||
		posted[0]["session_id"] != thinConversation || posted[1]["session_id"] != thinConversation {
		t.Fatalf("posted = %v", posted)
	}
	if seen[1].Key == "" || seen[3].Key == "" || seen[1].Key == seen[3].Key ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+seen[1].Key) ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+seen[3].Key) {
		t.Fatalf("keys %q %q, stderr %q", seen[1].Key, seen[3].Key, errs.String())
	}
	for _, line := range []string{"[x] s1  draft", "[ ] s2  check", "[ ] s3  Wire the route", "[ ] s4  Guide it"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("stdout lacks %q:\n%s", line, out.String())
		}
	}
}

// With no title nothing is asked; a refusal stops the rest and names what
// was already added.
func TestItemStepAddStopsAtARefusal(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "step-add", itemFlags{}, []string{"item-1", " "}, thinConversation, "",
		envOf(nil)); code != 2 || len(s.requests()) != 0 {
		t.Fatalf("no titles: exit %d, asked %d times", code, len(s.requests()))
	}
	s, b = newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost {
			return 403, `{"error":"not_item_owner","detail":"Only the owning Session may add item steps."}`
		}
		return 200, createdItem
	})
	errs.Reset()
	code := sessionItem(&out, &errs, b, "step-add", itemFlags{}, []string{"item-1", "one", "two"}, thinConversation, "",
		envOf(nil))
	if code != 1 || len(s.requests()) != 2 || !strings.Contains(errs.String(), "refused, 403 not_item_owner") ||
		!strings.Contains(errs.String(), "No step was added") {
		t.Fatalf("exit %d, asked %d times, stderr %q", code, len(s.requests()), errs.String())
	}
}

// `item claim` reads this conversation's latest run and the item's version,
// then claims the item on the Agent route under a key it prints first; the
// body names only this conversation, never another Session or terminal.
func TestItemClaimPostsTheLatestRunAndTheItemsVersion(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/run") {
			return 200, `{"ok":true,"run":{"id":"` + itemRun + `","session_id":"` + thinConversation + `"}}`
		}
		return 200, createdItem
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "claim", itemFlags{}, []string{"item-1"}, "", "",
		envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 3 || seen[0].EscapedPath != "/v1/orchestrator/sessions/"+thinConversation+"/run" ||
		seen[1].Method != "GET" || seen[1].EscapedPath != "/v1/work/v2/items/item-1" {
		t.Fatalf("requests = %+v", seen)
	}
	r := seen[2]
	if r.Method != "POST" || r.EscapedPath != "/v1/work/v2/agent/items/item-1/claim" || !strings.HasPrefix(r.Key, "item-") ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+r.Key) {
		t.Fatalf("claim = %+v, stderr %q", r, errs.String())
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil || len(body) != 3 || body["session_id"] != thinConversation ||
		body["expected_version"] != float64(2) || body["via"].(map[string]any)["run"] != itemRun {
		t.Fatalf("body = %s", r.Body)
	}
	if !strings.Contains(out.String(), "item-1  Ship it  [feature, assigned, assigned to "+thinConversation+"]") {
		t.Fatalf("stdout = %s", out.String())
	}
}

// --run and --key are used as given; with no run at all nothing is claimed
// and the person's own assignment is named; a refusal is said by its code.
func TestItemClaimUsesTheNamedRunOrClaimsNothing(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "claim", itemFlags{run: itemRun}, []string{"item-1"}, thinConversation,
		"claim-retry", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 2 || seen[1].Key != "claim-retry" {
		t.Fatalf("requests = %+v", seen)
	}

	none, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 404, `{"error":"no_run","detail":"Nobody has sent this session a message through Clawdline."}`
	})
	out.Reset()
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "claim", itemFlags{}, []string{"item-1"}, thinConversation, "", envOf(nil)); code != 1 ||
		!strings.Contains(errs.String(), "Nothing was claimed") || len(none.requests()) != 1 {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}

	_, b = newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, createdItem
		}
		return 409, `{"error":"item_assigned","detail":"That item already has a Session."}`
	})
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "claim", itemFlags{run: itemRun}, []string{"item-1"}, thinConversation, "",
		envOf(nil)); code != 1 || !strings.Contains(errs.String(), "refused, 409 item_assigned") {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
}
