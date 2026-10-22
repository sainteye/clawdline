package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewerReadKeepsTheHeldTranscriptRevision(t *testing.T) {
	const revision = "agy1:synthetic:12:+&?"
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/v1/cloud/viewer/detail" || r.Header.Get("Authorization") != "Bearer synthetic-local" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"transcript":{"signature":"agy1:synthetic:older","entries":[]}}`))
	}))
	defer server.Close()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "local-token"), []byte("synthetic-local"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	args := []string{"read", "--machine", "synthetic-machine", "--session", "%1", "--generation", strings.Repeat("a", 32), "--before", "12", "--revision", revision}
	var out, failures bytes.Buffer
	if code := cloudViewerSessionCommand(&out, &failures, args); code != 0 {
		t.Fatalf("exit %d: %s", code, failures.String())
	}
	if received.Get("revision") != revision || received.Get("before") != "12" || received.Get("session") != "%1" || received.Get("generation") != strings.Repeat("a", 32) {
		t.Fatalf("lost held page identity: %v", received)
	}
	if !strings.Contains(out.String(), "agy1:synthetic:older") {
		t.Fatalf("missing next page revision: %s", out.String())
	}
}

func TestViewerRevisionRequiresAnOlderRead(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "local-token"), []byte("synthetic-local"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	for _, word := range []string{"read", "sessions", "send", "answer", "interrupt", "end", "receipt"} {
		var out, failures bytes.Buffer
		args := []string{word, "--machine", "synthetic-machine", "--session", "%1", "--generation", strings.Repeat("a", 32), "--revision", "agy1:synthetic"}
		if word != "read" {
			args = append(args, "--before", "12")
		}
		if code := cloudViewerSessionCommand(&out, &failures, args); code != 2 {
			t.Errorf("%s accepted an unrelated revision: exit %d", word, code)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid revision use sent %d requests", calls)
	}
}
