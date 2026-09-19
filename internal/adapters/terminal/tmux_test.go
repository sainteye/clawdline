package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// Only tmux's own "no server" sentence is an empty listing. A socket
// directory with the wrong permissions exits 1 exactly as an absent server
// does, and reading that as "there are no panes" is the one answer that lets
// the broker call a live child's tab gone (D05 ③). Both sentences are tmux
// 3.6a's own, captured on this Mac.
func TestOnlyNoServerIsAnEmptyListing(t *testing.T) {
	for _, c := range []struct {
		said string
		want bool
	}{
		{"no server running on /private/tmp/tmux-501/default", true},
		{"error connecting to /private/tmp/x/tmux-501/default (No such file or directory)", true},
		{"directory /private/tmp/x/tmux-501 has unsafe permissions", false},
		{"error connecting to /private/tmp/tmux-501/default (Permission denied)", false},
		{"server exited unexpectedly", false},
		{"", false},
	} {
		if got := NoServer(c.said); got != c.want {
			t.Errorf("NoServer(%q) = %v, want %v", c.said, got, c.want)
		}
	}
}

// A listing that fails for any reason but "no server" is incomplete, and says
// why. Runs tmux against a socket directory it refuses.
func TestAFailedListingIsIncomplete(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-tmux-bad-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(sock, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sock, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_PANE")
	t.Setenv("TMUX_TMPDIR", dir)
	inv, err := NewTmux().Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Complete || len(inv.Notes) == 0 {
		t.Fatalf("a listing tmux refused read as %+v", inv)
	}
}

// CloseTmuxSession closes a session only when the pane it is given is one of
// that session's panes, and by the session's id. Runs a private tmux server.
func TestCloseTmuxSessionClosesOnlyWhatItsPaneProves(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clawdline-tmux-close-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX", "")
	os.Unsetenv("TMUX")
	os.Unsetenv("TMUX_PANE")
	t.Setenv("TMUX_TMPDIR", dir)
	open := func(name string) string {
		out, err := exec.Command(bin, "new-session", "-d", "-s", name, "-P", "-F", "#{pane_id}", "sleep", "60").Output()
		if err != nil {
			t.Skipf("could not start a private tmux server: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	defer exec.Command(bin, "kill-server").Run()
	ours := open("clawdline-task-aaaaaaaa")
	// Another session whose name starts with ours: a close by name would
	// reach it by prefix; the close by id cannot.
	other := open("clawdline-task-aaaaaaaa-other")
	alive := func(name string) bool {
		return exec.Command(bin, "has-session", "-t", "="+name).Run() == nil
	}
	l := NewLauncher()
	ctx := context.Background()

	if closed, err := l.CloseTmuxSession(ctx, other, "clawdline-task-aaaaaaaa"); closed || err != nil {
		t.Fatalf("a pane from another session closed=%v err=%v", closed, err)
	}
	if closed, err := l.CloseTmuxSession(ctx, "%99", "clawdline-task-aaaaaaaa"); closed || err != nil {
		t.Fatalf("a pane that is gone closed=%v err=%v", closed, err)
	}
	if !alive("clawdline-task-aaaaaaaa") || !alive("clawdline-task-aaaaaaaa-other") {
		t.Fatal("a refused close closed something")
	}
	if closed, err := l.CloseTmuxSession(ctx, ours, "clawdline-task-aaaaaaaa"); !closed || err != nil {
		t.Fatalf("the proven session closed=%v err=%v", closed, err)
	}
	if alive("clawdline-task-aaaaaaaa") {
		t.Fatal("the proven session is still there")
	}
	if !alive("clawdline-task-aaaaaaaa-other") {
		t.Fatal("closing ours took the session that shares its prefix")
	}
}
