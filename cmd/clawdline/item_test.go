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
 "owner_session":"` + thinConversation + `","version":2,"acceptance_criteria":"The release is visible.",
 "acceptance_version":1,"acceptance_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
 "gate_snapshot_cycle":1,"planning_gate":true,"verify_gate":false,"steps":[
 {"id":"s1","title":"draft","done":false},{"id":"s2","title":"check","done":false},{"id":"s3","title":"publish","done":false}]}}`

func TestItemNameSendsTheCurrentConversationAndReportsTheSavedName(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"stored_title":"Repair Session naming","display_title":"Person's title"}`
	})
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "name", itemFlags{}, []string{"item-1", "Repair Session naming"},
		thinConversation, "", envOf(nil)); code != 0 {
		t.Fatalf("name exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodPost ||
		seen[0].EscapedPath != "/v1/work/v2/agent/items/item-1/session-name" ||
		!strings.Contains(string(seen[0].Body), `"session_id":"`+thinConversation+`"`) {
		t.Fatalf("naming request: %+v", seen)
	}
	if !strings.Contains(out.String(), "Session name: Repair Session naming") ||
		!strings.Contains(out.String(), "Displayed name: Person's title") {
		t.Fatalf("naming output: %q", out.String())
	}
}

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
		description: "The person asked.", acceptance: "The release is visible.",
		steps: []string{"draft", " ", "check", "publish"}}, nil, "", "",
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
		SessionID          string            `json:"session_id"`
		Via                map[string]string `json:"via"`
		ProjectID          string            `json:"project_id"`
		Kind               string            `json:"kind"`
		AcceptanceCriteria string            `json:"acceptance_criteria"`
		Steps              []string          `json:"steps"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil || body.SessionID != thinConversation ||
		body.Via["run"] != itemRun || body.ProjectID != "p1" || body.Kind != "feature" ||
		body.AcceptanceCriteria != "The release is visible." || strings.Join(body.Steps, "|") != "draft|check|publish" {
		t.Fatalf("body = %s", r.Body)
	}
	for _, want := range []string{"item-1  Ship it  [feature, assigned, assigned to " + thinConversation + "]",
		"acceptance v1 sha256:aaaaaaaa", "The release is visible.",
		"gates cycle 1: planning=true verification=false", "needs independent review (set by the person): false",
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

func TestItemAddCanCreateThenDelegateFromTheMachineSteward(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "feature", title: "Ship it",
		description: "The person asked.", run: itemRun, assign: assignFlags{open: true, assistant: "codex"}},
		nil, thinConversation, "item-delegate", envOf(nil))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || !strings.Contains(string(seen[0].Body), `"assign":{"assistant":"codex","mode":"new_session"}`) {
		t.Fatalf("delegation was not sent with item creation: %+v", seen)
	}
}

// --assign-self asks the daemon for {"mode":"self"}; without it no assign is
// sent and the item waits unassigned, with the way to take it later named.
func TestItemAddTakesTheItemOnlyWithAssignSelf(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "feature", title: "Ship it",
		description: "d", run: itemRun, assign: assignFlags{self: true}}, nil, thinConversation, "item-self", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || !strings.Contains(string(seen[0].Body), `"assign":{"mode":"self"}`) {
		t.Fatalf("requests = %+v", seen)
	}

	const unassigned = `{"ok":true,"assigned":false,"assignment_state":"not_requested",` +
		`"item":{"id":"item-2","title":"Later","kind":"issue","phase":"created","owner_session":null,"version":1}}`
	s, b = newStandIn(t, func(r *http.Request) (int, string) { return 201, unassigned })
	out.Reset()
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "Later",
		description: "d", run: itemRun}, nil, thinConversation, "item-later", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || strings.Contains(string(seen[0].Body), `"assign"`) {
		t.Fatalf("requests = %+v", seen)
	}
	if !strings.Contains(out.String(), "item-2  Later  [issue, created, unassigned]") ||
		!strings.Contains(errs.String(), "clawdline item claim item-2") {
		t.Fatalf("stdout %q stderr %q", out.String(), errs.String())
	}

	for _, both := range []assignFlags{{self: true, open: true}, {self: true, terminal: "t1"}} {
		s, b = newStandIn(t, func(r *http.Request) (int, string) { return 201, createdItem })
		errs.Reset()
		if code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "issue", title: "x",
			description: "d", run: itemRun, assign: both}, nil, thinConversation, "", envOf(nil)); code != 2 ||
			!strings.Contains(errs.String(), "one choice") || len(s.requests()) != 0 {
			t.Fatalf("%+v: exit %d, %s", both, code, errs.String())
		}
	}
}

func TestMachineItemAddExplainsEveryDelegationState(t *testing.T) {
	for _, tc := range []struct {
		name, state, errorJSON, want string
		exit                         int
	}{
		{"assigned", "assigned", "", "", 0},
		{"omitted", "not_requested", "", "person can assign it from the Board", 0},
		{"waiting", "awaiting_user", "", "answer its first screen", 1},
		{"pending", "pending", "", "Check the Board", 1},
		{"failed", "failed", `,"assignment_error":{"code":"session_unavailable","message":"no pane"}`, "person can assign item", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := `{"ok":true,"assignment_state":"` + tc.state + `","assigned":` + fmt.Sprint(tc.state == "assigned") +
				tc.errorJSON + `,"item":{"id":"item-1","title":"Part","kind":"feature","phase":"created","owner_session":null,"version":1}}`
			_, b := newStandIn(t, func(*http.Request) (int, string) { return 201, response })
			var out, errs bytes.Buffer
			code := sessionItem(&out, &errs, b, "add", itemFlags{project: "p1", kind: "feature", title: "Part",
				description: "The person asked.", run: itemRun}, nil, thinConversation, "machine-state-"+tc.name, envOf(nil))
			if code != tc.exit || !strings.Contains(errs.String(), tc.want) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errs.String())
			}
		})
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

// `item doc` reads the item for its version and last document position, then
// posts the document after it, under a key it prints first; it needs a role
// and a title before it asks anything.
func TestItemDocPostsADocumentAfterTheLastOne(t *testing.T) {
	const withDocs = `{"ok":true,"item":{"id":"item-1","title":"Big","kind":"epic","phase":"assigned",
 "owner_session":"` + thinConversation + `","version":5,"steps":[],"documents":[
 {"id":"d1","role":"plan","title":"Plan","position":0},{"id":"d2","role":"spec","title":"Spec","position":3}]}}`
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, withDocs
		}
		return 201, withDocs
	})
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "doc", itemFlags{doc: docFlags{role: "plan_review", title: "Plan review",
		reference: "7e000000-0000-4000-8000-000000000001", body: "It holds."}}, []string{"item-1"}, "", "", env)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[1].Method != "POST" || seen[1].EscapedPath != "/v1/work/v2/agent/items/item-1/documents" ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+seen[1].Key) {
		t.Fatalf("requests %+v, stderr %q", seen, errs.String())
	}
	var body map[string]any
	if err := json.Unmarshal(seen[1].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["expected_version"] != 5.0 || body["session_id"] != thinConversation || body["role"] != "plan_review" ||
		body["title"] != "Plan review" || body["reference"] != "7e000000-0000-4000-8000-000000000001" ||
		body["body"] != "It holds." || body["position"] != 4.0 {
		t.Fatalf("body %v", body)
	}
	if !strings.Contains(out.String(), "doc d1  plan  Plan") {
		t.Fatalf("printed %q", out.String())
	}

	s2, b2 := newStandIn(t, func(r *http.Request) (int, string) { return 200, withDocs })
	errs.Reset()
	if code := sessionItem(&out, &errs, b2, "doc", itemFlags{doc: docFlags{title: "No role"}}, []string{"item-1"}, "", "", env); code != 2 ||
		len(s2.requests()) != 0 {
		t.Fatalf("no role: exit %d, %d requests", code, len(s2.requests()))
	}
}

func TestItemAcceptancePatchesTheOwnedItemWithItsCurrentVersion(t *testing.T) {
	const item = `{"ok":true,"item":{"id":"epic-1","title":"Epic","kind":"epic","phase":"assigned","version":3,"steps":[]}}`
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, item })
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "acceptance", itemFlags{acceptance: "- The result can be checked."},
		[]string{"epic-1"}, thinConversation, "", envOf(nil))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[1].Method != http.MethodPatch ||
		seen[1].EscapedPath != "/v1/work/v2/agent/items/epic-1/edit" ||
		!strings.Contains(string(seen[1].Body), `"expected_version":3`) ||
		!strings.Contains(string(seen[1].Body), `"acceptance_criteria":"- The result can be checked."`) {
		t.Fatalf("requests %+v", seen)
	}
}

func TestItemAcceptanceRevisionSendsStableVersionRunAndFileBytes(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"item":{"id":"epic-1","title":"Epic","kind":"epic","phase":"implementing","version":8,"acceptance_version":3,"acceptance_digest":"digest"},"acceptance_source":{"run":"run-1","session_id":"owner","at":17,"excerpt":"revise acceptance"}}`
	})
	var out, errs bytes.Buffer
	f := itemFlags{acceptance: "- Revised.\n", run: "run-1", expectedVersion: 7}
	if code := sessionItem(&out, &errs, b, "acceptance-revise", f, []string{"epic-1"}, thinConversation, "same-key", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodPost ||
		seen[0].EscapedPath != "/v1/work/v2/agent/items/epic-1/acceptance-revision" ||
		!strings.Contains(string(seen[0].Body), `"expected_version":7`) ||
		!strings.Contains(string(seen[0].Body), `"run":"run-1"`) ||
		!strings.Contains(out.String(), "requested by run run-1") {
		t.Fatalf("request %+v output %s", seen, out.String())
	}
}

const epicItem = `{"ok":true,"item":{"id":"epic-1","title":"Big","kind":"epic","phase":"implementing",
 "owner_session":"` + thinConversation + `","version":7}}`

// `item child` reads the Epic for its version and posts the child with its
// kind, steps and the Session it is assigned to, under a printed key, to the
// Epic's children route; no run is read, because the Epic's assignment is
// the authority.
func TestItemChildPostsTheChildUnderTheEpicsVersion(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, epicItem
		}
		return 201, `{"ok":true,"assigned":true,"item":{"id":"child-1","title":"Part","kind":"feature","phase":"assigned","owner_session":"other","version":2}}`
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "child", itemFlags{kind: "feature", title: "Part", description: "Build it.",
		acceptance: "The part works.", steps: []string{"one", " ", "two"}, assign: assignFlags{terminal: "%9"}},
		[]string{"epic-1"}, thinConversation, "", envOf(nil))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/work/v2/items/epic-1" {
		t.Fatalf("requests = %+v", seen)
	}
	r := seen[1]
	if r.Method != "POST" || r.EscapedPath != "/v1/work/v2/agent/items/epic-1/children" ||
		!strings.Contains(errs.String(), "Idempotency-Key: "+r.Key) {
		t.Fatalf("create = %+v, stderr %q", r, errs.String())
	}
	var body struct {
		ExpectedVersion    int64             `json:"expected_version"`
		SessionID          string            `json:"session_id"`
		Kind               string            `json:"kind"`
		AcceptanceCriteria string            `json:"acceptance_criteria"`
		Steps              []string          `json:"steps"`
		Assign             map[string]string `json:"assign"`
		Via                any               `json:"via"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil || body.ExpectedVersion != 7 || body.SessionID != thinConversation ||
		body.Kind != "feature" || body.AcceptanceCriteria != "The part works." ||
		strings.Join(body.Steps, "|") != "one|two" || body.Via != nil ||
		body.Assign["mode"] != "existing_session" || body.Assign["terminal_id"] != "%9" {
		t.Fatalf("body = %s", r.Body)
	}
	if !strings.Contains(out.String(), "child-1  Part  [feature, assigned, assigned to other]") {
		t.Fatalf("stdout = %s", out.String())
	}
}

// A child created but not assigned says so, names the assignment's code and
// how to assign it, and exits non-zero; two Session choices are refused
// before anything is asked.
func TestItemChildSaysAnAssignmentThatFailed(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, epicItem
		}
		return 201, `{"ok":true,"assigned":false,"assignment_error":{"code":"assignment_failed","message":"no terminal"},
 "item":{"id":"child-2","title":"Part","kind":"issue","phase":"created","owner_session":null,"version":2}}`
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "child", itemFlags{kind: "issue", title: "Part", description: "d",
		assign: assignFlags{open: true, assistant: "claude"}}, []string{"epic-1"}, thinConversation, "", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "created but not assigned: assignment_failed") ||
		!strings.Contains(errs.String(), "clawdline item assign child-2") || !strings.Contains(out.String(), "child-2") {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out.String(), errs.String())
	}
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, epicItem })
	errs.Reset()
	code = sessionItem(&out, &errs, b, "child", itemFlags{kind: "issue", title: "Part",
		assign: assignFlags{open: true, terminal: "%9"}}, []string{"epic-1"}, thinConversation, "", envOf(nil))
	if code != 2 || len(s.requests()) != 0 {
		t.Fatalf("two choices: exit %d, %d requests, %s", code, len(s.requests()), errs.String())
	}
}

// `item assign` reads the child's version and posts the chosen Session to
// its assign route; without a Session named it asks nothing.
func TestItemAssignPostsTheChosenSession(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"item":{"id":"child-1","title":"Part","kind":"feature","phase":"assigned","owner_session":"x","version":4,"parent_id":"epic-1"}}`
	})
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{assign: assignFlags{open: true, assistant: "codex", model: "m"}},
		[]string{"child-1"}, thinConversation, "k-1", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[1].EscapedPath != "/v1/work/v2/agent/items/child-1/assign" || seen[1].Key != "k-1" {
		t.Fatalf("requests = %+v", seen)
	}
	var body map[string]any
	if err := json.Unmarshal(seen[1].Body, &body); err != nil || body["mode"] != "new_session" ||
		body["assistant"] != "codex" || body["model"] != "m" || body["expected_version"] != float64(4) ||
		body["session_id"] != thinConversation {
		t.Fatalf("body = %s", seen[1].Body)
	}
	s, b = newStandIn(t, func(r *http.Request) (int, string) { return 200, epicItem })
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{}, []string{"child-1"}, thinConversation, "", envOf(nil)); code != 2 ||
		len(s.requests()) != 0 {
		t.Fatalf("no Session named: exit %d, %d requests", code, len(s.requests()))
	}
}

// `item assign --new` on an item that is no Epic's child goes under the
// person's message: it reads this conversation's latest run (or --run) and
// sends it as via.run. An existing Session is refused before anything is
// asked, and so is a conversation with no run, before the assignment.
func TestItemAssignOfAnOrdinaryItemGoesUnderThePersonsMessage(t *testing.T) {
	ordinary := `{"ok":true,"item":{"id":"item-1","title":"Notes","kind":"feature","phase":"created","owner_session":null,"version":3}}`
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/run") {
			return 200, `{"ok":true,"run":{"id":"` + itemRun + `","session_id":"` + thinConversation + `"}}`
		}
		return 200, ordinary
	})
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{assign: assignFlags{open: true, assistant: "claude", persona: "security"}},
		[]string{"item-1"}, thinConversation, "k-run", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 3 || seen[1].EscapedPath != "/v1/orchestrator/sessions/"+thinConversation+"/run" ||
		seen[2].EscapedPath != "/v1/work/v2/agent/items/item-1/assign" || seen[2].Key != "k-run" {
		t.Fatalf("requests = %+v", seen)
	}
	var body struct {
		Mode    string            `json:"mode"`
		Persona string            `json:"persona"`
		Version int64             `json:"expected_version"`
		Via     map[string]string `json:"via"`
	}
	if err := json.Unmarshal(seen[2].Body, &body); err != nil || body.Mode != "new_session" || body.Persona != "security" ||
		body.Version != 3 || body.Via["run"] != itemRun {
		t.Fatalf("body = %s", seen[2].Body)
	}

	s, b = newStandIn(t, func(r *http.Request) (int, string) { return 200, ordinary })
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{assign: assignFlags{terminal: "%9"}},
		[]string{"item-1"}, thinConversation, "", envOf(nil)); code != 2 || !strings.Contains(errs.String(), "item claim item-1") {
		t.Fatalf("existing Session: exit %d: %s", code, errs.String())
	}
	for _, r := range s.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("an existing Session posted: %+v", r)
		}
	}

	s, b = newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/run") {
			return 404, `{"error":"no_run","detail":"no message"}`
		}
		return 200, ordinary
	})
	errs.Reset()
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{assign: assignFlags{open: true}},
		[]string{"item-1"}, thinConversation, "", envOf(nil)); code == 0 || !strings.Contains(errs.String(), "Nothing was assigned.") {
		t.Fatalf("no run: exit %d: %s", code, errs.String())
	}
	for _, r := range s.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("no run posted: %+v", r)
		}
	}
}

// --persona goes with a new Session: `item child --assign-new` and `item
// assign --new` send it in the assign object, and with an existing terminal,
// without a new Session, or naming an id this build lacks it is a usage error
// before anything is asked.
func TestItemPersonaGoesOnlyWithANewSession(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, epicItem
		}
		return 201, `{"ok":true,"assigned":true,"item":{"id":"child-1","title":"Part","kind":"feature","phase":"assigned","owner_session":"x","version":2}}`
	})
	var out, errs bytes.Buffer
	if code := sessionItem(&out, &errs, b, "child", itemFlags{kind: "feature", title: "Part", description: "d",
		assign: assignFlags{open: true, assistant: "claude", persona: "backend"}}, []string{"epic-1"}, thinConversation, "",
		envOf(nil)); code != 0 {
		t.Fatalf("child: exit %d: %s", code, errs.String())
	}
	var child struct {
		Assign map[string]string `json:"assign"`
	}
	if seen := s.requests(); len(seen) != 2 || json.Unmarshal(seen[1].Body, &child) != nil ||
		child.Assign["mode"] != "new_session" || child.Assign["persona"] != "backend" {
		t.Fatalf("child body = %+v", seen)
	}

	s, b = newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"item":{"id":"child-1","title":"Part","kind":"feature","phase":"assigned","owner_session":"x","version":4,"parent_id":"epic-1"}}`
	})
	if code := sessionItem(&out, &errs, b, "assign", itemFlags{assign: assignFlags{open: true, persona: "code-reviewer"}},
		[]string{"child-1"}, thinConversation, "", envOf(nil)); code != 0 {
		t.Fatalf("assign: exit %d: %s", code, errs.String())
	}
	var assigned map[string]any
	if seen := s.requests(); len(seen) != 2 || json.Unmarshal(seen[1].Body, &assigned) != nil ||
		assigned["mode"] != "new_session" || assigned["persona"] != "code-reviewer" {
		t.Fatalf("assign body = %+v", seen)
	}

	for _, tc := range []struct {
		op     string
		assign assignFlags
		says   string
	}{
		{"child", assignFlags{terminal: "%9", persona: "backend"}, "not with --assign-terminal"},
		{"child", assignFlags{persona: "backend"}, "goes with --assign-new"},
		{"child", assignFlags{open: true, persona: "wizard"}, `"wizard" is not a persona`},
		{"assign", assignFlags{terminal: "%9", persona: "backend"}, "not with --terminal"},
		{"assign", assignFlags{open: true, persona: "wizard"}, `"wizard" is not a persona`},
	} {
		s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, epicItem })
		errs.Reset()
		f := itemFlags{kind: "feature", title: "Part", assign: tc.assign}
		if code := sessionItem(&out, &errs, b, tc.op, f, []string{"epic-1"}, thinConversation, "", envOf(nil)); code != 2 ||
			len(s.requests()) != 0 || !strings.Contains(errs.String(), tc.says) {
			t.Errorf("%s %+v: exit %d, %d requests, %q", tc.op, tc.assign, code, len(s.requests()), errs.String())
		}
	}
}

// `finish` reads the item for its version and posts only the notes it was
// given; a landing flag travels alone, for the daemon to fill in the rest.
func TestItemFinishPostsOnlyTheNotesItWasGiven(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})
	var out, errs bytes.Buffer
	f := itemFlags{phase: phaseEvidence{verification: "go test ./... passed", deployment: "daemon rebuilt", remote: "upstream"}}
	if code := sessionItem(&out, &errs, b, "finish", f, []string{"item-1"}, "", "", env); code != 0 {
		t.Fatalf("finish exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 || seen[0].Method != "GET" || seen[1].Method != "POST" ||
		seen[1].EscapedPath != "/v1/work/v2/agent/items/item-1/finish" || seen[1].Key == "" {
		t.Fatalf("requests = %+v", seen)
	}
	var body map[string]any
	if err := json.Unmarshal(seen[1].Body, &body); err != nil {
		t.Fatal(err)
	}
	landing, _ := body["landing"].(map[string]any)
	if body["expected_version"] != float64(2) || body["session_id"] != thinConversation ||
		body["verification"] != "go test ./... passed" || body["deployment"] != "daemon rebuilt" ||
		len(landing) != 1 || landing["remote"] != "upstream" || len(body) != 5 {
		t.Fatalf("finish body = %s", seen[1].Body)
	}

	// With no landing flag, no landing is sent: the daemon reads it.
	s, b = newStandIn(t, func(r *http.Request) (int, string) { return 200, createdItem })
	f = itemFlags{phase: phaseEvidence{noDeployment: "library only"}}
	if code := sessionItem(&out, &errs, b, "finish", f, []string{"item-1"}, "", "", env); code != 0 {
		t.Fatalf("finish exit %d: %s", code, errs.String())
	}
	if p := s.requests()[1]; strings.Contains(string(p.Body), "landing") || !strings.Contains(string(p.Body), `"no_deployment_reason":"library only"`) {
		t.Fatalf("finish body = %s", p.Body)
	}
}
