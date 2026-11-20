package http

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func paFloorsPost(t *testing.T, f *paServerFixture, token string, body contract.ProjectAssistantTowerFloorRequest, header string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/project-assistant-floors", strings.NewReader(string(raw)))
	r.Host = "127.0.0.1"
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", header)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func paFloorsAnswer(t *testing.T, w *httptest.ResponseRecorder) contract.ProjectAssistantTowerFloorsResponse {
	t.Helper()
	paServerOK(t, w)
	var out contract.ProjectAssistantTowerFloorsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || out.ContractVersion != pa.ContractVersion {
		t.Fatalf("missing envelope or private response policy: %s", w.Body)
	}
	return out
}

func TestProjectAssistantFloorsDefaultOff(t *testing.T) {
	f := newPAServerFixture(t, true)
	paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistant-floors?machine_id="+f.key.MachineID, f.local, nil), 403, pa.Disabled)
	var tables int
	if err := f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'project_assistant_%'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("a default-off read installed feature tables: %d %v", tables, err)
	}
}

func TestProjectAssistantFloorsArePlacedByAnyDeviceThatMayAct(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	read := "/v1/project-assistant-floors?machine_id=" + f.key.MachineID
	empty := paFloorsAnswer(t, f.request(t, "GET", read, f.readonly, nil))
	if empty.MachineID != f.key.MachineID || empty.Version != 0 || empty.Floors == nil || len(empty.Floors) != 0 {
		t.Fatalf("nothing placed yet: %+v", empty)
	}
	place := contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "floor-one", ExpectedVersion: 0, ProjectID: "project-a", Floor: 2}
	// A device paired to read only cannot place a Project.
	if w := paFloorsPost(t, f, f.readonly, place, place.IdempotencyKey); w.Code != 403 {
		t.Fatalf("a read-only device placed a Project: HTTP %d %s", w.Code, w.Body)
	}
	if still := paFloorsAnswer(t, f.request(t, "GET", read, f.local, nil)); still.Version != 0 {
		t.Fatalf("a refused placement was stored: %+v", still)
	}
	// A paired device that may act places it like this machine does.
	placed := paFloorsAnswer(t, paFloorsPost(t, f, f.lan, place, place.IdempotencyKey))
	if placed.Version != 1 || !reflect.DeepEqual(placed.Floors, []contract.ProjectAssistantTowerFloor{{ProjectID: "project-a", Floor: 2}}) {
		t.Fatalf("placed: %+v", placed)
	}
	if got := paFloorsAnswer(t, f.request(t, "GET", read, f.local, nil)); !reflect.DeepEqual(got, placed) {
		t.Fatalf("every device reads the same placement: %+v", got)
	}
	// Retrying after a lost reply answers the stored document.
	if again := paFloorsAnswer(t, paFloorsPost(t, f, f.local, place, place.IdempotencyKey)); !reflect.DeepEqual(again, placed) {
		t.Fatalf("retry: %+v", again)
	}
	other := contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "floor-two", ExpectedVersion: 0, ProjectID: "project-b", Floor: 1}
	paAssertHTTPFailure(t, paFloorsPost(t, f, f.local, other, other.IdempotencyKey), 409, pa.StaleVersion)
	for name, bad := range map[string]struct {
		body   contract.ProjectAssistantTowerFloorRequest
		header string
	}{
		"floor":           {contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "k", ExpectedVersion: 1, ProjectID: "p", Floor: pa.MaxTowerFloors}, "k"},
		"negative floor":  {contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "k", ExpectedVersion: 1, ProjectID: "p", Floor: -1}, "k"},
		"header mismatch": {contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "k", ExpectedVersion: 1, ProjectID: "p", Floor: 0}, "other"},
		"no key":          {contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, ExpectedVersion: 1, ProjectID: "p", Floor: 0}, ""},
		"project":         {contract.ProjectAssistantTowerFloorRequest{MachineID: f.key.MachineID, IdempotencyKey: "k", ExpectedVersion: 1, ProjectID: "", Floor: 0}, "k"},
	} {
		w := paFloorsPost(t, f, f.local, bad.body, bad.header)
		if w.Code != 400 {
			t.Fatalf("%s: HTTP %d %s", name, w.Code, w.Body)
		}
	}
	paAssertHTTPFailure(t, paFloorsPost(t, f, f.local, contract.ProjectAssistantTowerFloorRequest{MachineID: "another-machine", IdempotencyKey: "k", ExpectedVersion: 1, ProjectID: "p", Floor: 0}, "k"), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistant-floors?machine_id=another-machine", f.local, nil), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "GET", read+"&extra=1", f.local, nil), 400, pa.Invalid)
}
