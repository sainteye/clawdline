package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutsideTmuxRemovesAssistantIdentity(t *testing.T) {
	env := []string{
		"HOME=/tmp", "TMUX=socket", "TMUX_PANE=%1", "CLAUDE_CODE_SESSION_ID=claude",
		"CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CODEX_THREAD_ID=thread",
		"CODEX_SESSION_ID=session", "CODEX_CI=1", "PATH=/bin",
	}
	got := outsideTmux(env)
	want := []string{"HOME=/tmp", "CODEX_CI=1", "PATH=/bin"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("outsideTmux = %q, want %q", got, want)
	}
}

func TestNewTmuxPaneDoesNotInheritAssistantIdentityFromServer(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "cl-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	for _, key := range []string{"CLAUDE_CODE_SESSION_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "CODEX_SESSION_ID"} {
		t.Setenv(key, "leaked")
	}
	defer exec.Command(bin, "kill-server").Run()
	ctx := context.Background()
	if _, err := runTmux(ctx, bin, "new-session", "-d", "-s", "seed", "sleep 60"); err != nil {
		t.Skipf("could not start private tmux: %v", err)
	}
	initial, err := runTmux(ctx, bin, "show-environment", "-g")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(initial, "\n") {
		key, _, _ := strings.Cut(line, "=")
		if assistantIdentityKey(key) {
			t.Errorf("new server inherited %s", key)
		}
	}
	// A server may have been started by an older daemon or an assistant.
	for _, key := range []string{"CLAUDE_CODE_SESSION_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "CODEX_SESSION_ID"} {
		if out, err := exec.Command(bin, "set-environment", "-g", key, "leaked").CombinedOutput(); err != nil {
			t.Fatalf("seed %s: %v: %s", key, err, out)
		}
	}
	if err := clearTmuxAssistantIdentity(ctx, bin); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "pane-env")
	if _, err := runTmux(ctx, bin, "new-window", "-d", "-t", "seed:", "env > "+output); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for attempt := 0; attempt < 50; attempt++ {
		data, err = os.ReadFile(output)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, _, _ := strings.Cut(line, "=")
		if assistantIdentityKey(key) {
			t.Errorf("pane inherited %s", key)
		}
	}
}
