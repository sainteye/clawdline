package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func testLimits() Limits {
	return Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Millisecond * 100, CleanupTimeout: time.Second, RPCTimeout: time.Second * 3, DescendantPoll: time.Millisecond * 10}
}
func testFence() pa.Fence {
	return pa.Fence{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "repo", Kind: pa.RepositoryProject}, ConversationID: "chat", BindingID: "binding", Generation: 1}
}

// TestACPTestPeer runs only in a dedicated owned child process. It does not
// invoke an external provider, model, auth store or network endpoint.
func TestACPTestPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_ACP_TEST_PEER") != "1" {
		return
	}
	mode := os.Getenv("CLAWDLINE_ACP_TEST_MODE")
	if mode == "no-read" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "descendant" {
		cmd := exec.Command("/usr/bin/python3", "-c", "import os,time; os.setsid(); open(os.environ['CLAWDLINE_ACP_PID_FILE'],'w').write(str(os.getpid())); time.sleep(120)")
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
	}
	if mode == "stderr" {
		fmt.Fprint(os.Stderr, strings.Repeat("x", 8192))
	}
	writer := bufio.NewWriter(os.Stdout)
	send := func(f frame) {
		f.JSONRPC = "2.0"
		b, _ := json.Marshal(f)
		writer.Write(b)
		writer.WriteByte('\n')
		writer.Flush()
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 2<<20)
	var promptID json.RawMessage
	for scanner.Scan() {
		var f frame
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(3)
		}
		switch f.Method {
		case "initialize":
			if strings.HasPrefix(mode, "configure-") {
				name := adapterPackage(pa.Codex)
				if strings.Contains(mode, "claude") {
					name = adapterPackage(pa.ClaudeCode)
				}
				version := "scripted"
				if strings.Contains(mode, "wrong-version") {
					version = "wrong"
				}
				send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": name, "version": version}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}, "_meta": map[string]any{"claudeCode": map[string]any{}}}})})
			} else {
				send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}})})
			}
		case "session/new":
			if path := os.Getenv("CLAWDLINE_ACP_PARAMS_FILE"); path != "" {
				if err := os.WriteFile(path, scanner.Bytes(), 0600); err != nil {
					os.Exit(5)
				}
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"sessionId": peerSessionID("CLAWDLINE_ACP_NEW_ID")})})
		case "session/load":
			if path := os.Getenv("CLAWDLINE_ACP_PARAMS_FILE"); path != "" {
				if err := os.WriteFile(path, scanner.Bytes(), 0600); err != nil {
					os.Exit(5)
				}
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"sessionId": peerSessionID("CLAWDLINE_ACP_LOAD_ID")})})
		case "session/prompt":
			promptID = f.ID
			if mode == "quote" || mode == "handoff" {
				var payload struct {
					Prompt []struct {
						Text string `json:"text"`
					} `json:"prompt"`
				}
				if json.Unmarshal(f.Params, &payload) != nil || mode == "quote" && len(payload.Prompt) != 2 || mode == "handoff" && len(payload.Prompt) != 1 {
					os.Exit(4)
				}
				texts := []string{}
				for _, block := range payload.Prompt {
					texts = append(texts, block.Text)
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn", "text": strings.Join(texts, "\n")})})
				continue
			}
			if mode == "exit" {
				os.Exit(8)
			}
			if mode == "silent" {
				continue
			}
			if mode == "permission" {
				send(frame{ID: raw("permission-rpc"), Method: "session/request_permission", Params: raw(map[string]any{"sessionId": "scripted-session", "toolCall": map[string]string{"kind": "read"}, "options": []any{map[string]string{"optionId": "read-once", "name": "Read once", "kind": "allow_once"}, map[string]string{"optionId": "always", "name": "Always", "kind": "allow_always"}, map[string]string{"optionId": "reject", "name": "Reject", "kind": "reject_once"}}})})
				continue
			}
			if mode == "toolcall" {
				// Native tool use reports content as an array, not one block.
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call-1", "title": "Read notes.md", "kind": "read", "status": "pending", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "notes"}}}}})})
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{}}})})
			}
			if mode == "explicit" {
				send(frame{ID: raw("message-rpc"), Method: "clawdline/send_message", Params: raw(map[string]string{"sessionId": "scripted-session", "messageId": "one", "text": "explicit"})})
			}
			if mode == "overflow" {
				fmt.Fprintln(os.Stdout, strings.Repeat("x", 2<<20))
				continue
			}
			send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "partial"}}})})
			send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
		case "session/cancel":
			send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "cancelled"})})
		case "":
			if string(f.ID) == `"permission-rpc"` {
				send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "end_turn", "text": "after permission"})})
			}
		}
	}
	os.Exit(0)
}

// peerSessionID lets a test choose what native session ID the peer answers.
func peerSessionID(name string) string {
	if id := os.Getenv(name); id != "" {
		return id
	}
	return "scripted-session"
}

func peerConfiguration(t *testing.T, mode string) Configuration {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Configuration{Executable: exe, Arguments: []string{"-test.run=^TestACPTestPeer$"}, Environment: []string{"CLAWDLINE_ACP_TEST_PEER=1", "CLAWDLINE_ACP_TEST_MODE=" + mode, "GORACE=atexit_sleep_ms=0"}}
}
func openTestClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error) (*Client, *Process) {
	t.Helper()
	return openClassifiedTestClient(t, mode, sink, true)
}

// openClassifiedTestClient sets what the payload classifier says about every
// native permission request.
func openClassifiedTestClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error, readOnly bool) (*Client, *Process) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows native supervisor is explicitly unavailable")
	}
	if inventoryUnavailable() {
		t.Skip("process inventory is unavailable; safe startup admission must refuse")
	}
	l := testLimits()
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, mode), Directory: t.TempDir(), Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(p, l, testFence(), sink, func([]byte) bool { return readOnly })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		select {
		case <-p.done:
			p.mu.Lock()
			waitErr := p.waitErr
			p.mu.Unlock()
			if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 66 {
				t.Errorf("scripted peer race detector failed: %v; %s", waitErr, p.Stderr())
			}
		default:
			t.Error("direct owned process was not reaped")
		}
	})
	if err = c.initialize(context.Background(), app.OpenRequest{Directory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	return c, p
}
func sendTestPrompt(t *testing.T, c *Client) app.PendingPrompt {
	t.Helper()
	p, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Text: "fixed"}}, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestACPStreamsAndFallbackWaitsForIncoming(t *testing.T) {
	var mu sync.Mutex
	var events []app.ProtocolEvent
	c, _ := openTestClient(t, "explicit", func(_ context.Context, e app.ProtocolEvent) error {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		return nil
	})
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "partial" {
		t.Fatalf("final=%q", result.FinalText)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0].Message != "explicit" || events[1].Trace == nil {
		t.Fatalf("ordered events=%+v", events)
	}
}

func TestACPIncomingPermissionDoesNotWaitForPrompt(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 1)
	c, _ := openTestClient(t, "permission", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		return nil
	})
	pending := sendTestPrompt(t, c)
	var offer pa.PermissionOffer
	select {
	case offer = <-offers:
	case <-time.After(time.Second):
		t.Fatal("permission blocked behind prompt")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "always"); pa.Code(err) != pa.ScopeExpansionDenied {
		t.Fatal(err)
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); err != nil {
		t.Fatal(err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil || result.FinalText != "after permission" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err = c.Answer(context.Background(), offer.NativeRequestID, "read-once"); pa.Code(err) != pa.NotFound {
		t.Fatal("permission repeated", err)
	}
}

func TestACPCancelAndUnknownEffect(t *testing.T) {
	for _, mode := range []string{"silent", "exit", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := openTestClient(t, mode, func(context.Context, app.ProtocolEvent) error { return nil })
			p := sendTestPrompt(t, c)
			if mode == "silent" {
				if err := c.Cancel(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := p.Wait(ctx); err == nil {
				t.Fatal("unknown effect reported successful")
			}
		})
	}
}

func TestACPTimeoutDoesNotReplay(t *testing.T) {
	c, _ := openTestClient(t, "silent", func(context.Context, app.ProtocolEvent) error { return nil })
	p := sendTestPrompt(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	defer cancel()
	if _, err := p.Wait(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "two", Fence: testFence()}, Text: "retry"}); err == nil {
		t.Fatal("dead client accepted another prompt")
	}
}

func TestACPRejectsStaleGenerationBeforeWrite(t *testing.T) {
	c, _ := openTestClient(t, "silent", func(context.Context, app.ProtocolEvent) error { return nil })
	f := testFence()
	f.Generation++
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "wrong", Fence: f}, Text: "hello"}); pa.Code(err) != pa.StaleGeneration {
		t.Fatal(err)
	}
}

func TestACPQuoteBodyTraversesActualStdinAsSeparateBlock(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "quote-turn", Fence: testFence()}, Text: "explain this", Quote: &app.PromptQuote{MessageID: "reference-id", Text: "precise referenced body"}})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.FinalText, "Referenced message (reference-id):\nprecise referenced body") || !strings.HasSuffix(reply.FinalText, "explain this") {
		t.Fatalf("actual peer payload=%q", reply.FinalText)
	}
}

func TestACPFastCloseAfterInitializeKeepsOwnershipReceipt(t *testing.T) {
	for i := 0; i < 5; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			c, p := openTestClient(t, "", func(context.Context, app.ProtocolEvent) error { return nil })
			if err := c.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-p.done:
			default:
				t.Fatal("owned child was not reaped")
			}
		})
	}
}

func processAlive(pid int) bool {
	b, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	return err == nil && strings.TrimSpace(string(b)) != "" && !strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
}

func inventoryUnavailable() bool {
	_, err := exec.Command("/bin/ps", "-axo", "pid=,ppid=,lstart=").Output()
	return err != nil
}

func TestACPFreshSessionHandoffTraversesStdinAndRejectsStaleOpeningBeforeWrite(t *testing.T) {
	c, _ := openTestClient(t, "handoff", func(context.Context, app.ProtocolEvent) error { return nil })
	f := testFence()
	memory := pa.MemoryRevision{Key: f.Key, Project: f.Project, Revision: 2}
	filtered := pa.ContextSnapshot{Memory: memory, ProvenanceComplete: true, Text: "Filtered handoff summary", Sources: []pa.MemorySource{{ID: "verified-source", Key: f.Key, Project: f.Project, Revision: 1, Kind: "human"}}}
	turn := pa.Turn{ID: "fresh-turn", Fence: f, MemoryRevision: 2, Instructions: pa.InstructionSnapshot{Revision: 3, Text: "Captured standing instructions", RoleSnapshotRef: "role@3"}}
	handoff := &app.SessionHandoff{Identity: f, Agent: pa.Codex, Instructions: turn.Instructions, Context: filtered, Bookmark: app.SessionBookmark{MessageID: "stored-bookmark", MessageSeq: 42}, Unfinished: []app.SessionPending{{Kind: "work", ID: "old-work", State: "unknown", TaskID: "task-reference"}}}
	stale := *handoff
	stale.Instructions.RoleSnapshotRef = "stale-role"
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Context: filtered, Text: "New input", Handoff: &stale}); pa.Code(err) != pa.ContextStale {
		t.Fatal("stale opening entered native prompt", err)
	}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Context: filtered, Text: "New input", Handoff: handoff})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Filtered handoff summary", "Captured standing instructions", "stored-bookmark", "task-reference", "role@3", "New input"} {
		if !strings.Contains(result.FinalText, text) {
			t.Fatal("actual ACP wire omitted", text)
		}
	}
	if strings.Count(result.FinalText, "Filtered handoff summary") != 1 {
		t.Fatal("handoff memory duplicated")
	}
}

// As in CodyNC, the person may allow an action the host cannot prove
// read-only, once. A persistent rule is still refused.
func TestACPPersonMayAllowAChangingActionOnce(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 1)
	c, _ := openClassifiedTestClient(t, "permission", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		return nil
	}, false)
	pending := sendTestPrompt(t, c)
	var offer pa.PermissionOffer
	select {
	case offer = <-offers:
	case <-time.After(time.Second):
		t.Fatal("no permission request")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "always"); pa.Code(err) != pa.ScopeExpansionDenied {
		t.Fatalf("a persistent allow rule was accepted: %v", err)
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); err != nil {
		t.Fatalf("the person's one-time allow was refused: %v", err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil || result.FinalText != "after permission" {
		t.Fatalf("the allowed action did not continue the turn: %+v %v", result, err)
	}
}

// With native tools available, ACP reports tool calls whose content is an
// array. Such an update is not a chat chunk and must not end the turn.
func TestACPToolCallUpdatesDoNotEndTheTurn(t *testing.T) {
	c, _ := openTestClient(t, "toolcall", func(context.Context, app.ProtocolEvent) error { return nil })
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	if err != nil || result.FinalText != "partial" {
		t.Fatalf("a tool call update ended the turn: %+v %v", result, err)
	}
}

// As in Codync, a native session hears its instructions (system prompt) and
// memory (the fresh session's handoff) once; an ordinary later prompt carries
// only the person's text.
func TestALaterPromptCarriesOnlyThePersonsText(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	turn := pa.Turn{ID: "later-turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Revision: 1, Text: "STANDING-INSTRUCTIONS"}}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Text: "next question", Context: pa.ContextSnapshot{Text: "SAVED-MEMORY"}, Quote: &app.PromptQuote{MessageID: "q", Text: "quoted"}})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.FinalText, "STANDING-INSTRUCTIONS") || strings.Contains(reply.FinalText, "SAVED-MEMORY") || !strings.HasSuffix(reply.FinalText, "next question") {
		t.Fatalf("prompt repeated the snapshot: %q", reply.FinalText)
	}
}

// As in Codync, when memory changed since a resumed session last heard it,
// its next prompt carries all of memory as it is now before the text.
func TestAResumedPromptAfterAMemoryChangeCarriesTheWholeMemory(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	turn := pa.Turn{ID: "changed-turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Revision: 1, Text: "STANDING-INSTRUCTIONS"}}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Text: "next question", Context: pa.ContextSnapshot{Text: "Has two cats\nLikes tea"}, Quote: &app.PromptQuote{MessageID: "q", Text: "quoted"}, MemoryChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.FinalText, "<memory_update>\nYour memory changed.") || !strings.Contains(reply.FinalText, "Has two cats\nLikes tea\n</memory_update>\nnext question") || strings.Contains(reply.FinalText, "STANDING-INSTRUCTIONS") {
		t.Fatalf("memory update missing: %q", reply.FinalText)
	}
}
