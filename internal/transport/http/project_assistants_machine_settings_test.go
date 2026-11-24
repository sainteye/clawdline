package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paMachineSettingsPut(t *testing.T, f *paServerFixture, token string, body map[string]any, header string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/v1/project-assistant-shared-settings", strings.NewReader(string(raw)))
	r.Host = "127.0.0.1"
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", header)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func paMachineSettingsAnswer(t *testing.T, w *httptest.ResponseRecorder) machineSharedSettingsReply {
	t.Helper()
	paServerOK(t, w)
	var out machineSharedSettingsReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || out.ContractVersion != pa.ContractVersion {
		t.Fatalf("missing envelope or private response policy: %s", w.Body)
	}
	return out
}

// A phone or Cloud console names the machine whose settings it edits, and
// that machine's Bots authority decides, as for the tower floors.
func TestProjectAssistantMachineSettingsAreReadAndSavedByAnyDeviceThatMayAct(t *testing.T) {
	f := newPAServerFixture(t, true)
	read := "/v1/project-assistant-shared-settings?machine_id=" + f.key.MachineID
	paAssertHTTPFailure(t, f.request(t, "GET", read, f.local, nil), 403, pa.Disabled)
	f.enable(t)
	empty := paMachineSettingsAnswer(t, f.request(t, "GET", read, f.readonly, nil))
	if empty.MachineID != f.key.MachineID || empty.Settings.Version != 0 || empty.Agents == nil || empty.Limits["rules"] != pa.MaxDispatchRules {
		t.Fatalf("empty document: %+v", empty)
	}
	memory := []map[string]any{{"id": "m1", "text": "Answer with the conclusion first", "scope": map[string]any{"all": true}}}
	save := map[string]any{"machine_id": f.key.MachineID, "idempotency_key": "save-one", "expected_version": 0,
		"settings": map[string]any{"version": 0, "rules": []any{}, "memory": memory}}
	if w := paMachineSettingsPut(t, f, f.readonly, save, "save-one"); w.Code != 403 {
		t.Fatalf("a read-only device saved shared settings: HTTP %d %s", w.Code, w.Body)
	}
	saved := paMachineSettingsAnswer(t, paMachineSettingsPut(t, f, f.lan, save, "save-one"))
	if saved.Settings.Version != 1 || len(saved.Settings.Memory) != 1 || saved.Settings.Memory[0].Text != "Answer with the conclusion first" {
		t.Fatalf("saved: %+v", saved)
	}
	// The machine's own route reads the same document.
	var local sharedSettingsReply
	w := f.request(t, "GET", "/v1/project-assistants/shared-settings", f.local, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &local); err != nil || local.Settings.Version != 1 {
		t.Fatalf("local route: %v %s", err, w.Body)
	}
	paAssertHTTPFailure(t, paMachineSettingsPut(t, f, f.local, save, "save-one"), 409, pa.StaleVersion)
	paAssertHTTPFailure(t, paMachineSettingsPut(t, f, f.local, save, "other"), 400, pa.Invalid)
	other := map[string]any{"machine_id": "another-machine", "idempotency_key": "k", "expected_version": 1, "settings": map[string]any{"version": 1, "rules": []any{}, "memory": []any{}}}
	paAssertHTTPFailure(t, paMachineSettingsPut(t, f, f.local, other, "k"), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistant-shared-settings?machine_id=another-machine", f.local, nil), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "GET", read+"&extra=1", f.local, nil), 400, pa.Invalid)
}
