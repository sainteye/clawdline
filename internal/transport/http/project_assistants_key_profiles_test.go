package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botloginprofiles"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestAPIKeyProfilesBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_KEY_PROFILES_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_KEY_PROFILES_BROWSER_DIR for the synthetic production UI")
	}
	f, _ := loginCapabilitiesFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-key-profiles.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, e := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if e != nil {
		t.Fatalf("browser: %v\n%s", e, output)
	}
	x := fallbackHTTPWait(t, f)
	count := 0
	for _, m := range x.Messages {
		if strings.Contains(m.Text, "Synthetic selected login ") && strings.Contains(m.Text, "key=true") {
			count++
		}
	}
	if count != 2 {
		t.Fatal("both browser key selections did not reach actual ACP", count)
	}
}

func keySubmission(q contract.ProjectAssistantLoginProfileAction, value string) map[string]any {
	raw, _ := json.Marshal(q)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	out["value"] = value
	return out
}

func TestClaudeKeyUsesItsOwnAllowlistedNativeEnvironment(t *testing.T) {
	f, r := loginCapabilitiesFixture(t)
	// Use a separately created Claude Bot so this key-isolation fixture does
	// not change the existing Codex Bot's Agent.
	f.key.BotID = "synthetic-claude-key-bot"
	newBot := f.create()
	newBot.Settings.Agent = contract.ProjectAssistantAgentClaude
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, newBot))
	g := f.get(t, f.local)
	create := loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, "Synthetic Claude key")
	create.Agent = contract.ProjectAssistantAgentClaude
	create.Kind = "api_key"
	p := loginAct(t, f, create)
	prepare := loginAction(f, g, "prepare-key", p, "")
	prepare.Agent = contract.ProjectAssistantAgentClaude
	prepared := loginAct(t, f, prepare)
	submit := loginAction(f, g, "submit-key", prepared, "")
	submit.Agent = contract.ProjectAssistantAgentClaude
	submit.Target = prepared.Handoff.ID
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-secret", f.local, keySubmission(submit, "SYNTHETIC_PROFILE_KEY_ONLY")))
	b := pa.Bot{Key: pa.BotKey{MachineID: f.key.MachineID, BotID: f.key.BotID}, Project: pa.ProjectScope{MachineID: f.key.MachineID}, Agent: pa.ClaudeCode, LoginProfile: p.ID}
	env, release, e := r.Acquire(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	if len(env) != 2 || !strings.HasPrefix(env[0], "CLAUDE_CONFIG_DIR=") || env[1] != "ANTHROPIC_API_KEY=SYNTHETIC_PROFILE_KEY_ONLY" {
		t.Fatal("Claude inherited another identity or arbitrary environment")
	}
	release()
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "select-claude-key"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Agent: contract.ProjectAssistantAgentClaude, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: p.ID}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	paServerOK(t, f.send(t, f.local, "claude-key-native-turn"))
	x := fallbackHTTPWait(t, f)
	for _, m := range x.Messages {
		if strings.Contains(m.Text, "Synthetic selected login "+p.ID) && strings.Contains(m.Text, "key=true") {
			return
		}
	}
	t.Fatal("Claude selected key did not reach the actual local native entry", x.Turns)
}

func TestAPIKeyProfileCrossesSecretHTTPVaultRestartAndActualACP(t *testing.T) {
	f, registry := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	q := loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, "Synthetic encrypted key")
	q.Kind = "api_key"
	p := loginAct(t, f, q)
	if p.Kind != "api_key" || len(p.Variables) != 1 || p.Variables[0] != "CODEX_API_KEY" {
		t.Fatal("key profile metadata", p)
	}
	prepare := loginAction(f, g, "prepare-key", p, "")
	target := loginAct(t, f, prepare)
	if target.Handoff == nil {
		t.Fatal("no dedicated handoff")
	}
	if usage := f.s.projectAssistantCapacityMeasures()["bots.login_secret_targets"](); !usage.Known || usage.Used != 1 {
		t.Fatal("handoff count was not measured", usage)
	}
	submit := loginAction(f, g, "submit-key", target, "")
	submit.Target = target.Handoff.ID
	path := "/v1/project-assistants/" + f.key.BotID + "/login-secret"
	if w := f.request(t, "POST", path, f.readonly, keySubmission(submit, "SYNTHETIC_PROFILE_KEY_ONLY")); w.Code < 400 {
		t.Fatal("read-only secret accepted")
	}
	paServerOK(t, f.request(t, "POST", path, f.local, keySubmission(submit, "SYNTHETIC_PROFILE_KEY_ONLY")))
	signed := awaitLogin(t, f, p.ID, "signed_in")
	if usage := f.s.projectAssistantCapacityMeasures()["bots.login_secret_targets"](); !usage.Known || usage.Used != 0 {
		t.Fatal("consumed handoff retained a capacity allocation", usage)
	}
	// Replaying the same fixed operation must neither compare nor replace a key.
	paServerOK(t, f.request(t, "POST", path, f.local, keySubmission(submit, "DIFFERENT_SYNTHETIC_GUESS")))
	paServerOK(t, f.request(t, "POST", path, f.local, keySubmission(submit, "")))
	state := f.s.cfg.Dir
	keyHash := sha256.Sum256([]byte("SYNTHETIC_PROFILE_KEY_ONLY"))
	exported := f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/export?machine_id="+f.key.MachineID, f.local, nil)
	paServerOK(t, exported)
	for _, forbidden := range []string{"SYNTHETIC_PROFILE_KEY_ONLY", "DIFFERENT_SYNTHETIC_GUESS", hex.EncodeToString(keyHash[:])} {
		if strings.Contains(exported.Body.String(), forbidden) {
			t.Fatal("authenticated export exposed a key or credential fingerprint")
		}
	}
	if e := filepath.WalkDir(state, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			for _, s := range []string{"SYNTHETIC_PROFILE_KEY_ONLY", "DIFFERENT_SYNTHETIC_GUESS", hex.EncodeToString(keyHash[:])} {
				if strings.Contains(string(raw), s) {
					t.Error("secret or credential fingerprint persisted in fixture artifact")
				}
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// Close and reopen the registry, retaining the same real SQLite source.
	if e := registry.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	restarted, e := botloginprofiles.New(context.Background(), f.s.store.ProjectAssistantDatabase(), state, "synthetic-source-account", f.key.MachineID, acp.InstalledLoginNative{})
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close(context.Background())
	b := pa.Bot{Key: pa.BotKey{MachineID: f.key.MachineID, BotID: f.key.BotID}, Project: pa.ProjectScope{MachineID: f.key.MachineID}, Agent: pa.Codex, LoginProfile: p.ID}
	env, release, e := restarted.Acquire(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	if len(env) != 2 || env[1] != "CODEX_API_KEY=SYNTHETIC_PROFILE_KEY_ONLY" {
		t.Fatal("trusted adapter did not recover only the selected key")
	}
	release()
	// Reopen the real Host against the same SQLite store and execute through
	// the production HTTP route. Factories are rebound to the reopened registry.
	if e = f.s.projectAssistants.host.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.s.projectAssistants.mu.Lock()
	f.s.projectAssistants.host = nil
	// A daemon restart creates a fresh broker. Clear the stopped fixture
	// broker's in-memory Bot-owner registrations before rebuilding its Host.
	f.s.broker.AssistantAuthority = nil
	f.s.broker.AssistantWorkChanged = nil
	f.s.broker.AssistantWorkCompleted = nil
	f.s.projectAssistants.mu.Unlock()
	g = f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "select-key-after-source-restart"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: p.ID}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	paServerOK(t, f.send(t, f.local, "key-after-source-restart"))
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		found = found || strings.Contains(m.Text, "Synthetic selected login "+p.ID) && strings.Contains(m.Text, "key=true")
	}
	if !found {
		t.Fatal("restarted source did not execute with selected key", x.Turns)
	}
	_ = signed
}

func TestAPIKeyProfileActualACPAndExpiredTargets(t *testing.T) {
	f, r := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	create := loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, "Synthetic native key")
	create.Kind = "api_key"
	p := loginAct(t, f, create)
	prepared := loginAct(t, f, loginAction(f, g, "prepare-key", p, ""))
	q := loginAction(f, g, "submit-key", prepared, "")
	q.Target = prepared.Handoff.ID
	path := "/v1/project-assistants/" + f.key.BotID + "/login-secret"
	oldNow := r.Now
	r.Now = func() time.Time { return oldNow().Add(16 * time.Minute) }
	if w := f.request(t, "POST", path, f.local, keySubmission(q, "SYNTHETIC_PROFILE_KEY_ONLY")); w.Code < 400 {
		t.Fatal("expired handoff accepted")
	}
	r.Now = oldNow
	// Expiry cannot be undone by changing the wall clock in production; refresh
	// the fixture target through a new explicit operation.
	p = awaitLogin(t, f, p.ID, "signed_out")
	prepare := loginAction(f, g, "prepare-key", p, "")
	prepare.Command = f.command(pa.LoginProfileOperation, "fresh-key-target")
	prepared = loginAct(t, f, prepare)
	q = loginAction(f, g, "submit-key", prepared, "")
	q.Command = f.command(pa.LoginProfileOperation, "fresh-key-submission")
	q.Target = prepared.Handoff.ID
	paServerOK(t, f.request(t, "POST", path, f.local, keySubmission(q, "SYNTHETIC_PROFILE_KEY_ONLY")))
	p = awaitLogin(t, f, p.ID, "signed_in")
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "select-encrypted-key"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: p.ID}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	g = f.get(t, f.local)
	if w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, loginAction(f, g, "prepare-key", p, "")); w.Code < 400 {
		t.Fatal("referenced key was replaceable")
	}
	paServerOK(t, f.send(t, f.local, "synthetic-key-native-turn"))
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		found = found || strings.Contains(m.Text, "Synthetic selected login "+p.ID) && strings.Contains(m.Text, "key=true")
	}
	if !found {
		t.Fatal("selected encrypted key never reached actual native ACP", x.Turns)
	}
}

func TestPrimaryAgentSettingStartsItsOwnNextNativeBinding(t *testing.T) {
	f, _ := loginCapabilitiesFixture(t)
	paServerOK(t, f.send(t, f.local, "before-primary-agent-change"))
	before := fallbackHTTPWait(t, f)
	g := f.get(t, f.local)
	oldBinding := g.Conversation.BindingID
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "change-primary-agent"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Agent: contract.ProjectAssistantAgentClaude}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	g = f.get(t, f.local)
	if g.Bot.Agent != contract.ProjectAssistantAgentClaude || g.Bot.Instructions.Revision <= before.Bot.Instructions.Revision {
		t.Fatal("primary Agent did not receive a new configuration identity", g.Bot)
	}
	sent := f.send(t, f.local, "after-primary-agent-change")
	paServerOK(t, sent)
	turnID := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
	after := fallbackHTTPWait(t, f)
	if after.Conversation.BindingID == oldBinding {
		t.Fatal("a changed Agent reused the old native binding")
	}
	found := false
	for _, binding := range after.Bindings {
		if binding.Fence.BindingID == after.Conversation.BindingID && binding.Agent == pa.ClaudeCode {
			found = true
		}
	}
	if !found {
		t.Fatal("new primary Agent binding absent")
	}
	for _, message := range after.Messages {
		if message.TurnID == turnID && strings.Contains(message.Text, "Synthetic selected login default") {
			return
		}
	}
	t.Fatal("new primary Agent did not answer through the local native entry")
}
