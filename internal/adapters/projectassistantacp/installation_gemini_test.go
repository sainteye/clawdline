package projectassistantacp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const geminiTestSession = "00000000-0000-4000-8000-000000000042"

func geminiTestPeer(mode string, f frame, line []byte, send func(frame)) bool {
	result := json.RawMessage(`{}`)
	switch f.Method {
	case "initialize":
		result = json.RawMessage(`{
			"protocolVersion":1,
			"agentInfo":{"name":"Antigravity ACP Server","version":"scripted"},
			"authMethods":[{"id":"gemini-api-key"},{"id":"oauth-personal"}],
			"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"resume":{}},"mcpCapabilities":{"http":true},"promptCapabilities":{"image":true}}
		}`)
	case "authenticate":
		var request struct{ MethodID string }
		if json.Unmarshal(f.Params, &request) != nil || request.MethodID != "oauth-personal" {
			return false
		}
		if mode == "gemini-signed-out" {
			send(frame{ID: f.ID, Error: &rpcError{Code: -32000}})
			return true
		}
	case "session/new", "session/resume":
		if mode != "gemini-scripted" || os.WriteFile(os.Getenv("CLAWDLINE_ACP_PARAMS_FILE"), line, 0600) != nil {
			return false
		}
		// Setup metadata is allowed to precede the binding reply.
		send(frame{Method: "session/update", Params: raw(map[string]any{
			"sessionId": geminiTestSession, "update": map[string]string{"sessionUpdate": "current_mode_update"},
		})})
		result = raw(map[string]any{
			"sessionId": geminiTestSession,
			"modes": map[string]any{"currentModeId": "default", "availableModes": []any{
				map[string]string{"id": "default"}, map[string]string{"id": "auto"},
			}},
		})
	case "session/set_mode":
		var request struct{ SessionID, ModeID string }
		if json.Unmarshal(f.Params, &request) != nil || request.SessionID != geminiTestSession || request.ModeID != "auto" {
			return false
		}
	case "session/prompt":
		var request struct {
			SessionID string
			Prompt    []struct{ Type, Text string }
		}
		if json.Unmarshal(f.Params, &request) != nil || request.SessionID != geminiTestSession || len(request.Prompt) != 1 ||
			request.Prompt[0].Type != "text" || !strings.Contains(request.Prompt[0].Text, "Answer within this Project's fixed role.") ||
			!strings.HasSuffix(request.Prompt[0].Text, "Scripted input") {
			return false
		}
		send(frame{Method: "session/update", Params: raw(map[string]any{
			"sessionId": geminiTestSession, "update": map[string]any{
				"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Scripted Gemini reply"},
			},
		})})
		result = json.RawMessage(`{"stopReason":"end_turn"}`)
	default:
		return false
	}
	send(frame{ID: f.ID, Result: result})
	return true
}

func configuredGemini(t *testing.T, mode string) (*Provider, app.OpenRequest, string, string) {
	t.Helper()
	p, q, path := configuredProvider(t, pa.Gemini, mode)
	state := t.TempDir()
	config, err := withGeminiProfile(p.o.Configurations[pa.Gemini], state, state)
	if err != nil {
		t.Fatal(err)
	}
	config.Arguments = p.o.Configurations[pa.Gemini].Arguments // The peer is a Go test binary.
	p.o.Configurations[pa.Gemini] = config
	q.PersistNativeSession = true
	return p, q, path, config.geminiSettings
}

func TestGeminiBotNewAndResumeUsePersonalOAuthAndOwnedTools(t *testing.T) {
	for _, method := range []string{"session/new", "session/resume"} {
		t.Run(method, func(t *testing.T) {
			p, q, path, settings := configuredGemini(t, "gemini-scripted")
			q.Resume = method == "session/resume"
			if q.Resume {
				q.Binding.NativeSessionID = geminiTestSession
			}
			ctx := t.Context()
			session, err := p.Open(ctx, q, func(context.Context, app.ProtocolEvent) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := session.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Method string
				Params struct {
					SessionID  string
					MCPServers []struct{ Type, Name string }
				}
			}
			if json.Unmarshal(data, &request) != nil || request.Method != method || request.Params.SessionID != q.Binding.NativeSessionID ||
				len(request.Params.MCPServers) != 1 || request.Params.MCPServers[0].Name != "clawdline" || request.Params.MCPServers[0].Type != "http" {
				t.Fatalf("wrong native session request: method=%s session=%s", request.Method, request.Params.SessionID)
			}
			prompt := app.PromptRequest{Turn: pa.Turn{ID: "gemini-turn", Fence: q.Binding.Fence}, Text: "Scripted input"}
			if err := os.WriteFile(settings, []byte(strings.Replace(geminiSettings, "false", "true", 1)), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Send(ctx, prompt); pa.Code(err) != pa.MemoryIsolationUnverified {
				t.Fatalf("enabled AI Credits reached prompt: %v", err)
			}
			if err := os.WriteFile(settings, []byte(geminiSettings), 0600); err != nil {
				t.Fatal(err)
			}
			pending, err := session.Send(ctx, prompt)
			if err != nil {
				t.Fatal(err)
			}
			reply, err := pending.Wait(ctx)
			if err != nil || reply.FinalText != "Scripted Gemini reply" {
				t.Fatalf("reply=%q err=%v", reply.FinalText, err)
			}
		})
	}
}

func TestGeminiBotSignedOutStopsBeforeSessionCreation(t *testing.T) {
	p, q, path, _ := configuredGemini(t, "gemini-signed-out")
	native, err := p.Open(t.Context(), q, func(context.Context, app.ProtocolEvent) error { return nil })
	var failed *pa.Failure
	var notStarted *app.OpenNotStarted
	if native != nil || !errors.As(err, &notStarted) || !errors.As(err, &failed) || failed.Code != pa.AuthRejected || failed.Field != "gemini_login" {
		t.Fatalf("wrong sign-in refusal: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("sign-in refusal reached native session creation", err)
	}
}
