package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botloginprofiles"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// An owned local native protocol, with no vendor calls, keys or real sign-in.
func TestLoginCapabilitiesPeer(t *testing.T) {
	mode := os.Getenv("CLAWDLINE_LOGIN_PEER")
	if mode == "" {
		return
	}
	slot := os.Getenv("CODEX_HOME")
	selected := "default"
	if slot != "" {
		selected = filepath.Base(slot)
	}
	if mode == "native" {
		args := strings.Join(os.Args, " ")
		if strings.HasSuffix(args, "logout") {
			if e := os.Remove(filepath.Join(slot, "synthetic-signed")); e != nil && !os.IsNotExist(e) {
				os.Exit(2)
			}
			os.Exit(0)
		}
		if strings.HasSuffix(args, "login status") {
			if _, e := os.Stat(filepath.Join(slot, "synthetic-signed")); e != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
		if strings.HasSuffix(args, "login") {
			if e := os.WriteFile(filepath.Join(slot, "synthetic-signed"), []byte("synthetic local account"), 0600); e != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
		os.Exit(3)
	}
	skillSeen := false
	var connectorURL, connectorBearer string
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var f struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch f.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]any{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}}
		case "session/new":
			skillSeen = strings.Contains(string(f.Params["systemPrompt"]), "Selected application skill")
			var servers []struct {
				Name    string                         `json:"name"`
				URL     string                         `json:"url"`
				Headers []struct{ Name, Value string } `json:"headers"`
			}
			_ = json.Unmarshal(f.Params["mcpServers"], &servers)
			for _, server := range servers {
				if server.Name == "fixture-connector" {
					connectorURL = server.URL
					for _, h := range server.Headers {
						if h.Name == "Authorization" {
							connectorBearer = h.Value
						}
					}
				}
			}
			result = map[string]any{"sessionId": "synthetic-login-session", "configOptions": []any{}}
		case "session/prompt":
			if state := os.Getenv("CLAWDLINE_LOGIN_PEER_STATE"); state != "" {
				hold := filepath.Join(state, "synthetic-native-hold")
				if _, e := os.Stat(hold); e == nil {
					_ = os.WriteFile(filepath.Join(state, "synthetic-native-ready"), []byte("ready"), 0600)
					for {
						if _, e := os.Stat(hold); os.IsNotExist(e) {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
			}
			connectorSeen := false
			if connectorURL != "" {
				request, _ := http.NewRequest("POST", connectorURL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"fixture_echo","arguments":{}}}`))
				request.Header.Set("Authorization", connectorBearer)
				request.Header.Set("Content-Type", "application/json")
				response, e := http.DefaultClient.Do(request)
				if e != nil {
					os.Exit(5)
				}
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				connectorSeen = response.StatusCode == 200 && strings.Contains(string(body), "synthetic fixture connector")
				if !connectorSeen {
					os.Exit(6)
				}
			}
			if slot != "" {
				if _, e := os.Stat(filepath.Join(slot, "synthetic-signed")); e != nil {
					os.Exit(4)
				}
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-login-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": fmt.Sprintf("Synthetic selected login %s / skill=%t / connector=%t", selected, skillSeen, connectorSeen)}}}})
			result = map[string]string{"stopReason": "end_turn"}
		case "session/cancel":
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": result})
	}
	os.Exit(0)
}

type loginFixtureProvider struct {
	profiles app.LoginProfiles
	exe      string
	state    string
}

func (p loginFixtureProvider) provider(ctx context.Context, b pa.Bot) (*acp.Provider, func(), error) {
	env, release, e := p.profiles.Acquire(ctx, b)
	if e != nil {
		return nil, func() {}, e
	}
	env = append(env, "CLAWDLINE_LOGIN_PEER=acp", "CLAWDLINE_LOGIN_PEER_STATE="+p.state)
	provider, e := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: p.exe, Arguments: []string{"-test.run=^TestLoginCapabilitiesPeer$"}, Environment: env}}, Limits: acp.Limits{RPCPending: 16, IncomingPermissions: 16, FrameBytes: 1 << 20, CaptureBytes: 4 << 20, StderrBytes: 64 << 10, OwnedDescendants: 32, WriteTimeout: time.Second, CleanupTimeout: 5 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, c acp.Configuration) (pa.Capabilities, error) {
		// The exact selected environment is the evidence subject. No default
		// profile receipt is reused for a managed profile.
		if b.LoginProfile != "" && !strings.Contains(strings.Join(c.Environment, "\n"), "/"+b.LoginProfile) {
			return pa.Capabilities{}, &pa.Failure{Code: pa.MemoryIsolationUnverified, Field: "synthetic_profile"}
		}
		return paServerCaps(b), nil
	}})
	if e != nil {
		release()
	}
	return provider, release, e
}
func (p loginFixtureProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	v, release, e := p.provider(ctx, b)
	defer release()
	if e != nil {
		return pa.Capabilities{}, e
	}
	return v.Capabilities(ctx, b)
}

type loginFixtureSession struct {
	app.ProviderSession
	release func()
}

func (s loginFixtureSession) Close(ctx context.Context) error {
	e := s.ProviderSession.Close(ctx)
	if e == nil {
		s.release()
	}
	return e
}
func (p loginFixtureProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	v, release, e := p.provider(ctx, q.Bot)
	if e != nil {
		return nil, e
	}
	s, e := v.Open(ctx, q, sink)
	if s == nil {
		release()
		return nil, e
	}
	return loginFixtureSession{s, release}, e
}
func (p loginFixtureProvider) Configuration(ctx context.Context, b pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	v, release, e := p.provider(ctx, b)
	defer release()
	if e != nil {
		return pa.ConfigurationCatalog{}, e
	}
	return v.Configuration(ctx, b, dir, model)
}

type observedLoginNative struct {
	app.LoginNative
	t *testing.T
}

func (n observedLoginNative) Start(ctx context.Context, a pa.Agent, env []string) (app.LoginProcess, error) {
	p, e := n.LoginNative.Start(ctx, a, env)
	if e != nil {
		n.t.Logf("owned synthetic native startup: %v", e)
	}
	return p, e
}

func loginCapabilitiesFixture(t *testing.T) (*paServerFixture, *botloginprofiles.Registry) {
	t.Helper()
	f := newPAServerFixture(t, true)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	var profiles *botloginprofiles.Registry
	f.s.projectAssistants.loginProfiles = func(ctx context.Context) (app.LoginProfiles, error) {
		r, e := botloginprofiles.New(ctx, f.s.store.ProjectAssistantDatabase(), f.s.cfg.Dir, "synthetic-source-account", f.key.MachineID, observedLoginNative{t: t, LoginNative: acp.InstalledLoginNative{Resolve: func(_ context.Context, _ pa.Agent, env []string) (acp.Configuration, error) {
			return acp.Configuration{Executable: exe, Arguments: []string{"-test.run=^TestLoginCapabilitiesPeer$", "--"}, Environment: append(append([]string{}, env...), "CLAWDLINE_LOGIN_PEER=native")}, nil
		}}})
		profiles = r
		return r, e
	}
	skillPath := filepath.Join(f.s.cfg.Dir, "synthetic-installed-skill.json")
	skillBytes, _ := json.Marshal([]squad.Skill{{SkillID: "synthetic", Name: squad.Names{En: "Synthetic installed skill"}, Content: "Use the local synthetic fixture skill."}})
	if e := os.WriteFile(skillPath, skillBytes, 0600); e != nil {
		t.Fatal(e)
	}
	f.s.projectAssistants.extensions = botcapabilities.Inventory{State: f.s.cfg.Dir, Skills: func(context.Context) ([]squad.Skill, error) {
		b, e := os.ReadFile(skillPath)
		if e != nil {
			return nil, e
		}
		var skills []squad.Skill
		e = json.Unmarshal(b, &skills)
		return skills, e
	}}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return loginFixtureProvider{profiles, exe, f.s.cfg.Dir}, nil }
	f.enable(t)
	create := f.create()
	create.Settings.Agent = contract.ProjectAssistantAgentCodex
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record, e := os.OpenFile(filepath.Join(f.s.cfg.Dir, "synthetic-connector-requests"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			http.Error(w, "fixture failure", 500)
			return
		}
		_, _ = record.WriteString("called\n")
		_ = record.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"synthetic fixture connector"}]}}`)
	}))
	t.Cleanup(target.Close)
	g := f.get(t, f.local)
	definitions, _ := json.Marshal([]botcapabilities.Connector{{ID: "synthetic", Name: "fixture-connector", URL: target.URL, Project: paDomainProjectScope(g.Bot.Project), Agent: pa.Codex, Enabled: true}})
	if e := os.MkdirAll(filepath.Join(f.s.cfg.Dir, "project-assistants"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(f.s.cfg.Dir, "project-assistants", "connectors.json"), definitions, 0600); e != nil {
		t.Fatal(e)
	}
	return f, profiles
}
func loginProfilesURL(f *paServerFixture, g contract.ProjectAssistantGetResponse) string {
	return "/v1/project-assistants/" + f.key.BotID + "/login-profiles?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {g.Bot.ConversationID}, "agent": {"codex"}}.Encode()
}
func loginAction(f *paServerFixture, g contract.ProjectAssistantGetResponse, operation string, p contract.ProjectAssistantLoginProfile, name string) contract.ProjectAssistantLoginProfileAction {
	return contract.ProjectAssistantLoginProfileAction{Command: f.command(pa.LoginProfileOperation, operation+name+p.ID), Fence: contract.ProjectAssistantFence{Key: g.Bot.Key, Project: g.Bot.Project, ConversationID: g.Conversation.ID, BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation}, ExpectedVersion: g.Bot.Version, ProfileID: p.ID, Revision: p.Revision, Agent: contract.ProjectAssistantAgentCodex, Action: operation, Name: name}
}
func loginAct(t *testing.T, f *paServerFixture, q contract.ProjectAssistantLoginProfileAction) contract.ProjectAssistantLoginProfile {
	t.Helper()
	w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, q)
	paServerOK(t, w)
	return paHTTPDecode[contract.ProjectAssistantLoginProfileResponse](t, w).Profile
}
func awaitLogin(t *testing.T, f *paServerFixture, id, state string) contract.ProjectAssistantLoginProfile {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		g := f.get(t, f.local)
		w := f.request(t, "GET", loginProfilesURL(f, g), f.local, nil)
		paServerOK(t, w)
		for _, p := range paHTTPDecode[contract.ProjectAssistantLoginProfilesResponse](t, w).Profiles {
			if p.ID == id && p.State == state {
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native login did not reach " + state)
	return contract.ProjectAssistantLoginProfile{}
}

func TestLoginProfilesStoredHTTPAndNativeIsolation(t *testing.T) {
	f, profiles := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	first := loginAct(t, f, loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, "Synthetic first"))
	second := loginAct(t, f, loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, "Synthetic second"))
	start := loginAction(f, g, "start", first, "")
	accepted := loginAct(t, f, start)
	if accepted.State != "accepted" {
		t.Fatal(accepted)
	}
	signed := awaitLogin(t, f, first.ID, "signed_in")
	// Lost-response replay returns the original acceptance snapshot, no new login.
	if replay := loginAct(t, f, start); replay != accepted {
		t.Fatal("lost response replay changed", replay, accepted)
	}
	loginAct(t, f, loginAction(f, g, "start", second, ""))
	awaitLogin(t, f, second.ID, "signed_in")
	if read := f.request(t, "GET", loginProfilesURL(f, g), f.readonly, nil); read.Code != 200 {
		t.Fatal(read.Code, read.Body)
	}
	for n, selected := range []string{first.ID, second.ID, ""} {
		g = f.get(t, f.local)
		q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, fmt.Sprint("select-profile-", n)), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: selected}}}
		paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q))
		g = f.get(t, f.local)
		if selected != "" {
			profile := awaitLogin(t, f, selected, "signed_in")
			w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, loginAction(f, g, "start", profile, ""))
			if w.Code < 400 {
				t.Fatal("a selected account was replaced without a configuration change", w.Code)
			}
			if current := awaitLogin(t, f, selected, "signed_in"); current.Revision != profile.Revision {
				t.Fatal("refused reauthentication changed the account", current)
			}
		}
		sent := f.send(t, f.local, "profile-turn-"+fmt.Sprint(n))
		paServerOK(t, sent)
		turnID := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
		x := fallbackHTTPWait(t, f)
		want := selected
		if want == "" {
			want = "default"
		}
		found := false
		for _, m := range x.Messages {
			if m.TurnID == turnID && strings.Contains(m.Text, "Synthetic selected login "+want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("native selected profile not observed %s %+v", want, x.Turns)
		}
	}
	b := pa.Bot{Key: pa.BotKey{MachineID: f.key.MachineID, BotID: f.key.BotID}, Project: pa.ProjectScope{MachineID: f.key.MachineID}, Agent: pa.Codex, LoginProfile: first.ID}
	_, release, e := profiles.Acquire(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	g = f.get(t, f.local)
	signed = awaitLogin(t, f, first.ID, "signed_in")
	remove := loginAction(f, g, "remove", signed, "")
	w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, remove)
	if w.Code < 400 {
		t.Fatal("busy profile removed")
	}
	release()
	// Wrong Agent and invented IDs refuse at selection, before native effects.
	b.Agent = pa.ClaudeCode
	if _, _, e = profiles.Acquire(context.Background(), b); pa.Code(e) != pa.ScopeDenied {
		t.Fatal(e)
	}
	g = f.get(t, f.local)
	bad := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "unknown-profile"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: strings.Repeat("f", 32)}}}
	w = f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, bad)
	if w.Code < 400 || f.get(t, f.local).Bot.Version != g.Bot.Version {
		t.Fatal("unknown profile mutated Bot")
	}
}

func TestLoginProfilesBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_LOGIN_CAPABILITIES_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_LOGIN_CAPABILITIES_BROWSER_DIR for owned synthetic browser")
	}
	f, _ := loginCapabilitiesFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-login-capabilities.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, e := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if e != nil {
		t.Fatalf("browser: %v\n%s", e, output)
	}
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		found = found || strings.Contains(m.Text, "Synthetic selected login ") && !strings.Contains(m.Text, "Synthetic selected login default")
	}
	if !found {
		t.Fatal("browser selection did not reach actual ACP")
	}
}

func TestIndividualExtensionsCrossStoredHTTPAndActualACP(t *testing.T) {
	f, _ := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	path := "/v1/project-assistants/" + f.key.BotID + "/extensions?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {g.Bot.ConversationID}}.Encode()
	w := f.request(t, "GET", path, f.readonly, nil)
	paServerOK(t, w)
	catalog := paHTTPDecode[contract.ProjectAssistantExtensionsResponse](t, w).Catalog
	selection := contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{}}
	for _, o := range catalog.Items {
		ref := contract.ProjectAssistantExtensionRef{ID: o.ID, Fingerprint: o.Fingerprint}
		if o.Kind == "skill" {
			selection.Skills = append(selection.Skills, ref)
		} else {
			selection.Connectors = append(selection.Connectors, ref)
		}
	}
	if len(selection.Skills) != 1 || len(selection.Connectors) != 1 {
		t.Fatal("fixture inventory", catalog)
	}
	for n, want := range []bool{true, false} {
		g = f.get(t, f.local)
		selected := selection
		if !want {
			selected = contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{}}
		}
		edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, fmt.Sprint("extensions-", n)), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Extensions: &selected}}
		paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
		sent := f.send(t, f.local, fmt.Sprint("extensions-turn-", n))
		paServerOK(t, sent)
		turn := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
		x := fallbackHTTPWait(t, f)
		found := false
		for _, m := range x.Messages {
			if m.TurnID == turn && strings.Contains(m.Text, fmt.Sprintf("skill=%t / connector=%t", want, want)) {
				found = true
			}
		}
		if !found {
			t.Fatal("selection did not reach actual native protocol", x.Turns)
		}
	}
	records, e := os.ReadFile(filepath.Join(f.s.cfg.Dir, "synthetic-connector-requests"))
	if e != nil || string(records) != "called\n" {
		t.Fatal("deselected connector executed", string(records), e)
	}
	if e = os.WriteFile(filepath.Join(f.s.cfg.Dir, "project-assistants", "connectors.json"), []byte("[]"), 0600); e != nil {
		t.Fatal(e)
	}
	g = f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "revoked-extension"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Extensions: &selection}}
	w = f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit)
	if w.Code < 400 || f.get(t, f.local).Bot.Version != g.Bot.Version {
		t.Fatal("revoked definition saved")
	}
}

func TestDeselectingALiveConnectorPreventsItsNextNativeCall(t *testing.T) {
	f, _ := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	path := "/v1/project-assistants/" + f.key.BotID + "/extensions?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {g.Bot.ConversationID}}.Encode()
	catalog := paHTTPDecode[contract.ProjectAssistantExtensionsResponse](t, f.request(t, "GET", path, f.local, nil)).Catalog
	selection := contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{}}
	for _, o := range catalog.Items {
		if o.Kind == "connector" {
			selection.Connectors = append(selection.Connectors, contract.ProjectAssistantExtensionRef{ID: o.ID, Fingerprint: o.Fingerprint})
		}
	}
	save := func(id string, v contract.ProjectAssistantExtensionSelection) {
		g := f.get(t, f.local)
		q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, id), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Extensions: &v}}
		paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q))
	}
	save("select-live-connector", selection)
	hold := filepath.Join(f.s.cfg.Dir, "synthetic-native-hold")
	if e := os.WriteFile(hold, []byte("hold"), 0600); e != nil {
		t.Fatal(e)
	}
	paServerOK(t, f.send(t, f.local, "live-connector"))
	paResilienceAwait(t, func() bool { _, e := os.Stat(filepath.Join(f.s.cfg.Dir, "synthetic-native-ready")); return e == nil })
	save("deselect-live-connector", contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{}})
	if e := os.Remove(hold); e != nil {
		t.Fatal(e)
	}
	x := fallbackHTTPWait(t, f)
	if len(x.Turns) != 1 || x.Turns[0].State == pa.TurnSucceeded {
		t.Fatal("revoked live connector turn reported success", x.Turns)
	}
	if _, e := os.Stat(filepath.Join(f.s.cfg.Dir, "synthetic-connector-requests")); !os.IsNotExist(e) {
		t.Fatal("deselected live connector executed", e)
	}
}
