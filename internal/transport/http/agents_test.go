package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentTranscriptPathSplitsBeforeItDecodes(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/%25fixture/agents/agent_fixture?limit=40", nil)
	sessionID, agentID, ok := agentPath(request)
	if !ok || sessionID != "%fixture" || agentID != "agent_fixture" {
		t.Fatalf("got session=%q agent=%q ok=%v", sessionID, agentID, ok)
	}
	for _, path := range []string{
		"/v1/sessions/root/agents/../other",
		"/v1/sessions/root/agents/agent%2Fother",
		"/v1/sessions/root/agents/",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if readablePath(request.URL.EscapedPath()) {
			if _, _, ok := agentPath(request); ok {
				t.Fatalf("unsafe path %q was accepted", path)
			}
		}
	}
}
