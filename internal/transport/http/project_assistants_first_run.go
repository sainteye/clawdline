package http

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// This reads only the local default logins. In particular, it does not start
// an ACP adapter, install dependencies or ask a model to check credentials.
func (s *Server) projectAssistantFirstRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a := accessOf(r)
	if !s.validBotSetupAccess(r, a) {
		paWriteHTTPFailure(w, paHTTPFailure(pa.ScopeDenied, "first_run_local"))
		return
	}
	if !s.projectAssistants.firstRun.TryLock() {
		paWriteHTTPFailure(w, paHTTPFailure(pa.CapacityFull, "first_run"))
		return
	}
	defer s.projectAssistants.firstRun.Unlock()
	status, err := s.projectAssistantFeature(r.Context())
	if err != nil || !status.Enabled {
		if err == nil {
			err = paHTTPFailure(pa.Disabled, "first_run")
		}
		paWriteHTTPFailure(w, err)
		return
	}
	probe := s.projectAssistants.firstRunStatus
	if probe == nil {
		native := projectassistantacp.InstalledLoginNative{}
		probe = func(ctx context.Context, agent pa.Agent) (string, error) {
			return native.Status(ctx, agent, nil)
		}
	}
	agents := []pa.Agent{pa.ClaudeCode, pa.Codex}
	rows := make([]contract.ProjectAssistantFirstRunAgent, len(agents))
	var workers sync.WaitGroup
	for i, agent := range agents {
		workers.Go(func() {
			row := contract.ProjectAssistantFirstRunAgent{Agent: contract.ProjectAssistantAgent(agent)}
			if !projectassistantacp.SupportsInstalledAdapters() && s.projectAssistants.firstRunStatus == nil {
				row.State = contract.ProjectAssistantFirstRunStateUnsupported
			} else {
				row.State = firstRunAgentState(r.Context(), agent, probe)
			}
			rows[i] = row
		})
	}
	workers.Wait()
	if !s.validBotSetupAccess(r, a) {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Revoked, "first_run_local"))
		return
	}
	writeJSON(w, contract.ProjectAssistantFirstRun{Agents: rows})
}

func (s *Server) validBotSetupAccess(r *http.Request, a access) bool {
	if a.gate == nil || a.gate != s.gate() || a.machine || cloudtransport.ViaCloud(r.Context()) {
		return false
	}
	verdict := a.gate.auth.Revalidate(a.verdict)
	return verdict.Allowed && verdict.Caps.Has(auth.Send) &&
		(verdict.Local || loopbackHost(r.Host) && loopbackPeer(r.RemoteAddr))
}

func firstRunAgentState(ctx context.Context, agent pa.Agent, probe func(context.Context, pa.Agent) (string, error)) contract.ProjectAssistantFirstRunState {
	state, err := probe(ctx, agent)
	switch pa.Code(err) {
	case pa.CLINotInstalled, pa.ProviderVersionUnsupported:
		var failure *pa.Failure
		if errors.As(err, &failure) && (failure.Field == "native_node" || failure.Field == "native_node_version") {
			return contract.ProjectAssistantFirstRunStateNeedsNode
		}
		return contract.ProjectAssistantFirstRunStateNeedsAgent
	case "":
		switch state {
		case "signed_in":
			return contract.ProjectAssistantFirstRunStateReady
		case "signed_out":
			return contract.ProjectAssistantFirstRunStateNeedsSignIn
		}
	}
	return contract.ProjectAssistantFirstRunStateUnknown
}
