package http

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The source-owned fixture guarantees session/new has no prompt or effects.
// Actual ACP processes still open and close on each selected native identity;
// this guarantee is not asserted for installed vendor adapters.
type selectedFallbackNative struct {
	app.Provider
	mu                 sync.Mutex
	refused            map[string]bool
	opens              []app.OpenRequest
	lateProfile        string
	configurationCalls int
}

func (p *selectedFallbackNative) Configuration(ctx context.Context, b pa.Bot, directory, model string) (pa.ConfigurationCatalog, error) {
	p.mu.Lock()
	p.configurationCalls++
	p.mu.Unlock()
	if provider, ok := p.Provider.(interface {
		Configuration(context.Context, pa.Bot, string, string) (pa.ConfigurationCatalog, error)
	}); ok {
		return provider.Configuration(ctx, b, directory, model)
	}
	return pa.ConfigurationCatalog{}, &pa.Failure{Code: pa.Unsupported, Field: "synthetic_configuration"}
}

func (p *selectedFallbackNative) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, err := p.Provider.Open(ctx, q, sink)
	p.mu.Lock()
	p.opens = append(p.opens, q)
	refuse := p.refused[pa.LoginKey(q.Bot.Agent, q.Bot.LoginProfile)]
	late := p.lateProfile != "" && p.lateProfile == q.Bot.LoginProfile
	p.mu.Unlock()
	if err == nil && s != nil && refuse {
		return s, fallbackUsageRefusal()
	}
	if err == nil && s != nil && late {
		return lateSelectedSession{s}, nil
	}
	return s, err
}

type lateSelectedSession struct{ app.ProviderSession }

func (s lateSelectedSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	pending, err := s.ProviderSession.Send(ctx, q)
	if err != nil {
		return pending, err
	}
	return lateSelectedPending{pending}, nil
}

type lateSelectedPending struct{ app.PendingPrompt }

func (p lateSelectedPending) Wait(ctx context.Context) (app.PromptResult, error) {
	result, err := p.PendingPrompt.Wait(ctx)
	if err != nil {
		return result, err
	}
	// Even this typed fixture refusal cannot authorize replay after a real send.
	return result, fallbackUsageRefusal()
}

// Go's generated omitempty slice drops an explicit empty field. The browser
// wire retains it; preserve those exact bytes when issuing a fixture request.
func selectedFallbackEmptyRequest(t *testing.T, q contract.ProjectAssistantEditRequest, field string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	body["settings"].(map[string]any)[field] = []any{}
	return body
}

func TestSelectedFallbackProfilesCrossHTTPStoredStateAndActualACP(t *testing.T) {
	native := &selectedFallbackNative{refused: map[string]bool{}}
	f, _ := loginCapabilitiesFixture(t, func(provider app.Provider) app.Provider {
		native.Provider = provider
		return native
	})
	g := f.get(t, f.local)
	accounts := []contract.ProjectAssistantLoginProfile{}
	for i := 0; i < 2; i++ {
		p := loginAct(t, f, loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, fmt.Sprintf("Synthetic fallback %d", i)))
		loginAct(t, f, loginAction(f, g, "start", p, ""))
		accounts = append(accounts, awaitLogin(t, f, p.ID, "signed_in"))
	}
	edit := func(id string, steps []contract.ProjectAssistantFallbackStep) {
		t.Helper()
		g = f.get(t, f.local)
		q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, id), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: accounts[0].ID}, FallbackSteps: steps}}
		paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q))
	}
	steps := []contract.ProjectAssistantFallbackStep{{Agent: contract.ProjectAssistantAgentCodex, LoginProfile: accounts[1].ID}, {Agent: contract.ProjectAssistantAgentClaude}}
	edit("selected-logins", steps)
	g = f.get(t, f.local)
	if len(g.Bot.FallbackSteps) != 2 || g.Bot.FallbackSteps[0].LoginProfile != accounts[1].ID {
		t.Fatalf("selected chain did not round trip: %+v", g.Bot)
	}
	// An old client explicitly sending an empty legacy list preserves typed steps.
	q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "old-empty"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, FallbackAgents: []contract.ProjectAssistantAgent{}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, selectedFallbackEmptyRequest(t, q, "fallback_agents")))
	if len(f.get(t, f.local).Bot.FallbackSteps) != 2 {
		t.Fatal("legacy empty settings erased selected logins")
	}
	// A referenced fallback cannot be reauthenticated or removed underneath a turn.
	for _, action := range []string{"start", "remove"} {
		g = f.get(t, f.local)
		w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, loginAction(f, g, action, accounts[1], ""))
		if w.Code < 400 {
			t.Fatalf("referenced fallback allowed %s", action)
		}
	}
	native.mu.Lock()
	native.refused[pa.LoginKey(pa.Codex, accounts[0].ID)] = true
	native.mu.Unlock()
	run := func(id, want string) app.Export {
		t.Helper()
		sent := f.send(t, f.local, id)
		paServerOK(t, sent)
		turnID := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
		x := fallbackHTTPWait(t, f)
		found := false
		for _, m := range x.Messages {
			if m.TurnID == turnID && strings.Contains(m.Text, "Synthetic selected login "+want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("selected identity did not reach actual ACP (%s): %+v", want, x.Turns)
		}
		for _, turn := range x.Turns {
			if turn.ID == turnID && turn.State != pa.TurnSucceeded {
				t.Fatalf("turn did not complete: %+v", turn)
			}
		}
		return x
	}
	run("same-agent-second-login", accounts[1].ID)
	rests := store.New(f.s.store.ProjectAssistantDatabase())
	first, err := rests.ProfileRest(context.Background(), f.key.MachineID, pa.Codex, accounts[0].ID)
	if err != nil || first == nil {
		t.Fatalf("missing refused login rest: %+v %v", first, err)
	}
	second, err := rests.ProfileRest(context.Background(), f.key.MachineID, pa.Codex, accounts[1].ID)
	if err != nil || second != nil {
		t.Fatalf("one login rested another: %+v %v", second, err)
	}
	if rest, err := rests.AgentRest(context.Background(), f.key.MachineID, pa.Codex); err != nil || rest != nil {
		t.Fatal("managed login rested default")
	}
	// Rested primary is skipped; successful second login retains its own process
	// namespace. Then its qualified refusal chooses Claude's unselected default.
	run("same-agent-second-again", accounts[1].ID)
	native.mu.Lock()
	native.refused[pa.LoginKey(pa.Codex, accounts[1].ID)] = true
	native.mu.Unlock()
	x := run("different-agent-default", "default")
	native.mu.Lock()
	opens := append([]app.OpenRequest(nil), native.opens...)
	native.mu.Unlock()
	if len(opens) != 5 || opens[0].Bot.LoginProfile != accounts[0].ID || opens[1].Bot.LoginProfile != accounts[1].ID || opens[2].Bot.LoginProfile != accounts[1].ID || opens[4].Bot.Agent != pa.ClaudeCode || opens[4].Bot.LoginProfile != "" {
		t.Fatalf("native identity switched or retried incorrectly: %+v", opens)
	}
	for _, turn := range x.Turns {
		if len(turn.Attempts) > 0 && turn.Attempts[len(turn.Attempts)-1].Agent == pa.ClaudeCode && turn.Attempts[len(turn.Attempts)-1].LoginProfile != "" {
			t.Fatal("different Agent inherited selected login")
		}
	}
	// Unknown and wrong-Agent fallback IDs are refused before probe/launch.
	for i, step := range []contract.ProjectAssistantFallbackStep{{Agent: contract.ProjectAssistantAgentCodex, LoginProfile: strings.Repeat("f", 32)}, {Agent: contract.ProjectAssistantAgentClaude, LoginProfile: accounts[1].ID}} {
		g = f.get(t, f.local)
		native.mu.Lock()
		beforeProbes := native.configurationCalls
		native.mu.Unlock()
		bad := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, fmt.Sprintf("bad-step-%d", i)), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, FallbackSteps: []contract.ProjectAssistantFallbackStep{step}, RuntimeSettings: &contract.ProjectAssistantRuntimeSettings{Model: "synthetic-forbidden-probe", Options: map[string]string{}}}}
		w := f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, bad)
		if w.Code < 400 || f.get(t, f.local).Bot.Version != g.Bot.Version {
			t.Fatalf("wrong fallback identity mutated settings: %d", w.Code)
		}
		native.mu.Lock()
		afterProbes := native.configurationCalls
		native.mu.Unlock()
		if afterProbes != beforeProbes {
			t.Fatal("wrong fallback identity reached native model discovery before refusal")
		}
	}
}

func TestSelectedFallbackNeverReplaysALateActualNativeRefusal(t *testing.T) {
	native := &selectedFallbackNative{refused: map[string]bool{}}
	f, _ := loginCapabilitiesFixture(t, func(provider app.Provider) app.Provider {
		native.Provider = provider
		return native
	})
	g := f.get(t, f.local)
	accounts := []contract.ProjectAssistantLoginProfile{}
	for i := 0; i < 2; i++ {
		p := loginAct(t, f, loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, fmt.Sprintf("Synthetic late fallback %d", i)))
		loginAct(t, f, loginAction(f, g, "start", p, ""))
		accounts = append(accounts, awaitLogin(t, f, p.ID, "signed_in"))
	}
	g = f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "late-selected-logins"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: accounts[0].ID}, FallbackSteps: []contract.ProjectAssistantFallbackStep{{Agent: contract.ProjectAssistantAgentCodex, LoginProfile: accounts[1].ID}}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	native.mu.Lock()
	native.lateProfile = accounts[0].ID
	native.mu.Unlock()
	sent := f.send(t, f.local, "late-selected-native")
	paServerOK(t, sent)
	turnID := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
	x := fallbackHTTPWait(t, f)
	native.mu.Lock()
	opens := append([]app.OpenRequest(nil), native.opens...)
	native.mu.Unlock()
	if len(opens) != 1 || opens[0].Bot.LoginProfile != accounts[0].ID {
		t.Fatalf("late native failure rotated or replayed credentials: %+v", opens)
	}
	found := false
	for _, turn := range x.Turns {
		if turn.ID == turnID {
			found = true
			if turn.State != pa.TurnUnknown || len(turn.Attempts) == 0 {
				t.Fatalf("late native failure became replay-safe: %+v", turn)
			}
			final := turn.Attempts[len(turn.Attempts)-1]
			if !final.SendStarted || !final.NativeActivity || final.Stage != pa.AttemptHeld || final.Effect != "unknown" || final.LoginProfile != accounts[0].ID {
				t.Fatalf("lost late-effect barrier: %+v", final)
			}
		}
	}
	if !found {
		t.Fatal("native turn missing")
	}
}

func TestSelectedFallbackCaptureBlocksRekeyAfterSettingsDeselect(t *testing.T) {
	native := &selectedFallbackNative{refused: map[string]bool{}}
	f, _ := loginCapabilitiesFixture(t, func(provider app.Provider) app.Provider { native.Provider = provider; return native })
	g := f.get(t, f.local)
	accounts := []contract.ProjectAssistantLoginProfile{}
	for i := 0; i < 2; i++ {
		p := loginAct(t, f, loginAction(f, g, "create", contract.ProjectAssistantLoginProfile{}, fmt.Sprintf("Synthetic capture fallback %d", i)))
		loginAct(t, f, loginAction(f, g, "start", p, ""))
		accounts = append(accounts, awaitLogin(t, f, p.ID, "signed_in"))
	}
	g = f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "capture-selected-logins"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, LoginProfile: &contract.ProjectAssistantLoginSelection{ID: accounts[0].ID}, FallbackSteps: []contract.ProjectAssistantFallbackStep{{Agent: contract.ProjectAssistantAgentCodex, LoginProfile: accounts[1].ID}}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	hold := filepath.Join(f.s.cfg.Dir, "synthetic-native-hold")
	if err := os.WriteFile(hold, []byte("hold"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(hold) })
	native.mu.Lock()
	native.refused[pa.LoginKey(pa.Codex, accounts[0].ID)] = true
	native.mu.Unlock()
	sent := f.send(t, f.local, "captured-alternate-active")
	paServerOK(t, sent)
	turnID := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
	paResilienceAwait(t, func() bool {
		_, err := os.Stat(filepath.Join(f.s.cfg.Dir, "synthetic-native-ready"))
		return err == nil
	})
	paResilienceAwait(t, func() bool {
		turn, err := store.New(f.s.store.ProjectAssistantDatabase()).Turn(context.Background(), f.key, turnID)
		return err == nil && len(turn.Attempts) > 0 && turn.Attempts[len(turn.Attempts)-1].Stage == pa.AttemptSent
	})
	// Only the captured native turn refers to the second profile after this save.
	g = f.get(t, f.local)
	edit = contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "deselect-active-alternate"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, FallbackSteps: []contract.ProjectAssistantFallbackStep{}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, selectedFallbackEmptyRequest(t, edit, "fallback_steps")))
	if len(f.get(t, f.local).Bot.FallbackSteps) != 0 {
		t.Fatal("alternate remained configured")
	}
	for _, action := range []string{"start", "remove"} {
		g = f.get(t, f.local)
		w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/login-profile", f.local, loginAction(f, g, action, accounts[1], ""))
		if w.Code < 400 {
			t.Fatalf("captured alternate allowed %s after deselection", action)
		}
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		if m.TurnID == turnID && strings.Contains(m.Text, "Synthetic selected login "+accounts[1].ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("settings edit rotated captured native login")
	}
	g = f.get(t, f.local)
	current := awaitLogin(t, f, accounts[1].ID, "signed_in")
	loginAct(t, f, loginAction(f, g, "remove", current, ""))
	w := f.request(t, "GET", loginProfilesURL(f, g), f.local, nil)
	paServerOK(t, w)
	for _, profile := range paHTTPDecode[contract.ProjectAssistantLoginProfilesResponse](t, w).Profiles {
		if profile.ID == accounts[1].ID {
			t.Fatal("confirmed cleanup left alternate available")
		}
	}
}
