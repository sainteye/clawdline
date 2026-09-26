package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shellFixture is a Claude Code session folder as it is on disk: a transcript
// that announced two background commands, and the tasks folder beside
// /tmp/claude-<uid>/<project>/<session>/ with their output files.
func shellFixture(t *testing.T) (s *Shells, transcript, tasks string) {
	t.Helper()
	root := t.TempDir()
	transcript = filepath.Join(root, "projects", "-p", "sess.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, row := range []any{
		claudeRow("assistant", []m{{"type": "tool_use", "id": "call1", "name": "Bash",
			"input": m{"command": "npm run build", "description": "Build it", "run_in_background": true}}}),
		claudeRow("user", []m{{"type": "tool_result", "tool_use_id": "call1",
			"content": "Command running in background with ID: b0run1. Output is being written to: /tmp/x"}}),
		claudeRow("assistant", []m{{"type": "tool_use", "id": "call2", "name": "Bash",
			"input": m{"command": "make", "run_in_background": true}}}),
		claudeRow("user", []m{{"type": "tool_result", "tool_use_id": "call2",
			"content": "Command running in background with ID: b0done1"}}),
	} {
		line, _ := jsonLine(row)
		b.WriteString(line)
	}
	if err := os.WriteFile(transcript, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	s = NewShells()
	s.Root = filepath.Join(root, "tmp")
	tasks = s.Folder(transcript)
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"b0run1.output":     "compiling\nvite v7 building\n",
		"b0done1.output":    "built\n[exited with code 0]\n",
		"b0foreign.output":  "a foreground command's leftover\n",
		"secretfile.output": "",
	} {
		if err := os.WriteFile(filepath.Join(tasks, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s, transcript, tasks
}

// The Shell panel reads an announced command whether it is still going or has
// ended, and nothing else.
func TestShellOutputIsServedOnlyForAnnouncedCommands(t *testing.T) {
	s, transcript, tasks := shellFixture(t)

	running, ok := s.Output(transcript, "b0run1", ShellOutputDefault)
	if !ok || running.Ended || running.Text != "compiling\nvite v7 building\n" || running.Truncated {
		t.Fatalf("running: ok=%v %+v", ok, running)
	}
	if running.Shell.Command != "npm run build" || running.Shell.What != "Build it" || running.Shell.Doing != "vite v7 building" {
		t.Fatalf("running row: %+v", running.Shell)
	}
	if running.Signature == "" {
		t.Fatal("no signature")
	}

	ended, ok := s.Output(transcript, "b0done1", ShellOutputDefault)
	if !ok || !ended.Ended || ended.Shell.Doing != "" || ended.Shell.Command != "make" {
		t.Fatalf("an ended command is still readable, and says it ended: ok=%v %+v", ok, ended)
	}

	// The file moves, and so does the signature.
	if err := os.WriteFile(filepath.Join(tasks, "b0run1.output"), []byte("compiling\nvite v7 building\ndone in 2s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	grown, _ := s.Output(transcript, "b0run1", ShellOutputDefault)
	if grown.Signature == running.Signature || !strings.HasSuffix(grown.Text, "done in 2s\n") {
		t.Fatalf("the output grew and the answer did not: %+v", grown)
	}

	for _, id := range []string{
		"b0foreign",         // a file with no announcement: a foreground leftover
		"secretfile",        // same
		"b0never",           // announced nowhere, no file
		"../outside",        // escapes the folder
		"..",                // the folder's parent
		"b0run1/../b0done1", // a separator in the id
		"b0run1.output",     // the suffix is the reader's, not the caller's
		"",                  // nothing
		strings.Repeat("a", 65),
	} {
		if got, ok := s.Output(transcript, id, ShellOutputDefault); ok {
			t.Errorf("%q was served: %+v", id, got)
		}
	}

	// An announced command whose file is gone has nothing to show.
	if err := os.Remove(filepath.Join(tasks, "b0done1.output")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Output(transcript, "b0done1", ShellOutputDefault); ok {
		t.Error("an announced command with no output file was served")
	}
}

// The window is bounded both ways: raised to 1 KiB, lowered to 1 MiB, and the
// cut lands on a line boundary and says so.
func TestShellOutputWindowIsBounded(t *testing.T) {
	s, transcript, tasks := shellFixture(t)
	line := strings.Repeat("x", 99) + "\n" // 100 bytes
	big := strings.Repeat(line, (MaxShellOutput/100)+500)
	if err := os.WriteFile(filepath.Join(tasks, "b0run1.output"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ask  int64
		most int
	}{
		{ask: 1, most: ShellOutputFloor},
		{ask: 0, most: ShellOutputFloor},
		{ask: 4096, most: 4096},
		{ask: 1 << 30, most: MaxShellOutput},
	} {
		out, ok := s.Output(transcript, "b0run1", c.ask)
		if !ok {
			t.Fatalf("ask %d: not served", c.ask)
		}
		if len(out.Text) > c.most || len(out.Text) < c.most-100 {
			t.Errorf("ask %d: %d bytes, want at most %d and within a line of it", c.ask, len(out.Text), c.most)
		}
		if !out.Truncated || !strings.HasPrefix(out.Text, "x") {
			t.Errorf("ask %d: truncated=%v, starts %q", c.ask, out.Truncated, out.Text[:min(10, len(out.Text))])
		}
	}
}
