//go:build darwin

package terminal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A launch line longer than a tty line is not typed into the new tab whole:
// what is typed is short, and running it runs the same assistant, in the same
// directory, with the same arguments.
func TestANewITermTabIsNeverTypedMoreThanATTYLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", dir)
	line := personaLaunchLine(t)
	var typed []string
	was := itermOpenTab
	itermOpenTab = func(ctx context.Context, line string) ([]byte, string, error) {
		typed = append(typed, line)
		return []byte(`{"ok":true,"id":"w0t0p0:TEST"}`), "", nil
	}
	defer func() { itermOpenTab = was }()

	bin := fakeAssistant(t)
	for _, shell := range shells() {
		typed = nil
		id, err := (Launcher{}).NewITermTab(context.Background(), line)
		if err != nil || id != "w0t0p0:TEST" {
			t.Fatalf("NewITermTab = %q, %v", id, err)
		}
		if len(typed) != 1 {
			t.Fatalf("typed %d lines", len(typed))
		}
		if len(typed[0]) >= ttyLineBytes {
			t.Fatalf("a %d-byte line was typed into the tab whole (%d bytes typed); a tty line holds %d",
				len(line), len(typed[0]), ttyLineBytes)
		}
		if want, got := ranIn(t, shell, bin, line), ranIn(t, shell, bin, typed[0]); got != want {
			t.Fatalf("%s ran the typed line as\n%s\nand the launch line as\n%s", shell[0], got, want)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*", "*"))
	for _, f := range left {
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			t.Errorf("the line left %s behind after it ran", f)
		}
	}
}
