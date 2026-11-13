package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const nativeFirstBrowserPolicy = `{"browser_policy":{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":false,"user_chrome":false}}}`
const testFirstBrowserPolicy = `{"browser_policy":{"order":["isolated","native","user_chrome"],"automatic":{"native":true,"isolated":true,"user_chrome":false}}}`

func TestBothBrowserPolicySettingsEntrypointsRequireRealReadiness(t *testing.T) {
	for _, path := range []string{"/v1/browser/settings", "/v1/settings/browser"} {
		t.Run(path, func(t *testing.T) {
			f, _ := newGateFixture(t)
			s := &Server{cfg: config.Config{Dir: t.TempDir()}}
			route := s.browserSettingsRoute
			if path == "/v1/settings/browser" {
				route = s.settingsRoute
			}
			handler := f.g.wrap(http.HandlerFunc(route))
			send := func(method, body string) *responseForBrowserPolicy {
				a := (call{method: method, path: path, body: body, headers: map[string]string{"Authorization": "Bearer " + f.send, "Content-Type": "application/json", actorHeader: actorDevice}}).do(handler)
				return &responseForBrowserPolicy{code: a.Code, body: a.Body.Bytes()}
			}
			if a := send("POST", testFirstBrowserPolicy); a.code != 409 {
				t.Fatalf("unconfigured profile enabled: %d %s", a.code, a.body)
			}
			if a := send("POST", nativeFirstBrowserPolicy); a.code != 200 {
				t.Fatalf("native preference: %d %s", a.code, a.body)
			}
			probe, cancel := context.WithCancel(t.Context())
			cancel()
			b, _ := s.browsers().Channel("isolated")
			b.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "fixture"})
			if a := send("POST", testFirstBrowserPolicy); a.code != 200 {
				t.Fatalf("connected profile refused: %d %s", a.code, a.body)
			}
			a := send("GET", "")
			var got struct {
				Policy struct {
					Order     []string        `json:"order"`
					Automatic map[string]bool `json:"automatic"`
				} `json:"browser_policy"`
			}
			if json.Unmarshal(a.body, &got) != nil || len(got.Policy.Order) != 3 || got.Policy.Order[0] != "isolated" || !got.Policy.Automatic["native"] || !got.Policy.Automatic["isolated"] || got.Policy.Automatic["user_chrome"] {
				t.Fatalf("policy not round-tripped: %d %s", a.code, a.body)
			}
			denied := (call{method: "POST", path: path, body: nativeFirstBrowserPolicy, headers: map[string]string{"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}).do(handler)
			if denied.Code != 403 {
				t.Fatalf("agent changed automatic selection: %d %s", denied.Code, denied.Body)
			}
		})
	}
}

type responseForBrowserPolicy struct {
	code int
	body []byte
}

func TestExplicitBrowserChoiceUsesTheLiveSessionAndPreservesMachinePolicy(t *testing.T) {
	const conversation = "20000000-0000-4000-8000-000000000051"
	live := session.Session{ID: "pane-browser-policy", Backend: session.BackendTmux, Assistant: session.AssistantClaude, ConversationID: conversation, State: session.StateWorking}
	s := paneServer(t, &pane{s: live})
	s.cfg.Dir = t.TempDir()
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{live} }
	if _, err := s.settingsFile().Set(map[string]any{"browser_policy": json.RawMessage(`{"order":["native","isolated","user_chrome"],"automatic":{"native":true,"isolated":false,"user_chrome":false}}`)}); err != nil {
		t.Fatal(err)
	}
	f, _ := newGateFixture(t)
	handler := f.g.wrap(http.HandlerFunc(s.browserCommandRoute))
	command := func(c, mode string, explicit bool) int {
		raw, _ := json.Marshal(map[string]any{"conversation": c, "browser_mode": mode, "explicit": explicit, "op": "tabs", "args": map[string]any{}})
		answer := (call{method: "POST", path: "/v1/browser/commands", body: string(raw), headers: map[string]string{"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}).do(handler)
		return answer.Code
	}
	if code := command(conversation, "isolated", true); code != 409 {
		t.Fatalf("explicit request bypassed missing setup: %d", code)
	}
	probe, cancel := context.WithCancel(t.Context())
	cancel()
	bridge, _ := s.browsers().Channel("isolated")
	bridge.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "fixture"})
	if code := command(conversation, "isolated", false); code != 403 {
		t.Fatalf("named request bypassed automatic switch: %d", code)
	}
	hostCtx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for hostCtx.Err() == nil {
			for _, c := range bridge.Poll(hostCtx, browserbridge.HostInfo{Mode: "isolated", Version: "fixture"}) {
				_ = s.browsers().Deliver("isolated", []browserbridge.Result{{ID: c.ID, OK: true, Value: json.RawMessage(`[]`)}})
			}
		}
	}()
	if code := command(conversation, "isolated", true); code != 200 {
		t.Fatalf("explicit ready request refused: %d", code)
	}
	if code := command("20000000-0000-4000-8000-000000000099", "isolated", true); code == 200 {
		t.Fatal("unknown Session used the browser")
	}
	values, err := s.settingsFile().Read()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := values.BrowserPolicy()
	if err != nil || policy.Automatic["isolated"] || policy.Order[0] != "native" {
		t.Fatalf("explicit request changed machine policy: %+v %v", policy, err)
	}
	mode, _, _ := s.browsers().SelectedStatus(conversation, policy)
	if mode != "isolated" {
		t.Fatal("choice was not remembered for this Session")
	}
	other, _, _ := s.browsers().SelectedStatus("other", policy)
	if other != "native" {
		t.Fatal("choice leaked to another Session")
	}
	stop()
	<-done
}
