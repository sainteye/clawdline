//go:build darwin || linux

package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestOrdinaryCallbacksCanAttachProjectCacheWithoutACompileLease(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	commitFile(t, project, "source.go", "package fixture\n")
	var records []Record
	for i := 0; i < 2; i++ {
		req := callbackRequest(t, uuid.NewString(), "sh", "-c", `while ! test -e release; do sleep 0.05; done`)
		req.Dir = project
		if _, err := b.StartCallback(ctx, req); err != nil {
			t.Fatal(err)
		}
		end := time.Now().Add(5 * time.Second)
		for {
			r, _, err := b.Record(ctx, req.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Callback.StartedAt.IsZero() {
				records = append(records, r)
				break
			}
			if time.Now().After(end) {
				t.Fatal("ordinary callback not started")
			}
			time.Sleep(time.Millisecond)
		}
	}
	defer func() {
		_ = os.WriteFile(filepath.Join(project, "release"), nil, 0600)
		for _, r := range records {
			settledWithin(t, b, ctx, r.ID, 5*time.Second)
		}
	}()
	first, err := b.UseCallbackGoCache(ctx, records[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.UseCallbackGoCache(ctx, records[1].ID)
	if err != nil || first.Resource.ID != second.Resource.ID || first.Path != second.Path {
		t.Fatal("checks did not share their Project cache", first, second, err)
	}
	st, err := b.Store.Lease(ctx, ResourceCompile, compileKey)
	if err != nil || st.Holder != nil || len(st.Waiters) > 0 {
		t.Fatal("ordinary checks reserved native compilation", st, err)
	}
	plan, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{first.Resource.ID})
	if err != nil || plan.Items[0].Decision.Outcome != "kept" {
		t.Fatal("active ordinary check cache became disposable", plan, err)
	}
	again, err := b.UseCallbackGoCache(ctx, records[0].ID)
	if err != nil || again.Path != first.Path {
		t.Fatal(again, err)
	}
}

func TestCompletionNotifiesManagedReclaimerWithoutWaitingForLegacyInterval(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.ReclaimAuto = true
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", `printf temporary > "$GOCACHE/rebuildable"; printf retained > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 5*time.Second)
	end := time.Now().Add(5 * time.Second)
	for {
		row, err := b.Store.TaskResource(ctx, filepath.Base(r.Callback.CachePath))
		if err != nil {
			t.Fatal(err)
		}
		removed := row.Class == taskresource.Cache && row.LastOutcome == "removed"
		b.managedCleanup.mu.Lock()
		running := b.managedCleanup.running
		b.managedCleanup.mu.Unlock()
		if removed && !running {
			break
		}
		if time.Now().After(end) {
			t.Fatal("completed cache did not receive an event-driven safe sweep", row)
		}
		time.Sleep(time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(r.Dir, "artifacts", "result.txt"))
	if err != nil || string(raw) != "retained" {
		t.Fatal("result lost", err)
	}
}
