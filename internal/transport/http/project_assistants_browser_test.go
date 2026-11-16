package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

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
