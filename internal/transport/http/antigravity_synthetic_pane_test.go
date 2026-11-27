package http

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	antigravitySyntheticCancel = "synthetic-cancel"
	antigravitySyntheticStage  = "synthetic-stage"
)

type antigravitySyntheticPane struct {
	id   string
	root string
	ctx  context.Context
}

func TestManagedAntigravityWaitReportsProviderExit(t *testing.T) {
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	_, dir := runtimeServer(t)
	root := filepath.Dir(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := "printf '%s' 'waiting for managed-continue' > " + antigravitySyntheticStage + "; exit 23"
	pane := startAntigravitySyntheticPane(t, ctx, root, binary, filepath.Join(root, "exit.sock"), "synthetic", root, command)
	_, err = waitAntigravitySyntheticFile(pane.ctx, filepath.Join(root, "hook-result"))
	const want = "synthetic provider exited with code 23 while waiting for managed-continue"
	if err == nil || err.Error() != want {
		t.Fatalf("wait for hook-result: got %v, want %q", err, want)
	}
}

func startAntigravitySyntheticPane(t *testing.T, parent context.Context, root, binary, socket, session, cwd, command string) antigravitySyntheticPane {
	t.Helper()
	run := func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, binary, append([]string{"-S", socket}, args...)...)
		cmd.Env = append(os.Environ(), "TMUX=", "TMUX_PANE=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("private tmux %s: %w: %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out)), nil
	}

	// Keep the status of even a provider that exits before the first wait starts.
	config := filepath.Join(root, "synthetic-tmux.conf")
	if err := os.WriteFile(config, []byte("set -g remain-on-exit on\nset-hook -g pane-died 'wait-for -S synthetic-pane-exited'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pane, err := run(parent, "-f", config, "new-session", "-d", "-P", "-F", "#{pane_id}", "-s", session, "-c", cwd, command)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(parent)
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		for {
			if _, err := run(ctx, "wait-for", "synthetic-pane-exited"); err != nil {
				cancel(err)
				return
			}
			status, err := run(ctx, "display-message", "-p", "-t", pane, "#{pane_dead}|#{pane_dead_status}|#{pane_dead_signal}")
			if err != nil {
				cancel(err)
				return
			}
			dead, exit, _ := strings.Cut(status, "|")
			if dead != "1" {
				continue // The bootstrap fixture can open a separate pane.
			}
			code, signal, _ := strings.Cut(exit, "|")
			stage, err := os.ReadFile(filepath.Join(root, antigravitySyntheticStage))
			if err != nil {
				stage = []byte("starting the provider")
			}
			if signal != "" {
				cancel(fmt.Errorf("synthetic provider exited on signal %s while %s", signal, stage))
			} else {
				cancel(fmt.Errorf("synthetic provider exited with code %s while %s", code, stage))
			}
			return
		}
	}()
	cancelPublished := make(chan struct{})
	go func() {
		defer close(cancelPublished)
		<-ctx.Done()
		// The provider observes the same cancellation even while running a hook.
		if err := os.WriteFile(filepath.Join(root, antigravitySyntheticCancel), nil, 0600); err != nil {
			t.Errorf("cancel synthetic provider: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel(context.Canceled)
		<-cancelPublished
		<-observed
		if _, err := run(context.Background(), "kill-server"); err != nil {
			t.Error(err)
		}
	})
	return antigravitySyntheticPane{id: pane, root: root, ctx: ctx}
}

func (p antigravitySyntheticPane) wait(t *testing.T, name string) []byte {
	t.Helper()
	value, err := waitAntigravitySyntheticFile(p.ctx, filepath.Join(p.root, name))
	if err != nil {
		t.Fatalf("waiting for %s: %v", name, err)
	}
	return value
}

func waitAntigravitySyntheticFile(ctx context.Context, path string) ([]byte, error) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		value, err := os.ReadFile(path)
		if !errors.Is(err, os.ErrNotExist) {
			return value, err
		}
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-tick.C:
		}
	}
}

type antigravitySyntheticProvider struct {
	t    *testing.T
	root string
	ctx  context.Context
}

func newAntigravitySyntheticProvider(t *testing.T, root string) antigravitySyntheticProvider {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_, _ = waitAntigravitySyntheticFile(ctx, filepath.Join(root, antigravitySyntheticCancel))
		cancel()
	}()
	return antigravitySyntheticProvider{t: t, root: root, ctx: ctx}
}

func (p antigravitySyntheticProvider) publish(name string, value []byte) {
	p.t.Helper()
	if err := publishAntigravitySyntheticResult(filepath.Join(p.root, name), value, nil); err != nil {
		p.t.Fatal(err)
	}
}

func (p antigravitySyntheticProvider) wait(name string) bool {
	p.t.Helper()
	p.publish(antigravitySyntheticStage, []byte("waiting for "+name))
	_, err := waitAntigravitySyntheticFile(p.ctx, filepath.Join(p.root, name))
	if p.ctx.Err() != nil {
		return false
	}
	if err != nil {
		p.t.Fatal(err)
	}
	return true
}

func (p antigravitySyntheticProvider) hook(result string, env ...string) {
	p.t.Helper()
	p.publish(antigravitySyntheticStage, []byte("running the hook for "+result))
	cmd := exec.CommandContext(p.ctx, os.Args[0], "-test.run=^TestAntigravityRuntimeSyntheticHook$")
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if p.ctx.Err() != nil {
		return
	}
	if err != nil {
		output = append(output, []byte(err.Error())...)
	}
	p.publish(result, output)
}

func (p antigravitySyntheticProvider) hold() {
	p.publish(antigravitySyntheticStage, []byte("waiting for fixture cancellation"))
	<-p.ctx.Done()
}
