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
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(31)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]int{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "synthetic-http-native", "models": map[string]string{"currentModelId": "synthetic-flash-high"}, "_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
		case "session/prompt":
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
	infoResponse := httptest.NewRecorder()
	s.sessionInfoRoute(infoResponse, httptest.NewRequest(http.MethodGet, "/v1/sessions/fixture/info", nil), answer.ID)
	var info contract.SessionInfoReply
	if infoResponse.Code != 200 || json.Unmarshal(infoResponse.Body.Bytes(), &info) != nil || info.Info.Session.Model != "synthetic-flash-high" || info.Info.Context == nil || info.Info.Context.UsedPercent != 12 || info.Info.Context.WindowTokens != 100 || info.Info.Usage != nil || len(info.Info.Models) != 0 {
		t.Fatalf("native facts invented billing or terminal model controls: %d %s", infoResponse.Code, infoResponse.Body.String())
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
