package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/codegraph"
)

func TestCodeGraphCheckRefusesMissingIndexAndInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"check", t.TempDir()}, {"update", "."}, {"check"}, {"check", ".", "extra"}} {
		var out bytes.Buffer
		code := runCodeGraphCheck(context.Background(), args, &out)
		var s codegraph.Status
		if code == 0 || json.Unmarshal(out.Bytes(), &s) != nil || s.State == codegraph.Matched || s.Reason == "" {
			t.Fatalf("args=%v code=%d body=%s", args, code, out.String())
		}
	}
}

func TestCodeGraphQueryUsesOnlyLocalBrokerAndCurrentDirectory(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if r.Method != "POST" || r.URL.Path != "/v1/orchestrator/codegraph/query" || r.Header.Get("X-Clawdline-Orchestrator") != "private" {
			t.Error("wrong authority")
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["directory"] != "/test/worktree" || body["query"] != "Start" || len(body) != 2 {
			t.Error(body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"Start in main.go","generation":"g1"}`)
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "private", client: server.Client()}
	var out, errors bytes.Buffer
	if code := runCodeGraphQuery(b, "/test/worktree", "Start", &out, &errors); code != 0 || !strings.Contains(out.String(), "Start in main.go") {
		t.Fatal(code, out.String(), errors.String())
	}
	if code := runCodeGraphQuery(b, "/test/worktree", strings.Repeat("x", codegraph.QueryBytesLimit+1), &out, &errors); code != 2 || seen != 1 {
		t.Fatal("invalid query sent")
	}
}
