package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func syntheticResource(t *testing.T, n int) taskresource.Resource {
	t.Helper()
	id := fmt.Sprintf("10000000-0000-4000-8000-%012d", n)
	return taskresource.Resource{ID: id, Generation: fmt.Sprintf("20000000-0000-4000-8000-%012d", n), Owner: taskresource.Owner{Kind: "task", ID: "30000000-0000-4000-8000-000000000001", Project: t.TempDir()}, Class: taskresource.Scratch, RelativePath: id, Identity: fmt.Sprintf("synthetic-device:directory-%d", n), CreatedAt: time.Now().Unix()}
}

func TestTaskResourceOwnershipAndReceiptsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := syntheticResource(t, 1)
	if err := s.RegisterTaskResource(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterTaskResource(ctx, r); err != nil {
		t.Fatal("idempotent registration", err)
	}
	if n, err := s.EventCount(ctx, "resource.registered"); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	_, err = s.UpdateTaskResource(ctx, r.ID, "resource.cleanup_intent", time.Now(), func(r *taskresource.Resource) error {
		r.Manifest = "synthetic-manifest"
		r.LastOutcome = "removing"
		r.LastExecution = "synthetic-execution"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.TaskResource(ctx, r.ID)
	if err != nil || got.LastOutcome != "removing" || got.LastExecution != "synthetic-execution" || !sameResourceIdentity(r, got) {
		t.Fatal(got, err)
	}
	for _, mutate := range []func(*taskresource.Resource){
		func(r *taskresource.Resource) { r.Owner.ID = "30000000-0000-4000-8000-000000000002" },
		func(r *taskresource.Resource) { r.Class = taskresource.Cache },
		func(r *taskresource.Resource) { r.Identity = "different-directory" },
		func(r *taskresource.Resource) { r.Generation = "20000000-0000-4000-8000-000000000002" },
	} {
		_, err := s.UpdateTaskResource(ctx, r.ID, "resource.changed", time.Now(), func(r *taskresource.Resource) error { mutate(r); return nil })
		if !errors.Is(err, ErrResourceConflict) {
			t.Fatal("ownership mutation allowed", err)
		}
	}
	other := r
	other.Owner.ID = "30000000-0000-4000-8000-000000000002"
	if err := s.RegisterTaskResource(ctx, other); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("registration conflict allowed", err)
	}
}

func TestTaskResourceInventoryAdvancesAndDoesNotMutate(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if err := s.RegisterTaskResource(ctx, syntheticResource(t, i)); err != nil {
			t.Fatal(err)
		}
	}
	first, next, err := s.TaskResourcePage(ctx, "", 2, true)
	if err != nil || len(first) != 2 || next != first[1].ID {
		t.Fatal(first, next, err)
	}
	last, next, err := s.TaskResourcePage(ctx, next, 2, true)
	if err != nil || len(last) != 1 || next != "" || last[0].ID == first[0].ID {
		t.Fatal(last, next, err)
	}
	if n, err := s.EventCount(ctx, "resource.registered"); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	_, err = s.UpdateTaskResource(ctx, first[0].ID, "resource.removed", time.Now(), func(r *taskresource.Resource) error { r.LastOutcome = "removed"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := s.TaskResourcePage(ctx, "", taskresource.PageRowsLimit, true)
	if err != nil || len(active) != 2 {
		t.Fatal(active, err)
	}
	all, _, err := s.TaskResourcePage(ctx, "", taskresource.PageRowsLimit, false)
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
	if _, _, err := s.TaskResourcePage(ctx, "", taskresource.PageRowsLimit+1, true); err == nil {
		t.Fatal("unbounded inventory admitted")
	}
}

func TestTaskResourceUnknownProtectionFieldsAreNotDiscarded(t *testing.T) {
	for _, change := range []string{"new field", "null pin", "missing pin", "null lifecycle"} {
		t.Run(change, func(t *testing.T) {
			r := syntheticResource(t, 1)
			body, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "new field":
				fields["future_protection"] = true
			case "null pin":
				fields["pinned"] = nil
			case "missing pin":
				delete(fields, "pinned")
			case "null lifecycle":
				fields["last_outcome"] = nil
			}
			body, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeTaskResource(string(body)); err == nil {
				t.Fatal("unknown protection was ignored")
			}
		})
	}
}
