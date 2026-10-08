package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestMachineStorageUnknownAndZeroAreDifferent(t *testing.T) {
	now := time.Unix(1234, 0)
	if disk := machineDiskFromCapacity(nil, time.Time{}, false); disk.Known || disk.At != 0 {
		t.Fatalf("missing capacity pass looks measured: %+v", disk)
	}
	rows := []capacity.Status{{Resolved: capacity.Resolved{Entry: capacity.Entry{Name: capacity.StoreDB}},
		Reading: capacity.Reading{Known: true, HasDiskFree: true, DiskFree: 0}}}
	if disk := machineDiskFromCapacity(rows, now, false); !disk.Known || disk.FreeBytes != 0 || disk.At != 1234 {
		t.Fatalf("zero free bytes was lost: %+v", disk)
	}
	if body, err := json.Marshal(machineDiskFromCapacity(rows, now, false)); err != nil || !strings.Contains(string(body), `"free_bytes":0`) {
		t.Fatalf("zero free bytes disappeared from JSON: %s, %v", body, err)
	}
	if disk := machineDiskFromCapacity(rows, now, true); disk.Known {
		t.Fatalf("stalled beat looks current: %+v", disk)
	}
	rows[0].Reading.HasDiskFree = false
	if disk := machineDiskFromCapacity(rows, now, false); disk.Known {
		t.Fatalf("failed statfs looks measured: %+v", disk)
	}
}

func TestMachineReclaimSummaryGroupsWithoutDisclosingPrivateRows(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, row := range []store.Reclaim{
		{Task: "fictional-task-a", Subject: "worktree", Outcome: "kept", Reason: "preserve_failed", Detail: []byte(`{"path":"/fictional/private/task-a"}`)},
		{Task: "fictional-task-f", Subject: "worktree", Outcome: "kept", Reason: "preserve_failed"},
		{Task: "fictional-task-b", Subject: "task_dir", Outcome: "kept", Reason: "within_grace"},
		{Task: "fictional-task-c", Subject: "worktree", Outcome: "kept", Reason: "/fictional/private/unsafe-reason"},
		{Task: "fictional-task-d", Subject: "worktree", Outcome: "removing", Reason: "landed"},
		{Task: "fictional-task-e", Subject: "worktree", Outcome: "removed", Reason: "landed"},
	} {
		row.LastAt = time.Unix(100, 0)
		if _, err := st.MarkReclaim(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st}
	grouped, err := st.ReclaimCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range grouped {
		if strings.Contains(row.Reason, "/fictional/") {
			t.Fatalf("grouped store query returned a private reason: %+v", row)
		}
	}
	got := s.machineReclaimSummary(ctx)
	if !got.Known || got.Kept != 4 || got.Failures != 2 || got.Removing != 1 || len(got.Reasons) != 2 || string(got.Reasons[0].Code) != "preserve_failed" {
		t.Fatalf("grouped standing decisions: %+v", got)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fictional-task", "/fictional/private", "path", "evidence"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("machine summary disclosed %q: %s", secret, body)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if cached := s.machineReclaimSummary(ctx); !cached.Known || cached.Kept != 4 {
		t.Fatalf("a page view repeated the store read inside cache age: %+v", cached)
	}
	// A failed read after cache expiry is unknown, never a zero backlog.
	s.reclaimSummaryAt = time.Time{}
	if failed := s.machineReclaimSummary(ctx); failed.Known {
		t.Fatalf("failed store read looks empty: %+v", failed)
	}
}
