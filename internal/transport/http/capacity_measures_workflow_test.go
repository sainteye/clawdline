package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/workflow"
)

func TestWorkflowCapacityMeasuresStoredUsageAndReadFailures(t *testing.T) {
	s := capacityServer(t)
	for _, name := range []string{capacity.WorkflowLaunches, capacity.WorkflowSnapshotBytes} {
		if r := s.squadCapacityMeasures()[name](); !r.Known || r.Used != 0 {
			t.Fatalf("fresh %s: %+v", name, r)
		}
	}
	snapshot, err := workflow.Seal(workflow.Snapshot{ProfileID: "clawdline", Version: "1", Source: "builtin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PrepareWorkflowLaunch(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int64{capacity.WorkflowLaunches: 1, capacity.WorkflowSnapshotBytes: int64(len(document))} {
		if r := s.squadCapacityMeasures()[name](); !r.Known || r.Used != want {
			t.Fatalf("stored %s: %+v want %d", name, r, want)
		}
	}
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{capacity.WorkflowLaunches, capacity.WorkflowSnapshotBytes} {
		if r := s.squadCapacityMeasures()[name](); r.Known || r.Err == "" {
			t.Fatalf("closed %s reported zero: %+v", name, r)
		}
	}
	s.store = nil
	for _, name := range []string{capacity.WorkflowLaunches, capacity.WorkflowSnapshotBytes} {
		if r := s.squadCapacityMeasures()[name](); !r.Known || r.Used != 0 || r.Note != "no store attached" {
			t.Fatalf("detached %s: %+v", name, r)
		}
	}
}
