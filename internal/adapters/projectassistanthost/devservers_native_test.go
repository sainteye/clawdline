package projectassistanthost

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Runs only in the synthetic Go ACP subprocess. All requests use the actual
// owned MCP descriptor supplied by the Host, not an in-process signing port.
func runSourceServerMCPPeer(request func(string, any, any) []byte, call func(string, any) []byte, check func([]byte, bool)) {
	if raw := request("tools/list", 1, nil); !strings.Contains(string(raw), `"start_dev_server"`) {
		os.Exit(81)
	}
	decode := func(raw []byte) app.DevServerResult {
		check(raw, false)
		var reply struct {
			Result struct{ Content []struct{ Text string } }
		}
		var result app.DevServerResult
		if json.Unmarshal(raw, &reply) != nil || len(reply.Result.Content) != 1 || json.Unmarshal([]byte(reply.Result.Content[0].Text), &result) != nil {
			os.Exit(82)
		}
		return result
	}
	waitRunning := func(id string) {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			listed := decode(call("list_dev_servers", map[string]any{}))
			for _, server := range listed.Servers {
				if server.ID == id && server.State == "running" {
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
		os.Exit(83)
	}
	args := map[string]any{"command": "synthetic-server --foreground", "name": "Synthetic source command", "idempotency_key": "source-native-start"}
	first := decode(call("start_dev_server", args))
	if first.Server == nil {
		os.Exit(84)
	}
	firstID, firstAttempt := first.Server.ID, first.Server.Attempt
	again := decode(call("start_dev_server", args))
	if again.Server == nil || again.Server.ID != firstID || again.Server.Attempt != firstAttempt {
		os.Exit(85)
	}
	waitRunning(firstID)
	logs := decode(call("dev_server_logs", map[string]any{"id": firstID, "lines": 1}))
	if logs.Log == nil {
		os.Exit(86)
	}
	check(call("start_dev_server", map[string]any{"command": "synthetic", "idempotency_key": "forged", "bot_id": "foreign"}), true)
	check(call("stop_dev_server", map[string]any{"id": firstID}), true)
	stop := map[string]any{"id": firstID, "idempotency_key": "source-native-stop"}
	stopped := decode(call("stop_dev_server", stop))
	if stopped.Server == nil || stopped.Server.State != "stopped" {
		os.Exit(87)
	}
	args["idempotency_key"] = "source-native-restart"
	restarted := decode(call("start_dev_server", args))
	if restarted.Server == nil || restarted.Server.ID != firstID || restarted.Server.Attempt == firstAttempt {
		os.Exit(88)
	}
	waitRunning(firstID)
	decode(call("stop_dev_server", stop)) // Old cached stop must not hit the restart.
	waitRunning(firstID)
}

func TestHostActualOwnedMCPStartsRestartsAndStopsOnlyTheBoundSourceServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows native supervisor unavailable")
	}
	f := newHostFixture(t)
	f.o.Limits.PromptTimeout = 15 * time.Second
	var launched atomic.Int32
	started := make(chan *hostServerProcess, 4)
	f.o.ServerLaunch = func(context.Context, servers.Spec, pa.ProjectResolution) (servers.Process, error) {
		launched.Add(1)
		p := &hostServerProcess{exit: make(chan struct{})}
		started <- p
		return p, nil
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.o.Provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: executable, Arguments: []string{"-test.run=^TestHostOwnedMCPPeer$"}, Environment: []string{"CLAWDLINE_HOST_MCP_PEER=1", "CLAWDLINE_SERVER_ONLY=1", "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.SourceProcessLimits(), Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "Synthetic Go ACP/MCP peer; zero vendor/model calls", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, SendMessage: true}, nil
		}})
	}
	f.start(t)
	f.enable(t)
	bot := f.create(t, "native-source-server")
	if _, err := f.send(t, bot, "start"); err != nil {
		t.Fatal(err)
	}
	offer := hostSourceOffer(t, f, bot.Key)
	service, _ := f.h.Service()
	if _, err := service.Answer(context.Background(), pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "actual-mcp-always"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: string(pa.AllowAlways)}); err != nil {
		t.Fatal(err)
	}
	first := fixtureReceive(t, started)
	second := fixtureReceive(t, started)
	deadline := time.Now().Add(15 * time.Second)
	for {
		x, err := f.h.store.Export(context.Background(), bot.Key)
		if err != nil {
			t.Fatal(err)
		}
		if x.Conversation.ActiveTurnID == "" {
			if len(x.Turns) != 1 || x.Turns[0].State != pa.TurnSucceeded {
				t.Fatal("actual MCP route failed", x.Turns)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual MCP source tools did not finish", x.Turns)
		}
		time.Sleep(time.Millisecond)
	}
	if launched.Load() != 2 || !first.closed.Load() || second.closed.Load() {
		t.Fatal("retry/old stop/native cleanup changed source ownership", launched.Load(), first.closed.Load(), second.closed.Load())
	}
	if err := f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !second.closed.Load() {
		t.Fatal("Host disable lost its second source process")
	}
}
