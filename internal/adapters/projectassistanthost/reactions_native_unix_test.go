//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistanthost

import (
	"context"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"os"
	"testing"
	"time"
)

// The actual owned process supervisor is a Unix facility. The generic tests
// separately exercise the captured tool's refusal and durable metadata boundaries.
func TestNativeReactionThroughOwnedACPAndMCP(t *testing.T) {
	f := newHostFixture(t)
	f.o.Limits.PromptTimeout = 15 * time.Second
	f.o.Limits.CleanupTimeout = 2 * time.Second
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	f.o.Provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: executable, Arguments: []string{"-test.run=^TestHostOwnedMCPPeer$"}, Environment: []string{"CLAWDLINE_HOST_MCP_PEER=1", "CLAWDLINE_REACTION_ONLY=1", "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "Owned ACP/MCP peer; no provider", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, SendMessage: true}, nil
		}})
	}
	f.start(t)
	f.enable(t)
	b := f.create(t, "reaction-only")
	if _, e = f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		x, e := f.h.store.Export(context.Background(), b.Key)
		if e != nil {
			t.Fatal(e)
		}
		if len(x.Turns) == 1 && x.Conversation.ActiveTurnID == "" && x.Turns[0].State != pa.TurnQueued && x.Turns[0].State != pa.TurnAccepted {
			if x.Turns[0].State != pa.TurnSucceeded || len(x.Messages) != 1 || len(x.Reactions) != 1 || x.Reactions[0].Actor.Emojis[0] != "🎉" {
				t.Fatal("reaction-only outcome", x.Turns, x.Messages, x.Reactions)
			}
			for _, event := range x.Events {
				if event.Trace != nil && event.Trace.Category == "no_visible_reply" {
					t.Fatal("successful reaction reported empty")
				}
			}
			t.Log("Owned synthetic ACP -> authenticated owned MCP -> Host captured actor/target -> durable SQLite reaction; no fabricated assistant message; foreign arguments refused; provider/model calls=0")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native reaction did not finish", x.Turns)
		}
		time.Sleep(time.Millisecond)
	}
}
