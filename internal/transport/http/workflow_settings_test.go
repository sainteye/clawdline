package http

import (
	"encoding/json"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowSettingsDefaultsAndIndependentPersistentSwitches(t *testing.T) {
	previous := terminal.ITermIntegration()
	t.Cleanup(func() { terminal.SetITermIntegration(previous); terminal.SetITermScan(true) })
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "state")}}
	call := func(method, body string) contract.WorkflowSettingsSnapshot {
		t.Helper()
		r := httptest.NewRequest(method, "/v1/settings/workflow", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.workflowSettingsRoute(w, r)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got contract.WorkflowSettingsSnapshot
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := call("GET", "")
	if !got.DispatchFullPermission || !got.SessionAutoDiscovery {
		t.Fatalf("defaults: %+v", got)
	}
	if got.ItermIntegration == nil || *got.ItermIntegration {
		t.Fatalf("default consent: %+v", got)
	}
	for _, on := range []bool{true, false} {
		got = call("POST", fmt.Sprintf(`{"iterm_integration":%t}`, on))
		if got.ItermIntegration == nil || *got.ItermIntegration != on || terminal.ITermIntegration() != on || !got.DispatchFullPermission || !got.SessionAutoDiscovery {
			t.Fatalf("workflow consent did not take effect independently: %+v", got)
		}
		if got = call("GET", ""); got.ItermIntegration == nil || *got.ItermIntegration != on {
			t.Fatalf("consent not persisted: %+v", got)
		}
	}
	got = call("POST", `{"dispatch_full_permission":false}`)
	if got.DispatchFullPermission || !got.SessionAutoDiscovery {
		t.Fatalf("permission changed discovery: %+v", got)
	}
	got = call("POST", `{"session_auto_discovery":false}`)
	if got.DispatchFullPermission || got.SessionAutoDiscovery {
		t.Fatalf("saved switches: %+v", got)
	}
	values, err := s.settingsFile().Read()
	if err != nil || values.DispatchPermission() != "ask" || values.SessionAutoDiscovery() {
		t.Fatalf("runtime values: %+v %v", values, err)
	}
	// A second Server reads the same persisted state, as after a daemon restart.
	s = &Server{cfg: s.cfg}
	if got = call("GET", ""); got.DispatchFullPermission || got.SessionAutoDiscovery {
		t.Fatalf("restart: %+v", got)
	}
	got = call("POST", `{"dispatch_full_permission":true,"session_auto_discovery":true}`)
	if !got.DispatchFullPermission || !got.SessionAutoDiscovery {
		t.Fatalf("re-enabled: %+v", got)
	}
}

func TestWorkflowSettingsRejectUnrelatedOrMalformedChanges(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for _, body := range []string{`{"remote":true}`, `{"iterm_scan":true}`, `{"iterm_integration":"on"}`, `{"session_auto_discovery":"off"}`, `{"dispatch_full_permission":"off"}`} {
		r := httptest.NewRequest(http.MethodPost, "/v1/settings/workflow", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.workflowSettingsRoute(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body)
		}
	}
}
