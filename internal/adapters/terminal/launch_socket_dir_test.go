package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// firstInstallSocket is the carried tmux's socket on a machine where nothing
// has run yet: `<CLAWDLINE_NEXT_DIR>/tmux/sessions.sock`, whose directory does
// not exist. tmux 3.6a asked to start a server there exits 0, prints nothing
// on stdout and says "error creating …" on stderr (measured 2026-11-14), which
// a first session start reported as "tmux returned no new pane id."
func firstInstallSocket(t *testing.T) (bin, sock string) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "cl-tmux-")
	if err != nil {
		t.Fatal(err)
	}
	sock = filepath.Join(dir, "next", "tmux", SessionsSocketName)
	t.Cleanup(func() {
		_ = exec.Command(bin, "-S", sock, "kill-server").Run()
		_ = os.RemoveAll(dir)
	})
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("SHELL", "/bin/sh")
	return bin, sock
}

func TestTheFirstSessionStartsWhenTheSocketDirectoryIsMissing(t *testing.T) {
	bin, sock := firstInstallSocket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	launcher := Launcher{Tmux: &Tmux{Binary: bin, Socket: sock}}
	id, err := launcher.NewTmuxSession(ctx, t.TempDir(), "clawdline", "true")
	if err != nil {
		t.Fatalf("first session: %v", err)
	}
	if !strings.HasPrefix(id, "%") {
		t.Fatalf("pane id = %q", id)
	}
	info, err := os.Stat(filepath.Dir(sock))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("socket directory mode = %o, want 700", mode)
	}
}

func TestADispatchedSessionStartsWhenTheSocketDirectoryIsMissing(t *testing.T) {
	bin, sock := firstInstallSocket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := (&Tmux{Binary: bin, Socket: sock}).Open(ctx, ports.OpenRequest{Name: "task", Command: "sleep 30"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.HasPrefix(s.ID, "%") {
		t.Fatalf("pane id = %q", s.ID)
	}
}

// tmux exiting 0 without a pane id is still a failure, and it carries what
// tmux said on stderr; a hook that prints after the id does not become part
// of it.
func TestNewPaneIDReadsTheIDAndKeepsWhatTmuxSaid(t *testing.T) {
	cases := []struct {
		out, said, id, failure string
	}{
		{out: "%3\n", id: "%3"},
		{out: "%0\nhookout\n", id: "%0"},
		{out: "", said: "error creating /x/tmux/sessions.sock (No such file or directory)",
			failure: "tmux returned no new pane id: error creating /x/tmux/sessions.sock (No such file or directory)"},
		{out: "", failure: "tmux returned no new pane id."},
	}
	for _, c := range cases {
		id, err := newPaneID(c.out, c.said)
		if c.failure == "" {
			if err != nil || id != c.id {
				t.Errorf("newPaneID(%q) = %q, %v; want %q", c.out, id, err, c.id)
			}
			continue
		}
		if err == nil || err.Error() != c.failure {
			t.Errorf("newPaneID(%q, %q) error = %v; want %q", c.out, c.said, err, c.failure)
		}
	}
}
