package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The inventory runs under LC_ALL=C and splits on \x01. tmux rewrites control
// characters in format output for a non-UTF-8 client, so without `-u` every
// pane was silently dropped. Runs against a private tmux server.
func TestInventoryListsPanesUnderTheCLocale(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-tmux-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_PANE")
	t.Setenv("TMUX_TMPDIR", dir)
	start := exec.Command(bin, "new-session", "-d", "-s", "t", "-c", dir, "sleep", "30")
	if out, err := start.CombinedOutput(); err != nil {
		t.Skipf("could not start a private tmux server: %v %s", err, out)
	}
	defer exec.Command(bin, "kill-server").Run()

	inv, err := NewTmux().Inventory(context.Background())
	if err != nil || !inv.Complete {
		t.Fatalf("err=%v inv=%+v", err, inv)
	}
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID == "" || inv.Sessions[0].TTY == "" {
		t.Fatalf("want the one pane, got %+v", inv.Sessions)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if got := inv.Sessions[0].CWD; got != dir && got != resolved {
		t.Fatalf("cwd %q, want %q", got, dir)
	}
}
