//go:build !windows

package http

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestTerminalSessionScreensUseTheDaemonsTerminalServer(t *testing.T) {
	bin := terminal.FindTmux(context.Background()).Path
	if bin == "" {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "clt-session-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "tmux", "term.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-S", sock, "-f", "/dev/null", "new-session", "-d", "-P", "-F", "#{pane_id}",
		"printf 'terminal-session-screen-marker\\n'; exec /bin/cat")
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("private Terminal: %s %v", out, err)
	}
	t.Cleanup(func() { _ = exec.Command(bin, "-S", sock, "kill-server").Run() })
	srv, err := New(config.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.screens.Stop(); _ = srv.store.Close() })
	row := session.Session{ID: "terminal:" + strings.TrimSpace(string(out)), Backend: session.BackendTmux}
	// The production constructor must wire the same fixed server into both
	// the live input reader and held screen reader, not just discovery.
	deadline := time.Now().Add(3 * time.Second)
	for {
		capture, readable := srv.inventory.Screen.Capture(context.Background(), row)
		if readable && strings.Contains(capture, "terminal-session-screen-marker") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Terminal screen unreadable: %q", capture)
		}
		time.Sleep(10 * time.Millisecond)
	}
	found := false
	for _, h := range srv.terminals {
		if h.Name() != "tmux" {
			continue
		}
		// Pin the ordinary server away from the person's tmux. The Terminal
		// connection installed by New must survive that independent choice.
		h.(*terminal.Tmux).Socket = filepath.Join(dir, "absent-main.sock")
		inv, err := h.Inventory(context.Background())
		if err != nil || !inv.Complete {
			t.Fatalf("inventory: %+v %v", inv, err)
		}
		for _, pane := range inv.Sessions {
			if pane.ID == row.ID {
				found = true
			}
		}
		if err := h.(ports.KeyHost).Keystroke(context.Background(), row, []byte("screen-route-check")); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("Terminal pane missing from the production Session inventory")
	}
}
