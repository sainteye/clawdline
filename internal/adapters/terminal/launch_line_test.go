package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/persona"
)

// ttyLineBytes is what a tty in canonical mode holds of one line on macOS
// (MAX_CANON). Measured on 2026-09-30: a codex launch with a persona typed
// into a new iTerm2 tab arrived as its first 1024 bytes, cut off mid-argument,
// and the same 1610-byte line pasted into a fresh tmux pane showed 1024 bytes
// and ran nothing.
const ttyLineBytes = 1024

// fakeAssistant is a codex on a PATH of its own that writes where it ran and
// every argument it was given, one per line, to $LAUNCH_OUT.
func fakeAssistant(t *testing.T) (bin string) {
	t.Helper()
	bin = t.TempDir()
	script := "#!/bin/sh\npwd > \"$LAUNCH_OUT\"\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> \"$LAUNCH_OUT\"; done\n"
	if err := os.WriteFile(filepath.Join(bin, projects.AssistantCodex), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// personaLaunchLine is a codex launch with a persona, built as Starter.Start
// builds one (projects.Launch, PersonaArgs), in a project whose path has a
// space and a quote in it: longer than a tty line, and with every kind of
// quoting the line carries.
func personaLaunchLine(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "a project's "+strings.Repeat("deep/", 120)+"root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := persona.Persona{ID: "frontend", Name: persona.Names{En: "Frontend Developer"}}
	path := filepath.Join(t.TempDir(), "personas", "frontend.md")
	args := append(projects.UpdateCheckArgs(projects.AssistantCodex),
		projects.PersonaArgs(projects.AssistantCodex, p, path)...)
	line := projects.Launch{ProjectRoot: root, Assistant: projects.AssistantCodex, Arguments: args}.ShellLine()
	if len(line) <= ttyLineBytes {
		t.Fatalf("the launch line is %d bytes, not longer than a tty line", len(line))
	}
	return line
}

// shells are the shells a typed line is run in: sh, and zsh — what a new tab
// on a Mac runs — where there is one.
func shells() [][]string {
	out := [][]string{{"/bin/sh"}}
	if _, err := os.Stat("/bin/zsh"); err == nil {
		out = append(out, []string{"/bin/zsh", "-f"})
	}
	return out
}

// ranIn runs line in shell, as the tab's shell would run it, and answers what
// the fake assistant saw: its working directory and its arguments.
func ranIn(t *testing.T, shell []string, bin, line string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	cmd := exec.Command(shell[0], append(shell[1:], "-c", line)...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "LAUNCH_OUT="+out)
	if said, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s did not run the line: %v %s", shell[0], err, said)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// A short line is typed as it is and writes nothing.
func TestAShortLaunchLineIsTypedAsItIs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	line := "cd '/tmp' && claude"
	if typed, err := typedLaunchLine(line); err != nil || typed != line {
		t.Fatalf("typedLaunchLine = %q, %v", typed, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a short line wrote %v", entries)
	}
}

// A long line — the tmux route takes the same one — is typed short, its script
// is readable by nobody else, and running the short line runs the long one.
func TestALongLaunchLineIsTypedAsTheScriptThatHoldsIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	line := personaLaunchLine(t)
	bin := fakeAssistant(t)
	for _, shell := range shells() {
		typed, err := typedLaunchLine(line)
		if err != nil {
			t.Fatal(err)
		}
		if len(typed) > MaxTypedLaunchBytes || !strings.HasPrefix(typed, ". '") {
			t.Fatalf("typed %d bytes: %q", len(typed), typed)
		}
		scripts, _ := filepath.Glob(filepath.Join(launchScriptDir(), "launch-*.sh"))
		if len(scripts) != 1 {
			t.Fatalf("scripts %v", scripts)
		}
		for path, want := range map[string]os.FileMode{launchScriptDir(): 0o700, scripts[0]: 0o600} {
			if st, err := os.Stat(path); err != nil || st.Mode().Perm() != want {
				t.Errorf("%s: %v %v, want %v", path, st.Mode().Perm(), err, want)
			}
		}
		if want, got := ranIn(t, shell, bin, line), ranIn(t, shell, bin, typed); got != want {
			t.Fatalf("%s ran the typed line as\n%s\nand the launch line as\n%s", shell[0], got, want)
		}
		if _, err := os.Stat(scripts[0]); !os.IsNotExist(err) {
			t.Fatalf("the script is still there after %s ran it: %v", shell[0], err)
		}
	}
}

// Scripts nobody ran are bounded: the oldest go first.
func TestLaunchScriptsNobodyRanAreBounded(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	line := personaLaunchLine(t)
	for i := 0; i < MaxLaunchScripts+5; i++ {
		if _, err := typedLaunchLine(line); err != nil {
			t.Fatal(err)
		}
	}
	scripts, _ := filepath.Glob(filepath.Join(launchScriptDir(), "launch-*.sh"))
	if len(scripts) != MaxLaunchScripts {
		t.Fatalf("%d scripts kept, want %d", len(scripts), MaxLaunchScripts)
	}
}
