package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoteCreateInjectsIdentityAndPreservesRetryKey(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/v1/work/v2/agent/human-interventions" || r.Method != http.MethodPost ||
			r.Header.Get("X-Clawdline-Orchestrator") != "machine-secret" || r.Header.Get("Idempotency-Key") != "same-press" {
			t.Errorf("the note did not use the Agent route and credential: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["source_conversation"] != "root-conversation" || body["target_session"] != "target-terminal" ||
			body["title"] != "Choose a date" {
			t.Errorf("the note lost its source, target, or content: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"note":{"id":"note-id"}}`))
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "target-terminal", "", []byte(`{"kind":"answer","title":"Choose a date"}`), "same-press",
		func(name string) string {
			if name == conversationEnv[0] {
				return "root-conversation"
			}
			return ""
		})
	if code != 0 || called != 1 || !strings.Contains(out.String(), "note-id") {
		t.Fatalf("note create: code %d, calls %d, output %q, errors %q", code, called, out.String(), errs.String())
	}
}

func TestNoteCreateRejectsCallerSuppliedIdentity(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	defer server.Close()
	b := &broker{base: server.URL, token: "machine-secret", client: server.Client()}
	var out, errs bytes.Buffer
	code := createNote(&out, &errs, b, "target-terminal", "root-conversation",
		[]byte(`{"source_conversation":"somebody-else","kind":"answer"}`), "",
		func(string) string { return "" })
	if code != 2 || called != 0 || !strings.Contains(errs.String(), "source_conversation") {
		t.Fatalf("identity spoof was sent: code %d, calls %d, errors %q", code, called, errs.String())
	}
}
