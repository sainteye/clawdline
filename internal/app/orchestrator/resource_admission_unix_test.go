//go:build darwin || linux

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestUnknownResourceMeasurementDoesNotStartLegacyCleanup(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", true)
	enableTestResources(t, b)
	marker := filepath.Join(cache, "retained")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	prior := b.Reading
	b.Reading = func(ctx context.Context) session.Inventory { reads++; return prior(ctx) }
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		return taskresource.Usage{}, errors.New("source measurement unavailable")
	}
	err := b.AdmitHeavyResources(ctx)
	if refusalCode(err) != "resource_legacy_measurement_unknown" {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatalf("unknown measurement triggered %d cleanup inventory reads", reads)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "keep" {
		t.Fatal("unknown admission changed retained legacy data", r.ID, err)
	}
}

func TestCallbackRefusalRetainsResourceLookupCauseAndDeadlineAfterAttemptEnds(t *testing.T) {
	for _, kind := range []string{"canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			enableTestResources(t, b)
			id := "e9100000-0000-4000-8000-000000000001"
			r, files := stranded(t, b, ctx, id, 0)
			r.Callback.Intent = CallbackIntentHeavy
			r.Repository, r.ProjectDir = r.Callback.Dir, r.Callback.Dir
			if err := b.save(ctx, r, HashSecret("fixture"), "task.briefed"); err != nil {
				t.Fatal(err)
			}
			probe, err := b.Resources.Allocate(ctx, ResourceOwnerScope(), "task", r.ID, taskresource.Cache)
			if err != nil {
				t.Fatal(err)
			}
			attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
				if kind == "canceled" {
					cancel()
				} else {
					<-c.Done()
				}
				_, err := b.Resources.Plan(c, ResourceOwnerScope(), []string{probe.Resource.ID})
				return taskresource.Usage{}, err
			}
			runCallbackStart(attempt, b, store.Effect{Kind: EffectCallbackStart, Subject: r.ID, Attempts: 0})
			got, _, err := b.Record(ctx, r.ID)
			if err != nil || got.State != StateFailure || !got.Callback.NotStarted || attempted(files) {
				t.Fatal("refusal lost or command attempted", got.State, err)
			}
			want := "resource_lookup_canceled"
			if kind == "deadline" {
				want = "resource_lookup_deadline_exceeded"
			}
			marker := "; resource_diagnostic="
			parts := strings.SplitN(got.Verdict, marker, 2)
			var diagnostic taskresources.Diagnostic
			if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &diagnostic) != nil {
				t.Fatal("missing durable diagnostic", got.Verdict)
			}
			if diagnostic.Code != want || diagnostic.Caller != "callback.start" || diagnostic.CallerID != r.ID || diagnostic.ResourceID != probe.Resource.ID || diagnostic.Phase != "resource.lookup" || !diagnostic.HasDeadline {
				t.Fatal(diagnostic)
			}
			events, err := b.Store.LatestEvents(ctx, "resource.admission_refused", []string{r.ID})
			if err != nil || len(events) != 1 {
				t.Fatal("server refusal receipt missing", events, err)
			}
			var saved taskresources.Diagnostic
			if err := json.Unmarshal(events[r.ID].Payload, &saved); err != nil || saved != diagnostic {
				t.Fatal("server and task evidence differ", saved, err)
			}
		})
	}
}

func TestInnerLeaseRefusalDoesNotRewriteAnExecutedCallbackAsNeverStarted(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", `printf started > "$CLAWDLINE_ARTIFACT_DIR/started"; while ! test -e release; do sleep 0.05; done; exit 1`)
	req.Dir = project
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	release := func() { _ = os.WriteFile(filepath.Join(project, "release"), nil, 0600) }
	t.Cleanup(func() {
		release()
		if current, _, err := b.Record(ctx, req.TaskID); err == nil && !current.State.Terminal() {
			_, _ = b.CancelTask(ctx, req.TaskID, "finish isolated admission fixture", "", CancelCaller{Person: true, Principal: "local"})
		}
	})
	var running Record
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		running, _, err = b.Record(ctx, req.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if !running.Callback.StartedAt.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback did not start", running.Verdict)
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		return taskresource.Usage{}, context.DeadlineExceeded
	}
	leaseID := uuid.NewString()
	_, err := b.Acquire(ctx, LeaseRequest{Resource: ResourceCompile, RequestID: leaseID, Holder: "fixture inner compile", CallbackTaskID: req.TaskID})
	ref, ok := err.(Refusal)
	if !ok || ref.Code != "resource_measurement_interrupted" {
		t.Fatal(err)
	}
	d, ok := ref.Extra["resource_diagnostic"].(taskresources.Diagnostic)
	if !ok || d.Caller != "heavy.acquire" || d.CallerID != leaseID || d.CallbackTaskID != req.TaskID || d.Phase != "measure.legacy" {
		t.Fatal(d)
	}
	after, _, err := b.Record(ctx, req.TaskID)
	if err != nil || after.State != running.State || after.Callback.NotStarted || after.Callback.StartedAt != running.Callback.StartedAt {
		t.Fatal("inner refusal rewrote launch facts", after.State, err)
	}
	release()
	after = settledWithin(t, b, ctx, req.TaskID, 5*time.Second)
	if after.State != StateFailure || after.Callback.NotStarted || after.Callback.Exit == nil || *after.Callback.Exit != 1 {
		t.Fatal("executed failure became never-started", after.Callback, after.Verdict)
	}
	raw, err := os.ReadFile(filepath.Join(after.Dir, "artifacts", "started"))
	if err != nil || string(raw) != "started" {
		t.Fatal("execution evidence lost", err)
	}
}
