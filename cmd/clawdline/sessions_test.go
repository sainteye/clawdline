package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const sessionsAnswer = `{"sessions":[
 {"id":"%12","assistant":"claude","state":"working","work_state":"implementing","taskId":"task-a",
  "label":"Fix  the\nboard","cwd":"/repo","closeability":{}},
 {"id":"%13","state":"idle","work_state":"","closeability":{}}]}`

// `sessions` reads the daemon's address book and prints a row per Session,
// with a dash where a field is empty.
func TestSessionsPrintsARowPerSession(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, sessionsAnswer })
	var out, errs bytes.Buffer
	if code := sessionsRun(&out, &errs, b, false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/orchestrator/sessions" ||
		seen[0].Token != thinToken {
		t.Fatalf("requests = %+v", seen)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "TERMINAL ASSISTANT STATE WORK TASK LABEL CWD" ||
		strings.Join(strings.Fields(lines[1]), " ") != "%12 claude working implementing task-a Fix the board /repo" ||
		strings.Join(strings.Fields(lines[2]), " ") != "%13 - idle - - - -" {
		t.Fatalf("stdout:\n%s", out.String())
	}
}

// --json prints the daemon's answer itself.
func TestSessionsJSONPrintsTheAnswer(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, sessionsAnswer })
	var out, errs bytes.Buffer
	if code := sessionsRun(&out, &errs, b, true); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	var got, want any
	if json.Unmarshal(out.Bytes(), &got) != nil || json.Unmarshal([]byte(sessionsAnswer), &want) != nil {
		t.Fatalf("stdout is not JSON: %s", out.String())
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("stdout %s, want %s", g, w)
	}
}

// An answer that is no session list is an error, never an empty table.
func TestSessionsRefusesAnAnswerThatIsNoList(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"500", `{"error":"internal","detail":"boom"}`, 500},
		{"no list", `{"ok":true}`, 200},
		{"not JSON", `<html>`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, b := newStandIn(t, func(r *http.Request) (int, string) { return tc.status, tc.body })
			for _, asJSON := range []bool{false, true} {
				var out, errs bytes.Buffer
				if code := sessionsRun(&out, &errs, b, asJSON); code == 0 || out.Len() != 0 || errs.Len() == 0 {
					t.Fatalf("json=%t: exit %d, stdout %q, stderr %q", asJSON, code, out.String(), errs.String())
				}
			}
		})
	}
}
