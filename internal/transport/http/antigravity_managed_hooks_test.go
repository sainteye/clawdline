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
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func TestManagedAntigravityAuthenticatesThenPreparesOriginalContext(t *testing.T) {
	for _, name := range []string{"original_workspace", "wrong_workspace", "partial_native_record", "missing_loaded_row", "model_echo", "replaced_transcript", "completed_post", "completed_stop", "completion_missing", "completion_model_echo", "completion_replaced", "empty_context", "empty_completion", "legacy_unfenced"} {
		wrongWorkspace := name == "wrong_workspace"
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
			request := projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: project,
				Executable: executable, PermissionMode: "plan", Memory: "ORIGINAL_SYNTHETIC_MEMORY", Language: "zh-Hant-TW"}
			if strings.HasPrefix(name, "empty_") {
				request.Memory, request.Language = "", ""
			}
			prepared, err := owner.Prepare(ctx, request)
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
			initial := []byte{}
			if name == "partial_native_record" {
				initial = []byte("{")
			}
			if err := os.WriteFile(path, initial, 0600); err != nil {
				t.Fatal(err)
			}
			tmux := s.terminals[0].(*terminal.Tmux)
			tmux.Binary = binary
			tmux.Socket = filepath.Join(root, "managed.sock")
			options := s.ManagedAntigravityOptions()
			if name == "legacy_unfenced" {
				// Reproduce a known older response without inventing a baseline.
				options.PreInvocationResponse = func(c context.Context, b ports.AntigravityBinding) ([]byte, error) {
					if err := owner.RecordBinding(c, prepared.Record.ID, b, antigravitylaunch.KeepConversation); err != nil {
						return nil, err
					}
					return owner.PreInvocationResponse(c, b)
				}
			}
			stopRuntime, err := s.StartAntigravity(ctx, options)
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
			followup := "PreInvocation"
			if name == "completed_post" {
				followup = "PostInvocation"
			} else if strings.HasPrefix(name, "completion_") || name == "completed_stop" || name == "empty_completion" || name == "legacy_unfenced" {
				followup = "Stop"
			}
			command := "exec env AG_RUNTIME_FOLLOWUP_EVENT=" + followup + " AG_RUNTIME_SYNTHETIC_ROOT=" + projects.ShellQuoted(root) + " AG_RUNTIME_MANAGED=1 AG_RUNTIME_CONTEXT_FOLLOWUP=1 " + strings.Join(prepared.PrivateEnv, " ") + " " + projects.ShellQuoted(executable) + " -test.run=^TestAntigravityRuntimeSyntheticProvider$"
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
			if name == "partial_native_record" {
				if !bytes.Contains(output, []byte("antigravity_hook_response_failed")) {
					t.Fatalf("partial native baseline accepted: %s", output)
				}
				if _, found, err := owner.Receipt(ctx, prepared.Record.ID); err != nil || found {
					t.Fatal("invalid baseline acquired a prepared response", err)
				}
				return
			}
			if strings.HasPrefix(name, "empty_") {
				if string(bytes.TrimSpace(output)) != "{}" {
					t.Fatal("empty context returned injection", string(output))
				}
				first, found, err := owner.Receipt(ctx, prepared.Record.ID)
				if err != nil || !found || first.Fenced || first.Loaded {
					t.Fatal("empty context waits for a nonexistent system row", first.Fenced, first.Loaded, err)
				}
				injection, err := owner.ExpectedInjection(ctx, prepared.Record.ID)
				if err != nil || injection.Text != "" || first.InjectionDigest != injection.Digest {
					t.Fatal("empty injection metadata changed", err)
				}
				signal("managed-next")
				next := wait("hook-next-result")
				latest, found, err := owner.Receipt(ctx, prepared.Record.ID)
				if err != nil || !found || latest.Fenced || latest.Loaded || string(bytes.TrimSpace(next)) != "{}" {
					t.Fatal("empty context cannot complete or advance", string(next), err)
				}
				if name == "empty_completion" && latest != first {
					t.Fatal("empty completion manufactured a response")
				}
				check := func(c context.Context, b ports.AntigravityBinding) error {
					return s.checkManagedAntigravityContext(c, prepared.Record.ID, b)
				}
				if err := owner.CheckContextLoaded(ctx, prepared.Record.ID, home, check); err == nil {
					t.Fatal("empty context acquired a loaded badge")
				}
				history, err := owner.WorkspaceBindings(ctx)
				if err != nil || history[conversation] != project {
					t.Fatal("empty context lost native identity", err)
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
			if err != nil || !found || receipt.Status != "PREPARED" || receipt.Fenced != (name != "legacy_unfenced") || receipt.Loaded || receipt.Fence.Bytes != 0 {
				t.Fatal("response became a false loaded receipt", err)
			}
			history, err := owner.WorkspaceBindings(ctx)
			if err != nil || history[conversation] != project {
				t.Fatal("positive native workspace evidence missing", err)
			}

			// A second real Unix helper is descended from the same owned
			// foreground test executable. Only a native system row appended
			// AFTER the first response permits the next fenced response.
			fixture, err := os.ReadFile(filepath.Join("..", "..", "adapters", "transcript", "testdata", "antigravity-context.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err := json.Unmarshal(fixture, &row); err != nil {
				t.Fatal(err)
			}
			const synthetic = "Synthetic immutable context.\nRole ALDER; language zh-Hant-TW; memory MAPLE."
			content := row["content"].(string)
			if strings.Count(content, synthetic) != 1 {
				t.Fatal("native fixture context missing")
			}
			row["content"] = strings.Replace(content, synthetic, response.InjectSteps[0].SystemMessage.SystemMessage, 1)
			row["step_index"] = 0
			if name == "model_echo" || name == "completion_model_echo" {
				row["source"] = "MODEL"
			}
			data, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')
			if name != "missing_loaded_row" && name != "completion_missing" {
				target := path
				if name == "replaced_transcript" || name == "completion_replaced" {
					target = path + ".replacement"
				}
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
				if target != path {
					if err := os.Rename(target, path); err != nil {
						t.Fatal(err)
					}
				}
			}
			signal("managed-next")
			nextOutput := wait("hook-next-result")
			nextReceipt, found, err := owner.Receipt(ctx, prepared.Record.ID)
			if err != nil || !found {
				t.Fatal("response receipt lost", err)
			}
			if name != "original_workspace" && name != "completed_post" && name != "completed_stop" {
				if !bytes.Contains(nextOutput, []byte("antigravity_hook_response_failed")) {
					t.Fatalf("missing/stale/non-native proof advanced: %s", nextOutput)
				}
				if nextReceipt != receipt {
					t.Fatal("failed proof replaced its original baseline")
				}
				return
			}
			if name == "completed_post" || name == "completed_stop" {
				if string(bytes.TrimSpace(nextOutput)) != "{}" || !nextReceipt.Loaded || nextReceipt.Sequence != receipt.Sequence || nextReceipt.Fence != receipt.Fence || nextReceipt.Proof.RowEnd <= nextReceipt.Proof.RowStart {
					t.Fatal("completion did not verify the existing response", nextReceipt.Loaded, string(nextOutput))
				}
				check := func(c context.Context, b ports.AntigravityBinding) error {
					return s.checkManagedAntigravityContext(c, record.ID, b)
				}
				if err := owner.CheckContextLoaded(ctx, record.ID, home, check); err != nil {
					t.Fatal("completion proof was not currently verifiable", err)
				}
				return
			}
			if !json.Valid(nextOutput) || !bytes.Contains(nextOutput, []byte("injectSteps")) {
				t.Fatalf("native proof could not advance response: %s", nextOutput)
			}
			if nextReceipt.Sequence <= receipt.Sequence || !nextReceipt.Fenced || nextReceipt.Loaded || nextReceipt.Fence.Bytes != int64(len(data)) || nextReceipt.Fence.LastStep != 0 {
				t.Fatal("next response did not retain its own pre-response baseline")
			}
		})
	}
}
