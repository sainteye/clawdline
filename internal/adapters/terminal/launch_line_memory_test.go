package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// A Project's memory reaches Claude Code as one quoted argument with
// newlines in it. Typed, the first newline would end the line inside the
// quote; so even a line short enough to type is scripted, and the assistant
// is handed the memory as exactly one argument, newlines and quotes intact.
func TestALaunchLineWithANewlineIsScriptedAndArrivesAsOneArgument(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	memory := "Index:\n- it's-a-name — a 'quoted' line\n"
	line := "claude " + strings.Join(projects.MemoryArgs(projects.AssistantClaude, memory), " ")
	if len(line) > MaxTypedLaunchBytes {
		t.Fatalf("the line is %d bytes; this test is about a short one", len(line))
	}
	typed, err := typedLaunchLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(typed, ". '") || strings.ContainsAny(typed, "\r\n") {
		t.Fatalf("a line with a newline was typed as %q", typed)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\npwd > \"$LAUNCH_OUT\"\nprintf '%s\\n' \"$#\" >> \"$LAUNCH_OUT\"\nprintf '%s' \"$2\" >> \"$LAUNCH_OUT\"\n"
	if err := os.WriteFile(filepath.Join(bin, projects.AssistantClaude), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, shell := range shells() {
		typed, err := typedLaunchLine(line)
		if err != nil {
			t.Fatal(err)
		}
		_, got, _ := strings.Cut(ranIn(t, shell, bin, typed), "\n")
		if got != "2\n"+memory {
			t.Fatalf("%s handed the assistant\n%q\nwant two arguments, the second %q", shell[0], got, memory)
		}
	}
}
