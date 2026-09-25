package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	dispatchID     = "d1570000-0000-4000-8000-000000000001"
	dispatchSecret = "5ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2"
)

// dispatchWorld is a stand-in daemon for dispatch: an inventory with a live
// row that overlaps, and the nth POST answered by post.
type dispatchWorld struct {
	root        string
	generations []string
	posts       int
}

func (w *dispatchWorld) daemon(t *testing.T, post func(n int) (int, string)) (*standIn, *broker) {
	t.Helper()
	return newStandIn(t, func(r *http.Request) (int, string) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orchestrator/inventory":
			gen := w.generations[0]
			if len(w.generations) > 1 {
				w.generations = w.generations[1:]
			}
			inv, _ := json.Marshal(map[string]any{
				"generation": gen, "task_root": w.root,
				"live": []map[string]any{
					{"task": "other-task", "title": "Someone else", "state": "working", "overlaps": []string{"a.go"}},
					{"task": "quiet-task", "title": "Elsewhere", "state": "working"},
				},
				"unlanded": []any{}, "droppable": []any{}, "unreadable": []any{},
			})
			return 200, string(inv)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orchestrator/tasks":
			w.posts++
			return post(w.posts)
		}
		return 404, `{"error":{"code":"not_found","message":"no"}}`
	})
}

func dispatchedAnswer(state string) string {
	return `{"ok":true,"task":{"id":"` + dispatchID + `","state":"` + state + `",` +
		`"worktree":{"path":"/wt/` + dispatchID + `"}},` +
		`"warnings":[{"code":"claims_ignored_for_worktree","message":"claims are advisory in a worktree"}]}`
}

const staleAnswer = `{"error":{"code":"stale_inventory","message":"This dispatch carried an inventory_generation this repository has moved past."}}`

func testDispatchEnv(env map[string]string) dispatchEnv {
	return dispatchEnv{
		getenv:   envOf(env),
		toplevel: func() (string, error) { return "/repo", nil },
		fresh:    func() (string, string, error) { return dispatchID, dispatchSecret, nil },
	}
}

func testDispatchOptions() dispatchOptions {
	return dispatchOptions{
		Title: "Build the thing", Claims: []string{"a.go", "b.go"}, ClaimsGiven: true,
		Isolation: "worktree", PermissionMode: "edits", Timeout: 45, Kind: "feature",
		Deliverables: []string{"docs/x.md"}, Model: "opus", Label: "my root",
		Instructions: "Do the whole thing.\n",
	}
}

// postBodies are the bodies the stand-in received for POST /tasks.
func postBodies(s *standIn) []map[string]string {
	var out []map[string]string
	for _, r := range s.requests() {
		if r.Method == http.MethodPost && r.EscapedPath == "/v1/orchestrator/tasks" {
			var body map[string]string
			_ = json.Unmarshal(r.Body, &body)
			out = append(out, body)
		}
	}
	return out
}

// The happy path: the inventory is read with the claims, task.json is written
// with every field guide §4 lists and no secret, the POST carries id, secret
// and generation, and the output is one line per fact — the secret in none.
func TestDispatchWritesTheBriefAndPostsIt(t *testing.T) {
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"0123456789abcdef"}}
	s, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
	var out, errs bytes.Buffer
	code := dispatchTask(&out, &errs, b, testDispatchOptions(),
		testDispatchEnv(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}

	seen := s.requests()
	if seen[0].EscapedPath != "/v1/orchestrator/inventory" || seen[0].Query != "claims=a.go%2Cb.go&project=%2Frepo" {
		t.Fatalf("inventory asked as %+v", seen[0])
	}
	bodies := postBodies(s)
	if len(bodies) != 1 {
		t.Fatalf("posted %d times", len(bodies))
	}
	want := map[string]string{"task_id": dispatchID, "secret": dispatchSecret, "inventory_generation": "0123456789abcdef"}
	for k, v := range want {
		if bodies[0][k] != v {
			t.Fatalf("POST body %s = %q, want %q", k, bodies[0][k], v)
		}
	}
	for _, r := range seen {
		if r.Token != thinToken {
			t.Fatalf("a request went without the orchestrator token: %+v", r)
		}
	}

	dir := filepath.Join(w.root, dispatchID)
	raw, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), dispatchSecret) {
		t.Fatal("task.json carries the secret")
	}
	var task map[string]any
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{
		"clawdline_protocol": 1.0, "task_id": dispatchID, "assistant": "claude", "project_dir": "/repo",
		"title": "Build the thing", "instructions": "Do the whole thing.\n", "isolation": "worktree",
		"permission_mode": "edits", "timeout_minutes": 45.0, "kind": "feature", "model": "opus",
	} {
		if task[k] != v {
			t.Errorf("task.json %s = %#v, want %#v", k, task[k], v)
		}
	}
	if got, _ := json.Marshal(task["claims"]); string(got) != `["a.go","b.go"]` {
		t.Errorf("claims = %s", got)
	}
	if got, _ := json.Marshal(task["deliverables"]); string(got) != `["docs/x.md"]` {
		t.Errorf("deliverables = %s", got)
	}
	if got, _ := json.Marshal(task["root"]); string(got) !=
		`{"assistant":"claude","label":"my root","session_id":"`+thinConversation+`"}` {
		t.Errorf("root = %s", got)
	}
	if _, ok := task["secret"]; ok {
		t.Error("task.json has a secret field")
	}
	if info, err := os.Stat(filepath.Join(dir, "task.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("task.json mode = %v, %v", info.Mode(), err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("task directory mode = %v, %v", info.Mode(), err)
	}

	wantOut := "dispatched " + dispatchID + " spawning worktree /wt/" + dispatchID + "\n" +
		"warning claims_ignored_for_worktree: claims are advisory in a worktree\n" +
		`warning overlap: live task other-task (working) "Someone else" also claims a.go` + "\n"
	if out.String() != wantOut {
		t.Fatalf("stdout:\n%s\nwant:\n%s", out.String(), wantOut)
	}
	if strings.Contains(out.String()+errs.String(), dispatchSecret) || strings.Contains(out.String()+errs.String(), thinToken) {
		t.Fatal("a credential was printed")
	}
}

// A stale answer is met by reading the inventory once more and resending
// with the new generation; the same task.json serves both.
func TestDispatchResendsOnceAfterAStaleInventory(t *testing.T) {
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"}}
	s, b := w.daemon(t, func(n int) (int, string) {
		if n == 1 {
			return 409, staleAnswer
		}
		return 200, dispatchedAnswer("spawning")
	})
	var out, errs bytes.Buffer
	code := dispatchTask(&out, &errs, b, testDispatchOptions(),
		testDispatchEnv(map[string]string{"CODEX_THREAD_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	bodies := postBodies(s)
	if len(bodies) != 2 || bodies[0]["inventory_generation"] != "aaaaaaaaaaaaaaaa" ||
		bodies[1]["inventory_generation"] != "bbbbbbbbbbbbbbbb" || bodies[1]["task_id"] != dispatchID {
		t.Fatalf("posts = %+v", bodies)
	}
	raw, err := os.ReadFile(filepath.Join(w.root, dispatchID, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		Assistant string            `json:"assistant"`
		Root      map[string]string `json:"root"`
	}
	_ = json.Unmarshal(raw, &task)
	if task.Assistant != "codex" || task.Root["assistant"] != "codex" {
		t.Fatalf("a Codex root's child and root assistant = %q, %q", task.Assistant, task.Root["assistant"])
	}
	if !strings.HasPrefix(out.String(), "dispatched "+dispatchID+" spawning") {
		t.Fatalf("stdout: %s", out.String())
	}
}

// A second stale answer is reported as the refusal it is, not chased; the
// brief this command wrote is taken back.
func TestDispatchReportsASecondStaleInventory(t *testing.T) {
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"}}
	s, b := w.daemon(t, func(int) (int, string) { return 409, staleAnswer })
	var out, errs bytes.Buffer
	code := dispatchTask(&out, &errs, b, testDispatchOptions(),
		testDispatchEnv(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := len(postBodies(s)); n != 2 {
		t.Fatalf("posted %d times, want 2", n)
	}
	want := "clawdline dispatch: refused, 409 stale_inventory: This dispatch carried an inventory_generation this repository has moved past.\n"
	if errs.String() != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", errs.String(), want)
	}
	if _, err := os.Stat(filepath.Join(w.root, dispatchID)); !os.IsNotExist(err) {
		t.Fatalf("the refused task's directory is still there: %v", err)
	}
}

// With no conversation to name as root, nothing is read, written or sent.
func TestDispatchRefusesWithoutAConversation(t *testing.T) {
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
	s, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
	var out, errs bytes.Buffer
	code := dispatchTask(&out, &errs, b, testDispatchOptions(), testDispatchEnv(nil))
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errs.String(), "Pass --conversation") || !strings.Contains(errs.String(), "Nothing was dispatched.") {
		t.Fatalf("stderr: %s", errs.String())
	}
	if len(s.requests()) != 0 {
		t.Fatalf("the daemon was asked: %+v", s.requests())
	}
	if entries, _ := os.ReadDir(w.root); len(entries) != 0 {
		t.Fatalf("something was written: %v", entries)
	}
}

// Instructions past the daemon's 16 KiB are refused here, before the daemon
// is asked anything — from stdin, from a file, and as a value.
func TestDispatchRefusesOverLongInstructionsLocally(t *testing.T) {
	long := strings.Repeat("x", dispatchInstructionsLimit+1)
	if _, err := readInstructions("", strings.NewReader(long)); err == nil ||
		!strings.Contains(err.Error(), "longer than 16384 bytes") {
		t.Fatalf("stdin: %v", err)
	}
	file := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(file, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstructions(file, strings.NewReader("")); err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("file: %v", err)
	}
	if got, err := readInstructions("", strings.NewReader(long[:dispatchInstructionsLimit])); err != nil ||
		len(got) != dispatchInstructionsLimit {
		t.Fatalf("exactly the limit was refused: %v", err)
	}

	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
	s, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
	o := testDispatchOptions()
	o.Instructions = long
	var out, errs bytes.Buffer
	if code := dispatchTask(&out, &errs, b, o,
		testDispatchEnv(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if len(s.requests()) != 0 {
		t.Fatalf("the daemon was asked: %+v", s.requests())
	}
}

// --json prints the daemon's answer, and an answer that somehow carried the
// secret is printed with it masked.
func TestDispatchNeverPrintsTheSecret(t *testing.T) {
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
	_, b := w.daemon(t, func(int) (int, string) {
		return 200, `{"ok":true,"echo":"` + dispatchSecret + `","task":{"id":"` + dispatchID + `","state":"spawning"}}`
	})
	o := testDispatchOptions()
	o.JSON = true
	var out, errs bytes.Buffer
	if code := dispatchTask(&out, &errs, b, o,
		testDispatchEnv(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if strings.Contains(out.String()+errs.String(), dispatchSecret) {
		t.Fatalf("the secret was printed:\n%s%s", out.String(), errs.String())
	}
	if !strings.Contains(out.String(), `"echo": "<task-secret>"`) {
		t.Fatalf("stdout: %s", out.String())
	}
	if !strings.Contains(errs.String(), "warning overlap: live task other-task") {
		t.Fatalf("the overlap warning did not go to stderr beside --json: %s", errs.String())
	}
}

// Forgetting --claims is refused; `--claims ""` is an explicit empty set.
func TestDispatchClaimsMustBeSaid(t *testing.T) {
	var l listFlag
	if err := l.Set(""); err != nil || !l.set || len(l.values) != 0 {
		t.Fatalf("--claims '' = %+v", l)
	}
	_ = l.Set("a.go, b.go")
	_ = l.Set("c.go")
	if strings.Join(l.values, "|") != "a.go|b.go|c.go" {
		t.Fatalf("values = %v", l.values)
	}
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
	_, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
	o := testDispatchOptions()
	o.Claims, o.ClaimsGiven = nil, false
	var out, errs bytes.Buffer
	if code := dispatchTask(&out, &errs, b, o,
		testDispatchEnv(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})); code != 2 ||
		!strings.Contains(errs.String(), "--claims is required") {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
}

// A fresh id is a lowercase UUID v4 and the secret 64 lowercase hex.
func TestFreshTaskIsAUUIDAndASecret(t *testing.T) {
	id, secret, err := freshTask()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) || strings.ToLower(id) != id {
		t.Fatalf("id = %s", id)
	}
	if len(secret) != 64 || strings.Trim(secret, "0123456789abcdef") != "" {
		t.Fatalf("secret = %s", secret)
	}
}

// `task ack` posts the notice id alone and says so in one line.
func TestTaskAckPostsTheNotice(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"acknowledged":true,"changed":true,"notice_id":"n-1"}`
	})
	var out, errs bytes.Buffer
	if code := ackTask(&out, &errs, b, dispatchID, "N-1"); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != "POST" ||
		seen[0].EscapedPath != "/v1/orchestrator/tasks/"+dispatchID+"/completion/ack" ||
		string(seen[0].Body) != `{"notice_id":"N-1"}` || seen[0].Token != thinToken {
		t.Fatalf("asked %+v", seen)
	}
	if out.String() != "acknowledged "+dispatchID+" notice n-1\n" {
		t.Fatalf("stdout: %q", out.String())
	}

	_, b = newStandIn(t, func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"completion_notice_mismatch","message":"The notice id does not identify this task's completion envelope."}}`
	})
	errs.Reset()
	if code := ackTask(&out, &errs, b, dispatchID, "wrong"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if errs.String() != "clawdline task ack: refused, 409 completion_notice_mismatch: "+
		"The notice id does not identify this task's completion envelope.\n" {
		t.Fatalf("stderr: %q", errs.String())
	}
}
