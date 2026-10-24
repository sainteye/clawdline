//go:build darwin || linux

package terminal

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func TestAntigravityMetadataOutputRefusesOverflow(t *testing.T) {
	output := &metadataOutput{}
	// Hide WriterTo so io.Copy exercises any accidentally promoted ReaderFrom.
	reader := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", antigravity.CLIProbeOutputLimit+1))}
	n, err := io.Copy(output, reader)
	if err == nil || n > antigravity.CLIProbeOutputLimit {
		t.Fatal("metadata overflow was accepted")
	}
}

type terminalNativePort struct {
	reader *antigravity.NativeProcessReader
}

func (r terminalNativePort) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	p, err := r.reader.ReadAntigravityProcess(ctx, pid)
	if err == nil {
		p.TTY = strings.TrimPrefix(p.TTY, "/dev/")
	}
	return p, err
}

func TestAntigravityPrivateTmuxKernelIdentityAndForeground(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("LC_ALL", "C")
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	root, err := os.MkdirTemp("/private/tmp", "ag-pane-")
	if err != nil {
		root, err = os.MkdirTemp("/tmp", "ag-pane-")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "t.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-S", socket}, args...)...)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "TMUX=") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("private tmux %s: %v %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	command("new-session", "-d", "-s", "synthetic", "/bin/sleep 30")
	defer func() {
		cmd := exec.Command(binary, "-S", socket, "kill-server")
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		_ = cmd.Run()
	}()
	meta := strings.Split(command("display-message", "-p", "#{pane_id}|#{pane_tty}|#{pane_pid}"), "|")
	if len(meta) != 3 {
		t.Fatal("bad synthetic pane")
	}
	pid, err := strconv.Atoi(meta[2])
	if err != nil {
		t.Fatal(err)
	}
	tmux := &Tmux{Binary: binary, Socket: socket}
	reader := antigravity.NewNativeProcessReader(tmux.ExactAntigravityTerminal)
	p, err := reader.ReadAntigravityProcess(ctx, pid)
	expectedExecutable, pathErr := filepath.EvalSymlinks("/bin/sleep")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	for ctx.Err() == nil && (err != nil || p.Executable != expectedExecutable) {
		time.Sleep(20 * time.Millisecond)
		p, err = reader.ReadAntigravityProcess(ctx, pid)
	}
	if err != nil || p.TerminalID != meta[0] || p.TTY != meta[1] {
		t.Fatalf("kernel mapping: %+v %v", p, err)
	}
	if err = antigravity.VerifyNativeForeground(ctx, p); err != nil {
		t.Fatalf("foreground refused: %v", err)
	}
	wrong := p
	wrong.KernelGeneration = "stale"
	if antigravity.VerifyNativeForeground(ctx, wrong) == nil {
		t.Fatal("stale generation accepted")
	}
	bridge, err := antigravity.NewBridge(root, filepath.Join(root, "state.json"), p.Executable, terminalNativePort{reader}, func(string, string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	got, err := tmux.AntigravityPaneProcess(ctx, meta[0], strings.TrimPrefix(meta[1], "/dev/"), bridge)
	if err != nil || got.PID != pid || got.TTY != strings.TrimPrefix(p.TTY, "/dev/") || got.TerminalID != p.TerminalID {
		t.Fatalf("pane callback mismatch: %+v %v", got, err)
	}
	if _, err = tmux.AntigravityPaneProcess(ctx, "%foreign", got.TTY, bridge); err == nil {
		t.Fatal("foreign pane accepted")
	}
	proveCandidates := func(total, foreground int) []ports.AntigravityProcess {
		t.Helper()
		got.TTY = strings.TrimPrefix(command("display-message", "-p", "-t", meta[0], "#{pane_tty}"), "/dev/")
		for ctx.Err() == nil {
			cmd := exec.CommandContext(ctx, "/bin/ps", "-t", got.TTY, "-o", "pid=")
			data, err := cmd.Output()
			var valid []ports.AntigravityProcess
			front := 0
			if err == nil {
				for _, field := range strings.Fields(string(data)) {
					candidate, e := strconv.Atoi(field)
					if e != nil {
						t.Fatal(e)
					}
					proof, e := bridge.VerifyAntigravityProcess(ctx, candidate)
					if e != nil {
						continue
					}
					valid = append(valid, proof)
					raw := proof
					raw.TTY = "/dev/" + raw.TTY
					if antigravity.VerifyNativeForeground(ctx, raw) == nil {
						front++
					}
				}
			}
			if len(valid) == total && front == foreground {
				t.Logf("kernel candidates=%d foreground=%d", total, foreground)
				return valid
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("synthetic process topology never became ready")
		return nil
	}
	// The shell wrapper is the pane root while its child is the provider.
	command("respawn-pane", "-k", "-t", meta[0], "/bin/sh -c '/bin/sleep 30 & wait'")
	children := proveCandidates(1, 1)
	mapped, err := tmux.AntigravityPaneProcess(ctx, meta[0], got.TTY, bridge)
	if err != nil || mapped != children[0] {
		t.Fatalf("helper ancestry: %+v %v", mapped, err)
	}
	// Two ordinary sleep children share the foreground group. Neither the
	// group leader nor the first matching executable is sufficient proof.
	command("respawn-pane", "-k", "-t", meta[0], "/bin/sh -c '/bin/sleep 30 & /bin/sleep 30 & wait'")
	proveCandidates(2, 2)
	if _, err = tmux.AntigravityPaneProcess(ctx, meta[0], got.TTY, bridge); err == nil {
		t.Fatal("ambiguous foreground providers accepted")
	}
	// Job control gives the child a background group; the wrapper stays foreground.
	command("respawn-pane", "-k", "-t", meta[0], "/bin/sh -c 'set -m; /bin/sleep 30 & wait'")
	proveCandidates(1, 0)
	if _, err = tmux.AntigravityPaneProcess(ctx, meta[0], got.TTY, bridge); err == nil {
		t.Fatal("background provider accepted")
	}
	// The same PID and terminal survive exec; the kernel generation must not.
	command("respawn-pane", "-k", "-t", meta[0], "read answer; exec /bin/sleep 30")
	beforePID, err := strconv.Atoi(command("display-message", "-p", "-t", meta[0], "#{pane_pid}"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := reader.ReadAntigravityProcess(ctx, beforePID)
	if err != nil {
		t.Fatal(err)
	}
	command("send-keys", "-t", meta[0], "continue", "Enter")
	for ctx.Err() == nil {
		after, e := reader.ReadAntigravityProcess(ctx, beforePID)
		if e == nil && after.Executable == p.Executable {
			if after.KernelGeneration == before.KernelGeneration {
				t.Fatal("exec retained generation")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatal("synthetic exec not observed")
	}
	if antigravity.VerifyNativeForeground(ctx, before) == nil {
		t.Fatal("pre-exec identity accepted")
	}
	command("kill-pane", "-t", meta[0])
	if antigravity.VerifyNativeForeground(ctx, p) == nil {
		t.Fatal("exited process accepted")
	}
}
