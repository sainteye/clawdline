//go:build darwin || linux

package http

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestOwnedHTTPPeer(t *testing.T) {
	if os.Getenv("CLW_HTTP_PEER") != "yes" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	model, effort := "synthetic-flash", "high"
	efforts := []string{"low", "medium", "high"}
	config := func() []any {
		values := []any{}
		for _, value := range efforts {
			values = append(values, map[string]string{"value": value, "name": value})
		}
		return []any{
			map[string]any{"id": "model", "type": "select", "currentValue": model, "options": []any{map[string]string{"value": "synthetic-flash", "name": "Synthetic Flash"}, map[string]string{"value": "synthetic-pro", "name": "Synthetic Pro"}}},
			map[string]any{"id": "thinking_level", "type": "select", "currentValue": effort, "options": values},
		}
	}
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
				Prompt   []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(31)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]int{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "synthetic-http-native", "models": map[string]any{"currentModelId": "synthetic-flash-high", "availableModels": []any{map[string]string{"modelId": "synthetic-flash-high", "name": "Synthetic High"}, map[string]string{"modelId": "synthetic-flash-low", "name": "Synthetic Low"}}}, "_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
			result.(map[string]any)["configOptions"] = config()
		case "session/set_config_option":
			f, _ := os.OpenFile(filepath.Join(os.Getenv("GEMINI_HOME"), "config-writes"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			f.WriteString(req.Params.ConfigID + "=" + req.Params.Value + "\n")
			f.Close()
			if req.Params.ConfigID == "model" {
				model, effort, efforts = req.Params.Value, "medium", []string{"medium", "high"}
			} else {
				effort = req.Params.Value
			}
			result = map[string]any{"configOptions": config()}
		case "session/prompt":
			if len(req.Params.Prompt) == 1 && req.Params.Prompt[0].Text == "/usage" {
				f, _ := os.OpenFile(filepath.Join(os.Getenv("GEMINI_HOME"), "usage"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				f.WriteString("usage\n")
				f.Close()
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-http-native", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 62% remaining (resets Oct 29, 2026, 07:51 UTC)\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining"}}}})
				result = map[string]string{"stopReason": "end_turn"}
				break
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-http-native", "update": map[string]any{"sessionUpdate": "usage_update", "used": 12, "size": 100}}})
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-http-native", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "synthetic HTTP reply"}}}})
			result = map[string]string{"stopReason": "end_turn"}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}
func TestOwnedSessionUsesExistingStartInventorySendAndTranscript(t *testing.T) {
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	host := ownedAntigravityTerminal{s}
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	stop, err := s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if s.antigravitySessions.Load() != nil {
		t.Fatal("absent configuration enabled runtime")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLW_HTTP_PEER", "yes")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"executable": exe, "accountDir": dir, "args": []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// Explicit fixtures exclude every real provider history or Project.
	fixture := filepath.Join(dir, "places.json")
	paths, _ := json.Marshal([]string{dir})
	if err := os.WriteFile(fixture, paths, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projects.FixtureEnv, fixture)
	list := s.projectReaders().places.List(nil, 40)
	if len(list) != 1 {
		t.Fatalf("synthetic project list=%+v", list)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/places/fixture/start/antigravity", nil)
	s.startPlace(rec, req, list[0].ID, "antigravity", "", "")
	var answer contract.PlaceStarted
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &answer) != nil || !answer.OK || answer.Backend != contract.BackendOwned {
		t.Fatalf("existing start=%d %s", rec.Code, rec.Body.String())
	}
	inv := s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != answer.ID || inv.Sessions[0].Binding != session.BindingProtocol || inv.Sessions[0].State != session.StateIdle {
		t.Fatalf("inventory overwrote owned protocol: %+v", inv)
	}
	checks := 0
	ctx := app.WithOperationCheck(context.Background(), func() error { checks++; return nil })
	initial := httptest.NewRecorder()
	s.sessionInfoRoute(initial, httptest.NewRequest(http.MethodGet, "/v1/sessions/fixture/info?parts=summary", nil), answer.ID)
	var initialInfo contract.SessionInfoReply
	if initial.Code != 200 || json.Unmarshal(initial.Body.Bytes(), &initialInfo) != nil || initialInfo.Info.Session.Model != "synthetic-flash" || initialInfo.Info.Session.Effort != "high" || !reflect.DeepEqual(initialInfo.Info.Efforts, []string{"low", "medium", "high"}) || len(initialInfo.Info.Models) != 2 || initialInfo.Info.Models[0].Name != "Synthetic Flash" {
		t.Fatalf("initial native config=%s", initial.Body.String())
	}
	for _, command := range []string{"/effort low", "/model synthetic-pro"} {
		if _, err := s.actions().Send(ctx, answer.ID, command); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.actions().Send(ctx, answer.ID, "/effort low"); err == nil {
		t.Fatal("model-specific effort list not enforced")
	}
	writes, _ := os.ReadFile(filepath.Join(dir, "config-writes"))
	if string(writes) != "thinking_level=low\nmodel=synthetic-pro\n" {
		t.Fatalf("metadata writes=%q", writes)
	}
	if _, err := s.actions().Send(ctx, answer.ID, "synthetic HTTP input"); err != nil {
		t.Fatal(err)
	}
	if checks < 2 {
		t.Fatal("protocol bypassed normal operation checks")
	}
	var page contract.TranscriptPage
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page = s.transcriptPage(answer.ID, inv.Sessions[0], 20)
		if len(page.Entries) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if page.Evidence != contract.EvidenceStructured || len(page.Entries) != 2 || page.Entries[0].Role != "user" || page.Entries[1].Text != "synthetic HTTP reply" {
		t.Fatalf("existing transcript=%+v", page)
	}
	summaryResponse := httptest.NewRecorder()
	s.sessionInfoRoute(summaryResponse, httptest.NewRequest(http.MethodGet, "/v1/sessions/fixture/info?parts=summary", nil), answer.ID)
	var summary contract.SessionInfoReply
	if summaryResponse.Code != 200 || json.Unmarshal(summaryResponse.Body.Bytes(), &summary) != nil || len(summary.Info.Models) != 2 || summary.Info.Limits == nil || len(summary.Info.Limits.Windows) != 0 {
		t.Fatalf("summary=%s", summaryResponse.Body.String())
	}
	if summary.Info.Session.Model != "synthetic-pro" || summary.Info.Session.Effort != "medium" || !reflect.DeepEqual(summary.Info.Efforts, []string{"medium", "high"}) {
		t.Fatalf("summary native config=%s", summaryResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "usage")); !os.IsNotExist(err) {
		t.Fatal("summary queried provider quota")
	}
	infoResponse := httptest.NewRecorder()
	s.sessionInfoRoute(infoResponse, httptest.NewRequest(http.MethodGet, "/v1/sessions/fixture/info", nil), answer.ID)
	var info contract.SessionInfoReply
	if infoResponse.Code != 200 || json.Unmarshal(infoResponse.Body.Bytes(), &info) != nil || info.Info.Session.Model != "synthetic-pro" || info.Info.Session.Effort != "medium" || !reflect.DeepEqual(info.Info.Efforts, []string{"medium", "high"}) || info.Info.Context == nil || info.Info.Context.UsedPercent != 12 || info.Info.Context.WindowTokens != 100 || info.Info.Usage != nil || len(info.Info.Models) != 2 || info.Info.Models[1].Command != "synthetic-pro" || info.Info.Limits == nil || len(info.Info.Limits.Windows) != 1 || info.Info.Limits.Windows[0].UsedPercent != 38 {
		t.Fatalf("native facts missing or billing invented: %d %s", infoResponse.Code, infoResponse.Body.String())
	}
	readAt := info.Info.Limits.ReadAtMs
	summaryResponse = httptest.NewRecorder()
	s.sessionInfoRoute(summaryResponse, httptest.NewRequest(http.MethodGet, "/v1/sessions/fixture/info?parts=summary", nil), answer.ID)
	if json.Unmarshal(summaryResponse.Body.Bytes(), &summary) != nil || summary.Info.Limits.ReadAtMs != readAt {
		t.Fatal("summary fabricated quota freshness")
	}
	usage, _ := os.ReadFile(filepath.Join(dir, "usage"))
	if string(usage) != "usage\n" {
		t.Fatalf("full info quota calls=%q", usage)
	}
	page = s.transcriptPage(answer.ID, inv.Sessions[0], 20)
	if len(page.Entries) != 2 || page.Entries[1].Text != "synthetic HTTP reply" {
		t.Fatal("quota entered transcript", page)
	}
	for _, row := range s.availableAssistants() {
		if row.ID == "antigravity" && (row.AuthState != contract.StartAuthStateReady || !row.Capabilities.Send || !row.Capabilities.Transcript || row.Capabilities.Resume || row.Capabilities.Menus) {
			t.Fatalf("incorrect preview capabilities: %+v", row)
		}
	}
	if err := host.Close(context.Background(), inv.Sessions[0]); err != nil {
		t.Fatal(err)
	}
	stop()
	inv = s.inventory.Read(context.Background())
	if len(inv.Sessions) != 0 {
		t.Fatal("shutdown retained live Session")
	}
}
