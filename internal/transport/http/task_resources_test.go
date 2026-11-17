package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestManagedResourceHTTPRequiresMachineAuthorityAndReviewedPlan(t *testing.T) {
	pane := shellPane(t.TempDir())
	s, h, local, machine := wholeServer(t, pane, pane)
	if err := s.broker.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	defer s.CloseTaskResources()
	output := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "result.txt"), []byte("retained result"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	owner := taskresources.OwnerState{Owner: taskresource.Owner{Kind: "task", ID: uuid.NewString(), Project: filepath.Dir(output)}, OutputRoot: output, EvidenceDigest: "synthetic accepted completion", Presence: "absent", UsePresence: "absent"}
	service := s.broker.Resources
	service.Uses = func(context.Context, taskresource.Resource) string { return "absent" }
	service.Owner = func(context.Context, string, string) (taskresources.OwnerState, error) { return owner, nil }
	service.FreeSpace = func() (int64, error) { return 1 << 40, nil }
	service.Clock = func() time.Time { return now.Add(-26 * time.Hour) }
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Host = "127.0.0.1:7757"
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			if token == local {
				r.Header.Set("Authorization", "Bearer "+token)
			} else {
				r.Header.Set("X-Clawdline-Orchestrator", token)
			}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	// A paired console/local read credential cannot be used as cleanup authority.
	if r := request("GET", "/v1/orchestrator/resources", local, nil); r.Code != 403 {
		t.Fatal(r.Code, r.Body)
	}
	r := request("POST", "/v1/orchestrator/resources/allocate", machine, map[string]any{"owner_kind": "task", "owner_id": owner.Owner.ID, "class": "scratch"})
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	var a contract.TaskResourceAllocation
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Resource.Owner.ID != owner.Owner.ID || a.Artifacts != output {
		t.Fatal(a)
	}
	if err := os.WriteFile(filepath.Join(a.Path, "temporary"), []byte("rebuild me"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner.Complete = true
	owner.CompletedAt = now.Add(-25 * time.Hour).Unix()
	owner.OutputsSaved = true
	service.Clock = func() time.Time { return now }
	r = request("POST", "/v1/orchestrator/resources/publish", machine, map[string]string{"resource": a.Resource.ID})
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	r = request("POST", "/v1/orchestrator/resources/plan", machine, map[string]any{"resources": []string{a.Resource.ID}, "scope": map[string]bool{"all_projects": true}})
	if r.Code < 400 {
		t.Fatal("scope body was accepted")
	}
	r = request("POST", "/v1/orchestrator/resources/plan", machine, map[string]any{"resources": []string{a.Resource.ID}})
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	var plan contract.TaskResourcePlan
	if err := json.Unmarshal(r.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Decision.Outcome != "eligible" {
		t.Fatal(plan)
	}
	if _, err := os.Stat(a.Path); err != nil {
		t.Fatal("plan mutated data", err)
	}
	execution := uuid.NewString()
	body := map[string]any{"plan": plan, "execution": execution}
	r = request("POST", "/v1/orchestrator/resources/apply", machine, body)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	var receipt contract.TaskResourceResult
	if err := json.Unmarshal(r.Body.Bytes(), &receipt); err != nil || len(receipt.Items) != 1 || receipt.Items[0].Outcome != "removed" {
		t.Fatal(receipt, err)
	}
	r = request("POST", "/v1/orchestrator/resources/apply", machine, body)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	if err := json.Unmarshal(r.Body.Bytes(), &receipt); err != nil || !receipt.Replayed {
		t.Fatal(receipt, err)
	}
	raw, err := os.ReadFile(filepath.Join(output, "result.txt"))
	if err != nil || string(raw) != "retained result" {
		t.Fatal(string(raw), err)
	}
}
