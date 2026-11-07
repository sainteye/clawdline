package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stepCancelHelper = "CLAWDLINE_ITEM_STEP_CANCEL_HELPER"

// TestItemStepCancelHelperProcess runs `clawdline item` in a process of its
// own, because the command exits; it is not a test on its own.
func TestItemStepCancelHelperProcess(t *testing.T) {
	if os.Getenv(stepCancelHelper) != "1" {
		t.Skip("helper process")
	}
	var args []string
	_ = json.Unmarshal([]byte(os.Getenv(stepCancelHelper+"_ARGS")), &args)
	itemCommand(args)
	os.Exit(0)
}

// `item step-cancel --reason` parsed the reason and never kept it, so every
// call was refused "--reason is required" (a Gemini probe, 2026-11-07). The
// reason now reaches the daemon whether it comes before or after the ids.
func TestItemStepCancelSendsTheReasonItWasGiven(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.URL.Path+" "+string(raw))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(createdItem))
	}))
	defer server.Close()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "orchestrator-token"), []byte("synthetic-orchestrator"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"step-cancel", "--reason", "not needed", "item-1", "s1"},
		{"step-cancel", "item-1", "s1", "--reason", "not needed"},
	} {
		raw, _ := json.Marshal(args)
		cmd := exec.Command(os.Args[0], "-test.run=^TestItemStepCancelHelperProcess$")
		cmd.Env = append(os.Environ(), stepCancelHelper+"=1", stepCancelHelper+"_ARGS="+string(raw),
			"CLAWDLINE_NEXT_DIR="+state, "CLAWDLINE_NEXT_PORT="+strings.TrimPrefix(server.URL, "http://127.0.0.1:"),
			"CLAUDE_CODE_SESSION_ID="+thinConversation)
		out, err := cmd.CombinedOutput()
		if strings.Contains(string(out), "--reason") {
			t.Fatalf("%v was refused for its reason: %s (%v)", args, out, err)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("the daemon was asked %d times: %v", len(bodies), bodies)
	}
	for _, b := range bodies {
		if !strings.HasPrefix(b, "/v1/work/v2/agent/items/item-1/steps/s1/cancel ") || !strings.Contains(b, `"reason":"not needed"`) {
			t.Fatalf("request = %s", b)
		}
	}
}
