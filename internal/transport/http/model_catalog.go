package http

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/modelcatalog"
	"github.com/sainteye/clawdline/internal/contract"
	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// modelCatalog is the daemon's one model catalog (docs/model-catalog.md),
// built on first use. The fast tier reads the agent's own files or CLI; the
// agent tier asks the agent itself through the Bots discovery slot, under
// the machine's own model-catalog identity, and only while Bots are enabled.
func (s *Server) modelCatalog() *modelcatalog.Service {
	s.modelCatalogOnce.Do(func() {
		probe := s.modelCatalogProbe
		if probe == nil {
			probe = s.discoverModels
		}
		native := s.modelCatalogNative
		if native == nil {
			native = nativeModels
		}
		s.modelCatalogService = modelcatalog.New(modelcatalog.Options{Native: native, Probe: probe})
	})
	return s.modelCatalogService
}

func nativeModels(agent string) []mc.Model {
	home, _ := os.UserHomeDir()
	rows := transcript.Models(home, agent)
	out := make([]modelcatalog.NativeModel, 0, len(rows))
	for _, r := range rows {
		out = append(out, modelcatalog.NativeModel{Name: r.Name, Command: r.Command})
	}
	return modelcatalog.FromNative(out)
}

// discoverModels runs the existing ACP configuration probe without a Bot: in
// the model-catalog identity's own empty personal workspace, with the
// person's own login. A Host that is not running or not enabled means no
// agent tier, never a Bots setup started by a model read.
func (s *Server) discoverModels(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
	p := &s.projectAssistants
	p.mu.RLock()
	h, allowed, machine := p.host, p.allowed, p.machine
	p.mu.RUnlock()
	if h == nil || !allowed {
		return pa.ConfigurationCatalog{}, paHTTPFailure(pa.Disabled, "model_catalog_bots")
	}
	bot := pa.ModelCatalogBot(machine, pa.Agent(agent))
	workspace, err := projectAssistantProjects{s}.personalWorkspace(pa.ProjectSelection{MachineID: machine, ProjectID: bot.Project.ProjectID}, true)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return h.DiscoverModels(ctx, pa.Agent(agent), workspace.Directory)
}

// modelCatalogRoute answers GET /v1/model-catalog: every agent, or the one
// named by ?agent=. It never waits for an agent: a read that starts a
// background discovery says so with refreshing, and the reader polls.
func (s *Server) modelCatalogRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	for key := range q {
		if key != "agent" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "the only parameter is agent")
			return
		}
	}
	var catalogs []mc.Catalog
	if agent := q.Get("agent"); q.Has("agent") {
		c, err := s.modelCatalog().Read(agent)
		if err != nil {
			writeRefusal(w, http.StatusBadRequest, "unknown_agent", "agent is one of claude, codex, antigravity, grok")
			return
		}
		catalogs = []mc.Catalog{c}
	} else {
		catalogs = s.modelCatalog().ReadAll()
	}
	writeJSON(w, modelCatalogAnswer(catalogs))
}

func modelCatalogAnswer(catalogs []mc.Catalog) contract.ModelCatalogAnswer {
	out := contract.ModelCatalogAnswer{Agents: make([]contract.ModelCatalogAgent, 0, len(catalogs))}
	for _, c := range catalogs {
		models := make([]contract.ModelCatalogModel, 0, len(c.Models))
		for _, m := range c.Models {
			models = append(models, contract.ModelCatalogModel{Value: m.Value, Name: m.Name, Description: m.Description})
		}
		agent := contract.ModelCatalogAgent{
			Agent: c.Agent, Models: models, DefaultName: c.DefaultName, Efforts: append([]string{}, c.Efforts...),
			Source: string(c.Source), Refreshing: c.Refreshing, Problem: c.Problem, ProblemField: c.ProblemField,
		}
		if !c.ObservedAt.IsZero() {
			agent.ObservedAt = c.ObservedAt.UTC().Format(time.RFC3339)
		}
		out.Agents = append(out.Agents, agent)
	}
	return out
}
