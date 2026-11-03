//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// This synthetic CLI exercises the real tty and tmux input path, without
// launching a provider or making a model request.
func TestNativeTerminalPTYHelper(t *testing.T) {
	receipt := os.Getenv("CLAWDLINE_NATIVE_PTY_RECEIPT")
	if receipt == "" {
		t.Skip("private tty helper")
	}
	fmt.Print("\x1b[2J\x1b[HSelect a model\r\n❯ Current model\r\n  Next model\r\n")
	if err := exec.Command(os.Getenv("CLAWDLINE_NATIVE_PTY_TMUX"), "-S", os.Getenv("CLAWDLINE_NATIVE_PTY_SOCKET"), "wait-for", "-S", "native-ready").Run(); err != nil {
		os.Exit(2)
	}
	var buffer [1024]byte
	for {
		n, err := os.Stdin.Read(buffer[:])
		if err != nil {
			os.Exit(0)
		}
		f, err := os.OpenFile(receipt, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(3)
		}
		_, _ = f.Write(buffer[:n])
		_ = f.Close()
		fmt.Printf("\r\nReceived %x\r\n", buffer[:n])
		if n == 1 && buffer[0] == 0x1b {
			os.Exit(0)
		}
	}
}

func TestNativeTerminalUsesTheExistingPTYAndStopsAfterTheCLIExits(t *testing.T) {
	bin := FindTmux(context.Background()).Path
	if bin == "" {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "cl-native-pty-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "claude")
	if err := os.WriteFile(helper, binary, 0700); err != nil {
		t.Fatal(err)
	}
	socket, receipt := filepath.Join(dir, "tmux.sock"), filepath.Join(dir, "received")
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("TMUX", "")
	t.Setenv("CLAWDLINE_NATIVE_PTY_RECEIPT", receipt)
	t.Setenv("CLAWDLINE_NATIVE_PTY_TMUX", bin)
	t.Setenv("CLAWDLINE_NATIVE_PTY_SOCKET", socket)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	command := "stty raw -echo; exec " + quote(helper) + " -test.run '^TestNativeTerminalPTYHelper$'"
	out, err := exec.Command(bin, "-S", socket, "-f", "/dev/null", "new-session", "-d", "-P", "-F", "#{pane_id}|#{pane_tty}|#{pane_pid}", command).CombinedOutput()
	if err != nil {
		t.Fatalf("private tmux: %s %v", out, err)
	}
	// Stop the server before the task-owned socket directory is removed.
	t.Cleanup(func() { _ = exec.Command(bin, "-S", socket, "kill-server").Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, bin, "-S", socket, "wait-for", "native-ready").Run(); err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(fields) != 3 {
		t.Fatal("missing pane identity")
	}
	pid, err := strconv.Atoi(fields[2])
	if err != nil {
		t.Fatal(err)
	}
	s := session.Session{ID: fields[0], TTY: fields[1], PID: pid, Backend: session.BackendTmux, Assistant: session.AssistantClaude}
	host := &Tmux{Binary: bin, Socket: socket}
	before, err := host.NativeTerminalIdentity(ctx, s)
	if err != nil || before == "" {
		t.Fatalf("foreground CLI: %s %v", before, err)
	}
	for _, input := range []string{"\x1b[B", "filter", "\t", "\r"} {
		if err := host.Keystroke(ctx, s, []byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	expected := "\x1b[Bfilter\t\r"
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, _ := os.ReadFile(receipt)
		if string(got) == expected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("received %q, want %q", got, expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := host.Keystroke(ctx, s, []byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		if _, err := host.NativeTerminalIdentity(ctx, s); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exited CLI still admitted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
