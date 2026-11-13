package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSetupDoesNotEnableOrStartAnUnqualifiedInstallation(t *testing.T) {
	state := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "not-a-provider")
	if e := os.WriteFile(path, []byte("not executable or a qualified artifact"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, err bytes.Buffer
	if code := projectAssistantsCommand(context.Background(), []string{"setup-claude", "--node", path, "--adapter", path, "--cli", path}, state, &out, &err); code != 1 || out.Len() != 0 {
		t.Fatalf("unqualified setup: code=%d output=%q", code, out.String())
	}
	entries, e := os.ReadDir(state)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("refused setup changed private state")
	}
}
