package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantFirstRunChecksLocalLoginsOnly(t *testing.T) {
	f := newPAServerFixture(t, false)
	var signedIn atomic.Bool
	var probes atomic.Int32
	f.s.projectAssistants.firstRunStatus = func(_ context.Context, agent pa.Agent) (string, error) {
		probes.Add(1)
		if agent == pa.ClaudeCode {
			return "", &pa.Failure{Code: pa.CLINotInstalled, Field: "native_cli"}
		}
		if signedIn.Load() {
			return "signed_in", nil
		}
		return "signed_out", nil
	}
	if err := f.s.StartProjectAssistants(t.Context()); err != nil {
		t.Fatal(err)
	}
	const path = "/v1/project-assistants/first-run"
	paAssertHTTPFailure(t, f.request(t, "GET", path, f.lan, nil), http.StatusForbidden, pa.ScopeDenied)
	if probes.Load() != 0 {
		t.Fatal("remote device invoked the local provider probe")
	}
	localBrowser := func() contract.ProjectAssistantFirstRun {
		t.Helper()
		request := httptest.NewRequest("GET", path, nil)
		request.Host = "localhost"
		request.RemoteAddr = "127.0.0.1:44000"
		request.Header.Set("Authorization", "Bearer "+f.lan)
		response := httptest.NewRecorder()
		f.h.ServeHTTP(response, request)
		paServerOK(t, response)
		var readiness contract.ProjectAssistantFirstRun
		if err := json.Unmarshal(response.Body.Bytes(), &readiness); err != nil || len(readiness.Agents) != 2 {
			t.Fatalf("first-run response: %s", response.Body)
		}
		return readiness
	}
	if before := localBrowser(); before.Agents[0].State != contract.ProjectAssistantFirstRunStateNeedsAgent ||
		before.Agents[1].State != contract.ProjectAssistantFirstRunStateNeedsSignIn {
		t.Fatalf("initial readiness: %+v", before)
	}
	signedIn.Store(true)
	if after := localBrowser(); after.Agents[1].State != contract.ProjectAssistantFirstRunStateReady {
		t.Fatalf("updated readiness: %+v", after)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("checking sign-in initialized a model provider")
	}
}
