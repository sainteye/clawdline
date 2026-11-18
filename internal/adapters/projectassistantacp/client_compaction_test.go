package projectassistantacp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func nativeCompactor(t *testing.T, session any) app.NativeCompactor {
	t.Helper()
	port, ok := session.(app.NativeCompactor)
	if !ok {
		t.Fatal("the native session has no dedicated compaction port")
	}
	return port
}

func TestNativeCompactionRefusesUnqualifiedCapabilitiesBeforeTransport(t *testing.T) {
	for _, commands := range []string{"", `[]`, `null`, `[{"name":"other"}]`} {
		t.Run(commands, func(t *testing.T) {
			// No process exists: a refusal must precede any transport access.
			c := &Client{fence: testFence(), sessionID: "own-session"}
			if commands != "" {
				if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"available_commands_update","availableCommands":`+commands+`}`); err != nil {
					t.Fatal(err)
				}
			}
			pending, err := nativeCompactor(t, c).BeginCompaction(context.Background(), testFence())
			if pa.Code(err) != pa.Unsupported || pending != nil {
				t.Fatal("unqualified compaction entered transport", pending, err)
			}
		})
	}
}

func TestNativeCompactionRefusesForeignAuthorityAndBusySessions(t *testing.T) {
	c := &Client{fence: testFence(), sessionID: "own-session"}
	if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact"}]}`); err != nil {
		t.Fatal(err)
	}
	port := nativeCompactor(t, c)
	for _, change := range []func(*pa.Fence){
		func(f *pa.Fence) { f.Key.BotID = "other" },
		func(f *pa.Fence) { f.Project.ProjectID = "other" },
		func(f *pa.Fence) { f.ConversationID = "other" },
		func(f *pa.Fence) { f.BindingID = "other" },
		func(f *pa.Fence) { f.Generation++ },
	} {
		f := testFence()
		change(&f)
		if p, err := port.BeginCompaction(context.Background(), f); err == nil || p != nil {
			t.Fatal("foreign authority entered maintenance transport", f, p, err)
		}
	}
	c.turnID = "actual-prompt"
	if p, err := port.BeginCompaction(context.Background(), testFence()); pa.Code(err) != pa.CapacityFull || p != nil {
		t.Fatal("a busy session entered maintenance transport", p, err)
	}
	c.turnID = ""
	c.cancelled = true
	if p, err := port.BeginCompaction(context.Background(), testFence()); pa.Code(err) != pa.Revoked || p != nil {
		t.Fatal("a cancelled session entered maintenance transport", p, err)
	}
}

func openCompactionPeer(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "requests.txt")
	t.Setenv("CLAWDLINE_ACP_PROMPT_LOG", log)
	c, _ := openTestClient(t, mode, func(context.Context, app.ProtocolEvent) error {
		t.Error("maintenance fabricated a chat/tool event")
		return nil
	})
	// The ordered load reply also drains early session/new metadata.
	if _, err := c.call(context.Background(), "session/load", map[string]string{"sessionId": c.SessionID()}); err != nil {
		t.Fatal(err)
	}
	return c, log
}

func requireOneCompactionWrite(t *testing.T, log string) {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil || len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 1 {
		t.Fatal("maintenance was missing or retried", string(data), err)
	}
}

func TestNativeCompactionWireTerminalsAndUnknownNeverReplay(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		state   app.NativeCompactionState
		unknown bool
	}{
		{"compact-completed", app.NativeCompactionCompleted, false},
		{"compact-duplicate-terminal", app.NativeCompactionCompleted, false},
		{"compact-failed", app.NativeCompactionFailed, false},
		{"compact-cancelled", app.NativeCompactionCancelled, false},
		{"compact-unknown-status", app.NativeCompactionUnknown, true},
		{"compact-malformed-result", app.NativeCompactionUnknown, true},
		{"compact-no-terminal", app.NativeCompactionUnknown, true},
		{"compact-missing-start", app.NativeCompactionUnknown, true},
		{"compact-wrong-id", app.NativeCompactionUnknown, true},
		{"compact-eof", app.NativeCompactionUnknown, true},
		{"compact-rpc-error", app.NativeCompactionUnknown, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			c, log := openCompactionPeer(t, tc.mode)
			port := nativeCompactor(t, c)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			pending, err := port.BeginCompaction(ctx, testFence())
			if err != nil {
				t.Fatal(err)
			}
			got, err := pending.Wait(ctx)
			if got.State != tc.state || got.Fence != testFence() || got.NativeSessionID != "scripted-session" || tc.unknown != (pa.Code(err) == pa.ExecutionEffectUnknown) || !tc.unknown && err != nil {
				t.Fatal("unqualified maintenance outcome", got, err)
			}
			// Inspecting the same pending fact cannot read/send another RPC.
			again, againErr := pending.Wait(ctx)
			if again != got || pa.Code(againErr) != pa.Code(err) {
				t.Fatal("pending outcome changed", again, againErr)
			}
			if retry, e := port.BeginCompaction(ctx, testFence()); e == nil || retry != nil {
				t.Fatal("maintenance attempt replayed", retry, e)
			}
			if _, e := c.Send(ctx, app.PromptRequest{Turn: pa.Turn{ID: "not-a-maintenance-turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Text: "must not enter compact"}}, Text: "must not send"}); pa.Code(e) != pa.Revoked {
				t.Fatal("the maintenance client accepted chat", e)
			}
			requireOneCompactionWrite(t, log)
			c.mu.Lock()
			turn := c.turnID
			c.mu.Unlock()
			if turn != "" {
				t.Fatal("maintenance fabricated a prompt turn", turn)
			}
		})
	}
}

func TestNativeCompactionCancellationKeepsLateTerminalUnknown(t *testing.T) {
	c, log := openCompactionPeer(t, "compact-wait")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p, err := nativeCompactor(t, c).BeginCompaction(ctx, testFence())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	if result, err := p.Wait(ctx); result.State != app.NativeCompactionUnknown || pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("late terminal replaced cancellation", result, err)
	}
	requireOneCompactionWrite(t, log)
}

func TestNativeCompactionAndChatClaimOneClientLifetime(t *testing.T) {
	c, log := openCompactionPeer(t, "compact-race")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := make(chan struct{})
	var group sync.WaitGroup
	var compact app.PendingNativeCompaction
	var prompt app.PendingPrompt
	var compactErr, promptErr error
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		compact, compactErr = nativeCompactor(t, c).BeginCompaction(ctx, testFence())
	}()
	go func() {
		defer group.Done()
		<-start
		prompt, promptErr = c.Send(ctx, app.PromptRequest{Turn: pa.Turn{ID: "turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Text: "fixed"}}, Text: "hello"})
	}()
	close(start)
	group.Wait()
	if compactErr == nil {
		if promptErr == nil || prompt != nil {
			t.Fatal("chat raced into maintenance")
		}
		if _, err := compact.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	} else {
		if promptErr != nil || prompt == nil {
			t.Fatal("both operations failed without a winner", compactErr, promptErr)
		}
		if got, err := prompt.Wait(ctx); err != nil || got.FinalText != "Normal answer" {
			t.Fatal(got, err)
		}
	}
	requireOneCompactionWrite(t, log)
}

func TestNativeCompactionIsolationKeepsUnsupportedExplicit(t *testing.T) {
	c := &Client{fence: testFence(), sessionID: "own-session"}
	for _, session := range []app.ProviderSession{c, struct{ app.ProviderSession }{c}} {
		wrapped := &isolatedNativeSession{ProviderSession: session}
		if p, err := nativeCompactor(t, wrapped).BeginCompaction(context.Background(), testFence()); p != nil || pa.Code(err) != pa.Unsupported {
			t.Fatal("isolation invented compaction authority", p, err)
		}
	}
}
