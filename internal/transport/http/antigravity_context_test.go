//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestGeminiRoleStartBindsTheExistingActorAndKeepsOriginalResumeRole(t *testing.T) {
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	host := ownedAntigravityTerminal{s}
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	profile := t.TempDir()
	for _, dir := range []string{"antigravity-acp", ".git"} {
		if err := os.MkdirAll(filepath.Join(profile, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"},"useG1Credits":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLW_HTTP_PEER", "yes")
	cfg, _ := json.Marshal(map[string]any{"executable": exe, "accountDir": profile, "args": []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "places.json")
	paths, _ := json.Marshal([]string{profile})
	if err := os.WriteFile(fixture, paths, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projects.FixtureEnv, fixture)
	places := s.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatal("isolated Project missing")
	}
	stop, err := s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { stop() }()
	rec := httptest.NewRecorder()
	s.startPlace(rec, httptest.NewRequest(http.MethodPost, "/v1/places/fixture/start/antigravity/as/backend", nil), places[0].ID, "antigravity", "", "backend")
	var started contract.PlaceStarted
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &started) != nil || started.Persona != "backend" {
		t.Fatalf("start=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(profile, "last-prompt.json")); !os.IsNotExist(err) {
		t.Fatal("opening sent a model input")
	}
	inv, err := s.antigravitySessions.Load().Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 {
		t.Fatal("missing owned conversation", err)
	}
	row := inv.Sessions[0]
	binding, found, err := s.store.SquadBindingForSession(context.Background(), row.ID, row.ConversationID)
	if err != nil || !found || binding.DefinitionID != "clawdline.persona.backend" {
		t.Fatal("role actor not bound to owned and native identities", err)
	}
	if err := s.antigravitySessions.Load().Send(context.Background(), row, "original user input"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(profile, "last-prompt.json"))
		if len(data) > 0 {
			var prompt []struct {
				Text     string
				Resource struct{ Text string }
			}
			if json.Unmarshal(data, &prompt) != nil || len(prompt) != 2 || !strings.Contains(prompt[0].Resource.Text, "golang-concurrency") || prompt[1].Text != "original user input" {
				t.Fatalf("role/skill or original input missing: %s", data)
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	data, err := os.ReadFile(filepath.Join(profile, "last-capability-path"))
	if err != nil || len(data) == 0 {
		t.Fatal("native tool environment lacks its role actor", err)
	}
	capability, err := os.ReadFile(string(data))
	if err != nil {
		t.Fatal(err)
	}
	actor, bound, err := s.store.AuthenticateSquadActor(context.Background(), strings.TrimSpace(string(capability)))
	if err != nil || !bound || actor.TerminalID != row.ID || actor.ConversationID != row.ConversationID {
		t.Fatal("wrong capability owner", err)
	}
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wrong := httptest.NewRecorder()
	s.resumeOwnedAntigravity(wrong, httptest.NewRequest(http.MethodPost, "/resume", nil), places[0].ID, row.ConversationID, "architect")
	if wrong.Code != http.StatusConflict || !strings.Contains(wrong.Body.String(), "snapshot_persona_conflict") {
		t.Fatal("resume changed the original role", wrong.Code, wrong.Body.String())
	}
	resumed := httptest.NewRecorder()
	s.resumeOwnedAntigravity(resumed, httptest.NewRequest(http.MethodPost, "/resume", nil), places[0].ID, row.ConversationID, "backend")
	var result contract.PlaceResumed
	if resumed.Code != 200 || json.Unmarshal(resumed.Body.Bytes(), &result) != nil || result.ID != row.ID || result.Persona != "backend" {
		t.Fatal("original role did not resume", resumed.Code, resumed.Body.String())
	}
}
