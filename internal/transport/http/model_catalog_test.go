package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// catalogFixture answers the native tier from a table and the agent tier
// with Codex's own shape from 2026-11-25: a Default entry copying the default
// model's note, and one entry per effort.
func catalogFixture(s *Server) {
	s.modelCatalogNative = func(agent string) []mc.Model {
		switch agent {
		case "claude":
			return []mc.Model{{Value: "opus", Name: "Opus 5.5"}}
		case "codex":
			return []mc.Model{{Value: "model-sol", Name: "6.2 Sol"}}
		}
		return nil
	}
	s.modelCatalogProbe = func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		if agent != "codex" {
			return pa.ConfigurationCatalog{}, &pa.Failure{Code: pa.Disabled, Field: "model_catalog_bots"}
		}
		return pa.ConfigurationCatalog{Agent: pa.Codex, Options: []pa.ConfigOption{{ID: "model", Category: "model", Current: "default", Choices: []pa.ConfigChoice{
			{Value: "default", Name: "Default", Description: "Fast, go-to model for coding and everyday work."},
			{Value: "model-sol/high", Name: "6.2 Sol (high)", Description: "Fast, go-to model for coding and everyday work."},
			{Value: "model-sol/xhigh", Name: "6.2 Sol (xhigh)"},
			{Value: "model-astra/high", Name: "6.2 Astra (high)"},
		}}}}, nil
	}
}

func catalogRead(t *testing.T, s *Server, target string) (int, contract.ModelCatalogAnswer) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.modelCatalogRoute(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var out contract.ModelCatalogAnswer
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out
}

func TestTheModelCatalogRouteAnswersEveryAgentNativeFirstThenTheAgentsOwnList(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "next")}}
	catalogFixture(s)
	code, all := catalogRead(t, s, "/v1/model-catalog")
	if code != 200 || len(all.Agents) != 4 {
		t.Fatalf("HTTP %d %+v", code, all)
	}
	for i, want := range mc.Agents {
		if all.Agents[i].Agent != want || all.Agents[i].Models == nil || all.Agents[i].Efforts == nil {
			t.Fatalf("agent %d = %+v", i, all.Agents[i])
		}
	}
	codex := all.Agents[1]
	if codex.Source != "native" || !codex.Refreshing || codex.Models[0].Value != "model-sol" {
		t.Fatalf("the first read is the native tier while the agent is asked: %+v", codex)
	}
	if len(codex.Efforts) != 2 || codex.Efforts[1] != "xhigh" || len(all.Agents[2].Efforts) != 5 {
		t.Fatalf("efforts: codex %v antigravity %v", codex.Efforts, all.Agents[2].Efforts)
	}
	s.modelCatalog().Wait()
	code, one := catalogRead(t, s, "/v1/model-catalog?agent=codex")
	if code != 200 || len(one.Agents) != 1 {
		t.Fatalf("HTTP %d %+v", code, one)
	}
	codex = one.Agents[0]
	if codex.Source != "agent" || codex.Refreshing || codex.DefaultName != "model-sol" || codex.ObservedAt == "" {
		t.Fatalf("after the agent answered: %+v", codex)
	}
	if len(codex.Models) != 2 || codex.Models[0].Value != "model-sol" || codex.Models[1].Value != "model-astra" {
		t.Fatalf("per-effort entries collapse to one launchable model each: %+v", codex.Models)
	}
	// A failed agent tier keeps the native list and names why.
	_, claude := catalogRead(t, s, "/v1/model-catalog?agent=claude")
	if c := claude.Agents[0]; c.Source != "native" || c.Problem != string(pa.Disabled) || c.ProblemField != "model_catalog_bots" || c.Models[0].Value != "opus" {
		t.Fatalf("claude: %+v", c)
	}
	if code, _ := catalogRead(t, s, "/v1/model-catalog?agent=gpt"); code != http.StatusBadRequest {
		t.Fatalf("unknown agent: HTTP %d", code)
	}
	if code, _ := catalogRead(t, s, "/v1/model-catalog?model=x"); code != http.StatusBadRequest {
		t.Fatalf("unknown parameter: HTTP %d", code)
	}
	rec := httptest.NewRecorder()
	s.modelCatalogRoute(rec, httptest.NewRequest(http.MethodPost, "/v1/model-catalog", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: HTTP %d", rec.Code)
	}
}

func TestTheDefaultModelsPickerListsTheCatalogsModels(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "next")}}
	catalogFixture(s)
	s.modelCatalog().ReadAll()
	s.modelCatalog().Wait()
	rec := defaultModelsCall(t, s, http.MethodGet, "", "")
	var out defaultModelsAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Models["codex"]; len(got) != 2 || got[0].Value != "model-sol" || got[1].Value != "model-astra" {
		t.Fatalf("codex models: %+v", got)
	}
	if got := out.Models["claude"]; len(got) != 1 || got[0].Label != "Opus 5.5" {
		t.Fatalf("claude models: %+v", got)
	}
}

// The phone failure of 2026-11-25: Bots settings on app.clawdline.com chose
// Codex and its model list never arrived. The list now comes through the
// relay word model-catalog, end to end through the bridge, the gate and the
// route, with no Bot named.
func TestThePhoneReadsCodexModelsThroughTheCloudWord(t *testing.T) {
	st := cloudStandIn(t)
	catalogFixture(st.server)
	ask := func(seq uint64) contract.ModelCatalogAgent {
		answer, payload := st.ask(t, seq, map[string]any{"type": "model-catalog", "session": "__clawdline_machine__", "request": "req-catalog", "agent": "codex"})
		if answer.Status != 200 {
			t.Fatalf("HTTP %d %v", answer.Status, payload)
		}
		raw, err := json.Marshal(bodyOf(t, payload))
		if err != nil {
			t.Fatal(err)
		}
		var out contract.ModelCatalogAnswer
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Agents) != 1 {
			t.Fatalf("body %s: %v", raw, err)
		}
		return out.Agents[0]
	}
	first := ask(1)
	if first.Agent != "codex" || len(first.Models) == 0 || !first.Refreshing {
		t.Fatalf("the first answer is immediate and says it is refreshing: %+v", first)
	}
	st.server.modelCatalog().Wait()
	next := ask(2)
	if next.Source != "agent" || next.Refreshing || next.DefaultName != "model-sol" || len(next.Models) != 2 {
		t.Fatalf("the agent's own list reaches the phone: %+v", next)
	}
}
