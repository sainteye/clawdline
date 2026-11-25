package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A terminal session's panel offers the daemon's one model catalog, not a
// second reading of the agent's files: the catalog here names a model no
// file on this machine does, and the panel lists it.
func TestSessionInfoOfATerminalSessionListsTheModelCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("PATH", home)
	opus := ""
	for _, m := range transcript.ClaudeModels {
		if m.Command == "opus" {
			opus = m.ID
		}
	}
	if opus == "" {
		t.Fatal("no opus alias to resolve")
	}
	cases := []struct {
		assistant session.Assistant
		want      []contract.SessionModel
	}{
		// Claude Code takes the alias and reports the version: the row's id is
		// the version, its command the alias. A full value is its own id.
		{session.AssistantClaude, []contract.SessionModel{
			{ID: opus, Name: "Opus (catalog)", Command: "opus"},
			{ID: "claude-catalog-only-9", Name: "Catalog Only 9", Command: "claude-catalog-only-9"},
		}},
		{session.AssistantCodex, []contract.SessionModel{{ID: "model-sol", Name: "6.2 Sol", Command: "model-sol"}}},
		// No catalog source for Grok: an empty list, never null.
		{session.AssistantGrok, []contract.SessionModel{}},
	}
	for _, c := range cases {
		t.Run(string(c.assistant), func(t *testing.T) {
			p := &pane{s: session.Session{ID: "%19", Backend: session.BackendTmux,
				Assistant: c.assistant, CWD: home, State: session.StateIdle}}
			s := paneServer(t, p)
			s.modelCatalogNative = func(agent string) []mc.Model {
				switch agent {
				case "claude":
					return []mc.Model{{Value: "opus", Name: "Opus (catalog)"}, {Value: "claude-catalog-only-9", Name: "Catalog Only 9"}}
				case "codex":
					return []mc.Model{{Value: "model-sol", Name: "6.2 Sol"}}
				}
				return nil
			}
			s.modelCatalogProbe = func(context.Context, string) (pa.ConfigurationCatalog, error) {
				return pa.ConfigurationCatalog{}, &pa.Failure{Code: pa.Disabled, Field: "model_catalog_bots"}
			}
			t.Cleanup(func() { s.modelCatalog().Wait() })
			for _, parts := range []string{"summary", "all"} {
				rec := httptest.NewRecorder()
				s.sessionInfoRoute(rec, httptest.NewRequest("GET", "/v1/sessions/%2519/info?parts="+parts, nil), "%19")
				var reply struct {
					Info struct {
						Models json.RawMessage `json:"models"`
					} `json:"info"`
				}
				var models []contract.SessionModel
				if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || json.Unmarshal(reply.Info.Models, &models) != nil ||
					models == nil || !reflect.DeepEqual(models, c.want) {
					t.Fatalf("%s: %d %s", parts, rec.Code, rec.Body.String())
				}
			}
		})
	}
}
