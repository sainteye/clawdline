//go:build darwin || linux

package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// This executable is a synthetic provider with real kernel/TTY identity. It
// never sends model input, runs a vendor CLI, or reads an account directory.
func TestAntigravityBootstrapSyntheticProvider(t *testing.T) {
	root := os.Getenv("AG_BOOTSTRAP_SYNTHETIC_ROOT")
	if root == "" {
		return
	}
	if err := publishAntigravitySyntheticResult(filepath.Join(root, "bootstrap-ready"), []byte(strconv.Itoa(os.Getpid())), nil); err != nil {
		os.Exit(2)
	}
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		if _, err := os.Stat(filepath.Join(root, "bootstrap-hook")); err == nil {
			cmd := exec.Command(os.Args[0], "-test.run=^TestAntigravityRuntimeSyntheticHook$")
			cmd.Env = append(os.Environ(), "AG_RUNTIME_SYNTHETIC_ROOT="+root, "AG_RUNTIME_MANAGED=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				output = append(output, []byte(err.Error())...)
			}
			if err := publishAntigravitySyntheticResult(filepath.Join(root, "bootstrap-hook-result"), output, nil); err != nil {
				os.Exit(2)
			}
			<-time.After(20 * time.Second)
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(2)
}

type bootstrapFixture struct {
	s           *Server
	ctx         context.Context
	root        string
	prepared    antigravitylaunch.Prepared
	pane        string
	run         func(...string) string
	wait        func(string) []byte
	stopRuntime func()
}

func nativeBootstrapFixture(t *testing.T, wrongWorkspace bool) bootstrapFixture {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("LC_ALL", "C")
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	s, providerDir := runtimeServer(t)
	root := filepath.Dir(providerDir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	stopOwner, err := s.StartAntigravityLaunchStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stopOwner)
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := s.antigravityLaunchStore.Load().Prepare(ctx, projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: project, Executable: exe, PermissionMode: "plan", Memory: "Synthetic bootstrap context"})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	s.inventory.Identity.(*transcript.Host).Home = home
	path, ok := transcript.AntigravityPath(home, "74000000-0000-4000-8000-000000000003")
	if !ok {
		t.Fatal("native path refused")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	tmux := s.terminals[0].(*terminal.Tmux)
	tmux.Binary = binary
	tmux.Socket = filepath.Join(root, "bootstrap.sock")
	stopRuntime, err := s.StartAntigravity(ctx, s.ManagedAntigravityOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stopRuntime)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-S", tmux.Socket}, args...)...)
		cmd.Env = append(os.Environ(), "TMUX=", "TMUX_PANE=")
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("private tmux %s: %v %s", args[0], err, data)
		}
		return strings.TrimSpace(string(data))
	}
	cwd := project
	if wrongWorkspace {
		cwd = root
	}
	command := "exec env AG_BOOTSTRAP_SYNTHETIC_ROOT=" + projects.ShellQuoted(root) + " " + strings.Join(prepared.PrivateEnv, " ") + " " + projects.ShellQuoted(exe) + " -test.run=^TestAntigravityBootstrapSyntheticProvider$"
	run("-f", "/dev/null", "new-session", "-d", "-s", "bootstrap", "-c", cwd, command)
	t.Cleanup(func() {
		cmd := exec.Command(binary, "-S", tmux.Socket, "kill-server")
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		if err := cmd.Run(); err != nil {
			t.Error("stop owned tmux", err)
		}
	})
	pane := run("display-message", "-p", "#{pane_id}")
	if err := s.antigravityLaunchStore.Load().AttachTerminal(ctx, prepared.Record.ID, "tmux", pane); err != nil {
		t.Fatal(err)
	}
	wait := func(name string) []byte {
		t.Helper()
		for ctx.Err() == nil {
			if value, err := os.ReadFile(filepath.Join(root, name)); err == nil {
				return value
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("synthetic condition deadline", name)
		return nil
	}
	wait("bootstrap-ready")
	return bootstrapFixture{s: s, ctx: ctx, root: root, prepared: prepared, pane: pane, run: run, wait: wait, stopRuntime: stopRuntime}
}
func (f bootstrapFixture) hook(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, "bootstrap-hook"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	output := f.wait("bootstrap-hook-result")
	if !bytes.Contains(output, []byte("injectSteps")) {
		t.Fatalf("managed native hook: %s", output)
	}
}
func allowBootstrap(context.Context, antigravitylaunch.Record, ports.AntigravityProcess) error {
	return nil
}

func TestManagedAntigravityFirstInputPinsExactNativePaneAndNeverRegrants(t *testing.T) {
	f := nativeBootstrapFixture(t, false)
	p := f.s.preparedAntigravityInputs(allowBootstrap)
	target, err := p.Target(f.ctx, f.prepared.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.LaunchID != f.prepared.Record.ID || target.Process.TerminalID != f.pane || target.Process.PID == 0 || target.Backend != "tmux" {
		t.Fatal("incorrect native target", target)
	}
	record, err := p.owner.LookupProcess(f.ctx, target.Process)
	if err != nil || record.NativeID != "" {
		t.Fatal("bootstrap fabricated a UUID", err)
	}
	key := uuid.NewString()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic input")))
	attempt, err := p.Reserve(f.ctx, target, key, digest)
	if err != nil || attempt == nil {
		t.Fatal(err)
	}
	retry, err := p.Reserve(f.ctx, target, key, digest)
	if err != nil || retry != nil {
		t.Fatal("retry reacquired execution", err)
	}
	if next, err := p.Reserve(f.ctx, target, uuid.NewString(), digest); err == nil || next != nil {
		t.Fatal("different key acquired execution")
	}
	if err := attempt.BeforePaste(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := attempt.BeforeEnter(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.hook(t)
	if err := p.Check(f.ctx, target); err != nil {
		t.Fatal("same execution positive identity could not be latched", err)
	}
	record, err = p.owner.LookupProcess(f.ctx, target.Process)
	if err != nil || record.NativeID != "74000000-0000-4000-8000-000000000003" {
		t.Fatal("positive UUID missing", err)
	}
	if next, err := p.Reserve(f.ctx, target, key, digest); err != nil || next != nil {
		t.Fatal("bound launch reconstructed first input", err)
	}
	stats, err := p.owner.Stats(f.ctx)
	if err != nil || stats.FirstInputs != 1 {
		t.Fatal("first slot count", stats, err)
	}
}

func TestManagedAntigravityFirstInputRefusesBeforeAuthority(t *testing.T) {
	for _, kind := range []string{"missing-policy", "wrong-workspace", "wrong-pane", "unsupported-backend", "duplicate-host", "changed-context", "changed-capability", "bound-before-reservation"} {
		t.Run(kind, func(t *testing.T) {
			f := nativeBootstrapFixture(t, kind == "wrong-workspace")
			policy := antigravityFirstInputPolicy(allowBootstrap)
			switch kind {
			case "missing-policy":
				policy = nil
			case "wrong-pane":
				// A separately prepared launch cannot borrow this native pane by name.
				other, err := f.s.antigravityLaunchStore.Load().Prepare(f.ctx, f.prepared.Request)
				if err != nil {
					t.Fatal(err)
				}
				otherPane := f.run("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "bootstrap", "exec /bin/sleep 20")
				if otherPane == f.pane {
					t.Fatal("private server reused the provider pane")
				}
				if err := f.s.antigravityLaunchStore.Load().AttachTerminal(f.ctx, other.Record.ID, "tmux", otherPane); err != nil {
					t.Fatal(err)
				}
				f.prepared = other
			case "unsupported-backend":
				other, err := f.s.antigravityLaunchStore.Load().Prepare(f.ctx, f.prepared.Request)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.s.antigravityLaunchStore.Load().AttachTerminal(f.ctx, other.Record.ID, "iterm", f.pane); err != nil {
					t.Fatal(err)
				}
				f.prepared = other
			case "duplicate-host":
				f.s.terminals = append(f.s.terminals, f.s.terminals[0])
			case "changed-context":
				if err := os.WriteFile(f.prepared.Record.ContextPath, []byte("synthetic tamper"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-capability":
				if err := os.WriteFile(f.prepared.Record.CapabilityPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
					t.Fatal(err)
				}
			case "bound-before-reservation":
				f.hook(t)
			}
			p := f.s.preparedAntigravityInputs(policy)
			if target, err := p.Target(f.ctx, f.prepared.Record.ID); err == nil {
				t.Fatal("unsafe launch became a target", target)
			}
			if _, found, err := p.owner.FirstInputForLaunch(f.ctx, f.prepared.Record.ID); err != nil || found {
				t.Fatal("preflight consumed input slot", found, err)
			}
			stats, err := p.owner.Stats(f.ctx)
			if kind == "changed-context" {
				if err == nil {
					t.Fatal("tampered context was reported as known usage", stats)
				}
			} else if err != nil || stats.FirstInputs != 0 {
				t.Fatal("preflight input usage", stats, err)
			}
		})
	}
}

func TestManagedAntigravityFirstInputRevokesBeforeNextEffect(t *testing.T) {
	for _, kind := range []string{"policy-revoked", "runtime-stopped", "process-replaced", "bound-before-enter"} {
		t.Run(kind, func(t *testing.T) {
			f := nativeBootstrapFixture(t, false)
			allowed := true
			p := f.s.preparedAntigravityInputs(func(context.Context, antigravitylaunch.Record, ports.AntigravityProcess) error {
				if !allowed {
					return fmt.Errorf("synthetic policy revoked")
				}
				return nil
			})
			target, err := p.Target(f.ctx, f.prepared.Record.ID)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := p.Reserve(f.ctx, target, uuid.NewString(), fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic input"))))
			if err != nil || attempt == nil {
				t.Fatal(err)
			}
			switch kind {
			case "policy-revoked":
				allowed = false
			case "runtime-stopped":
				f.stopRuntime()
			case "process-replaced":
				f.run("respawn-pane", "-k", "-t", f.pane, "exec /bin/sleep 20")
			case "bound-before-enter":
				f.hook(t)
			}
			if err := p.Check(f.ctx, target); err == nil {
				t.Fatal("revoked native proof retained input authority")
			}
			stats, err := p.owner.Stats(f.ctx)
			if err != nil || stats.FirstInputs != 1 {
				t.Fatal("revocation released consumed slot", stats, err)
			}
		})
	}
}
