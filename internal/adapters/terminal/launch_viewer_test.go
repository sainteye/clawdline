package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingTmux is a tmux that writes each call's arguments as one line and
// fails `set-option` when refuse is set.
func recordingTmux(t *testing.T, refuse bool) (*Tmux, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	fail := "0"
	if refuse {
		fail = "1"
	}
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"if [ \"$1\" = set-option ] && [ " + fail + " = 1 ]; then echo 'no such session' >&2; exit 1; fi\nexit 0\n"
	bin := filepath.Join(dir, "tmux")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return &Tmux{Binary: bin}, calls
}

// The viewer's line attaches to exactly the one session, by the absolute tmux
// this daemon runs, and `exec` so the tab closes with the session; the only
// option set is set on that session.
func TestPrepareTmuxViewerSetsOnlyThatSessionAndAnswersAnExactAttach(t *testing.T) {
	tmux, calls := recordingTmux(t, false)
	line, err := Launcher{Tmux: tmux}.PrepareTmuxViewer(context.Background(), "clawdline-task-aaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if want := "exec '" + tmux.Binary + "' attach -t '=clawdline-task-aaaaaaaa'"; line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
	got, _ := os.ReadFile(calls)
	if want := "set-option -t =clawdline-task-aaaaaaaa: mouse on\n"; string(got) != want {
		t.Errorf("tmux was asked %q, want only %q", got, want)
	}
	if strings.Contains(string(got), "-g") {
		t.Error("a global option was set")
	}
}

// An option that would not set leaves the session as usable; the line is
// still answered.
func TestPrepareTmuxViewerKeepsTheLineWhenAnOptionIsRefused(t *testing.T) {
	tmux, _ := recordingTmux(t, true)
	line, err := Launcher{Tmux: tmux}.PrepareTmuxViewer(context.Background(), "s")
	if err != nil || !strings.HasSuffix(line, "attach -t '=s'") {
		t.Fatalf("line %q, err %v", line, err)
	}
}

// No session name or no tmux is no line: a tab running it would attach to
// whatever tmux picked.
func TestPrepareTmuxViewerRefusesWithoutASessionOrATmux(t *testing.T) {
	tmux, _ := recordingTmux(t, false)
	if _, err := (Launcher{Tmux: tmux}).PrepareTmuxViewer(context.Background(), ""); err == nil {
		t.Error("an empty session name was answered with a line")
	}
	none := Launcher{Tmux: &Tmux{Binary: "clawdline-no-such-tmux", fallbacks: []string{}}}
	if _, err := none.PrepareTmuxViewer(context.Background(), "s"); err == nil {
		t.Error("no tmux was answered with a line")
	}
}
