package main

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeSetupWithoutClaudeCodeChangesNothing(t *testing.T) {
	state := t.TempDir()
	var out, err bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), []string{"setup-claude", "--cli", "/elsewhere"}, state, &out, &err); code != 2 {
		t.Fatalf("paths are no longer accepted: code=%d", code)
	}
	want := "cli_not_installed"
	if runtime.GOOS != "darwin" {
		want = "pa_unsupported"
	}
	if code := projectAssistantsCommand(context.Background(), []string{"setup-claude"}, state, &out, &err); code != 1 || out.Len() != 0 || !strings.Contains(err.String(), want) {
		t.Fatalf("setup without tools: code=%d output=%q error=%q", code, out.String(), err.String())
	}
	entries, e := os.ReadDir(state)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("refused setup changed private state")
	}
}

func TestTheBotsSwitchReportsTheDaemonsState(t *testing.T) {
	var out, errOut bytes.Buffer
	var sent any
	call := func(method string, body any) (int, []byte, error) {
		sent = body
		if method == "GET" {
			return 200, []byte(`{"enabled":false}`), nil
		}
		return 200, []byte(`{"enabled":true}`), nil
	}
	if code := projectAssistantsSwitch("enable", &out, &errOut, call); code != 0 || !strings.Contains(out.String(), "Bots are on") {
		t.Fatalf("enable: %d %q %q", code, out.String(), errOut.String())
	}
	if m, ok := sent.(map[string]bool); !ok || !m["enabled"] {
		t.Fatalf("enable sent %v", sent)
	}
	out.Reset()
	if code := projectAssistantsSwitch("status", &out, &errOut, call); code != 0 || !strings.Contains(out.String(), "Bots are off") || sent != nil {
		t.Fatalf("status: %d %q %v", code, out.String(), sent)
	}
	refused := func(string, any) (int, []byte, error) { return 403, []byte(`{"error":"forbidden"}`), nil }
	if code := projectAssistantsSwitch("disable", &out, &errOut, refused); code != 1 {
		t.Fatalf("a refused switch reported success: %d", code)
	}
}
