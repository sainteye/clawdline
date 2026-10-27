package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaybookCommandsUseExistingTransport(t *testing.T) {
	entryLanguage(t, "en")
	var writes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Clawdline-Orchestrator") != "private-token" {
			t.Error("machine credential missing")
		}
		if r.Method == "GET" {
			if r.URL.RawQuery != "" && r.URL.Query().Get("session") != "%47" {
				t.Errorf("session was not URL encoded: %s", r.URL)
			}
			io.WriteString(w, `{"project":{"key":"/canonical/project","label":"Project"},"snippets":[{"id":"pb","scope":"playbook","title":"Saved","body":"Message","source":{"name":"Skill"}},{"id":"ordinary","scope":"project"}]}`)
			return
		}
		if r.Header.Get("Idempotency-Key") != "same-operation" {
			t.Error("retry identity changed")
		}
		raw, _ := io.ReadAll(r.Body)
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(raw))
		if r.Method != "DELETE" && r.Header.Get("Content-Type") != "application/json" {
			t.Error("JSON content type missing")
		}
		io.WriteString(w, `{"id":"pb","scope":"playbook"}`)
	}))
	defer server.Close()
	open := func(int) (*broker, error) {
		return &broker{base: server.URL, token: "private-token", client: server.Client()}, nil
	}
	run := func(args []string, input string) string {
		t.Helper()
		var out, errs bytes.Buffer
		if code := runPlaybook(args, strings.NewReader(input), &out, &errs, open); code != 0 {
			t.Fatalf("%v: exit %d, %s", args, code, errs.String())
		}
		if strings.Contains(out.String()+errs.String(), "private-token") {
			t.Fatal("credential leaked")
		}
		return out.String()
	}
	out := run([]string{"list", "--session", "%47"}, "")
	if strings.Contains(out, "ordinary") || !strings.Contains(out, "/canonical/project") || !strings.Contains(out, "Skill") {
		t.Fatalf("list lost Project/source or exposed a snippet: %s", out)
	}
	run([]string{"create", "--project", "/canonical/project", "--key", "same-operation", "--body-file", "-"}, `{"title":"Review","body":"Review changes"}`)
	run([]string{"create", "--global", "--key", "same-operation", "--body-file", "-"}, `{"title":"Global","body":"Review changes"}`)
	for i := 0; i < 2; i++ {
		run([]string{"edit", "--key", "same-operation", "--body-file", "-", "pb"}, `{"body":"Updated"}`)
	}
	run([]string{"delete", "--key", "same-operation", "pb"}, "")
	if len(writes) != 5 || writes[2] != writes[3] {
		t.Fatalf("retry changed request: %v", writes)
	}
	for i, want := range []string{
		`POST /v1/snippets {"body":"Review changes","project":"/canonical/project","scope":"playbook","title":"Review"}`,
		`POST /v1/snippets {"body":"Review changes","scope":"playbook","title":"Global"}`,
		`PATCH /v1/snippets/pb {"body":"Updated"}`,
		`PATCH /v1/snippets/pb {"body":"Updated"}`,
		`DELETE /v1/snippets/pb `,
	} {
		if writes[i] != want {
			t.Errorf("write %d: %s, want %s", i, writes[i], want)
		}
	}
}

func TestPlaybookRefusesAmbiguousScopeAndNonPlaybookWrites(t *testing.T) {
	entryLanguage(t, "en")
	for _, tc := range []struct {
		args []string
		body string
	}{
		{[]string{"create", "--body-file", "-"}, `{"title":"x","body":"y"}`},
		{[]string{"create", "--global", "--project", "/p", "--body-file", "-"}, `{}`},
		{[]string{"edit", "--body-file", "-", "pb"}, `{"scope":"project"}`},
		{[]string{"edit", "--body-file", "-", "pb"}, `{"project":"/another"}`},
		{[]string{"edit", "--body-file", "-", "pb"}, `{"id":"foreign"}`},
		{[]string{"edit", "--body-file", "-", "pb"}, `null`},
		{[]string{"edit", "--body-file", "-", "pb"}, `{} {}`},
	} {
		var out, errs bytes.Buffer
		code := runPlaybook(tc.args, strings.NewReader(tc.body), &out, &errs, func(int) (*broker, error) { t.Fatal("invalid input reached daemon"); return nil, nil })
		if code != 2 {
			t.Fatalf("%v: exit %d", tc.args, code)
		}
	}
	requests := 0
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		requests++
		if r.Method != "GET" {
			t.Fatal("ordinary snippet was changed")
		}
		return 200, `{"snippets":[{"id":"ordinary","scope":"global"}]}`
	})
	for _, args := range [][]string{{"delete", "ordinary"}, {"edit", "--body-file", "-", "ordinary"}} {
		var out, errs bytes.Buffer
		if code := runPlaybook(args, strings.NewReader(`{"title":"Changed"}`), &out, &errs, func(int) (*broker, error) { return b, nil }); code != 1 {
			t.Fatalf("non-Playbook exit %d", code)
		}
	}
	if requests != 2 {
		t.Fatalf("unexpected retries: %d", requests)
	}
}

func TestPlaybookListPropagatesRefusalAndRejectsUnreadableList(t *testing.T) {
	entryLanguage(t, "zh-Hant")
	for _, response := range []struct {
		status int
		body   string
	}{
		{401, `{"error":"unauthorized","detail":"Missing credential"}`},
		{200, `{"snippets":null}`},
		{200, `not-json`},
	} {
		_, b := newStandIn(t, func(*http.Request) (int, string) { return response.status, response.body })
		var out, errs bytes.Buffer
		if code := runPlaybook([]string{"list"}, strings.NewReader(""), &out, &errs, func(int) (*broker, error) { return b, nil }); code != 1 || out.Len() != 0 || errs.Len() == 0 {
			t.Fatalf("bad list treated as success: %d %s %s", code, &out, &errs)
		}
	}
	var out, errs bytes.Buffer
	runPlaybook(nil, nil, &out, &errs, nil)
	if !strings.Contains(errs.String(), "用法") {
		t.Fatal("Traditional Chinese help missing")
	}
	// Machine JSON stays independent of the selected language.
	_, b := newStandIn(t, func(*http.Request) (int, string) { return 200, `{"snippets":[]}` })
	if runPlaybook([]string{"list"}, nil, &out, &errs, func(int) (*broker, error) { return b, nil }) != 0 || !json.Valid(out.Bytes()) {
		t.Fatal("localized list lost JSON")
	}
}
