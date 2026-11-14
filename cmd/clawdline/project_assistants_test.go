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
