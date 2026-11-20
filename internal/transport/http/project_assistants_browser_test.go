package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A Bot is not a terminal Session. The machine-authenticated CLI route keeps
// refusing its native conversation before any browser admission, while the
// Host-owned path reaches the same engine under the Host-derived owner.
func TestBotBrowserUsesTheHostPathAndTheCLIRouteStillRefusesIt(t *testing.T) {
	const terminal = "20000000-0000-4000-8000-000000000061"
	const botNative = "20000000-0000-4000-8000-000000000062"
	live := session.Session{ID: "pane-root", Backend: session.BackendTmux, Assistant: session.AssistantClaude, ConversationID: terminal, State: session.StateWorking}
	s := paneServer(t, &pane{s: live})
	s.cfg.Dir = t.TempDir()
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{live} }
	if _, err := s.settingsFile().Set(map[string]any{"browser_policy": json.RawMessage(`{"order":["isolated","native","user_chrome"],"automatic":{"native":false,"isolated":true,"user_chrome":false}}`)}); err != nil {
		t.Fatal(err)
	}
	bridge, _ := s.browsers().Channel("isolated")
	probe, cancel := context.WithCancel(t.Context())
	cancel()
	bridge.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"})
	hostCtx, stop := context.WithCancel(t.Context())
	defer stop()
	var ops []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for hostCtx.Err() == nil {
			for _, c := range bridge.Poll(hostCtx, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"}) {
				ops = append(ops, c.Op)
				tab := c.Tab
				if c.Op == "open" {
					tab = 41
				}
				_ = s.browsers().Deliver("isolated", []browserbridge.Result{{ID: c.ID, OK: true, Tab: tab, Value: json.RawMessage(`{"text":"scripted page"}`)}})
			}
		}
	}()
	defer func() { stop(); <-done }()

	f, _ := newGateFixture(t)
	handler := f.g.wrap(http.HandlerFunc(s.browserCommandRoute))
	raw, _ := json.Marshal(map[string]any{"conversation": botNative, "op": "open", "args": map[string]any{"url": "http://127.0.0.1:9/page"}})
	answer := (call{method: "POST", path: "/v1/browser/commands", body: string(raw), headers: map[string]string{"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}).do(handler)
	if answer.Code != 403 || !strings.Contains(answer.Body.String(), "session_unresolved") {
		t.Fatalf("CLI route for a Bot conversation: %d %s", answer.Code, answer.Body)
	}
	if len(ops) != 0 || bridge.Status().Pending != 0 {
		t.Fatalf("the CLI refusal admitted a command: %v", ops)
	}

	access := s.projectAssistantBrowser()
	owner := projectassistanthost.BrowserOwner(pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}, Project: pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "c", Kind: pa.RepositoryProject}, ConversationID: "conv"}, 0)
	if _, err := access.Command(t.Context(), owner, app.BrowserCommand{Op: "open", Args: json.RawMessage(`{"url":"http://127.0.0.1:9/page"}`)}); err != nil {
		t.Fatal(err)
	}
	r, err := access.Command(t.Context(), owner, app.BrowserCommand{Op: "read"})
	if err != nil || string(r.Value) != `{"text":"scripted page"}` {
		t.Fatalf("read = %s %v", r.Value, err)
	}
	if bridge.OwnerOf(41) != owner {
		t.Fatalf("tab owner = %q", bridge.OwnerOf(41))
	}
	// The terminal Session cannot drive the Bot's tab, nor the Bot a foreign one.
	_, err = s.browsers().DoSession(t.Context(), terminal, "read", json.RawMessage(`{"tab":41}`), mustBrowserPolicy(t, s), "", false)
	if r, _ := browserbridge.IsRefusal(err); r != browserbridge.OtherSessionsTab {
		t.Fatalf("a Session read the Bot's tab: %v", err)
	}
	_, err = access.Command(t.Context(), owner, app.BrowserCommand{Op: "read", Args: json.RawMessage(`{"tab":77}`)})
	var typed *app.BrowserFailure
	if !errors.As(err, &typed) || typed.Code != string(browserbridge.OtherSessionsTab) {
		t.Fatalf("an unowned tab: %v", err)
	}
	_, err = access.Command(t.Context(), owner, app.BrowserCommand{Op: "open", Args: json.RawMessage(`{"url":"http://127.0.0.1:9/page"}`), Mode: "user_chrome"})
	if !errors.As(err, &typed) || typed.Code != string(browserbridge.Disabled) {
		t.Fatalf("a disabled browser by name: %v", err)
	}
	status, err := access.Status(t.Context(), owner)
	if err != nil || status.Mode != "isolated" || len(status.Tabs) != 1 || status.Tabs[0].Tab != 41 {
		t.Fatalf("status = %+v %v", status, err)
	}
	if strings.Join(ops, ",") != "open,read" {
		t.Fatalf("host saw %v", ops)
	}
}

func mustBrowserPolicy(t *testing.T, s *Server) nextconfig.BrowserPolicy {
	t.Helper()
	p, err := s.browserPolicy()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// browserPeerLine is one owned MCP browser call the scripted ACP process made,
// as it saw the result over TCP.
type browserPeerLine struct {
	Turn    string          `json:"turn"`
	Op      string          `json:"op"`
	IsError bool            `json:"is_error"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Types   []string        `json:"types"`
	Value   json.RawMessage `json:"value,omitempty"`
	// Image is the decoded size of an MCP image block, "WxH".
	Image string `json:"image,omitempty"`
}

// TestBotBrowserACPSubprocess is the scripted agent: it holds only the owned
// MCP descriptor ACP session/new gave it, never a daemon token or Go callback.
// It is not a model and not Claude Code or Codex.
func TestBotBrowserACPSubprocess(t *testing.T) {
	logPath := os.Getenv("CLAWDLINE_BROWSER_PEER_LOG")
	if logPath == "" {
		return
	}
	record := func(v any) {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(40)
		}
		b, _ := json.Marshal(v)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	fail := func(why string) {
		record(map[string]string{"failure": why})
		os.Exit(41)
	}
	write := func(id json.RawMessage, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	var endpoint, token string
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	rpc := func(method string, id any, params any) []byte {
		frame := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != nil {
			frame["id"] = id
		}
		if params != nil {
			frame["params"] = params
		}
		body, _ := json.Marshal(frame)
		r, e := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		if e != nil {
			fail("request")
		}
		r.Header.Set("Authorization", token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, e := client.Do(r)
		if e != nil {
			fail("transport " + e.Error())
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || resp.StatusCode >= 400 {
			fail(fmt.Sprintf("HTTP %d %s", resp.StatusCode, b))
		}
		return b
	}
	var turn string
	call := func(name string, args any) browserPeerLine {
		b := rpc("tools/call", "tool", map[string]any{"name": name, "arguments": args})
		var v struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Type, Text, Data, MimeType string
				}
			}
			Error json.RawMessage
		}
		if json.Unmarshal(b, &v) != nil || len(v.Error) > 0 || len(v.Result.Content) == 0 {
			fail("tools/call " + string(b))
		}
		line := browserPeerLine{Turn: turn, Op: name, IsError: v.Result.IsError, Types: []string{}}
		if m, ok := args.(map[string]any); ok && m["op"] != nil {
			line.Op = fmt.Sprint(m["op"])
		}
		for _, c := range v.Result.Content {
			line.Types = append(line.Types, c.Type)
			switch c.Type {
			case "text":
				line.Value = json.RawMessage(c.Text)
				var code struct{ Code, Message string }
				if line.IsError && json.Unmarshal(line.Value, &code) == nil {
					line.Code, line.Message = code.Code, code.Message
				}
			case "image":
				raw, err := base64.StdEncoding.DecodeString(c.Data)
				if err != nil || c.MimeType != "image/jpeg" {
					fail("image block")
				}
				img, err := jpeg.Decode(bytes.NewReader(raw))
				if err != nil {
					fail("jpeg " + err.Error())
				}
				line.Image = fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy())
			}
		}
		record(line)
		return line
	}
	command := func(op string, args map[string]any, mode string) browserPeerLine {
		q := map[string]any{"op": op}
		if args != nil {
			q["args"] = args
		}
		if mode != "" {
			q["browser_mode"] = mode
		}
		return call("browser_command", q)
	}
	// await polls the Bot's own status until the person's action is visible.
	await := func(ok func(app.BrowserStatus) bool) {
		var b []byte
		for i := 0; i < 500; i++ {
			b = rpc("tools/call", "tool", map[string]any{"name": "browser_status", "arguments": map[string]any{}})
			var v struct {
				Result struct{ Content []struct{ Text string } }
			}
			var st app.BrowserStatus
			if json.Unmarshal(b, &v) == nil && len(v.Result.Content) == 1 && json.Unmarshal([]byte(v.Result.Content[0].Text), &st) == nil && ok(st) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		fail("await status " + string(b))
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			fail("stdin")
		}
		switch q.Method {
		case "initialize":
			write(q.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new", "session/load":
			var p struct {
				MCPServers []struct {
					URL     string
					Headers []struct{ Name, Value string }
				} `json:"mcpServers"`
			}
			if json.Unmarshal(q.Params, &p) != nil || len(p.MCPServers) != 1 || len(p.MCPServers[0].Headers) != 1 {
				fail("session descriptor " + string(q.Params))
			}
			endpoint, token = p.MCPServers[0].URL, p.MCPServers[0].Headers[0].Value
			record(map[string]string{"acp": q.Method})
			// The test replays this descriptor after the turn has ended.
			record(map[string]string{"descriptor": endpoint, "authorization": token})
			write(q.ID, map[string]string{"sessionId": "browser-peer"})
		case "session/prompt":
			rpc("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "browser-peer", "version": "1"}})
			rpc("notifications/initialized", nil, nil)
			list := string(rpc("tools/list", 2, map[string]any{}))
			if !strings.Contains(list, `"browser_status"`) || !strings.Contains(list, `"browser_command"`) {
				fail("tools/list")
			}
			// A fresh-history prompt quotes recent turns before the new input, so
			// only the newest marker names this turn's script.
			prompt := newestBrowserMarker(string(q.Params))
			reply := ""
			switch {
			case prompt == "browser-drive":
				turn = "drive"
				open := command("open", map[string]any{"url": "http://127.0.0.1:9/form"}, "")
				call("browser_status", map[string]any{})
				command("read", nil, "")
				command("click", map[string]any{"label": "Count"}, "")
				command("type", map[string]any{"text": "Ada"}, "")
				last := command("read", nil, "")
				command("screenshot", nil, "")
				command("read", map[string]any{"tab": 77}, "")
				command("close", nil, "")
				call("browser_status", map[string]any{})
				if open.IsError || last.IsError {
					fail("drive")
				}
				var page struct {
					Value struct {
						Clicks int
						Text   string
					}
				}
				_ = json.Unmarshal(last.Value, &page)
				reply = fmt.Sprintf("Browser run complete: clicks=%d text=%s", page.Value.Clicks, page.Value.Text)
			case prompt == "browser-control":
				turn = "control"
				command("open", map[string]any{"url": "http://127.0.0.1:9/control"}, "")
				command("click", map[string]any{"label": "Missing"}, "")
				command("open", map[string]any{"url": "http://127.0.0.1:9/other"}, "user_chrome")
				// The scripted host takes the tab over for the person after this key.
				command("key", map[string]any{"key": "TakeOver"}, "")
				await(func(s app.BrowserStatus) bool { return len(s.Tabs) == 1 && s.Tabs[0].Person })
				command("read", nil, "")
				await(func(s app.BrowserStatus) bool { return len(s.Tabs) == 1 && !s.Tabs[0].Person })
				command("read", nil, "")
				// ... and stops the channel after this one.
				command("key", map[string]any{"key": "Stop"}, "")
				await(func(s app.BrowserStatus) bool { return s.Stopped })
				command("read", nil, "")
				await(func(s app.BrowserStatus) bool { return !s.Stopped })
				var wg sync.WaitGroup
				wg.Add(1)
				go func() { defer wg.Done(); command("close", nil, "") }()
				time.Sleep(150 * time.Millisecond)
				command("read", nil, "")
				wg.Wait()
				reply = "Browser control checks complete"
			case strings.HasPrefix(prompt, "browser-probe-"):
				turn = "probe"
				mode := ""
				if prompt == "browser-probe-user-chrome" {
					mode = "user_chrome"
				}
				command("open", map[string]any{"url": "http://127.0.0.1:9/probe"}, mode)
				reply = "Browser probe complete"
			default:
				fail("no turn marker in prompt")
			}
			write(q.ID, map[string]string{"stopReason": "end_turn", "text": reply})
		}
	}
	os.Exit(0)
}

// newestBrowserMarker returns whichever turn marker the test sends starts
// last in the prompt; a prompt naming none of them returns "".
func newestBrowserMarker(prompt string) string {
	newest, at := "", -1
	for _, marker := range []string{"browser-drive", "browser-control", "browser-probe-remembered", "browser-probe-user-chrome", "browser-probe-native"} {
		if i := strings.LastIndex(prompt, marker); i > at {
			newest, at = marker, i
		}
	}
	return newest
}

// scriptedNativeHost stands in for the native host and extension behind the
// production Bridge: it Polls and Delivers. It is not Chrome.
type scriptedNativeHost struct {
	bridge *browserbridge.Bridge
	// profiles is where the person's stop and takeover land, as the
	// console's /v1/browser routes call them.
	profiles *browserbridge.Profiles
	shot     string
	mu       sync.Mutex
	ops      []browserbridge.Command
	clicks   int
	text     string
	next     int
}

func (h *scriptedNativeHost) seen() []browserbridge.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]browserbridge.Command(nil), h.ops...)
}

func (h *scriptedNativeHost) serve(ctx context.Context, t *testing.T) {
	for ctx.Err() == nil {
		for _, c := range h.bridge.Poll(ctx, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"}) {
			h.mu.Lock()
			h.ops = append(h.ops, c)
			h.mu.Unlock()
			tab := c.Tab
			result := browserbridge.Result{ID: c.ID, OK: true, Tab: tab, Value: json.RawMessage(`{"ok":true}`)}
			var a struct{ Label, Text, Key, URL string }
			_ = json.Unmarshal(c.Args, &a)
			after := func() {}
			switch c.Op {
			case "open":
				h.next++
				result.Tab = 40 + h.next
				result.URL = a.URL
			case "click":
				if a.Label == "Missing" {
					result = browserbridge.Result{ID: c.ID, Tab: tab, Error: "no element labelled Missing"}
				} else {
					h.clicks++
				}
			case "type":
				h.text = a.Text
			case "read":
				result.Value, _ = json.Marshal(map[string]any{"clicks": h.clicks, "text": h.text})
			case "screenshot":
				result.Value, _ = json.Marshal(map[string]string{"data": h.shot, "url": "http://127.0.0.1:9/form"})
			case "key":
				switch a.Key {
				// The person acts once the key's result has been taken, then
				// ends the action; each change is logged with its error.
				case "TakeOver":
					after = func() {
						time.Sleep(100 * time.Millisecond)
						t.Logf("take over: %v", h.profiles.SetPersonControl("isolated", tab, true))
						time.AfterFunc(time.Second, func() { t.Logf("hand back: %v", h.profiles.SetPersonControl("isolated", tab, false)) })
					}
				case "Stop":
					after = func() {
						time.Sleep(100 * time.Millisecond)
						t.Logf("stop: %v", h.profiles.SetStopped(true))
						time.AfterFunc(time.Second, func() { t.Logf("resume: %v", h.profiles.SetStopped(false)) })
					}
				}
			case "close":
				time.Sleep(400 * time.Millisecond)
			}
			if err := h.bridge.Deliver([]browserbridge.Result{result}); err != nil {
				t.Errorf("deliver %s: %v", c.Op, err)
			}
			after()
		}
	}
}

func scriptedJPEG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for x := 0; x < 4; x++ {
		for y := 0; y < 3; y++ {
			img.Set(x, y, color.RGBA{R: uint8(60 * x), G: 90, B: uint8(80 * y), A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func readBrowserPeer(t *testing.T, path string) (lines []browserPeerLine, acpCalls []string, failures []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var probe map[string]any
		if json.Unmarshal([]byte(s), &probe) != nil {
			t.Fatalf("peer log %q", s)
		}
		if v, ok := probe["acp"]; ok {
			acpCalls = append(acpCalls, fmt.Sprint(v))
			continue
		}
		if _, ok := probe["descriptor"]; ok {
			continue
		}
		if v, ok := probe["failure"]; ok {
			failures = append(failures, fmt.Sprint(v))
			continue
		}
		var l browserPeerLine
		_ = json.Unmarshal([]byte(s), &l)
		lines = append(lines, l)
	}
	return lines, acpCalls, failures
}

// One scripted ACP subprocess, launched by the production ACP provider with
// the formal ACP handshake, receives the Host-owned HTTP MCP descriptor in
// session/new, initializes MCP over real TCP, lists and calls browser_status
// and browser_command. Each command passes Host binding checks, the
// production Profiles and Bridge, and a scripted native host's Poll/Deliver,
// and returns through MCP content to the process, whose reply is read back
// from SQLite. Nothing here is Chrome, a model, Claude Code or Codex.
func TestBotBrowserRunsEndToEndThroughACPAndOwnedMCP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native supervisor unavailable")
	}
	f := newPAServerFixture(t, true)
	setPolicy := func(policy string) {
		t.Helper()
		if _, err := f.s.settingsFile().Set(map[string]any{"browser_policy": json.RawMessage(policy)}); err != nil {
			t.Fatal(err)
		}
	}
	setPolicy(`{"order":["isolated","native","user_chrome"],"automatic":{"native":false,"isolated":true,"user_chrome":false}}`)
	bridge, err := f.s.browsers().Channel("isolated")
	if err != nil {
		t.Fatal(err)
	}
	probe, cancel := context.WithCancel(t.Context())
	cancel()
	bridge.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"})
	native := &scriptedNativeHost{bridge: bridge, profiles: f.s.browsers(), shot: scriptedJPEG(t)}
	hostCtx, stopHost := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { defer close(served); native.serve(hostCtx, t) }()
	defer func() { stopHost(); <-served }()

	logPath := filepath.Join(t.TempDir(), "browser-peer.log")
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := os.ReadFile(logPath)
			t.Logf("scripted peer log:\n%s\nnative host saw %d commands", b, len(native.seen()))
		}
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var opens, sends int
	var counted sync.Mutex
	project := pa.ProjectScope{MachineID: f.key.MachineID, ProjectID: "browser-project", CanonicalID: "browser-canonical", Kind: pa.RepositoryProject}
	resolver := collaborationProjects{directory: f.project, scopes: map[string]pa.ProjectScope{project.ProjectID: project}}
	provider := func() (app.Provider, error) {
		config := acp.Configuration{Executable: exe, Arguments: []string{"-test.run=^TestBotBrowserACPSubprocess$"}, Environment: []string{"CLAWDLINE_BROWSER_PEER_LOG=" + logPath, "GORACE=atexit_sleep_ms=0"}}
		p, e := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: config}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 20 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			// The owned MCP descriptor is offered only to an adapter verified
			// to carry it, as the collaboration fixture declares.
			c := paServerCaps(b)
			c.SendMessage = true
			return c, nil
		}})
		return countingProvider{Provider: p, open: func() { counted.Lock(); opens++; counted.Unlock() }, send: func() { counted.Lock(); sends++; counted.Unlock() }}, e
	}
	hosted, err := projectassistanthost.New(context.Background(), projectassistanthost.Options{Database: f.s.store.ProjectAssistantDatabase(), Reader: f.s.store.ProjectAssistantReader(), MachineID: f.key.MachineID, Projects: resolver, Broker: f.s.broker, Clock: time.Now, ID: projectAssistantID, Limits: app.RuntimeLimits{MaxBots: 2, MaxProcesses: 2, PromptTimeout: 30 * time.Second, CleanupTimeout: 2 * time.Second}, Authority: f.s.projectAssistantAuthority, Source: f.s.projectAssistantSource, Provider: provider, ReadViewByteLimit: 1 << 20, Browser: f.s.projectAssistantBrowser()})
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.host = hosted
	f.s.projectAssistants.machine = f.identity.MachineID
	f.s.projectAssistants.account = f.identity.AccountID
	h := &paSoakHTTP{fixture: f, server: httptest.NewServer(f.h)}
	h.reconnect()
	defer h.server.Close()
	defer h.client.CloseIdleConnections()
	if h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": true}, nil) != 200 {
		t.Fatal("enable")
	}
	f.key.BotID = "browser-bot"
	q := f.create()
	q.Settings.Target = paWireProjectSelection(pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID})
	if h.request(t, "POST", "/v1/project-assistants", q, nil) != 200 {
		t.Fatal("create")
	}
	st := store.New(f.s.store.ProjectAssistantDatabase())
	turnFinished := func(id string) app.Export {
		t.Helper()
		_, admission := h.send(t, id)
		x := paSoakAwait(t, f, admission.ResourceID, pa.TurnSucceeded)
		lines, _, failures := readBrowserPeer(t, logPath)
		if len(failures) > 0 {
			t.Fatalf("scripted peer failed: %v %+v", failures, lines)
		}
		return x
	}

	// 1. open → read → click → type → read → screenshot → close.
	x := turnFinished("browser-drive")
	owner := projectassistanthost.BrowserOwner(pa.Fence{Key: f.key, Project: project, ConversationID: x.Conversation.ID}, 0)
	lines, acpCalls, _ := readBrowserPeer(t, logPath)
	got := map[string]browserPeerLine{}
	var order []string
	for _, l := range lines {
		order = append(order, l.Op+map[bool]string{true: "!" + l.Code, false: ""}[l.IsError])
		if !l.IsError {
			got[l.Op] = l
		}
	}
	want := "open,browser_status,read,click,type,read,screenshot,read!browser_tab_other_session,close,browser_status"
	if strings.Join(order, ",") != want {
		t.Fatalf("MCP results = %v\nwant %s", order, want)
	}
	if shot := got["screenshot"]; strings.Join(shot.Types, ",") != "text,image" || shot.Image != "4x3" {
		t.Fatalf("screenshot content = %+v", shot)
	}
	if string(got["read"].Value) != `{"op":"read","value":{"clicks":1,"text":"Ada"}}` {
		t.Fatalf("last read = %s", got["read"].Value)
	}
	var final app.BrowserStatus
	if json.Unmarshal(lines[len(lines)-1].Value, &final) != nil || len(final.Tabs) != 0 || final.Mode != "isolated" {
		t.Fatalf("status after close = %s", lines[len(lines)-1].Value)
	}
	seen := native.seen()
	var ids, ops []string
	for _, c := range seen {
		ids, ops = append(ids, c.ID), append(ops, c.Op)
	}
	if strings.Join(ops, ",") != "open,read,click,type,read,screenshot,close" {
		t.Fatalf("native host received %v", ops)
	}
	// Every executed command and the refused foreign-tab read are audited
	// under the Host-derived owner; the refusal never reached the host.
	rows, _ := bridge.Records(owner, 50)
	if len(rows) != len(seen)+1 {
		t.Fatalf("audit rows %d for %d commands: %+v", len(rows), len(seen), rows)
	}
	refused := 0
	for i, row := range rows {
		if row.Conversation != owner || row.ID == "" {
			t.Fatalf("audit row %d = %+v, owner %s", i, row, owner)
		}
		if row.Error != "" {
			refused++
			if row.Error != string(browserbridge.OtherSessionsTab) || row.Tab != 77 || row.OK {
				t.Fatalf("refused audit row = %+v", row)
			}
		}
	}
	if refused != 1 {
		t.Fatalf("refused audit rows = %d", refused)
	}
	for _, id := range ids {
		found := false
		for _, row := range rows {
			found = found || row.ID == id
		}
		if !found {
			t.Fatalf("command %s has no audit row: %+v", id, rows)
		}
	}
	var reply string
	for _, m := range x.Messages {
		if m.Kind == pa.AssistantMessage {
			reply = m.Text
		}
	}
	if reply != "Browser run complete: clicks=1 text=Ada" {
		t.Fatalf("Bot reply in SQLite = %q (%+v)", reply, x.Messages)
	}
	stored, err := st.Export(context.Background(), f.key)
	if err != nil || len(stored.Turns) != 1 || stored.Turns[0].State != pa.TurnSucceeded {
		t.Fatalf("SQLite turns = %+v %v", stored.Turns, err)
	}
	t.Logf("drive: acp=%v commands=%v owner=%s audit=%d", acpCalls, ids, owner, len(rows))

	// The finished turn's retained MCP descriptor, replayed over TCP, admits
	// nothing: the Host revoked it with the execution.
	raw, _ := os.ReadFile(logPath)
	var retained struct{ Descriptor, Authorization string }
	for _, s := range strings.Split(string(raw), "\n") {
		if strings.Contains(s, `"descriptor"`) {
			_ = json.Unmarshal([]byte(s), &retained)
		}
	}
	if retained.Descriptor == "" {
		t.Fatal("no descriptor was recorded")
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"browser_command","arguments":{"op":"open","args":{"url":"http://127.0.0.1:9/late"}}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"browser_status","arguments":{}}}`,
	} {
		r, _ := http.NewRequest("POST", retained.Descriptor, strings.NewReader(body))
		r.Header.Set("Authorization", retained.Authorization)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}).Do(r)
		status, answerBody := 0, []byte(nil)
		if err == nil {
			status = resp.StatusCode
			answerBody, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		if err == nil && status < 400 && !strings.Contains(string(answerBody), `"isError":true`) {
			t.Fatalf("retained descriptor still worked: %d %s", status, answerBody)
		}
		t.Logf("retained descriptor: err=%v status=%d body=%.120s", err, status, answerBody)
	}
	if len(native.seen()) != len(seen) || bridge.Status().Pending != 0 {
		t.Fatal("a retained descriptor admitted a command")
	}

	// The CLI route still refuses this Bot's conversation before admission.
	g, _ := newGateFixture(t)
	handler := g.g.wrap(http.HandlerFunc(f.s.browserCommandRoute))
	raw, _ = json.Marshal(map[string]any{"conversation": x.Conversation.ID, "op": "open", "args": map[string]any{"url": "http://127.0.0.1:9/page"}})
	answer := (call{method: "POST", path: "/v1/browser/commands", body: string(raw), headers: map[string]string{"X-Clawdline-Orchestrator": g.machine, "Content-Type": "application/json"}}).do(handler)
	if answer.Code != 403 || !strings.Contains(answer.Body.String(), "session_unresolved") || len(native.seen()) != len(seen) {
		t.Fatalf("CLI route for the Bot conversation: %d %s", answer.Code, answer.Body)
	}

	// 2. Typed refusals cross MCP unchanged; the person's takeover and stop
	// refuse without reaching the host, and their end restores the tab.
	before := len(native.seen())
	turnFinished("browser-control")
	lines, _, _ = readBrowserPeer(t, logPath)
	order = order[:0]
	for _, l := range lines {
		if l.Turn == "control" {
			order = append(order, l.Op+map[bool]string{true: "!" + l.Code, false: ""}[l.IsError])
			if l.Code == "browser_page_error" && l.Message != "no element labelled Missing" {
				t.Fatalf("page error message = %q", l.Message)
			}
		}
	}
	want = "open,click!browser_page_error,open!browser_disabled,key,read!browser_person_controlling,read,key,read!browser_stopped,read!browser_tab_closing,close"
	if strings.Join(order, ",") != want {
		t.Fatalf("control results = %v\nwant %s", order, want)
	}
	ops = ops[:0]
	for _, c := range native.seen()[before:] {
		ops = append(ops, c.Op)
	}
	if strings.Join(ops, ",") != "open,click,key,read,key,close" {
		t.Fatalf("native host received %v for the control turn", ops)
	}

	// 3. A remembered browser never falls through to another one after a
	// possibly executed action; naming a browser that was never set up, and a
	// fresh Bot under a native-first policy, refuse with their typed codes.
	// None of them reaches the host.
	userChrome := `{"order":["user_chrome","isolated","native"],"automatic":{"native":false,"isolated":false,"user_chrome":true}}`
	for _, c := range []struct{ bot, policy, prompt, code string }{
		{"browser-bot", userChrome, "browser-probe-remembered", "browser_disabled"},
		{"browser-bot", userChrome, "browser-probe-user-chrome", "browser_setup_required"},
		{"browser-fresh", `{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":true,"user_chrome":false}}`, "browser-probe-native", "browser_native_tools"},
	} {
		setPolicy(c.policy)
		if c.bot != f.key.BotID {
			f.key.BotID = c.bot
			q := f.create()
			q.Settings.Target = paWireProjectSelection(pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID})
			if h.request(t, "POST", "/v1/project-assistants", q, nil) != 200 {
				t.Fatal("create", c.bot)
			}
		}
		before = len(native.seen())
		turnFinished(c.prompt)
		lines, _, _ = readBrowserPeer(t, logPath)
		last := lines[len(lines)-1]
		if last.Turn != "probe" || !last.IsError || last.Code != c.code || len(native.seen()) != before {
			t.Fatalf("%s under %s = %+v", c.prompt, c.policy, last)
		}
	}
	lines, acpCalls, _ = readBrowserPeer(t, logPath)
	counted.Lock()
	t.Logf("scripted provider: opens=%d sends=%d acp=%v mcp-browser-calls=%d native-host-commands=%d; Chrome=0 model=0 native CLI=0", opens, sends, acpCalls, len(lines), len(native.seen()))
	if opens != 5 || sends != 5 {
		t.Errorf("provider opens=%d sends=%d, want 5 each", opens, sends)
	}
	counted.Unlock()
}

type countingProvider struct {
	app.Provider
	open, send func()
}

func (p countingProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.open()
	s, err := p.Provider.Open(ctx, q, sink)
	if s == nil {
		return s, err
	}
	return countingSession{ProviderSession: s, send: p.send}, err
}

type countingSession struct {
	app.ProviderSession
	send func()
}

func (s countingSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.send()
	return s.ProviderSession.Send(ctx, q)
}
