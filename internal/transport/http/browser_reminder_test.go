package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserReminderIsDurableIdempotentAndDoesNotEnableUse(t *testing.T) {
	s, _, conversation := humanInterventionTestServer(t)
	path := "/v1/browser/reminder"
	body := `{"conversation":"` + conversation + `"}`
	send := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		s.browserReminderRoute(rr, personWorkV2Request(http.MethodPost, path, body, "remind-once"))
		return rr
	}
	first := send()
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body)
	}
	again := send()
	if again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	rows, _, err := s.humanInterventions().List(context.Background(), conversation)
	if err != nil || len(rows) != 1 || rows[0].Kind != "browser" || rows[0].TargetSession != "pane-target" {
		t.Fatalf("reminder: %+v %v", rows, err)
	}
	if s.browserSetting("browser_enabled") {
		t.Fatal("a reminder enabled browser use")
	}
	denied := httptest.NewRecorder()
	s.browserReminderRoute(denied, agentWorkV2Request(http.MethodPost, path, body, "agent-remind"))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("agent reminder: %d %s", denied.Code, denied.Body)
	}
}
