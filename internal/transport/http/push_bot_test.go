package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
)

type botBrowserDecoderRouter struct{ calls int }

func (r *botBrowserDecoderRouter) Do(_ context.Context, request cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	fields := map[string]any{}
	if request.Method == "GET" {
		for key, value := range request.Query {
			fields[key] = value
		}
	} else if err := json.Unmarshal(request.Body, &fields); err != nil {
		return cloudops.LocalResponse{}, err
	}
	if fields["bot_id"] != "synthetic-bot" || fields["conversation"] != "current" || fields["owner"] != nil || fields["machine_id"] != nil {
		return cloudops.LocalResponse{}, fmt.Errorf("wrong browser target: %v", fields)
	}
	r.calls++
	return cloudops.LocalResponse{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

// The real TypeScript SourceClient and real Go Cloud decoder share a request,
// rather than two independently maintained fixtures agreeing with each other.
func TestBotBrowserSourceCallsCrossTheCloudDecoder(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	module := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, "web/console/src/cloud/project-assistants.ts"))}).String()
	script := fmt.Sprintf(`
import {ProjectAssistantSourceClient,assistantRelayRequest} from %q;
const grant={accountKey:"synthetic",readerID:"reader",machineID:"machine",authorizationRevision:"one",apiLevel:74,paired:true,online:true,writable:true};
const calls=[],source=new ProjectAssistantSourceClient(grant,async(path,init)=>{
 if(path==="/v1/project-assistants/status")return Response.json({contract_version:"project-assistants.v1",sources:[{machine_id:"machine",state:"online",access:"interactive",readable:true,writable:true,fresh:true}]});
 calls.push(assistantRelayRequest(path,init));return Response.json({ok:true});
},()=>grant),key={machine_id:"machine",bot_id:"synthetic-bot"},signal=new AbortController().signal;
await source.status(signal);
for(const path of ["status","activity","audit?limit=200","screenshot?tab=9&quality=45&browser_mode=isolated"])await source.browser(key,"current","GET","/v1/browser/"+path,undefined,signal);
for(const [path,body] of [["control",{tab:9,person:true,browser_mode:"isolated"}],["person",{tab:9,op:"key",args:{key:"Tab"},browser_mode:"isolated"}],["open",{url:"https://synthetic.example",layout:"mobile"}],["mute",{muted:true}]])await source.browser(key,"current","POST","/v1/browser/"+path,body,signal);
console.log(JSON.stringify(calls));`, module)
	command := exec.Command("node", "--experimental-strip-types", "--input-type=module", "-e", script)
	output, err := command.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Log(string(e.Stderr))
		}
		t.Fatal(err)
	}
	var calls []struct {
		Word   string         `json:"word"`
		ID     string         `json:"id"`
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal(output, &calls); err != nil {
		t.Fatal(err)
	}
	router := &botBrowserDecoderRouter{}
	bridge := cloudops.Bridge{Router: router, AllowCommands: func() bool { return true }}
	for _, call := range calls {
		call.Fields["type"], call.Fields["session"], call.Fields["request"] = call.Word, cloudops.MachineReplySession, call.ID
		if call.ID == "" {
			call.Fields["request"] = "synthetic-read"
		}
		if call.Word == "chrome-audit" {
			limit := call.Fields["limit"]
			delete(call.Fields, "limit")
			bad, _ := json.Marshal(call.Fields)
			before := router.calls
			answer := bridge.Handle(t.Context(), cloudops.Command{Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Plaintext: bad})
			if answer.Status != 400 || router.calls != before {
				t.Fatalf("audit without a limit crossed the decoder: %+v", answer)
			}
			call.Fields["limit"] = limit
		}
		raw, _ := json.Marshal(call.Fields)
		answer := bridge.Handle(t.Context(), cloudops.Command{Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Plaintext: raw})
		if !answer.OK() {
			t.Fatalf("%s rejected the actual source request: status=%d code=%s payload=%s", call.Word, answer.Status, answer.Code, answer.Payload)
		}
	}
	if router.calls != 8 {
		t.Fatalf("decoded %d browser calls", router.calls)
	}
	t.Log("actual TypeScript browser source requests decoded: 8; audit without limit refused before routing; external model calls: 0")
}

func TestBotPushKeepsTheAgentSwitchAndLedger(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.executionMachineID = f.key.MachineID
	var destination string
	f.s.broker.Push = func(ctx context.Context, _, _, session, _ string) (int, int, error) {
		destination = f.s.pushDestinationURL(ctx, session)
		return 1, 0, nil
	}
	f.s.broker.NotifyEnabled = func() bool { return true }
	if _, err := f.s.broker.MachineNotifyBot(t.Context(), "Bot notice", "Synthetic site", f.key.BotID, "current"); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(destination)
	q, _ := url.ParseQuery(u.EscapedFragment())
	if q.Get("bot") != f.key.BotID || q.Get("conversation") != "current" || q.Get("machine") != f.s.executionMachine() || q.Has("session") {
		t.Fatalf("destination = %q", destination)
	}
	_, hour, err := f.s.broker.Store.NotificationCounts(t.Context(), "")
	if err != nil || hour != 1 {
		t.Fatalf("ledger = %d %v", hour, err)
	}
	f.s.broker.NotifyEnabled = func() bool { return false }
	if _, err := f.s.broker.MachineNotifyBot(t.Context(), "Bot notice", "Synthetic site", f.key.BotID, "current"); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("disabled = %v", err)
	}
}

func TestBotNewSitePushOpensItsOwnChat(t *testing.T) {
	f := newBotTargetFixture(t)
	f.s.executionMachineID = f.key.MachineID
	destinations := make(chan string, 1)
	f.s.broker.Push = func(ctx context.Context, _, _, session, _ string) (int, int, error) {
		destinations <- f.s.pushDestinationURL(ctx, session)
		return 1, 0, nil
	}
	f.s.broker.NotifyEnabled = func() bool { return true }
	if _, err := f.s.settingsFile().Set(map[string]any{"browser_notify_new_site": true}); err != nil {
		t.Fatal(err)
	}
	owner := f.owner(f.key, f.projectA, f.convA, 0)
	f.s.announceBrowserSite(browserbridge.NewSite{Conversation: owner, Site: "synthetic.example"})
	select {
	case got := <-destinations:
		want := adapterpush.BotDestinationURL(f.key.BotID, f.convA, f.s.executionMachine())
		if got != want {
			t.Fatalf("new-site tap = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notice")
	}
}

// Run explicitly: the normal gate does not launch a browser. The handler owns
// an empty state directory and a temporary TCP port, with a scripted host.
func TestBotChatBrowserPanelE2E(t *testing.T) {
	if os.Getenv("BOTS_BROWSER_UI_E2E") != "1" {
		t.Skip("explicit browser acceptance")
	}
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	empty := f.create()
	empty.Command.Key.BotID = "synthetic-empty-bot"
	empty.Command.IdempotencyKey = "empty-bot"
	empty.Settings.Name = "Empty Bot"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, empty))
	var emptyBot contract.ProjectAssistantGetResponse
	if err := json.Unmarshal(f.request(t, "GET", "/v1/project-assistants/"+empty.Command.Key.BotID+"?machine_id="+f.key.MachineID, f.local, nil).Body.Bytes(), &emptyBot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.settingsFile().Set(map[string]any{"browser_policy": json.RawMessage(`{"order":["isolated","native","user_chrome"],"automatic":{"native":false,"isolated":true,"user_chrome":false}}`)}); err != nil {
		t.Fatal(err)
	}
	bridge, err := f.s.browsers().Channel("isolated")
	if err != nil {
		t.Fatal(err)
	}
	shot := scriptedJPEG(t)
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		nextTab := 40
		for ctx.Err() == nil {
			for _, c := range bridge.Poll(ctx, browserbridge.HostInfo{Mode: "isolated", Version: "0.5.0"}) {
				result := browserbridge.Result{ID: c.ID, OK: true, Tab: c.Tab, URL: "https://synthetic.example/form", Value: json.RawMessage(`{"ok":true}`)}
				if c.Op == "open" {
					nextTab++
					result.Tab = nextTab
				}
				if c.Op == "screenshot" {
					result.Value, _ = json.Marshal(map[string]any{"data": shot, "url": "https://synthetic.example/form", "title": "Synthetic browser page", "width": 320, "height": 240, "layout": "desktop"})
				}
				if err := bridge.Deliver([]browserbridge.Result{result}); err != nil {
					t.Errorf("host deliver: %v", err)
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	probe, cancel := context.WithCancel(t.Context())
	cancel()
	bridge.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"})
	bot := f.get(t, f.local)
	owner := projectassistanthost.BrowserOwner(paDomainFence(contract.ProjectAssistantFence{Key: bot.Bot.Key, Project: bot.Bot.Project, ConversationID: bot.Conversation.ID}), 0)
	if _, err := f.s.projectAssistantBrowser().Command(t.Context(), owner, app.BrowserCommand{Op: "open", Args: json.RawMessage(`{"url":"https://synthetic.example/form"}`)}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.h)
	defer server.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "node", filepath.Join(root, "web/console/tools/project-assistants-browser-panel.e2e.mjs"))
	command.Dir = root
	command.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_CONVERSATION="+bot.Conversation.ID, "BOTS_EMPTY_BOT="+emptyBot.Bot.Key.BotID, "BOTS_EMPTY_CONVERSATION="+emptyBot.Conversation.ID)
	if os.Getenv("PLAYWRIGHT_BROWSERS_PATH") == "" {
		// TestMain isolates HOME. Resolve only the installed test-browser cache
		// from the OS account; the daemon and Chrome still keep the empty HOME.
		account, err := user.Current()
		if err != nil {
			t.Fatalf("resolve test browser cache; set PLAYWRIGHT_BROWSERS_PATH: %v", err)
		}
		cache := filepath.Join(account.HomeDir, ".cache", "ms-playwright")
		switch runtime.GOOS {
		case "darwin":
			cache = filepath.Join(account.HomeDir, "Library", "Caches", "ms-playwright")
		case "windows":
			cache = filepath.Join(account.HomeDir, "AppData", "Local", "ms-playwright")
		default:
			if root := os.Getenv("XDG_CACHE_HOME"); root != "" {
				cache = filepath.Join(root, "ms-playwright")
			}
		}
		command.Env = append(command.Env, "PLAYWRIGHT_BROWSERS_PATH="+cache)
	}
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatalf("model provider called %d times", f.providerCalls.Load())
	}
}
