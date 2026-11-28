package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestResourceAdmissionHTTPAllowsOverTargetStorageDuringCacheMaintenance(t *testing.T) {
	pane := shellPane(t.TempDir())
	s, h, _, machine := wholeServer(t, pane, pane)
	if err := s.broker.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	defer s.CloseTaskResources()
	ctx := context.Background()
	service := s.broker.Resources
	service.FreeSpace = func() (int64, error) { return 1 << 40, nil }
	service.Uses = func(context.Context, taskresource.Resource) string { return "absent" }
	project, output := t.TempDir(), t.TempDir()
	completedAt := time.Now().Add(-25 * time.Hour).Unix()
	if err := os.WriteFile(filepath.Join(output, "result.txt"), []byte("retained result"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Owner = func(_ context.Context, kind, id string) (taskresources.OwnerState, error) {
		return taskresources.OwnerState{
			Owner:    taskresource.Owner{Kind: kind, ID: id, Project: project},
			Complete: true, CompletedAt: completedAt,
			Presence: "absent", UsePresence: "absent", OutputsSaved: true,
			OutputRoot: output, EvidenceDigest: "fixture result",
		}, nil
	}
	scope := taskresources.Scope{Actor: "daemon", AllProjects: true, Inspect: true, Publish: true}
	var cached taskresource.Resource
	var entry string
	if err := service.UseSharedGo(ctx, scope, project, func(r taskresource.Resource, path string) error {
		cached = r
		prefix := filepath.Join(path, "aa")
		if err := os.Mkdir(prefix, 0o700); err != nil {
			return err
		}
		entry = filepath.Join(prefix, strings.Repeat("a", 64)+"-a")
		return os.WriteFile(entry, []byte("rebuildable Go cache entry"), 0o600)
	}); err != nil {
		t.Fatal(err)
	}

	var sweeping atomic.Bool
	var sweeps atomic.Int32
	service.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		if !sweeping.CompareAndSwap(false, true) {
			return taskresource.Usage{Known: true}, nil
		}
		defer sweeping.Store(false)
		for range 3 {
			if _, _, err := s.broker.SweepTaskResources(c, ""); err != nil {
				return taskresource.Usage{}, err
			}
			sweeps.Add(1)
		}
		return taskresource.Usage{
			Known:        true,
			CacheTotal:   taskresource.CacheTotalBytesLimit + 1,
			ScratchTotal: taskresource.ScratchTotalBytesLimit + 1,
		}, nil
	}

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Host = "127.0.0.1:7757"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Clawdline-Orchestrator", machine)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := post("/v1/orchestrator/resources/admit", "{}")
	if rec.Code != 200 {
		t.Fatalf("admission after %d completed sweeps: HTTP %d %s", sweeps.Load(), rec.Code, rec.Body)
	}
	usage := service.Usage()
	if sweeps.Load() != 3 || !usage.Known || usage.ScratchTotal != taskresource.ScratchTotalBytesLimit+1 || usage.CacheTotal <= taskresource.CacheTotalBytesLimit {
		t.Fatalf("expected one scan reporting over-target totals and three overlapping sweeps, got %d sweeps and %+v", sweeps.Load(), usage)
	}

	// The dev installer submits the same compile lease identity until storage
	// preparation has a durable outcome. Even another sweep before the lease
	// poll must leave the prepared admission usable.
	s.broker.AsyncResourceAdmission = true
	id := uuid.NewString()
	leaseBody := `{"resource":"heavy_compile","request_id":"` + id + `","holder":"dev-install fixture"}`
	rec = post("/v1/orchestrator/leases", leaseBody)
	var lease contract.LeaseReply
	if err := json.Unmarshal(rec.Body.Bytes(), &lease); err != nil || rec.Code != 200 || lease.State != "queued" {
		t.Fatal(rec.Code, rec.Body, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		events, err := s.broker.Store.LatestEvents(ctx, "resource.preparation.finished", []string{id})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dev-install lease did not finish storage preparation")
		}
		time.Sleep(time.Millisecond)
	}
	sweeping.Store(true)
	if _, _, err := s.broker.SweepTaskResources(ctx, ""); err != nil {
		t.Fatal(err)
	}
	sweeping.Store(false)
	rec = post("/v1/orchestrator/leases", leaseBody)
	if err := json.Unmarshal(rec.Body.Bytes(), &lease); err != nil || rec.Code != 200 || lease.State != "granted" || sweeps.Load() != 6 {
		t.Fatalf("dev-install lease after %d overlapping sweeps: HTTP %d %s, decode %v", sweeps.Load(), rec.Code, rec.Body, err)
	}
	post("/v1/orchestrator/leases/release", `{"resource":"heavy_compile","request_id":"`+id+`"}`)

	resource, err := s.broker.Store.TaskResource(ctx, cached.ID)
	if err != nil || resource.Manifest == "" || resource.LastExecution != "" {
		t.Fatal("a within-budget cache should be sealed without starting a cleanup intent", resource, err)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatal("within-budget cache data changed", err)
	}
}

func TestResourceFailureHTTPDistinguishesLeaseAdmitCancellationAndMissingRecord(t *testing.T) {
	for _, kind := range []string{"lease", "admit", "canceled admit", "churning admit", "missing"} {
		t.Run(kind, func(t *testing.T) {
			pane := shellPane(t.TempDir())
			s, h, _, machine := wholeServer(t, pane, pane)
			if err := s.broker.EnableTaskResources(); err != nil {
				t.Fatal(err)
			}
			defer s.CloseTaskResources()
			const secretData = "private database query contents"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service := s.broker.Resources
			owner := taskresource.Owner{Kind: "task", ID: uuid.NewString(), Project: t.TempDir()}
			service.Owner = func(context.Context, string, string) (taskresources.OwnerState, error) {
				return taskresources.OwnerState{Owner: owner}, nil
			}
			service.FreeSpace = func() (int64, error) { return 1 << 40, nil }
			scans := 0
			s.broker.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
				scans++
				if kind == "canceled admit" {
					cancel()
					return taskresource.Usage{}, c.Err()
				}
				if kind == "churning admit" {
					_, err := service.Allocate(c, taskresources.Scope{Actor: "daemon", AllProjects: true, Publish: true}, owner.Kind, owner.ID, taskresource.Cache)
					return taskresource.Usage{Known: true}, err
				}
				return taskresource.Usage{}, errors.New(secretData)
			}
			id := uuid.NewString()
			path := "/v1/orchestrator/resources/admit"
			body := map[string]any{}
			if kind == "lease" {
				path = "/v1/orchestrator/leases"
				body = map[string]any{"request_id": id, "resource": "heavy_compile", "holder": "fixture build"}
			}
			if kind == "missing" {
				path = "/v1/orchestrator/resources/plan"
				body = map[string]any{"resources": []string{id}}
			}
			raw, _ := json.Marshal(body)
			r := httptest.NewRequest("POST", path, strings.NewReader(string(raw))).WithContext(ctx)
			r.Host = "127.0.0.1:7757"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Clawdline-Orchestrator", machine)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			var response struct {
				Error struct {
					Code       string                   `json:"code"`
					Diagnostic taskresources.Diagnostic `json:"resource_diagnostic"`
					Recorded   bool                     `json:"resource_diagnostic_recorded"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(rec.Code, rec.Body, err)
			}
			d := response.Error.Diagnostic
			var scalar map[string]map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &scalar); err != nil || scalar["error"]["phase"] != d.Phase || scalar["error"]["caller"] != d.Caller {
				t.Fatal("CLI scalar correlation lost", rec.Body)
			}
			want := "resource_legacy_measurement_unknown"
			if kind == "missing" {
				want = "resource_not_found"
			}
			if kind == "canceled admit" {
				want = "resource_measurement_interrupted"
			}
			if kind == "churning admit" {
				want = "resource_measurement_changed"
			}
			if rec.Code < 400 || response.Error.Code != want || d.Code != want || strings.Contains(rec.Body.String(), secretData) {
				t.Fatal(rec.Code, rec.Body)
			}
			switch kind {
			case "missing":
				if d.Phase != "resource.lookup" || d.ResourceID != id || d.Cause != "not_found" {
					t.Fatal(d)
				}
			case "churning admit":
				if rec.Code != 503 || scans != taskresource.AdmissionMeasurementAttemptsLimit || d.Phase != "measure.revision" || d.Cause != "revision_changed" || !response.Error.Recorded || service.Usage().Known {
					t.Fatalf("expected bounded concurrent-change failure, got HTTP %d, %d scans, %+v", rec.Code, scans, d)
				}
			default:
				if d.Phase != "measure.legacy" || d.ResourceID != "" || !response.Error.Recorded {
					t.Fatal("refusal correlation or receipt lost", d, response.Error.Recorded)
				}
				if scans != 1 {
					t.Fatalf("read failure attempted %d measurements, want one", scans)
				}
				if kind == "canceled admit" && (d.Cause != "canceled" || d.ContextState != "canceled") {
					t.Fatal(d)
				}
			}
			if kind == "lease" {
				if d.Caller != "heavy.acquire" || d.CallerID != id || !d.HasDeadline {
					t.Fatal(d)
				}
				views, err := s.broker.Leases(context.Background())
				if err != nil || len(views) != 0 {
					t.Fatal("refused lease was acquired", views, err)
				}
			} else if kind != "missing" && (d.Caller != "resources.admit" || d.CallerID != "" || d.HasDeadline) {
				t.Fatal(d)
			}
		})
	}
}

func TestResourceAdmissionClientTimeoutDoesNotEraseTheServerRefusal(t *testing.T) {
	pane := shellPane(t.TempDir())
	s, h, _, machine := wholeServer(t, pane, pane)
	if err := s.broker.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	defer s.CloseTaskResources()
	entered, finished := make(chan struct{}), make(chan struct{})
	s.broker.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		close(entered)
		<-c.Done()
		return taskresource.Usage{}, c.Err()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); h.ServeHTTP(w, r) }))
	defer server.Close()
	client := server.Client()
	client.Timeout = 2 * time.Second
	req, err := http.NewRequest("POST", server.URL+"/v1/orchestrator/resources/admit", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Clawdline-Orchestrator", machine)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("client unexpectedly observed a terminal response")
	}
	select {
	case <-entered:
	default:
		t.Fatal("request did not reach admission", err)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled handler did not finish")
	}
	events, err := s.broker.Store.LatestEvents(context.Background(), "resource.admission_refused", []string{"resources.admit"})
	if err != nil || len(events) != 1 {
		t.Fatal("server decision was lost after client timeout", events, err)
	}
	var diagnostic taskresources.Diagnostic
	if err := json.Unmarshal(events["resources.admit"].Payload, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic.Code != "resource_measurement_interrupted" || diagnostic.Caller != "resources.admit" || diagnostic.CallerID != "" || diagnostic.Phase != "measure.legacy" || diagnostic.Cause != "canceled" || diagnostic.ContextState != "canceled" || diagnostic.HasDeadline {
		t.Fatal(diagnostic)
	}
}

func TestCompileHTTPReturnsQueuedWhileDiskPreparationContinues(t *testing.T) {
	pane := shellPane(t.TempDir())
	s, h, _, machine := wholeServer(t, pane, pane)
	if err := s.broker.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	defer s.CloseTaskResources()
	s.broker.AsyncResourceAdmission = true
	s.broker.Resources.FreeSpace = func() (int64, error) { return 1 << 40, nil }
	release := make(chan struct{})
	released := false
	s.broker.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		select {
		case <-release:
			return taskresource.Usage{Known: true}, nil
		case <-c.Done():
			return taskresource.Usage{}, c.Err()
		}
	}
	server := httptest.NewServer(h)
	defer server.Close()
	id := uuid.NewString()
	defer func() {
		if !released {
			close(release)
		}
	}()
	post := func(path, body string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		r.Header.Set("X-Clawdline-Orchestrator", machine)
		r.Header.Set("Content-Type", "application/json")
		r.Host = "127.0.0.1:7757"
		client := &http.Client{Timeout: time.Second}
		out, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	begin := time.Now()
	resp := post("/v1/orchestrator/leases", `{"resource":"heavy_compile","request_id":"`+id+`","holder":"synthetic HTTP preparation"}`)
	var a contract.LeaseReply
	err := json.NewDecoder(resp.Body).Decode(&a)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || a.State != "queued" || a.HoldReason != "storage_preparing" {
		t.Fatal(a, resp.StatusCode, err)
	}
	t.Logf("queued HTTP response in %s while storage is blocked (client deadline 1s)", time.Since(begin))
	// Cross the client's deadline with preparation still blocked. Closing that
	// HTTP response must not cancel the durable waiter or background work.
	time.Sleep(1100 * time.Millisecond)
	close(release)
	released = true
	deadline := time.Now().Add(3 * time.Second)
	for {
		events, err := s.broker.Store.LatestEvents(context.Background(), "resource.preparation.finished", []string{id})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request cancellation stopped accepted preparation")
		}
		time.Sleep(time.Millisecond)
	}
	resp = post("/v1/orchestrator/leases", `{"resource":"heavy_compile","request_id":"`+id+`","holder":"synthetic HTTP preparation"}`)
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil || a.State != "granted" {
		t.Fatal(a, err)
	}
	t.Logf("prepared and granted after %s with the same request identity", time.Since(begin))
	resp = post("/v1/orchestrator/leases/release", `{"resource":"heavy_compile","request_id":"`+id+`"}`)
	resp.Body.Close()
}

func TestSharedGoAttachmentHTTPRequiresMachineAndClosedCallbackIdentity(t *testing.T) {
	pane := shellPane(t.TempDir())
	s, h, local, machine := wholeServer(t, pane, pane)
	if err := s.broker.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	defer s.CloseTaskResources()
	for _, test := range []struct {
		token, body string
		status      int
	}{{local, `{"callback_task_id":"e9200000-0000-4000-8000-000000000001"}`, 403}, {machine, `{"callback_task_id":"e9200000-0000-4000-8000-000000000001","path":"/foreign"}`, 400}} {
		req := httptest.NewRequest("POST", "/v1/orchestrator/resources/use-go", strings.NewReader(test.body))
		req.Host = "127.0.0.1:7757"
		req.Header.Set("Content-Type", "application/json")
		if test.token == local {
			req.Header.Set("Authorization", "Bearer "+test.token)
		} else {
			req.Header.Set("X-Clawdline-Orchestrator", test.token)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != test.status {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}
