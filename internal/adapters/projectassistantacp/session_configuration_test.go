package projectassistantacp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func configuredProvider(t *testing.T, agent pa.Agent, mode string) (*Provider, app.OpenRequest, string) {
	t.Helper()
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("native supervisor unavailable")
	}
	fence := testFence()
	q := app.OpenRequest{Bot: pa.Bot{Key: fence.Key, Project: fence.Project, Agent: agent, ConversationID: fence.ConversationID, Instructions: pa.InstructionSnapshot{Revision: 7, Text: "Answer within this Project's fixed role."}}, Binding: pa.ExecutionBinding{Fence: fence, Agent: agent, InstructionRevision: 7}, Directory: t.TempDir(), Tools: mcpTestTools{}}
	path := filepath.Join(t.TempDir(), "session-request.json")
	cfg := peerConfiguration(t, mode)
	cfg.Environment = append(cfg.Environment, "CLAWDLINE_ACP_PARAMS_FILE="+path)
	p, err := NewProvider(ProviderOptions{Limits: testLimits(), Configurations: map[pa.Agent]Configuration{agent: cfg}, Verify: func(context.Context, pa.Bot, Configuration) (pa.Capabilities, error) {
		return pa.Capabilities{Key: fence.Key, Project: fence.Project, Agent: agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "scripted only; no native readiness", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, Resume: true, SendMessage: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p, q, path
}
func TestProviderPassesCapturedInstructionsAndOwnedMCPOnNewAndLoad(t *testing.T) {
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		for _, resume := range []bool{false, true} {
			t.Run(string(agent)+map[bool]string{false: "-new", true: "-load"}[resume], func(t *testing.T) {
				mode := "configure-codex"
				if agent == pa.ClaudeCode {
					mode = "configure-claude"
				}
				p, q, path := configuredProvider(t, agent, mode)
				q.Resume = resume
				if resume {
					q.Binding.NativeSessionID = "scripted-session"
				}
				if _, err := p.Capabilities(context.Background(), q.Bot); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("metadata read started process", err)
				}
				session, err := p.Open(context.Background(), q, func(context.Context, app.ProtocolEvent) error { return nil })
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close(context.Background())
				request, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Method string                     `json:"method"`
					Params map[string]json.RawMessage `json:"params"`
				}
				if err = json.Unmarshal(request, &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Method != map[bool]string{false: "session/new", true: "session/load"}[resume] {
					t.Fatal(wire.Method)
				}
				var servers []struct {
					Type, Name, URL string
					Headers         []struct{ Name, Value string }
				}
				if err = json.Unmarshal(wire.Params["mcpServers"], &servers); err != nil || len(servers) != 1 {
					t.Fatal("owned MCP missing", err, string(request))
				}
				if servers[0].Type != "http" || servers[0].Name != "clawdline" || !strings.HasPrefix(servers[0].URL, "http://127.0.0.1:") || len(servers[0].Headers) != 1 || servers[0].Headers[0].Name != "Authorization" {
					t.Fatal("wrong MCP wire", servers)
				}
				if !strings.Contains(string(request), q.Bot.Instructions.Text) {
					t.Fatal("fixed instructions missing")
				}
				if agent == pa.Codex {
					var prompt struct {
						Mode    string
						Content []struct{ Type, Text string }
					}
					if json.Unmarshal(wire.Params["systemPrompt"], &prompt) != nil || prompt.Mode != "append" || len(prompt.Content) != 1 || prompt.Content[0].Text != q.Bot.Instructions.Text {
						t.Fatal("Codex system prompt", string(request))
					}
				}
				if agent == pa.ClaudeCode {
					var meta struct {
						SystemPrompt struct {
							Append                 string
							ExcludeDynamicSections bool
						}
						ClaudeCode struct {
							Options struct {
								Tools, SettingSources                            []string
								StrictMcpConfig, AllowDangerouslySkipPermissions bool
								Settings                                         struct{ AutoMemoryEnabled, AutoDreamEnabled, DisableAllHooks bool }
							}
						}
					}
					if err = json.Unmarshal(wire.Params["_meta"], &meta); err != nil {
						t.Fatal(err)
					}
					o := meta.ClaudeCode.Options
					if meta.SystemPrompt.Append != q.Bot.Instructions.Text || !meta.SystemPrompt.ExcludeDynamicSections || o.Tools == nil || len(o.Tools) != 0 || o.SettingSources == nil || len(o.SettingSources) != 0 || !o.StrictMcpConfig || o.AllowDangerouslySkipPermissions || o.Settings.AutoMemoryEnabled || o.Settings.AutoDreamEnabled || !o.Settings.DisableAllHooks {
						t.Fatal("Claude owned preset", string(request))
					}
				}
				if err = session.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
func TestProviderRejectsChangedAdapterBeforeSessionCreation(t *testing.T) {
	p, q, path := configuredProvider(t, pa.Codex, "configure-codex-wrong-version")
	session, err := p.Open(context.Background(), q, func(context.Context, app.ProtocolEvent) error { return nil })
	if session != nil || pa.Code(err) != pa.ProviderVersionUnsupported {
		t.Fatal(session, err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session created before adapter verification")
	}
}
func TestProviderPreservesPartialCleanupHandleAndSealsToolsFirst(t *testing.T) {
	p, q, _ := configuredProvider(t, pa.Codex, "configure-codex-wrong-version")
	var attempts atomic.Int32
	p.o.OpenMCP = func(context.Context, app.OpenRequest) (OwnedMCP, error) {
		return OwnedMCP{Fence: testFence(), URL: "http://127.0.0.1:12345/mcp", BearerToken: "owned", Revoke: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return errors.New("injected cleanup uncertainty")
			}
			return nil
		}}, nil
	}
	session, err := p.Open(context.Background(), q, func(context.Context, app.ProtocolEvent) error { return nil })
	if session == nil || pa.Code(err) != pa.ExecutionEffectUnknown || attempts.Load() != 1 {
		t.Fatal("cleanup handle dropped", session, err, attempts.Load())
	}
	if err = session.Close(context.Background()); err != nil || attempts.Load() != 2 {
		t.Fatal("explicit cleanup retry", err, attempts.Load())
	}
}
func TestProviderRefusesUnownedOrRevokedToolsBeforeLaunching(t *testing.T) {
	for _, mutate := range []func(*app.OpenRequest){func(q *app.OpenRequest) { q.Tools = nil }, func(q *app.OpenRequest) { q.Binding.Revoked = true }, func(q *app.OpenRequest) { q.Binding.InstructionRevision++ }, func(q *app.OpenRequest) { q.Binding.Agent = pa.ClaudeCode }} {
		p, q, path := configuredProvider(t, pa.Codex, "configure-codex")
		mutate(&q)
		session, err := p.Open(context.Background(), q, func(context.Context, app.ProtocolEvent) error { return nil })
		if err == nil || session != nil {
			t.Fatal("unowned Open admitted")
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unowned session started")
		}
	}
}
