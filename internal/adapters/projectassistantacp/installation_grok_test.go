package projectassistantacp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestGrokBotNewAndResumeUseOwnedToolsAndWaitForReply(t *testing.T) {
	for _, method := range []string{"session/new", "session/resume"} {
		t.Run(method, func(t *testing.T) {
			p, q, path := configuredProvider(t, pa.Grok, "grok-scripted")
			q.PersistNativeSession = true
			q.Resume = method == "session/resume"
			if q.Resume {
				q.Binding.NativeSessionID = "00000000-0000-4000-8000-000000000042"
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
					Meta       struct{ Rules string } `json:"_meta"`
				}
			}
			if json.Unmarshal(data, &request) != nil || request.Method != method || request.Params.SessionID != q.Binding.NativeSessionID ||
				len(request.Params.MCPServers) != 1 || request.Params.MCPServers[0].Name != "clawdline" || request.Params.MCPServers[0].Type != "http" ||
				!strings.Contains(request.Params.Meta.Rules, q.Bot.Instructions.Text) {
				t.Fatalf("wrong native session request: method=%s session=%s", request.Method, request.Params.SessionID)
			}
			promptCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			pending, err := session.Send(promptCtx, app.PromptRequest{Turn: pa.Turn{ID: "grok-turn", Fence: q.Binding.Fence}, Text: "Scripted test input"})
			if err != nil {
				t.Fatal(err)
			}
			reply, err := pending.Wait(promptCtx)
			if err != nil || reply.FinalText != "Scripted Grok reply" {
				t.Fatalf("reply=%q err=%v", reply.FinalText, err)
			}
		})
	}
}

func TestGrokBotRequiresCachedPersonalLoginBeforeNativeSession(t *testing.T) {
	for mode, field := range map[string]string{"grok-signed-out": "grok_login", "grok-backed-key": "grok_auth_mode_unsupported"} {
		t.Run(mode, func(t *testing.T) {
			p, q, path := configuredProvider(t, pa.Grok, mode)
			native, err := p.Open(t.Context(), q, func(context.Context, app.ProtocolEvent) error { return nil })
			var failed *pa.Failure
			if !errors.As(err, &failed) || failed.Code != pa.AuthRejected || failed.Field != field {
				t.Fatalf("wrong sign-in refusal: %v", err)
			}
			var notStarted *app.OpenNotStarted
			if native != nil || !errors.As(err, &notStarted) {
				t.Fatal("sign-in refusal did not confirm cleanup and that no session started")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("sign-in refusal reached native session creation", err)
			}
		})
	}
}
