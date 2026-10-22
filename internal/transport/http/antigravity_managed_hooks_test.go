package http

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

func TestManagedAntigravityAuthenticatesThenPreparesOriginalContext(t *testing.T) {
	for _, wrongWorkspace := range []bool{false, true} {
		name := "original_workspace"
		if wrongWorkspace {
			name = "wrong_workspace"
		}
		t.Run(name, func(t *testing.T) {
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
			defer cancel()
			stopOwner, err := s.StartAntigravityLaunchStore(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stopOwner()
			owner := s.antigravityLaunchStore.Load()
			project := filepath.Join(root, "project")
			if err := os.Mkdir(project, 0700); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			executable, err = filepath.EvalSymlinks(executable)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := owner.Prepare(ctx, projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: project,
				Executable: executable, PermissionMode: "plan", Memory: "ORIGINAL_SYNTHETIC_MEMORY", Language: "zh-Hant-TW"})
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			s.inventory.Identity.(*transcript.Host).Home = home
			const conversation = "74000000-0000-4000-8000-000000000003"
			path, _ := transcript.AntigravityPath(home, conversation)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			tmux := s.terminals[0].(*terminal.Tmux)
			tmux.Binary = binary
			tmux.Socket = filepath.Join(root, "managed.sock")
			stopRuntime, err := s.StartAntigravity(ctx, s.ManagedAntigravityOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer stopRuntime() // callbacks join before the owner closes
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.CommandContext(ctx, binary, append([]string{"-S", tmux.Socket}, args...)...)
				cmd.Env = append(os.Environ(), "TMUX=", "TMUX_PANE=")
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("private tmux %s: %v %s", args[0], err, output)
				}
				return strings.TrimSpace(string(output))
			}
			cwd := project
			if wrongWorkspace {
				cwd = root
			}
			command := "exec env AG_RUNTIME_SYNTHETIC_ROOT=" + projects.ShellQuoted(root) + " AG_RUNTIME_MANAGED=1 " + strings.Join(prepared.PrivateEnv, " ") + " " + projects.ShellQuoted(executable) + " -test.run=^TestAntigravityRuntimeSyntheticProvider$"
			run("-f", "/dev/null", "new-session", "-d", "-s", "managed", "-c", cwd, command)
			defer func() {
				cmd := exec.Command(binary, "-S", tmux.Socket, "kill-server")
				cmd.Env = []string{"PATH=/usr/bin:/bin"}
				if err := cmd.Run(); err != nil {
					t.Errorf("stop private tmux: %v", err)
				}
			}()
			pane := run("display-message", "-p", "#{pane_id}")
			if err := owner.AttachTerminal(ctx, prepared.Record.ID, "tmux", pane); err != nil {
				t.Fatal(err)
			}
			signal := func(name string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			wait := func(name string) []byte {
				t.Helper()
				for ctx.Err() == nil {
					if value, err := os.ReadFile(filepath.Join(root, name)); err == nil {
						return value
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("synthetic hook result %s did not arrive", name)
				return nil
			}
			signal("managed-open")
			bad := wait("managed-bad-result")
			if !bytes.Contains(bad, []byte("antigravity_hook_admission_failed")) {
				t.Fatalf("bad capability: %s", bad)
			}
			hook, err := antigravity.LoadHookLaunch(providerDir, prepared.Record.CapabilityPath)
			if err != nil {
				t.Fatal(err)
			}
			record, err := owner.VerifyLaunchCapability(ctx, prepared.Record.ID, hook.Capability)
			if err != nil || record.Process.PID != 0 {
				t.Fatal("bad capability changed process authority", err)
			}
			signal("managed-continue")
			output := wait("hook-result")
			if wrongWorkspace {
				if !bytes.Contains(output, []byte("antigravity_hook_admission_failed")) {
					t.Fatalf("wrong workspace: %s", output)
				}
				record, err = owner.VerifyLaunchCapability(ctx, prepared.Record.ID, hook.Capability)
				if err != nil || record.Process.PID != 0 {
					t.Fatal("wrong workspace pinned a process", err)
				}
				history, err := owner.WorkspaceBindings(ctx)
				if err != nil || len(history) != 0 {
					t.Fatal("wrong workspace manufactured history", err)
				}
				return
			}
			var response struct {
				InjectSteps []struct {
					SystemMessage struct {
						SystemMessage string `json:"systemMessage"`
					} `json:"systemMessage"`
				} `json:"injectSteps"`
			}
			if err := json.Unmarshal(output, &response); err != nil || len(response.InjectSteps) != 1 || !strings.Contains(response.InjectSteps[0].SystemMessage.SystemMessage, "ORIGINAL_SYNTHETIC_MEMORY") || !strings.Contains(response.InjectSteps[0].SystemMessage.SystemMessage, "zh-Hant-TW") {
				t.Fatalf("original context was not prepared: %v %s", err, output)
			}
			record, err = owner.VerifyLaunchCapability(ctx, prepared.Record.ID, hook.Capability)
			if err != nil || record.Process.PID == 0 || record.NativeID != conversation {
				t.Fatal("native binding not retained", err)
			}
			receipt, found, err := owner.Receipt(ctx, prepared.Record.ID)
			if err != nil || !found || receipt.Status != "PREPARED" {
				t.Fatal("response became a false loaded receipt", err)
			}
			history, err := owner.WorkspaceBindings(ctx)
			if err != nil || history[conversation] != project {
				t.Fatal("positive native workspace evidence missing", err)
			}
		})
	}
}
