package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// `todo add` posts every argument as one row, to the conversation the
// environment names, under an Idempotency-Key it prints before asking.
func TestTodoAddPostsEveryRowUnderOneKey(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 201, `{"ok":true,"todos":[{"id":"t1","text":"one"},{"id":"t2","text":"two"}]}`
	})
	var out, errs bytes.Buffer
	code := sessionTodo(&out, &errs, b, "add", []string{"one", "  ", "two"}, "", "",
		envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 {
		t.Fatalf("asked %d times", len(seen))
	}
	r := seen[0]
	if r.Method != "POST" || r.EscapedPath != "/v1/work/v2/agent/session-todos/"+thinConversation || r.Token != thinToken {
		t.Fatalf("request = %+v", r)
	}
	if !strings.HasPrefix(r.Key, "todo-") || !strings.Contains(errs.String(), "Idempotency-Key: "+r.Key) {
		t.Fatalf("key %q, stderr %q", r.Key, errs.String())
	}
	var body struct {
		Todos []map[string]string `json:"todos"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil || len(body.Todos) != 2 ||
		body.Todos[0]["text"] != "one" || body.Todos[1]["text"] != "two" {
		t.Fatalf("body = %s", r.Body)
	}
	if !strings.Contains(out.String(), `"t2"`) {
		t.Fatalf("stdout = %s", out.String())
	}
}

// A retry names its key, and --conversation wins over the environment.
func TestTodoAddRetriesWithTheGivenKey(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, `{"ok":true,"todos":[]}` })
	var out, errs bytes.Buffer
	other := "b7200000-0000-4000-8000-000000000003"
	if code := sessionTodo(&out, &errs, b, "add", []string{"x"}, other, "todo-retry",
		envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	r := s.requests()[0]
	if r.Key != "todo-retry" || r.EscapedPath != "/v1/work/v2/agent/session-todos/"+other {
		t.Fatalf("request = %+v", r)
	}
}

// The work system's flat refusal is said with its status and code.
func TestTodoAddSaysTheRefusal(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 413, `{"error":"too_many_todos","detail":"One call adds at most 20 to-dos; nothing was added."}`
	})
	var out, errs bytes.Buffer
	code := sessionTodo(&out, &errs, b, "add", []string{"x"}, thinConversation, "", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "refused, 413 too_many_todos: One call adds at most 20") {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
	if out.Len() != 0 {
		t.Fatalf("a refusal printed to stdout: %q", out.String())
	}
}

func TestTodoListAndDoneUseTheAgentRoutes(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, `{"ok":true}` })
	env := envOf(map[string]string{"CODEX_THREAD_ID": thinConversation})
	var out, errs bytes.Buffer
	if code := sessionTodo(&out, &errs, b, "list", nil, "", "", env); code != 0 {
		t.Fatalf("list exit %d: %s", code, errs.String())
	}
	if code := sessionTodo(&out, &errs, b, "done", []string{"todo-id-1"}, "", "", env); code != 0 {
		t.Fatalf("done exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 2 {
		t.Fatalf("asked %d times", len(seen))
	}
	if seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/work/v2/agent/session-todos/"+thinConversation || seen[0].Key != "" {
		t.Fatalf("list = %+v", seen[0])
	}
	if seen[1].Method != "POST" || seen[1].EscapedPath != "/v1/work/v2/agent/session-todos/"+thinConversation+"/todo-id-1/complete" ||
		seen[1].Key == "" || string(seen[1].Body) != "{}" {
		t.Fatalf("done = %+v", seen[1])
	}
}

// Without a conversation nothing is asked.
func TestTodoWithoutAConversationAsksNothing(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 201, `{}` })
	var out, errs bytes.Buffer
	if code := sessionTodo(&out, &errs, b, "add", []string{"x"}, "", "", envOf(nil)); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if len(s.requests()) != 0 || !strings.Contains(errs.String(), "Nothing was changed") {
		t.Fatalf("asked %d times; stderr %q", len(s.requests()), errs.String())
	}
}

func TestTodoLinesTakesOneRowPerNonEmptyLine(t *testing.T) {
	rows, err := todoLines(strings.NewReader("1. first\n\n  2. second  \r\n\n3. third"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0] != "1. first" || rows[1] != "2. second" || rows[2] != "3. third" {
		t.Fatalf("rows = %q", rows)
	}
}
