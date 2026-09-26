package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
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

// F6: the close takes the pane it has proof of, not the whole session. A
// window somebody added to the session — to look at why a child was slow, or
// moved in from elsewhere — is not the task's, and survives it. Runs a private
// tmux server.
func TestCloseTmuxSessionLeavesAWindowThatIsNotTheTasks(t *testing.T) {
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
	const name = "clawdline-task-bbbbbbbb"
	out, err := exec.Command(bin, "new-session", "-d", "-s", name, "-P", "-F", "#{pane_id}", "sleep", "60").Output()
	if err != nil {
		t.Skipf("could not start a private tmux server: %v", err)
	}
	defer exec.Command(bin, "kill-server").Run()
	ours := strings.TrimSpace(string(out))
	theirs, err := exec.Command(bin, "new-window", "-d", "-t", "="+name+":", "-P", "-F", "#{pane_id}", "sleep", "60").Output()
	if err != nil {
		t.Fatalf("could not add a window: %v", err)
	}
	if closed, err := NewLauncher().CloseTmuxSession(context.Background(), ours, name); !closed || err != nil {
		t.Fatalf("the proven pane closed=%v err=%v", closed, err)
	}
	panes, err := exec.Command(bin, "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		t.Fatalf("the window that was not the task's went with it: %v", err)
	}
	got := strings.Fields(string(panes))
	if len(got) != 1 || got[0] != strings.TrimSpace(string(theirs)) {
		t.Fatalf("panes left %v, want only %s", got, strings.TrimSpace(string(theirs)))
	}
}

// tmux 3.4 renders the \x01 separator as its octal escape, so a listing that
// is read literally loses every field on every line at once. That must not
// come back as "there are no panes": it is the answer D05 ③ reserves for
// evidence. Driven through a stand-in binary, because this Mac has 3.6a and
// cannot produce 3.4's spelling.
func TestAnEscapedSeparatorIsStillAListing(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines string
		want  int
		whole bool
	}{
		{"raw", "%1\x01/dev/ttys001\x01claude\x01s\x01title\x01/tmp\n", 1, true},
		{"escaped", `%1\001/dev/ttys001\001claude\001s\001title\001/tmp` + "\n", 1, true},
		{"unreadable", "%1 ttys001 claude s title /tmp\n", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := stubTmux(t, c.lines)
			inv, err := (&Tmux{Binary: bin}).Inventory(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(inv.Sessions) != c.want {
				t.Fatalf("got %d panes, want %d: %+v", len(inv.Sessions), c.want, inv.Sessions)
			}
			if inv.Complete != c.whole {
				t.Fatalf("Complete=%v, want %v (notes %v)", inv.Complete, c.whole, inv.Notes)
			}
			if !c.whole && len(inv.Notes) == 0 {
				t.Fatal("an incomplete listing has to say why")
			}
		})
	}
}

// A tmux server with no panes still answers, and that empty answer is
// complete. The drop counter above must not read it as damage.
func TestAnEmptyListingIsStillComplete(t *testing.T) {
	inv, err := (&Tmux{Binary: stubTmux(t, "")}).Inventory(context.Background())
	if err != nil || !inv.Complete || len(inv.Sessions) != 0 || len(inv.Notes) != 0 {
		t.Fatalf("err=%v inv=%+v", err, inv)
	}
}

// A daemon opened from Finder inherits launchd's small PATH. Homebrew's tmux
// may still have a live server, so finding the binary outside that PATH proves
// the opposite of an authoritative empty list: this source was not read.
func TestATmuxOutsideTheDaemonPATHIsAnIncompleteInventory(t *testing.T) {
	found := stubTmux(t, "%1\x01/dev/ttys001\x01claude\x01s\x01title\x01/tmp\n")
	t.Setenv("PATH", t.TempDir())
	tmux := &Tmux{Binary: "tmux", fallbacks: []string{found}}

	inv, err := tmux.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Complete || len(inv.Sessions) != 0 {
		t.Fatalf("tmux outside PATH read as an authoritative listing: %+v", inv)
	}
	if len(inv.Notes) != 1 || !strings.Contains(inv.Notes[0], found) ||
		!strings.Contains(inv.Notes[0], "not on this daemon's PATH") {
		t.Fatalf("the reading did not say where tmux was: %v", inv.Notes)
	}
}

// stubTmux writes an executable that prints out and exits 0, whatever it is
// asked. It stands in for a tmux whose version this machine does not have.
func stubTmux(t *testing.T, out string) string {
	t.Helper()
	dir := t.TempDir()
	// The bytes go in a file and the script cats it: a separator written
	// into the script would have to survive the shell's own escaping, and
	// \x01 does not.
	data := filepath.Join(dir, "out")
	if err := os.WriteFile(data, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat "+data+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// A stop is one Escape, the key both assistants name on their working line,
// and not C-c: a second C-c at an idle prompt quits Claude Code and Codex, so
// a stop pressed twice because the first seemed slow closed the session.
func TestInterruptTypesOneEscapeAndNotControlC(t *testing.T) {
	dir := t.TempDir()
	said := filepath.Join(dir, "args")
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+said+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (&Tmux{Binary: path}).Interrupt(context.Background(), session.Session{ID: "%19"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(said)
	if err != nil {
		t.Fatal(err)
	}
	if want := "send-keys\n-t\n%19\n-H\n1b\n"; string(got) != want {
		t.Fatalf("tmux was asked %q, want %q", got, want)
	}
}
