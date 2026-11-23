package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// A task whose artifacts expired still reads with its summary, and its detail
// says the files were removed by retention rather than never written.
func TestTaskDetailSaysExpiredArtifactsAndKeepsTheSummary(t *testing.T) {
	const id = "a7e10000-0000-4000-8000-000000000030"
	s := paneServer(t, waitingPane("%90", ""))
	ctx := context.Background()
	ended := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
		Title: "Old delivery", State: orchestrator.StateSuccess, Claims: []string{}, ProjectDir: t.TempDir(),
		CreatedAt: ended.Add(-time.Hour), FinishedAt: ended, TimeoutMinutes: 60,
		Result: &taskdir.Result{Protocol: orchestrator.Protocol, TaskID: id, Status: "success",
			Summary: "The delivered summary", Artifacts: []string{"artifacts/report.md"}}}
	if _, err := s.store.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: r.ProjectDir, Assistant: r.Assistant,
		State: string(r.State), CreatedAt: r.CreatedAt, UpdatedAt: ended, SecretHash: orchestrator.HashSecret("fixture"),
		Record: r.Encode()}, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(s.orchestratorTaskRoute))
	defer server.Close()
	read := func() (task struct {
		Summary          string          `json:"summary"`
		ArtifactsExpired json.RawMessage `json:"artifacts_expired"`
	}) {
		t.Helper()
		response, err := server.Client().Get(server.URL + "/v1/orchestrator/tasks/" + id)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var detail struct {
			Task struct {
				Summary          string          `json:"summary"`
				ArtifactsExpired json.RawMessage `json:"artifacts_expired"`
			} `json:"task"`
		}
		if err := json.NewDecoder(response.Body).Decode(&detail); err != nil || response.StatusCode != 200 {
			t.Fatalf("status %d: %v", response.StatusCode, err)
		}
		return detail.Task
	}
	if got := read(); got.ArtifactsExpired != nil || got.Summary != "The delivered summary" {
		t.Fatalf("before expiry: %+v", got)
	}
	removedAt := ended.Add(31 * 24 * time.Hour)
	if _, err := s.store.MarkReclaim(ctx, store.Reclaim{Task: id, Subject: orchestrator.ReclaimTaskArtifacts,
		Outcome: orchestrator.ReclaimRemoved, Reason: orchestrator.WhyArtifactRetention, Bytes: 4096, LastAt: removedAt}, nil); err != nil {
		t.Fatal(err)
	}
	got := read()
	var expired struct {
		At               int64  `json:"at"`
		Reason           string `json:"reason"`
		Bytes            int64  `json:"bytes"`
		RetentionSeconds int64  `json:"retention_seconds"`
	}
	if err := json.Unmarshal(got.ArtifactsExpired, &expired); err != nil || got.Summary != "The delivered summary" ||
		expired.At != removedAt.Unix() || expired.Reason != "artifact_retention_expired" || expired.Bytes != 4096 ||
		expired.RetentionSeconds != taskresource.ArtifactRetentionSecondsLimit {
		t.Fatalf("after expiry: %+v %s %v", got, got.ArtifactsExpired, err)
	}
}
